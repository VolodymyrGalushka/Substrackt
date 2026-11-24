//go:build !linux && !darwin && !windows && !android && !ios

package browser

func defaultCookieSources() ([]CookieSource, error) {
	return allCookieSources(), nil
}

func openDefaultBrowser(rawURL string) error {
	return startDetached("xdg-open", rawURL)
}
