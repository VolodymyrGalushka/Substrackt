<script lang="ts">
  import { go, nav, type Route } from "../lib/router.svelte";
  import { store } from "../lib/state.svelte";
  import { setTheme, theme, type ThemeMode } from "../lib/theme.svelte";
  import logoUrl from "../assets/logo.svg";

  const items: { route: Route; label: string; icon: string }[] = [
    { route: "dashboard", label: "Dashboard", icon: "◧" },
    { route: "subscriptions", label: "Subscriptions", icon: "≡" },
    { route: "review", label: "Review queue", icon: "◎" },
    { route: "sources", label: "Sources", icon: "⇣" },
    { route: "activity", label: "Activity", icon: "↻" },
    { route: "payment-methods", label: "Payment methods", icon: "◫" },
    { route: "settings", label: "Settings", icon: "⚙" },
  ];

  const themes: { mode: ThemeMode; label: string; title: string }[] = [
    { mode: "system", label: "Auto", title: "Follow the system light/dark setting" },
    { mode: "light", label: "Light", title: "Always use the light theme" },
    { mode: "dark", label: "Dark", title: "Always use the dark theme" },
  ];

  const activeCount = $derived(store.summary?.activeCount ?? 0);
  const trialCount = $derived(store.summary?.trialCount ?? 0);
  const runningCount = $derived((store.jobCounts?.queued ?? 0) + (store.jobCounts?.running ?? 0));

  function badge(route: Route): number {
    if (route === "subscriptions") return activeCount + trialCount;
    if (route === "review") return store.reviewCount;
    if (route === "activity") return runningCount;
    return 0;
  }
</script>

<aside class="sidebar">
  <div class="brand">
    <img class="brand-mark" src={logoUrl} alt="Substrackt logo" width="34" height="34" />
    <div>
      <strong>Substrackt</strong>
      <small>subscription tracker</small>
    </div>
  </div>

  <nav class="nav">
    {#each items as item (item.route)}
      <button
        class="nav-item"
        class:active={nav.route === item.route}
        onclick={() => go(item.route)}
      >
        <span class="nav-icon" aria-hidden="true">{item.icon}</span>
        {item.label}
        {#if badge(item.route) > 0}
          <span class="nav-count" class:warn={item.route === "review"}>{badge(item.route)}</span>
        {/if}
      </button>
    {/each}
  </nav>

  <div class="theme-control" role="group" aria-label="Theme">
    {#each themes as option (option.mode)}
      <button
        type="button"
        class="theme-option"
        class:active={theme.mode === option.mode}
        onclick={() => setTheme(option.mode)}
        title={option.title}
        aria-pressed={theme.mode === option.mode}
      >
        {option.label}
      </button>
    {/each}
  </div>

  <div class="sidebar-foot">
    <span class="dot" class:ok={store.summary != null}></span>
    {store.summary ? "Local database connected" : "Connecting…"}
  </div>
</aside>
