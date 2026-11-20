//go:build !android && !ios

package browser

import "testing"

func TestResolveRuntimeAutoPrefersInstalledBrowser(t *testing.T) {
	runtimes := []Runtime{
		{ID: RuntimeAuto, Name: "Automatic", Installed: true},
		{ID: RuntimeChrome, Name: "Google Chrome", Installed: true, ExecutablePath: "/opt/chrome"},
		{ID: RuntimeBrave, Name: "Brave", Installed: true, ExecutablePath: "/opt/brave"},
		{ID: RuntimeBundled, Name: "Bundled Chromium", Installed: true},
	}

	got, err := resolveRuntime("", runtimes)
	if err != nil {
		t.Fatalf("resolve auto: %v", err)
	}
	if got.ID != RuntimeChrome || got.ExecutablePath != "/opt/chrome" {
		t.Fatalf("auto runtime = %+v, want Chrome", got)
	}
}

func TestResolveRuntimeAutoFallsBackToBundled(t *testing.T) {
	runtimes := []Runtime{
		{ID: RuntimeAuto, Name: "Automatic", Installed: true},
		{ID: RuntimeChrome, Name: "Google Chrome", Installed: false},
		{ID: RuntimeBundled, Name: "Bundled Chromium", Installed: true},
	}

	got, err := resolveRuntime(RuntimeAuto, runtimes)
	if err != nil {
		t.Fatalf("resolve auto: %v", err)
	}
	if got.ID != RuntimeBundled {
		t.Fatalf("auto runtime = %+v, want bundled", got)
	}
}

func TestResolveRuntimeExplicitMissingBrowserFails(t *testing.T) {
	runtimes := []Runtime{
		{ID: RuntimeChrome, Name: "Google Chrome", Installed: false},
		{ID: RuntimeBundled, Name: "Bundled Chromium", Installed: true},
	}

	if _, err := resolveRuntime(RuntimeChrome, runtimes); err == nil {
		t.Fatal("expected a missing Chrome install to be rejected")
	}
	if _, err := resolveRuntime("magic", runtimes); err == nil {
		t.Fatal("expected an unknown runtime to be rejected")
	}
}

func TestOptionsForBundledHasNoExecutablePath(t *testing.T) {
	options := OptionsFor(Runtime{ID: RuntimeBundled, Name: "Bundled Chromium", Installed: true})
	if options.ExecutablePath != "" {
		t.Fatalf("bundled options = %+v, want an empty executable path", options)
	}
}

func TestAvailableRuntimesAlwaysIncludesAutoAndBundled(t *testing.T) {
	var auto, bundled bool
	for _, rt := range AvailableRuntimes() {
		switch rt.ID {
		case RuntimeAuto:
			auto = true
		case RuntimeBundled:
			bundled = true
		}
	}
	if !auto || !bundled {
		t.Fatalf("runtimes = %+v, want auto and bundled", AvailableRuntimes())
	}
}
