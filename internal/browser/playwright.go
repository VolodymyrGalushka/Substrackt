//go:build !android && !ios

package browser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/mxschmitt/playwright-go"
)

// ErrSessionExpired means the saved browser session is no longer signed in.
var ErrSessionExpired = errors.New("browser session expired; sign in again")

// PageData is the readable text captured from one billing page.
type PageData struct {
	URL   string
	Title string
	Text  string
}

// Engine is the browser automation surface used by the app. Tests use a fake.
type Engine interface {
	Ready(options Options) (bool, string)
	Install(ctx context.Context, options Options, progress func(string)) error
	Login(ctx context.Context, connector Connector, options Options, timeout time.Duration) (storageState string, err error)
	Fetch(ctx context.Context, connector Connector, options Options, storageState string, pageURL string, timeout time.Duration) (PageData, error)
	Close() error
}

// Playwright runs connectors with a lazily started Playwright driver.
type Playwright struct {
	mu     sync.Mutex
	pw     *playwright.Playwright
	closed bool
}

// NewPlaywright creates an engine. The driver starts on first use.
func NewPlaywright() *Playwright { return &Playwright{} }

func (p *Playwright) instance() (*playwright.Playwright, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil, errors.New("browser engine is closed")
	}
	if p.pw != nil {
		return p.pw, nil
	}
	pw, err := playwright.Run()
	if err != nil {
		return nil, fmt.Errorf("start Playwright: %w", err)
	}
	p.pw = pw
	return pw, nil
}

// Ready reports whether the selected browser and the Playwright driver are
// installed. A system browser still needs the driver, but not the bundled
// Chromium download.
func (p *Playwright) Ready(options Options) (bool, string) {
	if options.ExecutablePath != "" {
		if !isExecutableFile(options.ExecutablePath) {
			return false, "the selected browser is not installed"
		}
		if _, err := p.instance(); err != nil {
			return false, err.Error()
		}
		return true, ""
	}
	pw, err := p.instance()
	if err != nil {
		return false, err.Error()
	}
	path := pw.Chromium.ExecutablePath()
	if path == "" {
		return false, "Chromium is not installed for Playwright"
	}
	return true, ""
}

// Install downloads the Playwright driver. The bundled Chromium is downloaded
// only when the selected runtime needs it. It is idempotent.
func (p *Playwright) Install(ctx context.Context, options Options, progress func(string)) error {
	if progress != nil {
		if options.ExecutablePath != "" {
			progress("Downloading the Playwright driver (first run only)")
		} else {
			progress("Downloading the Playwright driver and Chromium (first run only)")
		}
	}
	done := make(chan error, 1)
	go func() {
		done <- playwright.Install(&playwright.RunOptions{
			Browsers:            []string{"chromium"},
			SkipInstallBrowsers: options.ExecutablePath != "",
			Verbose:             false,
		})
	}()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-done:
		if err != nil {
			return fmt.Errorf("install Playwright: %w", err)
		}
	}
	// Drop a stale instance so the next call sees the new installation.
	p.mu.Lock()
	if p.pw != nil {
		_ = p.pw.Stop()
		p.pw = nil
	}
	p.mu.Unlock()
	return nil
}

// Login opens a headed browser for the user to sign in and returns the storage
// state to keep as a secret. It polls the connector's authenticated check until
// the session is usable, the context is cancelled, or the timeout elapses.
func (p *Playwright) Login(ctx context.Context, connector Connector, options Options, timeout time.Duration) (string, error) {
	pw, err := p.instance()
	if err != nil {
		return "", err
	}
	browser, err := pw.Chromium.Launch(launchOptions(options, false))
	if err != nil {
		return "", fmt.Errorf("open browser: %w", err)
	}
	defer func() { _ = browser.Close() }()

	bctx, err := browser.NewContext(playwright.BrowserNewContextOptions{
		Locale:   playwright.String("en-US"),
		Viewport: &playwright.Size{Width: 1280, Height: 900},
	})
	if err != nil {
		return "", fmt.Errorf("create browser context: %w", err)
	}
	defer func() { _ = bctx.Close() }()

	page, err := bctx.NewPage()
	if err != nil {
		return "", fmt.Errorf("open browser page: %w", err)
	}

	stop := closeOnCancel(ctx, browser)
	defer stop()

	if _, err := page.Goto(connector.LoginURL, playwright.PageGotoOptions{
		WaitUntil: playwright.WaitUntilStateDomcontentloaded,
		Timeout:   playwright.Float(30000),
	}); err != nil && ctx.Err() == nil {
		return "", fmt.Errorf("open %s: %w", connector.LoginURL, err)
	}

	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		if page.IsClosed() {
			return "", errors.New("browser window closed before sign-in finished")
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf("sign-in timed out after %s", timeout.Round(time.Second))
		}
		if authed, evalErr := page.Evaluate(connector.AuthenticatedJS); evalErr == nil && authed == true {
			state, err := bctx.StorageState()
			if err != nil {
				return "", fmt.Errorf("capture browser session: %w", err)
			}
			encoded, err := json.Marshal(state)
			if err != nil {
				return "", fmt.Errorf("encode browser session: %w", err)
			}
			return string(encoded), nil
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-ticker.C:
		}
	}
}

// Fetch loads the billing page with a saved session and returns its readable
// text. It reports ErrSessionExpired when the site asks for a sign-in.
func (p *Playwright) Fetch(ctx context.Context, connector Connector, options Options, storageState, pageURL string, timeout time.Duration) (PageData, error) {
	pw, err := p.instance()
	if err != nil {
		return PageData{}, err
	}
	browser, err := pw.Chromium.Launch(launchOptions(options, true))
	if err != nil {
		return PageData{}, fmt.Errorf("open browser: %w", err)
	}
	defer func() { _ = browser.Close() }()

	contextOptions := playwright.BrowserNewContextOptions{
		Locale:   playwright.String("en-US"),
		Viewport: &playwright.Size{Width: 1280, Height: 900},
	}
	if storageState != "" {
		var state playwright.OptionalStorageState
		if err := json.Unmarshal([]byte(storageState), &state); err != nil {
			return PageData{}, fmt.Errorf("decode browser session: %w", err)
		}
		contextOptions.StorageState = &state
	}
	bctx, err := browser.NewContext(contextOptions)
	if err != nil {
		return PageData{}, fmt.Errorf("create browser context: %w", err)
	}
	defer func() { _ = bctx.Close() }()

	page, err := bctx.NewPage()
	if err != nil {
		return PageData{}, fmt.Errorf("open browser page: %w", err)
	}

	stop := closeOnCancel(ctx, browser)
	defer stop()

	if _, err := page.Goto(pageURL, playwright.PageGotoOptions{
		WaitUntil: playwright.WaitUntilStateDomcontentloaded,
		Timeout:   playwright.Float(float64(timeout.Milliseconds())),
	}); err != nil && ctx.Err() == nil {
		return PageData{}, fmt.Errorf("open %s: %w", pageURL, err)
	}
	if ctx.Err() != nil {
		return PageData{}, ctx.Err()
	}
	if authed, evalErr := page.Evaluate(connector.AuthenticatedJS); evalErr == nil && authed != true {
		return PageData{}, ErrSessionExpired
	}

	// A missing ready selector is not fatal: some vendors reword their pages,
	// and the extracted text is still worth parsing.
	_, _ = page.WaitForSelector(connector.ReadySelector, playwright.PageWaitForSelectorOptions{
		Timeout: playwright.Float(5000),
	})
	// Billing pages usually hydrate after DOMContentLoaded; give the network a
	// short idle window without failing the sync when it stays busy.
	_ = page.WaitForLoadState(playwright.PageWaitForLoadStateOptions{
		State:   playwright.LoadStateNetworkidle,
		Timeout: playwright.Float(5000),
	})

	value, err := page.Evaluate(connector.ExtractJS)
	if err != nil {
		return PageData{}, fmt.Errorf("read billing page: %w", err)
	}
	text, _ := value.(string)
	if len(text) < 20 {
		return PageData{}, fmt.Errorf("billing page %s contained no readable text", pageURL)
	}
	title, _ := page.Title()
	return PageData{URL: page.URL(), Title: title, Text: text}, nil
}

// Close stops the Playwright driver process.
func (p *Playwright) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closed = true
	if p.pw == nil {
		return nil
	}
	err := p.pw.Stop()
	p.pw = nil
	return err
}

func launchOptions(options Options, headless bool) playwright.BrowserTypeLaunchOptions {
	launch := playwright.BrowserTypeLaunchOptions{Headless: playwright.Bool(headless)}
	if options.ExecutablePath != "" {
		launch.ExecutablePath = playwright.String(options.ExecutablePath)
	}
	return launch
}

func closeOnCancel(ctx context.Context, browser playwright.Browser) func() {
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = browser.Close()
		case <-done:
		}
	}()
	return func() { close(done) }
}
