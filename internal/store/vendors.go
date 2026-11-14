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

const vendorColumns = `id, name, category, domains, aliases, logo_url, created_at, updated_at`

// ListVendors returns all vendors sorted by name.
func (s *Store) ListVendors(ctx context.Context) ([]domain.Vendor, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT "+vendorColumns+" FROM vendors ORDER BY name COLLATE NOCASE")
	if err != nil {
		return nil, fmt.Errorf("list vendors: %w", err)
	}
	defer rows.Close()

	vendors := make([]domain.Vendor, 0, 16)
	for rows.Next() {
		v, err := scanVendor(rows)
		if err != nil {
			return nil, err
		}
		vendors = append(vendors, v)
	}
	return vendors, rows.Err()
}

// GetVendor loads a single vendor by ID.
func (s *Store) GetVendor(ctx context.Context, id string) (domain.Vendor, error) {
	row := s.db.QueryRowContext(ctx, "SELECT "+vendorColumns+" FROM vendors WHERE id = ?", id)
	v, err := scanVendor(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Vendor{}, ErrNotFound
	}
	return v, err
}

// FindVendorByName looks a vendor up case-insensitively.
func (s *Store) FindVendorByName(ctx context.Context, name string) (domain.Vendor, error) {
	row := s.db.QueryRowContext(ctx,
		"SELECT "+vendorColumns+" FROM vendors WHERE name = ? COLLATE NOCASE",
		strings.TrimSpace(name))
	v, err := scanVendor(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Vendor{}, ErrNotFound
	}
	return v, err
}

// CreateVendor inserts v, assigning an ID and timestamps when missing.
func (s *Store) CreateVendor(ctx context.Context, v *domain.Vendor) error {
	if v.ID == "" {
		v.ID = domain.NewID()
	}
	now := time.Now()
	if v.CreatedAt.IsZero() {
		v.CreatedAt = now
	}
	v.UpdatedAt = now

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO vendors (`+vendorColumns+`) VALUES (?,?,?,?,?,?,?,?)`,
		v.ID, v.Name, v.Category, jsonList(v.Domains), jsonList(v.Aliases), v.LogoURL,
		formatTime(v.CreatedAt), formatTime(v.UpdatedAt),
	)
	if err != nil {
		if isUniqueViolation(err) {
			return fmt.Errorf("vendor %q already exists", v.Name)
		}
		return fmt.Errorf("create vendor: %w", err)
	}
	return nil
}

// UpdateVendor replaces the mutable fields of an existing vendor.
func (s *Store) UpdateVendor(ctx context.Context, v *domain.Vendor) error {
	v.UpdatedAt = time.Now()

	res, err := s.db.ExecContext(ctx,
		`UPDATE vendors SET name = ?, category = ?, domains = ?, aliases = ?, logo_url = ?, updated_at = ?
		 WHERE id = ?`,
		v.Name, v.Category, jsonList(v.Domains), jsonList(v.Aliases), v.LogoURL,
		formatTime(v.UpdatedAt), v.ID,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return fmt.Errorf("vendor %q already exists", v.Name)
		}
		return fmt.Errorf("update vendor: %w", err)
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

// DeleteVendor removes a vendor. Subscriptions referencing it have their
// reference cleared by the ON DELETE SET NULL foreign key.
func (s *Store) DeleteVendor(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM vendors WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete vendor: %w", err)
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

func scanVendor(sc interface{ Scan(dest ...any) error }) (domain.Vendor, error) {
	var (
		v         domain.Vendor
		domains   string
		aliases   string
		createdAt string
		updatedAt string
	)
	err := sc.Scan(
		&v.ID, &v.Name, &v.Category, &domains, &aliases, &v.LogoURL,
		&createdAt, &updatedAt,
	)
	if err != nil {
		return domain.Vendor{}, err
	}

	v.Domains = parseList(domains)
	v.Aliases = parseList(aliases)
	v.CreatedAt = parseTime(createdAt)
	v.UpdatedAt = parseTime(updatedAt)
	return v, nil
}

func isUniqueViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}
