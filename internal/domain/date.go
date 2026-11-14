package domain

import (
	"encoding/json"
	"fmt"
	"time"
)

// Date is a calendar date without a time zone. Subscription billing periods are
// calendar-based, so storing instants would introduce off-by-one-day bugs.
type Date struct {
	Year  int
	Month time.Month
	Day   int
}

// NewDate returns the calendar date of t in t's location.
func NewDate(t time.Time) Date {
	y, m, d := t.Date()
	return Date{Year: y, Month: m, Day: d}
}

// Today returns the current local calendar date.
func Today() Date { return NewDate(time.Now()) }

// ParseDate parses a YYYY-MM-DD string.
func ParseDate(s string) (Date, error) {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return Date{}, fmt.Errorf("invalid date %q: %w", s, err)
	}
	return NewDate(t), nil
}

func (d Date) String() string {
	return fmt.Sprintf("%04d-%02d-%02d", d.Year, int(d.Month), d.Day)
}

// IsZero reports whether d is the zero date.
func (d Date) IsZero() bool { return d.Year == 0 && d.Month == 0 && d.Day == 0 }

// Time returns midnight UTC for d.
func (d Date) Time() time.Time {
	return time.Date(d.Year, d.Month, d.Day, 0, 0, 0, 0, time.UTC)
}

// Compare returns -1, 0 or 1.
func (d Date) Compare(o Date) int {
	switch {
	case d.Year != o.Year:
		if d.Year < o.Year {
			return -1
		}
		return 1
	case d.Month != o.Month:
		if d.Month < o.Month {
			return -1
		}
		return 1
	case d.Day != o.Day:
		if d.Day < o.Day {
			return -1
		}
		return 1
	}
	return 0
}

func (d Date) Before(o Date) bool { return d.Compare(o) < 0 }
func (d Date) After(o Date) bool  { return d.Compare(o) > 0 }
func (d Date) Equal(o Date) bool  { return d.Compare(o) == 0 }

// AddDays returns d shifted by n days.
func (d Date) AddDays(n int) Date {
	return NewDate(d.Time().AddDate(0, 0, n))
}

// AddMonths returns d shifted by n months, clamping the day to the last day of
// the target month (Jan 31 + 1 month = Feb 28/29).
func (d Date) AddMonths(n int) Date {
	y := d.Year
	m := int(d.Month) - 1 + n
	y += m / 12
	m = m % 12
	if m < 0 {
		m += 12
		y--
	}
	month := time.Month(m + 1)
	day := d.Day
	if last := daysIn(y, month); day > last {
		day = last
	}
	return Date{Year: y, Month: month, Day: day}
}

func daysIn(year int, month time.Month) int {
	return time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()
}

// MarshalJSON encodes the date as "YYYY-MM-DD"; the zero date encodes as "".
func (d Date) MarshalJSON() ([]byte, error) {
	if d.IsZero() {
		return []byte(`""`), nil
	}
	return json.Marshal(d.String())
}

// UnmarshalJSON accepts "YYYY-MM-DD" or "".
func (d *Date) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	if s == "" {
		*d = Date{}
		return nil
	}
	parsed, err := ParseDate(s)
	if err != nil {
		return err
	}
	*d = parsed
	return nil
}

// DatePtr returns a pointer to d, for optional model fields.
func DatePtr(d Date) *Date { return &d }
