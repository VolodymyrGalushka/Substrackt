<script lang="ts">
  import type { ReviewAcceptInput, ReviewItem } from "../lib/api";
  import { ReviewService, errorMessage } from "../lib/api";
  import ImportEvidence from "../components/ImportEvidence.svelte";
  import {
    cycleLabel,
    formatDate,
    formatMoney,
    relativeToToday,
    scaleFor,
    statusLabel,
    toMajorUnits,
    toMinorUnits,
  } from "../lib/format";
  import { go } from "../lib/router.svelte";
  import { loadAll, loadReview, store, type ReviewStatus } from "../lib/state.svelte";

  type AcceptForm = {
    vendorName: string;
    name: string;
    plan: string;
    amount: string;
    currency: string;
    intervalCount: number;
    intervalUnit: string;
    anchorDate: string;
    nextRenewal: string;
    status: string;
    notes: string;
  };

  const statuses: { value: ReviewStatus; label: string }[] = [
    { value: "pending", label: "Pending" },
    { value: "accepted", label: "Accepted" },
    { value: "rejected", label: "Rejected" },
    { value: "superseded", label: "Superseded" },
  ];

  const units = ["day", "week", "month", "year"];

  let importOpen = $state(false);
  let busyId = $state("");
  let editingId = $state("");
  let selected = $state<string[]>([]);
  let openDetails = $state<string[]>([]);
  let form = $state(blank());

  const items = $derived(store.reviewItems);
  const isPending = $derived(store.reviewStatus === "pending");
  const allSelected = $derived(items.length > 0 && selected.length === items.length);

  function blank(): AcceptForm {
    return {
      vendorName: "",
      name: "",
      plan: "",
      amount: "",
      currency: "USD",
      intervalCount: 1,
      intervalUnit: "month",
      anchorDate: "",
      nextRenewal: "",
      status: "active",
      notes: "",
    };
  }

  function beginEdit(item: ReviewItem) {
    editingId = item.id;
    form = {
      vendorName: item.vendorName,
      name: item.name,
      plan: item.plan,
      amount: toMajorUnits(item.price.amountMinor, item.price.scale),
      currency: item.price.currency,
      intervalCount: item.intervalCount || 1,
      intervalUnit: item.intervalUnit || "month",
      anchorDate: item.anchorDate,
      nextRenewal: item.nextRenewal,
      status: item.desiredStatus || "active",
      notes: "",
    };
  }

  function cancelEdit() {
    editingId = "";
    form = blank();
  }

  function emptyInput(): ReviewAcceptInput {
    return {
      vendorName: "",
      name: "",
      plan: "",
      amountMinor: 0,
      currency: "",
      scale: 0,
      intervalCount: 0,
      intervalUnit: "",
      anchorDate: "",
      nextRenewal: "",
      status: "",
      paymentMethodId: "",
      tags: [],
      notes: "",
    };
  }

  function inputFromForm(): ReviewAcceptInput {
    const currency = form.currency.trim().toUpperCase();
    const scale = scaleFor(currency);
    return {
      vendorName: form.vendorName,
      name: form.name,
      plan: form.plan,
      amountMinor: toMinorUnits(form.amount, scale),
      currency,
      scale,
      intervalCount: Number(form.intervalCount) || 1,
      intervalUnit: form.intervalUnit,
      anchorDate: form.anchorDate,
      nextRenewal: form.nextRenewal,
      status: form.status,
      paymentMethodId: "",
      tags: [],
      notes: form.notes,
    };
  }

  async function setStatus(status: ReviewStatus) {
    store.reviewStatus = status;
    selected = [];
    editingId = "";
    await loadReview(status);
  }

  async function accept(item: ReviewItem) {
    busyId = item.id;
    try {
      const input = editingId === item.id ? inputFromForm() : emptyInput();
      await ReviewService.Accept(item.id, input);
      selected = selected.filter((id) => id !== item.id);
      if (editingId === item.id) cancelEdit();
      await loadAll();
    } catch (err) {
      store.error = errorMessage(err);
    } finally {
      busyId = "";
    }
  }

  async function reject(item: ReviewItem) {
    if (!confirm(`Dismiss the proposal for ${item.name || item.vendorName}?`)) return;
    busyId = item.id;
    try {
      await ReviewService.Reject(item.id);
      selected = selected.filter((id) => id !== item.id);
      await loadAll();
    } catch (err) {
      store.error = errorMessage(err);
    } finally {
      busyId = "";
    }
  }

  async function bulkAccept() {
    if (selected.length === 0) return;
    busyId = "bulk";
    try {
      const result = await ReviewService.BulkAccept(selected);
      const failed = result.failed ?? [];
      if (failed.length > 0) {
        store.error = `${failed.length} item(s) could not be accepted: ${failed[0].error}`;
      }
      selected = [];
      await loadAll();
    } catch (err) {
      store.error = errorMessage(err);
    } finally {
      busyId = "";
    }
  }

  function toggleSelected(id: string) {
    selected = selected.includes(id) ? selected.filter((row) => row !== id) : [...selected, id];
  }

  function toggleAll() {
    selected = allSelected ? [] : items.map((item) => item.id);
  }

  function toggleDetails(id: string) {
    openDetails = openDetails.includes(id)
      ? openDetails.filter((row) => row !== id)
      : [...openDetails, id];
  }

  function matchLabel(kind: string): string {
    switch (kind) {
      case "same":
        return "Matches an existing subscription";
      case "price_change":
        return "Price change";
      case "cancellation":
        return "Cancellation";
      case "ambiguous":
        return "Possible duplicate";
      default:
        return "";
    }
  }
</script>

<div class="page">
  <header class="page-head">
    <div>
      <h1>Review queue</h1>
      <p>Parsed proposals wait here until you accept them into your subscriptions.</p>
    </div>
    <button class="btn primary" onclick={() => (importOpen = true)}>+ Import text</button>
  </header>

  <div class="toolbar">
    <div class="chips">
      {#each statuses as status (status.value)}
        <button
          class="chip"
          class:active={store.reviewStatus === status.value}
          onclick={() => setStatus(status.value)}
        >
          {status.label}
        </button>
      {/each}
    </div>

    {#if isPending && items.length > 0}
      <div class="row">
        <label class="row" style="gap: 7px; font-size: 12.5px;">
          <input type="checkbox" checked={allSelected} onchange={toggleAll} />
          Select all
        </label>
        <button
          class="btn primary sm"
          disabled={selected.length === 0 || busyId === "bulk"}
          onclick={bulkAccept}
        >
          Accept selected ({selected.length})
        </button>
      </div>
    {/if}
  </div>

  {#if items.length === 0}
    <div class="card empty">
      <strong>{isPending ? "Nothing waiting for review" : "No items with this status"}</strong>
      {#if isPending}
        <span>
          Import a receipt or email to see proposed subscriptions here. High-confidence
          proposals may be applied automatically when auto-apply is enabled.
        </span>
        <button class="btn primary" onclick={() => (importOpen = true)}>Import text</button>
      {:else}
        <span>Switch back to Pending to see the current queue.</span>
      {/if}
    </div>
  {:else}
    <div class="review-list">
      {#each items as item (item.id)}
        <article class="card review-card" class:editing={editingId === item.id}>
          <div class="review-head">
            {#if isPending}
              <input
                type="checkbox"
                checked={selected.includes(item.id)}
                onchange={() => toggleSelected(item.id)}
                aria-label={`Select ${item.name || item.vendorName}`}
              />
            {/if}
            <div class="review-title">
              <strong>{item.name || item.vendorName || "Unnamed proposal"}</strong>
              <span class="cell-sub">
                {item.vendorName || "unknown vendor"}
                {#if item.plan}· {item.plan}{/if}
                · {item.source} · {formatDate(item.createdAt.slice(0, 10))}
              </span>
            </div>
            <span class="badge {item.status}">{item.status}</span>
            <span class="confidence" title="Extraction confidence">
              {Math.round(item.confidence * 100)}%
            </span>
          </div>

          <dl class="review-facts">
            <div>
              <dt>Price</dt>
              <dd class="mono">{formatMoney(item.price)}</dd>
            </div>
            <div>
              <dt>Cycle</dt>
              <dd>{cycleLabel(item.intervalCount, item.intervalUnit)}</dd>
            </div>
            <div>
              <dt>Next renewal</dt>
              <dd>
                {formatDate(item.nextRenewal)}
                {#if item.nextRenewal}
                  <span class="cell-sub"> · {relativeToToday(item.nextRenewal)}</span>
                {/if}
              </dd>
            </div>
            <div>
              <dt>Status</dt>
              <dd>{statusLabel(item.desiredStatus)}</dd>
            </div>
            {#if item.model}
              <div>
                <dt>Extracted by</dt>
                <dd>{item.model}</dd>
              </div>
            {/if}
          </dl>

          {#if item.matchKind !== "none"}
            <p class="review-match">
              {matchLabel(item.matchKind)}
              {#if item.matchSubscriptionName}: <strong>{item.matchSubscriptionName}</strong>{/if}
            </p>
          {/if}

          {#if item.evidence}
            <blockquote class="evidence">{item.evidence}</blockquote>
          {/if}

          {#if openDetails.includes(item.id)}
            <div class="review-details">
              {#if item.paymentHint}
                <p><span class="muted">Payment hint:</span> {item.paymentHint}</p>
              {/if}
              {#if item.rationale}
                <p><span class="muted">Rationale:</span> {item.rationale}</p>
              {/if}
              <p><span class="muted">Anchor date:</span> {formatDate(item.anchorDate)}</p>
            </div>
          {/if}

          {#if editingId === item.id}
            <form class="review-form" onsubmit={(event) => { event.preventDefault(); void accept(item); }}>
              <div class="form-grid">
                <div class="field">
                  <label for={`name-${item.id}`}>Name</label>
                  <input id={`name-${item.id}`} class="input" bind:value={form.name} />
                </div>
                <div class="field">
                  <label for={`vendor-${item.id}`}>Vendor</label>
                  <input id={`vendor-${item.id}`} class="input" bind:value={form.vendorName} />
                </div>
                <div class="field">
                  <label for={`plan-${item.id}`}>Plan</label>
                  <input id={`plan-${item.id}`} class="input" bind:value={form.plan} />
                </div>
                <div class="field">
                  <label for={`amount-${item.id}`}>Price</label>
                  <div class="row">
                    <input
                      id={`amount-${item.id}`}
                      class="input"
                      inputmode="decimal"
                      bind:value={form.amount}
                      style="flex: 1;"
                    />
                    <input
                      class="input"
                      bind:value={form.currency}
                      maxlength="10"
                      aria-label="Currency"
                      style="width: 84px;"
                    />
                  </div>
                </div>
                <div class="field">
                  <label for={`interval-${item.id}`}>Billing cycle</label>
                  <div class="row">
                    <input
                      id={`interval-${item.id}`}
                      class="input"
                      type="number"
                      min="1"
                      bind:value={form.intervalCount}
                      style="width: 70px;"
                      aria-label="Interval count"
                    />
                    <select class="select" bind:value={form.intervalUnit} style="flex: 1;">
                      {#each units as unit (unit)}
                        <option value={unit}>{unit}</option>
                      {/each}
                    </select>
                  </div>
                </div>
                <div class="field">
                  <label for={`status-${item.id}`}>Status</label>
                  <select id={`status-${item.id}`} class="select" bind:value={form.status}>
                    <option value="active">Active</option>
                    <option value="trial">Trial</option>
                    <option value="cancelled">Cancelled</option>
                  </select>
                </div>
                <div class="field">
                  <label for={`anchor-${item.id}`}>Anchor date</label>
                  <input id={`anchor-${item.id}`} class="input" type="date" bind:value={form.anchorDate} />
                </div>
                <div class="field">
                  <label for={`next-${item.id}`}>Next renewal</label>
                  <input id={`next-${item.id}`} class="input" type="date" bind:value={form.nextRenewal} />
                </div>
                <div class="field span-2">
                  <label for={`notes-${item.id}`}>Notes</label>
                  <input id={`notes-${item.id}`} class="input" bind:value={form.notes} />
                </div>
              </div>
            </form>
          {/if}

          <div class="review-actions">
            <button class="btn ghost sm" onclick={() => toggleDetails(item.id)}>
              {openDetails.includes(item.id) ? "Hide details" : "Details"}
            </button>
            {#if isPending}
              <div class="spacer"></div>
              {#if editingId === item.id}
                <button class="btn ghost sm" onclick={cancelEdit} disabled={busyId === item.id}>
                  Cancel edit
                </button>
              {:else}
                <button class="btn ghost sm" onclick={() => beginEdit(item)} disabled={busyId === item.id}>
                  Edit…
                </button>
              {/if}
              <button class="btn danger sm" onclick={() => reject(item)} disabled={busyId === item.id}>
                Reject
              </button>
              <button class="btn primary sm" onclick={() => accept(item)} disabled={busyId === item.id}>
                {busyId === item.id ? "Saving…" : editingId === item.id ? "Save & accept" : "Accept"}
              </button>
            {/if}
          </div>
        </article>
      {/each}
    </div>
  {/if}
</div>

<ImportEvidence open={importOpen} onclose={() => (importOpen = false)} onqueued={() => go("activity")} />
