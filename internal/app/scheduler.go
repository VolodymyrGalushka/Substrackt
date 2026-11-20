package app

import (
	"context"
	"sync"
	"time"

	"substrackt/internal/domain"
	"substrackt/internal/jobs"
	"substrackt/internal/store"
)

// defaultSchedulerTick is how often due sources are checked. Individual sources
// are still polled no more often than their own schedule.
const defaultSchedulerTick = time.Minute

// SourceScheduler queues email syncs for enabled sources whose schedule has
// elapsed. It is optional: the Sources screen can always sync manually.
type SourceScheduler struct {
	store   *store.Store
	manager *jobs.Manager
	tick    time.Duration
	now     func() time.Time

	mu           sync.Mutex
	lastEnqueued map[string]time.Time
}

// NewSourceScheduler creates a scheduler. Work starts with Run.
func NewSourceScheduler(st *store.Store, manager *jobs.Manager) *SourceScheduler {
	return &SourceScheduler{
		store:        st,
		manager:      manager,
		tick:         defaultSchedulerTick,
		now:          time.Now,
		lastEnqueued: make(map[string]time.Time),
	}
}

// Run ticks until ctx is cancelled.
func (s *SourceScheduler) Run(ctx context.Context) {
	s.Tick(ctx)
	ticker := time.NewTicker(s.tick)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.Tick(ctx)
		}
	}
}

// Tick queues every due source and returns how many jobs were enqueued.
func (s *SourceScheduler) Tick(ctx context.Context) (int, error) {
	sources, err := s.store.ListSources(ctx)
	if err != nil {
		return 0, err
	}

	now := s.now()
	seen := make(map[string]struct{}, len(sources))
	enqueued := 0
	var firstErr error

	s.mu.Lock()
	defer s.mu.Unlock()

	for _, src := range sources {
		seen[src.ID] = struct{}{}
		if !src.Enabled || src.SecretRef == "" {
			continue
		}
		if src.Kind != domain.SourceEmail && src.Kind != domain.SourceBrowser {
			continue
		}

		schedule := time.Duration(src.ScheduleMinutes) * time.Minute
		if schedule <= 0 {
			schedule = time.Duration(domain.DefaultSourceScheduleMinutes) * time.Minute
		}
		last := src.LastSyncAt
		if at, ok := s.lastEnqueued[src.ID]; ok && at.After(last) {
			last = at
		}
		if !last.IsZero() && now.Sub(last) < schedule {
			continue
		}

		var kind string
		var payload any
		if src.Kind == domain.SourceBrowser {
			kind = BrowserJobKind
			payload = browserPayload{SourceID: src.ID}
		} else {
			kind = EmailJobKind
			payload = emailPayload{SourceID: src.ID}
		}
		if _, err := s.manager.Enqueue(ctx, kind, payload); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		s.lastEnqueued[src.ID] = now
		enqueued++
	}

	// Do not grow the cooldown map when sources are deleted.
	for id := range s.lastEnqueued {
		if _, ok := seen[id]; !ok {
			delete(s.lastEnqueued, id)
		}
	}
	return enqueued, firstErr
}
