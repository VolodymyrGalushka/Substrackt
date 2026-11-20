//go:build !android && !ios

package browser

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// Browser runtime identifiers. They are stored in settings, so keep the values
// stable. "auto" is a preference, not a concrete browser.
const (
	RuntimeAuto     = "auto"
	RuntimeBundled  = "bundled"
	RuntimeChrome   = "chrome"
	RuntimeChromium = "chromium"
	RuntimeBrave    = "brave"
	RuntimeEdge     = "edge"
)

// Runtime is one selectable Chromium-family browser. ExecutablePath is empty
// for the bundled browser, which Playwright resolves from its own cache.
type Runtime struct {
	ID             string
	Name           string
	ExecutablePath string
	Installed      bool
}

// Options describe the browser binary a launch should use. An empty
// ExecutablePath selects Playwright's bundled Chromium.
type Options struct {
	ExecutablePath string
}

// OptionsFor converts a resolved runtime into launch options.
func OptionsFor(rt Runtime) Options {
	return Options{ExecutablePath: rt.ExecutablePath}
}

type runtimeCandidate struct {
	id       string
	name     string
	commands []string
	paths    []string
}

// AvailableRuntimes lists every known runtime, including ones that are not
// installed, so the settings screen can explain what is missing.
func AvailableRuntimes() []Runtime {
	out := []Runtime{{ID: RuntimeAuto, Name: "Automatic (recommended)", Installed: true}}
	for _, candidate := range runtimeCandidates() {
		path := findExecutable(candidate.commands, candidate.paths)
		out = append(out, Runtime{
			ID:             candidate.id,
			Name:           candidate.name,
			ExecutablePath: path,
			Installed:      path != "",
		})
	}
	out = append(out, Runtime{ID: RuntimeBundled, Name: "Bundled Chromium", Installed: true})
	return out
}

// ResolveRuntime turns the stored preference into a concrete runtime. Auto
// prefers an installed browser and falls back to bundled Chromium so Google
// sign-in sees a normal browser instead of Playwright's Chrome-for-Testing
// build whenever possible.
func ResolveRuntime(preference string) (Runtime, error) {
	return resolveRuntime(preference, AvailableRuntimes())
}

func resolveRuntime(preference string, runtimes []Runtime) (Runtime, error) {
	preference = strings.ToLower(strings.TrimSpace(preference))
	if preference == "" {
		preference = RuntimeAuto
	}
	if preference == RuntimeAuto {
		for _, id := range []string{RuntimeChrome, RuntimeBrave, RuntimeChromium, RuntimeEdge} {
			if rt, ok := runtimeByID(runtimes, id); ok && rt.Installed {
				return rt, nil
			}
		}
		if rt, ok := runtimeByID(runtimes, RuntimeBundled); ok {
			return rt, nil
		}
		return Runtime{}, fmt.Errorf("no browser runtime is available")
	}

	rt, ok := runtimeByID(runtimes, preference)
	if !ok {
		return Runtime{}, fmt.Errorf("unknown browser runtime %q", preference)
	}
	if !rt.Installed {
		return Runtime{}, fmt.Errorf("%s is not installed; install it or choose Automatic or Bundled Chromium", rt.Name)
	}
	return rt, nil
}

func runtimeByID(runtimes []Runtime, id string) (Runtime, bool) {
	for _, rt := range runtimes {
		if rt.ID == id {
			return rt, true
		}
	}
	return Runtime{}, false
}

func runtimeCandidates() []runtimeCandidate {
	home, _ := os.UserHomeDir()
	flatpak := func(app string) []string {
		if runtime.GOOS != "linux" {
			return nil
		}
		paths := []string{filepath.Join("/var/lib/flatpak/exports/bin", app)}
		if home != "" {
			paths = append([]string{filepath.Join(home, ".local/share/flatpak/exports/bin", app)}, paths...)
		}
		return paths
	}

	return []runtimeCandidate{
		{
			id:       RuntimeChrome,
			name:     "Google Chrome",
			commands: []string{"google-chrome", "google-chrome-stable", "google-chrome-beta"},
			paths: joinPaths(
				[]string{
					"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
					filepath.Join(home, "Applications/Google Chrome.app/Contents/MacOS/Google Chrome"),
				},
				windowsBrowserPaths("Google", "Chrome", "Application", "chrome.exe"),
				flatpak("com.google.Chrome"),
			),
		},
		{
			id:       RuntimeBrave,
			name:     "Brave",
			commands: []string{"brave-browser", "brave"},
			paths: joinPaths(
				[]string{
					"/Applications/Brave Browser.app/Contents/MacOS/Brave Browser",
					filepath.Join(home, "Applications/Brave Browser.app/Contents/MacOS/Brave Browser"),
				},
				windowsBrowserPaths("BraveSoftware", "Brave-Browser", "Application", "brave.exe"),
				flatpak("com.brave.Browser"),
			),
		},
		{
			id:       RuntimeChromium,
			name:     "Chromium",
			commands: []string{"chromium", "chromium-browser"},
			paths: joinPaths(
				[]string{
					"/Applications/Chromium.app/Contents/MacOS/Chromium",
					filepath.Join(home, "Applications/Chromium.app/Contents/MacOS/Chromium"),
				},
				windowsBrowserPaths("Chromium", "Application", "chrome.exe"),
				flatpak("org.chromium.Chromium"),
			),
		},
		{
			id:       RuntimeEdge,
			name:     "Microsoft Edge",
			commands: []string{"microsoft-edge", "microsoft-edge-stable"},
			paths: joinPaths(
				[]string{
					"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
					filepath.Join(home, "Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge"),
				},
				windowsBrowserPaths("Microsoft", "Edge", "Application", "msedge.exe"),
			),
		},
	}
}

func joinPaths(groups ...[]string) []string {
	var out []string
	for _, group := range groups {
		out = append(out, group...)
	}
	return out
}

// windowsBrowserPaths returns the usual per-machine and per-user install
// locations for a Windows browser. It is empty on other platforms.
func windowsBrowserPaths(parts ...string) []string {
	if runtime.GOOS != "windows" {
		return nil
	}
	roots := []string{
		os.Getenv("PROGRAMFILES"),
		os.Getenv("PROGRAMFILES(X86)"),
		os.Getenv("LOCALAPPDATA"),
	}
	out := make([]string, 0, len(roots))
	for _, root := range roots {
		if root == "" {
			continue
		}
		pathParts := append([]string{root}, parts...)
		out = append(out, filepath.Join(pathParts...))
	}
	return out
}

func findExecutable(commands, paths []string) string {
	for _, command := range commands {
		if path, err := exec.LookPath(command); err == nil && isExecutableFile(path) {
			return path
		}
	}
	for _, path := range paths {
		if path == "" {
			continue
		}
		if isExecutableFile(path) {
			return path
		}
	}
	return ""
}

func isExecutableFile(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	if runtime.GOOS == "windows" {
		return true
	}
	return info.Mode().Perm()&0o111 != 0
}
