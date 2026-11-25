package llm

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestValidateBaseURL(t *testing.T) {
	allowed := []string{
		"http://127.0.0.1:11434",
		"http://localhost:1234/v1",
		"http://192.168.1.20:8000/v1/",
		"http://ollama:11434",
		"http://host.docker.internal:11434",
		"http://llm.home.arpa/v1",
	}
	for _, raw := range allowed {
		if _, err := ValidateBaseURL(raw); err != nil {
			t.Fatalf("expected %q to be allowed: %v", raw, err)
		}
		// Local endpoints need no consent either way.
		if _, err := ValidateBaseURLConsent(raw, false); err != nil {
			t.Fatalf("expected %q without consent to be allowed: %v", raw, err)
		}
	}

	remote := []string{
		"https://api.openai.com/v1",
		"https://api.anthropic.com",
		"https://example.com/v1",
	}
	for _, raw := range remote {
		if _, err := ValidateBaseURL(raw); !errors.Is(err, ErrRemoteEndpoint) {
			t.Fatalf("expected %q to be refused as remote, got %v", raw, err)
		}
		if _, err := ValidateBaseURLConsent(raw, false); !errors.Is(err, ErrRemoteEndpoint) {
			t.Fatalf("expected %q without consent to be refused, got %v", raw, err)
		}
		if _, err := ValidateBaseURLConsent(raw, true); err != nil {
			t.Fatalf("expected %q with consent to be allowed: %v", raw, err)
		}
	}

	// The structural rules apply even with consent: no odd schemes, embedded
	// credentials, queries or fragments.
	structural := []string{
		"ftp://127.0.0.1:11434",
		"http://user:pass@127.0.0.1:11434",
		"http://127.0.0.1:11434?key=secret",
		"https://api.openai.com/v1?key=secret",
	}
	for _, raw := range structural {
		if _, err := ValidateBaseURLConsent(raw, true); err == nil {
			t.Fatalf("expected %q to be rejected even with consent", raw)
		}
	}

	normalized, err := ValidateBaseURL("http://127.0.0.1:11434/")
	if err != nil {
		t.Fatal(err)
	}
	if normalized != "http://127.0.0.1:11434/v1" {
		t.Fatalf("normalized = %q", normalized)
	}
}

func TestCheckBaseURLClassifiesHosts(t *testing.T) {
	info, err := CheckBaseURL("http://127.0.0.1:11434")
	if err != nil {
		t.Fatal(err)
	}
	if info.Remote || info.Host != "127.0.0.1" || info.Normalized != "http://127.0.0.1:11434/v1" {
		t.Fatalf("local info = %+v", info)
	}

	info, err = CheckBaseURL("https://api.openai.com/v1")
	if err != nil {
		t.Fatal(err)
	}
	if !info.Remote || info.Host != "api.openai.com" {
		t.Fatalf("remote info = %+v", info)
	}

	if _, err := CheckBaseURL("not a url"); err == nil {
		t.Fatal("expected an invalid URL to be rejected")
	}
}

func TestNewWithConsent(t *testing.T) {
	const remote = "https://api.openai.com/v1"
	if _, err := New(remote, "gpt-4o", time.Second); !errors.Is(err, ErrRemoteEndpoint) {
		t.Fatalf("New without consent: %v", err)
	}
	if _, err := NewWithConsent(remote, "gpt-4o", time.Second, false); !errors.Is(err, ErrRemoteEndpoint) {
		t.Fatalf("NewWithConsent(false): %v", err)
	}
	client, err := NewWithConsent(remote, "gpt-4o", time.Second, true)
	if err != nil {
		t.Fatalf("NewWithConsent(true): %v", err)
	}
	if client.BaseURL() != remote || client.Model() != "gpt-4o" {
		t.Fatalf("client = %q/%q", client.BaseURL(), client.Model())
	}
}

func TestExtractRepairsInvalidJSON(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var req chatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if calls == 1 {
			writeChat(t, w, `{"is_subscription": true, "vendor": "Netflix"`)
			return
		}
		if len(req.Messages) < 4 {
			t.Fatalf("repair request should include the invalid answer and the error, got %d messages", len(req.Messages))
		}
		writeChat(t, w, `{"is_subscription":true,"vendor":"Netflix","name":"Netflix","plan":"Standard","amount":"12.99","currency":"USD","interval_count":1,"interval_unit":"month","next_renewal":"2026-12-01","status":"active","confidence":0.93,"evidence":"Netflix $12.99","reasoning":"clear receipt"}`)
	}))
	defer server.Close()

	client, err := New(server.URL, "test-model", 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	ex, meta, err := client.Extract(context.Background(), "Netflix receipt")
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if calls != 2 {
		t.Fatalf("calls = %d, want 2", calls)
	}
	if meta.Repairs != 1 {
		t.Fatalf("repairs = %d, want 1", meta.Repairs)
	}
	if ex.Vendor != "Netflix" || ex.Amount != "12.99" || ex.Confidence != 0.93 {
		t.Fatalf("unexpected extraction: %+v", ex)
	}
}

func TestExtractFallsBackWithoutJSONMode(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var req chatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.ResponseFormat != nil {
			http.Error(w, "response_format is not supported", http.StatusBadRequest)
			return
		}
		writeChat(t, w, `{"is_subscription":true,"vendor":"Spotify","name":"Spotify","plan":"Premium","amount":"9.99","currency":"EUR","interval_count":1,"interval_unit":"month","next_renewal":"","status":"active","confidence":0.9,"evidence":"Spotify 9.99 EUR","reasoning":"receipt"}`)
	}))
	defer server.Close()

	client, err := New(server.URL, "test-model", 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	ex, _, err := client.Extract(context.Background(), "Spotify receipt")
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if calls != 2 {
		t.Fatalf("calls = %d, want 2", calls)
	}
	if ex.Vendor != "Spotify" || ex.Currency != "EUR" {
		t.Fatalf("unexpected extraction: %+v", ex)
	}
}

func TestExtractRejectsNonSubscription(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeChat(t, w, `{"is_subscription":false,"reasoning":"shipping notice"}`)
	}))
	defer server.Close()

	client, err := New(server.URL, "test-model", 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	ex, _, err := client.Extract(context.Background(), "Your parcel has shipped")
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if ex.IsSubscription {
		t.Fatal("expected non-subscription")
	}
}

func TestModels(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"llama3.2"},{"id":"qwen2.5"}]}`))
	}))
	defer server.Close()

	client, err := New(server.URL, "test-model", 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	models, err := client.Models(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 2 || models[0] != "llama3.2" {
		t.Fatalf("models = %v", models)
	}
}

func writeChat(t *testing.T, w http.ResponseWriter, content string) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	payload := map[string]any{
		"choices": []map[string]any{
			{"message": map[string]string{"content": content}},
		},
		"usage": map[string]int{"prompt_tokens": 10, "completion_tokens": 5},
	}
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		t.Fatalf("encode response: %v", err)
	}
}

func TestExtractTruncatesContent(t *testing.T) {
	var seen string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req chatRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		seen = req.Messages[len(req.Messages)-1].Content
		writeChat(t, w, `{"is_subscription":false}`)
	}))
	defer server.Close()

	client, err := New(server.URL, "test-model", 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := client.Extract(context.Background(), strings.Repeat("x", 20_000)); err != nil {
		t.Fatal(err)
	}
	if len(seen) > 13_000 {
		t.Fatalf("content was not capped: %d bytes", len(seen))
	}
}
