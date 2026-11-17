// Theme preference. "system" follows the OS light/dark setting live; "light"
// and "dark" pin a palette. The resolved value is written to
// <html data-theme="...">, which app.css uses to swap CSS variables.
//
// WebKitGTK derives prefers-color-scheme from the GTK theme, so main.go sets
// GTK_THEME for a dark desktop before GTK starts. The system value itself comes
// from the Go backend (gsettings/GTK theme, with Wails' portal as fallback),
// which is more reliable than the web media query alone.
import { Events, System } from "@wailsio/runtime";
import { SettingsService } from "./api";

export type ThemeMode = "system" | "light" | "dark";
export type ResolvedTheme = "light" | "dark";

export const THEME_STORAGE_KEY = "substrackt:theme";

const MEDIA_QUERY = "(prefers-color-scheme: dark)";

function readMode(): ThemeMode {
  if (typeof localStorage === "undefined") return "system";
  try {
    const value = localStorage.getItem(THEME_STORAGE_KEY);
    if (value === "system" || value === "light" || value === "dark") return value;
  } catch {
    // Restricted webviews can deny storage access; fall back to system.
  }
  return "system";
}

const media =
  typeof window !== "undefined" && typeof window.matchMedia === "function"
    ? window.matchMedia(MEDIA_QUERY)
    : null;

// null until the OS preference has been queried.
let systemDark: boolean | null = null;

const initialMode = readMode();

function systemTheme(): ResolvedTheme {
  const dark = systemDark ?? media?.matches ?? true;
  return dark ? "dark" : "light";
}

function resolve(mode: ThemeMode): ResolvedTheme {
  if (mode === "system") return systemTheme();
  return mode;
}

export const theme = $state({
  mode: initialMode,
  resolved: resolve(initialMode),
});

function apply(): void {
  theme.resolved = resolve(theme.mode);
  if (typeof document !== "undefined") {
    document.documentElement.dataset.theme = theme.resolved;
  }
}

// refreshSystem reads the real desktop preference. It is async, so the first
// paint uses the web media query until it settles.
async function refreshSystem(): Promise<void> {
  try {
    // Go-side detection handles Linux desktops whose portal disagrees with the
    // GTK theme; Wails' own API is the fallback.
    systemDark = await SettingsService.SystemDarkMode();
  } catch {
    try {
      systemDark = await System.IsDarkMode();
    } catch {
      // Browser preview or no Wails bridge: use the web media query.
      systemDark = media?.matches ?? true;
    }
  }
  if (theme.mode === "system") apply();
}

// setTheme persists the preference. Storage failures must not block switching.
export function setTheme(mode: ThemeMode): void {
  theme.mode = mode;
  if (typeof localStorage !== "undefined") {
    try {
      localStorage.setItem(THEME_STORAGE_KEY, mode);
    } catch {
      // Ignore: the in-memory choice still applies for this session.
    }
  }
  apply();
}

let started = false;

// initTheme applies the saved preference and starts following OS changes while
// the mode is "system". Safe to call more than once.
export function initTheme(): void {
  apply();
  if (started) return;
  started = true;

  void refreshSystem();

  if (media) {
    media.addEventListener("change", () => {
      if (systemDark !== null) return; // Wails is authoritative.
      if (theme.mode === "system") apply();
    });
  }

  // Wails maps the platform event to common:ThemeChanged. Listening to the
  // platform names as well is harmless and covers builds that only emit those.
  for (const name of ["common:ThemeChanged", "linux:SystemThemeChanged", "windows:SystemThemeChanged"]) {
    Events.On(name, () => {
      void refreshSystem();
    });
  }

  // Desktop themes change outside the portal's notifications on some setups.
  // Poll cheaply while "system" is active and the window is visible.
  if (typeof window !== "undefined") {
    window.setInterval(() => {
      if (theme.mode === "system" && document.visibilityState !== "hidden") {
        void refreshSystem();
      }
    }, 15000);
  }
}
