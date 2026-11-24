//go:build linux && !android && !ios

package browser

import (
	"fmt"
	"os/exec"
	"strings"
)

// defaultCookieSources resolves the default browser on Linux and returns its
// cookie store. If the desktop setting cannot be read, every known browser is
// tried; only the one with the vendor session cookie will be used.
func defaultCookieSources() ([]CookieSource, error) {
	id := defaultDesktopBrowser()
	if id != "" {
		if source, ok := cookieSourceFromDesktopID(id); ok {
			return []CookieSource{source}, nil
		}
		return nil, fmt.Errorf("the default browser %q is not supported for session import; switch Sign-in method to Open an isolated browser", id)
	}
	return allCookieSourcesWithRoots(), nil
}

func defaultDesktopBrowser() string {
	out, err := exec.Command("xdg-settings", "get", "default-web-browser").Output()
	if err != nil {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(string(out)))
}

func cookieSourceFromDesktopID(id string) (CookieSource, bool) {
	lower := strings.ToLower(id)
	switch {
	case strings.Contains(lower, "firefox"):
		return cookieSource("Firefox", "firefox"), true
	case strings.Contains(lower, "brave"):
		return cookieSource("Brave", "brave"), true
	case strings.Contains(lower, "chromium"):
		return cookieSource("Chromium", "chromium"), true
	case strings.Contains(lower, "edge"):
		return cookieSource("Microsoft Edge", "edge"), true
	case strings.Contains(lower, "chrome"):
		return cookieSource("Google Chrome", "chrome"), true
	default:
		return CookieSource{}, false
	}
}

func cookieSource(name, kind string) CookieSource {
	return CookieSource{Name: name, Kind: kind, Root: flatpakRoot(kind)}
}

func allCookieSourcesWithRoots() []CookieSource {
	sources := allCookieSources()
	for i := range sources {
		sources[i].Root = flatpakRoot(sources[i].Kind)
	}
	return sources
}

func openDefaultBrowser(rawURL string) error {
	commands := [][]string{
		{"xdg-open", rawURL},
		{"gio", "open", rawURL},
		{"sensible-browser", rawURL},
	}
	for _, args := range commands {
		if _, err := exec.LookPath(args[0]); err != nil {
			continue
		}
		if err := startDetached(args[0], args[1:]...); err == nil {
			return nil
		}
	}
	return fmt.Errorf("could not open the default browser")
}
