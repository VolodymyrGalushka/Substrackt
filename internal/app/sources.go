package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

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
	store   *store.Store
	secrets secrets.Store
	manager *jobs.Manager
	dial    emailDialer
}

// NewSourcesService wires the service to the store, secret store and queue.
func NewSourcesService(st *store.Store, secretStore secrets.Store, manager *jobs.Manager) *SourcesService {
	return &SourcesService{
		store:   st,
		secrets: secretStore,
		manager: manager,
		dial:    dialIMAP,
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
		return "", fmt.Errorf("no password is saved for this source")
	}
	value, err := s.secrets.Get(src.SecretRef)
	if err != nil {
		return "", fmt.Errorf("load password: %w", err)
	}
	return value, nil
}

// Create saves a source and its password.
func (s *SourcesService) Create(in SourceInput) (Source, error) {
	ctx := context.Background()
	src, err := sourceFromInput(domain.Source{}, in)
	if err != nil {
		return Source{}, err
	}
	if in.Password == "" {
		return Source{}, fmt.Errorf("password is required")
	}
	if len(in.Password) > maxSecretBytes {
		return Source{}, fmt.Errorf("password is too long (limit %d bytes)", maxSecretBytes)
	}

	src.ID = domain.NewID()
	src.SecretRef = sourceSecretRef(src.ID)
	if err := s.secrets.Set(src.SecretRef, in.Password); err != nil {
		return Source{}, fmt.Errorf("store password: %w", err)
	}
	if err := s.store.CreateSource(ctx, &src); err != nil {
		// Do not leave an orphaned credential behind a failed insert.
		_ = s.secrets.Delete(src.SecretRef)
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
	if src.Kind != domain.SourceEmail {
		return Job{}, fmt.Errorf("source %q is not an email source", src.Name)
	}
	if !src.Enabled {
		return Job{}, fmt.Errorf("source %q is disabled", src.Name)
	}
	if src.SecretRef == "" {
		return Job{}, fmt.Errorf("source %q has no stored password", src.Name)
	}
	job, err := s.manager.Enqueue(context.Background(), EmailJobKind, emailPayload{SourceID: id})
	if err != nil {
		return Job{}, err
	}
	return jobDTO(job), nil
}

// Test connects to the configured mailbox without saving anything. When id
// refers to a stored source, an empty password reuses its credential.
func (s *SourcesService) Test(id string, in SourceInput) (SourceTestResult, error) {
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
	return src.Config, password, nil
}

func sourceFromInput(base domain.Source, in SourceInput) (domain.Source, error) {
	kind := domain.SourceKind(strings.ToLower(strings.TrimSpace(in.Kind)))
	if kind == "" {
		kind = domain.SourceEmail
	}

	src := base
	src.Kind = kind
	src.Name = in.Name
	src.Enabled = in.Enabled
	src.ScheduleMinutes = in.ScheduleMinutes
	if kind == domain.SourceEmail {
		src.Config = sourceConfigDomain(in.Config)
	}
	src.Normalize()
	if err := src.Validate(); err != nil {
		return domain.Source{}, err
	}
	return src, nil
}

func sourceSecretRef(id string) string { return "source:" + id }
