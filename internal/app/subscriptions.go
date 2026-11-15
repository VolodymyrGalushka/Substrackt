package app

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"substrackt/internal/domain"
	"substrackt/internal/store"
)

// SubscriptionsService exposes subscription CRUD to the frontend.
type SubscriptionsService struct {
	store *store.Store
}

// NewSubscriptionsService wires the service to the store.
func NewSubscriptionsService(st *store.Store) *SubscriptionsService {
	return &SubscriptionsService{store: st}
}

// List returns subscriptions matching the filter.
func (s *SubscriptionsService) List(filter SubscriptionFilter) ([]Subscription, error) {
	ctx := context.Background()

	statuses := make([]domain.Status, 0, len(filter.Statuses))
	for _, raw := range filter.Statuses {
		st := domain.Status(strings.ToLower(strings.TrimSpace(raw)))
		if !st.Valid() {
			return nil, fmt.Errorf("invalid status filter %q", raw)
		}
		statuses = append(statuses, st)
	}

	subs, err := s.store.ListSubscriptions(ctx, store.SubscriptionFilter{
		Statuses:        statuses,
		VendorID:        filter.VendorID,
		PaymentMethodID: filter.PaymentMethodID,
		Search:          filter.Search,
		Limit:           filter.Limit,
	})
	if err != nil {
		return nil, err
	}
	return s.toDTOs(ctx, subs)
}

// Get returns a single subscription.
func (s *SubscriptionsService) Get(id string) (Subscription, error) {
	ctx := context.Background()
	sub, err := s.store.GetSubscription(ctx, id)
	if err != nil {
		return Subscription{}, err
	}
	return s.toDTO(ctx, sub)
}

// Upcoming returns billable subscriptions renewing within the next days days.
func (s *SubscriptionsService) Upcoming(days int) ([]Subscription, error) {
	ctx := context.Background()
	subs, err := s.store.UpcomingRenewals(ctx, s.today(ctx), days)
	if err != nil {
		return nil, err
	}
	return s.toDTOs(ctx, subs)
}

// Create adds a subscription.
func (s *SubscriptionsService) Create(in SubscriptionInput) (Subscription, error) {
	ctx := context.Background()

	sub, err := s.fromInput(ctx, domain.Subscription{}, in)
	if err != nil {
		return Subscription{}, err
	}
	sub.ID = ""
	if sub.NextRenewal.IsZero() {
		sub.RecomputeNextRenewal(s.today(ctx))
	}

	if err := s.store.CreateSubscription(ctx, &sub); err != nil {
		return Subscription{}, err
	}

	dto, err := s.toDTO(ctx, sub)
	if err != nil {
		return Subscription{}, err
	}
	emit("subscription:changed", dto)
	return dto, nil
}

// Update replaces the editable fields of a subscription.
func (s *SubscriptionsService) Update(id string, in SubscriptionInput) (Subscription, error) {
	ctx := context.Background()

	current, err := s.store.GetSubscription(ctx, id)
	if err != nil {
		return Subscription{}, err
	}
	updated, err := s.fromInput(ctx, current, in)
	if err != nil {
		return Subscription{}, err
	}
	if updated.NextRenewal.IsZero() {
		updated.RecomputeNextRenewal(s.today(ctx))
	}

	if err := s.store.UpdateSubscription(ctx, &updated); err != nil {
		return Subscription{}, err
	}

	dto, err := s.toDTO(ctx, updated)
	if err != nil {
		return Subscription{}, err
	}
	emit("subscription:changed", dto)
	return dto, nil
}

// SetStatus moves a subscription to another lifecycle status.
func (s *SubscriptionsService) SetStatus(id, status string) (Subscription, error) {
	ctx := context.Background()

	st := domain.Status(strings.ToLower(strings.TrimSpace(status)))
	if !st.Valid() {
		return Subscription{}, fmt.Errorf("invalid status %q", status)
	}

	sub, err := s.store.GetSubscription(ctx, id)
	if err != nil {
		return Subscription{}, err
	}
	if sub.Status == st {
		return s.toDTO(ctx, sub)
	}

	today := s.today(ctx)
	sub.Status = st
	if st == domain.StatusCancelled || st == domain.StatusExpired {
		sub.CancelledAt = &today
	} else {
		sub.CancelledAt = nil
	}
	sub.RecomputeNextRenewal(today)

	if err := s.store.UpdateSubscription(ctx, &sub); err != nil {
		return Subscription{}, err
	}

	dto, err := s.toDTO(ctx, sub)
	if err != nil {
		return Subscription{}, err
	}
	emit("subscription:changed", dto)
	return dto, nil
}

// Delete removes a subscription permanently.
func (s *SubscriptionsService) Delete(id string) error {
	if err := s.store.DeleteSubscription(context.Background(), id); err != nil {
		return err
	}
	emit("subscription:changed", map[string]string{"id": id, "deleted": "true"})
	return nil
}

func (s *SubscriptionsService) fromInput(ctx context.Context, base domain.Subscription, in SubscriptionInput) (domain.Subscription, error) {
	sub := base
	sub.Name = in.Name
	sub.Plan = in.Plan
	sub.Price = moneyDomain(in.Price)
	sub.Interval.Count = in.IntervalCount
	if sub.Interval.Count == 0 {
		sub.Interval.Count = 1
	}
	unit := strings.ToLower(strings.TrimSpace(in.IntervalUnit))
	if unit == "" {
		unit = string(domain.UnitMonth)
	}
	sub.Interval.Unit = domain.Unit(unit)
	sub.AutoRenew = in.AutoRenew
	sub.Tags = in.Tags
	sub.Notes = in.Notes
	sub.PaymentMethodID = in.PaymentMethodID

	status := domain.Status(strings.ToLower(strings.TrimSpace(in.Status)))
	if status == "" {
		status = domain.StatusActive
	}
	sub.Status = status

	anchor := strings.TrimSpace(in.AnchorDate)
	if anchor == "" {
		sub.AnchorDate = domain.Today()
	} else {
		d, err := domain.ParseDate(anchor)
		if err != nil {
			return domain.Subscription{}, err
		}
		sub.AnchorDate = d
	}

	if next := strings.TrimSpace(in.NextRenewal); next != "" {
		d, err := domain.ParseDate(next)
		if err != nil {
			return domain.Subscription{}, err
		}
		sub.NextRenewal = d
	} else {
		sub.NextRenewal = domain.Date{}
	}

	if in.StartedAt != "" {
		d, err := domain.ParseDate(strings.TrimSpace(in.StartedAt))
		if err != nil {
			return domain.Subscription{}, err
		}
		sub.StartedAt = &d
	}
	if in.TrialEndsAt != "" {
		d, err := domain.ParseDate(strings.TrimSpace(in.TrialEndsAt))
		if err != nil {
			return domain.Subscription{}, err
		}
		sub.TrialEndsAt = &d
	}

	vendorID, err := s.resolveVendor(ctx, in)
	if err != nil {
		return domain.Subscription{}, err
	}
	sub.VendorID = vendorID

	sub.Normalize()
	if err := sub.Validate(); err != nil {
		return domain.Subscription{}, err
	}
	return sub, nil
}

func (s *SubscriptionsService) resolveVendor(ctx context.Context, in SubscriptionInput) (string, error) {
	if id := strings.TrimSpace(in.VendorID); id != "" {
		if _, err := s.store.GetVendor(ctx, id); err != nil {
			return "", fmt.Errorf("vendor %s: %w", id, err)
		}
		return id, nil
	}

	name := strings.TrimSpace(in.VendorName)
	if name == "" {
		return "", nil
	}

	v, err := s.store.FindVendorByName(ctx, name)
	if err == nil {
		return v.ID, nil
	}
	if !errors.Is(err, store.ErrNotFound) {
		return "", err
	}

	created := domain.Vendor{Name: name}
	if err := s.store.CreateVendor(ctx, &created); err != nil {
		return "", err
	}
	return created.ID, nil
}

func (s *SubscriptionsService) toDTO(ctx context.Context, sub domain.Subscription) (Subscription, error) {
	vendorName, err := s.vendorName(ctx, sub.VendorID)
	if err != nil {
		return Subscription{}, err
	}
	label, err := s.paymentLabel(ctx, sub.PaymentMethodID)
	if err != nil {
		return Subscription{}, err
	}
	return subscriptionDTO(sub, vendorName, label), nil
}

func (s *SubscriptionsService) toDTOs(ctx context.Context, subs []domain.Subscription) ([]Subscription, error) {
	vendors, err := s.store.ListVendors(ctx)
	if err != nil {
		return nil, err
	}
	methods, err := s.store.ListPaymentMethods(ctx, true)
	if err != nil {
		return nil, err
	}

	vendorNames := make(map[string]string, len(vendors))
	for _, v := range vendors {
		vendorNames[v.ID] = v.Name
	}
	methodLabels := make(map[string]string, len(methods))
	for _, pm := range methods {
		methodLabels[pm.ID] = pm.Label
	}

	out := make([]Subscription, 0, len(subs))
	for _, sub := range subs {
		out = append(out, subscriptionDTO(sub, vendorNames[sub.VendorID], methodLabels[sub.PaymentMethodID]))
	}
	return out, nil
}

func (s *SubscriptionsService) vendorName(ctx context.Context, id string) (string, error) {
	if id == "" {
		return "", nil
	}
	v, err := s.store.GetVendor(ctx, id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return "", nil
		}
		return "", err
	}
	return v.Name, nil
}

func (s *SubscriptionsService) paymentLabel(ctx context.Context, id string) (string, error) {
	if id == "" {
		return "", nil
	}
	pm, err := s.store.GetPaymentMethod(ctx, id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return "", nil
		}
		return "", err
	}
	return pm.Label, nil
}
