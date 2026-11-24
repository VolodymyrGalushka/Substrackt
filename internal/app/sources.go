package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"substrackt/internal/api"
	"substrackt/internal/browser"
	"substrackt/internal/domain"
	"substrackt/internal/jobs"
	"substrackt/internal/mail"
	"substrackt/internal/secrets"
	"substrackt/internal/store"
)

const maxSecretBytes = 4096

// SourcesService exposes configured ingestion channels to the frontend. It owns
// credential handling: passwords are written to the secret store and never
// returned by any method.
type SourcesService struct {
	store    *store.Store
	secrets  secrets.Store
	manager  *jobs.Manager
	browser  *BrowserManager
	dial     emailDialer
	apiFetch apiFetcher
}

// NewSourcesService wires the service to the store, secret store and queue.
func NewSourcesService(st *store.Store, secretStore secrets.Store, manager *jobs.Manager, BrowserManager *BrowserManager) *SourcesService {
	return &SourcesService{
		store:   st,
		secrets: secretStore,
		manager: manager,
		browser: BrowserManager,
		dial:    dialIMAP,
		apiFetch: func(ctx context.Context, connector api.Connector, token string, timeout time.Duration, max int) ([]api.Subscription, error) {
			return api.NewClient(timeout).Subscriptions(ctx, connector, token, max)
		},
	}
}

// List returns every configured source, enabled first.
func (s *SourcesService) List() ([]Source, error) {
	rows, err := s.store.ListSources(context.Background())
	if err != nil {
		return nil, err
	}
	out := make([]Source, 0, len(rows))
	for _, row := range rows {
		out = append(out, sourceDTO(row))
	}
	return out, nil
}

// Get returns one source.
func (s *SourcesService) Get(id string) (Source, error) {
	src, err := s.store.GetSource(context.Background(), id)
	if err != nil {
		return Source{}, err
	}
	return sourceDTO(src), nil
}

// RevealPassword returns the stored credential for a source. It is only used
// by the explicit "show saved password" action in the source editor; the value
// is never included in DTOs, events or logs.
func (s *SourcesService) RevealPassword(id string) (string, error) {
	src, err := s.store.GetSource(context.Background(), id)
	if err != nil {
		return "", err
	}
	if src.SecretRef == "" {
		return "", fmt.Errorf("no credential is saved for this source")
	}
	if src.Kind != domain.SourceEmail && src.Kind != domain.SourceAPI {
		return "", fmt.Errorf("this source does not use a credential")
	}
	value, err := s.secrets.Get(src.SecretRef)
	if err != nil {
		return "", fmt.Errorf("load password: %w", err)
	}
	return value, nil
}

// Create saves a source and its credential. Email and API-key sources require
// a credential; browser and OAuth sources start signed out.
func (s *SourcesService) Create(in SourceInput) (Source, error) {
	ctx := context.Background()
	src, err := sourceFromInput(domain.Source{}, in)
	if err != nil {
		return Source{}, err
	}
	if sourceNeedsCredential(src) && in.Password == "" {
		return Source{}, fmt.Errorf("%s is required", sourceCredentialLabel(src))
	}
	if len(in.Password) > maxSecretBytes {
		return Source{}, fmt.Errorf("credential is too long (limit %d bytes)", maxSecretBytes)
	}

	src.ID = domain.NewID()
	if sourceNeedsCredential(src) {
		src.SecretRef = sourceSecretRef(src.ID)
		if err := s.secrets.Set(src.SecretRef, in.Password); err != nil {
			return Source{}, fmt.Errorf("store credential: %w", err)
		}
	}
	if err := s.store.CreateSource(ctx, &src); err != nil {
		// Do not leave an orphaned credential behind a failed insert.
		if src.SecretRef != "" {
			_ = s.secrets.Delete(src.SecretRef)
		}
		return Source{}, err
	}
	dto := sourceDTO(src)
	emit("source:changed", dto)
	return dto, nil
}

// Update replaces the editable fields of a source. An empty password keeps the
// stored credential.
func (s *SourcesService) Update(id string, in SourceInput) (Source, error) {
	ctx := context.Background()
	current, err := s.store.GetSource(ctx, id)
	if err != nil {
		return Source{}, err
	}

	updated, err := sourceFromInput(current, in)
	if err != nil {
		return Source{}, err
	}
	if len(in.Password) > maxSecretBytes {
		return Source{}, fmt.Errorf("password is too long (limit %d bytes)", maxSecretBytes)
	}

	if updated.Kind == domain.SourceBrowser {
		// A session is scoped to the vendor. Changing connectors must not send
		// the old cookies to a different site.
		if current.Kind != domain.SourceBrowser || current.Browser.Connector != updated.Browser.Connector {
			if current.SecretRef != "" {
				_ = s.secrets.Delete(current.SecretRef)
			}
			updated.SecretRef = ""
		} else {
			updated.SecretRef = current.SecretRef
		}
		if err := s.store.UpdateSource(ctx, &updated); err != nil {
			return Source{}, err
		}
		dto := sourceDTO(updated)
		emit("source:changed", dto)
		return dto, nil
	}

	if updated.Kind == domain.SourceAPI {
		// Tokens are scoped to a connector and auth mode. Do not delete the old
		// credential until the source update has succeeded, so a failed save
		// cannot leave the source locked out.
		scopedChange := current.Kind != domain.SourceAPI ||
			current.API.Connector != updated.API.Connector ||
			current.API.Auth != updated.API.Auth
		newSecret := in.Password != ""
		if scopedChange && !newSecret && updated.API.Auth == domain.APIAuthKey && updated.Enabled {
			return Source{}, fmt.Errorf("API key is required after changing the connector")
		}

		oldRef := current.SecretRef
		oldValue, oldErr := "", error(nil)
		if oldRef != "" {
			oldValue, oldErr = s.secrets.Get(oldRef)
		}
		if newSecret {
			if updated.SecretRef == "" {
				updated.SecretRef = sourceSecretRef(updated.ID)
			}
			if err := s.secrets.Set(updated.SecretRef, in.Password); err != nil {
				return Source{}, fmt.Errorf("store credential: %w", err)
			}
		} else if scopedChange {
			updated.SecretRef = ""
		} else {
			updated.SecretRef = current.SecretRef
		}
		if updated.SecretRef == "" && updated.API.Auth == domain.APIAuthKey && updated.Enabled {
			return Source{}, fmt.Errorf("API key is required for an enabled source")
		}

		if err := s.store.UpdateSource(ctx, &updated); err != nil {
			if newSecret {
				if oldErr == nil && oldRef != "" {
					_ = s.secrets.Set(oldRef, oldValue)
				} else {
					_ = s.secrets.Delete(updated.SecretRef)
				}
			}
			return Source{}, err
		}
		if scopedChange && !newSecret && oldRef != "" {
			_ = s.secrets.Delete(oldRef)
		}
		dto := sourceDTO(updated)
		emit("source:changed", dto)
		return dto, nil
	}

	newSecret := in.Password != ""
	if newSecret {
		if updated.SecretRef == "" {
			updated.SecretRef = sourceSecretRef(updated.ID)
		}
		oldValue, oldErr := s.secrets.Get(updated.SecretRef)
		if err := s.secrets.Set(updated.SecretRef, in.Password); err != nil {
			return Source{}, fmt.Errorf("store password: %w", err)
		}
		if err := s.store.UpdateSource(ctx, &updated); err != nil {
			// Keep the previous credential usable when the update failed.
			if oldErr == nil {
				_ = s.secrets.Set(updated.SecretRef, oldValue)
			} else {
				_ = s.secrets.Delete(updated.SecretRef)
			}
			return Source{}, err
		}
	} else {
		if updated.SecretRef == "" && updated.Enabled {
			return Source{}, fmt.Errorf("password is required for an enabled source")
		}
		if err := s.store.UpdateSource(ctx, &updated); err != nil {
			return Source{}, err
		}
	}

	dto := sourceDTO(updated)
	emit("source:changed", dto)
	return dto, nil
}

// Delete removes a source, its mailbox cursors and its stored password.
func (s *SourcesService) Delete(id string) error {
	ctx := context.Background()
	src, err := s.store.GetSource(ctx, id)
	if err != nil {
		return err
	}
	if err := s.store.DeleteSource(ctx, id); err != nil {
		return err
	}
	if src.SecretRef != "" {
		// The source is already gone; an unavailable keyring must not make the
		// deletion look failed. The file backend removes its copy regardless.
		_ = s.secrets.Delete(src.SecretRef)
	}
	emit("source:changed", map[string]string{"id": id, "deleted": "true"})
	return nil
}

// SyncNow queues an immediate sync for a source.
func (s *SourcesService) SyncNow(id string) (Job, error) {
	src, err := s.store.GetSource(context.Background(), id)
	if err != nil {
		return Job{}, err
	}
	if !src.Enabled {
		return Job{}, fmt.Errorf("source %q is disabled", src.Name)
	}
	switch src.Kind {
	case domain.SourceEmail:
		if src.SecretRef == "" {
			return Job{}, fmt.Errorf("source %q has no stored password", src.Name)
		}
		job, err := s.manager.Enqueue(context.Background(), EmailJobKind, emailPayload{SourceID: id})
		if err != nil {
			return Job{}, err
		}
		return jobDTO(job), nil
	case domain.SourceBrowser:
		if src.SecretRef == "" {
			return Job{}, fmt.Errorf("source %q is not signed in; use Sign in first", src.Name)
		}
		job, err := s.manager.Enqueue(context.Background(), BrowserJobKind, browserPayload{SourceID: id})
		if err != nil {
			return Job{}, err
		}
		return jobDTO(job), nil
	case domain.SourceAPI:
		if src.SecretRef == "" {
			return Job{}, fmt.Errorf("source %q has no API credential; edit it to add one", src.Name)
		}
		job, err := s.manager.Enqueue(context.Background(), APIJobKind, apiPayload{SourceID: id})
		if err != nil {
			return Job{}, err
		}
		return jobDTO(job), nil
	default:
		return Job{}, fmt.Errorf("source %q has an unsupported kind", src.Name)
	}
}

// Test connects to the configured mailbox without saving anything. When id
// refers to a stored source, an empty password reuses its credential.
func (s *SourcesService) Test(id string, in SourceInput) (SourceTestResult, error) {
	kind := domain.SourceKind(strings.ToLower(strings.TrimSpace(in.Kind)))
	if kind == "" && id != "" {
		if stored, err := s.store.GetSource(context.Background(), id); err == nil {
			kind = stored.Kind
		}
	}
	if kind == domain.SourceBrowser {
		return s.testBrowser(id, in)
	}
	if kind == domain.SourceAPI {
		return s.testAPI(id, in)
	}

	cfg, password, err := s.testConfig(id, in)
	if err != nil {
		return SourceTestResult{Error: err.Error()}, nil
	}
	if password == "" {
		return SourceTestResult{Error: "enter the mailbox password first"}, nil
	}

	client, err := s.dial(mail.Config{
		Address:  cfg.Address,
		Username: cfg.Username,
		Password: password,
		Security: cfg.Security,
		Auth:     cfg.Auth,
		TLSHost:  cfg.TLSHost,
	})
	if err != nil {
		return SourceTestResult{Error: err.Error()}, nil
	}
	defer func() { _ = client.Close() }()

	done := make(chan struct{})
	go func() {
		select {
		case <-time.After(25 * time.Second):
			_ = client.Abort()
		case <-done:
		}
	}()
	defer close(done)

	info, err := client.SelectInfo(cfg.Mailbox)
	if err != nil {
		return SourceTestResult{Error: err.Error()}, nil
	}
	return SourceTestResult{OK: true, Mailbox: cfg.Mailbox, Messages: int(info.NumMessages)}, nil
}

func (s *SourcesService) testConfig(id string, in SourceInput) (domain.EmailSourceConfig, string, error) {
	password := in.Password
	var stored domain.Source
	if id != "" {
		src, err := s.store.GetSource(context.Background(), id)
		if err != nil {
			return domain.EmailSourceConfig{}, "", err
		}
		stored = src
		if password == "" && src.SecretRef != "" {
			value, err := s.secrets.Get(src.SecretRef)
			if err != nil && !errors.Is(err, secrets.ErrNotFound) {
				return domain.EmailSourceConfig{}, "", fmt.Errorf("load password: %w", err)
			}
			password = value
		}
	}

	src, err := sourceFromInput(stored, in)
	if err != nil {
		return domain.EmailSourceConfig{}, "", err
	}
	return src.Email, password, nil
}

func sourceFromInput(base domain.Source, in SourceInput) (domain.Source, error) {
	kind := domain.SourceKind(strings.ToLower(strings.TrimSpace(in.Kind)))
	if kind == "" {
		kind = domain.SourceEmail
	}
	if kind == domain.SourceBrowser {
		return browserSourceFromInput(base, in)
	}
	if kind == domain.SourceAPI {
		return apiSourceFromInput(base, in)
	}

	src := base
	src.Kind = kind
	src.Name = in.Name
	src.Enabled = in.Enabled
	src.ScheduleMinutes = in.ScheduleMinutes
	src.Email = sourceEmailConfig(in.Config)
	src.Normalize()
	if err := src.Validate(); err != nil {
		return domain.Source{}, err
	}
	return src, nil
}

func (s *SourcesService) testBrowser(id string, in SourceInput) (SourceTestResult, error) {
	if s.browser == nil {
		return SourceTestResult{Error: "browser support is not available"}, nil
	}
	var base domain.Source
	if id != "" {
		stored, err := s.store.GetSource(context.Background(), id)
		if err != nil {
			return SourceTestResult{Error: err.Error()}, nil
		}
		base = stored
	}
	src, err := browserSourceFromInput(base, in)
	if err != nil {
		return SourceTestResult{Error: err.Error()}, nil
	}
	// Testing an existing source keeps its saved session; a brand-new source
	// has none yet and must be signed in first.
	src.SecretRef = base.SecretRef
	return s.browser.TestBrowser(context.Background(), src)
}

func sourceSecretRef(id string) string { return "source:" + id }

// BrowserRuntime reports whether Playwright and Chromium are installed.
func (s *SourcesService) BrowserRuntime() BrowserRuntime {
	if s.browser == nil {
		return BrowserRuntime{Error: "browser support is not available"}
	}
	return s.browser.Runtime()
}

// InstallBrowser downloads the Playwright driver and Chromium on first use.
func (s *SourcesService) InstallBrowser() (BrowserRuntime, error) {
	if s.browser == nil {
		return BrowserRuntime{Error: "browser support is not available"}, fmt.Errorf("browser support is not available")
	}
	if err := s.browser.Install(context.Background()); err != nil {
		return s.browser.Runtime(), err
	}
	runtime := s.browser.Runtime()
	if !runtime.Ready {
		return runtime, fmt.Errorf("browser runtime is not ready: %s", runtime.Error)
	}
	return runtime, nil
}

// BrowserConnectors lists the vendor presets available to browser sources.
func (s *SourcesService) BrowserConnectors() []BrowserConnector {
	connectors := browser.Connectors()
	out := make([]BrowserConnector, 0, len(connectors))
	for _, connector := range connectors {
		out = append(out, BrowserConnector{
			ID:         connector.ID,
			Name:       connector.Name,
			LoginURL:   connector.LoginURL,
			BillingURL: connector.BillingURL,
		})
	}
	return out
}

// BeginSignIn opens a browser window for an interactive login. Progress arrives
// through the browser:login event; use CancelSignIn to abort.
func (s *SourcesService) BeginSignIn(id string) error {
	if s.browser == nil {
		return fmt.Errorf("browser support is not available")
	}
	src, err := s.store.GetSource(context.Background(), id)
	if err != nil {
		return err
	}
	return s.browser.BeginLogin(src)
}

// CancelSignIn aborts an in-flight interactive login.
func (s *SourcesService) CancelSignIn(id string) error {
	if s.browser == nil {
		return fmt.Errorf("browser support is not available")
	}
	return s.browser.CancelLogin(id)
}

// SignInStatus returns the latest sign-in state for a browser source.
func (s *SourcesService) SignInStatus(id string) BrowserLoginStatus {
	if s.browser == nil {
		return BrowserLoginStatus{SourceID: id}
	}
	return s.browser.LoginStatus(id)
}
