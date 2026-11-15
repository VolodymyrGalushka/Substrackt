package domain

import "time"

// LLMUsage is one audited call to the local extraction model. Prompts and
// responses are not stored here; only counters and outcome.
type LLMUsage struct {
	ID               string
	Model            string
	BaseURL          string
	PromptTokens     int
	CompletionTokens int
	LatencyMS        int64
	OK               bool
	Cached           bool
	Error            string
	CreatedAt        time.Time
}
