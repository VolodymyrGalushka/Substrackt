package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"substrackt/internal/domain"
	"substrackt/internal/jobs"
	"substrackt/internal/secrets"
	"substrackt/internal/store"
)

type testApp struct {
	store    *store.Store
	subs     *SubscriptionsService
	methods  *PaymentMethodsService
	dash     *DashboardService
	review   *ReviewService
	settings *SettingsService
	jobsvc   *JobsService
	ingest   *IngestionService
	sources  *SourcesService
	email    *EmailHandler
	browser  *BrowserManager
	bhandler *BrowserHandler
	bengine  *fakeBrowserEngine
	secrets  secrets.Store
	manager  *jobs.Manager
	notified []ReviewItem
	// sourceStatus collects source:status notifications from the email handler.
	sourceStatus []Source
}

func newTestApp(t *testing.T) *testApp {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "m2.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	subs := NewSubscriptionsService(st)
	manager := jobs.New(st, 1, jobs.Events{})
	a := &testApp{
		store:    st,
		subs:     subs,
		methods:  NewPaymentMethodsService(st),
		dash:     NewDashboardService(st, subs),
		review:   NewReviewService(st, subs),
		settings: NewSettingsService(st),
		jobsvc:   NewJobsService(st, manager),
		manager:  manager,
	}
	a.ingest = NewIngestionService(manager)
	secretStore := secrets.NewMemoryStore()
	handler := NewIngestionHandler(st, subs)
	handler.SetReviewNotifier(func(item ReviewItem) {
		a.notified = append(a.notified, item)
	})
	manager.Handle(ManualJobKind, handler.RunJob)

	a.secrets = secretStore
	engine := &fakeBrowserEngine{ready: true, loginState: `{"cookies":[],"origins":[]}`}
	a.bengine = engine
	a.browser = NewBrowserManager(st, secretStore, engine)
	a.sources = NewSourcesService(st, secretStore, manager, a.browser)
	a.bhandler = NewBrowserHandler(st, handler, a.browser)
	manager.Handle(BrowserJobKind, a.bhandler.RunJob)
	a.email = NewEmailHandler(st, secretStore, handler)
	a.email.SetNotifier(func(src Source) {
		a.sourceStatus = append(a.sourceStatus, src)
	})
	manager.Handle(EmailJobKind, a.email.RunJob)
	return a
}

func (a *testApp) drain(t *testing.T) {
	t.Helper()
	for {
		ran, err := a.manager.RunNext(context.Background())
		if err != nil {
			t.Fatalf("run next: %v", err)
		}
		if !ran {
			return
		}
	}
}

func TestManualIngestionCreatesReviewThenSubscription(t *testing.T) {
	a := newTestApp(t)

	job, err := a.ingest.IngestManual(ManualInput{
		Title:   "Acme Cloud invoice",
		Content: "From: Acme Cloud\nPlan: Pro\n$9.99 per month\nNext payment January 15, 2027",
	})
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	if job.ID == "" || job.Status != string(domain.JobQueued) {
		t.Fatalf("job = %+v", job)
	}

	a.drain(t)

	finished, err := a.jobsvc.Get(job.ID)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if finished.Status != string(domain.JobSucceeded) {
		t.Fatalf("job status = %s (%s)", finished.Status, finished.Error)
	}
	if finished.Progress != 100 {
		t.Fatalf("job progress = %d", finished.Progress)
	}

	count, err := a.review.Count()
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("pending review items = %d, want 1", count)
	}
	if len(a.notified) != 1 {
		t.Fatalf("review notifications = %d, want 1", len(a.notified))
	}

	items, err := a.review.List("")
	if err != nil {
		t.Fatalf("list review: %v", err)
	}
	item := items[0]
	if item.VendorName != "Acme Cloud" || item.Price.AmountMinor != 999 || item.Price.Currency != "USD" {
		t.Fatalf("review item = %+v", item)
	}
	if item.IntervalUnit != "month" || item.IntervalCount != 1 {
		t.Fatalf("interval = %d %s", item.IntervalCount, item.IntervalUnit)
	}
	if item.NextRenewal != "2027-01-15" {
		t.Fatalf("next renewal = %q, want 2027-01-15", item.NextRenewal)
	}
	if item.Confidence <= 0 || item.Confidence > 0.85 {
		t.Fatalf("confidence = %v", item.Confidence)
	}
	if item.MatchKind != string(domain.MatchNone) {
		t.Fatalf("match kind = %q", item.MatchKind)
	}

	// The user edits the price before accepting.
	accepted, err := a.review.Accept(item.ID, ReviewAcceptInput{AmountMinor: 1199})
	if err != nil {
		t.Fatalf("accept: %v", err)
	}
	if accepted.Status != string(domain.ExtractionAccepted) || accepted.MatchSubscriptionID == "" {
		t.Fatalf("accepted = %+v", accepted)
	}

	subs, err := a.subs.List(SubscriptionFilter{})
	if err != nil {
		t.Fatalf("list subs: %v", err)
	}
	if len(subs) != 1 {
		t.Fatalf("subscriptions = %d, want 1", len(subs))
	}
	sub := subs[0]
	if sub.Name != "Acme Cloud" || sub.Price.AmountMinor != 1199 || sub.Price.Currency != "USD" {
		t.Fatalf("subscription = %+v", sub)
	}
	if sub.NextRenewal != "2027-01-15" {
		t.Fatalf("next renewal = %q, want the extracted date", sub.NextRenewal)
	}
	if sub.VendorID == "" || sub.VendorName != "Acme Cloud" {
		t.Fatalf("vendor not resolved: %+v", sub)
	}

	count, err = a.review.Count()
	if err != nil {
		t.Fatalf("count after accept: %v", err)
	}
	if count != 0 {
		t.Fatalf("pending after accept = %d", count)
	}
}

func TestManualIngestionDeduplicatesAndRedacts(t *testing.T) {
	a := newTestApp(t)

	content := "From: Acme Cloud\n$9.99 per month\ncard 4111 1111 1111 1111 cvv 123"
	for i := 0; i < 2; i++ {
		if _, err := a.ingest.IngestManual(ManualInput{Content: content}); err != nil {
			t.Fatalf("ingest %d: %v", i, err)
		}
	}
	a.drain(t)

	count, err := a.review.Count()
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("pending = %d, want 1 (identical content must not queue twice)", count)
	}

	items, err := a.review.List("")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	artifact, err := a.store.GetArtifact(context.Background(), items[0].ArtifactID)
	if err != nil {
		t.Fatalf("get artifact: %v", err)
	}
	if strings.Contains(artifact.Content, "4111 1111 1111 1111") {
		t.Fatalf("artifact stored a PAN: %q", artifact.Content)
	}
	if strings.Contains(artifact.Content, "123") && strings.Contains(artifact.Content, "cvv") {
		t.Fatalf("artifact stored a CVV: %q", artifact.Content)
	}

	if _, err := a.ingest.IngestManual(ManualInput{Content: "   "}); err == nil {
		t.Fatal("expected empty content to be rejected")
	}
	if _, err := a.ingest.IngestManual(ManualInput{Content: "x", OccurredAt: "not-a-date"}); err == nil {
		t.Fatal("expected an invalid date to be rejected")
	}
}

func TestReviewRejectAndBulkAccept(t *testing.T) {
	a := newTestApp(t)

	for _, content := range []string{
		"From: Alpha Cloud\n$5.00 per month",
		"From: Beta Cloud\n$6.00 per month",
	} {
		if _, err := a.ingest.IngestManual(ManualInput{Content: content}); err != nil {
			t.Fatalf("ingest: %v", err)
		}
	}
	a.drain(t)

	items, err := a.review.List("")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("pending = %d, want 2", len(items))
	}

	if _, err := a.review.Reject(items[0].ID); err != nil {
		t.Fatalf("reject: %v", err)
	}
	if _, err := a.review.Reject(items[0].ID); err == nil {
		t.Fatal("rejecting twice should fail")
	}

	result, err := a.review.BulkAccept([]string{items[1].ID, "missing"})
	if err != nil {
		t.Fatalf("bulk accept: %v", err)
	}
	if result.Accepted != 1 || len(result.Failed) != 1 {
		t.Fatalf("bulk result = %+v", result)
	}
	if result.Failed[0].ID != "missing" || result.Failed[0].Error == "" {
		t.Fatalf("failure = %+v", result.Failed[0])
	}

	// Accepting an item that was already resolved must fail.
	if _, err := a.review.Accept(items[1].ID, ReviewAcceptInput{}); err == nil {
		t.Fatal("accepting a resolved item should fail")
	}

	// A bulk accept with no successes still reports each failure.
	result, err = a.review.BulkAccept([]string{"missing"})
	if err != nil {
		t.Fatalf("bulk accept all-failed: %v", err)
	}
	if result.Accepted != 0 || len(result.Failed) != 1 || result.Failed[0].ID != "missing" {
		t.Fatalf("all-failed bulk result = %+v", result)
	}

	accepted, err := a.review.List("accepted")
	if err != nil {
		t.Fatalf("list accepted: %v", err)
	}
	if len(accepted) != 1 || accepted[0].ID != items[1].ID {
		t.Fatalf("accepted = %+v", accepted)
	}
}

func TestReviewBulkReject(t *testing.T) {
	a := newTestApp(t)

	for _, content := range []string{
		"From: Gamma Cloud\n$7.00 per month",
		"From: Delta Cloud\n$8.00 per month",
	} {
		if _, err := a.ingest.IngestManual(ManualInput{Content: content}); err != nil {
			t.Fatalf("ingest: %v", err)
		}
	}
	a.drain(t)

	items, err := a.review.List("")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("pending = %d, want 2", len(items))
	}

	result, err := a.review.BulkReject([]string{items[0].ID, "missing"})
	if err != nil {
		t.Fatalf("bulk reject: %v", err)
	}
	if result.Rejected != 1 || result.Accepted != 0 || len(result.Failed) != 1 {
		t.Fatalf("bulk reject result = %+v", result)
	}
	if result.Failed[0].ID != "missing" || result.Failed[0].Error == "" {
		t.Fatalf("failure = %+v", result.Failed[0])
	}

	// Rejecting an already resolved item must be reported, not silently dropped.
	result, err = a.review.BulkReject([]string{items[0].ID})
	if err != nil {
		t.Fatalf("bulk reject repeated: %v", err)
	}
	if result.Rejected != 0 || len(result.Failed) != 1 || result.Failed[0].ID != items[0].ID {
		t.Fatalf("repeated bulk reject result = %+v", result)
	}

	rejected, err := a.review.List("rejected")
	if err != nil {
		t.Fatalf("list rejected: %v", err)
	}
	if len(rejected) != 1 || rejected[0].ID != items[0].ID {
		t.Fatalf("rejected = %+v", rejected)
	}

	// The untouched item is still pending and can be rejected in bulk.
	result, err = a.review.BulkReject([]string{items[1].ID})
	if err != nil {
		t.Fatalf("bulk reject remaining: %v", err)
	}
	if result.Rejected != 1 || len(result.Failed) != 0 {
		t.Fatalf("remaining bulk reject result = %+v", result)
	}

	pending, err := a.review.List("")
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("pending = %d, want 0", len(pending))
	}
}

func TestSettingsValidationAndLocalEndpointPolicy(t *testing.T) {
	a := newTestApp(t)

	if _, err := a.settings.Get(); err != nil {
		t.Fatalf("get defaults: %v", err)
	}

	// A hosted provider must be refused: subscription data never leaves the LAN.
	if _, err := a.settings.Update(Settings{
		BaseCurrency:      "USD",
		LLMBaseURL:        "https://api.openai.com/v1",
		LLMModel:          "gpt-4o",
		LLMTimeoutSeconds: 30,
		AutoApply:         true,
	}); err == nil {
		t.Fatal("expected a public LLM endpoint to be rejected")
	}

	if _, err := a.settings.Update(Settings{
		BaseCurrency:      "USD",
		LLMBaseURL:        "http://127.0.0.1:11434",
		LLMTimeoutSeconds: 30,
	}); err == nil {
		t.Fatal("expected missing model to be rejected")
	}

	if _, err := a.settings.Update(Settings{
		BaseCurrency:      "USD",
		Timezone:          "Mars/Base",
		LLMTimeoutSeconds: 30,
	}); err == nil {
		t.Fatal("expected an unknown timezone to be rejected")
	}

	saved, err := a.settings.Update(Settings{
		Timezone:           "Europe/Berlin",
		BaseCurrency:       "eur",
		LLMBaseURL:         "http://127.0.0.1:11434",
		LLMModel:           "qwen2.5",
		LLMTimeoutSeconds:  45,
		AutoApply:          false,
		AutoApplyThreshold: 0.95,
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if saved.BaseCurrency != "EUR" {
		t.Fatalf("base currency = %q", saved.BaseCurrency)
	}
	if saved.LLMBaseURL != "http://127.0.0.1:11434/v1" {
		t.Fatalf("endpoint = %q, want the /v1 suffix", saved.LLMBaseURL)
	}

	got, err := a.settings.Get()
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got != saved {
		t.Fatalf("settings = %+v, want %+v", got, saved)
	}

	reset, err := a.settings.Reset()
	if err != nil {
		t.Fatalf("reset: %v", err)
	}
	if reset.BaseCurrency != "USD" || reset.LLMBaseURL != "" {
		t.Fatalf("reset = %+v", reset)
	}
}

func TestSettingsTestLLM(t *testing.T) {
	a := newTestApp(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]string{{"id": "qwen2.5"}, {"id": "llama3.2"}},
		})
	}))
	defer server.Close()

	result, err := a.settings.TestLLM()
	if err != nil {
		t.Fatalf("test with no endpoint: %v", err)
	}
	if result.OK || result.Error == "" {
		t.Fatalf("unconfigured result = %+v", result)
	}

	if _, err := a.settings.Update(Settings{
		BaseCurrency:      "USD",
		LLMBaseURL:        server.URL,
		LLMModel:          "qwen2.5",
		LLMTimeoutSeconds: 10,
		AutoApply:         true,
	}); err != nil {
		t.Fatalf("update: %v", err)
	}

	result, err = a.settings.TestLLM()
	if err != nil {
		t.Fatalf("test: %v", err)
	}
	if !result.OK || result.Error != "" {
		t.Fatalf("result = %+v", result)
	}
	if len(result.Models) != 2 || result.Models[0] != "qwen2.5" {
		t.Fatalf("models = %+v", result.Models)
	}
}

func TestSettingsDriveLLMDuringIngestion(t *testing.T) {
	a := newTestApp(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{
				"message": map[string]string{"content": `{"is_subscription":true,"vendor":"Spotify","name":"Spotify","amount":"11.99","currency":"USD","interval_count":1,"interval_unit":"month","status":"active","confidence":0.97,"evidence":"Spotify $11.99 per month"}`},
			}},
			"usage": map[string]int{"prompt_tokens": 20, "completion_tokens": 8},
		})
	}))
	defer server.Close()

	if _, err := a.settings.Update(Settings{
		BaseCurrency:       "USD",
		LLMBaseURL:         server.URL,
		LLMModel:           "local-model",
		LLMTimeoutSeconds:  10,
		AutoApply:          true,
		AutoApplyThreshold: domain.DefaultAutoApplyThreshold,
	}); err != nil {
		t.Fatalf("update: %v", err)
	}

	if _, err := a.ingest.IngestManual(ManualInput{
		Content: "Spotify receipt: $11.99 per month",
	}); err != nil {
		t.Fatalf("ingest: %v", err)
	}
	a.drain(t)

	// The LLM's confidence clears the threshold, so no review item is created.
	count, err := a.review.Count()
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Fatalf("pending = %d, want 0", count)
	}
	subs, err := a.subs.List(SubscriptionFilter{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(subs) != 1 || subs[0].Name != "Spotify" || subs[0].Price.AmountMinor != 1199 {
		t.Fatalf("subscriptions = %+v", subs)
	}

	usage, err := a.settings.Usage(10)
	if err != nil {
		t.Fatalf("usage: %v", err)
	}
	if len(usage) != 1 || !usage[0].OK || usage[0].Model != "local-model" {
		t.Fatalf("usage = %+v", usage)
	}
	if usage[0].PromptTokens != 20 || usage[0].CompletionTokens != 8 {
		t.Fatalf("tokens = %+v", usage[0])
	}
}

func TestJobsServiceCountsAndCancel(t *testing.T) {
	a := newTestApp(t)

	job, err := a.ingest.IngestManual(ManualInput{Content: "From: Gamma\n$3.00 per month"})
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}

	counts, err := a.jobsvc.Counts()
	if err != nil {
		t.Fatalf("counts: %v", err)
	}
	if counts.Queued != 1 {
		t.Fatalf("counts = %+v", counts)
	}

	cancelled, err := a.jobsvc.Cancel(job.ID)
	if err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if cancelled.Status != string(domain.JobCancelled) {
		t.Fatalf("status = %q", cancelled.Status)
	}

	// A cancelled job must not run.
	a.drain(t)
	count, err := a.review.Count()
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Fatalf("pending = %d, want 0", count)
	}

	// Retry queues it again; the worker now processes it.
	if _, err := a.jobsvc.Retry(job.ID); err != nil {
		t.Fatalf("retry: %v", err)
	}
	a.drain(t)
	count, err = a.review.Count()
	if err != nil {
		t.Fatalf("count after retry: %v", err)
	}
	if count != 1 {
		t.Fatalf("pending after retry = %d, want 1", count)
	}

	list, err := a.jobsvc.List(10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 || list[0].Status != string(domain.JobSucceeded) {
		t.Fatalf("jobs = %+v", list)
	}
}

func TestReviewUpdatesPriceChangeAndCancellation(t *testing.T) {
	a := newTestApp(t)

	created, err := a.subs.Create(SubscriptionInput{
		Name:          "Netflix",
		VendorName:    "Netflix",
		Status:        "active",
		Price:         Money{AmountMinor: 1549, Currency: "USD", Scale: 2},
		IntervalCount: 1,
		IntervalUnit:  "month",
		AnchorDate:    "2026-01-01",
		AutoRenew:     true,
		Tags:          []string{"shared", "household"},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if _, err := a.ingest.IngestManual(ManualInput{
		Content: "From: Netflix\n$17.99 per month",
	}); err != nil {
		t.Fatalf("ingest price change: %v", err)
	}
	a.drain(t)

	items, err := a.review.List("")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(items) != 1 || items[0].MatchKind != string(domain.MatchPriceChange) {
		t.Fatalf("price-change review = %+v", items)
	}
	// The UI sends an empty tags array when the user does not edit tags; an
	// update must preserve the subscription's existing tags.
	if _, err := a.review.Accept(items[0].ID, ReviewAcceptInput{Tags: []string{}}); err != nil {
		t.Fatalf("accept price change: %v", err)
	}

	subs, err := a.subs.List(SubscriptionFilter{})
	if err != nil {
		t.Fatalf("list subs: %v", err)
	}
	if len(subs) != 1 || subs[0].ID != created.ID {
		t.Fatalf("price change must update in place: %+v", subs)
	}
	if subs[0].Price.AmountMinor != 1799 {
		t.Fatalf("price = %d, want 1799", subs[0].Price.AmountMinor)
	}
	if len(subs[0].Tags) != 2 || subs[0].Tags[0] != "shared" {
		t.Fatalf("tags = %+v, want the existing tags", subs[0].Tags)
	}

	if _, err := a.ingest.IngestManual(ManualInput{
		Content: "From: Netflix\nYour subscription has been cancelled.",
	}); err != nil {
		t.Fatalf("ingest cancellation: %v", err)
	}
	a.drain(t)

	items, err = a.review.List("")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(items) != 1 || items[0].MatchKind != string(domain.MatchCancellation) {
		t.Fatalf("cancellation review = %+v", items)
	}
	if _, err := a.review.Accept(items[0].ID, ReviewAcceptInput{}); err != nil {
		t.Fatalf("accept cancellation: %v", err)
	}

	subs, err = a.subs.List(SubscriptionFilter{})
	if err != nil {
		t.Fatalf("list subs: %v", err)
	}
	if len(subs) != 1 || subs[0].Status != "cancelled" {
		t.Fatalf("cancelled subscription = %+v", subs)
	}
	if subs[0].CancelledAt == "" {
		t.Fatal("cancelled_at was not set")
	}
}

func TestManualIngestionMatchesExistingSubscription(t *testing.T) {
	a := newTestApp(t)

	created, err := a.subs.Create(SubscriptionInput{
		Name:          "Netflix",
		VendorName:    "Netflix",
		Status:        "active",
		Price:         Money{AmountMinor: 1549, Currency: "USD", Scale: 2},
		IntervalCount: 1,
		IntervalUnit:  "month",
		AnchorDate:    "2026-01-01",
		AutoRenew:     true,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if _, err := a.ingest.IngestManual(ManualInput{
		Content: "From: Netflix\n$15.49 per month",
	}); err != nil {
		t.Fatalf("ingest: %v", err)
	}
	a.drain(t)

	count, err := a.review.Count()
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Fatalf("pending = %d, want 0 (identical price should match silently)", count)
	}

	all, err := a.review.List("accepted")
	if err != nil {
		t.Fatalf("list accepted: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("accepted = %d, want 1", len(all))
	}
	if all[0].MatchKind != string(domain.MatchSame) || all[0].MatchSubscriptionID != created.ID {
		t.Fatalf("match = %+v", all[0])
	}
}
