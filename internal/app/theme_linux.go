//go:build linux && !android

package app

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

// desktopDarkMode reports the desktop colour-scheme preference from the
// settings every Linux desktop keeps. It returns ok=false when nothing
// authoritative is available, so the caller can fall back to the freedesktop
// portal exposed by Wails.
//
// Reading the portal alone is not enough on every desktop: some portal
// implementations (notably Budgie's) report "prefer light" while the GTK theme
// in use is dark, which would make "Use system theme" ignore the desktop.
func desktopDarkMode() (dark bool, ok bool) {
	if value, found := gsettingsValue("org.gnome.desktop.interface", "color-scheme"); found {
		switch value {
		case "prefer-dark":
			return true, true
		case "prefer-light":
			return false, true
		}
	}
	if value, found := gsettingsValue("org.gnome.desktop.interface", "gtk-theme"); found {
		return strings.Contains(strings.ToLower(value), "dark"), true
	}
	return false, false
}

func gsettingsValue(schema, key string) (string, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	out, err := exec.CommandContext(ctx, "gsettings", "get", schema, key).Output()
	if err != nil {
		return "", false
	}
	value := strings.Trim(strings.TrimSpace(string(out)), "'\"")
	return value, value != ""
}
