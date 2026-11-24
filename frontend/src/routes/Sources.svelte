<script lang="ts">
  import { onMount } from "svelte";
  import { Events } from "@wailsio/runtime";
  import type {
    BrowserConnector,
    BrowserLoginStatus,
    BrowserRuntime,
    Source,
    SourceConfig,
    SourceInput,
    SourceTestResult,
  } from "../lib/api";
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
  let runtime = $state<BrowserRuntime | null>(null);
  let connectors = $state<BrowserConnector[]>([]);
  let installing = $state(false);
  let login = $state<Record<string, BrowserLoginStatus>>({});

  onMount(() => {
    void refreshRuntime();
    void refreshConnectors();
    const off = Events.On("browser:login", (event) => {
      const status = event.data as BrowserLoginStatus;
      if (!status?.sourceId) return;
      login = { ...login, [status.sourceId]: status };
      if (status.state === "saved" || status.state === "failed" || status.state === "cancelled") {
        void loadSources();
      }
    });
    return () => off();
  });

  async function refreshRuntime() {
    try {
      runtime = await SourcesService.BrowserRuntime();
    } catch {
      runtime = {
        ready: false,
        error: "Browser support is unavailable",
        selected: "auto",
        resolved: "",
        resolvedName: "",
        signIn: "default",
        options: [],
      };
    }
  }

  async function refreshConnectors() {
    try {
      connectors = (await SourcesService.BrowserConnectors()) ?? [];
      if (!form.config.connector && connectors.length > 0) {
        form.config.connector = connectors[0].id;
      }
    } catch {
      connectors = [];
    }
  }

  async function installBrowser() {
    installing = true;
    store.error = "";
    try {
      runtime = await SourcesService.InstallBrowser();
    } catch (err) {
      store.error = errorMessage(err);
      await refreshRuntime();
    } finally {
      installing = false;
    }
  }

  async function beginSignIn(src: Source) {
    login = { ...login, [src.id]: { sourceId: src.id, state: "starting", error: "" } };
    try {
      await SourcesService.BeginSignIn(src.id);
    } catch (err) {
      login = {
        ...login,
        [src.id]: { sourceId: src.id, state: "failed", error: errorMessage(err) },
      };
    }
  }

  async function cancelSignIn(src: Source) {
    try {
      await SourcesService.CancelSignIn(src.id);
    } catch (err) {
      store.error = errorMessage(err);
    }
  }

  function loginState(src: Source): BrowserLoginStatus | null {
    const status = login[src.id];
    if (status) return status;
    return null;
  }

  function loginLabel(src: Source): string {
    const status = loginState(src);
    if (status) {
      if (status.state === "saved") {
        // The session may have expired since: trust the refreshed source.
        return src.hasSecret ? "Signed in" : "Not signed in";
      }
      switch (status.state) {
        case "starting":
          return runtime?.signIn === "default" ? "Opening your browser…" : "Starting…";
        case "installing":
          return "Installing browser…";
        case "waiting":
          return runtime?.signIn === "default"
            ? "Waiting for sign-in in your browser…"
            : "Waiting for sign-in…";
        case "cancelled":
          return "Sign-in cancelled";
        case "failed":
          return status.error || "Sign-in failed";
      }
    }
    return src.hasSecret ? "Signed in" : "Not signed in";
  }

  function isSigningIn(src: Source): boolean {
    const state = loginState(src)?.state;
    return state === "starting" || state === "installing" || state === "waiting";
  }

  function browserConnector(id: string): BrowserConnector | undefined {
    return connectors.find((item) => item.id === id);
  }

  function onKindChange(event: Event) {
    const target = event.currentTarget as HTMLSelectElement;
    form.kind = target.value;
    if (form.kind === "browser") {
      form.config.connector = form.config.connector || connectors[0]?.id || "";
      form.config.timeoutSeconds = form.config.timeoutSeconds || 30;
    }
  }

  const providers = [
    {
      id: "yahoo",
      label: "Yahoo Mail",
      address: "imap.mail.yahoo.com:993",
      security: "tls",
      hint: "Use your full Yahoo address as the username. If Yahoo rejects your account password (common with 2-Step Verification or Account Key), generate an app password under Account Security.",
    },
    {
      id: "gmail",
      label: "Gmail",
      address: "imap.gmail.com:993",
      security: "tls",
      hint: "The username is usually your full Gmail address. If 2-Step Verification is on, Google may require an app password (Google Account → Security → App passwords).",
    },
    {
      id: "icloud",
      label: "iCloud Mail",
      address: "imap.mail.me.com:993",
      security: "tls",
      hint: "The username is usually your iCloud address. Apple may require an app-specific password from appleid.apple.com.",
    },
    {
      id: "fastmail",
      label: "Fastmail",
      address: "imap.fastmail.com:993",
      security: "tls",
      hint: "Enable IMAP in Fastmail settings. Fastmail may require an app password; your account password can be rejected.",
    },
    {
      id: "outlook",
      label: "Outlook / Microsoft 365",
      address: "outlook.office365.com:993",
      security: "tls",
      hint: "Microsoft may disable basic password authentication for IMAP. Try your password first; if it is rejected, switch Authentication to XOAUTH2 and paste an access token.",
    },
    {
      id: "custom",
      label: "Custom IMAP",
      address: "",
      security: "tls",
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
        connector: "",
        billingUrl: "",
        timeoutSeconds: 30,
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
        connector: form.config.connector,
        billingUrl: form.config.billingUrl,
        timeoutSeconds: Number(form.config.timeoutSeconds) || 0,
      },
      password: form.password,
    };
  }

  function applyProvider(id: string) {
    provider = id;
    const preset = providers.find((item) => item.id === id);
    if (!preset || preset.id === "custom") return;
    // Presets only fill the server and encryption; the authentication method and
    // credential stay the user's choice (app passwords/tokens are optional).
    form.config.address = preset.address;
    form.config.security = preset.security;
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
      : "Use your account password, or an app password if the provider supports it. App passwords are optional; they are needed only when the provider rejects your account password (common with two-factor authentication).",
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
        await loadSources();
        close();
      } else {
        const created = await SourcesService.Create(payload());
        await loadSources();
        close();
        if (created.kind === "browser") {
          // Saving a browser source only creates the placeholder; open the
          // vendor sign-in immediately so the next step is obvious.
          void beginSignIn(created);
        }
      }
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
      testResult = { ok: false, mailbox: "", messages: 0, message: "", error: errorMessage(err) };
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
    const detail =
      src.kind === "browser"
        ? "Its saved browser session is removed too."
        : "Its stored password and sync position are removed too.";
    if (!confirm(`Delete "${src.name}"? ${detail}`)) {
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
        Ingestion channels that feed the review queue. Email passwords and browser sessions go to
        the OS keyring or an encrypted file, never to the database.
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

  {#if runtime && !runtime.ready}
    <div class="banner" role="status">
      <span>
        {#if runtime.resolved}
          {runtime.signIn === "default" ? "Headless sync" : "Browser sign-in"} needs the
          Playwright driver
          {runtime.error ? `(${runtime.error})` : ""}.
        {:else}
          Headless sync browser is unavailable:
          {runtime.error || "the selected browser is not installed."}
          Change it in Settings.
        {/if}
      </span>
      {#if runtime.resolved}
        <button class="btn sm" onclick={installBrowser} disabled={installing}>
          {installing ? "Installing…" : "Install sync runtime"}
        </button>
      {/if}
    </div>
  {:else if runtime?.resolvedName}
    <p class="field-hint" style="margin: -4px 0 12px;">
      {#if runtime.signIn === "default"}
        Sign-in opens a new tab in your default browser; headless sync uses
        {runtime.resolvedName}.
      {:else}
        Browser sign-in uses {runtime.resolvedName}.
      {/if}
      <a href="#/settings">Change in Settings</a>
    </p>
  {/if}

  {#if sources.length === 0}
    <div class="card empty">
      <strong>No sources yet</strong>
      <span>
        Connect a mailbox to pull subscription receipts, or sign in to a vendor billing page.
        Detected subscriptions wait in the review queue unless a local model is confident.
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
              {#if src.kind === "browser"}
                <div class="cell-sub mono">
                  {browserConnector(src.config.connector)?.name ?? src.config.connector}
                </div>
                <div class="cell-sub mono">
                  {src.config.billingUrl ||
                    browserConnector(src.config.connector)?.billingUrl ||
                    "Default billing page"}
                </div>
              {:else}
                <div class="cell-sub mono">{src.config.username}</div>
                <div class="cell-sub mono">{src.config.address} · {src.config.mailbox}</div>
              {/if}
            </div>
            <span class="badge" class:active={src.enabled}>
              {src.enabled ? "Enabled" : "Paused"}
            </span>
          </div>

          <p class="cell-sub" style="margin-top: 10px;">
            {scheduleLabel(src.scheduleMinutes)} · last sync {when(src.lastSyncAt)}
          </p>
          {#if src.kind === "browser"}
            <p class="cell-sub" class:error-text={!src.hasSecret && !isSigningIn(src)}>
              {loginLabel(src)}
            </p>
          {:else if !src.hasSecret}
            <p class="cell-sub error-text">No password saved. Edit the source to add one.</p>
          {/if}
          {#if src.lastError}
            <p class="cell-sub error-text">{src.lastError}</p>
          {/if}

          <div class="row" style="margin-top: 14px; justify-content: flex-end;">
            {#if src.kind === "browser"}
              {#if isSigningIn(src)}
                <button
                  class="btn ghost sm"
                  onclick={() => cancelSignIn(src)}
                  disabled={busyId === src.id}
                >
                  Cancel sign-in
                </button>
              {:else}
                <button
                  class="btn ghost sm"
                  onclick={() => beginSignIn(src)}
                  disabled={busyId === src.id}
                >
                  {src.hasSecret ? "Sign in again" : "Sign in"}
                </button>
              {/if}
            {/if}
            <button
              class="btn ghost sm"
              onclick={() => syncNow(src)}
              disabled={busyId === src.id || !src.enabled || (src.kind === "browser" && !src.hasSecret)}
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

<Modal open={editorOpen} title={editing ? "Edit source" : "New source"} onclose={close}>
  <form onsubmit={submit}>
    <div class="form-grid">
      <div class="field span-2">
        <label for="source-kind">Channel</label>
        <select
          id="source-kind"
          class="select"
          value={form.kind}
          onchange={onKindChange}
          disabled={editing != null}
        >
          <option value="email">Email (IMAP)</option>
          <option value="browser">Vendor website (browser)</option>
        </select>
        {#if editing == null}
          <span class="field-hint">
            Email pulls receipts from a mailbox. Browser signs in to a vendor billing page.
          </span>
        {/if}
      </div>

      <div class="field span-2">
        <label for="source-name">Name</label>
        <input
          id="source-name"
          class="input"
          bind:value={form.name}
          placeholder={form.kind === "browser" ? "GitHub billing" : "Personal mail"}
          required
        />
      </div>

      {#if form.kind === "browser"}
        <div class="field span-2">
          <label for="source-connector">Vendor connector</label>
          <select id="source-connector" class="select" bind:value={form.config.connector}>
            {#each connectors as connector (connector.id)}
              <option value={connector.id}>{connector.name}</option>
            {/each}
          </select>
          <span class="field-hint">
            Signing in opens a separate browser window. Your session is stored as a secret; it is
            never written to the database.
          </span>
        </div>

        <div class="field span-2">
          <label for="source-billing-url">Billing page (optional)</label>
          <input
            id="source-billing-url"
            class="input"
            bind:value={form.config.billingUrl}
            placeholder={browserConnector(form.config.connector)?.billingUrl ?? ""}
          />
          <span class="field-hint">
            Leave blank to use the connector default. An override must stay on the same vendor
            host.
          </span>
        </div>

        <div class="field">
          <label for="source-timeout">Page timeout (seconds)</label>
          <input
            id="source-timeout"
            class="input"
            type="number"
            min="10"
            max="300"
            bind:value={form.config.timeoutSeconds}
          />
        </div>

        <div class="field">
          <label for="source-browser-schedule">Sync schedule</label>
          <select id="source-browser-schedule" class="select" bind:value={form.scheduleMinutes}>
            <option value={5}>every 5 minutes</option>
            <option value={15}>every 15 minutes</option>
            <option value={30}>every 30 minutes</option>
            <option value={60}>every hour</option>
            <option value={360}>every 6 hours</option>
            <option value={1440}>every day</option>
          </select>
        </div>

        <label class="row" style="gap: 8px;">
          <input type="checkbox" bind:checked={form.enabled} />
          <span>Enabled</span>
        </label>
      {:else}
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
            Choose the preset that matches your mail provider; it fills the server and encryption
            only. Authentication stays as you set it.
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
      {/if}
    </div>

    {#if formError}
      <p class="banner error" role="alert" style="margin-top: 14px;">{formError}</p>
    {/if}

    {#if testResult}
      <div class="test-result" class:ok={testResult.ok} class:bad={!testResult.ok}>
        {#if testResult.ok}
          <strong>{testResult.message || `Connected to ${testResult.mailbox}`}</strong>
          {#if testResult.messages > 0}
            <span>{testResult.messages} message(s) in the mailbox.</span>
          {/if}
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
