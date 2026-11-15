package ingest

import (
	"strings"
	"testing"
)

func TestRedactSensitive(t *testing.T) {
	input := "Card 4242 4242 4242 4242 expires 12/29, CVV: 123. Thanks!"
	got := RedactSensitive(input)
	if strings.Contains(got, "4242 4242 4242 4242") {
		t.Fatalf("card number was not redacted: %q", got)
	}
	if !strings.Contains(got, "4242]") {
		t.Fatalf("last four digits should remain visible: %q", got)
	}
	if strings.Contains(got, "CVV: 123") || strings.Contains(got, "cvv: 123") {
		t.Fatalf("CVV was not redacted: %q", got)
	}
}

func TestNormalizeText(t *testing.T) {
	input := "  First line  \r\n\r\n\r\nSecond   line\n"
	got := NormalizeText(input)
	if got != "First line\n\nSecond   line" {
		t.Fatalf("normalized = %q", got)
	}
	if ContentHash("abc") != ContentHash("abc") {
		t.Fatal("hash must be stable")
	}
	if ContentHash("abc") == ContentHash("abd") {
		t.Fatal("different content must hash differently")
	}
}

func TestHeuristicExtract(t *testing.T) {
	today := mustDate(t, "2026-11-15")
	tests := []struct {
		name     string
		content  string
		hint     string
		vendor   string
		amount   int64
		currency string
		interval int
		unit     string
		renewal  string
		status   string
		ok       bool
	}{
		{
			name:     "monthly receipt",
			content:  "Receipt from Netflix\nNetflix Standard\nTotal: $15.49 per month\nNext payment on December 1, 2026",
			vendor:   "Netflix",
			amount:   1549,
			currency: "USD",
			interval: 1,
			unit:     "month",
			renewal:  "2026-12-01",
			status:   "active",
			ok:       true,
		},
		{
			name:     "european decimal",
			content:  "Spotify AB\nPremium plan\n9,99 EUR per month",
			vendor:   "Spotify AB",
			amount:   999,
			currency: "EUR",
			interval: 1,
			unit:     "month",
			status:   "active",
			ok:       true,
		},
		{
			name:     "annual",
			content:  "Invoice from Acme Cloud\nBilled annually: $119.88\nRenews on 2027-02-01",
			vendor:   "Acme Cloud",
			amount:   11988,
			currency: "USD",
			interval: 1,
			unit:     "year",
			renewal:  "2027-02-01",
			status:   "active",
			ok:       true,
		},
		{
			name:     "quarterly",
			content:  "From: Figma <billing@figma.com>\nProfessional plan\n$45.00 every 3 months",
			vendor:   "Figma",
			amount:   4500,
			currency: "USD",
			interval: 3,
			unit:     "month",
			status:   "active",
			ok:       true,
		},
		{
			name:     "cancelled",
			content:  "From: Acme Cloud\nPlan $9.99 per month. Your subscription has been cancelled.",
			vendor:   "Acme Cloud",
			amount:   999,
			currency: "USD",
			interval: 1,
			unit:     "month",
			status:   "cancelled",
			ok:       true,
		},
		{
			name:    "no subscription",
			content: "Your parcel has shipped and will arrive on Tuesday.",
			ok:      false,
		},
		{
			name:     "vendor hint wins",
			content:  "Your receipt is attached. Total $5.00 per month.",
			hint:     "Example Co",
			vendor:   "Example Co",
			amount:   500,
			currency: "USD",
			interval: 1,
			unit:     "month",
			status:   "active",
			ok:       true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ex, ok := HeuristicExtract(tc.content, tc.hint, today, today, "USD")
			if ok != tc.ok {
				t.Fatalf("ok = %v, want %v (extract %+v)", ok, tc.ok, ex)
			}
			if !ok {
				return
			}
			if tc.vendor != "" && ex.VendorName != tc.vendor {
				t.Fatalf("vendor = %q, want %q", ex.VendorName, tc.vendor)
			}
			if tc.amount != 0 && ex.AmountMinor != tc.amount {
				t.Fatalf("amount = %d, want %d", ex.AmountMinor, tc.amount)
			}
			if ex.Currency != tc.currency {
				t.Fatalf("currency = %q, want %q", ex.Currency, tc.currency)
			}
			if ex.Interval.Count != tc.interval || string(ex.Interval.Unit) != tc.unit {
				t.Fatalf("interval = %+v, want %d %s", ex.Interval, tc.interval, tc.unit)
			}
			if tc.renewal != "" && ex.NextRenewal.String() != tc.renewal {
				t.Fatalf("renewal = %s, want %s", ex.NextRenewal, tc.renewal)
			}
			if string(ex.Status) != tc.status {
				t.Fatalf("status = %s, want %s", ex.Status, tc.status)
			}
			if ex.Confidence > 0.85 {
				t.Fatalf("heuristic confidence must stay below auto-apply: %v", ex.Confidence)
			}
		})
	}
}

func TestParseAmount(t *testing.T) {
	tests := []struct {
		raw   string
		scale int
		want  int64
		ok    bool
	}{
		{"12.99", 2, 1299, true},
		{"1,234.56", 2, 123456, true},
		{"1.234,56", 2, 123456, true},
		{"1,234", 2, 123400, true},
		{"12,5", 2, 1250, true},
		{"1200", 0, 1200, true},
		{"0.00000001", 8, 1, true},
		{"", 2, 0, false},
		{"abc", 2, 0, false},
	}
	for _, tc := range tests {
		got, ok := parseAmount(tc.raw, tc.scale)
		if ok != tc.ok || (ok && got != tc.want) {
			t.Fatalf("parseAmount(%q, %d) = %d,%v want %d,%v", tc.raw, tc.scale, got, ok, tc.want, tc.ok)
		}
	}
}
