package browser

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestPlaywrightFetchLocalPage(t *testing.T) {
	if os.Getenv("SUBSTRACKT_BROWSER_TEST") == "" {
		t.Skip("set SUBSTRACKT_BROWSER_TEST=1 to exercise Playwright")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<html><head><title>Test billing</title></head><body><main>Acme plan $9.00 per month</main></body></html>`))
	}))
	defer server.Close()

	engine := NewPlaywright()
	defer func() { _ = engine.Close() }()
	if ready, reason := engine.Ready(); !ready {
		t.Fatalf("Playwright is not ready: %s", reason)
	}

	page, err := engine.Fetch(context.Background(), Connector{
		ID:              "test",
		Name:            "Test",
		BillingURL:      server.URL,
		AuthenticatedJS: "true",
		ReadySelector:   "main",
		ExtractJS:       "document.body.innerText",
	}, "", server.URL, 20*time.Second)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if !strings.Contains(page.Text, "Acme plan") || page.Title != "Test billing" {
		t.Fatalf("page = %+v", page)
	}
}
