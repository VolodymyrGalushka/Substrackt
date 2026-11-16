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
  let showPassword = $state(false);
  let revealing = $state(false);
  let provider = $state("custom");

  const providers = [
    {
      id: "yahoo",
      label: "Yahoo Mail",
      address: "imap.mail.yahoo.com:993",
      security: "tls",
      auth: "auto",
      hint: "Yahoo requires an app password: Yahoo Account Security → Generate app password. Use your full Yahoo address as the username.",
    },
    {
      id: "gmail",
      label: "Gmail",
      address: "imap.gmail.com:993",
      security: "tls",
      auth: "auto",
      hint: "With 2-Step Verification enabled, Google requires an app password (Google Account → Security → App passwords).",
    },
    {
      id: "icloud",
      label: "iCloud Mail",
      address: "imap.mail.me.com:993",
      security: "tls",
      auth: "auto",
      hint: "Create an app-specific password at appleid.apple.com. The username is usually your iCloud address.",
    },
    {
      id: "fastmail",
      label: "Fastmail",
      address: "imap.fastmail.com:993",
      security: "tls",
      auth: "auto",
      hint: "Enable IMAP in Fastmail settings and create an app password. Your account password may be rejected.",
    },
    {
      id: "outlook",
      label: "Outlook / Microsoft 365",
      address: "outlook.office365.com:993",
      security: "tls",
      auth: "xoauth2",
      hint: "Microsoft disabled basic password authentication for IMAP. Use an OAuth2 access token (XOAUTH2) instead of a password.",
    },
    {
      id: "custom",
      label: "Custom IMAP",
      address: "",
      security: "tls",
      auth: "auto",
      hint: "",
    },
  ] as const;

  const activeProvider = $derived(
    providers.find((item) => item.id === provider) ?? providers[providers.length - 1],
  );

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
        security: "tls",
        auth: "auto",
        tlsHost: "",
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
    showPassword = false;
    revealing = false;
    const src = editing;
    if (!src) {
      form = blank();
      provider = "custom";
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
    const match = providers.find(
      (item) => item.id !== "custom" && item.address && item.address === src.config.address,
    );
    provider = match?.id ?? "custom";
  });

  // OAuth2 needs an encrypted channel; keep the form consistent instead of
  // letting the save fail server-side.
  $effect(() => {
    if (form.config.security === "none" && form.config.auth === "xoauth2") {
      form.config.auth = "auto";
    }
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
        security: form.config.security,
        auth: form.config.auth,
        tlsHost: form.config.tlsHost,
        initialDays: Number(form.config.initialDays) || 0,
        maxMessages: Number(form.config.maxMessages) || 0,
      },
      password: form.password,
    };
  }

  function applyProvider(id: string) {
    provider = id;
    const preset = providers.find((item) => item.id === id);
    if (!preset || preset.id === "custom") return;
    form.config.address = preset.address;
    form.config.security = preset.security;
    form.config.auth = preset.auth;
  }

  function onProviderChange(event: Event) {
    const target = event.currentTarget as HTMLSelectElement;
    applyProvider(target.value);
  }

  async function revealSaved() {
    if (!editing) return;
    revealing = true;
    formError = "";
    try {
      form.password = await SourcesService.RevealPassword(editing.id);
      showPassword = true;
    } catch (err) {
      formError = errorMessage(err);
    } finally {
      revealing = false;
    }
  }

  const passwordHint = $derived(
    form.config.auth === "xoauth2"
      ? "Paste a short-lived OAuth2 access token. If your provider supports app passwords, they are usually more practical for scheduled syncs."
      : "Use an app password when the provider has two-factor authentication (Yahoo, Gmail, iCloud). Account passwords are often rejected.",
  );

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
        <label for="source-provider">Provider preset</label>
        <select
          id="source-provider"
          class="select"
          value={provider}
          onchange={onProviderChange}
        >
          {#each providers as preset (preset.id)}
            <option value={preset.id}>{preset.label}</option>
          {/each}
        </select>
        {#if activeProvider.hint}
          <span class="field-hint">{activeProvider.hint}</span>
        {:else}
          <span class="field-hint">
            Choose the preset that matches your mail provider; it fills the server, port and
            authentication method.
          </span>
        {/if}
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
        <span class="field-hint">
          Host and port. Use 993 with TLS or 143 with STARTTLS; plain TCP is for local test
          servers only.
        </span>
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
        <label for="source-mailbox">Mailbox</label>
        <input id="source-mailbox" class="input" bind:value={form.config.mailbox} placeholder="INBOX" />
      </div>

      <div class="field span-2">
        <label for="source-password">
          {form.config.auth === "xoauth2" ? "OAuth2 access token" : "Password / app password"}
        </label>
        <div class="row" style="display: flex; gap: 8px; flex-wrap: nowrap; align-items: center;">
          <input
            id="source-password"
            class="input"
            type={showPassword ? "text" : "password"}
            bind:value={form.password}
            autocomplete={form.config.auth === "xoauth2" ? "off" : "new-password"}
            placeholder={editing ? "Leave blank to keep the saved credential" : ""}
            style="flex: 1; min-width: 0;"
          />
          <button
            type="button"
            class="btn ghost sm"
            onclick={() => (showPassword = !showPassword)}
            aria-label={showPassword ? "Hide password" : "Show password"}
            style="white-space: nowrap;"
          >
            {showPassword ? "Hide" : "Show"}
          </button>
          {#if editing}
            <button
              type="button"
              class="btn ghost sm"
              onclick={revealSaved}
              disabled={revealing || !editing.hasSecret}
              title="Load the saved password into this field"
              style="white-space: nowrap;"
            >
              {revealing ? "Loading…" : "Reveal saved"}
            </button>
          {/if}
        </div>
        <span class="field-hint">{passwordHint}</span>
      </div>

      <div class="field">
        <label for="source-security">Encryption</label>
        <select id="source-security" class="select" bind:value={form.config.security}>
          <option value="tls">TLS (implicit, usually 993)</option>
          <option value="starttls">STARTTLS (usually 143)</option>
          <option value="none">None (local test servers only)</option>
        </select>
      </div>

      <div class="field">
        <label for="source-auth">Authentication</label>
        <select id="source-auth" class="select" bind:value={form.config.auth}>
          <option value="auto">Auto (recommended)</option>
          <option value="login">LOGIN command</option>
          <option value="plain">SASL PLAIN</option>
          <option value="xoauth2">XOAUTH2 access token</option>
        </select>
      </div>

      {#if form.config.security !== "none"}
        <div class="field span-2">
          <label for="source-tlshost">Certificate hostname (optional)</label>
          <input
            id="source-tlshost"
            class="input"
            bind:value={form.config.tlsHost}
            placeholder="mail.example.com"
          />
          <span class="field-hint">
            Set this only when the certificate name differs from the server address, such as an
            IP address or a LAN alias.
          </span>
        </div>
      {/if}

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
