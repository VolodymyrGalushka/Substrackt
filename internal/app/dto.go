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
	Name          string `json:"name"`
	Plan          string `json:"plan"`
	VendorID      string `json:"vendorId"`
	VendorName    string `json:"vendorName"`
	Status        string `json:"status"`
	Price         Money  `json:"price"`
	IntervalCount int    `json:"intervalCount"`
	IntervalUnit  string `json:"intervalUnit"`
	AnchorDate    string `json:"anchorDate"`
	// NextRenewal is optional. When set (for example by the review queue) the
	// service keeps it instead of recomputing from the anchor.
	NextRenewal     string   `json:"nextRenewal"`
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

// Job is a background job as seen by the frontend.
type Job struct {
	ID          string `json:"id"`
	Kind        string `json:"kind"`
	Status      string `json:"status"`
	Progress    int    `json:"progress"`
	Message     string `json:"message"`
	Error       string `json:"error"`
	Attempts    int    `json:"attempts"`
	MaxAttempts int    `json:"maxAttempts"`
	CreatedAt   string `json:"createdAt"`
	StartedAt   string `json:"startedAt"`
	FinishedAt  string `json:"finishedAt"`
	UpdatedAt   string `json:"updatedAt"`
}

// JobCounts summarises the queue for badges.
type JobCounts struct {
	Queued    int `json:"queued"`
	Running   int `json:"running"`
	Succeeded int `json:"succeeded"`
	Failed    int `json:"failed"`
	Cancelled int `json:"cancelled"`
}

// ReviewItem is a pending or resolved ingestion proposal.
type ReviewItem struct {
	ID                    string  `json:"id"`
	Status                string  `json:"status"`
	Source                string  `json:"source"`
	VendorName            string  `json:"vendorName"`
	Name                  string  `json:"name"`
	Plan                  string  `json:"plan"`
	Price                 Money   `json:"price"`
	IntervalCount         int     `json:"intervalCount"`
	IntervalUnit          string  `json:"intervalUnit"`
	AnchorDate            string  `json:"anchorDate"`
	NextRenewal           string  `json:"nextRenewal"`
	DesiredStatus         string  `json:"desiredStatus"`
	PaymentHint           string  `json:"paymentHint"`
	Confidence            float64 `json:"confidence"`
	Model                 string  `json:"model"`
	Rationale             string  `json:"rationale"`
	Evidence              string  `json:"evidence"`
	MatchSubscriptionID   string  `json:"matchSubscriptionId"`
	MatchSubscriptionName string  `json:"matchSubscriptionName"`
	MatchKind             string  `json:"matchKind"`
	JobID                 string  `json:"jobId"`
	ArtifactID            string  `json:"artifactId"`
	CreatedAt             string  `json:"createdAt"`
	UpdatedAt             string  `json:"updatedAt"`
}

// ReviewAcceptInput is the subscription the user approves. Zero values fall back
// to the extracted proposal, so the frontend can send only edited fields.
type ReviewAcceptInput struct {
	VendorName      string   `json:"vendorName"`
	Name            string   `json:"name"`
	Plan            string   `json:"plan"`
	AmountMinor     int64    `json:"amountMinor"`
	Currency        string   `json:"currency"`
	Scale           int      `json:"scale"`
	IntervalCount   int      `json:"intervalCount"`
	IntervalUnit    string   `json:"intervalUnit"`
	AnchorDate      string   `json:"anchorDate"`
	NextRenewal     string   `json:"nextRenewal"`
	Status          string   `json:"status"`
	PaymentMethodID string   `json:"paymentMethodId"`
	Tags            []string `json:"tags"`
	Notes           string   `json:"notes"`
}

// ReviewFailure describes one item that bulk accept could not apply.
type ReviewFailure struct {
	ID    string `json:"id"`
	Error string `json:"error"`
}

// ReviewBulkResult reports the outcome of a bulk accept or reject.
type ReviewBulkResult struct {
	Accepted int             `json:"accepted"`
	Rejected int             `json:"rejected"`
	Failed   []ReviewFailure `json:"failed"`
}

// ManualInput is pasted evidence for the manual ingestion channel.
type ManualInput struct {
	Title      string `json:"title"`
	Content    string `json:"content"`
	VendorHint string `json:"vendorHint"`
	OccurredAt string `json:"occurredAt"`
}

// Settings are the application-wide preferences.
type Settings struct {
	Timezone           string  `json:"timezone"`
	BaseCurrency       string  `json:"baseCurrency"`
	LLMBaseURL         string  `json:"llmBaseUrl"`
	LLMModel           string  `json:"llmModel"`
	LLMTimeoutSeconds  int     `json:"llmTimeoutSeconds"`
	AutoApply          bool    `json:"autoApply"`
	AutoApplyThreshold float64 `json:"autoApplyThreshold"`
	BrowserRuntime     string  `json:"browserRuntime"`
	BrowserSignIn      string  `json:"browserSignIn"`
}

// LLMTestResult is the outcome of probing the local endpoint.
type LLMTestResult struct {
	OK     bool     `json:"ok"`
	Models []string `json:"models"`
	Error  string   `json:"error"`
}

// LLMUsage is one audited model call.
type LLMUsage struct {
	ID               string `json:"id"`
	Model            string `json:"model"`
	BaseURL          string `json:"baseUrl"`
	PromptTokens     int    `json:"promptTokens"`
	CompletionTokens int    `json:"completionTokens"`
	LatencyMS        int64  `json:"latencyMs"`
	OK               bool   `json:"ok"`
	Cached           bool   `json:"cached"`
	Error            string `json:"error"`
	CreatedAt        string `json:"createdAt"`
}

// SourceConfig is the typed configuration of an ingestion channel as seen by
// the frontend. Credentials are never part of it.
type SourceConfig struct {
	// Email/IMAP fields.
	Address     string `json:"address"`
	Username    string `json:"username"`
	Mailbox     string `json:"mailbox"`
	Security    string `json:"security"`
	Auth        string `json:"auth"`
	TLSHost     string `json:"tlsHost"`
	InitialDays int    `json:"initialDays"`
	MaxMessages int    `json:"maxMessages"`
	// Browser and API connector.
	Connector string `json:"connector"`
	// Browser fields.
	BillingURL     string `json:"billingUrl"`
	TimeoutSeconds int    `json:"timeoutSeconds"`
	// API fields.
	APIAuth           string `json:"apiAuth"`
	APIAccount        string `json:"apiAccount"`
	APITimeoutSeconds int    `json:"apiTimeoutSeconds"`
	APIMaxItems       int    `json:"apiMaxItems"`
}

// APIConnector describes one vendor API connector preset.
type APIConnector struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Vendor  string   `json:"vendor"`
	DocsURL string   `json:"docsUrl"`
	Auths   []string `json:"auths"`
}

// Source is a configured ingestion channel as seen by the frontend.
type Source struct {
	ID              string       `json:"id"`
	Kind            string       `json:"kind"`
	Name            string       `json:"name"`
	Enabled         bool         `json:"enabled"`
	ScheduleMinutes int          `json:"scheduleMinutes"`
	LastSyncAt      string       `json:"lastSyncAt"`
	LastError       string       `json:"lastError"`
	HasSecret       bool         `json:"hasSecret"`
	Config          SourceConfig `json:"config"`
	CreatedAt       string       `json:"createdAt"`
	UpdatedAt       string       `json:"updatedAt"`
}

// SourceInput is the payload for creating or updating a source. Password is
// write-only: it is stored in the secret store and never returned.
type SourceInput struct {
	Kind            string       `json:"kind"`
	Name            string       `json:"name"`
	Enabled         bool         `json:"enabled"`
	ScheduleMinutes int          `json:"scheduleMinutes"`
	Config          SourceConfig `json:"config"`
	Password        string       `json:"password"`
}

// SourceTestResult reports the outcome of connecting to a source.
type SourceTestResult struct {
	OK       bool   `json:"ok"`
	Mailbox  string `json:"mailbox"`
	Messages int    `json:"messages"`
	Message  string `json:"message"`
	Error    string `json:"error"`
}

// BrowserRuntime reports the selected browser and whether it is ready.
type BrowserRuntime struct {
	Ready        bool                   `json:"ready"`
	Error        string                 `json:"error"`
	Selected     string                 `json:"selected"`
	Resolved     string                 `json:"resolved"`
	ResolvedName string                 `json:"resolvedName"`
	SignIn       string                 `json:"signIn"`
	Options      []BrowserRuntimeOption `json:"options"`
}

// BrowserRuntimeOption is one selectable browser binary.
type BrowserRuntimeOption struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Installed bool   `json:"installed"`
}

// BrowserConnector describes a vendor preset the Sources screen can offer.
type BrowserConnector struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	LoginURL   string `json:"loginUrl"`
	BillingURL string `json:"billingUrl"`
}

// BrowserLoginStatus is emitted while an interactive sign-in is running.
type BrowserLoginStatus struct {
	SourceID string `json:"sourceId"`
	State    string `json:"state"`
	Error    string `json:"error"`
}
