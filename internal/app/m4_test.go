package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"substrackt/internal/browser"
)

type fakeBrowserEngine struct {
	ready       bool
	readyError  string
	installs    int
	loginState  string
	loginErr    error
	page        browser.PageData
	fetchErr    error
	fetches     int
	closed      bool
	lastOptions browser.Options
}

func (f *fakeBrowserEngine) Ready(options browser.Options) (bool, string) {
	f.lastOptions = options
	return f.ready, f.readyError
}

func (f *fakeBrowserEngine) Install(ctx context.Context, options browser.Options, progress func(string)) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f.lastOptions = options
	f.installs++
	f.ready = true
	return nil
}

func (f *fakeBrowserEngine) Login(ctx context.Context, connector browser.Connector, options browser.Options, timeout time.Duration) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	f.lastOptions = options
	if f.loginErr != nil {
		return "", f.loginErr
	}
	return f.loginState, nil
}

func (f *fakeBrowserEngine) Fetch(ctx context.Context, connector browser.Connector, options browser.Options, storageState, pageURL string, timeout time.Duration) (browser.PageData, error) {
	if err := ctx.Err(); err != nil {
		return browser.PageData{}, err
	}
	f.lastOptions = options
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

func TestSettingsBrowserRuntime(t *testing.T) {
	a := newTestApp(t)

	saved, err := a.settings.Update(Settings{
		BaseCurrency:      "USD",
		LLMTimeoutSeconds: 60,
		BrowserRuntime:    " CHROME ",
	})
	if err != nil {
		t.Fatalf("update browser runtime: %v", err)
	}
	if saved.BrowserRuntime != "chrome" {
		t.Fatalf("browser runtime = %q, want chrome", saved.BrowserRuntime)
	}
	if saved.BrowserSignIn != "default" {
		t.Fatalf("browser sign-in = %q, want default", saved.BrowserSignIn)
	}
	status := a.sources.BrowserRuntime()
	if status.Selected != "chrome" {
		t.Fatalf("selected = %q, want chrome", status.Selected)
	}
	if status.SignIn != "default" {
		t.Fatalf("sign-in = %q, want default", status.SignIn)
	}
	if len(status.Options) == 0 {
		t.Fatal("runtime status should list the available browsers")
	}

	if _, err := a.settings.Update(Settings{
		BaseCurrency:      "USD",
		LLMTimeoutSeconds: 60,
		BrowserRuntime:    "magic",
	}); err == nil {
		t.Fatal("expected an unknown browser runtime to be rejected")
	}
	if _, err := a.settings.Update(Settings{
		BaseCurrency:      "USD",
		LLMTimeoutSeconds: 60,
		BrowserSignIn:     "magic",
	}); err == nil {
		t.Fatal("expected an unknown sign-in method to be rejected")
	}

	isolated, err := a.settings.Update(Settings{
		BaseCurrency:      "USD",
		LLMTimeoutSeconds: 60,
		BrowserSignIn:     " ISOLATED ",
	})
	if err != nil {
		t.Fatalf("set isolated sign-in: %v", err)
	}
	if isolated.BrowserSignIn != "isolated" {
		t.Fatalf("browser sign-in = %q, want isolated", isolated.BrowserSignIn)
	}

	reset, err := a.settings.Reset()
	if err != nil {
		t.Fatalf("reset: %v", err)
	}
	if reset.BrowserRuntime != "auto" {
		t.Fatalf("reset browser runtime = %q, want auto", reset.BrowserRuntime)
	}
	if reset.BrowserSignIn != "default" {
		t.Fatalf("reset browser sign-in = %q, want default", reset.BrowserSignIn)
	}
	status = a.sources.BrowserRuntime()
	if status.Selected != "auto" {
		t.Fatalf("selected after reset = %q, want auto", status.Selected)
	}
	foundBundled := false
	for _, option := range status.Options {
		if option.ID == "bundled" {
			foundBundled = true
		}
	}
	if !foundBundled {
		t.Fatalf("options = %+v, want bundled Chromium", status.Options)
	}
}

func TestDefaultBrowserSignInImportsCookies(t *testing.T) {
	a := newTestApp(t)
	if _, err := a.settings.Update(Settings{
		BaseCurrency:      "USD",
		LLMTimeoutSeconds: 60,
		BrowserSignIn:     "default",
	}); err != nil {
		t.Fatalf("enable default sign-in: %v", err)
	}

	created, err := a.sources.Create(browserInput())
	if err != nil {
		t.Fatalf("create browser source: %v", err)
	}

	var opened string
	a.browser.openURL = func(url string) error {
		opened = url
		return nil
	}
	a.browser.cookieSources = func() ([]browser.CookieSource, error) {
		return []browser.CookieSource{{Name: "Test browser", Kind: "brave"}}, nil
	}
	a.browser.waitSession = func(ctx context.Context, sources []browser.CookieSource, connector browser.Connector, pageURL string, timeout time.Duration) (string, error) {
		return `{"cookies":[{"name":"logged_in","value":"yes","domain":".github.com","path":"/","expires":-1}],"origins":[]}`, nil
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
			t.Fatal("timed out waiting for default-browser sign-in")
		}
	}
	if opened != "https://github.com/settings/billing" {
		t.Fatalf("opened %q, want the GitHub billing page", opened)
	}
	updated, err := a.sources.Get(created.ID)
	if err != nil {
		t.Fatalf("get source: %v", err)
	}
	if !updated.HasSecret {
		t.Fatal("default-browser sign-in should save a session")
	}
	session, err := a.secrets.Get(browserSessionRef(created.ID))
	if err != nil || !strings.Contains(session, "logged_in") {
		t.Fatalf("session secret = %q, %v", session, err)
	}
}

func TestDefaultBrowserSignInReportsUnavailableSource(t *testing.T) {
	a := newTestApp(t)
	if _, err := a.settings.Update(Settings{
		BaseCurrency:      "USD",
		LLMTimeoutSeconds: 60,
		BrowserSignIn:     "default",
	}); err != nil {
		t.Fatalf("enable default sign-in: %v", err)
	}
	created, err := a.sources.Create(browserInput())
	if err != nil {
		t.Fatalf("create browser source: %v", err)
	}
	a.browser.cookieSources = func() ([]browser.CookieSource, error) {
		return nil, errors.New("default browser is not supported")
	}
	if err := a.sources.BeginSignIn(created.ID); err == nil {
		t.Fatal("expected an unsupported default browser to be reported")
	}
}
