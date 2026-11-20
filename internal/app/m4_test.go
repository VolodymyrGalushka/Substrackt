package app

import (
	"context"
	"strings"
	"testing"
	"time"

	"substrackt/internal/browser"
)

type fakeBrowserEngine struct {
	ready      bool
	readyError string
	installs   int
	loginState string
	loginErr   error
	page       browser.PageData
	fetchErr   error
	fetches    int
	closed     bool
}

func (f *fakeBrowserEngine) Ready() (bool, string) { return f.ready, f.readyError }

func (f *fakeBrowserEngine) Install(ctx context.Context, progress func(string)) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f.installs++
	f.ready = true
	return nil
}

func (f *fakeBrowserEngine) Login(ctx context.Context, connector browser.Connector, timeout time.Duration) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if f.loginErr != nil {
		return "", f.loginErr
	}
	return f.loginState, nil
}

func (f *fakeBrowserEngine) Fetch(ctx context.Context, connector browser.Connector, storageState, pageURL string, timeout time.Duration) (browser.PageData, error) {
	if err := ctx.Err(); err != nil {
		return browser.PageData{}, err
	}
	f.fetches++
	if f.fetchErr != nil {
		return browser.PageData{}, f.fetchErr
	}
	return f.page, nil
}

func (f *fakeBrowserEngine) Close() error {
	f.closed = true
	return nil
}

func browserInput() SourceInput {
	return SourceInput{
		Kind:            "browser",
		Name:            "GitHub billing",
		Enabled:         true,
		ScheduleMinutes: 60,
		Config: SourceConfig{
			Connector:      "github",
			TimeoutSeconds: 30,
		},
	}
}

func TestBrowserSourceSignInAndSync(t *testing.T) {
	a := newTestApp(t)
	a.bengine.page = browser.PageData{
		URL:   "https://github.com/settings/billing",
		Title: "Billing and plans",
		Text:  "GitHub\nPro plan\n$4.00 per month\nNext billing date: December 1, 2026",
	}

	created, err := a.sources.Create(browserInput())
	if err != nil {
		t.Fatalf("create browser source: %v", err)
	}
	if created.HasSecret {
		t.Fatal("a new browser source should start signed out")
	}
	if created.Config.Connector != "github" || created.Config.TimeoutSeconds != 30 {
		t.Fatalf("config = %+v", created.Config)
	}

	statuses := make(chan BrowserLoginStatus, 8)
	a.browser.SetLoginNotifier(func(status BrowserLoginStatus) { statuses <- status })
	if err := a.sources.BeginSignIn(created.ID); err != nil {
		t.Fatalf("begin sign-in: %v", err)
	}
	var saved BrowserLoginStatus
	deadline := time.After(3 * time.Second)
	for saved.State == "" {
		select {
		case status := <-statuses:
			if status.State == "failed" {
				t.Fatalf("sign-in failed: %s", status.Error)
			}
			if status.State == "saved" {
				saved = status
			}
		case <-deadline:
			t.Fatal("timed out waiting for sign-in")
		}
	}
	if saved.SourceID != created.ID {
		t.Fatalf("saved source = %q, want %q", saved.SourceID, created.ID)
	}

	updated, err := a.sources.Get(created.ID)
	if err != nil {
		t.Fatalf("get source: %v", err)
	}
	if !updated.HasSecret {
		t.Fatal("saved sign-in should mark the source as signed in")
	}
	session, err := a.secrets.Get(browserSessionRef(created.ID))
	if err != nil || !strings.Contains(session, "cookies") {
		t.Fatalf("session secret = %q, %v", session, err)
	}

	if _, err := a.sources.SyncNow(created.ID); err != nil {
		t.Fatalf("queue sync: %v", err)
	}
	a.drain(t)

	if a.bengine.fetches != 1 {
		t.Fatalf("fetches = %d, want 1", a.bengine.fetches)
	}
	synced, err := a.sources.Get(created.ID)
	if err != nil {
		t.Fatalf("get synced source: %v", err)
	}
	if synced.LastSyncAt == "" || synced.LastError != "" {
		t.Fatalf("source status = %+v", synced)
	}
	items, err := a.review.List("")
	if err != nil {
		t.Fatalf("list review: %v", err)
	}
	if len(items) != 1 || items[0].Source != "browser" {
		t.Fatalf("review items = %+v", items)
	}
	if items[0].VendorName != "GitHub" {
		t.Fatalf("vendor = %q, want GitHub", items[0].VendorName)
	}
}

func TestBrowserExpiredSessionClearsSignIn(t *testing.T) {
	a := newTestApp(t)
	a.bengine.page = browser.PageData{URL: "https://github.com/settings/billing", Title: "Billing", Text: strings.Repeat("billing ", 10)}

	created, err := a.sources.Create(browserInput())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	a.browser.SetLoginNotifier(func(BrowserLoginStatus) {})
	if err := a.sources.BeginSignIn(created.ID); err != nil {
		t.Fatalf("begin sign-in: %v", err)
	}
	// Wait until the session is stored.
	for i := 0; i < 50; i++ {
		src, err := a.sources.Get(created.ID)
		if err == nil && src.HasSecret {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	a.bengine.fetchErr = browser.ErrSessionExpired
	if _, err := a.sources.SyncNow(created.ID); err != nil {
		t.Fatalf("queue sync: %v", err)
	}
	a.drain(t)

	src, err := a.sources.Get(created.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if src.HasSecret {
		t.Fatal("expired session should be removed so the UI asks for a new sign-in")
	}
	if !strings.Contains(strings.ToLower(src.LastError), "expired") {
		t.Fatalf("last error = %q, want an expired-session message", src.LastError)
	}
}

func TestBrowserSourceValidation(t *testing.T) {
	a := newTestApp(t)

	unknown := browserInput()
	unknown.Config.Connector = "not-a-vendor"
	if _, err := a.sources.Create(unknown); err == nil {
		t.Fatal("expected an unknown connector to be rejected")
	}

	offsite := browserInput()
	offsite.Config.BillingURL = "https://example.com/billing"
	if _, err := a.sources.Create(offsite); err == nil {
		t.Fatal("expected an off-site billing URL to be rejected")
	}

	badTimeout := browserInput()
	badTimeout.Config.TimeoutSeconds = 2
	if _, err := a.sources.Create(badTimeout); err == nil {
		t.Fatal("expected a short timeout to be rejected")
	}

	// An unknown connector must also fail when an existing source is edited.
	created, err := a.sources.Create(browserInput())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	edit := browserInput()
	edit.Config.Connector = "nope"
	if _, err := a.sources.Update(created.ID, edit); err == nil {
		t.Fatal("expected an unknown connector on update to be rejected")
	}
}

func TestBrowserTestRequiresSignIn(t *testing.T) {
	a := newTestApp(t)
	created, err := a.sources.Create(browserInput())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	result, err := a.sources.Test(created.ID, browserInput())
	if err != nil {
		t.Fatalf("test returned a Go error: %v", err)
	}
	if result.OK || !strings.Contains(result.Error, "signed in") {
		t.Fatalf("test result = %+v", result)
	}

	a.browser.SetLoginNotifier(func(BrowserLoginStatus) {})
	if err := a.sources.BeginSignIn(created.ID); err != nil {
		t.Fatalf("begin sign-in: %v", err)
	}
	for i := 0; i < 50; i++ {
		src, err := a.sources.Get(created.ID)
		if err == nil && src.HasSecret {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	a.bengine.page = browser.PageData{URL: "https://github.com/settings/billing", Title: "Billing", Text: strings.Repeat("billing ", 10)}
	result, err = a.sources.Test(created.ID, browserInput())
	if err != nil {
		t.Fatalf("test: %v", err)
	}
	if !result.OK || !strings.Contains(result.Message, "Billing") {
		t.Fatalf("test result = %+v", result)
	}
}

func TestBrowserLoginCancellation(t *testing.T) {
	a := newTestApp(t)
	created, err := a.sources.Create(browserInput())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := a.sources.CancelSignIn(created.ID); err == nil {
		t.Fatal("cancelling when no sign-in is running should fail")
	}
	// The fake returns immediately, so start and cancel a sign-in; either the
	// saved or cancelled state is acceptable, but CancelSignIn must not block.
	statuses := make(chan BrowserLoginStatus, 4)
	a.browser.SetLoginNotifier(func(status BrowserLoginStatus) { statuses <- status })
	if err := a.sources.BeginSignIn(created.ID); err != nil {
		t.Fatalf("begin sign-in: %v", err)
	}
	_ = a.sources.CancelSignIn(created.ID)
	select {
	case <-statuses:
	case <-time.After(time.Second):
		t.Fatal("no sign-in status was emitted")
	}
}

var _ browser.Engine = (*fakeBrowserEngine)(nil)
