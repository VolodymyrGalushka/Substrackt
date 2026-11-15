package app

import (
	"context"

	"substrackt/internal/domain"
	"substrackt/internal/jobs"
	"substrackt/internal/store"
)

// JobsService exposes the persisted background queue to the frontend.
type JobsService struct {
	store   *store.Store
	manager *jobs.Manager
}

// NewJobsService wires the service to the store and job manager.
func NewJobsService(st *store.Store, manager *jobs.Manager) *JobsService {
	return &JobsService{store: st, manager: manager}
}

// List returns recent jobs, newest first.
func (s *JobsService) List(limit int) ([]Job, error) {
	rows, err := s.store.ListJobs(context.Background(), limit)
	if err != nil {
		return nil, err
	}
	out := make([]Job, 0, len(rows))
	for _, row := range rows {
		out = append(out, jobDTO(row))
	}
	return out, nil
}

// Get returns one job.
func (s *JobsService) Get(id string) (Job, error) {
	job, err := s.store.GetJob(context.Background(), id)
	if err != nil {
		return Job{}, err
	}
	return jobDTO(job), nil
}

// Cancel stops a queued job or requests cancellation of a running one.
func (s *JobsService) Cancel(id string) (Job, error) {
	job, err := s.manager.Cancel(context.Background(), id)
	if err != nil {
		return Job{}, err
	}
	return jobDTO(job), nil
}

// Retry queues a failed or cancelled job again.
func (s *JobsService) Retry(id string) (Job, error) {
	job, err := s.manager.Retry(context.Background(), id)
	if err != nil {
		return Job{}, err
	}
	return jobDTO(job), nil
}

// Counts returns queue counts for badges.
func (s *JobsService) Counts() (JobCounts, error) {
	counts, err := s.store.CountJobsByStatus(context.Background())
	if err != nil {
		return JobCounts{}, err
	}
	return JobCounts{
		Queued:    counts[domain.JobQueued],
		Running:   counts[domain.JobRunning],
		Succeeded: counts[domain.JobSucceeded],
		Failed:    counts[domain.JobFailed],
		Cancelled: counts[domain.JobCancelled],
	}, nil
}
