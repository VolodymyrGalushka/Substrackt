package domain

import "time"

// ExtractionStatus is the review state of a parsed proposal.
type ExtractionStatus string

const (
	ExtractionPending    ExtractionStatus = "pending"
	ExtractionAccepted   ExtractionStatus = "accepted"
	ExtractionRejected   ExtractionStatus = "rejected"
	ExtractionSuperseded ExtractionStatus = "superseded"
)

func (s ExtractionStatus) Valid() bool {
	switch s {
	case ExtractionPending, ExtractionAccepted, ExtractionRejected, ExtractionSuperseded:
		return true
	}
	return false
}

// MatchKind describes how an extraction relates to an existing subscription.
type MatchKind string

const (
	MatchNone         MatchKind = "none"
	MatchSame         MatchKind = "same"
	MatchPriceChange  MatchKind = "price_change"
	MatchCancellation MatchKind = "cancellation"
	MatchAmbiguous    MatchKind = "ambiguous"
)

func (m MatchKind) Valid() bool {
	switch m {
	case MatchNone, MatchSame, MatchPriceChange, MatchCancellation, MatchAmbiguous:
		return true
	}
	return false
}

// Extraction is a parsed proposal produced by the ingestion pipeline. Pending
// extractions form the review queue; accepting one turns it into a subscription.
type Extraction struct {
	ID          string
	JobID       string
	ArtifactID  string
	Source      string
	Status      ExtractionStatus
	VendorName  string
	Name        string
	Plan        string
	AmountMinor int64
	Currency    string
	Scale       int
	Interval    Interval
	AnchorDate  Date
	NextRenewal Date
	// DesiredStatus is the subscription status the source implies (active,
	// trial or cancelled). It is not the extraction's review status.
	DesiredStatus Status
	PaymentHint   string
	Confidence    float64
	Model         string
	Rationale     string
	Evidence      string
	MatchSubID    string
	MatchKind     MatchKind
	Raw           string
	Error         string
	CreatedAt     time.Time
	UpdatedAt     time.Time
	ResolvedAt    time.Time
}

// Artifact is the raw evidence behind an extraction. Content is redacted before
// it is stored: card numbers and security codes never reach the database.
type Artifact struct {
	ID          string
	Kind        string
	MediaType   string
	ContentHash string
	Content     string
	Size        int
	Metadata    string
	CreatedAt   time.Time
}
