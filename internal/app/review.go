package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"substrackt/internal/domain"
	"substrackt/internal/store"
)

// ReviewService is the review queue: parsed proposals wait here until the user
// accepts or rejects them.
type ReviewService struct {
	store *store.Store
	subs  *SubscriptionsService
}

// NewReviewService wires the queue to the store and subscription mapper.
func NewReviewService(st *store.Store, subs *SubscriptionsService) *ReviewService {
	return &ReviewService{store: st, subs: subs}
}

// List returns proposals with the given status. An empty status means pending.
func (s *ReviewService) List(status string) ([]ReviewItem, error) {
	raw := strings.ToLower(strings.TrimSpace(status))
	if raw == "" {
		raw = string(domain.ExtractionPending)
	}
	extractionStatus := domain.ExtractionStatus(raw)
	if !extractionStatus.Valid() {
		return nil, fmt.Errorf("invalid review status %q", status)
	}

	rows, err := s.store.ListExtractions(context.Background(), extractionStatus, 200)
	if err != nil {
		return nil, err
	}
	out := make([]ReviewItem, 0, len(rows))
	for _, ex := range rows {
		out = append(out, s.toDTO(context.Background(), ex))
	}
	return out, nil
}

// Get returns one proposal.
func (s *ReviewService) Get(id string) (ReviewItem, error) {
	ex, err := s.store.GetExtraction(context.Background(), id)
	if err != nil {
		return ReviewItem{}, err
	}
	return s.toDTO(context.Background(), ex), nil
}

// Count returns the number of pending proposals.
func (s *ReviewService) Count() (int, error) {
	return s.store.CountExtractions(context.Background(), domain.ExtractionPending)
}

// Accept turns a proposal into a subscription, or updates the subscription it
// matched. Fields supplied in the input override the extracted values.
func (s *ReviewService) Accept(id string, in ReviewAcceptInput) (ReviewItem, error) {
	ctx := context.Background()
	ex, err := s.store.GetExtraction(ctx, id)
	if err != nil {
		return ReviewItem{}, err
	}
	if ex.Status != domain.ExtractionPending {
		return ReviewItem{}, fmt.Errorf("review item is already %s", ex.Status)
	}

	subID, err := s.apply(ctx, ex, in)
	if err != nil {
		return ReviewItem{}, err
	}

	ex.Status = domain.ExtractionAccepted
	ex.MatchSubID = subID
	ex.ResolvedAt = time.Now()
	ex.Error = ""
	if err := s.store.UpdateExtraction(ctx, &ex); err != nil {
		return ReviewItem{}, err
	}
	dto := s.toDTO(ctx, ex)
	emit("review:changed", dto)
	return dto, nil
}

// Reject dismisses a proposal without touching subscriptions.
func (s *ReviewService) Reject(id string) (ReviewItem, error) {
	ctx := context.Background()
	ex, err := s.store.GetExtraction(ctx, id)
	if err != nil {
		return ReviewItem{}, err
	}
	if ex.Status != domain.ExtractionPending {
		return ReviewItem{}, fmt.Errorf("review item is already %s", ex.Status)
	}
	ex.Status = domain.ExtractionRejected
	ex.ResolvedAt = time.Now()
	if err := s.store.UpdateExtraction(ctx, &ex); err != nil {
		return ReviewItem{}, err
	}
	dto := s.toDTO(ctx, ex)
	emit("review:changed", dto)
	return dto, nil
}

// BulkAccept accepts each ID independently and reports per-item failures.
func (s *ReviewService) BulkAccept(ids []string) (ReviewBulkResult, error) {
	result := ReviewBulkResult{Failed: []ReviewFailure{}}
	if len(ids) == 0 {
		return result, nil
	}
	for _, id := range ids {
		if _, err := s.Accept(id, ReviewAcceptInput{}); err != nil {
			result.Failed = append(result.Failed, ReviewFailure{ID: id, Error: err.Error()})
			continue
		}
		result.Accepted++
	}
	return result, nil
}

// BulkReject dismisses each ID independently and reports per-item failures.
func (s *ReviewService) BulkReject(ids []string) (ReviewBulkResult, error) {
	result := ReviewBulkResult{Failed: []ReviewFailure{}}
	if len(ids) == 0 {
		return result, nil
	}
	for _, id := range ids {
		if _, err := s.Reject(id); err != nil {
			result.Failed = append(result.Failed, ReviewFailure{ID: id, Error: err.Error()})
			continue
		}
		result.Rejected++
	}
	return result, nil
}

func (s *ReviewService) apply(ctx context.Context, ex domain.Extraction, in ReviewAcceptInput) (string, error) {
	update := false
	base := domain.Subscription{}
	if ex.MatchSubID != "" && (ex.MatchKind == domain.MatchPriceChange || ex.MatchKind == domain.MatchCancellation) {
		current, err := s.store.GetSubscription(ctx, ex.MatchSubID)
		if err != nil {
			if !errors.Is(err, store.ErrNotFound) {
				return "", err
			}
		} else {
			base = current
			update = true
		}
	}

	input := s.reviewInput(ex, in, base, update)
	sub, err := s.subs.fromInput(ctx, base, input)
	if err != nil {
		return "", err
	}
	sub.Confidence = ex.Confidence
	sub.SourceID = ex.Source
	sub.ExternalID = ex.ArtifactID
	today := s.subs.today(ctx)
	if sub.Status == domain.StatusCancelled || sub.Status == domain.StatusExpired {
		if sub.CancelledAt == nil {
			sub.CancelledAt = &today
		}
		sub.RecomputeNextRenewal(today)
	} else if sub.NextRenewal.IsZero() {
		sub.RecomputeNextRenewal(today)
	}

	if update {
		if err := s.store.UpdateSubscription(ctx, &sub); err != nil {
			return "", err
		}
	} else {
		sub.ID = ""
		if err := s.store.CreateSubscription(ctx, &sub); err != nil {
			return "", err
		}
	}

	dto, err := s.subs.toDTO(ctx, sub)
	if err != nil {
		return "", err
	}
	emit("subscription:changed", dto)
	return sub.ID, nil
}

func (s *ReviewService) reviewInput(ex domain.Extraction, in ReviewAcceptInput, base domain.Subscription, update bool) SubscriptionInput {
	amount := in.AmountMinor
	if amount <= 0 {
		amount = ex.AmountMinor
	}
	currency := strings.TrimSpace(in.Currency)
	scale := in.Scale
	if currency == "" {
		currency = ex.Currency
		scale = ex.Scale
	}
	if currency == "" {
		currency = "USD"
		scale = domain.ScaleFor(currency)
	}

	vendorName := strings.TrimSpace(in.VendorName)
	if vendorName == "" {
		vendorName = ex.VendorName
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		name = ex.Name
	}
	if name == "" {
		name = ex.VendorName
	}
	plan := strings.TrimSpace(in.Plan)
	if plan == "" {
		plan = ex.Plan
	}

	intervalCount := in.IntervalCount
	if intervalCount <= 0 {
		intervalCount = ex.Interval.Count
	}
	if intervalCount <= 0 {
		intervalCount = 1
	}
	unit := strings.ToLower(strings.TrimSpace(in.IntervalUnit))
	if unit == "" {
		unit = string(ex.Interval.Unit)
	}
	if unit == "" {
		unit = string(domain.UnitMonth)
	}

	anchor := strings.TrimSpace(in.AnchorDate)
	if anchor == "" {
		anchor = dateString(ex.AnchorDate)
	}
	next := strings.TrimSpace(in.NextRenewal)
	if next == "" {
		next = dateString(ex.NextRenewal)
	}

	status := strings.ToLower(strings.TrimSpace(in.Status))
	if status == "" {
		status = string(ex.DesiredStatus)
	}
	if update && strings.TrimSpace(in.Status) == "" && ex.MatchKind != domain.MatchCancellation &&
		ex.DesiredStatus == domain.StatusActive {
		status = string(base.Status)
	}

	tags := in.Tags
	if update && len(tags) == 0 {
		tags = base.Tags
	}
	notes := in.Notes
	if update && notes == "" {
		notes = base.Notes
	}
	paymentMethodID := strings.TrimSpace(in.PaymentMethodID)
	if update && paymentMethodID == "" {
		paymentMethodID = base.PaymentMethodID
	}

	autoRenew := true
	if update {
		autoRenew = base.AutoRenew
	}

	return SubscriptionInput{
		Name:            name,
		Plan:            plan,
		VendorName:      vendorName,
		Status:          status,
		Price:           Money{AmountMinor: amount, Currency: currency, Scale: scale},
		IntervalCount:   intervalCount,
		IntervalUnit:    unit,
		AnchorDate:      anchor,
		NextRenewal:     next,
		AutoRenew:       autoRenew,
		PaymentMethodID: paymentMethodID,
		Tags:            tags,
		Notes:           notes,
	}
}

func (s *ReviewService) toDTO(ctx context.Context, ex domain.Extraction) ReviewItem {
	matchName := ""
	if ex.MatchSubID != "" {
		if sub, err := s.store.GetSubscription(ctx, ex.MatchSubID); err == nil {
			matchName = sub.Name
		}
	}
	return reviewDTO(ex, matchName)
}
