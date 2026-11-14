package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"substrackt/internal/domain"
)

const subscriptionColumns = `id, vendor_id, name, plan, status, amount_minor, currency, scale,
	interval_count, interval_unit, anchor_date, next_renewal, started_at, trial_ends_at,
	cancelled_at, auto_renew, payment_method_id, source_id, external_id, confidence, tags,
	notes, created_at, updated_at`

// SubscriptionFilter narrows a subscription listing.
type SubscriptionFilter struct {
	Statuses        []domain.Status
	VendorID        string
	PaymentMethodID string
	Search          string
	Limit           int
}

const defaultSubscriptionLimit = 500

// ListSubscriptions returns subscriptions matching the filter, ordered by
// status (billable first) and next renewal.
func (s *Store) ListSubscriptions(ctx context.Context, f SubscriptionFilter) ([]domain.Subscription, error) {
	var (
		where []string
		args  []any
	)

	if len(f.Statuses) > 0 {
		placeholders := make([]string, len(f.Statuses))
		for i, st := range f.Statuses {
			placeholders[i] = "?"
			args = append(args, string(st))
		}
		where = append(where, "status IN ("+strings.Join(placeholders, ",")+")")
	}
	if f.VendorID != "" {
		where = append(where, "vendor_id = ?")
		args = append(args, f.VendorID)
	}
	if f.PaymentMethodID != "" {
		where = append(where, "payment_method_id = ?")
		args = append(args, f.PaymentMethodID)
	}
	if search := strings.TrimSpace(f.Search); search != "" {
		where = append(where, "(LOWER(name) LIKE ? OR LOWER(plan) LIKE ?)")
		like := "%" + strings.ToLower(search) + "%"
		args = append(args, like, like)
	}

	query := "SELECT " + subscriptionColumns + " FROM subscriptions"
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	query += ` ORDER BY
		CASE status
			WHEN 'active' THEN 0
			WHEN 'trial' THEN 1
			WHEN 'paused' THEN 2
			WHEN 'cancelled' THEN 3
			ELSE 4
		END,
		CASE WHEN next_renewal = '' THEN 1 ELSE 0 END,
		next_renewal,
		name COLLATE NOCASE`

	limit := f.Limit
	if limit <= 0 {
		limit = defaultSubscriptionLimit
	}
	query += " LIMIT ?"
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list subscriptions: %w", err)
	}
	defer rows.Close()

	subs := make([]domain.Subscription, 0, 32)
	for rows.Next() {
		sub, err := scanSubscription(rows)
		if err != nil {
			return nil, err
		}
		subs = append(subs, sub)
	}
	return subs, rows.Err()
}

// UpcomingRenewals returns billable subscriptions renewing within the next
// days days (inclusive of from).
func (s *Store) UpcomingRenewals(ctx context.Context, from domain.Date, days int) ([]domain.Subscription, error) {
	until := from.AddDays(days)
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+subscriptionColumns+` FROM subscriptions
		 WHERE status IN ('active','trial')
		   AND next_renewal <> ''
		   AND next_renewal >= ? AND next_renewal <= ?
		 ORDER BY next_renewal, name COLLATE NOCASE`,
		from.String(), until.String(),
	)
	if err != nil {
		return nil, fmt.Errorf("upcoming renewals: %w", err)
	}
	defer rows.Close()

	subs := make([]domain.Subscription, 0, 16)
	for rows.Next() {
		sub, err := scanSubscription(rows)
		if err != nil {
			return nil, err
		}
		subs = append(subs, sub)
	}
	return subs, rows.Err()
}

// GetSubscription loads a single subscription by ID.
func (s *Store) GetSubscription(ctx context.Context, id string) (domain.Subscription, error) {
	row := s.db.QueryRowContext(ctx, "SELECT "+subscriptionColumns+" FROM subscriptions WHERE id = ?", id)
	sub, err := scanSubscription(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Subscription{}, ErrNotFound
	}
	return sub, err
}

// CreateSubscription inserts sub, assigning an ID and timestamps when missing.
func (s *Store) CreateSubscription(ctx context.Context, sub *domain.Subscription) error {
	if sub.ID == "" {
		sub.ID = domain.NewID()
	}
	now := time.Now()
	if sub.CreatedAt.IsZero() {
		sub.CreatedAt = now
	}
	sub.UpdatedAt = now

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO subscriptions (`+subscriptionColumns+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		sub.ID, nullString(sub.VendorID), sub.Name, sub.Plan, string(sub.Status),
		sub.Price.AmountMinor, sub.Price.Currency, sub.Price.Scale,
		sub.Interval.Count, string(sub.Interval.Unit),
		formatDate(sub.AnchorDate), formatDate(sub.NextRenewal),
		formatDatePtr(sub.StartedAt), formatDatePtr(sub.TrialEndsAt), formatDatePtr(sub.CancelledAt),
		boolInt(sub.AutoRenew), nullString(sub.PaymentMethodID), sub.SourceID, sub.ExternalID,
		sub.Confidence, jsonList(sub.Tags), sub.Notes,
		formatTime(sub.CreatedAt), formatTime(sub.UpdatedAt),
	)
	if err != nil {
		return fmt.Errorf("create subscription: %w", err)
	}
	return nil
}

// UpdateSubscription replaces the mutable fields of an existing subscription.
func (s *Store) UpdateSubscription(ctx context.Context, sub *domain.Subscription) error {
	sub.UpdatedAt = time.Now()

	res, err := s.db.ExecContext(ctx,
		`UPDATE subscriptions SET
			vendor_id = ?, name = ?, plan = ?, status = ?, amount_minor = ?, currency = ?,
			scale = ?, interval_count = ?, interval_unit = ?, anchor_date = ?, next_renewal = ?,
			started_at = ?, trial_ends_at = ?, cancelled_at = ?, auto_renew = ?,
			payment_method_id = ?, source_id = ?, external_id = ?, confidence = ?, tags = ?,
			notes = ?, updated_at = ?
		 WHERE id = ?`,
		nullString(sub.VendorID), sub.Name, sub.Plan, string(sub.Status),
		sub.Price.AmountMinor, sub.Price.Currency, sub.Price.Scale,
		sub.Interval.Count, string(sub.Interval.Unit),
		formatDate(sub.AnchorDate), formatDate(sub.NextRenewal),
		formatDatePtr(sub.StartedAt), formatDatePtr(sub.TrialEndsAt), formatDatePtr(sub.CancelledAt),
		boolInt(sub.AutoRenew), nullString(sub.PaymentMethodID), sub.SourceID, sub.ExternalID,
		sub.Confidence, jsonList(sub.Tags), sub.Notes, formatTime(sub.UpdatedAt),
		sub.ID,
	)
	if err != nil {
		return fmt.Errorf("update subscription: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteSubscription removes a subscription permanently.
func (s *Store) DeleteSubscription(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM subscriptions WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete subscription: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

// CountSubscriptionsByStatus returns how many subscriptions are in each status.
func (s *Store) CountSubscriptionsByStatus(ctx context.Context) (map[domain.Status]int, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT status, COUNT(*) FROM subscriptions GROUP BY status`)
	if err != nil {
		return nil, fmt.Errorf("count subscriptions: %w", err)
	}
	defer rows.Close()

	counts := make(map[domain.Status]int)
	for rows.Next() {
		var (
			status string
			count  int
		)
		if err := rows.Scan(&status, &count); err != nil {
			return nil, err
		}
		counts[domain.Status(status)] = count
	}
	return counts, rows.Err()
}

func scanSubscription(sc interface{ Scan(dest ...any) error }) (domain.Subscription, error) {
	var (
		sub        domain.Subscription
		vendorID   sql.NullString
		pmID       sql.NullString
		status     string
		unit       string
		anchor     string
		next       string
		started    string
		trial      string
		cancelled  string
		autoRenew  int
		tags       string
		createdAt  string
		updatedAt  string
		confidence float64
	)
	err := sc.Scan(
		&sub.ID, &vendorID, &sub.Name, &sub.Plan, &status,
		&sub.Price.AmountMinor, &sub.Price.Currency, &sub.Price.Scale,
		&sub.Interval.Count, &unit, &anchor, &next, &started, &trial, &cancelled,
		&autoRenew, &pmID, &sub.SourceID, &sub.ExternalID, &confidence,
		&tags, &sub.Notes, &createdAt, &updatedAt,
	)
	if err != nil {
		return domain.Subscription{}, err
	}

	sub.VendorID = vendorID.String
	sub.PaymentMethodID = pmID.String
	sub.Status = domain.Status(status)
	sub.Interval.Unit = domain.Unit(unit)
	sub.AutoRenew = autoRenew != 0
	sub.Confidence = confidence
	sub.Tags = parseList(tags)
	sub.CreatedAt = parseTime(createdAt)
	sub.UpdatedAt = parseTime(updatedAt)
	sub.StartedAt = parseDatePtr(started)
	sub.TrialEndsAt = parseDatePtr(trial)
	sub.CancelledAt = parseDatePtr(cancelled)

	if anchor != "" {
		d, err := domain.ParseDate(anchor)
		if err != nil {
			return domain.Subscription{}, fmt.Errorf("subscription %s: %w", sub.ID, err)
		}
		sub.AnchorDate = d
	}
	if next != "" {
		d, err := domain.ParseDate(next)
		if err != nil {
			return domain.Subscription{}, fmt.Errorf("subscription %s: %w", sub.ID, err)
		}
		sub.NextRenewal = d
	}
	return sub, nil
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
