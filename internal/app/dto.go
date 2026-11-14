// Package app contains the Wails-bound services. Services translate between
// domain types and the DTOs exposed to the frontend, validate input, and emit
// change events.
package app

// Money is an amount in minor units plus its currency and scale.
type Money struct {
	AmountMinor int64  `json:"amountMinor"`
	Currency    string `json:"currency"`
	Scale       int    `json:"scale"`
}

// Subscription is a subscription as seen by the frontend.
type Subscription struct {
	ID                 string   `json:"id"`
	Name               string   `json:"name"`
	Plan               string   `json:"plan"`
	VendorID           string   `json:"vendorId"`
	VendorName         string   `json:"vendorName"`
	Status             string   `json:"status"`
	Price              Money    `json:"price"`
	IntervalCount      int      `json:"intervalCount"`
	IntervalUnit       string   `json:"intervalUnit"`
	AnchorDate         string   `json:"anchorDate"`
	NextRenewal        string   `json:"nextRenewal"`
	StartedAt          string   `json:"startedAt"`
	TrialEndsAt        string   `json:"trialEndsAt"`
	CancelledAt        string   `json:"cancelledAt"`
	AutoRenew          bool     `json:"autoRenew"`
	PaymentMethodID    string   `json:"paymentMethodId"`
	PaymentMethodLabel string   `json:"paymentMethodLabel"`
	Tags               []string `json:"tags"`
	Notes              string   `json:"notes"`
	CreatedAt          string   `json:"createdAt"`
	UpdatedAt          string   `json:"updatedAt"`
}

// SubscriptionInput is the payload for creating or updating a subscription.
type SubscriptionInput struct {
	Name            string   `json:"name"`
	Plan            string   `json:"plan"`
	VendorID        string   `json:"vendorId"`
	VendorName      string   `json:"vendorName"`
	Status          string   `json:"status"`
	Price           Money    `json:"price"`
	IntervalCount   int      `json:"intervalCount"`
	IntervalUnit    string   `json:"intervalUnit"`
	AnchorDate      string   `json:"anchorDate"`
	AutoRenew       bool     `json:"autoRenew"`
	PaymentMethodID string   `json:"paymentMethodId"`
	Tags            []string `json:"tags"`
	Notes           string   `json:"notes"`
	StartedAt       string   `json:"startedAt"`
	TrialEndsAt     string   `json:"trialEndsAt"`
}

// SubscriptionFilter narrows the subscription list.
type SubscriptionFilter struct {
	Statuses        []string `json:"statuses"`
	VendorID        string   `json:"vendorId"`
	PaymentMethodID string   `json:"paymentMethodId"`
	Search          string   `json:"search"`
	Limit           int      `json:"limit"`
}

// PaymentMethod is a payment instrument as seen by the frontend.
type PaymentMethod struct {
	ID              string `json:"id"`
	Label           string `json:"label"`
	Type            string `json:"type"`
	Issuer          string `json:"issuer"`
	Network         string `json:"network"`
	Last4           string `json:"last4"`
	ExpMonth        int    `json:"expMonth"`
	ExpYear         int    `json:"expYear"`
	WalletAddress   string `json:"walletAddress"`
	Chain           string `json:"chain"`
	BillingCurrency string `json:"billingCurrency"`
	Active          bool   `json:"active"`
	Notes           string `json:"notes"`
	CreatedAt       string `json:"createdAt"`
	UpdatedAt       string `json:"updatedAt"`
}

// PaymentMethodInput is the payload for creating or updating a payment method.
type PaymentMethodInput struct {
	Label           string `json:"label"`
	Type            string `json:"type"`
	Issuer          string `json:"issuer"`
	Network         string `json:"network"`
	Last4           string `json:"last4"`
	ExpMonth        int    `json:"expMonth"`
	ExpYear         int    `json:"expYear"`
	WalletAddress   string `json:"walletAddress"`
	Chain           string `json:"chain"`
	BillingCurrency string `json:"billingCurrency"`
	Active          bool   `json:"active"`
	Notes           string `json:"notes"`
}

// Vendor is a merchant as seen by the frontend.
type Vendor struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Category  string   `json:"category"`
	Domains   []string `json:"domains"`
	Aliases   []string `json:"aliases"`
	LogoURL   string   `json:"logoUrl"`
	CreatedAt string   `json:"createdAt"`
	UpdatedAt string   `json:"updatedAt"`
}

// VendorInput is the payload for creating or updating a vendor.
type VendorInput struct {
	Name     string   `json:"name"`
	Category string   `json:"category"`
	Domains  []string `json:"domains"`
	Aliases  []string `json:"aliases"`
	LogoURL  string   `json:"logoUrl"`
}

// SpendBucket is a monthly-equivalent total for a grouping.
type SpendBucket struct {
	Key     string `json:"key"`
	Label   string `json:"label"`
	Monthly Money  `json:"monthly"`
	Count   int    `json:"count"`
}

// DashboardSummary powers the dashboard screen.
type DashboardSummary struct {
	ActiveCount     int            `json:"activeCount"`
	TrialCount      int            `json:"trialCount"`
	PausedCount     int            `json:"pausedCount"`
	CancelledCount  int            `json:"cancelledCount"`
	MonthlyTotals   []Money        `json:"monthlyTotals"`
	YearlyTotals    []Money        `json:"yearlyTotals"`
	Upcoming        []Subscription `json:"upcoming"`
	ByPaymentMethod []SpendBucket  `json:"byPaymentMethod"`
	ByVendor        []SpendBucket  `json:"byVendor"`
}
