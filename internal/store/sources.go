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

const sourceColumns = `id, kind, name, config, secret_ref, enabled, schedule_minutes,
	last_sync_at, last_error, created_at, updated_at`

// CreateSource inserts a configured source, assigning an ID and timestamps.
func (s *Store) CreateSource(ctx context.Context, src *domain.Source) error {
	if src.ID == "" {
		src.ID = domain.NewID()
	}
	now := time.Now()
	if src.CreatedAt.IsZero() {
		src.CreatedAt = now
	}
	src.UpdatedAt = now
	config, err := encodeSourceConfig(*src)
	if err != nil {
		return err
	}

	_, err = s.db.ExecContext(ctx,
		`INSERT INTO sources (`+sourceColumns+`) VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		src.ID, string(src.Kind), src.Name, string(config), src.SecretRef,
		boolInt(src.Enabled), src.ScheduleMinutes, formatTime(src.LastSyncAt),
		src.LastError, formatTime(src.CreatedAt), formatTime(src.UpdatedAt),
	)
	if err != nil {
		return fmt.Errorf("create source: %w", err)
	}
	return nil
}

// GetSource loads one source by ID.
func (s *Store) GetSource(ctx context.Context, id string) (domain.Source, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+sourceColumns+` FROM sources WHERE id = ?`, id)
	src, err := scanSource(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Source{}, ErrNotFound
	}
	return src, err
}

// ListSources returns configured sources, enabled first and then by name.
func (s *Store) ListSources(ctx context.Context) ([]domain.Source, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+sourceColumns+` FROM sources ORDER BY enabled DESC, name COLLATE NOCASE, id`)
	if err != nil {
		return nil, fmt.Errorf("list sources: %w", err)
	}
	defer rows.Close()

	out := make([]domain.Source, 0, 4)
	for rows.Next() {
		src, err := scanSource(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, src)
	}
	return out, rows.Err()
}

// UpdateSource persists the editable fields of a source.
func (s *Store) UpdateSource(ctx context.Context, src *domain.Source) error {
	src.UpdatedAt = time.Now()
	config, err := encodeSourceConfig(*src)
	if err != nil {
		return err
	}

	res, err := s.db.ExecContext(ctx,
		`UPDATE sources SET kind = ?, name = ?, config = ?, secret_ref = ?, enabled = ?,
			schedule_minutes = ?, last_sync_at = ?, last_error = ?, updated_at = ?
		 WHERE id = ?`,
		string(src.Kind), src.Name, string(config), src.SecretRef, boolInt(src.Enabled),
		src.ScheduleMinutes, formatTime(src.LastSyncAt), src.LastError,
		formatTime(src.UpdatedAt), src.ID,
	)
	if err != nil {
		return fmt.Errorf("update source: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteSource removes a source and its mailbox cursors.
func (s *Store) DeleteSource(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM sources WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete source: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

// MarkSourceSynced records a successful sync and clears the last error.
func (s *Store) MarkSourceSynced(ctx context.Context, id string, at time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE sources SET last_sync_at = ?, last_error = '', updated_at = ? WHERE id = ?`,
		formatTime(at), formatTime(at), id,
	)
	if err != nil {
		return fmt.Errorf("mark source synced: %w", err)
	}
	return nil
}

// MarkSourceError records a failed sync without losing the last success time.
func (s *Store) MarkSourceError(ctx context.Context, id, message string) error {
	now := time.Now()
	_, err := s.db.ExecContext(ctx,
		`UPDATE sources SET last_error = ?, updated_at = ? WHERE id = ?`,
		message, formatTime(now), id,
	)
	if err != nil {
		return fmt.Errorf("mark source error: %w", err)
	}
	return nil
}

// GetMailboxCursor returns the cursor for a source mailbox, or ErrNotFound.
func (s *Store) GetMailboxCursor(ctx context.Context, sourceID, mailbox string) (domain.MailboxCursor, error) {
	var (
		cursor    domain.MailboxCursor
		updatedAt string
	)
	err := s.db.QueryRowContext(ctx,
		`SELECT source_id, mailbox, uid_validity, last_uid, updated_at
		 FROM mailbox_cursors WHERE source_id = ? AND mailbox = ?`,
		sourceID, mailbox,
	).Scan(&cursor.SourceID, &cursor.Mailbox, &cursor.UIDValidity, &cursor.LastUID, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.MailboxCursor{}, ErrNotFound
	}
	if err != nil {
		return domain.MailboxCursor{}, fmt.Errorf("get mailbox cursor: %w", err)
	}
	cursor.UpdatedAt = parseTime(updatedAt)
	return cursor, nil
}

// SaveMailboxCursor stores the last processed UID for a mailbox.
func (s *Store) SaveMailboxCursor(ctx context.Context, cursor domain.MailboxCursor) error {
	cursor.UpdatedAt = time.Now()
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO mailbox_cursors (source_id, mailbox, uid_validity, last_uid, updated_at)
		 VALUES (?,?,?,?,?)
		 ON CONFLICT (source_id, mailbox) DO UPDATE SET
			uid_validity = excluded.uid_validity,
			last_uid = excluded.last_uid,
			updated_at = excluded.updated_at`,
		cursor.SourceID, cursor.Mailbox, cursor.UIDValidity, cursor.LastUID,
		formatTime(cursor.UpdatedAt),
	)
	if err != nil {
		return fmt.Errorf("save mailbox cursor: %w", err)
	}
	return nil
}

func scanSource(sc interface{ Scan(dest ...any) error }) (domain.Source, error) {
	var (
		src       domain.Source
		kind      string
		config    string
		enabled   int
		lastSync  string
		createdAt string
		updatedAt string
	)
	err := sc.Scan(
		&src.ID, &kind, &src.Name, &config, &src.SecretRef, &enabled,
		&src.ScheduleMinutes, &lastSync, &src.LastError, &createdAt, &updatedAt,
	)
	if err != nil {
		return domain.Source{}, err
	}
	src.Kind = domain.SourceKind(kind)
	if config != "" {
		if err := decodeSourceConfig(kind, config, &src); err != nil {
			return domain.Source{}, fmt.Errorf("decode source %s config: %w", src.ID, err)
		}
	}
	src.Enabled = enabled != 0
	src.LastSyncAt = parseTime(lastSync)
	src.CreatedAt = parseTime(createdAt)
	src.UpdatedAt = parseTime(updatedAt)
	return src, nil
}

// encodeSourceConfig stores only the config that belongs to the source kind.
// Email configs keep their historical flat JSON shape so existing rows and
// older builds can still read them.
func encodeSourceConfig(src domain.Source) ([]byte, error) {
	var value any
	switch src.Kind {
	case domain.SourceBrowser:
		value = src.Browser
	default:
		value = src.Email
	}
	config, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encode source config: %w", err)
	}
	return config, nil
}

func decodeSourceConfig(kind, config string, src *domain.Source) error {
	if domain.SourceKind(kind) == domain.SourceBrowser {
		return json.Unmarshal([]byte(config), &src.Browser)
	}
	return json.Unmarshal([]byte(config), &src.Email)
}
