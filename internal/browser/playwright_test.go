package browser

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ChristopherDavenport/cookiecracker"
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
	options := Options{ExecutablePath: os.Getenv("SUBSTRACKT_BROWSER_EXECUTABLE")}
	if ready, reason := engine.Ready(options); !ready {
		t.Fatalf("Playwright is not ready: %s", reason)
	}

	page, err := engine.Fetch(context.Background(), Connector{
		ID:              "test",
		Name:            "Test",
		BillingURL:      server.URL,
		AuthenticatedJS: "true",
		ReadySelector:   "main",
		ExtractJS:       "document.body.innerText",
	}, options, "", server.URL, 20*time.Second)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if !strings.Contains(page.Text, "Acme plan") || page.Title != "Test billing" {
		t.Fatalf("page = %+v", page)
	}
}

func TestPlaywrightUsesImportedStorageState(t *testing.T) {
	if os.Getenv("SUBSTRACKT_BROWSER_TEST") == "" {
		t.Skip("set SUBSTRACKT_BROWSER_TEST=1 to exercise Playwright")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<html><head><title>Billing</title></head><body><main>Cookie-protected billing</main></body></html>`))
	}))
	defer server.Close()
	target, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse server URL: %v", err)
	}
	state, err := storageStateJSON([]cookiecracker.Cookie{{
		Name:   "test",
		Value:  "ok",
		Domain: "127.0.0.1",
		Path:   "/",
	}}, target)
	if err != nil {
		t.Fatalf("storage state: %v", err)
	}

	engine := NewPlaywright()
	defer func() { _ = engine.Close() }()
	options := Options{ExecutablePath: os.Getenv("SUBSTRACKT_BROWSER_EXECUTABLE")}
	page, err := engine.Fetch(context.Background(), Connector{
		ID:              "test",
		Name:            "Test",
		AuthenticatedJS: "document.cookie.includes('test=ok')",
		ReadySelector:   "main",
		ExtractJS:       "document.body.innerText",
	}, options, state, server.URL, 20*time.Second)
	if err != nil {
		t.Fatalf("fetch with imported state: %v", err)
	}
	if !strings.Contains(page.Text, "Cookie-protected billing") {
		t.Fatalf("page = %+v", page)
	}
}
