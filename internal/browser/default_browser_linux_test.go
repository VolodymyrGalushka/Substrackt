//go:build linux && !android && !ios

package browser

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCookieSourceFromDesktopID(t *testing.T) {
	tests := []struct {
		id   string
		kind string
	}{
		{"brave-browser.desktop", "brave"},
		{"com.brave.Browser.desktop", "brave"},
		{"google-chrome.desktop", "chrome"},
		{"chromium-browser.desktop", "chromium"},
		{"microsoft-edge.desktop", "edge"},
		{"firefox.desktop", "firefox"},
	}
	for _, test := range tests {
		source, ok := cookieSourceFromDesktopID(test.id)
		if !ok {
			t.Fatalf("%s should map to a cookie source", test.id)
		}
		if source.Kind != test.kind {
			t.Fatalf("%s kind = %q, want %q", test.id, source.Kind, test.kind)
		}
	}
	if _, ok := cookieSourceFromDesktopID("safari.desktop"); ok {
		t.Fatal("unsupported browsers should not map")
	}
}

func TestOpenDefaultBrowserUsesXdgOpen(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "args")
	script := filepath.Join(dir, "xdg-open")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf '%s' \"$1\" > \"$OUT\"\n"), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}
	t.Setenv("PATH", dir)
	t.Setenv("OUT", out)

	if err := openDefaultBrowser("https://github.com/settings/billing"); err != nil {
		t.Fatalf("open: %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(out)
		if err == nil {
			if string(data) != "https://github.com/settings/billing" {
				t.Fatalf("args = %q", data)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("xdg-open was not invoked")
}
