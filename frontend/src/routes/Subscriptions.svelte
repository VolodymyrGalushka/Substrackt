<script lang="ts">
  import type { Subscription } from "../lib/api";
  import { SubscriptionsService, errorMessage } from "../lib/api";
  import SubscriptionEditor from "../components/SubscriptionEditor.svelte";
  import { cycleLabel, formatDate, formatMoney, relativeToToday, statusLabel } from "../lib/format";
  import { loadAll, loadSubscriptions, store, type StatusFilter } from "../lib/state.svelte";

  let editorOpen = $state(false);
  let editing = $state<Subscription | null>(null);
  let searchTimer: ReturnType<typeof setTimeout> | undefined;

  const statuses: { value: StatusFilter; label: string }[] = [
    { value: "all", label: "All" },
    { value: "active", label: "Active" },
    { value: "trial", label: "Trial" },
    { value: "paused", label: "Paused" },
    { value: "cancelled", label: "Cancelled" },
  ];

  const subscriptions = $derived(store.subscriptions);

  function openNew() {
    editing = null;
    editorOpen = true;
  }

  function openEdit(sub: Subscription) {
    editing = sub;
    editorOpen = true;
  }

  function closeEditor() {
    editorOpen = false;
  }

  function onSearch() {
    clearTimeout(searchTimer);
    searchTimer = setTimeout(() => void loadSubscriptions(), 250);
  }

  async function setFilter(value: StatusFilter) {
    store.statusFilter = value;
    await loadSubscriptions();
  }

  async function togglePause(sub: Subscription) {
    try {
      await SubscriptionsService.SetStatus(sub.id, sub.status === "paused" ? "active" : "paused");
      await loadAll();
    } catch (err) {
      store.error = errorMessage(err);
    }
  }

  async function cancelSubscription(sub: Subscription) {
    if (!confirm(`Mark ${sub.name} as cancelled?`)) return;
    try {
      await SubscriptionsService.SetStatus(sub.id, "cancelled");
      await loadAll();
    } catch (err) {
      store.error = errorMessage(err);
    }
  }

  async function removeSubscription(sub: Subscription) {
    if (!confirm(`Delete ${sub.name} permanently? This cannot be undone.`)) return;
    try {
      await SubscriptionsService.Delete(sub.id);
      await loadAll();
    } catch (err) {
      store.error = errorMessage(err);
    }
  }

  function priceSuffix(count: number, unit: string): string {
    if (count !== 1) return "";
    switch (unit) {
      case "month":
        return "/mo";
      case "year":
        return "/yr";
      case "week":
        return "/wk";
      case "day":
        return "/day";
      default:
        return "";
    }
  }
</script>

<div class="page">
  <header class="page-head">
    <div>
      <h1>Subscriptions</h1>
      <p>Everything you pay for, in one place.</p>
    </div>
    <button class="btn primary" onclick={openNew}>+ New subscription</button>
  </header>

  <div class="toolbar">
    <input
      class="input search"
      placeholder="Search by name or plan…"
      bind:value={store.search}
      oninput={onSearch}
      aria-label="Search subscriptions"
    />
    <div class="chips">
      {#each statuses as status (status.value)}
        <button
          class="chip"
          class:active={store.statusFilter === status.value}
          onclick={() => setFilter(status.value)}
        >
          {status.label}
        </button>
      {/each}
    </div>
  </div>

  {#if subscriptions.length === 0}
    <div class="card empty">
      <strong>No subscriptions yet</strong>
      <span>Add your first subscription, or import it later from email, an API or a browser session.</span>
      <button class="btn primary" onclick={openNew}>+ New subscription</button>
    </div>
  {:else}
    <div class="table-wrap">
      <table>
        <thead>
          <tr>
            <th>Service</th>
            <th>Price</th>
            <th>Cycle</th>
            <th>Next renewal</th>
            <th>Payment</th>
            <th>Status</th>
            <th></th>
          </tr>
        </thead>
        <tbody>
          {#each subscriptions as sub (sub.id)}
            <tr>
              <td>
                <div class="cell-title">{sub.vendorName || sub.name}</div>
                <div class="cell-sub">
                  {sub.vendorName && sub.vendorName !== sub.name ? sub.name : sub.plan || "—"}
                </div>
              </td>
              <td class="mono">{formatMoney(sub.price)}{priceSuffix(sub.intervalCount, sub.intervalUnit)}</td>
              <td>{cycleLabel(sub.intervalCount, sub.intervalUnit)}</td>
              <td>
                {#if sub.nextRenewal}
                  <div>{formatDate(sub.nextRenewal)}</div>
                  <div class="cell-sub">{relativeToToday(sub.nextRenewal)}</div>
                {:else}
                  <span class="muted">—</span>
                {/if}
              </td>
              <td>{sub.paymentMethodLabel || "—"}</td>
              <td>
                <span class="badge {sub.status}">{statusLabel(sub.status)}</span>
              </td>
              <td>
                <div class="row-actions">
                  <button class="btn ghost sm" onclick={() => openEdit(sub)}>Edit</button>
                  <button class="btn ghost sm" onclick={() => togglePause(sub)}>
                    {sub.status === "paused" ? "Resume" : "Pause"}
                  </button>
                  {#if sub.status !== "cancelled"}
                    <button class="btn ghost sm" onclick={() => cancelSubscription(sub)}>Cancel</button>
                  {/if}
                  <button class="btn danger sm" onclick={() => removeSubscription(sub)}>Delete</button>
                </div>
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    </div>
  {/if}
</div>

<SubscriptionEditor open={editorOpen} initial={editing} onclose={closeEditor} />
