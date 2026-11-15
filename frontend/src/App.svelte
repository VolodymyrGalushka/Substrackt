<script lang="ts">
  import { onMount } from "svelte";
  import { Events } from "@wailsio/runtime";
  import Sidebar from "./components/Sidebar.svelte";
  import Dashboard from "./routes/Dashboard.svelte";
  import Subscriptions from "./routes/Subscriptions.svelte";
  import Review from "./routes/Review.svelte";
  import Activity from "./routes/Activity.svelte";
  import PaymentMethods from "./routes/PaymentMethods.svelte";
  import Settings from "./routes/Settings.svelte";
  import { nav } from "./lib/router.svelte";
  import {
    clearError,
    loadAll,
    loadJobs,
    loadReview,
    loadReviewCount,
    store,
  } from "./lib/state.svelte";

  let ready = $state(false);
  let reviewTimer: ReturnType<typeof setTimeout> | undefined;
  let jobTimer: ReturnType<typeof setTimeout> | undefined;

  // Bulk operations emit one event per item. Debounce the reloads so a large
  // accept does not re-query the database once per row.
  function scheduleFullRefresh() {
    clearTimeout(reviewTimer);
    reviewTimer = setTimeout(() => {
      void loadAll();
    }, 150);
  }

  function scheduleJobRefresh() {
    clearTimeout(jobTimer);
    jobTimer = setTimeout(() => {
      void loadJobs();
    }, 100);
  }

  onMount(() => {
    loadAll().finally(() => {
      ready = true;
    });

    const unsubscribe = [
      Events.On("subscription:changed", () => void loadAll()),
      Events.On("payment-method:changed", () => void loadAll()),
      Events.On("vendor:changed", () => void loadAll()),
      Events.On("settings:changed", () => void loadAll()),
      Events.On("review:new", () => {
        void loadReview();
        void loadReviewCount();
      }),
      Events.On("review:changed", scheduleFullRefresh),
      Events.On("job:progress", scheduleJobRefresh),
      Events.On("job:finished", scheduleFullRefresh),
    ];

    return () => {
      unsubscribe.forEach((off) => off());
      clearTimeout(reviewTimer);
      clearTimeout(jobTimer);
    };
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
    {:else if nav.route === "review"}
      <Review />
    {:else if nav.route === "activity"}
      <Activity />
    {:else if nav.route === "payment-methods"}
      <PaymentMethods />
    {:else if nav.route === "settings"}
      <Settings />
    {:else}
      <Dashboard />
    {/if}
  </main>
</div>
