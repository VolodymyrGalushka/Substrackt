package domain

import "fmt"

// Unit is a billing interval unit.
type Unit string

const (
	UnitDay   Unit = "day"
	UnitWeek  Unit = "week"
	UnitMonth Unit = "month"
	UnitYear  Unit = "year"
)

func (u Unit) Valid() bool {
	switch u {
	case UnitDay, UnitWeek, UnitMonth, UnitYear:
		return true
	}
	return false
}

// Interval is a billing cadence: every Count units.
type Interval struct {
	Count int
	Unit  Unit
}

func (iv Interval) Valid() error {
	if iv.Count <= 0 {
		return fmt.Errorf("billing interval count must be positive")
	}
	if !iv.Unit.Valid() {
		return fmt.Errorf("invalid billing interval unit %q", iv.Unit)
	}
	return nil
}

// Add returns d advanced by one interval, clamping month-end dates.
func (iv Interval) Add(d Date) Date {
	if iv.Count <= 0 {
		return d
	}
	switch iv.Unit {
	case UnitDay:
		return d.AddDays(iv.Count)
	case UnitWeek:
		return d.AddDays(iv.Count * 7)
	case UnitMonth:
		return d.AddMonths(iv.Count)
	case UnitYear:
		return d.AddMonths(iv.Count * 12)
	}
	return d
}

// NextRenewal returns the first period boundary on or after from, starting from
// anchor. Boundaries are always computed from the original anchor date, so a
// subscription anchored on Jan 31 renews on Feb 28, Mar 31, Apr 30 (no drift).
func (iv Interval) NextRenewal(anchor, from Date) Date {
	if anchor.IsZero() || iv.Count <= 0 || !iv.Unit.Valid() {
		return anchor
	}
	if !anchor.Before(from) {
		return anchor
	}

	switch iv.Unit {
	case UnitMonth, UnitYear:
		step := iv.Count
		if iv.Unit == UnitYear {
			step = iv.Count * 12
		}
		monthsBehind := (from.Year-anchor.Year)*12 + int(from.Month) - int(anchor.Month)
		k := monthsBehind / step
		if k < 0 {
			k = 0
		}
		d := anchor.AddMonths(k * step)
		for d.Before(from) {
			k++
			d = anchor.AddMonths(k * step)
		}
		return d
	case UnitDay, UnitWeek:
		stepDays := iv.Count
		if iv.Unit == UnitWeek {
			stepDays = iv.Count * 7
		}
		days := daysBetween(anchor, from)
		k := (days + stepDays - 1) / stepDays
		return anchor.AddDays(k * stepDays)
	}
	return anchor
}

// daysBetween returns whole days from a to b (negative when b precedes a).
func daysBetween(a, b Date) int {
	return int(b.Time().Sub(a.Time()).Hours() / 24)
}

// Label renders the interval in a compact human form, e.g. "every 3 months".
func (iv Interval) Label() string {
	switch {
	case iv.Count == 1:
		switch iv.Unit {
		case UnitDay:
			return "daily"
		case UnitWeek:
			return "weekly"
		case UnitMonth:
			return "monthly"
		case UnitYear:
			return "yearly"
		}
		return fmt.Sprintf("every %d %ss", iv.Count, iv.Unit)
	}
	return fmt.Sprintf("every %d %ss", iv.Count, iv.Unit)
}
