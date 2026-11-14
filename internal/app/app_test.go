package app

import (
	"path/filepath"
	"testing"

	"substrackt/internal/domain"
	"substrackt/internal/store"
)

func newTestServices(t *testing.T) (*SubscriptionsService, *PaymentMethodsService, *DashboardService) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	subs := NewSubscriptionsService(st)
	return subs, NewPaymentMethodsService(st), NewDashboardService(st, subs)
}

func TestSubscriptionLifecycle(t *testing.T) {
	subs, methods, dash := newTestServices(t)

	pm, err := methods.Create(PaymentMethodInput{
		Label: "Visa •• 4242", Type: "card", Last4: "4242", Active: true,
	})
	if err != nil {
		t.Fatalf("create payment method: %v", err)
	}

	created, err := subs.Create(SubscriptionInput{
		Name:            "Netflix",
		Plan:            "Standard",
		VendorName:      "Netflix",
		Status:          "active",
		Price:           Money{AmountMinor: 1299, Currency: "usd", Scale: 2},
		IntervalCount:   1,
		IntervalUnit:    "month",
		AnchorDate:      "2026-01-15",
		AutoRenew:       true,
		PaymentMethodID: pm.ID,
		Tags:            []string{" entertainment ", "entertainment"},
	})
	if err != nil {
		t.Fatalf("create subscription: %v", err)
	}

	if created.VendorID == "" || created.VendorName != "Netflix" {
		t.Fatalf("vendor not resolved: %+v", created)
	}
	if created.Price.Currency != "USD" {
		t.Fatalf("currency not normalised: %+v", created.Price)
	}
	if len(created.Tags) != 1 || created.Tags[0] != "entertainment" {
		t.Fatalf("tags not normalised: %v", created.Tags)
	}
	if created.PaymentMethodLabel != pm.Label {
		t.Fatalf("payment label = %q, want %q", created.PaymentMethodLabel, pm.Label)
	}
	if created.NextRenewal == "" {
		t.Fatal("next renewal was not computed")
	}

	list, err := subs.List(SubscriptionFilter{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 || list[0].ID != created.ID {
		t.Fatalf("list = %+v", list)
	}

	paused, err := subs.SetStatus(created.ID, "paused")
	if err != nil {
		t.Fatalf("pause: %v", err)
	}
	if paused.Status != "paused" {
		t.Fatalf("status = %q, want paused", paused.Status)
	}

	summary, err := dash.Summary(30)
	if err != nil {
		t.Fatalf("summary: %v", err)
	}
	if summary.PausedCount != 1 {
		t.Fatalf("paused count = %d, want 1", summary.PausedCount)
	}
	if len(summary.MonthlyTotals) != 0 {
		t.Fatalf("paused subscription should not count towards spend: %+v", summary.MonthlyTotals)
	}

	if _, err := subs.SetStatus(created.ID, "active"); err != nil {
		t.Fatalf("resume: %v", err)
	}
	summary, err = dash.Summary(30)
	if err != nil {
		t.Fatalf("summary after resume: %v", err)
	}
	if len(summary.MonthlyTotals) != 1 {
		t.Fatalf("monthly totals = %+v, want one currency", summary.MonthlyTotals)
	}
	if got := summary.MonthlyTotals[0]; got.AmountMinor != 1299 || got.Currency != "USD" {
		t.Fatalf("monthly total = %+v, want 1299 USD", got)
	}
	if got := summary.YearlyTotals[0]; got.AmountMinor != 15588 {
		t.Fatalf("yearly total = %d, want 15588", got.AmountMinor)
	}

	if err := subs.Delete(created.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	list, err = subs.List(SubscriptionFilter{})
	if err != nil {
		t.Fatalf("list after delete: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("expected empty list, got %+v", list)
	}
}

func TestSubscriptionValidation(t *testing.T) {
	subs, _, _ := newTestServices(t)

	if _, err := subs.Create(SubscriptionInput{
		Name:         "",
		Status:       "active",
		Price:        Money{AmountMinor: 100, Currency: "USD", Scale: 2},
		IntervalUnit: "month",
	}); err == nil {
		t.Fatal("expected missing name to be rejected")
	}

	if _, err := subs.Create(SubscriptionInput{
		Name:         "Bad interval",
		Status:       "active",
		Price:        Money{AmountMinor: 100, Currency: "USD", Scale: 2},
		IntervalUnit: "fortnight",
	}); err == nil {
		t.Fatal("expected invalid interval unit to be rejected")
	}

	if _, err := subs.Create(SubscriptionInput{
		Name:         "Bad status",
		Status:       "zombie",
		Price:        Money{AmountMinor: 100, Currency: "USD", Scale: 2},
		IntervalUnit: "month",
	}); err == nil {
		t.Fatal("expected invalid status to be rejected")
	}
}

func TestPaymentMethodNormalisation(t *testing.T) {
	_, methods, _ := newTestServices(t)

	pm, err := methods.Create(PaymentMethodInput{
		Label: "  Crypto wallet  ",
		Type:  "crypto",
		Chain: "Ethereum",
		// Only the last four digits must survive.
		Last4:  "0x1234567890abcdef",
		Active: true,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if pm.Label != "Crypto wallet" {
		t.Fatalf("label = %q", pm.Label)
	}
	if pm.Last4 != "cdef" {
		t.Fatalf("last4 = %q, want cdef", pm.Last4)
	}

	if _, err := methods.Create(PaymentMethodInput{Label: "", Type: "card"}); err == nil {
		t.Fatal("expected missing label to be rejected")
	}
	if _, err := methods.Create(PaymentMethodInput{Label: "X", Type: "spaceship"}); err == nil {
		t.Fatal("expected invalid type to be rejected")
	}

	archived, err := methods.SetActive(pm.ID, false)
	if err != nil {
		t.Fatalf("archive: %v", err)
	}
	if archived.Active {
		t.Fatal("payment method should be archived")
	}

	activeOnly, err := methods.List(false)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(activeOnly) != 0 {
		t.Fatalf("active-only list = %+v", activeOnly)
	}

	if err := methods.Delete(pm.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
}

func TestUpcomingUsesBillingInterval(t *testing.T) {
	subs, _, _ := newTestServices(t)

	today := domain.Today()
	created, err := subs.Create(SubscriptionInput{
		Name:          "Weekly thing",
		Status:        "active",
		Price:         Money{AmountMinor: 500, Currency: "USD", Scale: 2},
		IntervalCount: 1,
		IntervalUnit:  "week",
		AnchorDate:    today.AddDays(-1).String(),
		AutoRenew:     true,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.NextRenewal != today.AddDays(6).String() {
		t.Fatalf("next renewal = %s, want %s", created.NextRenewal, today.AddDays(6))
	}

	upcoming, err := subs.Upcoming(30)
	if err != nil {
		t.Fatalf("upcoming: %v", err)
	}
	if len(upcoming) != 1 || upcoming[0].ID != created.ID {
		t.Fatalf("upcoming = %+v", upcoming)
	}
}
