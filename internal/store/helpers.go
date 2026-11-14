package store

import (
	"encoding/json"
	"time"

	"substrackt/internal/domain"
)

const timeLayout = time.RFC3339Nano

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(timeLayout)
}

func parseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(timeLayout, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

func formatDate(d domain.Date) string {
	if d.IsZero() {
		return ""
	}
	return d.String()
}

func formatDatePtr(d *domain.Date) string {
	if d == nil || d.IsZero() {
		return ""
	}
	return d.String()
}

func parseDatePtr(s string) *domain.Date {
	if s == "" {
		return nil
	}
	d, err := domain.ParseDate(s)
	if err != nil {
		return nil
	}
	return &d
}

func jsonList(v []string) string {
	if len(v) == 0 {
		return "[]"
	}
	b, err := json.Marshal(v)
	if err != nil {
		return "[]"
	}
	return string(b)
}

// nullString maps "" to SQL NULL so optional foreign keys stay valid.
func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func parseList(s string) []string {
	out := []string{}
	if s == "" {
		return out
	}
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return []string{}
	}
	if out == nil {
		return []string{}
	}
	return out
}
