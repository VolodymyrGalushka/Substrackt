package app

import "github.com/wailsapp/wails/v3/pkg/application"

// SystemDarkMode reports whether the desktop currently prefers a dark colour
// scheme. The frontend calls it for the "Use system theme" mode, polling
// occasionally so a desktop change is picked up even when no portal signal is
// delivered.
func (s *SettingsService) SystemDarkMode() bool {
	if dark, ok := desktopDarkMode(); ok {
		return dark
	}
	if wailsApp := application.Get(); wailsApp != nil {
		return wailsApp.Env.IsDarkMode()
	}
	return false
}
