package app

import (
	"context"
	"sort"

	"substrackt/internal/domain"
	"substrackt/internal/store"
)

// DashboardService computes the dashboard aggregates.
type DashboardService struct {
	store *store.Store
	subs  *SubscriptionsService
}

// NewDashboardService wires the dashboard to the store and subscription mapper.
func NewDashboardService(st *store.Store, subs *SubscriptionsService) *DashboardService {
	return &DashboardService{store: st, subs: subs}
}

// Summary returns spend totals, upcoming renewals and groupings.
func (d *DashboardService) Summary(upcomingDays int) (DashboardSummary, error) {
	ctx := context.Background()
	if upcomingDays <= 0 {
		upcomingDays = 30
	}

	subs, err := d.store.ListSubscriptions(ctx, store.SubscriptionFilter{Limit: 5000})
	if err != nil {
		return DashboardSummary{}, err
	}

	summary := DashboardSummary{
		MonthlyTotals:   []Money{},
		YearlyTotals:    []Money{},
		Upcoming:        []Subscription{},
		ByPaymentMethod: []SpendBucket{},
		ByVendor:        []SpendBucket{},
	}

	monthly := make(map[string]int64) // currency -> amount
	scales := make(map[string]int)    // currency -> scale
	methodAgg := make(map[string]*spendAgg)
	vendorAgg := make(map[string]*spendAgg)

	for _, sub := range subs {
		switch sub.Status {
		case domain.StatusActive:
			summary.ActiveCount++
		case domain.StatusTrial:
			summary.TrialCount++
		case domain.StatusPaused:
			summary.PausedCount++
		case domain.StatusCancelled:
			summary.CancelledCount++
		}
		if !sub.Status.Billable() {
			continue
		}

		perMonth := sub.Price.PerMonth(sub.Interval)
		cur := perMonth.Currency
		monthly[cur] += perMonth.AmountMinor
		scales[cur] = perMonth.Scale

		methodKey := sub.PaymentMethodID + "|" + cur
		methodLabel := "Unassigned"
		if sub.PaymentMethodID != "" {
			if pm, err := d.store.GetPaymentMethod(ctx, sub.PaymentMethodID); err == nil {
				methodLabel = pm.Label
			}
		}
		accumulate(methodAgg, methodKey, methodLabel, perMonth)

		vendorKey := sub.VendorID + "|" + cur
		vendorLabel := "Unknown vendor"
		if sub.VendorID != "" {
			if v, err := d.store.GetVendor(ctx, sub.VendorID); err == nil {
				vendorLabel = v.Name
			}
		}
		accumulate(vendorAgg, vendorKey, vendorLabel, perMonth)
	}

	for cur, amount := range monthly {
		m := Money{AmountMinor: amount, Currency: cur, Scale: scales[cur]}
		summary.MonthlyTotals = append(summary.MonthlyTotals, m)
		summary.YearlyTotals = append(summary.YearlyTotals, Money{
			AmountMinor: amount * 12, Currency: cur, Scale: scales[cur],
		})
	}
	sortMoney(summary.MonthlyTotals)
	sortMoney(summary.YearlyTotals)

	upcoming, err := d.store.UpcomingRenewals(ctx, appToday(ctx, d.store), upcomingDays)
	if err != nil {
		return DashboardSummary{}, err
	}
	if summary.Upcoming, err = d.subs.toDTOs(ctx, upcoming); err != nil {
		return DashboardSummary{}, err
	}

	summary.ByPaymentMethod = flatten(methodAgg)
	summary.ByVendor = flatten(vendorAgg)
	return summary, nil
}

type spendAgg struct {
	label    string
	currency string
	scale    int
	amount   int64
	count    int
}

func accumulate(into map[string]*spendAgg, key, label string, m domain.Money) {
	agg, ok := into[key]
	if !ok {
		agg = &spendAgg{label: label, currency: m.Currency, scale: m.Scale}
		into[key] = agg
	}
	agg.amount += m.AmountMinor
	agg.count++
}

func flatten(in map[string]*spendAgg) []SpendBucket {
	out := make([]SpendBucket, 0, len(in))
	for key, agg := range in {
		out = append(out, SpendBucket{
			Key:   key,
			Label: agg.label,
			Monthly: Money{
				AmountMinor: agg.amount,
				Currency:    agg.currency,
				Scale:       agg.scale,
			},
			Count: agg.count,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Monthly.AmountMinor != out[j].Monthly.AmountMinor {
			return out[i].Monthly.AmountMinor > out[j].Monthly.AmountMinor
		}
		return out[i].Label < out[j].Label
	})
	return out
}

func sortMoney(in []Money) {
	sort.Slice(in, func(i, j int) bool {
		if in[i].Currency != in[j].Currency {
			return in[i].Currency < in[j].Currency
		}
		return in[i].AmountMinor > in[j].AmountMinor
	})
}
