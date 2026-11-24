//go:build !android && !ios

package browser

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/ChristopherDavenport/cookiecracker"
	"github.com/ChristopherDavenport/cookiecracker/chromium"
	"github.com/ChristopherDavenport/cookiecracker/firefox"
)

// CookieSource identifies a browser profile whose cookies can be read after a
// sign-in in the user's own browser.
type CookieSource struct {
	Name string
	// Kind is one of chrome, chromium, brave, edge or firefox.
	Kind string
	// Root optionally overrides the browser's profile root (used for Flatpak).
	Root string
}

// DefaultCookieSources returns the cookie stores that may contain the session
// after opening the user's default browser. On Linux this is the configured
// default browser; elsewhere all supported browsers are returned.
func DefaultCookieSources() ([]CookieSource, error) {
	return defaultCookieSources()
}

// OpenDefaultBrowser opens rawURL as a new tab in the user's default browser.
func OpenDefaultBrowser(rawURL string) error {
	return openDefaultBrowser(rawURL)
}

// WaitForSession polls the browser cookie stores until the connector's session
// cookie appears, then returns a Playwright storage-state document containing
// only the cookies that apply to pageURL. Cookie values are never logged.
func WaitForSession(ctx context.Context, sources []CookieSource, connector Connector, pageURL string, timeout time.Duration, progress func(string)) (string, error) {
	return waitForSession(ctx, sources, connector, pageURL, timeout, progress, readCookies)
}

func waitForSession(ctx context.Context, sources []CookieSource, connector Connector, pageURL string, timeout time.Duration, progress func(string), read func(CookieSource, *url.URL) ([]cookiecracker.Cookie, error)) (string, error) {
	target, err := url.Parse(pageURL)
	if err != nil || target.Host == "" {
		return "", fmt.Errorf("invalid browser page %q", pageURL)
	}
	if len(sources) == 0 {
		return "", errors.New("no browser cookie source is available")
	}
	if connector.SessionCookie == "" {
		return "", errors.New("browser connector does not define a session cookie")
	}
	if progress != nil {
		progress("Waiting for sign-in in your browser")
	}

	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(1500 * time.Millisecond)
	defer ticker.Stop()

	var lastErr error
	firstPoll := true
	for {
		readable := 0
		for _, source := range sources {
			cookies, err := read(source, target)
			if err != nil {
				lastErr = err
				continue
			}
			readable++
			if !hasSessionCookie(cookies, connector) {
				continue
			}
			state, err := storageStateJSON(cookies, target)
			if err != nil {
				lastErr = err
				continue
			}
			return state, nil
		}
		if firstPoll && readable == 0 && lastErr != nil {
			return "", fmt.Errorf("read browser session: %w", lastErr)
		}
		firstPoll = false

		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-deadline.C:
			if lastErr != nil {
				return "", fmt.Errorf("sign-in timed out; %w", lastErr)
			}
			return "", errors.New("sign-in timed out")
		case <-ticker.C:
		}
	}
}

func hasSessionCookie(cookies []cookiecracker.Cookie, connector Connector) bool {
	now := time.Now()
	for _, cookie := range cookies {
		if cookie.Name != connector.SessionCookie || cookie.Expired(now) {
			continue
		}
		if connector.SessionValue == "" || strings.EqualFold(cookie.Value, connector.SessionValue) {
			return true
		}
	}
	return false
}

func readCookies(source CookieSource, target *url.URL) ([]cookiecracker.Cookie, error) {
	var (
		cookies []cookiecracker.Cookie
		err     error
	)
	switch source.Kind {
	case "firefox":
		options := []firefox.Option{}
		if source.Root != "" {
			options = append(options, firefox.WithRoot(source.Root))
		}
		var browser *firefox.Firefox
		browser, err = firefox.New(options...)
		if err != nil {
			return nil, err
		}
		cookies, err = browser.CookiesForURL(target)
	default:
		variant, variantErr := chromiumVariant(source.Kind)
		if variantErr != nil {
			return nil, variantErr
		}
		options := []chromium.Option{}
		if source.Root != "" {
			options = append(options, chromium.WithRoot(source.Root))
		}
		var browser *chromium.Extractor
		browser, err = chromium.New(variant, options...)
		if err != nil {
			return nil, err
		}
		cookies, err = browser.CookiesForURL(target)
	}
	if err != nil {
		return nil, err
	}
	for i := range cookies {
		cookies[i] = stripHostHash(cookies[i])
	}
	return cookies, nil
}

// stripHostHash removes the SHA-256(host_key) prefix that Chromium cookie-store
// schema 24 prepends to every decrypted value. cookiecracker v0.0.1 does not do
// this yet; without it every imported value starts with 32 binary bytes.
func stripHostHash(cookie cookiecracker.Cookie) cookiecracker.Cookie {
	if len(cookie.Value) <= sha256.Size {
		return cookie
	}
	sum := sha256.Sum256([]byte(strings.ToLower(cookie.Domain)))
	if bytes.Equal([]byte(cookie.Value[:sha256.Size]), sum[:]) {
		cookie.Value = cookie.Value[sha256.Size:]
	}
	return cookie
}

func chromiumVariant(kind string) (chromium.Variant, error) {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "chrome":
		return chromium.Chrome, nil
	case "chromium":
		return chromium.Chromium, nil
	case "brave":
		return chromium.Brave, nil
	case "edge":
		return chromium.Edge, nil
	default:
		return chromium.Variant{}, fmt.Errorf("unknown cookie source %q", kind)
	}
}

type storageStateFile struct {
	Cookies []storageStateCookie `json:"cookies"`
	Origins []any                `json:"origins"`
}

type storageStateCookie struct {
	Name     string  `json:"name"`
	Value    string  `json:"value"`
	Domain   string  `json:"domain"`
	Path     string  `json:"path"`
	Expires  float64 `json:"expires"`
	HTTPOnly bool    `json:"httpOnly"`
	Secure   bool    `json:"secure"`
	SameSite string  `json:"sameSite,omitempty"`
}

func storageStateJSON(cookies []cookiecracker.Cookie, target *url.URL) (string, error) {
	now := time.Now()
	state := storageStateFile{Cookies: make([]storageStateCookie, 0, len(cookies)), Origins: []any{}}
	for _, cookie := range cookies {
		// Container/partition cookies cannot be represented in a Chromium
		// storage state, and the vendor session is in the default container.
		if cookie.OriginAttributes != "" || cookie.Expired(now) {
			continue
		}
		domain := cookie.Domain
		if domain == "" {
			domain = target.Hostname()
		}
		path := cookie.Path
		if path == "" {
			path = "/"
		}
		expires := float64(-1)
		if !cookie.Expires.IsZero() {
			expires = float64(cookie.Expires.Unix())
		}
		state.Cookies = append(state.Cookies, storageStateCookie{
			Name:     cookie.Name,
			Value:    cookie.Value,
			Domain:   domain,
			Path:     path,
			Expires:  expires,
			HTTPOnly: cookie.HTTPOnly,
			Secure:   cookie.Secure,
			SameSite: sameSiteName(cookie.SameSite),
		})
	}
	if len(state.Cookies) == 0 {
		return "", errors.New("no usable cookies were found for this browser page")
	}
	encoded, err := json.Marshal(state)
	if err != nil {
		return "", fmt.Errorf("encode browser session: %w", err)
	}
	return string(encoded), nil
}

func sameSiteName(value http.SameSite) string {
	switch value {
	case http.SameSiteStrictMode:
		return "Strict"
	case http.SameSiteLaxMode:
		return "Lax"
	case http.SameSiteNoneMode:
		return "None"
	default:
		return ""
	}
}

func startDetached(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

func allCookieSources() []CookieSource {
	return []CookieSource{
		{Name: "Google Chrome", Kind: "chrome"},
		{Name: "Brave", Kind: "brave"},
		{Name: "Chromium", Kind: "chromium"},
		{Name: "Microsoft Edge", Kind: "edge"},
		{Name: "Firefox", Kind: "firefox"},
	}
}

// flatpakRoot returns the Flatpak profile root for a browser when it exists.
func flatpakRoot(kind string) string {
	if os.Getenv("FLATPAK_ID") != "" {
		// Inside Flatpak the host profile is already mounted at the standard
		// location, so an extra root is unnecessary.
		return ""
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	var path string
	switch kind {
	case "chrome":
		path = filepath.Join(home, ".var/app/com.google.Chrome/config/google-chrome")
	case "chromium":
		path = filepath.Join(home, ".var/app/org.chromium.Chromium/config/chromium")
	case "brave":
		path = filepath.Join(home, ".var/app/com.brave.Browser/config/BraveSoftware/Brave-Browser")
	case "edge":
		path = filepath.Join(home, ".var/app/com.microsoft.Edge/config/microsoft-edge")
	case "firefox":
		path = filepath.Join(home, ".var/app/org.mozilla.firefox/.mozilla/firefox")
	default:
		return ""
	}
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		return path
	}
	return ""
}
