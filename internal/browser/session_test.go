//go:build !android && !ios

package browser

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/ChristopherDavenport/cookiecracker"
)

func TestHasSessionCookie(t *testing.T) {
	connector := Connector{SessionCookie: "logged_in", SessionValue: "yes"}
	cookies := []cookiecracker.Cookie{
		{Name: "logged_in", Value: "no", Domain: ".github.com", Path: "/"},
		{Name: "logged_in", Value: "YES", Domain: ".github.com", Path: "/"},
	}
	if !hasSessionCookie(cookies, connector) {
		t.Fatal("expected logged_in=yes to count as a session")
	}
	if hasSessionCookie(cookies[:1], connector) {
		t.Fatal("logged_in=no should not count as a session")
	}
	expired := []cookiecracker.Cookie{{
		Name:    "logged_in",
		Value:   "yes",
		Domain:  ".github.com",
		Path:    "/",
		Expires: time.Now().Add(-time.Hour),
	}}
	if hasSessionCookie(expired, connector) {
		t.Fatal("an expired session cookie should be ignored")
	}
}

func TestStorageStateJSON(t *testing.T) {
	target, _ := url.Parse("https://github.com/settings/billing")
	cookies := []cookiecracker.Cookie{
		{
			Name:     "logged_in",
			Value:    "yes",
			Domain:   ".github.com",
			Path:     "/",
			Expires:  time.Now().Add(time.Hour),
			Secure:   true,
			HTTPOnly: true,
			SameSite: http.SameSiteLaxMode,
		},
		{
			Name:   "session",
			Value:  "abc",
			Domain: "github.com",
			Path:   "/",
		},
		{
			Name:             "container",
			Value:            "skip",
			Domain:           ".github.com",
			Path:             "/",
			OriginAttributes: "^userContextId=2",
		},
	}

	raw, err := storageStateJSON(cookies, target)
	if err != nil {
		t.Fatalf("storage state: %v", err)
	}
	var state struct {
		Cookies []struct {
			Name     string  `json:"name"`
			Value    string  `json:"value"`
			Domain   string  `json:"domain"`
			Path     string  `json:"path"`
			Expires  float64 `json:"expires"`
			HTTPOnly bool    `json:"httpOnly"`
			Secure   bool    `json:"secure"`
			SameSite string  `json:"sameSite"`
		} `json:"cookies"`
	}
	if err := json.Unmarshal([]byte(raw), &state); err != nil {
		t.Fatalf("decode storage state: %v", err)
	}
	if len(state.Cookies) != 2 {
		t.Fatalf("cookies = %+v, want 2", state.Cookies)
	}
	if state.Cookies[0].SameSite != "Lax" || !state.Cookies[0].HTTPOnly || !state.Cookies[0].Secure {
		t.Fatalf("first cookie = %+v", state.Cookies[0])
	}
	if state.Cookies[1].Expires != -1 {
		t.Fatalf("session cookie expires = %v, want -1", state.Cookies[1].Expires)
	}
	if strings.Contains(raw, "container") {
		t.Fatal("container cookies must not be imported")
	}
}

func TestWaitForSessionReturnsImportedCookies(t *testing.T) {
	read := func(CookieSource, *url.URL) ([]cookiecracker.Cookie, error) {
		return []cookiecracker.Cookie{{
			Name:    "logged_in",
			Value:   "yes",
			Domain:  ".github.com",
			Path:    "/",
			Secure:  true,
			Expires: time.Now().Add(time.Hour),
		}}, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	state, err := waitForSession(ctx,
		[]CookieSource{{Name: "Brave", Kind: "brave"}},
		Connector{SessionCookie: "logged_in", SessionValue: "yes"},
		"https://github.com/settings/billing",
		2*time.Second,
		nil,
		read,
	)
	if err != nil {
		t.Fatalf("wait: %v", err)
	}
	if !strings.Contains(state, `"logged_in"`) {
		t.Fatalf("state = %s, want the imported cookie", state)
	}
}

func TestWaitForSessionTimesOutWithoutLogin(t *testing.T) {
	read := func(CookieSource, *url.URL) ([]cookiecracker.Cookie, error) {
		return []cookiecracker.Cookie{{Name: "logged_in", Value: "no", Domain: ".github.com", Path: "/"}}, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := waitForSession(ctx,
		[]CookieSource{{Name: "Brave", Kind: "brave"}},
		Connector{SessionCookie: "logged_in", SessionValue: "yes"},
		"https://github.com/settings/billing",
		20*time.Millisecond,
		nil,
		read,
	); err == nil {
		t.Fatal("expected a timeout without a session cookie")
	}
}

func TestStripHostHash(t *testing.T) {
	domain := ".github.com"
	sum := sha256.Sum256([]byte(domain))
	cookie := cookiecracker.Cookie{Name: "logged_in", Domain: domain, Value: string(sum[:]) + "yes"}
	if got := stripHostHash(cookie); got.Value != "yes" {
		t.Fatalf("value = %q, want yes", got.Value)
	}
	plain := cookiecracker.Cookie{Name: "logged_in", Domain: domain, Value: "yes"}
	if got := stripHostHash(plain); got.Value != "yes" {
		t.Fatalf("plain value = %q, want yes", got.Value)
	}
}
