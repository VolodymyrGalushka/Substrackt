//go:build windows && !android && !ios

package browser

// defaultCookieSources returns every supported browser profile. The browser
// opened by the shell is the default one; only the profile holding the vendor
// session cookie is used.
func defaultCookieSources() ([]CookieSource, error) {
	return allCookieSources(), nil
}

func openDefaultBrowser(rawURL string) error {
	return startDetached("rundll32", "url.dll,FileProtocolHandler", rawURL)
}
