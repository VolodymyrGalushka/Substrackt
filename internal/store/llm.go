package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"substrackt/internal/domain"
)

const usageColumns = `id, model, base_url, prompt_tokens, completion_tokens, latency_ms,
	ok, cached, error, created_at`

// GetLLMCache returns a cached extraction result for key.
func (s *Store) GetLLMCache(ctx context.Context, key string) (string, bool, error) {
	var result string
	err := s.db.QueryRowContext(ctx, `SELECT result FROM llm_cache WHERE key = ?`, key).Scan(&result)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("get llm cache: %w", err)
	}
	return result, true, nil
}

// PutLLMCache stores an extraction result keyed by model, prompt version and
// content hash.
func (s *Store) PutLLMCache(ctx context.Context, key, model, result string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO llm_cache (key, model, result, created_at) VALUES (?, ?, ?, ?)
		 ON CONFLICT (key) DO UPDATE SET model = excluded.model, result = excluded.result,
			created_at = excluded.created_at`,
		key, model, result, formatTime(time.Now()),
	)
	if err != nil {
		return fmt.Errorf("put llm cache: %w", err)
	}
	return nil
}

// LogLLMUsage appends an audit row for one model call.
func (s *Store) LogLLMUsage(ctx context.Context, usage *domain.LLMUsage) error {
	if usage.ID == "" {
		usage.ID = domain.NewID()
	}
	if usage.CreatedAt.IsZero() {
		usage.CreatedAt = time.Now()
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO llm_usage (`+usageColumns+`) VALUES (?,?,?,?,?,?,?,?,?,?)`,
		usage.ID, usage.Model, usage.BaseURL, usage.PromptTokens, usage.CompletionTokens,
		usage.LatencyMS, boolInt(usage.OK), boolInt(usage.Cached), usage.Error,
		formatTime(usage.CreatedAt),
	)
	if err != nil {
		return fmt.Errorf("log llm usage: %w", err)
	}
	return nil
}

// ListLLMUsage returns recent model calls, newest first.
func (s *Store) ListLLMUsage(ctx context.Context, limit int) ([]domain.LLMUsage, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+usageColumns+` FROM llm_usage ORDER BY created_at DESC, id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("list llm usage: %w", err)
	}
	defer rows.Close()

	out := make([]domain.LLMUsage, 0, 16)
	for rows.Next() {
		var (
			usage     domain.LLMUsage
			ok        int
			cached    int
			createdAt string
		)
		if err := rows.Scan(
			&usage.ID, &usage.Model, &usage.BaseURL, &usage.PromptTokens,
			&usage.CompletionTokens, &usage.LatencyMS, &ok, &cached, &usage.Error,
			&createdAt,
		); err != nil {
			return nil, err
		}
		usage.OK = ok != 0
		usage.Cached = cached != 0
		usage.CreatedAt = parseTime(createdAt)
		out = append(out, usage)
	}
	return out, rows.Err()
}
