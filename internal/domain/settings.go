package domain

import (
	"fmt"
	"strings"
	"time"
)

// DefaultAutoApplyThreshold is the confidence at or above which an unambiguous
// extraction may be applied without review. Heuristic extraction is capped
// below this value, so only an LLM can auto-apply.
const DefaultAutoApplyThreshold = 0.9

// Browser runtimes select which Chromium-family binary browser sources use.
// "auto" prefers an installed browser and falls back to the bundled one.
const (
	BrowserRuntimeAuto     = "auto"
	BrowserRuntimeBundled  = "bundled"
	BrowserRuntimeChrome   = "chrome"
	BrowserRuntimeChromium = "chromium"
	BrowserRuntimeBrave    = "brave"
	BrowserRuntimeEdge     = "edge"
)

// Browser sign-in methods.
const (
	// BrowserSignInDefault opens a new tab in the user's default browser and
	// imports the vendor's session cookies.
	BrowserSignInDefault = "default"
	// BrowserSignInIsolated opens an isolated Playwright browser window.
	BrowserSignInIsolated = "isolated"
)

// Settings are the application-wide preferences stored in SQLite.
type Settings struct {
	// Timezone is an IANA name used to decide what "today" is. Empty means the
	// host's local timezone.
	Timezone string
	// BaseCurrency is the fallback currency when a source omits one.
	BaseCurrency string
	// LLMBaseURL is an OpenAI-compatible endpoint. It must be local unless the
	// user has explicitly accepted the data-exposure warning recorded in
	// LLMRemoteConsent.
	LLMBaseURL string
	LLMModel   string
	// LLMTimeoutSeconds bounds a single extraction request.
	LLMTimeoutSeconds int
	// LLMRemoteConsent records that the user accepted sending subscription data
	// to a non-local endpoint. It is cleared whenever no remote endpoint is
	// configured, so a later remote endpoint must be accepted again.
	LLMRemoteConsent bool
	// AutoApply lets unambiguous, high-confidence proposals create
	// subscriptions directly instead of waiting in the review queue.
	AutoApply          bool
	AutoApplyThreshold float64
	// BrowserRuntime selects the browser binary used by browser sources.
	BrowserRuntime string
	// BrowserSignIn selects how browser sign-in is performed.
	BrowserSignIn string
}

// DefaultSettings returns the settings used before the user saves any.
func DefaultSettings() Settings {
	return Settings{
		BaseCurrency:       "USD",
		LLMTimeoutSeconds:  60,
		AutoApply:          true,
		AutoApplyThreshold: DefaultAutoApplyThreshold,
		BrowserRuntime:     BrowserRuntimeAuto,
		BrowserSignIn:      BrowserSignInDefault,
	}
}

// Normalize trims and canonicalises user input.
func (s *Settings) Normalize() {
	s.Timezone = strings.TrimSpace(s.Timezone)
	s.BaseCurrency = strings.ToUpper(strings.TrimSpace(s.BaseCurrency))
	s.LLMBaseURL = strings.TrimSpace(s.LLMBaseURL)
	s.LLMModel = strings.TrimSpace(s.LLMModel)
	if s.LLMBaseURL == "" {
		s.LLMRemoteConsent = false
	}
	s.BrowserRuntime = strings.ToLower(strings.TrimSpace(s.BrowserRuntime))
	if s.BrowserRuntime == "" {
		s.BrowserRuntime = BrowserRuntimeAuto
	}
	s.BrowserSignIn = strings.ToLower(strings.TrimSpace(s.BrowserSignIn))
	if s.BrowserSignIn == "" {
		s.BrowserSignIn = BrowserSignInDefault
	}
}

// Validate checks the settings that do not depend on the LLM transport. The
// endpoint's locality is enforced by the llm package, which owns that policy.
func (s Settings) Validate() error {
	if s.Timezone != "" {
		if _, err := time.LoadLocation(s.Timezone); err != nil {
			return fmt.Errorf("unknown timezone %q", s.Timezone)
		}
	}
	if s.BaseCurrency == "" {
		return fmt.Errorf("base currency is required")
	}
	if !currencyCode(s.BaseCurrency) {
		return fmt.Errorf("invalid base currency %q", s.BaseCurrency)
	}
	if s.LLMTimeoutSeconds < 1 || s.LLMTimeoutSeconds > 600 {
		return fmt.Errorf("LLM timeout must be between 1 and 600 seconds")
	}
	if s.AutoApplyThreshold < 0 || s.AutoApplyThreshold > 1 {
		return fmt.Errorf("auto-apply threshold %v out of range 0-1", s.AutoApplyThreshold)
	}
	if s.LLMBaseURL != "" && s.LLMModel == "" {
		return fmt.Errorf("an LLM model is required when an endpoint is configured")
	}
	if s.LLMBaseURL == "" && s.LLMModel != "" {
		return fmt.Errorf("an LLM endpoint is required when a model is configured")
	}
	browserRuntime := s.BrowserRuntime
	if browserRuntime == "" {
		browserRuntime = BrowserRuntimeAuto
	}
	switch browserRuntime {
	case BrowserRuntimeAuto, BrowserRuntimeBundled, BrowserRuntimeChrome,
		BrowserRuntimeChromium, BrowserRuntimeBrave, BrowserRuntimeEdge:
	default:
		return fmt.Errorf("unknown browser runtime %q", s.BrowserRuntime)
	}
	browserSignIn := s.BrowserSignIn
	if browserSignIn == "" {
		browserSignIn = BrowserSignInDefault
	}
	switch browserSignIn {
	case BrowserSignInDefault, BrowserSignInIsolated:
	default:
		return fmt.Errorf("unknown browser sign-in method %q", s.BrowserSignIn)
	}
	return nil
}

// LLMEnabled reports whether a local extraction endpoint is configured.
func (s Settings) LLMEnabled() bool { return s.LLMBaseURL != "" && s.LLMModel != "" }

func currencyCode(s string) bool {
	if len(s) < 2 || len(s) > 10 {
		return false
	}
	for _, r := range s {
		if r < 'A' || r > 'Z' {
			return false
		}
	}
	return true
}
