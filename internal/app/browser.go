package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"substrackt/internal/browser"
	"substrackt/internal/domain"
	"substrackt/internal/ingest"
	"substrackt/internal/secrets"
	"substrackt/internal/store"
)

// BrowserJobKind is the queue kind for one browser page sync.
const BrowserJobKind = "browser.sync"

type browserPayload struct {
	SourceID string `json:"sourceId"`
}

// BrowserManager owns the Playwright engine, interactive sign-ins and session
// secrets. It is shared by the bound SourcesService and the job handler.
type BrowserManager struct {
	store   *store.Store
	secrets secrets.Store
	engine  browser.Engine

	mu       sync.Mutex
	logins   map[string]context.CancelFunc
	statuses map[string]BrowserLoginStatus
	notify   func(BrowserLoginStatus)
	changed  func(Source)
}

func NewBrowserManager(st *store.Store, secretStore secrets.Store, engine browser.Engine) *BrowserManager {
	return &BrowserManager{
		store:    st,
		secrets:  secretStore,
		engine:   engine,
		logins:   make(map[string]context.CancelFunc),
		statuses: make(map[string]BrowserLoginStatus),
	}
}

func (m *BrowserManager) SetLoginNotifier(fn func(BrowserLoginStatus)) { m.notify = fn }
func (m *BrowserManager) SetSourceNotifier(fn func(Source))            { m.changed = fn }

func (m *BrowserManager) emit(status BrowserLoginStatus) {
	m.mu.Lock()
	m.statuses[status.SourceID] = status
	m.mu.Unlock()
	if m.notify != nil {
		m.notify(status)
	}
}

// Close cancels in-flight sign-ins and stops the Playwright driver.
func (m *BrowserManager) Close() {
	m.mu.Lock()
	for _, cancel := range m.logins {
		cancel()
	}
	m.mu.Unlock()
	_ = m.engine.Close()
}

// LoginStatus returns the last sign-in state for a source.
func (m *BrowserManager) LoginStatus(sourceID string) BrowserLoginStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	if status, ok := m.statuses[sourceID]; ok {
		return status
	}
	return BrowserLoginStatus{SourceID: sourceID}
}

func (m *BrowserManager) emitSource(src domain.Source) {
	if m.changed != nil {
		m.changed(sourceDTO(src))
	}
}

// Runtime reports which browser runtime is selected and whether it is ready.
func (m *BrowserManager) Runtime() BrowserRuntime {
	settings, err := m.store.GetSettings(context.Background())
	if err != nil {
		return BrowserRuntime{Error: "load settings: " + err.Error()}
	}
	return m.runtimeStatus(settings.BrowserRuntime)
}

func (m *BrowserManager) runtimeStatus(selected string) BrowserRuntime {
	options := browser.AvailableRuntimes()
	status := BrowserRuntime{
		Selected: selected,
		Options:  browserRuntimeOptions(options),
	}
	resolved, err := browser.ResolveRuntime(selected)
	if err != nil {
		status.Error = err.Error()
		return status
	}
	status.Resolved = resolved.ID
	status.ResolvedName = resolved.Name
	status.Ready, status.Error = m.engine.Ready(browser.OptionsFor(resolved))
	return status
}

func browserRuntimeOptions(runtimes []browser.Runtime) []BrowserRuntimeOption {
	out := make([]BrowserRuntimeOption, 0, len(runtimes))
	for _, rt := range runtimes {
		out = append(out, BrowserRuntimeOption{
			ID:        rt.ID,
			Name:      rt.Name,
			Installed: rt.Installed,
		})
	}
	return out
}

// resolvedRuntime reads the current preference and resolves it to a browser.
func (m *BrowserManager) resolvedRuntime(ctx context.Context) (browser.Runtime, error) {
	settings, err := m.store.GetSettings(ctx)
	if err != nil {
		return browser.Runtime{}, fmt.Errorf("load browser settings: %w", err)
	}
	return browser.ResolveRuntime(settings.BrowserRuntime)
}

// Install downloads the Playwright driver, plus bundled Chromium when needed.
func (m *BrowserManager) Install(ctx context.Context) error {
	resolved, err := m.resolvedRuntime(ctx)
	if err != nil {
		return err
	}
	return m.engine.Install(ctx, browser.OptionsFor(resolved), nil)
}

// BeginLogin starts an interactive sign-in and returns immediately. Progress is
// reported through the browser:login event.
func (m *BrowserManager) BeginLogin(src domain.Source) error {
	if src.Kind != domain.SourceBrowser {
		return fmt.Errorf("source %q is not a browser source", src.Name)
	}
	connector, err := browser.ConnectorFor(src.Browser.Connector)
	if err != nil {
		return err
	}
	resolved, err := m.resolvedRuntime(context.Background())
	if err != nil {
		return err
	}
	options := browser.OptionsFor(resolved)

	m.mu.Lock()
	if _, running := m.logins[src.ID]; running {
		m.mu.Unlock()
		return fmt.Errorf("a sign-in is already running for %q", src.Name)
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.logins[src.ID] = cancel
	m.mu.Unlock()

	go func() {
		defer func() {
			m.mu.Lock()
			delete(m.logins, src.ID)
			m.mu.Unlock()
			cancel()
		}()

		status := BrowserLoginStatus{SourceID: src.ID, State: "starting"}
		m.emit(status)

		if ready, _ := m.engine.Ready(options); !ready {
			status.State = "installing"
			m.emit(status)
			if err := m.engine.Install(ctx, options, nil); err != nil {
				m.failLogin(status, err)
				return
			}
			if ctx.Err() != nil {
				m.cancelLogin(status)
				return
			}
		}

		status.State = "waiting"
		m.emit(status)

		state, err := m.engine.Login(ctx, connector, options, 10*time.Minute)
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, context.Canceled) {
				m.cancelLogin(status)
				return
			}
			m.failLogin(status, err)
			return
		}

		ref := browserSessionRef(src.ID)
		if err := m.secrets.Set(ref, state); err != nil {
			m.failLogin(status, fmt.Errorf("save browser session: %w", err))
			return
		}
		src.SecretRef = ref
		src.LastError = ""
		writeCtx, writeCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer writeCancel()
		if err := m.store.UpdateSource(writeCtx, &src); err != nil {
			_ = m.secrets.Delete(ref)
			m.failLogin(status, fmt.Errorf("save sign-in state: %w", err))
			return
		}
		m.emitSource(src)
		status.State = "saved"
		m.emit(status)
	}()
	return nil
}

// CancelLogin stops an in-flight sign-in.
func (m *BrowserManager) CancelLogin(sourceID string) error {
	m.mu.Lock()
	cancel, ok := m.logins[sourceID]
	m.mu.Unlock()
	if !ok {
		return fmt.Errorf("no sign-in is running for this source")
	}
	cancel()
	return nil
}

func (m *BrowserManager) failLogin(status BrowserLoginStatus, err error) {
	status.State = "failed"
	status.Error = err.Error()
	m.emit(status)
}

func (m *BrowserManager) cancelLogin(status BrowserLoginStatus) {
	status.State = "cancelled"
	m.emit(status)
}

// Fetch loads a browser source's billing page with its saved session. An
// expired session is removed so the UI can prompt for a new sign-in.
func (m *BrowserManager) Fetch(ctx context.Context, src domain.Source, progress func(int, string)) (browser.PageData, error) {
	if src.Kind != domain.SourceBrowser {
		return browser.PageData{}, fmt.Errorf("source %q is not a browser source", src.Name)
	}
	connector, err := browser.ConnectorFor(src.Browser.Connector)
	if err != nil {
		return browser.PageData{}, err
	}
	pageURL, err := browser.BillingPage(connector, src.Browser.BillingURL)
	if err != nil {
		return browser.PageData{}, err
	}
	resolved, err := m.resolvedRuntime(ctx)
	if err != nil {
		return browser.PageData{}, err
	}
	if src.SecretRef == "" {
		return browser.PageData{}, fmt.Errorf("source %q is not signed in; use Sign in first", src.Name)
	}
	state, err := m.secrets.Get(src.SecretRef)
	if err != nil {
		if errors.Is(err, secrets.ErrNotFound) {
			m.clearSession(ctx, src)
			return browser.PageData{}, fmt.Errorf("source %q has no saved session; sign in again", src.Name)
		}
		return browser.PageData{}, fmt.Errorf("load browser session: %w", err)
	}
	if progress != nil {
		progress(20, "Opening "+pageURL)
	}
	timeout := time.Duration(src.Browser.TimeoutSeconds) * time.Second
	if timeout < 10*time.Second {
		timeout = 30 * time.Second
	}
	page, err := m.engine.Fetch(ctx, connector, browser.OptionsFor(resolved), state, pageURL, timeout)
	if errors.Is(err, browser.ErrSessionExpired) {
		m.clearSession(ctx, src)
		return browser.PageData{}, fmt.Errorf("session for %q expired; sign in again", src.Name)
	}
	return page, err
}

func (m *BrowserManager) clearSession(ctx context.Context, src domain.Source) {
	if src.SecretRef != "" {
		_ = m.secrets.Delete(src.SecretRef)
	}
	src.SecretRef = ""
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := m.store.UpdateSource(writeCtx, &src); err == nil {
		m.emitSource(src)
	}
}

// TestBrowser loads the configured page with the current session.
func (m *BrowserManager) TestBrowser(ctx context.Context, src domain.Source) (SourceTestResult, error) {
	page, err := m.Fetch(ctx, src, nil)
	if err != nil {
		return SourceTestResult{Error: err.Error()}, nil
	}
	message := fmt.Sprintf("Signed in and read %q (%d characters)", page.Title, len(page.Text))
	return SourceTestResult{OK: true, Message: message}, nil
}

// BrowserHandler executes queued browser syncs. It is not a bound service.
type BrowserHandler struct {
	store     *store.Store
	ingestion *IngestionHandler
	browser   *BrowserManager
	now       func() time.Time
}

// NewBrowserHandler wires the browser channel to the shared ingestion pipeline.
func NewBrowserHandler(st *store.Store, ingestion *IngestionHandler, manager *BrowserManager) *BrowserHandler {
	return &BrowserHandler{store: st, ingestion: ingestion, browser: manager, now: time.Now}
}

// RunJob is the jobs.Handler for browser syncs.
func (h *BrowserHandler) RunJob(ctx context.Context, job domain.Job, progress func(int, string)) (string, error) {
	var payload browserPayload
	if err := json.Unmarshal([]byte(job.Payload), &payload); err != nil {
		return "", fmt.Errorf("decode browser payload: %w", err)
	}
	payload.SourceID = strings.TrimSpace(payload.SourceID)
	if payload.SourceID == "" {
		return "", fmt.Errorf("browser sync payload is missing a source")
	}
	message, err := h.sync(ctx, job.ID, payload.SourceID, progress)
	if err != nil {
		if ctx.Err() == nil {
			h.markError(ctx, payload.SourceID, err)
		}
		return "", err
	}
	return message, nil
}

func (h *BrowserHandler) sync(ctx context.Context, jobID, sourceID string, progress func(int, string)) (string, error) {
	browserReport(progress, 2, "Loading source")
	src, err := h.store.GetSource(ctx, sourceID)
	if err != nil {
		return "", fmt.Errorf("load source: %w", err)
	}
	if src.Kind != domain.SourceBrowser {
		return "", fmt.Errorf("source %q is not a browser source", src.Name)
	}
	if !src.Enabled {
		return "", fmt.Errorf("source %q is disabled", src.Name)
	}
	src.Browser.Normalize()
	if err := src.Browser.Validate(); err != nil {
		return "", fmt.Errorf("source %q is misconfigured: %w", src.Name, err)
	}
	connector, err := browser.ConnectorFor(src.Browser.Connector)
	if err != nil {
		return "", fmt.Errorf("source %q: %w", src.Name, err)
	}

	page, err := h.browser.Fetch(ctx, src, progress)
	if err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}

	pipeline, err := h.ingestion.pipeline(ctx, jobID)
	if err != nil {
		return "", err
	}
	browserReport(progress, 50, "Reading billing page")
	result, err := pipeline.Run(ctx, ingest.RawItem{
		Channel:    "browser",
		ExternalID: "browser:" + connector.ID + ":" + page.URL,
		Title:      page.Title,
		Content:    page.Text,
		VendorHint: connector.Vendor,
		OccurredAt: h.now(),
	}, scaledProgress(progress, 50, 95))
	if err != nil {
		return "", fmt.Errorf("ingest billing page: %w", err)
	}
	if result.ReviewID != "" {
		h.ingestion.notifyNewReview(ctx, result.ReviewID)
	}

	now := h.now()
	if err := h.store.MarkSourceSynced(ctx, src.ID, now); err != nil {
		return "", err
	}
	if h.browser != nil {
		src.LastSyncAt = now
		src.LastError = ""
		h.browser.emitSource(src)
	}

	summary := browserSummary(result)
	browserReport(progress, 100, summary)
	return summary, nil
}

func browserReport(progress func(int, string), percent int, message string) {
	if progress != nil {
		progress(percent, message)
	}
}

func browserSummary(result ingest.Result) string {
	switch result.Action {
	case ingest.ActionCreated:
		return "Billing page read; " + result.Message
	case ingest.ActionMatched:
		return "Billing page read; " + result.Message
	case ingest.ActionReview:
		return "Billing page read; " + result.Message
	case ingest.ActionDuplicate:
		return "Billing page unchanged"
	case ingest.ActionIgnored:
		return "Billing page read; not recognised as a subscription"
	default:
		if result.Message != "" {
			return result.Message
		}
		return "Billing page read"
	}
}

func (h *BrowserHandler) markError(ctx context.Context, sourceID string, cause error) {
	message := strings.TrimSpace(cause.Error())
	if len(message) > 500 {
		message = message[:500]
	}
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := h.store.MarkSourceError(writeCtx, sourceID, message); err != nil {
		return
	}
	if src, err := h.store.GetSource(writeCtx, sourceID); err == nil && h.browser != nil {
		h.browser.emitSource(src)
	}
}

func browserSessionRef(sourceID string) string { return "source:" + sourceID + ":session" }

// browserSourceFromInput validates a browser source against the registry.
func browserSourceFromInput(base domain.Source, in SourceInput) (domain.Source, error) {
	src := base
	src.Kind = domain.SourceBrowser
	src.Name = in.Name
	src.Enabled = in.Enabled
	src.ScheduleMinutes = in.ScheduleMinutes
	src.Browser = sourceBrowserConfig(in.Config)
	src.Normalize()
	if err := src.Validate(); err != nil {
		return domain.Source{}, err
	}
	connector, err := browser.ConnectorFor(src.Browser.Connector)
	if err != nil {
		return domain.Source{}, err
	}
	if _, err := browser.BillingPage(connector, src.Browser.BillingURL); err != nil {
		return domain.Source{}, err
	}
	return src, nil
}
