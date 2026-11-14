<script lang="ts">
  import type { Subscription, SubscriptionInput } from "../lib/api";
  import { SubscriptionsService, errorMessage } from "../lib/api";
  import Modal from "./Modal.svelte";
  import { scaleFor, toMajorUnits, toMinorUnits, todayISO } from "../lib/format";
  import { loadAll, store } from "../lib/state.svelte";

  let {
    open = false,
    initial = null,
    onclose,
  }: { open?: boolean; initial?: Subscription | null; onclose: () => void } = $props();

  function blank() {
    return {
      vendorId: "",
      vendorName: "",
      name: "",
      plan: "",
      status: "active",
      amount: "",
      currency: "USD",
      scale: 2,
      intervalCount: 1,
      intervalUnit: "month",
      anchorDate: todayISO(),
      autoRenew: true,
      paymentMethodId: "",
      tags: "",
      notes: "",
      startedAt: "",
      trialEndsAt: "",
    };
  }

  let form = $state(blank());
  let saving = $state(false);
  let formError = $state("");

  const activeMethods = $derived(
    store.paymentMethods.filter((pm) => pm.active || pm.id === form.paymentMethodId),
  );

  $effect(() => {
    if (!open) return;
    const sub = initial;
    formError = "";
    if (!sub) {
      form = blank();
      return;
    }
    form = {
      vendorId: sub.vendorId,
      vendorName: sub.vendorName,
      name: sub.name,
      plan: sub.plan,
      status: sub.status,
      amount: toMajorUnits(sub.price.amountMinor, sub.price.scale),
      currency: sub.price.currency,
      scale: sub.price.scale,
      intervalCount: sub.intervalCount,
      intervalUnit: sub.intervalUnit,
      anchorDate: sub.anchorDate || todayISO(),
      autoRenew: sub.autoRenew,
      paymentMethodId: sub.paymentMethodId,
      tags: (sub.tags ?? []).join(", "),
      notes: sub.notes,
      startedAt: sub.startedAt,
      trialEndsAt: sub.trialEndsAt,
    };
  });

  function onCurrencyInput() {
    form.currency = form.currency.toUpperCase();
    form.scale = scaleFor(form.currency);
  }

  async function submit(event: SubmitEvent) {
    event.preventDefault();
    formError = "";
    saving = true;
    const input: SubscriptionInput = {
      vendorId: form.vendorId,
      vendorName: form.vendorName,
      name: form.name,
      plan: form.plan,
      status: form.status,
      price: {
        amountMinor: toMinorUnits(form.amount, form.scale),
        currency: form.currency.trim().toUpperCase(),
        scale: form.scale,
      },
      intervalCount: Math.max(1, Number(form.intervalCount) || 1),
      intervalUnit: form.intervalUnit,
      anchorDate: form.anchorDate,
      autoRenew: form.autoRenew,
      paymentMethodId: form.paymentMethodId,
      tags: form.tags
        .split(",")
        .map((t) => t.trim())
        .filter(Boolean),
      notes: form.notes,
      startedAt: form.startedAt,
      trialEndsAt: form.trialEndsAt,
    };

    try {
      if (initial) {
        await SubscriptionsService.Update(initial.id, input);
      } else {
        await SubscriptionsService.Create(input);
      }
      await loadAll();
      onclose();
    } catch (err) {
      formError = errorMessage(err);
    } finally {
      saving = false;
    }
  }
</script>

<Modal {open} title={initial ? "Edit subscription" : "New subscription"} {onclose}>
  <form onsubmit={submit}>
    <div class="form-grid">
      <div class="field span-2">
        <label for="sub-name">Service name</label>
        <input id="sub-name" class="input" bind:value={form.name} placeholder="Netflix, Spotify, …" required />
      </div>

      <div class="field">
        <label for="sub-vendor">Vendor</label>
        <input
          id="sub-vendor"
          class="input"
          bind:value={form.vendorName}
          oninput={() => (form.vendorId = "")}
          list="vendor-options"
          placeholder="Netflix Inc."
        />
        <datalist id="vendor-options">
          {#each store.vendors as vendor (vendor.id)}
            <option value={vendor.name}></option>
          {/each}
        </datalist>
      </div>

      <div class="field">
        <label for="sub-plan">Plan</label>
        <input id="sub-plan" class="input" bind:value={form.plan} placeholder="Standard, Family, …" />
      </div>

      <div class="field">
        <label for="sub-amount">Price</label>
        <input
          id="sub-amount"
          class="input"
          bind:value={form.amount}
          inputmode="decimal"
          placeholder="12.99"
          required
        />
      </div>

      <div class="field">
        <label for="sub-currency">Currency</label>
        <div class="row">
          <input
            id="sub-currency"
            class="input"
            bind:value={form.currency}
            onchange={onCurrencyInput}
            maxlength="10"
          />
          <input
            class="input"
            type="number"
            min="0"
            max="8"
            bind:value={form.scale}
            aria-label="Currency decimal places"
            style="width: 84px;"
          />
        </div>
      </div>

      <div class="field">
        <label for="sub-count">Billing cycle</label>
        <div class="row">
          <input
            id="sub-count"
            class="input"
            type="number"
            min="1"
            bind:value={form.intervalCount}
            style="width: 84px;"
          />
          <select class="select" bind:value={form.intervalUnit} aria-label="Billing unit">
            <option value="day">days</option>
            <option value="week">weeks</option>
            <option value="month">months</option>
            <option value="year">years</option>
          </select>
        </div>
      </div>

      <div class="field">
        <label for="sub-anchor">Anchor date</label>
        <input id="sub-anchor" class="input" type="date" bind:value={form.anchorDate} required />
      </div>

      <div class="field">
        <label for="sub-status">Status</label>
        <select id="sub-status" class="select" bind:value={form.status}>
          <option value="active">Active</option>
          <option value="trial">Trial</option>
          <option value="paused">Paused</option>
          <option value="cancelled">Cancelled</option>
          <option value="expired">Expired</option>
        </select>
      </div>

      <div class="field">
        <label for="sub-method">Payment method</label>
        <select id="sub-method" class="select" bind:value={form.paymentMethodId}>
          <option value="">No payment method</option>
          {#each activeMethods as pm (pm.id)}
            <option value={pm.id}>{pm.label}</option>
          {/each}
        </select>
      </div>

      <div class="field span-2">
        <label for="sub-tags">Tags</label>
        <input id="sub-tags" class="input" bind:value={form.tags} placeholder="work, household" />
      </div>

      <div class="field span-2">
        <label for="sub-notes">Notes</label>
        <textarea id="sub-notes" class="input" bind:value={form.notes}></textarea>
      </div>

      <label class="row span-2" style="gap: 8px;">
        <input type="checkbox" bind:checked={form.autoRenew} />
        <span>Renews automatically</span>
      </label>
    </div>

    {#if formError}
      <p class="banner error" role="alert" style="margin-top: 14px;">{formError}</p>
    {/if}

    <div class="modal-actions">
      <button type="button" class="btn ghost" onclick={onclose}>Cancel</button>
      <button type="submit" class="btn primary" disabled={saving}>
        {saving ? "Saving…" : initial ? "Save changes" : "Create subscription"}
      </button>
    </div>
  </form>
</Modal>
