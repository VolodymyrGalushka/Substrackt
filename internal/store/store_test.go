package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"substrackt/internal/domain"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func testSubscription(t *testing.T) domain.Subscription {
	t.Helper()
	return domain.Subscription{
		Name:       "Test Service",
		Plan:       "Pro",
		Status:     domain.StatusActive,
		Price:      domain.NewMoney(1299, "usd", 2),
		Interval:   domain.Interval{Count: 1, Unit: domain.UnitMonth},
		AnchorDate: mustDate(t, "2026-01-15"),
		AutoRenew:  true,
		Tags:       []string{"work"},
	}
}

func TestMigrateIsIdempotent(t *testing.T) {
	st := testStore(t)
	for i := 0; i < 3; i++ {
		if err := st.Migrate(context.Background()); err != nil {
			t.Fatalf("migrate run %d: %v", i, err)
		}
	}
}

func TestSubscriptionCRUD(t *testing.T) {
	ctx := context.Background()
	st := testStore(t)

	sub := testSubscription(t)
	sub.RecomputeNextRenewal(mustDate(t, "2026-01-01"))
	if err := st.CreateSubscription(ctx, &sub); err != nil {
		t.Fatalf("create: %v", err)
	}
	if sub.ID == "" {
		t.Fatal("create should assign an ID")
	}
	if sub.NextRenewal.String() != "2026-01-15" {
		t.Fatalf("next renewal = %s, want 2026-01-15", sub.NextRenewal)
	}

	got, err := st.GetSubscription(ctx, sub.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Name != sub.Name || got.Price.AmountMinor != 1299 || got.Price.Currency != "USD" {
		t.Fatalf("roundtrip mismatch: %+v", got)
	}
	if len(got.Tags) != 1 || got.Tags[0] != "work" {
		t.Fatalf("tags roundtrip = %v", got.Tags)
	}

	got.Plan = "Business"
	got.Status = domain.StatusPaused
	if err := st.UpdateSubscription(ctx, &got); err != nil {
		t.Fatalf("update: %v", err)
	}
	got2, err := st.GetSubscription(ctx, sub.ID)
	if err != nil {
		t.Fatalf("get after update: %v", err)
	}
	if got2.Plan != "Business" || got2.Status != domain.StatusPaused {
		t.Fatalf("update not persisted: %+v", got2)
	}

	if err := st.DeleteSubscription(ctx, sub.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := st.GetSubscription(ctx, sub.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestListFiltersAndUpcoming(t *testing.T) {
	ctx := context.Background()
	st := testStore(t)

	active := testSubscription(t)
	active.Name = "Active One"
	active.RecomputeNextRenewal(mustDate(t, "2026-01-01"))
	paused := testSubscription(t)
	paused.Name = "Paused One"
	paused.Status = domain.StatusPaused
	paused.RecomputeNextRenewal(mustDate(t, "2026-01-01"))
	cancelled := testSubscription(t)
	cancelled.Name = "Cancelled One"
	cancelled.Status = domain.StatusCancelled
	cancelled.NextRenewal = mustDate(t, "2026-01-15")

	for _, s := range []*domain.Subscription{&active, &paused, &cancelled} {
		if err := st.CreateSubscription(ctx, s); err != nil {
			t.Fatalf("create %s: %v", s.Name, err)
		}
	}

	all, err := st.ListSubscriptions(ctx, SubscriptionFilter{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("list returned %d, want 3", len(all))
	}
	if all[0].Name != "Active One" || all[1].Name != "Paused One" {
		t.Fatalf("unexpected ordering: %s, %s, %s", all[0].Name, all[1].Name, all[2].Name)
	}

	billable, err := st.ListSubscriptions(ctx, SubscriptionFilter{Statuses: []domain.Status{domain.StatusActive}})
	if err != nil {
		t.Fatalf("filter: %v", err)
	}
	if len(billable) != 1 || billable[0].Name != "Active One" {
		t.Fatalf("status filter wrong: %+v", billable)
	}

	found, err := st.ListSubscriptions(ctx, SubscriptionFilter{Search: "paused"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(found) != 1 || found[0].Name != "Paused One" {
		t.Fatalf("search filter wrong: %+v", found)
	}

	upcoming, err := st.UpcomingRenewals(ctx, mustDate(t, "2026-01-01"), 30)
	if err != nil {
		t.Fatalf("upcoming: %v", err)
	}
	if len(upcoming) != 1 || upcoming[0].Name != "Active One" {
		t.Fatalf("upcoming wrong: %+v", upcoming)
	}
}

func TestPaymentMethodDeleteClearsSubscriptionReference(t *testing.T) {
	ctx := context.Background()
	st := testStore(t)

	pm := domain.PaymentMethod{Label: "Visa •• 4242", Type: domain.PaymentCard, Last4: "4242", Active: true}
	if err := st.CreatePaymentMethod(ctx, &pm); err != nil {
		t.Fatalf("create payment method: %v", err)
	}

	sub := testSubscription(t)
	sub.PaymentMethodID = pm.ID
	sub.RecomputeNextRenewal(mustDate(t, "2026-01-01"))
	if err := st.CreateSubscription(ctx, &sub); err != nil {
		t.Fatalf("create subscription: %v", err)
	}

	if err := st.DeletePaymentMethod(ctx, pm.ID); err != nil {
		t.Fatalf("delete payment method: %v", err)
	}

	got, err := st.GetSubscription(ctx, sub.ID)
	if err != nil {
		t.Fatalf("get subscription: %v", err)
	}
	if got.PaymentMethodID != "" {
		t.Fatalf("payment method reference = %q, want empty", got.PaymentMethodID)
	}
}

func TestVendorUniquenessAndDelete(t *testing.T) {
	ctx := context.Background()
	st := testStore(t)

	v := domain.Vendor{Name: "Acme"}
	if err := st.CreateVendor(ctx, &v); err != nil {
		t.Fatalf("create vendor: %v", err)
	}

	dup := domain.Vendor{Name: "acme"}
	if err := st.CreateVendor(ctx, &dup); err == nil {
		t.Fatal("expected case-insensitive duplicate vendor to be rejected")
	}

	found, err := st.FindVendorByName(ctx, "ACME")
	if err != nil {
		t.Fatalf("find vendor: %v", err)
	}
	if found.ID != v.ID {
		t.Fatalf("found wrong vendor: %+v", found)
	}

	sub := testSubscription(t)
	sub.VendorID = v.ID
	sub.RecomputeNextRenewal(mustDate(t, "2026-01-01"))
	if err := st.CreateSubscription(ctx, &sub); err != nil {
		t.Fatalf("create subscription: %v", err)
	}

	if err := st.DeleteVendor(ctx, v.ID); err != nil {
		t.Fatalf("delete vendor: %v", err)
	}
	got, err := st.GetSubscription(ctx, sub.ID)
	if err != nil {
		t.Fatalf("get subscription: %v", err)
	}
	if got.VendorID != "" {
		t.Fatalf("vendor reference = %q, want empty", got.VendorID)
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
