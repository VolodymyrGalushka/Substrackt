package domain

import (
	"fmt"
	"strings"
	"time"
)

// Status is the lifecycle state of a subscription.
type Status string

const (
	StatusTrial     Status = "trial"
	StatusActive    Status = "active"
	StatusPaused    Status = "paused"
	StatusCancelled Status = "cancelled"
	StatusExpired   Status = "expired"
)

func (s Status) Valid() bool {
	switch s {
	case StatusTrial, StatusActive, StatusPaused, StatusCancelled, StatusExpired:
		return true
	}
	return false
}

// Billable reports whether the subscription still costs money.
func (s Status) Billable() bool { return s == StatusActive || s == StatusTrial }

// PaymentType is the kind of payment instrument behind a subscription.
type PaymentType string

const (
	PaymentCard        PaymentType = "card"
	PaymentBankAccount PaymentType = "bank_account"
	PaymentPayPal      PaymentType = "paypal"
	PaymentApplePay    PaymentType = "apple_pay"
	PaymentGooglePay   PaymentType = "google_pay"
	PaymentCrypto      PaymentType = "crypto"
	PaymentStoreCredit PaymentType = "store_credit"
	PaymentOther       PaymentType = "other"
)

func (p PaymentType) Valid() bool {
	switch p {
	case PaymentCard, PaymentBankAccount, PaymentPayPal, PaymentApplePay,
		PaymentGooglePay, PaymentCrypto, PaymentStoreCredit, PaymentOther:
		return true
	}
	return false
}

// Vendor is the merchant behind one or more subscriptions.
type Vendor struct {
	ID        string
	Name      string
	Category  string
	Domains   []string
	Aliases   []string
	LogoURL   string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Normalize cleans the vendor's string fields.
func (v *Vendor) Normalize() {
	v.Name = strings.TrimSpace(v.Name)
	v.Category = strings.TrimSpace(v.Category)
	v.LogoURL = strings.TrimSpace(v.LogoURL)
	v.Domains = normalizeList(v.Domains)
	v.Aliases = normalizeList(v.Aliases)
}

// Validate checks required fields.
func (v *Vendor) Validate() error {
	if v.Name == "" {
		return fmt.Errorf("vendor name is required")
	}
	return nil
}

// PaymentMethod is an instrument used to pay for subscriptions. Only
// non-sensitive metadata is ever stored: never a full card number or CVV.
type PaymentMethod struct {
	ID              string
	Label           string
	Type            PaymentType
	Issuer          string
	Network         string
	Last4           string
	ExpMonth        int
	ExpYear         int
	WalletAddress   string
	Chain           string
	BillingCurrency string
	Active          bool
	Notes           string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// Normalize cleans and constrains the payment method's fields.
func (p *PaymentMethod) Normalize() {
	p.Label = strings.TrimSpace(p.Label)
	p.Issuer = strings.TrimSpace(p.Issuer)
	p.Network = strings.TrimSpace(p.Network)
	p.Last4 = strings.TrimSpace(p.Last4)
	p.Chain = strings.TrimSpace(p.Chain)
	p.WalletAddress = strings.TrimSpace(p.WalletAddress)
	p.BillingCurrency = strings.ToUpper(strings.TrimSpace(p.BillingCurrency))
	if len(p.Last4) > 4 {
		// Defensive: never persist more than the last four digits.
		p.Last4 = p.Last4[len(p.Last4)-4:]
	}
}

// Validate checks required fields and value ranges.
func (p *PaymentMethod) Validate() error {
	if p.Label == "" {
		return fmt.Errorf("payment method label is required")
	}
	if !p.Type.Valid() {
		return fmt.Errorf("invalid payment method type %q", p.Type)
	}
	if p.ExpMonth < 0 || p.ExpMonth > 12 {
		return fmt.Errorf("expiry month %d out of range", p.ExpMonth)
	}
	if len(p.Last4) > 4 {
		return fmt.Errorf("last4 must be at most 4 characters")
	}
	return nil
}

// Subscription is a recurring payment the user is tracking.
type Subscription struct {
	ID              string
	VendorID        string
	Name            string
	Plan            string
	Status          Status
	Price           Money
	Interval        Interval
	AnchorDate      Date
	NextRenewal     Date
	StartedAt       *Date
	TrialEndsAt     *Date
	CancelledAt     *Date
	AutoRenew       bool
	PaymentMethodID string
	SourceID        string
	ExternalID      string
	Confidence      float64
	Tags            []string
	Notes           string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// Normalize cleans the subscription's string fields.
func (s *Subscription) Normalize() {
	s.Name = strings.TrimSpace(s.Name)
	s.Plan = strings.TrimSpace(s.Plan)
	s.Notes = strings.TrimSpace(s.Notes)
	s.Tags = normalizeList(s.Tags)
	s.Price.Currency = strings.ToUpper(strings.TrimSpace(s.Price.Currency))
}

// Validate checks the subscription's required fields and enums.
func (s *Subscription) Validate() error {
	if s.Name == "" {
		return fmt.Errorf("subscription name is required")
	}
	if !s.Status.Valid() {
		return fmt.Errorf("invalid status %q", s.Status)
	}
	if err := s.Price.Valid(); err != nil {
		return err
	}
	if err := s.Interval.Valid(); err != nil {
		return err
	}
	if s.AnchorDate.IsZero() {
		return fmt.Errorf("anchor date is required")
	}
	if s.Confidence < 0 || s.Confidence > 1 {
		return fmt.Errorf("confidence %v out of range 0-1", s.Confidence)
	}
	return nil
}

// RecomputeNextRenewal refreshes NextRenewal relative to today.
func (s *Subscription) RecomputeNextRenewal(today Date) {
	if s.Status == StatusCancelled || s.Status == StatusExpired {
		if s.CancelledAt != nil {
			s.NextRenewal = *s.CancelledAt
		}
		return
	}
	s.NextRenewal = s.Interval.NextRenewal(s.AnchorDate, today)
}

func normalizeList(in []string) []string {
	if len(in) == 0 {
		return []string{}
	}
	out := make([]string, 0, len(in))
	seen := make(map[string]struct{}, len(in))
	for _, item := range in {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		key := strings.ToLower(item)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, item)
	}
	return out
}
