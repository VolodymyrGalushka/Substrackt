package domain

import "time"

// JobStatus is the lifecycle state of a background job.
type JobStatus string

const (
	JobQueued    JobStatus = "queued"
	JobRunning   JobStatus = "running"
	JobSucceeded JobStatus = "succeeded"
	JobFailed    JobStatus = "failed"
	JobCancelled JobStatus = "cancelled"
)

func (s JobStatus) Valid() bool {
	switch s {
	case JobQueued, JobRunning, JobSucceeded, JobFailed, JobCancelled:
		return true
	}
	return false
}

// Terminal reports whether the job will not run again without an explicit retry.
func (s JobStatus) Terminal() bool {
	return s == JobSucceeded || s == JobFailed || s == JobCancelled
}

// Job is a persisted unit of background work. Payload is opaque JSON owned by
// the handler registered for Kind.
type Job struct {
	ID          string
	Kind        string
	Status      JobStatus
	Payload     string
	Progress    int
	Message     string
	Error       string
	Attempts    int
	MaxAttempts int
	CreatedAt   time.Time
	StartedAt   time.Time
	FinishedAt  time.Time
	UpdatedAt   time.Time
}
