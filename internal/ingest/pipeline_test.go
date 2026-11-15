package ingest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"substrackt/internal/domain"
	"substrackt/internal/llm"
	"substrackt/internal/store"
)

type fakeApplier struct {
	created  int
	proposal Proposal
	subID    string
}

func (f *fakeApplier) FindMatch(context.Context, Proposal) (Match, error) {
	return Match{Kind: domain.MatchNone}, nil
}

func (f *fakeApplier) CreateProposal(_ context.Context, p Proposal, _ Provenance) (string, error) {
	f.created++
	f.proposal = p
	if f.subID == "" {
		return "sub-1", nil
	}
	return f.subID, nil
}

func (f *fakeApplier) UpdateProposal(context.Context, string, Proposal, Provenance) (string, error) {
	return "", nil
}

func testPipeline(t *testing.T, applier Applier, cfg Config) (*Pipeline, *store.Store) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "ingest.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if cfg.JobID != "" {
		job := &domain.Job{ID: cfg.JobID, Kind: "ingest"}
		if err := st.CreateJob(context.Background(), job); err != nil {
			t.Fatalf("create job: %v", err)
		}
	}
	return &Pipeline{Store: st, Applier: applier, Config: cfg}, st
}

func TestPipelineQueuesHeuristicReview(t *testing.T) {
	applier := &fakeApplier{}
	pipeline, st := testPipeline(t, applier, Config{
		AutoApply:    true,
		Threshold:    0.9,
		BaseCurrency: "USD",
		Today:        mustDate(t, "2026-11-15"),
		JobID:        "job-1",
	})

	result, err := pipeline.Run(context.Background(), RawItem{
		Channel: "manual",
		Content: "From: Acme Cloud\nPlan $9.99 per month",
	}, nil)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if result.Action != ActionReview || result.ReviewID == "" {
		t.Fatalf("result = %+v, want review", result)
	}
	if applier.created != 0 {
		t.Fatal("heuristic extraction must not auto-apply")
	}

	ex, err := st.GetExtraction(context.Background(), result.ReviewID)
	if err != nil {
		t.Fatal(err)
	}
	if ex.Status != domain.ExtractionPending {
		t.Fatalf("status = %s", ex.Status)
	}
	if ex.VendorName != "Acme Cloud" || ex.AmountMinor != 999 || ex.Interval.Unit != domain.UnitMonth {
		t.Fatalf("unexpected extraction: %+v", ex)
	}
	if ex.Confidence > 0.85 {
		t.Fatalf("confidence = %v", ex.Confidence)
	}

	artifact, err := st.GetArtifact(context.Background(), result.ArtifactID)
	if err != nil {
		t.Fatal(err)
	}
	if artifact.Content != "From: Acme Cloud\nPlan $9.99 per month" {
		t.Fatalf("artifact content = %q", artifact.Content)
	}

	// Re-ingesting identical text must not create a second review item.
	duplicate, err := pipeline.Run(context.Background(), RawItem{
		Channel: "manual",
		Content: "From: Acme Cloud\nPlan $9.99 per month",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if duplicate.Action != ActionDuplicate || duplicate.ReviewID != result.ReviewID {
		t.Fatalf("duplicate = %+v", duplicate)
	}
	count, err := st.CountExtractions(context.Background(), domain.ExtractionPending)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("pending extractions = %d", count)
	}
}

func TestPipelineNeverAutoAppliesHeuristics(t *testing.T) {
	applier := &fakeApplier{}
	pipeline, _ := testPipeline(t, applier, Config{
		AutoApply:    true,
		Threshold:    0.1, // a user-lowered threshold must not let rules auto-apply
		BaseCurrency: "USD",
		Today:        mustDate(t, "2026-11-15"),
	})

	result, err := pipeline.Run(context.Background(), RawItem{
		Channel: "manual",
		Content: "From: Low Bar\n$4.00 per month",
	}, nil)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if result.Action != ActionReview || result.ReviewID == "" {
		t.Fatalf("result = %+v, want review", result)
	}
	if applier.created != 0 {
		t.Fatal("heuristic extraction must never auto-apply")
	}
}

func TestPipelineAutoAppliesHighConfidenceLLM(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{
				"message": map[string]string{"content": `{"is_subscription":true,"vendor":"Netflix","name":"Netflix","plan":"Standard","amount":"15.49","currency":"USD","interval_count":1,"interval_unit":"month","next_renewal":"2026-12-01","status":"active","payment_hint":"Visa ending 4242","confidence":0.96,"evidence":"Netflix $15.49 per month","reasoning":"clear receipt"}`},
			}},
			"usage": map[string]int{"prompt_tokens": 100, "completion_tokens": 20},
		})
	}))
	defer server.Close()

	client, err := llm.New(server.URL, "test-model", 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	applier := &fakeApplier{subID: "sub-42"}
	pipeline, st := testPipeline(t, applier, Config{
		AutoApply:    true,
		Threshold:    0.9,
		BaseCurrency: "USD",
		Today:        mustDate(t, "2026-11-15"),
		JobID:        "job-2",
		Client:       client,
		Model:        "test-model",
	})

	// The fake applier reports an existing subscription, so it must exist for
	// the extraction's match_subscription_id foreign key.
	if err := st.CreateSubscription(context.Background(), &domain.Subscription{
		ID:         "sub-42",
		Name:       "Netflix",
		Status:     domain.StatusActive,
		Price:      domain.NewMoney(1549, "USD", 2),
		Interval:   domain.Interval{Count: 1, Unit: domain.UnitMonth},
		AnchorDate: mustDate(t, "2026-11-15"),
	}); err != nil {
		t.Fatalf("create subscription: %v", err)
	}

	result, err := pipeline.Run(context.Background(), RawItem{
		Channel: "manual",
		Content: "Netflix receipt: $15.49 per month, next payment December 1, 2026",
	}, nil)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if result.Action != ActionCreated || result.SubscriptionID != "sub-42" {
		t.Fatalf("result = %+v", result)
	}
	if applier.created != 1 {
		t.Fatalf("created = %d", applier.created)
	}
	if applier.proposal.NextRenewal.String() != "2026-12-01" {
		t.Fatalf("next renewal = %s", applier.proposal.NextRenewal)
	}

	ex, err := st.GetExtraction(context.Background(), result.ExtractionID)
	if err != nil {
		t.Fatal(err)
	}
	if ex.Status != domain.ExtractionAccepted || ex.MatchSubID != "sub-42" {
		t.Fatalf("extraction = %+v", ex)
	}
	usage, err := st.ListLLMUsage(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(usage) != 1 || !usage[0].OK {
		t.Fatalf("usage = %+v", usage)
	}
}

func TestPipelineCachesLLMResults(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{
				"message": map[string]string{"content": `{"is_subscription":true,"vendor":"Hulu","name":"Hulu","amount":"7.99","currency":"USD","interval_count":1,"interval_unit":"month","status":"active","confidence":0.95}`},
			}},
			"usage": map[string]int{"prompt_tokens": 5, "completion_tokens": 3},
		})
	}))
	defer server.Close()

	client, err := llm.New(server.URL, "test-model", 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	pipeline, st := testPipeline(t, &fakeApplier{}, Config{
		AutoApply:    true,
		Threshold:    0.9,
		BaseCurrency: "USD",
		Today:        mustDate(t, "2026-11-15"),
		Client:       client,
		Model:        "test-model",
	})

	content := "Hulu $7.99 per month"
	today := mustDate(t, "2026-11-15")
	if _, _, err := pipeline.extractWithLLM(context.Background(), content, today, today); err != nil {
		t.Fatal(err)
	}
	if _, _, err := pipeline.extractWithLLM(context.Background(), content, today, today); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("model calls = %d, want 1 (second call should hit the cache)", calls)
	}
	usage, err := st.ListLLMUsage(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(usage) != 2 {
		t.Fatalf("usage rows = %d, want 2", len(usage))
	}
	cached := 0
	for _, row := range usage {
		if row.Cached {
			cached++
		}
	}
	if cached != 1 {
		t.Fatalf("cached rows = %d, want 1", cached)
	}
}

func TestPipelineIgnoresNonSubscription(t *testing.T) {
	pipeline, _ := testPipeline(t, &fakeApplier{}, Config{
		AutoApply:    true,
		Threshold:    0.9,
		BaseCurrency: "USD",
		Today:        mustDate(t, "2026-11-15"),
	})
	result, err := pipeline.Run(context.Background(), RawItem{
		Channel: "manual",
		Content: "Your parcel has shipped and will arrive on Tuesday.",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Action != ActionIgnored {
		t.Fatalf("result = %+v", result)
	}
}

func TestPipelineReprocessesIgnoredContent(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "ingest.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	content := "Your parcel has shipped and will arrive on Tuesday."
	first := &Pipeline{Store: st, Applier: &fakeApplier{}, Config: Config{
		BaseCurrency: "USD",
		Today:        mustDate(t, "2026-11-15"),
	}}
	ignored, err := first.Run(ctx, RawItem{Channel: "manual", Content: content}, nil)
	if err != nil {
		t.Fatalf("first run: %v", err)
	}
	if ignored.Action != ActionIgnored || ignored.ArtifactID == "" {
		t.Fatalf("ignored = %+v", ignored)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{
				"message": map[string]string{"content": `{"is_subscription":true,"vendor":"Parcel Plus","name":"Parcel Plus","amount":"4.99","currency":"USD","interval_count":1,"interval_unit":"month","status":"active","confidence":0.95}`},
			}},
			"usage": map[string]int{"prompt_tokens": 5, "completion_tokens": 3},
		})
	}))
	defer server.Close()

	client, err := llm.New(server.URL, "test-model", 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	// fakeApplier creates "sub-1", which must exist to satisfy the extraction's
	// match_subscription_id foreign key.
	if err := st.CreateSubscription(ctx, &domain.Subscription{
		ID:         "sub-1",
		Name:       "Parcel Plus",
		Status:     domain.StatusActive,
		Price:      domain.NewMoney(499, "USD", 2),
		Interval:   domain.Interval{Count: 1, Unit: domain.UnitMonth},
		AnchorDate: mustDate(t, "2026-11-15"),
	}); err != nil {
		t.Fatalf("create subscription: %v", err)
	}
	second := &Pipeline{Store: st, Applier: &fakeApplier{}, Config: Config{
		AutoApply:    true,
		Threshold:    0.9,
		BaseCurrency: "USD",
		Today:        mustDate(t, "2026-11-15"),
		Client:       client,
		Model:        "test-model",
	}}
	result, err := second.Run(ctx, RawItem{Channel: "manual", Content: content}, nil)
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if result.Action != ActionCreated {
		t.Fatalf("second run = %+v, want a created subscription", result)
	}
	if result.ArtifactID != ignored.ArtifactID {
		t.Fatalf("artifact = %q, want the existing one %q", result.ArtifactID, ignored.ArtifactID)
	}
}

func mustDate(t *testing.T, s string) domain.Date {
	t.Helper()
	d, err := domain.ParseDate(s)
	if err != nil {
		t.Fatal(err)
	}
	return d
}
