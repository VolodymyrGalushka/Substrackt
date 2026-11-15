package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"substrackt/internal/domain"
)

const artifactColumns = `id, kind, media_type, content_hash, content, size, metadata, created_at`

// CreateArtifact stores raw evidence. The content hash is unique, so callers
// should look it up first to avoid a constraint error.
func (s *Store) CreateArtifact(ctx context.Context, a *domain.Artifact) error {
	if a.ID == "" {
		a.ID = domain.NewID()
	}
	if a.CreatedAt.IsZero() {
		a.CreatedAt = time.Now()
	}
	if a.Size == 0 {
		a.Size = len(a.Content)
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO artifacts (`+artifactColumns+`) VALUES (?,?,?,?,?,?,?,?)`,
		a.ID, a.Kind, a.MediaType, a.ContentHash, a.Content, a.Size, a.Metadata,
		formatTime(a.CreatedAt),
	)
	if err != nil {
		if isUniqueViolation(err) {
			return fmt.Errorf("artifact already exists")
		}
		return fmt.Errorf("create artifact: %w", err)
	}
	return nil
}

// FindArtifactByHash loads an artifact by its content hash.
func (s *Store) FindArtifactByHash(ctx context.Context, hash string) (domain.Artifact, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+artifactColumns+` FROM artifacts WHERE content_hash = ?`, hash)
	a, err := scanArtifact(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Artifact{}, ErrNotFound
	}
	return a, err
}

// GetArtifact loads an artifact by ID.
func (s *Store) GetArtifact(ctx context.Context, id string) (domain.Artifact, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+artifactColumns+` FROM artifacts WHERE id = ?`, id)
	a, err := scanArtifact(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Artifact{}, ErrNotFound
	}
	return a, err
}

func scanArtifact(sc interface{ Scan(dest ...any) error }) (domain.Artifact, error) {
	var (
		a         domain.Artifact
		createdAt string
	)
	err := sc.Scan(&a.ID, &a.Kind, &a.MediaType, &a.ContentHash, &a.Content, &a.Size,
		&a.Metadata, &createdAt)
	if err != nil {
		return domain.Artifact{}, err
	}
	a.CreatedAt = parseTime(createdAt)
	return a, nil
}
