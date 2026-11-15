package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"substrackt/internal/domain"
)

const jobColumns = `id, kind, status, payload, progress, message, error, attempts,
	max_attempts, created_at, started_at, finished_at, updated_at`

const defaultJobLimit = 200

// CreateJob inserts a queued job, assigning an ID and timestamps when missing.
func (s *Store) CreateJob(ctx context.Context, job *domain.Job) error {
	if job.ID == "" {
		job.ID = domain.NewID()
	}
	now := time.Now()
	if job.Status == "" {
		job.Status = domain.JobQueued
	}
	if !job.Status.Valid() {
		return fmt.Errorf("invalid job status %q", job.Status)
	}
	if job.MaxAttempts <= 0 {
		job.MaxAttempts = 3
	}
	if job.CreatedAt.IsZero() {
		job.CreatedAt = now
	}
	job.UpdatedAt = now

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO jobs (`+jobColumns+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		job.ID, job.Kind, string(job.Status), job.Payload, job.Progress, job.Message,
		job.Error, job.Attempts, job.MaxAttempts,
		formatTime(job.CreatedAt), formatTime(job.StartedAt), formatTime(job.FinishedAt),
		formatTime(job.UpdatedAt),
	)
	if err != nil {
		return fmt.Errorf("create job: %w", err)
	}
	return nil
}

// GetJob loads one job by ID.
func (s *Store) GetJob(ctx context.Context, id string) (domain.Job, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+jobColumns+` FROM jobs WHERE id = ?`, id)
	job, err := scanJob(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Job{}, ErrNotFound
	}
	return job, err
}

// ListJobs returns recent jobs, newest first.
func (s *Store) ListJobs(ctx context.Context, limit int) ([]domain.Job, error) {
	if limit <= 0 {
		limit = defaultJobLimit
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+jobColumns+` FROM jobs ORDER BY created_at DESC, id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("list jobs: %w", err)
	}
	defer rows.Close()

	jobs := make([]domain.Job, 0, 16)
	for rows.Next() {
		job, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}

// ClaimNextJob atomically moves the oldest queued job to running. It returns
// ErrNotFound when the queue is empty.
func (s *Store) ClaimNextJob(ctx context.Context) (domain.Job, error) {
	for attempt := 0; attempt < 8; attempt++ {
		var id string
		err := s.db.QueryRowContext(ctx,
			`SELECT id FROM jobs WHERE status = ? ORDER BY created_at, id LIMIT 1`,
			string(domain.JobQueued),
		).Scan(&id)
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Job{}, ErrNotFound
		}
		if err != nil {
			return domain.Job{}, fmt.Errorf("find queued job: %w", err)
		}

		now := time.Now()
		res, err := s.db.ExecContext(ctx,
			`UPDATE jobs SET status = ?, started_at = ?, updated_at = ?, attempts = attempts + 1
			 WHERE id = ? AND status = ?`,
			string(domain.JobRunning), formatTime(now), formatTime(now), id, string(domain.JobQueued),
		)
		if err != nil {
			return domain.Job{}, fmt.Errorf("claim job: %w", err)
		}
		affected, err := res.RowsAffected()
		if err != nil {
			return domain.Job{}, err
		}
		if affected == 1 {
			return s.GetJob(ctx, id)
		}
	}
	return domain.Job{}, ErrNotFound
}

// UpdateJobProgress records a running job's progress and status message.
func (s *Store) UpdateJobProgress(ctx context.Context, id string, percent int, message string) error {
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE jobs SET progress = ?, message = ?, updated_at = ? WHERE id = ? AND status = ?`,
		percent, message, formatTime(time.Now()), id, string(domain.JobRunning),
	)
	if err != nil {
		return fmt.Errorf("update job progress: %w", err)
	}
	return nil
}

// FinishJob moves a running job to a terminal state.
func (s *Store) FinishJob(ctx context.Context, id string, status domain.JobStatus, message, errText string) error {
	if !status.Terminal() {
		return fmt.Errorf("job status %q is not terminal", status)
	}
	now := time.Now()
	progress := 100
	if status == domain.JobCancelled {
		progress = 100
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE jobs SET status = ?, progress = ?, message = ?, error = ?, finished_at = ?, updated_at = ?
		 WHERE id = ?`,
		string(status), progress, message, errText, formatTime(now), formatTime(now), id,
	)
	if err != nil {
		return fmt.Errorf("finish job: %w", err)
	}
	return nil
}

// RequeueJob resets a terminal job so a worker can run it again.
func (s *Store) RequeueJob(ctx context.Context, id string) error {
	now := time.Now()
	res, err := s.db.ExecContext(ctx,
		`UPDATE jobs SET status = ?, progress = 0, message = '', error = '',
			attempts = 0, started_at = '', finished_at = '', updated_at = ?
		 WHERE id = ?`,
		string(domain.JobQueued), formatTime(now), id,
	)
	if err != nil {
		return fmt.Errorf("requeue job: %w", err)
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

// CancelQueuedJob cancels a job that has not started yet. It reports whether a
// row was changed; a running job must be cancelled through its context.
func (s *Store) CancelQueuedJob(ctx context.Context, id string) (bool, error) {
	now := time.Now()
	res, err := s.db.ExecContext(ctx,
		`UPDATE jobs SET status = ?, progress = 100, message = 'Cancelled', finished_at = ?, updated_at = ?
		 WHERE id = ? AND status = ?`,
		string(domain.JobCancelled), formatTime(now), formatTime(now), id, string(domain.JobQueued),
	)
	if err != nil {
		return false, fmt.Errorf("cancel job: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return affected > 0, nil
}

// RecoverInterruptedJobs requeues jobs that were running when the process
// stopped. It returns how many were recovered.
func (s *Store) RecoverInterruptedJobs(ctx context.Context) (int, error) {
	now := time.Now()
	res, err := s.db.ExecContext(ctx,
		`UPDATE jobs SET status = ?, progress = 0, message = 'Requeued after restart',
			error = '', started_at = '', updated_at = ?
		 WHERE status = ?`,
		string(domain.JobQueued), formatTime(now), string(domain.JobRunning),
	)
	if err != nil {
		return 0, fmt.Errorf("recover interrupted jobs: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	return int(affected), nil
}

// CountJobsByStatus returns how many jobs are in each status.
func (s *Store) CountJobsByStatus(ctx context.Context) (map[domain.JobStatus]int, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT status, COUNT(*) FROM jobs GROUP BY status`)
	if err != nil {
		return nil, fmt.Errorf("count jobs: %w", err)
	}
	defer rows.Close()

	counts := make(map[domain.JobStatus]int)
	for rows.Next() {
		var (
			status string
			count  int
		)
		if err := rows.Scan(&status, &count); err != nil {
			return nil, err
		}
		counts[domain.JobStatus(status)] = count
	}
	return counts, rows.Err()
}

func scanJob(sc interface{ Scan(dest ...any) error }) (domain.Job, error) {
	var (
		job        domain.Job
		status     string
		createdAt  string
		startedAt  string
		finishedAt string
		updatedAt  string
	)
	err := sc.Scan(
		&job.ID, &job.Kind, &status, &job.Payload, &job.Progress, &job.Message, &job.Error,
		&job.Attempts, &job.MaxAttempts, &createdAt, &startedAt, &finishedAt, &updatedAt,
	)
	if err != nil {
		return domain.Job{}, err
	}
	job.Status = domain.JobStatus(status)
	job.CreatedAt = parseTime(createdAt)
	job.StartedAt = parseTime(startedAt)
	job.FinishedAt = parseTime(finishedAt)
	job.UpdatedAt = parseTime(updatedAt)
	return job, nil
}
