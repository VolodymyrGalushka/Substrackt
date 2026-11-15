package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"substrackt/internal/domain"
)

const settingsKey = "app"

// GetSettings returns the saved settings, or defaults when none were saved yet.
func (s *Store) GetSettings(ctx context.Context) (domain.Settings, error) {
	settings := domain.DefaultSettings()
	var raw string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, settingsKey).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return settings, nil
	}
	if err != nil {
		return domain.Settings{}, fmt.Errorf("get settings: %w", err)
	}
	if err := json.Unmarshal([]byte(raw), &settings); err != nil {
		return domain.Settings{}, fmt.Errorf("decode settings: %w", err)
	}
	return settings, nil
}

// SaveSettings replaces the application settings.
func (s *Store) SaveSettings(ctx context.Context, settings domain.Settings) error {
	raw, err := json.Marshal(settings)
	if err != nil {
		return fmt.Errorf("encode settings: %w", err)
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO settings (key, value, updated_at) VALUES (?, ?, ?)
		 ON CONFLICT (key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		settingsKey, string(raw), formatTime(time.Now()),
	)
	if err != nil {
		return fmt.Errorf("save settings: %w", err)
	}
	return nil
}
