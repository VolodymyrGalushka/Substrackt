package app

import (
	"context"
	"testing"
	"time"

	apiconnector "substrackt/internal/api"
	"substrackt/internal/domain"
	"substrackt/internal/ingest"
)

func apiSourceInput() SourceInput {
	return SourceInput{
		Kind:            "api",
		Name:            "Stripe billing",
		Enabled:         true,
		ScheduleMinutes: 60,
		Config: SourceConfig{
			Connector:         "stripe",
			APIAuth:           "api_key",
			APIAccount:        "Acme",
			APITimeoutSeconds: 30,
			APIMaxItems:       100,
		},
		Password: "rk_test_123",
	}
}

func TestAPISourceCRUDAndSync(t *testing.T) {
	a := newTestApp(t)
	if _, err := a.settings.Update(Settings{
		BaseCurrency:       "USD",
		LLMTimeoutSeconds:  60,
		AutoApply:          true,
		AutoApplyThreshold: 0.9,
		BrowserSignIn:      "isolated",
	}); err != nil {
		t.Fatalf("enable auto-apply: %v", err)
	}

	created, err := a.sources.Create(apiSourceInput())
	if err != nil {
		t.Fatalf("create API source: %v", err)
	}
	if created.Kind != "api" || created.Config.Connector != "stripe" || !created.HasSecret {
		t.Fatalf("created = %+v", created)
	}
	if created.Config.APIAuth != "api_key" || created.Config.APIMaxItems != 100 {
		t.Fatalf("config = %+v", created.Config)
	}

	subscription := apiconnector.Subscription{
		ExternalID: "stripe:sub_123",
		Title:      "Stripe subscription: Pro",
		Content:    "Stripe subscription sub_123\nStatus: active\nPlan: Pro\nAmount: 1200 USD",
		VendorHint: "Stripe",
		OccurredAt: time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC),
		Proposal:   ingestProposal(),
	}
	a.apih.fetch = func(context.Context, apiconnector.Connector, string, time.Duration, int) ([]apiconnector.Subscription, error) {
		return []apiconnector.Subscription{subscription}, nil
	}

	if _, err := a.sources.SyncNow(created.ID); err != nil {
		t.Fatalf("sync now: %v", err)
	}
	a.drain(t)

	subs, err := a.subs.List(SubscriptionFilter{})
	if err != nil {
		t.Fatalf("list subscriptions: %v", err)
	}
	if len(subs) != 1 {
		t.Fatalf("subscriptions = %+v", subs)
	}
	if subs[0].VendorName != "Stripe" || subs[0].Price.AmountMinor != 1200 || subs[0].Price.Currency != "USD" {
		t.Fatalf("subscription = %+v", subs[0])
	}
	src, err := a.sources.Get(created.ID)
	if err != nil {
		t.Fatalf("get source: %v", err)
	}
	if src.LastSyncAt == "" || src.LastError != "" {
		t.Fatalf("source status = %+v", src)
	}
}

func TestAPISourceTestUsesStoredKey(t *testing.T) {
	a := newTestApp(t)
	created, err := a.sources.Create(apiSourceInput())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	var token string
	a.sources.apiFetch = func(_ context.Context, connector apiconnector.Connector, value string, _ time.Duration, _ int) ([]apiconnector.Subscription, error) {
		if connector.ID != "stripe" {
			t.Fatalf("connector = %s", connector.ID)
		}
		token = value
		return []apiconnector.Subscription{{ExternalID: "stripe:sub_1"}}, nil
	}
	result, err := a.sources.Test(created.ID, apiSourceInput())
	if err != nil {
		t.Fatalf("test: %v", err)
	}
	if !result.OK || token != "rk_test_123" {
		t.Fatalf("result = %+v, token = %q", result, token)
	}
}

func TestAPISourceValidation(t *testing.T) {
	a := newTestApp(t)

	unknown := apiSourceInput()
	unknown.Config.Connector = "unknown"
	if _, err := a.sources.Create(unknown); err == nil {
		t.Fatal("expected an unknown API connector to be rejected")
	}

	missingKey := apiSourceInput()
	missingKey.Password = ""
	if _, err := a.sources.Create(missingKey); err == nil {
		t.Fatal("expected a missing API key to be rejected")
	}

	created, err := a.sources.Create(apiSourceInput())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	switched := apiSourceInput()
	switched.Config.Connector = "paddle"
	switched.Password = ""
	if _, err := a.sources.Update(created.ID, switched); err == nil {
		t.Fatal("changing the connector without a new key should be rejected")
	}
	if stored, err := a.secrets.Get(sourceSecretRef(created.ID)); err != nil || stored != "rk_test_123" {
		t.Fatalf("failed update should keep the old key, got %q, %v", stored, err)
	}
	switched.Password = "paddle_key"
	updated, err := a.sources.Update(created.ID, switched)
	if err != nil {
		t.Fatalf("switch connector: %v", err)
	}
	if !updated.HasSecret {
		t.Fatal("the new connector should have a stored key")
	}
	stored, err := a.secrets.Get(sourceSecretRef(created.ID))
	if err != nil || stored != "paddle_key" {
		t.Fatalf("stored key = %q, %v", stored, err)
	}
}

func ingestProposal() ingest.Proposal {
	return ingest.Proposal{
		VendorName:  "Stripe",
		Name:        "Pro",
		Plan:        "Pro",
		AmountMinor: 1200,
		Currency:    "USD",
		Scale:       2,
		Interval:    domain.Interval{Count: 1, Unit: domain.UnitMonth},
		AnchorDate:  domain.Date{Year: 2024, Month: 1, Day: 2},
		Status:      domain.StatusActive,
		Confidence:  1,
		PaymentHint: "Stripe",
	}
}
