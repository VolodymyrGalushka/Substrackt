<script lang="ts">
  import type { SpendBucket } from "../lib/api";
  import { cycleLabel, formatDate, formatMoney, relativeToToday } from "../lib/format";
  import { go } from "../lib/router.svelte";
  import { store } from "../lib/state.svelte";

  const summary = $derived(store.summary);
  const monthly = $derived(summary?.monthlyTotals ?? []);
  const yearly = $derived(summary?.yearlyTotals ?? []);
  const upcoming = $derived(summary?.upcoming ?? []);
  const byMethod = $derived(summary?.byPaymentMethod ?? []);
  const byVendor = $derived(summary?.byVendor ?? []);

  function maxAmount(buckets: SpendBucket[]): number {
    return Math.max(1, ...buckets.map((b) => b.monthly.amountMinor));
  }

  function barWidth(bucket: SpendBucket, buckets: SpendBucket[]): string {
    return `${Math.max(4, (bucket.monthly.amountMinor / maxAmount(buckets)) * 100)}%`;
  }
</script>

<div class="page">
  <header class="page-head">
    <div>
      <h1>Dashboard</h1>
      <p>What you pay, and what renews next.</p>
    </div>
    <button class="btn" onclick={() => go("subscriptions")}>Manage subscriptions</button>
  </header>

  <div class="stats">
    <article class="stat accent">
      <span class="label">Monthly spend</span>
      {#if monthly.length === 0}
        <span class="value">—</span>
      {:else}
        <div class="currency-list">
          {#each monthly as total (total.currency)}
            <span class="value">{formatMoney(total)}</span>
          {/each}
        </div>
      {/if}
      <span class="hint">normalised per month</span>
    </article>

    <article class="stat">
      <span class="label">Yearly spend</span>
      {#if yearly.length === 0}
        <span class="value">—</span>
      {:else}
        <div class="currency-list">
          {#each yearly as total (total.currency)}
            <span class="value">{formatMoney(total)}</span>
          {/each}
        </div>
      {/if}
      <span class="hint">monthly × 12</span>
    </article>

    <article class="stat">
      <span class="label">Active</span>
      <span class="value">{summary?.activeCount ?? 0}</span>
      <span class="hint">
        {summary?.trialCount ?? 0} on trial
      </span>
    </article>

    <article class="stat">
      <span class="label">Paused</span>
      <span class="value">{summary?.pausedCount ?? 0}</span>
      <span class="hint">{summary?.cancelledCount ?? 0} cancelled</span>
    </article>
  </div>

  <div class="grid-2">
    <section class="card">
      <h3>Renewing in the next 30 days</h3>
      {#if upcoming.length === 0}
        <div class="empty">
          <strong>Nothing due soon</strong>
          <span>Add a subscription to see renewal reminders here.</span>
        </div>
      {:else}
        <div class="list">
          {#each upcoming as sub (sub.id)}
            <div class="list-item">
              <div class="list-main">
                <span class="cell-title">{sub.name}</span>
                <span class="cell-sub">
                  {cycleLabel(sub.intervalCount, sub.intervalUnit)}
                  {#if sub.paymentMethodLabel}· {sub.paymentMethodLabel}{/if}
                </span>
              </div>
              <div class="list-side">
                <strong>{formatMoney(sub.price)}</strong>
                <span class="when">{formatDate(sub.nextRenewal)} · {relativeToToday(sub.nextRenewal)}</span>
              </div>
            </div>
          {/each}
        </div>
      {/if}
    </section>

    <section class="card">
      <h3>Spend by payment method</h3>
      {#if byMethod.length === 0}
        <div class="empty">
          <strong>No payment methods yet</strong>
          <span>Link a card, wallet or account to a subscription.</span>
          <button class="btn sm" onclick={() => go("payment-methods")}>Add payment method</button>
        </div>
      {:else}
        <div class="bars">
          {#each byMethod as bucket (bucket.key)}
            <div class="bar-row">
              <div class="bar-meta">
                <span>{bucket.label} · {bucket.count}</span>
                <strong>{formatMoney(bucket.monthly)}/mo</strong>
              </div>
              <div class="bar-track">
                <div class="bar-fill" style={`width: ${barWidth(bucket, byMethod)}`}></div>
              </div>
            </div>
          {/each}
        </div>
      {/if}
    </section>
  </div>

  <section class="card">
    <h3>Top vendors</h3>
    {#if byVendor.length === 0}
      <div class="empty">
        <strong>No vendor data yet</strong>
        <span>Vendor names are created when you add a subscription.</span>
      </div>
    {:else}
      <div class="bars">
        {#each byVendor.slice(0, 6) as bucket (bucket.key)}
          <div class="bar-row">
            <div class="bar-meta">
              <span>{bucket.label} · {bucket.count} subscription{bucket.count === 1 ? "" : "s"}</span>
              <strong>{formatMoney(bucket.monthly)}/mo</strong>
            </div>
            <div class="bar-track">
              <div class="bar-fill" style={`width: ${barWidth(bucket, byVendor)}`}></div>
            </div>
          </div>
        {/each}
      </div>
    {/if}
  </section>
</div>
