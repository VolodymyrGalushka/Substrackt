package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestAuthorizeLoopbackFlow(t *testing.T) {
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form: %v", err)
		}
		if r.Form.Get("grant_type") != "authorization_code" || r.Form.Get("code") != "test-code" {
			t.Fatalf("form = %v", r.Form)
		}
		if r.Form.Get("code_verifier") == "" {
			t.Fatal("PKCE verifier was not sent")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"access-123","refresh_token":"refresh-456","token_type":"Bearer","expires_in":3600}`))
	}))
	defer tokenServer.Close()

	open := func(raw string) error {
		parsed, err := url.Parse(raw)
		if err != nil {
			return err
		}
		query := parsed.Query()
		if query.Get("state") == "" || query.Get("code_challenge") == "" {
			return fmt.Errorf("missing OAuth query parameters: %v", query)
		}
		redirect, err := url.Parse(query.Get("redirect_uri"))
		if err != nil {
			return err
		}
		callback := *redirect
		callback.RawQuery = url.Values{
			"code":  {"test-code"},
			"state": {query.Get("state")},
		}.Encode()
		go func() {
			_, _ = http.Get(callback.String())
		}()
		return nil
	}

	token, err := Authorize(context.Background(), OAuthConfig{
		AuthorizeURL: "https://example.com/authorize",
		TokenURL:     tokenServer.URL,
		ClientID:     "client-id",
		UsePKCE:      true,
	}, open)
	if err != nil {
		t.Fatalf("authorize: %v", err)
	}
	if token.AccessToken != "access-123" || token.RefreshToken != "refresh-456" {
		t.Fatalf("token = %+v", token)
	}
	if token.Expiry.Before(time.Now().Add(50 * time.Minute)) {
		t.Fatalf("expiry = %v", token.Expiry)
	}
}

func TestAuthorizeRejectsStateMismatch(t *testing.T) {
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("token endpoint should not be called")
	}))
	defer tokenServer.Close()

	open := func(raw string) error {
		parsed, _ := url.Parse(raw)
		redirect, _ := url.Parse(parsed.Query().Get("redirect_uri"))
		callback := *redirect
		callback.RawQuery = url.Values{"code": {"test-code"}, "state": {"wrong"}}.Encode()
		go func() { _, _ = http.Get(callback.String()) }()
		return nil
	}
	if _, err := Authorize(context.Background(), OAuthConfig{
		AuthorizeURL: "https://example.com/authorize",
		TokenURL:     tokenServer.URL,
		ClientID:     "client-id",
	}, open); err == nil || !strings.Contains(err.Error(), "state") {
		t.Fatalf("error = %v, want state mismatch", err)
	}
}
