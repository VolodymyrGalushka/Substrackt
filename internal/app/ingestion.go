package app

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"
	"unicode"

	"substrackt/internal/domain"
	"substrackt/internal/ingest"
	"substrackt/internal/jobs"
	"substrackt/internal/llm"
	"substrackt/internal/store"
)

// ManualJobKind is the queue kind used by pasted receipts and emails.
const ManualJobKind = "ingest.manual"

// IngestionService owns the manual channel. Email, API and browser channels
// will enqueue the same kind of job later.
type IngestionService struct {
	manager *jobs.Manager
}

// NewIngestionService wires manual ingestion to the job manager.
func NewIngestionService(manager *jobs.Manager) *IngestionService {
	return &IngestionService{manager: manager}
}

// IngestManual queues pasted evidence for the ingestion pipeline.
func (s *IngestionService) IngestManual(in ManualInput) (Job, error) {
	payload, err := manualPayloadFromInput(in)
	if err != nil {
		return Job{}, err
	}
	job, err := s.manager.Enqueue(context.Background(), ManualJobKind, payload)
	if err != nil {
		return Job{}, err
	}
	return jobDTO(job), nil
}

// IngestionHandler executes queued ingestion jobs. It is not a bound service:
// only IngestionService is exposed to the frontend.
type IngestionHandler struct {
	store        *store.Store
	applier      *proposalApplier
	notifyReview func(ReviewItem)
}

// NewIngestionHandler wires the pipeline to the store and subscriptions.
func NewIngestionHandler(st *store.Store, subs *SubscriptionsService) *IngestionHandler {
	return &IngestionHandler{
		store:   st,
		applier: &proposalApplier{store: st, subs: subs},
	}
}

// SetReviewNotifier registers a callback fired when a new review item is
// created. Main uses it to emit review:new.
func (h *IngestionHandler) SetReviewNotifier(fn func(ReviewItem)) {
	h.notifyReview = fn
}

func manualPayloadFromInput(in ManualInput) (manualPayload, error) {
	content := strings.TrimSpace(in.Content)
	if content == "" {
		return manualPayload{}, fmt.Errorf("paste the receipt or email text first")
	}
	if len(content) > 100_000 {
		return manualPayload{}, fmt.Errorf("content is too large (limit 100000 characters)")
	}
	title := strings.TrimSpace(in.Title)
	if len(title) > 300 {
		title = title[:300]
	}
	vendorHint := strings.TrimSpace(in.VendorHint)
	if len(vendorHint) > 120 {
		vendorHint = vendorHint[:120]
	}

	occurred := time.Now()
	if raw := strings.TrimSpace(in.OccurredAt); raw != "" {
		parsed, err := parseOccurredAt(raw)
		if err != nil {
			return manualPayload{}, err
		}
		occurred = parsed
	}

	return manualPayload{
		Channel:    "manual",
		Title:      title,
		Content:    content,
		VendorHint: vendorHint,
		OccurredAt: occurred,
	}, nil
}

type manualPayload struct {
	Channel    string    `json:"channel"`
	Title      string    `json:"title"`
	Content    string    `json:"content"`
	VendorHint string    `json:"vendorHint"`
	OccurredAt time.Time `json:"occurredAt"`
}

// RunJob is the jobs.Handler for manual ingestion.
func (h *IngestionHandler) RunJob(ctx context.Context, job domain.Job, progress func(int, string)) (string, error) {
	var payload manualPayload
	if err := json.Unmarshal([]byte(job.Payload), &payload); err != nil {
		return "", fmt.Errorf("decode manual payload: %w", err)
	}

	channel := payload.Channel
	if channel == "" {
		channel = "manual"
	}
	item := ingest.RawItem{
		Channel:    channel,
		ExternalID: "",
		Title:      payload.Title,
		Content:    payload.Content,
		VendorHint: payload.VendorHint,
		OccurredAt: payload.OccurredAt,
	}

	result, err := h.runItem(ctx, job.ID, item, progress)
	if err != nil {
		return "", err
	}
	if result.Message == "" {
		result.Message = "Ingestion finished"
	}
	return result.Message, nil
}

// runItem executes one channel-independent item and notifies the review queue.
func (h *IngestionHandler) runItem(ctx context.Context, jobID string, item ingest.RawItem, progress func(int, string)) (ingest.Result, error) {
	pipeline, err := h.pipeline(ctx, jobID)
	if err != nil {
		return ingest.Result{}, err
	}
	result, err := pipeline.Run(ctx, item, progress)
	if err != nil {
		return ingest.Result{}, err
	}
	if result.ReviewID != "" {
		h.notifyNewReview(ctx, result.ReviewID)
	}
	return result, nil
}

// pipeline builds a configured pipeline. Email sync reuses one across messages
// so settings and the LLM client are created once per job.
func (h *IngestionHandler) pipeline(ctx context.Context, jobID string) (ingest.Pipeline, error) {
	settings, err := h.store.GetSettings(ctx)
	if err != nil {
		return ingest.Pipeline{}, err
	}

	config := ingest.Config{
		AutoApply:    settings.AutoApply,
		Threshold:    settings.AutoApplyThreshold,
		BaseCurrency: settings.BaseCurrency,
		Today:        appToday(ctx, h.store),
		JobID:        jobID,
		Model:        settings.LLMModel,
	}
	if settings.LLMEnabled() {
		client, err := llm.NewWithConsent(settings.LLMBaseURL, settings.LLMModel,
			time.Duration(settings.LLMTimeoutSeconds)*time.Second,
			settings.LLMRemoteConsent)
		if err != nil {
			return ingest.Pipeline{}, fmt.Errorf("configured LLM is invalid: %w", err)
		}
		config.Client = client
	}
	return ingest.Pipeline{Store: h.store, Applier: h.applier, Config: config}, nil
}

func (h *IngestionHandler) notifyNewReview(ctx context.Context, id string) {
	if h.notifyReview == nil {
		return
	}
	ex, err := h.store.GetExtraction(ctx, id)
	if err != nil {
		return
	}
	matchName := ""
	if ex.MatchSubID != "" {
		if sub, err := h.store.GetSubscription(ctx, ex.MatchSubID); err == nil {
			matchName = sub.Name
		}
	}
	h.notifyReview(reviewDTO(ex, matchName))
}

func parseOccurredAt(raw string) (time.Time, error) {
	for _, layout := range []string{time.RFC3339, "2006-01-02"} {
		if parsed, err := time.Parse(layout, raw); err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, fmt.Errorf("invalid date %q; use YYYY-MM-DD", raw)
}

// proposalApplier implements ingest.Applier using the subscription service so
// vendor resolution, validation and events stay in one place.
type proposalApplier struct {
	store *store.Store
	subs  *SubscriptionsService
}

func (a *proposalApplier) CreateProposal(ctx context.Context, p ingest.Proposal, prov ingest.Provenance) (string, error) {
	input := proposalInput(p, domain.Subscription{}, false)
	sub, err := a.subs.fromInput(ctx, domain.Subscription{}, input)
	if err != nil {
		return "", err
	}
	sub.Confidence = prov.Confidence
	sub.SourceID = prov.Source
	sub.ExternalID = prov.ExternalID
	if sub.NextRenewal.IsZero() {
		sub.RecomputeNextRenewal(a.subs.today(ctx))
	}
	if err := a.store.CreateSubscription(ctx, &sub); err != nil {
		return "", err
	}
	if dto, err := a.subs.toDTO(ctx, sub); err == nil {
		emit("subscription:changed", dto)
	}
	return sub.ID, nil
}

func (a *proposalApplier) UpdateProposal(ctx context.Context, subID string, p ingest.Proposal, prov ingest.Provenance) (string, error) {
	current, err := a.store.GetSubscription(ctx, subID)
	if err != nil {
		return "", err
	}
	input := proposalInput(p, current, true)
	sub, err := a.subs.fromInput(ctx, current, input)
	if err != nil {
		return "", err
	}
	sub.Confidence = prov.Confidence
	sub.SourceID = prov.Source
	sub.ExternalID = prov.ExternalID
	if sub.NextRenewal.IsZero() {
		sub.RecomputeNextRenewal(a.subs.today(ctx))
	}
	if err := a.store.UpdateSubscription(ctx, &sub); err != nil {
		return "", err
	}
	if dto, err := a.subs.toDTO(ctx, sub); err == nil {
		emit("subscription:changed", dto)
	}
	return sub.ID, nil
}

func (a *proposalApplier) FindMatch(ctx context.Context, p ingest.Proposal) (ingest.Match, error) {
	subs, err := a.store.ListSubscriptions(ctx, store.SubscriptionFilter{Limit: 5000})
	if err != nil {
		return ingest.Match{}, err
	}
	vendors, err := a.store.ListVendors(ctx)
	if err != nil {
		return ingest.Match{}, err
	}
	vendorNames := make(map[string]string, len(vendors))
	for _, v := range vendors {
		vendorNames[v.ID] = v.Name
	}

	best := ingest.Match{Kind: domain.MatchNone}
	bestScore := 0.0
	bestPriority := 0
	for _, sub := range subs {
		vendorName := vendorNames[sub.VendorID]
		if vendorName == "" {
			vendorName = sub.Name
		}
		vendorScore := nameScore(p.VendorName, vendorName)
		subScore := nameScore(p.Name, sub.Name)
		score := math.Max(vendorScore, subScore)
		if score < 0.6 {
			continue
		}

		sameCurrency := strings.EqualFold(p.Currency, sub.Price.Currency)
		sameAmount := p.AmountMinor > 0 && p.AmountMinor == sub.Price.AmountMinor
		sameInterval := p.Interval.Count == sub.Interval.Count && p.Interval.Unit == sub.Interval.Unit

		kind := domain.MatchNone
		reason := ""
		priority := 0
		switch {
		case p.Status == domain.StatusCancelled && score >= 0.7:
			kind = domain.MatchCancellation
			reason = fmt.Sprintf("Cancellation of %q", sub.Name)
			priority = 4
		case sameCurrency && sameAmount && sameInterval && score >= 0.8:
			kind = domain.MatchSame
			reason = fmt.Sprintf("Matches %q", sub.Name)
			priority = 3
		case sameCurrency && p.AmountMinor > 0 && !sameAmount && vendorScore >= 0.8:
			kind = domain.MatchPriceChange
			reason = fmt.Sprintf("Price change for %q", sub.Name)
			priority = 2
		case score >= 0.75:
			kind = domain.MatchAmbiguous
			reason = fmt.Sprintf("Possible match with %q", sub.Name)
			priority = 1
		default:
			continue
		}

		if priority > bestPriority || (priority == bestPriority && score > bestScore) {
			best = ingest.Match{
				SubscriptionID:   sub.ID,
				SubscriptionName: sub.Name,
				Kind:             kind,
				Reason:           reason,
			}
			bestPriority = priority
			bestScore = score
		}
	}
	return best, nil
}

func proposalInput(p ingest.Proposal, base domain.Subscription, update bool) SubscriptionInput {
	name := strings.TrimSpace(p.Name)
	if name == "" {
		name = strings.TrimSpace(p.VendorName)
	}
	input := SubscriptionInput{
		Name:          name,
		Plan:          p.Plan,
		VendorName:    p.VendorName,
		Status:        string(p.Status),
		Price:         Money{AmountMinor: p.AmountMinor, Currency: p.Currency, Scale: p.Scale},
		IntervalCount: p.Interval.Count,
		IntervalUnit:  string(p.Interval.Unit),
		AnchorDate:    dateString(p.AnchorDate),
		NextRenewal:   dateString(p.NextRenewal),
		AutoRenew:     true,
	}
	if update {
		input.AutoRenew = base.AutoRenew
		input.PaymentMethodID = base.PaymentMethodID
		input.Tags = base.Tags
		input.Notes = base.Notes
	}
	return input
}

func nameScore(a, b string) float64 {
	left := normalizeName(a)
	right := normalizeName(b)
	if left == "" || right == "" {
		return 0
	}
	if left == right {
		return 1
	}
	if strings.Contains(left, right) || strings.Contains(right, left) {
		return 0.82
	}
	leftTokens := strings.Fields(left)
	rightTokens := strings.Fields(right)
	if len(leftTokens) == 0 || len(rightTokens) == 0 {
		return 0
	}
	seen := make(map[string]struct{}, len(leftTokens))
	for _, token := range leftTokens {
		seen[token] = struct{}{}
	}
	shared := 0
	for _, token := range rightTokens {
		if _, ok := seen[token]; ok {
			shared++
		}
	}
	union := len(leftTokens) + len(rightTokens) - shared
	if union == 0 {
		return 0
	}
	return float64(shared) / float64(union)
}

func normalizeName(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		} else {
			b.WriteByte(' ')
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}
