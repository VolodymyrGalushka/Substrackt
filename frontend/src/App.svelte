<script lang="ts">
  import { onMount } from "svelte";
  import { Events } from "@wailsio/runtime";
  import Sidebar from "./components/Sidebar.svelte";
  import Dashboard from "./routes/Dashboard.svelte";
  import Subscriptions from "./routes/Subscriptions.svelte";
  import PaymentMethods from "./routes/PaymentMethods.svelte";
  import { nav } from "./lib/router.svelte";
  import { clearError, loadAll, store } from "./lib/state.svelte";

  let ready = $state(false);

  onMount(() => {
    loadAll().finally(() => {
      ready = true;
    });

    const unsubscribe = [
      Events.On("subscription:changed", () => void loadAll()),
      Events.On("payment-method:changed", () => void loadAll()),
      Events.On("vendor:changed", () => void loadAll()),
    ];

    return () => unsubscribe.forEach((off) => off());
  });
</script>

<div class="shell">
  <Sidebar />
  <main class="main">
    {#if store.error}
      <div class="banner error" role="alert">
        <span>{store.error}</span>
        <button class="btn ghost sm" onclick={clearError}>Dismiss</button>
      </div>
    {/if}

    {#if !ready}
      <div class="loading">Loading…</div>
    {:else if nav.route === "subscriptions"}
      <Subscriptions />
    {:else if nav.route === "payment-methods"}
      <PaymentMethods />
    {:else}
      <Dashboard />
    {/if}
  </main>
</div>
