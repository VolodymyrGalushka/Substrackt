// Package browser runs vendor billing pages through Playwright. A source stores
// only the connector and the page to read; the signed-in session is a secret.
package browser

import (
	"fmt"
	"sort"
	"strings"
)

// Connector describes one vendor scraped through an interactive sign-in.
type Connector struct {
	ID         string
	Name       string
	Vendor     string
	LoginURL   string
	BillingURL string
	// AuthenticatedJS evaluates to true on a signed-in page. It must be safe to
	// run on any page of the site.
	AuthenticatedJS string
	// ReadySelector is waited for before extracting text.
	ReadySelector string
	// ExtractJS returns the billing text to ingest. It should be stable enough
	// that two syncs with no account change produce the same content hash.
	ExtractJS string
}

var connectors = map[string]Connector{
	"github": {
		ID:         "github",
		Name:       "GitHub",
		Vendor:     "GitHub",
		LoginURL:   "https://github.com/login",
		BillingURL: "https://github.com/settings/billing",
		AuthenticatedJS: `(() => {
			const login = (document.querySelector('meta[name="user-login"]')?.getAttribute('content') || '').trim();
			const marker = login || document.querySelector('a[href="/logout"], summary[aria-label="View profile and more"]');
			if (!marker) return false;
			const path = location.pathname;
			return !path.startsWith('/login') && !path.startsWith('/sessions') && !path.startsWith('/signup');
		})()`,
		ReadySelector: "main",
		ExtractJS: `(() => {
			const main = document.querySelector('main') || document.body;
			return (main ? main.innerText : '').trim();
		})()`,
	},
}

// Connectors returns the registry sorted by display name.
func Connectors() []Connector {
	out := make([]Connector, 0, len(connectors))
	for _, connector := range connectors {
		out = append(out, connector)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// ConnectorFor resolves a connector ID from stored configuration.
func ConnectorFor(id string) (Connector, error) {
	id = strings.ToLower(strings.TrimSpace(id))
	connector, ok := connectors[id]
	if !ok {
		return Connector{}, fmt.Errorf("unknown browser connector %q", id)
	}
	return connector, nil
}

// BillingPage returns the configured page, falling back to the connector's
// default. A configured URL must belong to the same host as the connector so a
// session cannot be sent to an unrelated site.
func BillingPage(connector Connector, override string) (string, error) {
	url := strings.TrimSpace(override)
	if url == "" {
		return connector.BillingURL, nil
	}
	if !strings.HasPrefix(url, "https://") {
		return "", fmt.Errorf("billing URL must use https")
	}
	host := func(raw string) string {
		raw = strings.SplitN(raw, "://", 2)[1]
		return strings.ToLower(strings.SplitN(raw, "/", 2)[0])
	}
	if host(url) != host(connector.BillingURL) {
		return "", fmt.Errorf("billing URL must stay on %s", host(connector.BillingURL))
	}
	return url, nil
}
