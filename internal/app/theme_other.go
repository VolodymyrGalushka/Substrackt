//go:build !linux || android

package app

// desktopDarkMode has no extra signal outside Linux; Wails' native
// Environment.IsDarkMode is authoritative there.
func desktopDarkMode() (bool, bool) {
	return false, false
}

// PrepareSystemTheme is a no-op outside Linux: the native toolkit already
// follows the OS appearance.
func PrepareSystemTheme() {}
