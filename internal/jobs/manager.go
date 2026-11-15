// Package jobs runs persisted background work. Jobs survive restarts and are
// cancelled cooperatively through their context.
package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"substrackt/internal/domain"
	"substrackt/internal/store"
)

// Handler executes one job. It may report progress and must return quickly when
// ctx is cancelled.
type Handler func(ctx context.Context, job domain.Job, progress func(percent int, message string)) (string, error)

// Events receives lifecycle notifications. Callbacks run on worker goroutines.
type Events struct {
	OnProgress func(domain.Job)
	OnFinished func(domain.Job)
}

// Manager owns the worker pool and the cancellation registry.
type Manager struct {
	store   *store.Store
	workers int
	events  Events

	mu       sync.Mutex
	handlers map[string]Handler
	cancels  map[string]context.CancelFunc
	ctx      context.Context
	cancel   context.CancelFunc
	started  bool
	wg       sync.WaitGroup
	signal   chan struct{}
}

// New creates a manager. Work starts only after Start.
func New(st *store.Store, workers int, events Events) *Manager {
	if workers <= 0 {
		workers = 2
	}
	return &Manager{
		store:    st,
		workers:  workers,
		events:   events,
		handlers: make(map[string]Handler),
		cancels:  make(map[string]context.CancelFunc),
		signal:   make(chan struct{}, 1),
	}
}

// Handle registers the handler for a job kind.
func (m *Manager) Handle(kind string, handler Handler) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.handlers[kind] = handler
}

// Start launches the worker pool.
func (m *Manager) Start(parent context.Context) {
	m.mu.Lock()
	if m.started {
		m.mu.Unlock()
		return
	}
	m.ctx, m.cancel = context.WithCancel(parent)
	m.started = true
	m.mu.Unlock()

	for i := 0; i < m.workers; i++ {
		m.wg.Add(1)
		go m.worker()
	}
	m.Poke()
}

// Stop cancels running jobs and waits for the workers.
func (m *Manager) Stop() {
	m.mu.Lock()
	if !m.started {
		m.mu.Unlock()
		return
	}
	cancel := m.cancel
	m.started = false
	m.mu.Unlock()

	cancel()
	m.wg.Wait()
}

// Poke wakes one worker after new work is queued.
func (m *Manager) Poke() {
	select {
	case m.signal <- struct{}{}:
	default:
	}
}

// Enqueue persists a queued job and wakes a worker.
func (m *Manager) Enqueue(ctx context.Context, kind string, payload any) (domain.Job, error) {
	if kind == "" {
		return domain.Job{}, fmt.Errorf("job kind is required")
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return domain.Job{}, fmt.Errorf("encode job payload: %w", err)
	}
	job := domain.Job{
		Kind:    kind,
		Status:  domain.JobQueued,
		Payload: string(encoded),
	}
	if err := m.store.CreateJob(ctx, &job); err != nil {
		return domain.Job{}, err
	}
	m.Poke()
	return job, nil
}

// Recover requeues jobs that were running when the process stopped.
func (m *Manager) Recover(ctx context.Context) (int, error) {
	count, err := m.store.RecoverInterruptedJobs(ctx)
	if err == nil && count > 0 {
		m.Poke()
	}
	return count, err
}

// RunNext claims and executes one queued job. It reports false when the queue
// is empty, which makes it usable as a deterministic test driver.
func (m *Manager) RunNext(ctx context.Context) (bool, error) {
	job, err := m.store.ClaimNextJob(ctx)
	if errors.Is(err, store.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	m.mu.Lock()
	handler := m.handlers[job.Kind]
	m.mu.Unlock()
	if handler == nil {
		m.finish(job.ID, domain.JobFailed, "No handler registered", "unknown job kind "+job.Kind)
		return true, nil
	}

	jobCtx, cancel := context.WithCancel(ctx)
	m.mu.Lock()
	m.cancels[job.ID] = cancel
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		delete(m.cancels, job.ID)
		m.mu.Unlock()
		cancel()
	}()

	progress := func(percent int, message string) {
		if jobCtx.Err() != nil {
			return
		}
		writeCtx, writeCancel := dbContext()
		err := m.store.UpdateJobProgress(writeCtx, job.ID, percent, message)
		var updated domain.Job
		if err == nil {
			updated, err = m.store.GetJob(writeCtx, job.ID)
		}
		writeCancel()
		if err == nil && m.events.OnProgress != nil {
			m.events.OnProgress(updated)
		}
	}

	message, runErr := invoke(handler, jobCtx, job, progress)

	status := domain.JobSucceeded
	errText := ""
	switch {
	case jobCtx.Err() != nil || errors.Is(runErr, context.Canceled):
		status = domain.JobCancelled
		if message == "" {
			message = "Cancelled"
		}
	case runErr != nil:
		status = domain.JobFailed
		errText = runErr.Error()
		if message == "" {
			message = "Failed"
		}
	}
	m.finish(job.ID, status, message, errText)
	return true, nil
}

// Cancel stops a queued job immediately or asks a running job to stop.
func (m *Manager) Cancel(ctx context.Context, id string) (domain.Job, error) {
	job, err := m.store.GetJob(ctx, id)
	if err != nil {
		return domain.Job{}, err
	}
	if job.Status.Terminal() {
		return domain.Job{}, fmt.Errorf("job already %s", job.Status)
	}

	if job.Status == domain.JobQueued {
		cancelled, err := m.store.CancelQueuedJob(ctx, id)
		if err != nil {
			return domain.Job{}, err
		}
		if cancelled {
			return m.store.GetJob(ctx, id)
		}
		// It started between the read and the update; fall through to the
		// running path.
	}

	m.mu.Lock()
	cancel := m.cancels[id]
	m.mu.Unlock()
	if cancel == nil {
		return domain.Job{}, fmt.Errorf("job is not running yet; try again")
	}
	writeCtx, writeCancel := dbContext()
	_ = m.store.UpdateJobProgress(writeCtx, id, job.Progress, "Cancelling…")
	writeCancel()
	cancel()
	return m.store.GetJob(ctx, id)
}

// Retry requeues a terminal job.
func (m *Manager) Retry(ctx context.Context, id string) (domain.Job, error) {
	job, err := m.store.GetJob(ctx, id)
	if err != nil {
		return domain.Job{}, err
	}
	if !job.Status.Terminal() {
		return domain.Job{}, fmt.Errorf("job is still %s", job.Status)
	}
	if err := m.store.RequeueJob(ctx, id); err != nil {
		return domain.Job{}, err
	}
	m.Poke()
	return m.store.GetJob(ctx, id)
}

// WaitIdle blocks until no queued or running jobs remain.
func (m *Manager) WaitIdle(ctx context.Context) error {
	for {
		counts, err := m.store.CountJobsByStatus(ctx)
		if err != nil {
			return err
		}
		if counts[domain.JobQueued] == 0 && counts[domain.JobRunning] == 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(25 * time.Millisecond):
		}
	}
}

func (m *Manager) worker() {
	defer m.wg.Done()
	for {
		select {
		case <-m.ctx.Done():
			return
		case <-m.signal:
		}
		for {
			if m.ctx.Err() != nil {
				return
			}
			ran, err := m.RunNext(m.ctx)
			if err != nil {
				time.Sleep(100 * time.Millisecond)
				break
			}
			if !ran {
				break
			}
		}
	}
}

func (m *Manager) finish(id string, status domain.JobStatus, message, errText string) {
	writeCtx, cancel := dbContext()
	defer cancel()
	_ = m.store.FinishJob(writeCtx, id, status, message, errText)
	if m.events.OnFinished == nil {
		return
	}
	if job, err := m.store.GetJob(writeCtx, id); err == nil {
		m.events.OnFinished(job)
	}
}

func invoke(handler Handler, ctx context.Context, job domain.Job, progress func(int, string)) (message string, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("job panicked: %v", r)
		}
	}()
	return handler(ctx, job, progress)
}

func dbContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 5*time.Second)
}
