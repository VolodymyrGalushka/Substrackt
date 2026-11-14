package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"substrackt/internal/domain"
)

const paymentMethodColumns = `id, label, type, issuer, network, last4, exp_month, exp_year,
	wallet_address, chain, billing_currency, active, notes, created_at, updated_at`

// ListPaymentMethods returns all payment methods, newest first. Archived
// methods are included only when includeInactive is true.
func (s *Store) ListPaymentMethods(ctx context.Context, includeInactive bool) ([]domain.PaymentMethod, error) {
	query := "SELECT " + paymentMethodColumns + " FROM payment_methods"
	if !includeInactive {
		query += " WHERE active = 1"
	}
	query += " ORDER BY active DESC, label COLLATE NOCASE"

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list payment methods: %w", err)
	}
	defer rows.Close()

	methods := make([]domain.PaymentMethod, 0, 8)
	for rows.Next() {
		pm, err := scanPaymentMethod(rows)
		if err != nil {
			return nil, err
		}
		methods = append(methods, pm)
	}
	return methods, rows.Err()
}

// GetPaymentMethod loads a single payment method by ID.
func (s *Store) GetPaymentMethod(ctx context.Context, id string) (domain.PaymentMethod, error) {
	row := s.db.QueryRowContext(ctx, "SELECT "+paymentMethodColumns+" FROM payment_methods WHERE id = ?", id)
	pm, err := scanPaymentMethod(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.PaymentMethod{}, ErrNotFound
	}
	return pm, err
}

// CreatePaymentMethod inserts pm, assigning an ID and timestamps when missing.
func (s *Store) CreatePaymentMethod(ctx context.Context, pm *domain.PaymentMethod) error {
	if pm.ID == "" {
		pm.ID = domain.NewID()
	}
	now := time.Now()
	if pm.CreatedAt.IsZero() {
		pm.CreatedAt = now
	}
	pm.UpdatedAt = now

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO payment_methods (`+paymentMethodColumns+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		pm.ID, pm.Label, string(pm.Type), pm.Issuer, pm.Network, pm.Last4,
		pm.ExpMonth, pm.ExpYear, pm.WalletAddress, pm.Chain, pm.BillingCurrency,
		boolInt(pm.Active), pm.Notes, formatTime(pm.CreatedAt), formatTime(pm.UpdatedAt),
	)
	if err != nil {
		return fmt.Errorf("create payment method: %w", err)
	}
	return nil
}

// UpdatePaymentMethod replaces the mutable fields of an existing method.
func (s *Store) UpdatePaymentMethod(ctx context.Context, pm *domain.PaymentMethod) error {
	pm.UpdatedAt = time.Now()

	res, err := s.db.ExecContext(ctx,
		`UPDATE payment_methods SET
			label = ?, type = ?, issuer = ?, network = ?, last4 = ?, exp_month = ?,
			exp_year = ?, wallet_address = ?, chain = ?, billing_currency = ?, active = ?,
			notes = ?, updated_at = ?
		 WHERE id = ?`,
		pm.Label, string(pm.Type), pm.Issuer, pm.Network, pm.Last4, pm.ExpMonth,
		pm.ExpYear, pm.WalletAddress, pm.Chain, pm.BillingCurrency, boolInt(pm.Active),
		pm.Notes, formatTime(pm.UpdatedAt), pm.ID,
	)
	if err != nil {
		return fmt.Errorf("update payment method: %w", err)
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

// DeletePaymentMethod removes a payment method. Subscriptions using it have
// their reference cleared by the ON DELETE SET NULL foreign key.
func (s *Store) DeletePaymentMethod(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM payment_methods WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete payment method: %w", err)
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

func scanPaymentMethod(sc interface{ Scan(dest ...any) error }) (domain.PaymentMethod, error) {
	var (
		pm        domain.PaymentMethod
		pmType    string
		active    int
		createdAt string
		updatedAt string
	)
	err := sc.Scan(
		&pm.ID, &pm.Label, &pmType, &pm.Issuer, &pm.Network, &pm.Last4,
		&pm.ExpMonth, &pm.ExpYear, &pm.WalletAddress, &pm.Chain, &pm.BillingCurrency,
		&active, &pm.Notes, &createdAt, &updatedAt,
	)
	if err != nil {
		return domain.PaymentMethod{}, err
	}

	pm.Type = domain.PaymentType(pmType)
	pm.Active = active != 0
	pm.CreatedAt = parseTime(createdAt)
	pm.UpdatedAt = parseTime(updatedAt)
	return pm, nil
}
