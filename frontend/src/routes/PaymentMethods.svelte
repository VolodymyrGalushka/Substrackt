<script lang="ts">
  import type { PaymentMethod, PaymentMethodInput } from "../lib/api";
  import { PaymentMethodsService, errorMessage } from "../lib/api";
  import Modal from "../components/Modal.svelte";
  import { paymentTypeLabel } from "../lib/format";
  import { loadAll, store } from "../lib/state.svelte";

  let editorOpen = $state(false);
  let editing = $state<PaymentMethod | null>(null);
  let saving = $state(false);
  let formError = $state("");

  function blank() {
    return {
      label: "",
      type: "card",
      issuer: "",
      network: "",
      last4: "",
      expMonth: 0,
      expYear: 0,
      walletAddress: "",
      chain: "",
      billingCurrency: "",
      active: true,
      notes: "",
    };
  }

  let form = $state(blank());

  const types = [
    "card",
    "bank_account",
    "paypal",
    "apple_pay",
    "google_pay",
    "crypto",
    "store_credit",
    "other",
  ];

  $effect(() => {
    if (!editorOpen) return;
    formError = "";
    const pm = editing;
    if (!pm) {
      form = blank();
      return;
    }
    form = {
      label: pm.label,
      type: pm.type,
      issuer: pm.issuer,
      network: pm.network,
      last4: pm.last4,
      expMonth: pm.expMonth,
      expYear: pm.expYear,
      walletAddress: pm.walletAddress,
      chain: pm.chain,
      billingCurrency: pm.billingCurrency,
      active: pm.active,
      notes: pm.notes,
    };
  });

  function openNew() {
    editing = null;
    editorOpen = true;
  }

  function openEdit(pm: PaymentMethod) {
    editing = pm;
    editorOpen = true;
  }

  function close() {
    editorOpen = false;
  }

  function detail(pm: PaymentMethod): string {
    const parts: string[] = [paymentTypeLabel(pm.type)];
    if (pm.last4) parts.push(`•••• ${pm.last4}`);
    if (pm.issuer) parts.push(pm.issuer);
    if (pm.network && pm.network !== pm.issuer) parts.push(pm.network);
    if (pm.expMonth && pm.expYear) {
      parts.push(`exp ${String(pm.expMonth).padStart(2, "0")}/${String(pm.expYear).slice(-2)}`);
    }
    if (pm.chain) parts.push(pm.chain);
    return parts.join(" · ");
  }

  async function submit(event: SubmitEvent) {
    event.preventDefault();
    formError = "";
    saving = true;

    const input: PaymentMethodInput = {
      label: form.label,
      type: form.type,
      issuer: form.issuer,
      network: form.network,
      last4: form.last4,
      expMonth: Number(form.expMonth) || 0,
      expYear: Number(form.expYear) || 0,
      walletAddress: form.walletAddress,
      chain: form.chain,
      billingCurrency: form.billingCurrency,
      active: form.active,
      notes: form.notes,
    };

    try {
      if (editing) {
        await PaymentMethodsService.Update(editing.id, input);
      } else {
        await PaymentMethodsService.Create(input);
      }
      await loadAll();
      close();
    } catch (err) {
      formError = errorMessage(err);
    } finally {
      saving = false;
    }
  }

  async function toggleActive(pm: PaymentMethod) {
    try {
      await PaymentMethodsService.SetActive(pm.id, !pm.active);
      await loadAll();
    } catch (err) {
      store.error = errorMessage(err);
    }
  }

  async function remove(pm: PaymentMethod) {
    if (!confirm(`Delete "${pm.label}"? Subscriptions using it keep their data but lose the link.`)) {
      return;
    }
    try {
      await PaymentMethodsService.Delete(pm.id);
      await loadAll();
    } catch (err) {
      store.error = errorMessage(err);
    }
  }
</script>

<div class="page">
  <header class="page-head">
    <div>
      <h1>Payment methods</h1>
      <p>Cards, wallets and accounts used to pay for subscriptions. Only non-sensitive details are stored.</p>
    </div>
    <button class="btn primary" onclick={openNew}>+ Add payment method</button>
  </header>

  {#if store.paymentMethods.length === 0}
    <div class="card empty">
      <strong>No payment methods yet</strong>
      <span>Add a card, bank account or crypto wallet to link subscriptions to how you pay.</span>
      <button class="btn primary" onclick={openNew}>+ Add payment method</button>
    </div>
  {:else}
    <div class="pm-grid">
      {#each store.paymentMethods as pm (pm.id)}
        <article class="card">
          <div class="row" style="justify-content: space-between; align-items: flex-start;">
            <div>
              <div class="cell-title">{pm.label}</div>
              <div class="cell-sub">{detail(pm)}</div>
            </div>
            <span class="badge" class:active={pm.active}>
              {pm.active ? "Active" : "Archived"}
            </span>
          </div>

          {#if pm.walletAddress}
            <p class="cell-sub mono" style="margin-top: 10px; word-break: break-all;">{pm.walletAddress}</p>
          {/if}
          {#if pm.notes}
            <p class="cell-sub" style="margin-top: 8px;">{pm.notes}</p>
          {/if}

          <div class="row" style="margin-top: 14px; justify-content: flex-end;">
            <button class="btn ghost sm" onclick={() => openEdit(pm)}>Edit</button>
            <button class="btn ghost sm" onclick={() => toggleActive(pm)}>
              {pm.active ? "Archive" : "Restore"}
            </button>
            <button class="btn danger sm" onclick={() => remove(pm)}>Delete</button>
          </div>
        </article>
      {/each}
    </div>
  {/if}
</div>

<Modal open={editorOpen} title={editing ? "Edit payment method" : "New payment method"} onclose={close}>
  <form onsubmit={submit}>
    <div class="form-grid">
      <div class="field span-2">
        <label for="pm-label">Label</label>
        <input id="pm-label" class="input" bind:value={form.label} placeholder="Personal Visa" required />
      </div>

      <div class="field">
        <label for="pm-type">Type</label>
        <select id="pm-type" class="select" bind:value={form.type}>
          {#each types as type (type)}
            <option value={type}>{paymentTypeLabel(type)}</option>
          {/each}
        </select>
      </div>

      <div class="field">
        <label for="pm-issuer">Issuer</label>
        <input id="pm-issuer" class="input" bind:value={form.issuer} placeholder="Revolut, Chase, …" />
      </div>

      {#if form.type === "card"}
        <div class="field">
          <label for="pm-network">Network</label>
          <input id="pm-network" class="input" bind:value={form.network} placeholder="Visa, Mastercard, …" />
        </div>

        <div class="field">
          <label for="pm-last4">Last 4 digits</label>
          <input id="pm-last4" class="input" bind:value={form.last4} maxlength="4" inputmode="numeric" />
        </div>

        <div class="field">
          <label for="pm-exp">Expiry</label>
          <div class="row">
            <input
              id="pm-exp"
              class="input"
              type="number"
              min="1"
              max="12"
              bind:value={form.expMonth}
              placeholder="MM"
              aria-label="Expiry month"
              style="width: 80px;"
            />
            <input
              class="input"
              type="number"
              min="2000"
              max="2100"
              bind:value={form.expYear}
              placeholder="YYYY"
              aria-label="Expiry year"
              style="width: 100px;"
            />
          </div>
        </div>
      {:else if form.type === "crypto"}
        <div class="field">
          <label for="pm-chain">Chain</label>
          <input id="pm-chain" class="input" bind:value={form.chain} placeholder="Ethereum, Solana, Bitcoin…" />
        </div>

        <div class="field span-2">
          <label for="pm-wallet">Wallet address</label>
          <input id="pm-wallet" class="input mono" bind:value={form.walletAddress} />
        </div>
      {/if}

      <div class="field">
        <label for="pm-currency">Billing currency</label>
        <input id="pm-currency" class="input" bind:value={form.billingCurrency} placeholder="USD" maxlength="10" />
      </div>

      <label class="row span-2" style="gap: 8px;">
        <input type="checkbox" bind:checked={form.active} />
        <span>Active</span>
      </label>

      <div class="field span-2">
        <label for="pm-notes">Notes</label>
        <textarea id="pm-notes" class="input" bind:value={form.notes}></textarea>
      </div>
    </div>

    {#if formError}
      <p class="banner error" role="alert" style="margin-top: 14px;">{formError}</p>
    {/if}

    <div class="modal-actions">
      <button type="button" class="btn ghost" onclick={close}>Cancel</button>
      <button type="submit" class="btn primary" disabled={saving}>
        {saving ? "Saving…" : editing ? "Save changes" : "Create payment method"}
      </button>
    </div>
  </form>
</Modal>
