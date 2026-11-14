<script lang="ts">
  import { go, nav, type Route } from "../lib/router.svelte";
  import { store } from "../lib/state.svelte";

  const items: { route: Route; label: string; icon: string }[] = [
    { route: "dashboard", label: "Dashboard", icon: "◧" },
    { route: "subscriptions", label: "Subscriptions", icon: "≡" },
    { route: "payment-methods", label: "Payment methods", icon: "◫" },
  ];

  const activeCount = $derived(store.summary?.activeCount ?? 0);
  const trialCount = $derived(store.summary?.trialCount ?? 0);
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
        {#if item.route === "subscriptions" && activeCount + trialCount > 0}
          <span class="nav-count">{activeCount + trialCount}</span>
        {/if}
      </button>
    {/each}
  </nav>

  <div class="sidebar-foot">
    <span class="dot" class:ok={store.summary != null}></span>
    {store.summary ? "Local database connected" : "Connecting…"}
  </div>
</aside>
