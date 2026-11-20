package browser

import "testing"

func TestConnectorRegistry(t *testing.T) {
	connector, err := ConnectorFor("GitHub")
	if err != nil {
		t.Fatalf("ConnectorFor: %v", err)
	}
	if connector.ID != "github" || connector.BillingURL == "" || connector.LoginURL == "" {
		t.Fatalf("connector = %+v", connector)
	}
	if _, err := ConnectorFor("nope"); err == nil {
		t.Fatal("unknown connector should fail")
	}
	if len(Connectors()) == 0 {
		t.Fatal("registry should not be empty")
	}
}

func TestBillingPage(t *testing.T) {
	connector, err := ConnectorFor("github")
	if err != nil {
		t.Fatalf("ConnectorFor: %v", err)
	}

	page, err := BillingPage(connector, "")
	if err != nil || page != connector.BillingURL {
		t.Fatalf("default page = %q, %v", page, err)
	}

	override := "https://github.com/settings/billing/plans"
	if page, err := BillingPage(connector, override); err != nil || page != override {
		t.Fatalf("override = %q, %v", page, err)
	}

	if _, err := BillingPage(connector, "https://example.com/billing"); err == nil {
		t.Fatal("off-site URL should be rejected")
	}
	if _, err := BillingPage(connector, "http://github.com/settings/billing"); err == nil {
		t.Fatal("non-https URL should be rejected")
	}
}
