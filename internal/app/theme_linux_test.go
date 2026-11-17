//go:build linux && !android

package app

import (
	"os"
	"testing"
)

func TestIsDarkThemeName(t *testing.T) {
	cases := map[string]bool{
		"Materia-dark":  true,
		"Adwaita-dark":  true,
		"Adwaita:dark":  true,
		"Arc-Dark":      true,
		"Materia-light": false,
		"Adwaita":       false,
		"Arc":           false,
		"":              false,
	}
	for name, want := range cases {
		if got := isDarkThemeName(name); got != want {
			t.Errorf("isDarkThemeName(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestPrepareSystemThemeKeepsExplicitOverride(t *testing.T) {
	t.Setenv("GTK_THEME", "Materia-light")
	PrepareSystemTheme()
	if got := os.Getenv("GTK_THEME"); got != "Materia-light" {
		t.Fatalf("GTK_THEME = %q, want the explicit override", got)
	}
}

func TestPrepareSystemThemeSetsDarkGTKTheme(t *testing.T) {
	t.Setenv("GTK_THEME", "")
	dark, ok := desktopDarkMode()
	if !ok || !dark {
		t.Skip("desktop does not report a dark scheme")
	}
	PrepareSystemTheme()
	got := os.Getenv("GTK_THEME")
	if got == "" || !isDarkThemeName(got) {
		t.Fatalf("GTK_THEME = %q, want a dark theme name", got)
	}
}
