package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"substrackt/internal/domain"
)

func TestSourceCRUDAndMailboxCursor(t *testing.T) {
	ctx := context.Background()
	st := testStore(t)

	src := domain.Source{
		Kind:            domain.SourceEmail,
		Name:            "Personal mail",
		Config:          domain.EmailSourceConfig{Address: "imap.example.com:993", Username: "me@example.com", Mailbox: "INBOX", InitialDays: 30, MaxMessages: 50},
		SecretRef:       "source:test",
		Enabled:         true,
		ScheduleMinutes: 15,
	}
	if err := st.CreateSource(ctx, &src); err != nil {
		t.Fatalf("create: %v", err)
	}
	if src.ID == "" {
		t.Fatal("create should assign an ID")
	}

	got, err := st.GetSource(ctx, src.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Name != src.Name || got.Kind != domain.SourceEmail || got.Config.Address != src.Config.Address {
		t.Fatalf("roundtrip = %+v", got)
	}
	if got.SecretRef != "source:test" || !got.Enabled || got.ScheduleMinutes != 15 {
		t.Fatalf("fields = %+v", got)
	}

	got.Enabled = false
	got.ScheduleMinutes = 60
	got.Config.MaxMessages = 10
	if err := st.UpdateSource(ctx, &got); err != nil {
		t.Fatalf("update: %v", err)
	}
	updated, err := st.GetSource(ctx, src.ID)
	if err != nil {
		t.Fatalf("get updated: %v", err)
	}
	if updated.Enabled || updated.ScheduleMinutes != 60 || updated.Config.MaxMessages != 10 {
		t.Fatalf("updated = %+v", updated)
	}

	if err := st.MarkSourceError(ctx, src.ID, "login failed"); err != nil {
		t.Fatalf("mark error: %v", err)
	}
	failed, err := st.GetSource(ctx, src.ID)
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if failed.LastError != "login failed" || !failed.LastSyncAt.IsZero() {
		t.Fatalf("error state = %+v", failed)
	}

	now := time.Now().UTC().Truncate(time.Second)
	if err := st.MarkSourceSynced(ctx, src.ID, now); err != nil {
		t.Fatalf("mark synced: %v", err)
	}
	synced, err := st.GetSource(ctx, src.ID)
	if err != nil {
		t.Fatalf("get synced: %v", err)
	}
	if synced.LastError != "" || !synced.LastSyncAt.Equal(now) {
		t.Fatalf("synced state = %+v", synced)
	}

	cursor := domain.MailboxCursor{SourceID: src.ID, Mailbox: "INBOX", UIDValidity: 7, LastUID: 100}
	if err := st.SaveMailboxCursor(ctx, cursor); err != nil {
		t.Fatalf("save cursor: %v", err)
	}
	loaded, err := st.GetMailboxCursor(ctx, src.ID, "INBOX")
	if err != nil {
		t.Fatalf("get cursor: %v", err)
	}
	if loaded.UIDValidity != 7 || loaded.LastUID != 100 {
		t.Fatalf("cursor = %+v", loaded)
	}

	cursor.UIDValidity = 8
	cursor.LastUID = 120
	if err := st.SaveMailboxCursor(ctx, cursor); err != nil {
		t.Fatalf("update cursor: %v", err)
	}
	loaded, err = st.GetMailboxCursor(ctx, src.ID, "INBOX")
	if err != nil {
		t.Fatalf("get updated cursor: %v", err)
	}
	if loaded.UIDValidity != 8 || loaded.LastUID != 120 {
		t.Fatalf("updated cursor = %+v", loaded)
	}

	list, err := st.ListSources(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 || list[0].ID != src.ID {
		t.Fatalf("list = %+v", list)
	}

	if err := st.DeleteSource(ctx, src.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := st.GetSource(ctx, src.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("get deleted error = %v", err)
	}
	if _, err := st.GetMailboxCursor(ctx, src.ID, "INBOX"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cursor should be deleted with the source: %v", err)
	}
	if _, err := st.GetSource(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing source error = %v", err)
	}
}
