package app

import (
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"substrackt/internal/domain"
)

// emit sends a frontend event when the application is running. In tests there
// is no Wails app instance and the event is dropped.
func emit(name string, data ...any) {
	app := application.Get()
	if app == nil || app.Event == nil {
		return
	}
	app.Event.Emit(name, data...)
}

func moneyDTO(m domain.Money) Money {
	return Money{AmountMinor: m.AmountMinor, Currency: m.Currency, Scale: m.Scale}
}

func moneyDomain(m Money) domain.Money {
	return domain.Money{AmountMinor: m.AmountMinor, Currency: m.Currency, Scale: m.Scale}
}

func dateString(d domain.Date) string {
	if d.IsZero() {
		return ""
	}
	return d.String()
}

func datePtrString(d *domain.Date) string {
	if d == nil {
		return ""
	}
	return d.String()
}

func timeString(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func subscriptionDTO(sub domain.Subscription, vendorName, paymentLabel string) Subscription {
	tags := sub.Tags
	if tags == nil {
		tags = []string{}
	}
	return Subscription{
		ID:                 sub.ID,
		Name:               sub.Name,
		Plan:               sub.Plan,
		VendorID:           sub.VendorID,
		VendorName:         vendorName,
		Status:             string(sub.Status),
		Price:              moneyDTO(sub.Price),
		IntervalCount:      sub.Interval.Count,
		IntervalUnit:       string(sub.Interval.Unit),
		AnchorDate:         dateString(sub.AnchorDate),
		NextRenewal:        dateString(sub.NextRenewal),
		StartedAt:          datePtrString(sub.StartedAt),
		TrialEndsAt:        datePtrString(sub.TrialEndsAt),
		CancelledAt:        datePtrString(sub.CancelledAt),
		AutoRenew:          sub.AutoRenew,
		PaymentMethodID:    sub.PaymentMethodID,
		PaymentMethodLabel: paymentLabel,
		Tags:               tags,
		Notes:              sub.Notes,
		CreatedAt:          timeString(sub.CreatedAt),
		UpdatedAt:          timeString(sub.UpdatedAt),
	}
}

func paymentMethodDTO(pm domain.PaymentMethod) PaymentMethod {
	return PaymentMethod{
		ID:              pm.ID,
		Label:           pm.Label,
		Type:            string(pm.Type),
		Issuer:          pm.Issuer,
		Network:         pm.Network,
		Last4:           pm.Last4,
		ExpMonth:        pm.ExpMonth,
		ExpYear:         pm.ExpYear,
		WalletAddress:   pm.WalletAddress,
		Chain:           pm.Chain,
		BillingCurrency: pm.BillingCurrency,
		Active:          pm.Active,
		Notes:           pm.Notes,
		CreatedAt:       timeString(pm.CreatedAt),
		UpdatedAt:       timeString(pm.UpdatedAt),
	}
}

// JobDTO maps a job for events emitted from main.
func JobDTO(j domain.Job) Job { return jobDTO(j) }

func jobDTO(j domain.Job) Job {
	return Job{
		ID:          j.ID,
		Kind:        j.Kind,
		Status:      string(j.Status),
		Progress:    j.Progress,
		Message:     j.Message,
		Error:       j.Error,
		Attempts:    j.Attempts,
		MaxAttempts: j.MaxAttempts,
		CreatedAt:   timeString(j.CreatedAt),
		StartedAt:   timeString(j.StartedAt),
		FinishedAt:  timeString(j.FinishedAt),
		UpdatedAt:   timeString(j.UpdatedAt),
	}
}

func reviewDTO(ex domain.Extraction, matchName string) ReviewItem {
	return ReviewItem{
		ID:                    ex.ID,
		Status:                string(ex.Status),
		Source:                ex.Source,
		VendorName:            ex.VendorName,
		Name:                  ex.Name,
		Plan:                  ex.Plan,
		Price:                 Money{AmountMinor: ex.AmountMinor, Currency: ex.Currency, Scale: ex.Scale},
		IntervalCount:         ex.Interval.Count,
		IntervalUnit:          string(ex.Interval.Unit),
		AnchorDate:            dateString(ex.AnchorDate),
		NextRenewal:           dateString(ex.NextRenewal),
		DesiredStatus:         string(ex.DesiredStatus),
		PaymentHint:           ex.PaymentHint,
		Confidence:            ex.Confidence,
		Model:                 ex.Model,
		Rationale:             ex.Rationale,
		Evidence:              ex.Evidence,
		MatchSubscriptionID:   ex.MatchSubID,
		MatchSubscriptionName: matchName,
		MatchKind:             string(ex.MatchKind),
		JobID:                 ex.JobID,
		ArtifactID:            ex.ArtifactID,
		CreatedAt:             timeString(ex.CreatedAt),
		UpdatedAt:             timeString(ex.UpdatedAt),
	}
}

func settingsDTO(s domain.Settings) Settings {
	return Settings{
		Timezone:           s.Timezone,
		BaseCurrency:       s.BaseCurrency,
		LLMBaseURL:         s.LLMBaseURL,
		LLMModel:           s.LLMModel,
		LLMTimeoutSeconds:  s.LLMTimeoutSeconds,
		AutoApply:          s.AutoApply,
		AutoApplyThreshold: s.AutoApplyThreshold,
		BrowserRuntime:     s.BrowserRuntime,
		BrowserSignIn:      s.BrowserSignIn,
	}
}

func settingsDomain(s Settings) domain.Settings {
	return domain.Settings{
		Timezone:           s.Timezone,
		BaseCurrency:       s.BaseCurrency,
		LLMBaseURL:         s.LLMBaseURL,
		LLMModel:           s.LLMModel,
		LLMTimeoutSeconds:  s.LLMTimeoutSeconds,
		AutoApply:          s.AutoApply,
		AutoApplyThreshold: s.AutoApplyThreshold,
		BrowserRuntime:     s.BrowserRuntime,
		BrowserSignIn:      s.BrowserSignIn,
	}
}

func llmUsageDTO(u domain.LLMUsage) LLMUsage {
	return LLMUsage{
		ID:               u.ID,
		Model:            u.Model,
		BaseURL:          u.BaseURL,
		PromptTokens:     u.PromptTokens,
		CompletionTokens: u.CompletionTokens,
		LatencyMS:        u.LatencyMS,
		OK:               u.OK,
		Cached:           u.Cached,
		Error:            u.Error,
		CreatedAt:        timeString(u.CreatedAt),
	}
}

func vendorDTO(v domain.Vendor) Vendor {
	domains := v.Domains
	if domains == nil {
		domains = []string{}
	}
	aliases := v.Aliases
	if aliases == nil {
		aliases = []string{}
	}
	return Vendor{
		ID:        v.ID,
		Name:      v.Name,
		Category:  v.Category,
		Domains:   domains,
		Aliases:   aliases,
		LogoURL:   v.LogoURL,
		CreatedAt: timeString(v.CreatedAt),
		UpdatedAt: timeString(v.UpdatedAt),
	}
}

func sourceDTO(src domain.Source) Source {
	return Source{
		ID:              src.ID,
		Kind:            string(src.Kind),
		Name:            src.Name,
		Enabled:         src.Enabled,
		ScheduleMinutes: src.ScheduleMinutes,
		LastSyncAt:      timeString(src.LastSyncAt),
		LastError:       src.LastError,
		HasSecret:       src.SecretRef != "",
		Config:          sourceConfigDTO(src),
		CreatedAt:       timeString(src.CreatedAt),
		UpdatedAt:       timeString(src.UpdatedAt),
	}
}

func sourceConfigDTO(src domain.Source) SourceConfig {
	switch src.Kind {
	case domain.SourceBrowser:
		return SourceConfig{
			Connector:      src.Browser.Connector,
			BillingURL:     src.Browser.BillingURL,
			TimeoutSeconds: src.Browser.TimeoutSeconds,
		}
	case domain.SourceAPI:
		return SourceConfig{
			Connector:         src.API.Connector,
			APIAuth:           src.API.Auth,
			APIAccount:        src.API.Account,
			APITimeoutSeconds: src.API.TimeoutSeconds,
			APIMaxItems:       src.API.MaxItems,
		}
	}
	return SourceConfig{
		Address:     src.Email.Address,
		Username:    src.Email.Username,
		Mailbox:     src.Email.Mailbox,
		Security:    src.Email.Security,
		Auth:        src.Email.Auth,
		TLSHost:     src.Email.TLSHost,
		InitialDays: src.Email.InitialDays,
		MaxMessages: src.Email.MaxMessages,
	}
}

func sourceEmailConfig(cfg SourceConfig) domain.EmailSourceConfig {
	return domain.EmailSourceConfig{
		Address:     cfg.Address,
		Username:    cfg.Username,
		Mailbox:     cfg.Mailbox,
		Security:    cfg.Security,
		Auth:        cfg.Auth,
		TLSHost:     cfg.TLSHost,
		InitialDays: cfg.InitialDays,
		MaxMessages: cfg.MaxMessages,
	}
}

func sourceBrowserConfig(cfg SourceConfig) domain.BrowserSourceConfig {
	return domain.BrowserSourceConfig{
		Connector:      cfg.Connector,
		BillingURL:     cfg.BillingURL,
		TimeoutSeconds: cfg.TimeoutSeconds,
	}
}

func sourceAPIConfig(cfg SourceConfig) domain.APISourceConfig {
	return domain.APISourceConfig{
		Connector:      cfg.Connector,
		Auth:           cfg.APIAuth,
		Account:        cfg.APIAccount,
		TimeoutSeconds: cfg.APITimeoutSeconds,
		MaxItems:       cfg.APIMaxItems,
	}
}
