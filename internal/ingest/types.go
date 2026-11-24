package ingest

import (
	"context"
	"time"

	"substrackt/internal/domain"
	"substrackt/internal/llm"
)

// RawItem is the channel-independent input to the ingestion pipeline. Every
// future source (email, API, browser) produces one of these.
type RawItem struct {
	Channel    string
	ExternalID string
	Title      string
	Content    string
	VendorHint string
	OccurredAt time.Time
	// Proposal carries authoritative structured data from an API connector.
	// When set the pipeline still stores an artifact and matches/applies the
	// proposal, but skips classification and extraction.
	Proposal *Proposal
}

// OccurredDate returns the item's calendar date, or the zero date.
func (r RawItem) OccurredDate() domain.Date {
	if r.OccurredAt.IsZero() {
		return domain.Date{}
	}
	return domain.NewDate(r.OccurredAt)
}

// Proposal is a normalised subscription candidate.
type Proposal struct {
	VendorName  string
	Name        string
	Plan        string
	AmountMinor int64
	Currency    string
	Scale       int
	Interval    domain.Interval
	AnchorDate  domain.Date
	NextRenewal domain.Date
	Status      domain.Status
	Confidence  float64
	PaymentHint string
}

// Provenance travels with an applied proposal so a subscription can be traced
// back to the artifact that produced it.
type Provenance struct {
	Source     string
	ExternalID string
	ArtifactID string
	JobID      string
	Confidence float64
}

// Match is the result of comparing a proposal with existing subscriptions.
type Match struct {
	SubscriptionID   string
	SubscriptionName string
	Kind             domain.MatchKind
	Reason           string
}

// Applier is implemented by the app layer, which owns subscription validation,
// vendor resolution and change events.
type Applier interface {
	FindMatch(ctx context.Context, p Proposal) (Match, error)
	CreateProposal(ctx context.Context, p Proposal, prov Provenance) (string, error)
	UpdateProposal(ctx context.Context, subID string, p Proposal, prov Provenance) (string, error)
}

// Config configures one pipeline run.
type Config struct {
	AutoApply    bool
	Threshold    float64
	BaseCurrency string
	Today        domain.Date
	JobID        string
	Client       *llm.Client
	Model        string
}

// Result reports what the pipeline did with an item.
type Result struct {
	Action         string
	ArtifactID     string
	ExtractionID   string
	SubscriptionID string
	ReviewID       string
	Message        string
}

// Pipeline actions.
const (
	ActionCreated   = "created"
	ActionMatched   = "matched"
	ActionUpdated   = "updated"
	ActionReview    = "review"
	ActionDuplicate = "duplicate"
	ActionIgnored   = "ignored"
)

// Extract is a proposal plus the evidence and model metadata used to produce it.
type Extract struct {
	Proposal
	Model     string
	Rationale string
	Evidence  string
	Raw       string
}

func report(progress func(int, string), percent int, message string) {
	if progress != nil {
		progress(percent, message)
	}
}
