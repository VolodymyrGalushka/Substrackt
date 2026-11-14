package domain

import (
	"fmt"
	"strings"
)

// Money is an amount in minor units of a currency. Scale is the number of
// decimal digits the currency uses (2 for USD, 8 for BTC).
type Money struct {
	AmountMinor int64
	Currency    string
	Scale       int
}

// NewMoney builds a Money value.
func NewMoney(amountMinor int64, currency string, scale int) Money {
	return Money{AmountMinor: amountMinor, Currency: strings.ToUpper(strings.TrimSpace(currency)), Scale: scale}
}

func (m Money) IsZero() bool { return m.AmountMinor == 0 }

// Valid checks that the currency and scale are usable.
func (m Money) Valid() error {
	if strings.TrimSpace(m.Currency) == "" {
		return fmt.Errorf("currency is required")
	}
	if len(m.Currency) > 10 {
		return fmt.Errorf("currency %q is too long", m.Currency)
	}
	if m.Scale < 0 || m.Scale > 8 {
		return fmt.Errorf("currency scale %d out of range 0-8", m.Scale)
	}
	return nil
}

// PerMonth converts an amount billed every iv into a monthly equivalent, in the
// same currency. Rounding is half away from zero.
func (m Money) PerMonth(iv Interval) Money {
	out := m
	if iv.Count <= 0 {
		return out
	}
	n := int64(iv.Count)
	switch iv.Unit {
	case UnitDay:
		out.AmountMinor = divRound(m.AmountMinor*365, n*12)
	case UnitWeek:
		out.AmountMinor = divRound(m.AmountMinor*52, n*12)
	case UnitMonth:
		out.AmountMinor = divRound(m.AmountMinor, n)
	case UnitYear:
		out.AmountMinor = divRound(m.AmountMinor, n*12)
	}
	return out
}

// PerYear converts an amount billed every iv into a yearly equivalent.
func (m Money) PerYear(iv Interval) Money {
	monthly := m.PerMonth(iv)
	monthly.AmountMinor *= 12
	return monthly
}

// Add returns m + o. Both amounts must share a currency and scale.
func (m Money) Add(o Money) (Money, error) {
	if m.Currency != o.Currency || m.Scale != o.Scale {
		return Money{}, fmt.Errorf("cannot add %s/%d to %s/%d", o.Currency, o.Scale, m.Currency, m.Scale)
	}
	out := m
	out.AmountMinor += o.AmountMinor
	return out, nil
}

// divRound divides n by d rounding half away from zero, without floats.
func divRound(n, d int64) int64 {
	if d == 0 {
		return 0
	}
	if n < 0 {
		return -((-n + d/2) / d)
	}
	return (n + d/2) / d
}
