package jobs

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"substrackt/internal/domain"
	"substrackt/internal/store"
)

const testKind = "ingest.manual"

func testManager(t *testing.T) (*Manager, *store.Store) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "jobs.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return New(st, 1, Events{}), st
}

func TestRunNextExecutesHandler(t *testing.T) {
	ctx := context.Background()
	mgr, st := testManager(t)

	var (
		progressed []int
		gotPayload string
	)
	mgr.Handle(testKind, func(ctx context.Context, job domain.Job, progress func(int, string)) (string, error) {
		gotPayload = job.Payload
		progress(50, "halfway")
		progressed = append(progressed, 50)
		return "Ingested", nil
	})

	job, err := mgr.Enqueue(ctx, testKind, map[string]string{"text": "hello"})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if job.Status != domain.JobQueued {
		t.Fatalf("status = %q, want queued", job.Status)
	}

	ran, err := mgr.RunNext(ctx)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !ran {
		t.Fatal("expected a job to run")
	}

	got, err := st.GetJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != domain.JobSucceeded {
		t.Fatalf("status = %q, want succeeded", got.Status)
	}
	if got.Message != "Ingested" || got.Progress != 100 {
		t.Fatalf("job = %+v", got)
	}
	if !strings.Contains(gotPayload, "hello") {
		t.Fatalf("payload = %q", gotPayload)
	}
	if len(progressed) != 1 {
		t.Fatalf("progress calls = %d", len(progressed))
	}

	ran, err = mgr.RunNext(ctx)
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if ran {
		t.Fatal("empty queue should not run a job")
	}
}

func TestRunNextRecordsHandlerErrors(t *testing.T) {
	ctx := context.Background()
	mgr, st := testManager(t)

	mgr.Handle(testKind, func(context.Context, domain.Job, func(int, string)) (string, error) {
		return "Extraction failed", errors.New("model unavailable")
	})
	job, err := mgr.Enqueue(ctx, testKind, nil)
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if _, err := mgr.RunNext(ctx); err != nil {
		t.Fatalf("run: %v", err)
	}

	got, err := st.GetJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != domain.JobFailed || got.Error != "model unavailable" {
		t.Fatalf("job = %+v", got)
	}
	if got.Message != "Extraction failed" {
		t.Fatalf("message = %q", got.Message)
	}
}

func TestRunNextFailsUnknownKind(t *testing.T) {
	ctx := context.Background()
	mgr, st := testManager(t)

	job, err := mgr.Enqueue(ctx, "unknown.kind", nil)
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if _, err := mgr.RunNext(ctx); err != nil {
		t.Fatalf("run: %v", err)
	}

	got, err := st.GetJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != domain.JobFailed {
		t.Fatalf("status = %q, want failed", got.Status)
	}
	if !strings.Contains(got.Error, "unknown job kind") {
		t.Fatalf("error = %q", got.Error)
	}
}

func TestRunNextRecoversFromPanic(t *testing.T) {
	ctx := context.Background()
	mgr, st := testManager(t)

	mgr.Handle(testKind, func(context.Context, domain.Job, func(int, string)) (string, error) {
		panic("boom")
	})
	job, err := mgr.Enqueue(ctx, testKind, nil)
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if _, err := mgr.RunNext(ctx); err != nil {
		t.Fatalf("run: %v", err)
	}

	got, err := st.GetJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != domain.JobFailed || !strings.Contains(got.Error, "boom") {
		t.Fatalf("job = %+v", got)
	}
}

func TestCancelQueuedAndRetry(t *testing.T) {
	ctx := context.Background()
	mgr, st := testManager(t)
	mgr.Handle(testKind, func(context.Context, domain.Job, func(int, string)) (string, error) {
		return "ok", nil
	})

	job, err := mgr.Enqueue(ctx, testKind, nil)
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	cancelled, err := mgr.Cancel(ctx, job.ID)
	if err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if cancelled.Status != domain.JobCancelled {
		t.Fatalf("status = %q, want cancelled", cancelled.Status)
	}
	if _, err := mgr.Cancel(ctx, job.ID); err == nil {
		t.Fatal("cancelling a terminal job should fail")
	}

	retried, err := mgr.Retry(ctx, job.ID)
	if err != nil {
		t.Fatalf("retry: %v", err)
	}
	if retried.Status != domain.JobQueued || retried.Attempts != 0 {
		t.Fatalf("retried = %+v", retried)
	}

	if _, err := mgr.RunNext(ctx); err != nil {
		t.Fatalf("run: %v", err)
	}
	got, err := st.GetJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != domain.JobSucceeded {
		t.Fatalf("status = %q, want succeeded", got.Status)
	}
	if _, err := mgr.Retry(ctx, job.ID+"missing"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("retry missing = %v, want ErrNotFound", err)
	}
}

func TestCancelRunningJobCooperatively(t *testing.T) {
	ctx := context.Background()
	mgr, st := testManager(t)

	started := make(chan struct{})
	mgr.Handle(testKind, func(ctx context.Context, _ domain.Job, progress func(int, string)) (string, error) {
		progress(10, "starting")
		close(started)
		<-ctx.Done()
		return "", ctx.Err()
	})

	job, err := mgr.Enqueue(ctx, testKind, nil)
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := mgr.RunNext(ctx)
		done <- err
	}()

	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("handler did not start")
	}

	if _, err := mgr.Cancel(ctx, job.ID); err != nil {
		t.Fatalf("cancel running: %v", err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("run did not return after cancellation")
	}

	got, err := st.GetJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != domain.JobCancelled {
		t.Fatalf("status = %q, want cancelled", got.Status)
	}
}

func TestStartDrainsQueue(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	st, err := store.Open(filepath.Join(t.TempDir(), "jobs.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	finished := make(chan domain.Job, 4)
	mgr := New(st, 2, Events{OnFinished: func(job domain.Job) { finished <- job }})
	mgr.Handle(testKind, func(context.Context, domain.Job, func(int, string)) (string, error) {
		return "done", nil
	})
	mgr.Start(ctx)
	defer mgr.Stop()

	for i := 0; i < 3; i++ {
		if _, err := mgr.Enqueue(ctx, testKind, map[string]int{"n": i}); err != nil {
			t.Fatalf("enqueue: %v", err)
		}
	}

	waitCtx, waitCancel := context.WithTimeout(ctx, 5*time.Second)
	defer waitCancel()
	if err := mgr.WaitIdle(waitCtx); err != nil {
		t.Fatalf("wait idle: %v", err)
	}

	counts, err := st.CountJobsByStatus(ctx)
	if err != nil {
		t.Fatalf("counts: %v", err)
	}
	if counts[domain.JobSucceeded] != 3 {
		t.Fatalf("counts = %+v, want 3 succeeded", counts)
	}

	for i := 0; i < 3; i++ {
		select {
		case job := <-finished:
			if job.Status != domain.JobSucceeded {
				t.Fatalf("finished event = %+v", job)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("finished events = %d, want 3", i)
		}
	}
}

func TestRecoverRequeuesRunningJobs(t *testing.T) {
	ctx := context.Background()
	mgr, st := testManager(t)

	job := &domain.Job{Kind: testKind}
	if err := st.CreateJob(ctx, job); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := st.ClaimNextJob(ctx); err != nil {
		t.Fatalf("claim: %v", err)
	}

	recovered, err := mgr.Recover(ctx)
	if err != nil {
		t.Fatalf("recover: %v", err)
	}
	if recovered != 1 {
		t.Fatalf("recovered = %d, want 1", recovered)
	}
	got, err := st.GetJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != domain.JobQueued {
		t.Fatalf("status = %q, want queued", got.Status)
	}
}

func TestEnqueueValidatesKind(t *testing.T) {
	ctx := context.Background()
	mgr, _ := testManager(t)
	if _, err := mgr.Enqueue(ctx, "", nil); err == nil {
		t.Fatal("expected an empty kind to be rejected")
	}
}
