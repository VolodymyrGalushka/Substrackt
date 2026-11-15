package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"substrackt/internal/domain"
)

const extractionColumns = `id, job_id, artifact_id, source, status, vendor_name, name, plan,
	amount_minor, currency, scale, interval_count, interval_unit, anchor_date,
	next_renewal, desired_status, payment_hint, confidence, model, rationale, evidence,
	match_subscription_id, match_kind, raw, error, created_at, updated_at, resolved_at`

const defaultExtractionLimit = 200

// CreateExtraction inserts a proposal, assigning an ID and timestamps when missing.
func (s *Store) CreateExtraction(ctx context.Context, ex *domain.Extraction) error {
	if ex.ID == "" {
		ex.ID = domain.NewID()
	}
	now := time.Now()
	if ex.Status == "" {
		ex.Status = domain.ExtractionPending
	}
	if !ex.Status.Valid() {
		return fmt.Errorf("invalid extraction status %q", ex.Status)
	}
	if !ex.MatchKind.Valid() {
		ex.MatchKind = domain.MatchNone
	}
	if ex.DesiredStatus == "" {
		ex.DesiredStatus = domain.StatusActive
	}
	if ex.CreatedAt.IsZero() {
		ex.CreatedAt = now
	}
	ex.UpdatedAt = now

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO extractions (`+extractionColumns+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		ex.ID, nullString(ex.JobID), nullString(ex.ArtifactID), ex.Source, string(ex.Status),
		ex.VendorName, ex.Name, ex.Plan, ex.AmountMinor, ex.Currency, ex.Scale,
		ex.Interval.Count, string(ex.Interval.Unit), formatDate(ex.AnchorDate),
		formatDate(ex.NextRenewal), string(ex.DesiredStatus), ex.PaymentHint, ex.Confidence,
		ex.Model, ex.Rationale, ex.Evidence, nullString(ex.MatchSubID), string(ex.MatchKind),
		ex.Raw, ex.Error, formatTime(ex.CreatedAt), formatTime(ex.UpdatedAt),
		formatTime(ex.ResolvedAt),
	)
	if err != nil {
		return fmt.Errorf("create extraction: %w", err)
	}
	return nil
}

// GetExtraction loads one proposal by ID.
func (s *Store) GetExtraction(ctx context.Context, id string) (domain.Extraction, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+extractionColumns+` FROM extractions WHERE id = ?`, id)
	ex, err := scanExtraction(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Extraction{}, ErrNotFound
	}
	return ex, err
}

// ListExtractions returns proposals with the given status, newest first. An
// empty status returns every proposal.
func (s *Store) ListExtractions(ctx context.Context, status domain.ExtractionStatus, limit int) ([]domain.Extraction, error) {
	if limit <= 0 {
		limit = defaultExtractionLimit
	}
	query := `SELECT ` + extractionColumns + ` FROM extractions`
	args := []any{}
	if status != "" {
		query += ` WHERE status = ?`
		args = append(args, string(status))
	}
	query += ` ORDER BY created_at DESC, id DESC LIMIT ?`
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list extractions: %w", err)
	}
	defer rows.Close()

	out := make([]domain.Extraction, 0, 16)
	for rows.Next() {
		ex, err := scanExtraction(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, ex)
	}
	return out, rows.Err()
}

// LatestExtractionByArtifact returns the most recent proposal for an artifact.
func (s *Store) LatestExtractionByArtifact(ctx context.Context, artifactID string) (domain.Extraction, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+extractionColumns+` FROM extractions WHERE artifact_id = ?
		 ORDER BY created_at DESC, id DESC LIMIT 1`, artifactID)
	ex, err := scanExtraction(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Extraction{}, ErrNotFound
	}
	return ex, err
}

// CountExtractions returns how many proposals have the given status.
func (s *Store) CountExtractions(ctx context.Context, status domain.ExtractionStatus) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM extractions WHERE status = ?`, string(status)).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count extractions: %w", err)
	}
	return count, nil
}

// UpdateExtraction persists all mutable fields of a proposal.
func (s *Store) UpdateExtraction(ctx context.Context, ex *domain.Extraction) error {
	if !ex.Status.Valid() {
		return fmt.Errorf("invalid extraction status %q", ex.Status)
	}
	if !ex.MatchKind.Valid() {
		ex.MatchKind = domain.MatchNone
	}
	ex.UpdatedAt = time.Now()

	res, err := s.db.ExecContext(ctx,
		`UPDATE extractions SET job_id = ?, artifact_id = ?, source = ?, status = ?,
			vendor_name = ?, name = ?, plan = ?, amount_minor = ?, currency = ?, scale = ?,
			interval_count = ?, interval_unit = ?, anchor_date = ?, next_renewal = ?,
			desired_status = ?, payment_hint = ?, confidence = ?, model = ?, rationale = ?,
			evidence = ?, match_subscription_id = ?, match_kind = ?, raw = ?, error = ?,
			updated_at = ?, resolved_at = ?
		 WHERE id = ?`,
		nullString(ex.JobID), nullString(ex.ArtifactID), ex.Source, string(ex.Status),
		ex.VendorName, ex.Name, ex.Plan, ex.AmountMinor, ex.Currency, ex.Scale,
		ex.Interval.Count, string(ex.Interval.Unit), formatDate(ex.AnchorDate),
		formatDate(ex.NextRenewal), string(ex.DesiredStatus), ex.PaymentHint, ex.Confidence,
		ex.Model, ex.Rationale, ex.Evidence, nullString(ex.MatchSubID), string(ex.MatchKind),
		ex.Raw, ex.Error, formatTime(ex.UpdatedAt), formatTime(ex.ResolvedAt), ex.ID,
	)
	if err != nil {
		return fmt.Errorf("update extraction: %w", err)
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

func scanExtraction(sc interface{ Scan(dest ...any) error }) (domain.Extraction, error) {
	var (
		ex         domain.Extraction
		jobID      sql.NullString
		artifactID sql.NullString
		status     string
		unit       string
		anchor     string
		next       string
		desired    string
		matchSubID sql.NullString
		matchKind  string
		createdAt  string
		updatedAt  string
		resolvedAt string
	)
	err := sc.Scan(
		&ex.ID, &jobID, &artifactID, &ex.Source, &status, &ex.VendorName, &ex.Name, &ex.Plan,
		&ex.AmountMinor, &ex.Currency, &ex.Scale, &ex.Interval.Count, &unit, &anchor,
		&next, &desired, &ex.PaymentHint, &ex.Confidence, &ex.Model, &ex.Rationale,
		&ex.Evidence, &matchSubID, &matchKind, &ex.Raw, &ex.Error, &createdAt, &updatedAt,
		&resolvedAt,
	)
	if err != nil {
		return domain.Extraction{}, err
	}

	ex.JobID = jobID.String
	ex.ArtifactID = artifactID.String
	ex.Status = domain.ExtractionStatus(status)
	ex.Interval.Unit = domain.Unit(unit)
	ex.DesiredStatus = domain.Status(desired)
	ex.MatchSubID = matchSubID.String
	ex.MatchKind = domain.MatchKind(matchKind)
	ex.CreatedAt = parseTime(createdAt)
	ex.UpdatedAt = parseTime(updatedAt)
	ex.ResolvedAt = parseTime(resolvedAt)

	if anchor != "" {
		d, err := domain.ParseDate(anchor)
		if err != nil {
			return domain.Extraction{}, fmt.Errorf("extraction %s anchor: %w", ex.ID, err)
		}
		ex.AnchorDate = d
	}
	if next != "" {
		d, err := domain.ParseDate(next)
		if err != nil {
			return domain.Extraction{}, fmt.Errorf("extraction %s renewal: %w", ex.ID, err)
		}
		ex.NextRenewal = d
	}
	return ex, nil
}
