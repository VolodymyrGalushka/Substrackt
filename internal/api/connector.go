// Package api contains declarative HTTP connectors for vendor billing APIs.
// Credentials are supplied by the caller and never stored in a Connector.
package api

import (
	"fmt"
	"sort"
	"strings"

	"substrackt/internal/domain"
)

// AuthMode is an authentication method an API connector accepts.
type AuthMode string

const (
	// AuthAPIKey sends a bearer token supplied by the user.
	AuthAPIKey AuthMode = "api_key"
	// AuthOAuth uses a token obtained through the loopback OAuth flow.
	AuthOAuth AuthMode = "oauth"
)

// Pagination selects how a connector follows pages.
type Pagination string

const (
	PaginationNone   Pagination = "none"
	PaginationStripe Pagination = "stripe"
	PaginationPaddle Pagination = "paddle"
)

// Mapping describes how a JSON response is turned into subscriptions.
type Mapping struct {
	// Items is the JSON path to the response array, for example "data".
	Items string
	// ID, Status, Customer, CreatedAt and NextRenewal are paths on each item.
	ID          string
	Status      string
	Customer    string
	CreatedAt   string
	NextRenewal string
	// NextRenewalFallback is checked on the first line item when NextRenewal is
	// empty (newer Stripe API versions put the billing period on items).
	NextRenewalFallback string
	// LineItems is the path to the subscription's line items. The first one is
	// used for amount, currency, interval and plan.
	LineItems     string
	Amount        string
	Currency      string
	Interval      string
	IntervalCount string
	Plan          string
	// PlanFallback is used when Plan is empty, usually the vendor price ID.
	PlanFallback string
	// Scale is the currency scale used by the API (usually 2).
	Scale int
	// Statuses maps vendor status strings to domain statuses.
	Statuses map[string]domain.Status
	// Intervals maps vendor interval strings to domain units.
	Intervals map[string]domain.Unit
}

// Connector is one declarative vendor API.
type Connector struct {
	ID      string
	Name    string
	Vendor  string
	DocsURL string
	// AuthModes lists accepted authentication methods.
	AuthModes []AuthMode
	BaseURL   string
	// SubscriptionsPath is appended to BaseURL.
	SubscriptionsPath string
	// PageSizeParam and PageSize configure the first request.
	PageSizeParam string
	PageSize      int
	Pagination    Pagination
	// ExtraQuery is added to every page request.
	ExtraQuery [][2]string
	Mapping    Mapping
}

// SupportsAuth reports whether the connector accepts an auth mode.
func (c Connector) SupportsAuth(mode string) bool {
	for _, supported := range c.AuthModes {
		if string(supported) == strings.ToLower(strings.TrimSpace(mode)) {
			return true
		}
	}
	return false
}

var connectors = map[string]Connector{
	"stripe": stripeConnector,
	"paddle": paddleConnector,
}

// Connectors returns the registry sorted by name.
func Connectors() []Connector {
	out := make([]Connector, 0, len(connectors))
	for _, connector := range connectors {
		out = append(out, connector)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// ConnectorFor resolves a connector ID.
func ConnectorFor(id string) (Connector, error) {
	id = strings.ToLower(strings.TrimSpace(id))
	connector, ok := connectors[id]
	if !ok {
		return Connector{}, fmt.Errorf("unknown API connector %q", id)
	}
	return connector, nil
}

var stripeConnector = Connector{
	ID:                "stripe",
	Name:              "Stripe",
	Vendor:            "Stripe",
	DocsURL:           "https://dashboard.stripe.com/apikeys",
	AuthModes:         []AuthMode{AuthAPIKey},
	BaseURL:           "https://api.stripe.com/v1",
	SubscriptionsPath: "/subscriptions",
	PageSizeParam:     "limit",
	PageSize:          100,
	Pagination:        PaginationStripe,
	ExtraQuery: [][2]string{
		{"status", "all"},
		{"expand[]", "data.customer"},
	},
	Mapping: Mapping{
		Items:               "data",
		ID:                  "id",
		Status:              "status",
		Customer:            "customer.name",
		CreatedAt:           "start_date",
		NextRenewal:         "current_period_end",
		NextRenewalFallback: "current_period_end",
		LineItems:           "items.data",
		Amount:              "price.unit_amount",
		Currency:            "price.currency",
		Interval:            "price.recurring.interval",
		IntervalCount:       "price.recurring.interval_count",
		Plan:                "price.nickname",
		PlanFallback:        "price.id",
		Scale:               2,
		Statuses: map[string]domain.Status{
			"active":             domain.StatusActive,
			"trialing":           domain.StatusTrial,
			"past_due":           domain.StatusActive,
			"unpaid":             domain.StatusPaused,
			"paused":             domain.StatusPaused,
			"canceled":           domain.StatusCancelled,
			"incomplete":         domain.StatusPaused,
			"incomplete_expired": domain.StatusExpired,
		},
		Intervals: map[string]domain.Unit{
			"day":   domain.UnitDay,
			"week":  domain.UnitWeek,
			"month": domain.UnitMonth,
			"year":  domain.UnitYear,
		},
	},
}

var paddleConnector = Connector{
	ID:                "paddle",
	Name:              "Paddle",
	Vendor:            "Paddle",
	DocsURL:           "https://developer.paddle.com/api-reference/overview",
	AuthModes:         []AuthMode{AuthAPIKey},
	BaseURL:           "https://api.paddle.com",
	SubscriptionsPath: "/subscriptions",
	PageSizeParam:     "per_page",
	PageSize:          100,
	Pagination:        PaginationPaddle,
	Mapping: Mapping{
		Items:         "data",
		ID:            "id",
		Status:        "status",
		Customer:      "customer_id",
		CreatedAt:     "created_at",
		NextRenewal:   "next_billed_at",
		LineItems:     "items",
		Amount:        "price.unit_price.amount",
		Currency:      "price.unit_price.currency_code",
		Interval:      "price.billing_cycle.interval",
		IntervalCount: "price.billing_cycle.frequency",
		Plan:          "price.name",
		PlanFallback:  "price.id",
		Scale:         2,
		Statuses: map[string]domain.Status{
			"active":    domain.StatusActive,
			"trialing":  domain.StatusTrial,
			"past_due":  domain.StatusActive,
			"paused":    domain.StatusPaused,
			"canceled":  domain.StatusCancelled,
			"cancelled": domain.StatusCancelled,
		},
		Intervals: map[string]domain.Unit{
			"day":   domain.UnitDay,
			"week":  domain.UnitWeek,
			"month": domain.UnitMonth,
			"year":  domain.UnitYear,
		},
	},
}
