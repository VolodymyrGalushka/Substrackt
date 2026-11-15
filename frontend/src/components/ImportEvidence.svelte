<script lang="ts">
  import type { Job } from "../lib/api";
  import { IngestionService, errorMessage } from "../lib/api";
  import Modal from "./Modal.svelte";
  import { todayISO } from "../lib/format";

  let {
    open = false,
    onclose,
    onqueued,
  }: { open?: boolean; onclose: () => void; onqueued?: (job: Job) => void } = $props();

  let form = $state(blank());
  let saving = $state(false);
  let formError = $state("");

  function blank() {
    return { title: "", vendorHint: "", occurredAt: todayISO(), content: "" };
  }

  $effect(() => {
    if (!open) return;
    form = blank();
    formError = "";
    saving = false;
  });

  async function submit(event: SubmitEvent) {
    event.preventDefault();
    formError = "";
    saving = true;
    try {
      const job = await IngestionService.IngestManual({
        title: form.title,
        vendorHint: form.vendorHint,
        occurredAt: form.occurredAt,
        content: form.content,
      });
      onqueued?.(job);
      onclose();
    } catch (err) {
      formError = errorMessage(err);
    } finally {
      saving = false;
    }
  }
</script>

<Modal {open} title="Import receipt or email" {onclose}>
  <form onsubmit={submit}>
    <p class="callout">
      Paste the text of a receipt, invoice or email. It is parsed locally; when a local
      model is configured in Settings it can also extract the details. Nothing is sent
      to a cloud service.
    </p>

    <div class="form-grid">
      <div class="field span-2">
        <label for="import-content">Receipt or email text</label>
        <textarea
          id="import-content"
          class="input code"
          rows="9"
          bind:value={form.content}
          placeholder={"From: Acme Cloud\nPlan: Pro\n$9.99 per month\nNext payment January 15, 2027"}
          required
        ></textarea>
      </div>

      <div class="field">
        <label for="import-title">Title (optional)</label>
        <input id="import-title" class="input" bind:value={form.title} placeholder="Acme invoice" />
      </div>

      <div class="field">
        <label for="import-vendor">Vendor hint (optional)</label>
        <input
          id="import-vendor"
          class="input"
          bind:value={form.vendorHint}
          placeholder="Acme Cloud"
        />
      </div>

      <div class="field">
        <label for="import-date">Received on</label>
        <input id="import-date" class="input" type="date" bind:value={form.occurredAt} />
      </div>
    </div>

    {#if formError}
      <p class="banner error" role="alert" style="margin-top: 14px;">{formError}</p>
    {/if}

    <div class="modal-actions">
      <button type="button" class="btn ghost" onclick={onclose}>Cancel</button>
      <button type="submit" class="btn primary" disabled={saving}>
        {saving ? "Queueing…" : "Queue import"}
      </button>
    </div>
  </form>
</Modal>
