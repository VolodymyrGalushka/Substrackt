package ingest

import (
	"context"
	"testing"

	"substrackt/internal/domain"
)

func TestClassifyBillingMail(t *testing.T) {
	tests := []struct {
		name    string
		subject string
		body    string
		want    Classification
	}{
		{"invoice", "Invoice #1042 from Acme", "Amount due: $9.99", ClassBilling},
		{"receipt", "Your receipt", "Thanks for your payment. Unsubscribe from this list.", ClassBilling},
		{"renewal", "Subscription renewal", "Your plan renews on 15 January.", ClassBilling},
		{"cancellation", "Your subscription has been cancelled", "We are sorry to see you go.", ClassBilling},
		{"trial", "Your free trial ends soon", "Add a payment method to continue.", ClassBilling},
		{"price change", "A change to your plan price", "The monthly price is now $11.99.", ClassBilling},
		{"shipping", "Your order has shipped", "Tracking number: 1Z999", ClassIgnore},
		{"newsletter", "Weekly newsletter", "Top stories from our team.", ClassIgnore},
		{"password", "Password reset requested", "Reset your password using the link.", ClassIgnore},
		{"security", "New sign-in to your account", "If this wasn't you, secure your account.", ClassIgnore},
		{"webinar", "Webinar tomorrow", "Join our event reminder list.", ClassIgnore},
		{"unknown", "Hello from Acme", "Following up on our conversation.", ClassUnknown},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Classify(tc.subject, "Acme <mail@acme.example>", tc.body)
			if got != tc.want {
				t.Fatalf("Classify(%q) = %v, want %v", tc.subject, got, tc.want)
			}
		})
	}
}

func TestPipelineIgnoresObviousNonBillingEmail(t *testing.T) {
	applier := &fakeApplier{}
	pipeline, st := testPipeline(t, applier, Config{
		AutoApply:    true,
		Threshold:    0.9,
		BaseCurrency: "USD",
		Today:        mustDate(t, "2026-11-15"),
		JobID:        "job-email",
	})

	result, err := pipeline.Run(context.Background(), RawItem{
		Channel:    "email",
		ExternalID: "message-1",
		Title:      "Weekly newsletter",
		VendorHint: "Acme",
		Content:    "Weekly newsletter\nTop stories from our team\nUnsubscribe from this list",
	}, nil)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if result.Action != ActionIgnored || result.Message != "Not a billing email" {
		t.Fatalf("result = %+v", result)
	}
	if result.ArtifactID == "" {
		t.Fatal("the artifact should still be kept for audit")
	}
	count, err := st.CountExtractions(context.Background(), domain.ExtractionPending)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Fatalf("pending extractions = %d, want 0", count)
	}
}
