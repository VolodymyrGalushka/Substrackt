//go:build linux && !android

package app

import (
	"context"
	"os"
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
	// An explicit override wins: it is what GTK actually renders with.
	if env := os.Getenv("GTK_THEME"); env != "" {
		return isDarkThemeName(env), true
	}

	scheme, haveScheme := gsettingsValue("org.gnome.desktop.interface", "color-scheme")
	theme, haveTheme := gsettingsValue("org.gnome.desktop.interface", "gtk-theme")
	themeDark := haveTheme && isDarkThemeName(theme)

	// Budgie's color-scheme key can lag behind the theme the desktop actually
	// applies, so trust the GTK theme there.
	if strings.Contains(strings.ToLower(os.Getenv("XDG_CURRENT_DESKTOP")), "budgie") && haveTheme {
		return themeDark, true
	}

	if haveScheme {
		switch scheme {
		case "prefer-dark":
			return true, true
		case "prefer-light":
			return false, true
		}
	}
	if haveTheme {
		return themeDark, true
	}
	return false, false
}

// isDarkThemeName reports whether a GTK theme name selects a dark variant.
// Names arrive either bare ("Materia-dark") or with a variant suffix
// ("Adwaita:dark").
func isDarkThemeName(name string) bool {
	return strings.Contains(strings.ToLower(name), "dark")
}

// gtkThemePreference returns the configured GTK theme name.
func gtkThemePreference() string {
	theme, _ := gsettingsValue("org.gnome.desktop.interface", "gtk-theme")
	return theme
}

// PrepareSystemTheme makes GTK and WebKitGTK agree with the desktop's dark
// preference. It must be called before application.New initialises GTK:
// WebKitGTK derives prefers-color-scheme from gtk-application-prefer-dark-theme,
// a theme name ending in -dark, or a GTK_THEME value ending in :dark. Without
// this, pages report a light scheme on dark desktops even though the app's own
// palette is dark.
func PrepareSystemTheme() {
	dark, ok := desktopDarkMode()
	if !ok || !dark || os.Getenv("GTK_THEME") != "" {
		return
	}
	theme := gtkThemePreference()
	if isDarkThemeName(theme) {
		// Use the desktop's own dark theme when it has one.
		_ = os.Setenv("GTK_THEME", theme)
		return
	}
	_ = os.Setenv("GTK_THEME", "Adwaita:dark")
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
