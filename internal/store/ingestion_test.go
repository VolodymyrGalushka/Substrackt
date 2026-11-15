package store

import (
	"context"
	"errors"
	"strings"
	"testing"

	"substrackt/internal/domain"
)

func testArtifact() domain.Artifact {
	return domain.Artifact{
		Kind:        "manual",
		MediaType:   "text/plain",
		ContentHash: "hash-1",
		Content:     "Acme Cloud $9.99 per month",
		Metadata:    `{"channel":"manual"}`,
	}
}

func TestArtifactDeduplicatesByHash(t *testing.T) {
	ctx := context.Background()
	st := testStore(t)

	artifact := testArtifact()
	if err := st.CreateArtifact(ctx, &artifact); err != nil {
		t.Fatalf("create: %v", err)
	}
	if artifact.ID == "" {
		t.Fatal("create should assign an ID")
	}
	if artifact.Size != len(artifact.Content) {
		t.Fatalf("size = %d, want %d", artifact.Size, len(artifact.Content))
	}

	found, err := st.FindArtifactByHash(ctx, artifact.ContentHash)
	if err != nil {
		t.Fatalf("find by hash: %v", err)
	}
	if found.ID != artifact.ID || found.Content != artifact.Content {
		t.Fatalf("hash lookup = %+v", found)
	}

	duplicate := testArtifact()
	if err := st.CreateArtifact(ctx, &duplicate); err == nil {
		t.Fatal("expected duplicate content hash to be rejected")
	}

	if _, err := st.FindArtifactByHash(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing hash error = %v, want ErrNotFound", err)
	}
}

func TestExtractionQueueLifecycle(t *testing.T) {
	ctx := context.Background()
	st := testStore(t)

	job := &domain.Job{Kind: "ingest.manual"}
	if err := st.CreateJob(ctx, job); err != nil {
		t.Fatalf("create job: %v", err)
	}
	artifact := testArtifact()
	if err := st.CreateArtifact(ctx, &artifact); err != nil {
		t.Fatalf("create artifact: %v", err)
	}

	ex := domain.Extraction{
		JobID:         job.ID,
		ArtifactID:    artifact.ID,
		Source:        "manual",
		VendorName:    "Acme Cloud",
		Name:          "Acme Cloud Pro",
		Plan:          "Pro",
		AmountMinor:   999,
		Currency:      "USD",
		Scale:         2,
		Interval:      domain.Interval{Count: 1, Unit: domain.UnitMonth},
		AnchorDate:    mustDate(t, "2026-11-15"),
		NextRenewal:   mustDate(t, "2026-12-15"),
		DesiredStatus: domain.StatusActive,
		Confidence:    0.8,
		Model:         "local",
		Rationale:     "rule match",
		Evidence:      "$9.99 per month",
		Raw:           `{}`,
	}
	if err := st.CreateExtraction(ctx, &ex); err != nil {
		t.Fatalf("create extraction: %v", err)
	}
	if ex.Status != domain.ExtractionPending {
		t.Fatalf("default status = %q, want pending", ex.Status)
	}

	got, err := st.GetExtraction(ctx, ex.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.VendorName != "Acme Cloud" || got.AmountMinor != 999 || got.Interval.Unit != domain.UnitMonth {
		t.Fatalf("roundtrip = %+v", got)
	}
	if got.AnchorDate.String() != "2026-11-15" || got.NextRenewal.String() != "2026-12-15" {
		t.Fatalf("dates = %s / %s", got.AnchorDate, got.NextRenewal)
	}
	if got.JobID != job.ID || got.ArtifactID != artifact.ID {
		t.Fatalf("links = %q / %q", got.JobID, got.ArtifactID)
	}

	pending, err := st.ListExtractions(ctx, domain.ExtractionPending, 10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(pending) != 1 || pending[0].ID != ex.ID {
		t.Fatalf("pending = %+v", pending)
	}
	if count, err := st.CountExtractions(ctx, domain.ExtractionPending); err != nil || count != 1 {
		t.Fatalf("count = %d, %v", count, err)
	}

	sub := testSubscription(t)
	sub.RecomputeNextRenewal(mustDate(t, "2026-11-01"))
	if err := st.CreateSubscription(ctx, &sub); err != nil {
		t.Fatalf("create subscription: %v", err)
	}

	got.Status = domain.ExtractionAccepted
	got.MatchKind = domain.MatchNone
	got.MatchSubID = sub.ID
	got.ResolvedAt = got.UpdatedAt
	if err := st.UpdateExtraction(ctx, &got); err != nil {
		t.Fatalf("update: %v", err)
	}

	accepted, err := st.GetExtraction(ctx, ex.ID)
	if err != nil {
		t.Fatalf("get after update: %v", err)
	}
	if accepted.Status != domain.ExtractionAccepted || accepted.MatchSubID != sub.ID {
		t.Fatalf("accepted = %+v", accepted)
	}
	if accepted.ResolvedAt.IsZero() {
		t.Fatal("resolved_at was not persisted")
	}

	latest, err := st.LatestExtractionByArtifact(ctx, artifact.ID)
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if latest.ID != ex.ID {
		t.Fatalf("latest = %q, want %q", latest.ID, ex.ID)
	}
	if _, err := st.LatestExtractionByArtifact(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing artifact error = %v", err)
	}
}

func TestSettingsRoundTrip(t *testing.T) {
	ctx := context.Background()
	st := testStore(t)

	defaults, err := st.GetSettings(ctx)
	if err != nil {
		t.Fatalf("defaults: %v", err)
	}
	if defaults.BaseCurrency != "USD" || defaults.AutoApplyThreshold != domain.DefaultAutoApplyThreshold {
		t.Fatalf("defaults = %+v", defaults)
	}
	if !defaults.AutoApply {
		t.Fatal("auto-apply should default to true")
	}

	saved := domain.Settings{
		Timezone:           "Europe/Berlin",
		BaseCurrency:       "EUR",
		LLMBaseURL:         "http://127.0.0.1:11434/v1",
		LLMModel:           "qwen2.5",
		LLMTimeoutSeconds:  30,
		AutoApply:          false,
		AutoApplyThreshold: 0.95,
	}
	if err := st.SaveSettings(ctx, saved); err != nil {
		t.Fatalf("save: %v", err)
	}

	got, err := st.GetSettings(ctx)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got != saved {
		t.Fatalf("settings = %+v, want %+v", got, saved)
	}

	got.BaseCurrency = "GBP"
	if err := st.SaveSettings(ctx, got); err != nil {
		t.Fatalf("overwrite: %v", err)
	}
	final, err := st.GetSettings(ctx)
	if err != nil {
		t.Fatalf("final: %v", err)
	}
	if final.BaseCurrency != "GBP" {
		t.Fatalf("base currency = %q, want GBP", final.BaseCurrency)
	}
}

func TestLLMCacheAndUsage(t *testing.T) {
	ctx := context.Background()
	st := testStore(t)

	if _, found, err := st.GetLLMCache(ctx, "missing"); err != nil || found {
		t.Fatalf("missing cache = %v, %v", found, err)
	}

	if err := st.PutLLMCache(ctx, "key-1", "model-a", `{"vendor":"Acme"}`); err != nil {
		t.Fatalf("put: %v", err)
	}
	if err := st.PutLLMCache(ctx, "key-1", "model-a", `{"vendor":"Acme 2"}`); err != nil {
		t.Fatalf("overwrite: %v", err)
	}
	result, found, err := st.GetLLMCache(ctx, "key-1")
	if err != nil || !found {
		t.Fatalf("get = %v, %v", found, err)
	}
	if !strings.Contains(result, "Acme 2") {
		t.Fatalf("result = %q", result)
	}

	ok := &domain.LLMUsage{
		Model: "model-a", BaseURL: "http://127.0.0.1:11434/v1",
		PromptTokens: 10, CompletionTokens: 4, LatencyMS: 120, OK: true,
	}
	if err := st.LogLLMUsage(ctx, ok); err != nil {
		t.Fatalf("log ok: %v", err)
	}
	bad := &domain.LLMUsage{Model: "model-a", Cached: true, Error: "timeout"}
	if err := st.LogLLMUsage(ctx, bad); err != nil {
		t.Fatalf("log bad: %v", err)
	}

	usage, err := st.ListLLMUsage(ctx, 10)
	if err != nil {
		t.Fatalf("list usage: %v", err)
	}
	if len(usage) != 2 {
		t.Fatalf("usage rows = %d, want 2", len(usage))
	}
	cached, failed := 0, 0
	for _, row := range usage {
		if row.Cached {
			cached++
		}
		if !row.OK {
			failed++
		}
	}
	if cached != 1 || failed != 1 {
		t.Fatalf("cached = %d, failed = %d", cached, failed)
	}
	if usage[0].ID == "" {
		t.Fatal("usage ID was not assigned")
	}
}

func TestJobQueueLifecycle(t *testing.T) {
	ctx := context.Background()
	st := testStore(t)

	first := &domain.Job{Kind: "ingest.manual", Payload: `{"n":1}`}
	second := &domain.Job{Kind: "ingest.manual", Payload: `{"n":2}`}
	for _, job := range []*domain.Job{first, second} {
		if err := st.CreateJob(ctx, job); err != nil {
			t.Fatalf("create: %v", err)
		}
	}
	if first.Status != domain.JobQueued || first.MaxAttempts != 3 {
		t.Fatalf("defaults = %+v", first)
	}

	claimed, err := st.ClaimNextJob(ctx)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if claimed.ID != first.ID {
		t.Fatalf("claimed %q, want the oldest job %q", claimed.ID, first.ID)
	}
	if claimed.Status != domain.JobRunning || claimed.Attempts != 1 {
		t.Fatalf("claimed = %+v", claimed)
	}
	if claimed.StartedAt.IsZero() {
		t.Fatal("started_at was not set")
	}

	if err := st.UpdateJobProgress(ctx, claimed.ID, 45, "Extracting"); err != nil {
		t.Fatalf("progress: %v", err)
	}
	running, err := st.GetJob(ctx, claimed.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if running.Progress != 45 || running.Message != "Extracting" {
		t.Fatalf("progress = %+v", running)
	}

	if err := st.FinishJob(ctx, claimed.ID, domain.JobSucceeded, "Done", ""); err != nil {
		t.Fatalf("finish: %v", err)
	}
	finished, err := st.GetJob(ctx, claimed.ID)
	if err != nil {
		t.Fatalf("get finished: %v", err)
	}
	if finished.Status != domain.JobSucceeded || finished.Progress != 100 || finished.FinishedAt.IsZero() {
		t.Fatalf("finished = %+v", finished)
	}

	// The second job is still queued and can be cancelled before it starts.
	cancelled, err := st.CancelQueuedJob(ctx, second.ID)
	if err != nil {
		t.Fatalf("cancel queued: %v", err)
	}
	if !cancelled {
		t.Fatal("queued job was not cancelled")
	}
	if again, err := st.CancelQueuedJob(ctx, second.ID); err != nil || again {
		t.Fatalf("second cancel = %v, %v", again, err)
	}

	if _, err := st.ClaimNextJob(ctx); !errors.Is(err, ErrNotFound) {
		t.Fatalf("claim on empty queue = %v, want ErrNotFound", err)
	}

	if err := st.RequeueJob(ctx, claimed.ID); err != nil {
		t.Fatalf("requeue: %v", err)
	}
	requeued, err := st.GetJob(ctx, claimed.ID)
	if err != nil {
		t.Fatalf("get requeued: %v", err)
	}
	if requeued.Status != domain.JobQueued || requeued.Attempts != 0 || requeued.Progress != 0 {
		t.Fatalf("requeued = %+v", requeued)
	}

	counts, err := st.CountJobsByStatus(ctx)
	if err != nil {
		t.Fatalf("counts: %v", err)
	}
	if counts[domain.JobQueued] != 1 || counts[domain.JobCancelled] != 1 {
		t.Fatalf("counts = %+v", counts)
	}

	if err := st.RequeueJob(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("requeue missing = %v, want ErrNotFound", err)
	}
}

func TestRecoverInterruptedJobs(t *testing.T) {
	ctx := context.Background()
	st := testStore(t)

	job := &domain.Job{Kind: "ingest.manual"}
	if err := st.CreateJob(ctx, job); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := st.ClaimNextJob(ctx); err != nil {
		t.Fatalf("claim: %v", err)
	}

	recovered, err := st.RecoverInterruptedJobs(ctx)
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
	if got.Status != domain.JobQueued || !got.StartedAt.IsZero() {
		t.Fatalf("recovered job = %+v", got)
	}

	// Nothing is running anymore, so a second recovery is a no-op.
	recovered, err = st.RecoverInterruptedJobs(ctx)
	if err != nil {
		t.Fatalf("recover again: %v", err)
	}
	if recovered != 0 {
		t.Fatalf("second recovery = %d, want 0", recovered)
	}
}

func TestJobListOrder(t *testing.T) {
	ctx := context.Background()
	st := testStore(t)

	for i := 0; i < 3; i++ {
		if err := st.CreateJob(ctx, &domain.Job{Kind: "ingest.manual"}); err != nil {
			t.Fatalf("create: %v", err)
		}
	}
	jobs, err := st.ListJobs(ctx, 2)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(jobs) != 2 {
		t.Fatalf("jobs = %d, want 2", len(jobs))
	}
	if jobs[0].CreatedAt.Before(jobs[1].CreatedAt) {
		t.Fatalf("jobs are not newest first: %+v", jobs)
	}
}
