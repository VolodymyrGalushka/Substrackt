package domain

import (
	"encoding/json"
	"testing"
)

func TestIntervalNextRenewal(t *testing.T) {
	tests := []struct {
		name   string
		anchor string
		iv     Interval
		from   string
		want   string
	}{
		{"anchor in future", "2026-01-15", Interval{1, UnitMonth}, "2026-01-01", "2026-01-15"},
		{"monthly rollover", "2026-01-15", Interval{1, UnitMonth}, "2026-03-01", "2026-03-15"},
		{"month end clamps", "2026-01-31", Interval{1, UnitMonth}, "2026-02-01", "2026-02-28"},
		{"leap day yearly", "2024-02-29", Interval{1, UnitYear}, "2025-01-01", "2025-02-28"},
		{"quarterly", "2026-01-10", Interval{3, UnitMonth}, "2026-04-01", "2026-04-10"},
		{"weekly", "2026-01-01", Interval{1, UnitWeek}, "2026-01-20", "2026-01-22"},
		{"daily", "2026-01-01", Interval{3, UnitDay}, "2026-01-10", "2026-01-10"},
		{"exactly today", "2026-01-10", Interval{1, UnitMonth}, "2026-01-10", "2026-01-10"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			anchor := mustDate(t, tc.anchor)
			from := mustDate(t, tc.from)
			got := tc.iv.NextRenewal(anchor, from)
			if got.String() != tc.want {
				t.Fatalf("NextRenewal(%s, %s) = %s, want %s", tc.anchor, tc.from, got, tc.want)
			}
		})
	}
}

func TestMonthEndDoesNotDrift(t *testing.T) {
	// A subscription anchored on the 31st must not creep backwards in later
	// months: Mar 31 -> Apr 30 -> May 31.
	anchor := mustDate(t, "2026-01-31")
	iv := Interval{1, UnitMonth}
	got := iv.NextRenewal(anchor, mustDate(t, "2026-04-01"))
	if got.String() != "2026-04-30" {
		t.Fatalf("April renewal = %s, want 2026-04-30", got)
	}
}

func TestMoneyPerMonth(t *testing.T) {
	tests := []struct {
		name string
		m    Money
		iv   Interval
		want int64
	}{
		{"monthly passthrough", NewMoney(1299, "usd", 2), Interval{1, UnitMonth}, 1299},
		{"yearly", NewMoney(11988, "usd", 2), Interval{1, UnitYear}, 999},
		{"quarterly", NewMoney(3000, "usd", 2), Interval{3, UnitMonth}, 1000},
		{"weekly", NewMoney(1000, "usd", 2), Interval{1, UnitWeek}, 4333},
		{"daily", NewMoney(100, "usd", 2), Interval{1, UnitDay}, 3042},
		{"every 2 years", NewMoney(24000, "usd", 2), Interval{2, UnitYear}, 1000},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.m.PerMonth(tc.iv)
			if got.AmountMinor != tc.want {
				t.Fatalf("PerMonth = %d, want %d", got.AmountMinor, tc.want)
			}
			if got.Currency != tc.m.Currency || got.Scale != tc.m.Scale {
				t.Fatalf("PerMonth changed currency/scale: %+v", got)
			}
		})
	}
}

func TestMoneyAddRejectsMismatch(t *testing.T) {
	if _, err := NewMoney(100, "usd", 2).Add(NewMoney(100, "eur", 2)); err == nil {
		t.Fatal("expected error adding USD to EUR")
	}
	sum, err := NewMoney(100, "usd", 2).Add(NewMoney(250, "usd", 2))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sum.AmountMinor != 350 {
		t.Fatalf("sum = %d, want 350", sum.AmountMinor)
	}
}

func TestDivRoundNegatives(t *testing.T) {
	if got := divRound(-1299, 12); got != -108 {
		t.Fatalf("divRound(-1299, 12) = %d, want -108", got)
	}
	if got := divRound(-6, 12); got != -1 {
		t.Fatalf("divRound(-6, 12) = %d, want -1", got)
	}
}

func TestDateJSON(t *testing.T) {
	d := mustDate(t, "2026-07-04")
	b, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `"2026-07-04"` {
		t.Fatalf("marshal = %s", b)
	}

	var back Date
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if !back.Equal(d) {
		t.Fatalf("roundtrip = %s, want %s", back, d)
	}

	var zero Date
	if err := json.Unmarshal([]byte(`""`), &zero); err != nil {
		t.Fatal(err)
	}
	if !zero.IsZero() {
		t.Fatalf("empty string should decode to zero date, got %s", zero)
	}
}

func TestDateAddMonthsClamp(t *testing.T) {
	if got := mustDate(t, "2026-01-31").AddMonths(1).String(); got != "2026-02-28" {
		t.Fatalf("Jan 31 + 1 month = %s, want 2026-02-28", got)
	}
	if got := mustDate(t, "2024-01-31").AddMonths(1).String(); got != "2024-02-29" {
		t.Fatalf("leap Feb = %s, want 2024-02-29", got)
	}
	if got := mustDate(t, "2026-12-15").AddMonths(1).String(); got != "2027-01-15" {
		t.Fatalf("year rollover = %s, want 2027-01-15", got)
	}
}

func TestSubscriptionValidate(t *testing.T) {
	valid := Subscription{
		Name:       "Netflix",
		Status:     StatusActive,
		Price:      NewMoney(1299, "usd", 2),
		Interval:   Interval{1, UnitMonth},
		AnchorDate: mustDate(t, "2026-01-01"),
		AutoRenew:  true,
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid subscription rejected: %v", err)
	}

	bad := valid
	bad.Status = "bogus"
	if err := bad.Validate(); err == nil {
		t.Fatal("expected invalid status to be rejected")
	}

	bad = valid
	bad.Interval.Count = 0
	if err := bad.Validate(); err == nil {
		t.Fatal("expected zero interval count to be rejected")
	}
}

func TestNewID(t *testing.T) {
	a := NewID()
	b := NewID()
	if len(a) != 26 || len(b) != 26 {
		t.Fatalf("ID length = %d/%d, want 26", len(a), len(b))
	}
	if a == b {
		t.Fatal("IDs must be unique")
	}
	if a >= b {
		t.Fatalf("IDs are not roughly time-sortable: %s >= %s", a, b)
	}
	for _, r := range a {
		if !containsRune(ulidAlphabet, r) {
			t.Fatalf("ID %q contains invalid character %q", a, r)
		}
	}
}

func containsRune(s string, r rune) bool {
	for _, c := range s {
		if c == r {
			return true
		}
	}
	return false
}

func mustDate(t *testing.T, s string) Date {
	t.Helper()
	d, err := ParseDate(s)
	if err != nil {
		t.Fatal(err)
	}
	return d
}
