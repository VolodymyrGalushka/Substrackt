<script lang="ts">
  import { go, nav, type Route } from "../lib/router.svelte";
  import { store } from "../lib/state.svelte";

  const items: { route: Route; label: string; icon: string }[] = [
    { route: "dashboard", label: "Dashboard", icon: "◧" },
    { route: "subscriptions", label: "Subscriptions", icon: "≡" },
    { route: "review", label: "Review queue", icon: "◎" },
    { route: "sources", label: "Sources", icon: "⇣" },
    { route: "activity", label: "Activity", icon: "↻" },
    { route: "payment-methods", label: "Payment methods", icon: "◫" },
    { route: "settings", label: "Settings", icon: "⚙" },
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
    <span class="brand-mark">S</span>
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

  <div class="sidebar-foot">
    <span class="dot" class:ok={store.summary != null}></span>
    {store.summary ? "Local database connected" : "Connecting…"}
  </div>
</aside>
