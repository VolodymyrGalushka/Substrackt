package app

import (
	"context"
	"fmt"
	"time"

	"substrackt/internal/domain"
	"substrackt/internal/llm"
	"substrackt/internal/store"
)

// SettingsService exposes application preferences and LLM diagnostics.
type SettingsService struct {
	store *store.Store
}

// NewSettingsService wires the service to the store.
func NewSettingsService(st *store.Store) *SettingsService {
	return &SettingsService{store: st}
}

// Get returns the current settings, including defaults.
func (s *SettingsService) Get() (Settings, error) {
	settings, err := s.store.GetSettings(context.Background())
	if err != nil {
		return Settings{}, err
	}
	settings.Normalize()
	return settingsDTO(settings), nil
}

// Update validates and saves settings. The endpoint must remain local.
func (s *SettingsService) Update(in Settings) (Settings, error) {
	settings := settingsDomain(in)
	settings.Normalize()

	if settings.LLMBaseURL == "" {
		settings.LLMModel = ""
	} else {
		normalized, err := llm.ValidateBaseURL(settings.LLMBaseURL)
		if err != nil {
			return Settings{}, err
		}
		settings.LLMBaseURL = normalized
	}
	if err := settings.Validate(); err != nil {
		return Settings{}, fmt.Errorf("settings: %w", err)
	}

	if err := s.store.SaveSettings(context.Background(), settings); err != nil {
		return Settings{}, err
	}
	emit("settings:changed", settingsDTO(settings))
	return settingsDTO(settings), nil
}

// Reset restores defaults.
func (s *SettingsService) Reset() (Settings, error) {
	settings := domain.DefaultSettings()
	if err := s.store.SaveSettings(context.Background(), settings); err != nil {
		return Settings{}, err
	}
	emit("settings:changed", settingsDTO(settings))
	return settingsDTO(settings), nil
}

// TestLLM probes the configured endpoint. Connection failures are returned in
// the result rather than as an error so the UI can show them inline.
func (s *SettingsService) TestLLM() (LLMTestResult, error) {
	ctx := context.Background()
	settings, err := s.store.GetSettings(ctx)
	if err != nil {
		return LLMTestResult{}, err
	}
	if !settings.LLMEnabled() {
		return LLMTestResult{
			Models: []string{},
			Error:  "Configure a local endpoint and model first.",
		}, nil
	}

	timeout := time.Duration(settings.LLMTimeoutSeconds) * time.Second
	if timeout <= 0 || timeout > 20*time.Second {
		timeout = 20 * time.Second
	}
	client, err := llm.New(settings.LLMBaseURL, settings.LLMModel, timeout)
	if err != nil {
		return LLMTestResult{Models: []string{}, Error: err.Error()}, nil
	}

	models, err := client.Models(ctx)
	if err != nil {
		return LLMTestResult{Models: []string{}, Error: err.Error()}, nil
	}
	if models == nil {
		models = []string{}
	}
	return LLMTestResult{OK: true, Models: models}, nil
}

// Usage returns recent local model calls.
func (s *SettingsService) Usage(limit int) ([]LLMUsage, error) {
	rows, err := s.store.ListLLMUsage(context.Background(), limit)
	if err != nil {
		return nil, err
	}
	out := make([]LLMUsage, 0, len(rows))
	for _, row := range rows {
		out = append(out, llmUsageDTO(row))
	}
	return out, nil
}
