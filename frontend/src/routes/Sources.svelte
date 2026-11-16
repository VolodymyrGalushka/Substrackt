<script lang="ts">
  import type { Source, SourceConfig, SourceInput, SourceTestResult } from "../lib/api";
  import { SourcesService, errorMessage } from "../lib/api";
  import Modal from "../components/Modal.svelte";
  import { loadJobs, loadSources, store } from "../lib/state.svelte";

  type Form = {
    kind: string;
    name: string;
    enabled: boolean;
    scheduleMinutes: number;
    config: SourceConfig;
    password: string;
  };

  let editorOpen = $state(false);
  let editing = $state<Source | null>(null);
  let saving = $state(false);
  let testing = $state(false);
  let busyId = $state("");
  let formError = $state("");
  let notice = $state("");
  let testResult = $state<SourceTestResult | null>(null);

  function blank(): Form {
    return {
      kind: "email",
      name: "",
      enabled: true,
      scheduleMinutes: 15,
      config: {
        address: "imap.example.com:993",
        username: "",
        mailbox: "INBOX",
        insecure: false,
        initialDays: 30,
        maxMessages: 50,
      },
      password: "",
    };
  }

  let form = $state<Form>(blank());

  $effect(() => {
    if (!editorOpen) return;
    formError = "";
    testResult = null;
    const src = editing;
    if (!src) {
      form = blank();
      return;
    }
    form = {
      kind: src.kind || "email",
      name: src.name,
      enabled: src.enabled,
      scheduleMinutes: src.scheduleMinutes,
      config: { ...src.config },
      password: "",
    };
  });

  const sources = $derived(store.sources);

  function openNew() {
    editing = null;
    editorOpen = true;
  }

  function openEdit(src: Source) {
    editing = src;
    editorOpen = true;
  }

  function close() {
    editorOpen = false;
  }

  function payload(): SourceInput {
    return {
      kind: form.kind || "email",
      name: form.name,
      enabled: form.enabled,
      scheduleMinutes: Number(form.scheduleMinutes) || 0,
      config: {
        address: form.config.address,
        username: form.config.username,
        mailbox: form.config.mailbox,
        insecure: form.config.insecure,
        initialDays: Number(form.config.initialDays) || 0,
        maxMessages: Number(form.config.maxMessages) || 0,
      },
      password: form.password,
    };
  }

  function inputFrom(src: Source): SourceInput {
    return {
      kind: src.kind,
      name: src.name,
      enabled: src.enabled,
      scheduleMinutes: src.scheduleMinutes,
      config: { ...src.config },
      password: "",
    };
  }

  async function submit(event: SubmitEvent) {
    event.preventDefault();
    formError = "";
    notice = "";
    saving = true;
    try {
      if (editing) {
        await SourcesService.Update(editing.id, payload());
      } else {
        await SourcesService.Create(payload());
      }
      await loadSources();
      close();
    } catch (err) {
      formError = errorMessage(err);
    } finally {
      saving = false;
    }
  }

  async function testConnection() {
    testing = true;
    testResult = null;
    try {
      testResult = await SourcesService.Test(editing?.id ?? "", payload());
    } catch (err) {
      testResult = { ok: false, mailbox: "", messages: 0, error: errorMessage(err) };
    } finally {
      testing = false;
    }
  }

  async function syncNow(src: Source) {
    busyId = src.id;
    notice = "";
    try {
      await SourcesService.SyncNow(src.id);
      notice = `Sync queued for ${src.name}. Progress appears in Activity.`;
      await loadJobs();
    } catch (err) {
      store.error = errorMessage(err);
    } finally {
      busyId = "";
    }
  }

  async function toggle(src: Source) {
    busyId = src.id;
    notice = "";
    try {
      await SourcesService.Update(src.id, { ...inputFrom(src), enabled: !src.enabled });
      await loadSources();
    } catch (err) {
      store.error = errorMessage(err);
    } finally {
      busyId = "";
    }
  }

  async function remove(src: Source) {
    if (!confirm(`Delete "${src.name}"? Its stored password and sync position are removed too.`)) {
      return;
    }
    busyId = src.id;
    try {
      await SourcesService.Delete(src.id);
      await loadSources();
    } catch (err) {
      store.error = errorMessage(err);
    } finally {
      busyId = "";
    }
  }

  function scheduleLabel(minutes: number): string {
    if (!minutes || minutes <= 0) return "manual only";
    if (minutes % 1440 === 0) return `every ${minutes / 1440} d`;
    if (minutes % 60 === 0) return `every ${minutes / 60} h`;
    if (minutes < 60) return `every ${minutes} min`;
    return `every ${minutes} min`;
  }

  function when(stamp: string): string {
    if (!stamp) return "never";
    const date = new Date(stamp);
    if (Number.isNaN(date.getTime())) return stamp;
    return date.toLocaleString(undefined, {
      month: "short",
      day: "numeric",
      hour: "2-digit",
      minute: "2-digit",
    });
  }
</script>

<div class="page">
  <header class="page-head">
    <div>
      <h1>Sources</h1>
      <p>
        Ingestion channels that feed the review queue. Passwords go to the OS keyring or an
        encrypted file, never to the database.
      </p>
    </div>
    <div class="row">
      <button class="btn ghost" onclick={() => void loadSources()}>Refresh</button>
      <button class="btn primary" onclick={openNew}>+ Add source</button>
    </div>
  </header>

  {#if notice}
    <div class="banner ok" role="status">
      <span>{notice}</span>
      <button class="btn ghost sm" onclick={() => (notice = "")}>Dismiss</button>
    </div>
  {/if}

  {#if sources.length === 0}
    <div class="card empty">
      <strong>No sources yet</strong>
      <span>
        Connect a mailbox to pull subscription receipts and invoices automatically. Detected
        subscriptions still wait in the review queue unless a local model is confident.
      </span>
      <button class="btn primary" onclick={openNew}>+ Add source</button>
    </div>
  {:else}
    <div class="pm-grid">
      {#each sources as src (src.id)}
        <article class="card">
          <div class="row" style="justify-content: space-between; align-items: flex-start;">
            <div>
              <div class="cell-title">{src.name}</div>
              <div class="cell-sub mono">{src.config.username}</div>
              <div class="cell-sub mono">{src.config.address} · {src.config.mailbox}</div>
            </div>
            <span class="badge" class:active={src.enabled}>
              {src.enabled ? "Enabled" : "Paused"}
            </span>
          </div>

          <p class="cell-sub" style="margin-top: 10px;">
            {scheduleLabel(src.scheduleMinutes)} · last sync {when(src.lastSyncAt)}
          </p>
          {#if !src.hasSecret}
            <p class="cell-sub error-text">No password saved. Edit the source to add one.</p>
          {/if}
          {#if src.lastError}
            <p class="cell-sub error-text">{src.lastError}</p>
          {/if}

          <div class="row" style="margin-top: 14px; justify-content: flex-end;">
            <button
              class="btn ghost sm"
              onclick={() => syncNow(src)}
              disabled={busyId === src.id || !src.enabled}
            >
              Sync now
            </button>
            <button class="btn ghost sm" onclick={() => toggle(src)} disabled={busyId === src.id}>
              {src.enabled ? "Pause" : "Enable"}
            </button>
            <button class="btn ghost sm" onclick={() => openEdit(src)}>Edit</button>
            <button class="btn danger sm" onclick={() => remove(src)} disabled={busyId === src.id}>
              Delete
            </button>
          </div>
        </article>
      {/each}
    </div>
  {/if}
</div>

<Modal open={editorOpen} title={editing ? "Edit source" : "New email source"} onclose={close}>
  <form onsubmit={submit}>
    <div class="form-grid">
      <div class="field span-2">
        <label for="source-name">Name</label>
        <input
          id="source-name"
          class="input"
          bind:value={form.name}
          placeholder="Personal mail"
          required
        />
      </div>

      <div class="field span-2">
        <label for="source-address">IMAP server</label>
        <input
          id="source-address"
          class="input"
          bind:value={form.config.address}
          placeholder="imap.example.com:993"
          required
        />
        <span class="field-hint">Use port 993 with TLS. Plain TCP is for local test servers only.</span>
      </div>

      <div class="field">
        <label for="source-username">Username</label>
        <input
          id="source-username"
          class="input"
          bind:value={form.config.username}
          autocomplete="username"
          required
        />
      </div>

      <div class="field">
        <label for="source-password">Password</label>
        <input
          id="source-password"
          class="input"
          type="password"
          bind:value={form.password}
          autocomplete="new-password"
          placeholder={editing ? "Leave blank to keep the saved password" : ""}
        />
      </div>

      <div class="field">
        <label for="source-mailbox">Mailbox</label>
        <input id="source-mailbox" class="input" bind:value={form.config.mailbox} placeholder="INBOX" />
      </div>

      <div class="field">
        <label for="source-schedule">Sync schedule</label>
        <select id="source-schedule" class="select" bind:value={form.scheduleMinutes}>
          <option value={5}>every 5 minutes</option>
          <option value={15}>every 15 minutes</option>
          <option value={30}>every 30 minutes</option>
          <option value={60}>every hour</option>
          <option value={360}>every 6 hours</option>
          <option value={1440}>every day</option>
        </select>
      </div>

      <div class="field">
        <label for="source-days">First sync window</label>
        <input
          id="source-days"
          class="input"
          type="number"
          min="1"
          max="3650"
          bind:value={form.config.initialDays}
        />
        <span class="field-hint">Days of mail to scan before a cursor exists.</span>
      </div>

      <div class="field">
        <label for="source-max">Messages per sync</label>
        <input
          id="source-max"
          class="input"
          type="number"
          min="1"
          max="500"
          bind:value={form.config.maxMessages}
        />
      </div>

      <label class="row" style="gap: 8px;">
        <input type="checkbox" bind:checked={form.enabled} />
        <span>Enabled</span>
      </label>

      <label class="row" style="gap: 8px;">
        <input type="checkbox" bind:checked={form.config.insecure} />
        <span>Plain TCP (local test servers only)</span>
      </label>
    </div>

    {#if formError}
      <p class="banner error" role="alert" style="margin-top: 14px;">{formError}</p>
    {/if}

    {#if testResult}
      <div class="test-result" class:ok={testResult.ok} class:bad={!testResult.ok}>
        {#if testResult.ok}
          <strong>Connected to {testResult.mailbox}</strong>
          <span>{testResult.messages} message(s) in the mailbox.</span>
        {:else}
          <strong>Connection failed</strong>
          <span>{testResult.error}</span>
        {/if}
      </div>
    {/if}

    <div class="modal-actions">
      <button type="button" class="btn ghost" onclick={close}>Cancel</button>
      <button type="button" class="btn" onclick={testConnection} disabled={testing || saving}>
        {testing ? "Testing…" : "Test connection"}
      </button>
      <button type="submit" class="btn primary" disabled={saving || testing}>
        {saving ? "Saving…" : editing ? "Save changes" : "Add source"}
      </button>
    </div>
  </form>
</Modal>
