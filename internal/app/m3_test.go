package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"substrackt/internal/domain"
	"substrackt/internal/mail"
	"substrackt/internal/secrets"
	"substrackt/internal/store"
)

type fetchCall struct {
	mailbox string
	lastUID uint32
	since   time.Time
	limit   int
}

type fetchResult struct {
	info     mail.MailboxInfo
	messages []mail.Message
	err      error
}

type fakeMailClient struct {
	results   []fetchResult
	calls     []fetchCall
	info      mail.MailboxInfo
	selectErr error
	aborted   bool
}

func (f *fakeMailClient) SelectInfo(mailbox string) (mail.MailboxInfo, error) {
	if f.selectErr != nil {
		return mail.MailboxInfo{}, f.selectErr
	}
	return f.info, nil
}

func (f *fakeMailClient) FetchSince(mailbox string, lastUID uint32, since time.Time, limit int) (mail.MailboxInfo, []mail.Message, error) {
	f.calls = append(f.calls, fetchCall{mailbox: mailbox, lastUID: lastUID, since: since, limit: limit})
	index := len(f.calls) - 1
	if index >= len(f.results) {
		return mail.MailboxInfo{}, nil, nil
	}
	result := f.results[index]
	return result.info, result.messages, result.err
}

func (f *fakeMailClient) Abort() error {
	f.aborted = true
	return nil
}

func (f *fakeMailClient) Close() error { return nil }

type fakeDialer struct {
	clients []*fakeMailClient
	configs []mail.Config
	err     error
}

func (d *fakeDialer) dial(cfg mail.Config) (emailClient, error) {
	d.configs = append(d.configs, cfg)
	if d.err != nil {
		return nil, d.err
	}
	if len(d.clients) == 0 {
		return nil, errors.New("no fake IMAP client queued")
	}
	client := d.clients[0]
	d.clients = d.clients[1:]
	return client, nil
}

func (a *testApp) setDialer(d *fakeDialer) {
	a.email.dial = d.dial
	a.sources.dial = d.dial
}

func testSourceInput(name, password string) SourceInput {
	return SourceInput{
		Kind:            "email",
		Name:            name,
		Enabled:         true,
		ScheduleMinutes: 15,
		Config: SourceConfig{
			Address:     "imap.example.com:993",
			Username:    "me@example.com",
			Mailbox:     "INBOX",
			InitialDays: 30,
			MaxMessages: 50,
		},
		Password: password,
	}
}

func TestSourcesServiceCRUDKeepsPasswordPrivate(t *testing.T) {
	a := newTestApp(t)

	created, err := a.sources.Create(testSourceInput("Personal mail", "top-secret"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.ID == "" || !created.HasSecret || !created.Enabled {
		t.Fatalf("created = %+v", created)
	}
	if created.Config.Address != "imap.example.com:993" || created.Config.Mailbox != "INBOX" {
		t.Fatalf("config = %+v", created.Config)
	}
	encoded, err := json.Marshal(created)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(encoded), "top-secret") {
		t.Fatal("the source DTO leaked the password")
	}

	ref := "source:" + created.ID
	stored, err := a.secrets.Get(ref)
	if err != nil {
		t.Fatalf("get secret: %v", err)
	}
	if stored != "top-secret" {
		t.Fatalf("stored password = %q", stored)
	}

	// An empty password on update keeps the existing credential.
	update := testSourceInput("Personal mail", "")
	update.Enabled = false
	updated, err := a.sources.Update(created.ID, update)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Enabled || !updated.HasSecret {
		t.Fatalf("updated = %+v", updated)
	}
	stored, err = a.secrets.Get(ref)
	if err != nil || stored != "top-secret" {
		t.Fatalf("secret after empty update = %q, %v", stored, err)
	}

	// A new password replaces it.
	if _, err := a.sources.Update(created.ID, testSourceInput("Personal mail", "new-secret")); err != nil {
		t.Fatalf("update password: %v", err)
	}
	stored, err = a.secrets.Get(ref)
	if err != nil || stored != "new-secret" {
		t.Fatalf("replaced secret = %q, %v", stored, err)
	}

	// Configuration is validated before a secret is written.
	bad := testSourceInput("Bad", "secret")
	bad.Config.Address = "not-host-port"
	if _, err := a.sources.Create(bad); err == nil {
		t.Fatal("expected an invalid address to be rejected")
	}
	noPassword := testSourceInput("No password", "")
	if _, err := a.sources.Create(noPassword); err == nil {
		t.Fatal("expected a missing password to be rejected")
	}

	list, err := a.sources.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 || list[0].ID != created.ID {
		t.Fatalf("list = %+v", list)
	}
	got, err := a.sources.Get(created.ID)
	if err != nil || got.ID != created.ID {
		t.Fatalf("get = %+v, %v", got, err)
	}

	if err := a.sources.Delete(created.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := a.secrets.Get(ref); !errors.Is(err, secrets.ErrNotFound) {
		t.Fatalf("secret after delete = %v, want ErrNotFound", err)
	}
	if _, err := a.sources.Get(created.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("get after delete = %v", err)
	}
}

func TestEmailSyncIngestsAndAdvancesCursor(t *testing.T) {
	a := newTestApp(t)
	src, err := a.sources.Create(testSourceInput("Personal mail", "password"))
	if err != nil {
		t.Fatalf("create source: %v", err)
	}

	fixed := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	a.email.now = func() time.Time { return fixed }

	first := &fakeMailClient{results: []fetchResult{{
		info: mail.MailboxInfo{NumMessages: 2, UIDNext: 13, UIDValidity: 9},
		messages: []mail.Message{
			{
				UID:       11,
				Subject:   "Your Acme Cloud receipt",
				From:      "Acme Cloud <billing@acme.example>",
				MessageID: "<acme-1@acme.example>",
				Date:      fixed.Add(-2 * time.Hour),
				Text:      "Acme Cloud Pro\n$9.99 per month\nNext payment January 15, 2027",
			},
			{
				UID:       12,
				Subject:   "Beta Cloud invoice",
				From:      "Beta Cloud <billing@beta.example>",
				MessageID: "<beta-1@beta.example>",
				Date:      fixed.Add(-time.Hour),
				Text:      "Beta Cloud\n$6.00 per month",
			},
		},
	}}}
	dialer := &fakeDialer{clients: []*fakeMailClient{first}}
	a.setDialer(dialer)

	job, err := a.sources.SyncNow(src.ID)
	if err != nil {
		t.Fatalf("sync now: %v", err)
	}
	a.drain(t)

	finished, err := a.jobsvc.Get(job.ID)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if finished.Status != string(domain.JobSucceeded) {
		t.Fatalf("job = %s (%s)", finished.Status, finished.Error)
	}
	if !strings.Contains(finished.Message, "2 message(s) synced") {
		t.Fatalf("job message = %q", finished.Message)
	}
	if len(first.calls) != 1 || first.calls[0].lastUID != 0 || first.calls[0].since.IsZero() {
		t.Fatalf("first fetch call = %+v", first.calls)
	}
	if first.calls[0].limit != 50 || first.calls[0].mailbox != "INBOX" {
		t.Fatalf("fetch options = %+v", first.calls[0])
	}
	if len(dialer.configs) != 1 || dialer.configs[0].Password != "password" {
		t.Fatalf("dial config = %+v", dialer.configs)
	}

	count, err := a.review.Count()
	if err != nil {
		t.Fatalf("review count: %v", err)
	}
	if count != 2 {
		t.Fatalf("pending review items = %d, want 2", count)
	}

	cursor, err := a.store.GetMailboxCursor(context.Background(), src.ID, "INBOX")
	if err != nil {
		t.Fatalf("get cursor: %v", err)
	}
	if cursor.LastUID != 12 || cursor.UIDValidity != 9 {
		t.Fatalf("cursor = %+v", cursor)
	}

	synced, err := a.sources.Get(src.ID)
	if err != nil {
		t.Fatalf("get source: %v", err)
	}
	if synced.LastSyncAt == "" || synced.LastError != "" {
		t.Fatalf("source status = %+v", synced)
	}
	if len(a.sourceStatus) != 1 || a.sourceStatus[0].ID != src.ID || a.sourceStatus[0].LastSyncAt == "" {
		t.Fatalf("source notifications = %+v", a.sourceStatus)
	}

	// The next sync resumes after the stored UID instead of rescanning.
	second := &fakeMailClient{results: []fetchResult{{
		info:     mail.MailboxInfo{NumMessages: 3, UIDNext: 14, UIDValidity: 9},
		messages: []mail.Message{{UID: 13, Subject: "Gamma Cloud", From: "Gamma Cloud <billing@gamma.example>", Date: fixed.Add(-time.Minute), Text: "Gamma Cloud\n$4.00 per month"}},
	}}}
	a.setDialer(&fakeDialer{clients: []*fakeMailClient{second}})
	if _, err := a.sources.SyncNow(src.ID); err != nil {
		t.Fatalf("second sync: %v", err)
	}
	a.drain(t)

	if len(second.calls) != 1 || second.calls[0].lastUID != 12 {
		t.Fatalf("incremental fetch = %+v", second.calls)
	}
	if !second.calls[0].since.IsZero() {
		t.Fatalf("incremental fetch should not use a date window: %+v", second.calls[0])
	}
	cursor, err = a.store.GetMailboxCursor(context.Background(), src.ID, "INBOX")
	if err != nil {
		t.Fatalf("get cursor: %v", err)
	}
	if cursor.LastUID != 13 {
		t.Fatalf("cursor after second sync = %+v", cursor)
	}
}

func TestEmailSyncRescansAfterUIDValidityChange(t *testing.T) {
	a := newTestApp(t)
	src, err := a.sources.Create(testSourceInput("Personal mail", "password"))
	if err != nil {
		t.Fatalf("create source: %v", err)
	}
	if err := a.store.SaveMailboxCursor(context.Background(), domain.MailboxCursor{
		SourceID: src.ID, Mailbox: "INBOX", UIDValidity: 1, LastUID: 50,
	}); err != nil {
		t.Fatalf("seed cursor: %v", err)
	}

	fixed := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	a.email.now = func() time.Time { return fixed }
	client := &fakeMailClient{results: []fetchResult{
		{info: mail.MailboxInfo{NumMessages: 1, UIDNext: 6, UIDValidity: 2}},
		{
			info:     mail.MailboxInfo{NumMessages: 1, UIDNext: 6, UIDValidity: 2},
			messages: []mail.Message{{UID: 5, Subject: "Acme Cloud", From: "Acme Cloud <billing@acme.example>", Date: fixed, Text: "Acme Cloud\n$9.99 per month"}},
		},
	}}
	a.setDialer(&fakeDialer{clients: []*fakeMailClient{client}})

	if _, err := a.sources.SyncNow(src.ID); err != nil {
		t.Fatalf("sync now: %v", err)
	}
	a.drain(t)

	if len(client.calls) != 2 {
		t.Fatalf("fetch calls = %+v, want an initial and a rescan call", client.calls)
	}
	if client.calls[0].lastUID != 50 || client.calls[1].lastUID != 0 {
		t.Fatalf("fetch sequence = %+v", client.calls)
	}
	cursor, err := a.store.GetMailboxCursor(context.Background(), src.ID, "INBOX")
	if err != nil {
		t.Fatalf("get cursor: %v", err)
	}
	if cursor.UIDValidity != 2 || cursor.LastUID != 5 {
		t.Fatalf("cursor = %+v", cursor)
	}
}

func TestEmailSyncFailureRecordsSourceError(t *testing.T) {
	a := newTestApp(t)
	src, err := a.sources.Create(testSourceInput("Personal mail", "password"))
	if err != nil {
		t.Fatalf("create source: %v", err)
	}
	client := &fakeMailClient{results: []fetchResult{{err: errors.New("imap is down")}}}
	a.setDialer(&fakeDialer{clients: []*fakeMailClient{client}})

	job, err := a.sources.SyncNow(src.ID)
	if err != nil {
		t.Fatalf("sync now: %v", err)
	}
	a.drain(t)

	finished, err := a.jobsvc.Get(job.ID)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if finished.Status != string(domain.JobFailed) || !strings.Contains(finished.Error, "imap is down") {
		t.Fatalf("job = %+v", finished)
	}
	failed, err := a.sources.Get(src.ID)
	if err != nil {
		t.Fatalf("get source: %v", err)
	}
	if !strings.Contains(failed.LastError, "imap is down") {
		t.Fatalf("source last error = %q", failed.LastError)
	}
	if failed.LastSyncAt != "" {
		t.Fatalf("failed sync must not look successful: %+v", failed)
	}
	if _, err := a.store.GetMailboxCursor(context.Background(), src.ID, "INBOX"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("cursor after failure = %v, want ErrNotFound", err)
	}
	if len(a.sourceStatus) != 1 || !strings.Contains(a.sourceStatus[0].LastError, "imap is down") {
		t.Fatalf("source notifications = %+v", a.sourceStatus)
	}
}

func TestSourceTestUsesStoredPassword(t *testing.T) {
	a := newTestApp(t)
	src, err := a.sources.Create(testSourceInput("Personal mail", "stored-password"))
	if err != nil {
		t.Fatalf("create source: %v", err)
	}

	client := &fakeMailClient{info: mail.MailboxInfo{NumMessages: 42}}
	dialer := &fakeDialer{clients: []*fakeMailClient{client}}
	a.sources.dial = dialer.dial

	result, err := a.sources.Test(src.ID, testSourceInput("Personal mail", ""))
	if err != nil {
		t.Fatalf("test: %v", err)
	}
	if !result.OK || result.Mailbox != "INBOX" || result.Messages != 42 {
		t.Fatalf("result = %+v", result)
	}
	if len(dialer.configs) != 1 || dialer.configs[0].Password != "stored-password" {
		t.Fatalf("dial config = %+v", dialer.configs)
	}

	bad := testSourceInput("Bad", "secret")
	bad.Config.Address = "nope"
	result, err = a.sources.Test("", bad)
	if err != nil {
		t.Fatalf("bad test returned a Go error: %v", err)
	}
	if result.OK || result.Error == "" {
		t.Fatalf("bad config result = %+v", result)
	}
}

func TestEmailSyncCancellationDoesNotRecordSourceError(t *testing.T) {
	a := newTestApp(t)
	src, err := a.sources.Create(testSourceInput("Personal mail", "password"))
	if err != nil {
		t.Fatalf("create source: %v", err)
	}

	client := &fakeMailClient{results: []fetchResult{{
		info: mail.MailboxInfo{NumMessages: 1, UIDNext: 2, UIDValidity: 3},
		messages: []mail.Message{{
			UID: 1, Subject: "Acme Cloud", From: "Acme Cloud <billing@acme.example>", Text: "Acme Cloud\n$9.99 per month",
		}},
	}}}
	a.setDialer(&fakeDialer{clients: []*fakeMailClient{client}})

	payload, err := json.Marshal(emailPayload{SourceID: src.ID})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := a.email.RunJob(ctx, domain.Job{ID: "job-cancel", Payload: string(payload)}, nil); err == nil {
		t.Fatal("expected a cancelled sync to return an error")
	}

	got, err := a.sources.Get(src.ID)
	if err != nil {
		t.Fatalf("get source: %v", err)
	}
	if got.LastError != "" || got.LastSyncAt != "" {
		t.Fatalf("cancellation was recorded as a source failure: %+v", got)
	}
}

func TestSourceSchedulerQueuesDueSourcesOnce(t *testing.T) {
	a := newTestApp(t)
	if _, err := a.sources.Create(testSourceInput("Personal mail", "password")); err != nil {
		t.Fatalf("create source: %v", err)
	}

	fixed := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	scheduler := NewSourceScheduler(a.store, a.manager)
	scheduler.now = func() time.Time { return fixed }

	queued, err := scheduler.Tick(context.Background())
	if err != nil || queued != 1 {
		t.Fatalf("first tick = %d, %v", queued, err)
	}
	queued, err = scheduler.Tick(context.Background())
	if err != nil || queued != 0 {
		t.Fatalf("second tick = %d, %v (must not duplicate an in-flight sync)", queued, err)
	}

	scheduler.now = func() time.Time { return fixed.Add(16 * time.Minute) }
	queued, err = scheduler.Tick(context.Background())
	if err != nil || queued != 1 {
		t.Fatalf("tick after schedule = %d, %v", queued, err)
	}
}
