<script lang="ts">
  import { onMount } from "svelte";
  import type { BrowserRuntime, LLMTestResult, Settings } from "../lib/api";
  import { SettingsService, SourcesService, errorMessage } from "../lib/api";
  import { loadLLMUsage, loadSettings, store } from "../lib/state.svelte";
  import { setTheme, theme, type ThemeMode } from "../lib/theme.svelte";

  let form = $state<Settings>(blank());
  let saving = $state(false);
  let testing = $state(false);
  let notice = $state("");
  let formError = $state("");
  let testResult = $state<LLMTestResult | null>(null);
  let runtimeStatus = $state<BrowserRuntime | null>(null);
  let loaded = $state(false);

  const presets = [
    { label: "Ollama", url: "http://127.0.0.1:11434/v1", model: "qwen2.5" },
    { label: "LM Studio", url: "http://127.0.0.1:1234/v1", model: "local-model" },
    { label: "llama.cpp", url: "http://127.0.0.1:8080/v1", model: "local-model" },
  ];

  const themes: { mode: ThemeMode; label: string }[] = [
    { mode: "system", label: "Use system theme" },
    { mode: "light", label: "Light" },
    { mode: "dark", label: "Dark" },
  ];

  function blank(): Settings {
    return {
      timezone: "",
      baseCurrency: "USD",
      llmBaseUrl: "",
      llmModel: "",
      llmTimeoutSeconds: 60,
      autoApply: true,
      autoApplyThreshold: 0.9,
      browserRuntime: "auto",
      browserSignIn: "default",
    };
  }

  $effect(() => {
    // Copy the loaded settings into the form once; never overwrite edits.
    const settings = store.settings;
    if (settings && !loaded) {
      form = { ...settings };
      loaded = true;
    }
  });

  onMount(() => {
    void loadLLMUsage();
    void loadSettings();
    void refreshBrowserRuntime();
  });

  async function refreshBrowserRuntime() {
    try {
      runtimeStatus = await SourcesService.BrowserRuntime();
    } catch {
      runtimeStatus = {
        ready: false,
        error: "",
        selected: "auto",
        resolved: "",
        resolvedName: "",
        signIn: "default",
        options: [
          { id: "auto", name: "Automatic (recommended)", installed: true },
          { id: "bundled", name: "Bundled Chromium", installed: true },
        ],
      };
    }
  }

  function applyPreset(preset: (typeof presets)[number]) {
    form.llmBaseUrl = preset.url;
    form.llmModel = preset.model;
    notice = "";
    testResult = null;
  }

  async function save(event: SubmitEvent) {
    event.preventDefault();
    formError = "";
    notice = "";
    saving = true;
    try {
      const threshold = Number(form.autoApplyThreshold);
      const saved = await SettingsService.Update({
        ...form,
        baseCurrency: form.baseCurrency.trim().toUpperCase(),
        autoApplyThreshold: Number.isFinite(threshold)
          ? Math.min(1, Math.max(0, threshold))
          : 0.9,
        llmTimeoutSeconds: Number(form.llmTimeoutSeconds) || 60,
      });
      store.settings = saved;
      form = { ...saved };
      notice = "Settings saved.";
      await refreshBrowserRuntime();
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
      testResult = await SettingsService.TestLLM();
    } catch (err) {
      testResult = { ok: false, models: [], error: errorMessage(err) };
    } finally {
      testing = false;
    }
  }

  async function reset() {
    if (!confirm("Restore default settings? Your subscriptions are not affected.")) return;
    formError = "";
    notice = "";
    try {
      const settings = await SettingsService.Reset();
      store.settings = settings;
      form = { ...settings };
      testResult = null;
      notice = "Defaults restored.";
      await loadLLMUsage();
      await refreshBrowserRuntime();
    } catch (err) {
      formError = errorMessage(err);
    }
  }

  function when(stamp: string): string {
    if (!stamp) return "—";
    const date = new Date(stamp);
    if (Number.isNaN(date.getTime())) return stamp;
    return date.toLocaleString(undefined, {
      month: "short",
      day: "numeric",
      hour: "2-digit",
      minute: "2-digit",
    });
  }

  const usage = $derived(store.llmUsage);
</script>

<div class="page">
  <header class="page-head">
    <div>
      <h1>Settings</h1>
      <p>Preferences, the local extraction model, and its recent activity.</p>
    </div>
    <button class="btn ghost" onclick={reset}>Restore defaults</button>
  </header>

  <form onsubmit={save} class="settings-form">
    <section class="card">
      <h3>General</h3>
      <div class="form-grid">
        <div class="field">
          <label for="settings-currency">Base currency</label>
          <input
            id="settings-currency"
            class="input"
            bind:value={form.baseCurrency}
            maxlength="10"
            placeholder="USD"
          />
          <span class="field-hint">Used when a receipt does not state a currency.</span>
        </div>

        <div class="field">
          <label for="settings-timezone">Timezone</label>
          <input
            id="settings-timezone"
            class="input"
            bind:value={form.timezone}
            placeholder="Europe/Berlin"
          />
          <span class="field-hint">Decides which calendar day counts as “today”. Empty uses the system zone.</span>
        </div>

        <label class="check-row span-2">
          <input type="checkbox" bind:checked={form.autoApply} />
          <span>
            <strong>Auto-apply high-confidence proposals</strong>
            <span class="field-hint">
              Only unambiguous new subscriptions above the threshold are created without review.
              Rules-only parses always wait for review.
            </span>
          </span>
        </label>

        <div class="field">
          <label for="settings-threshold">Auto-apply threshold</label>
          <input
            id="settings-threshold"
            class="input"
            type="number"
            min="0"
            max="1"
            step="0.01"
            bind:value={form.autoApplyThreshold}
          />
          <span class="field-hint">Confidence from 0 to 1. Default 0.9. Rules-only parses never auto-apply.</span>
        </div>
      </div>
    </section>

    <section class="card">
      <h3>Appearance</h3>
      <div class="row" style="justify-content: space-between; align-items: center;">
        <p class="field-hint" style="margin: 0;">
          Auto follows your operating system's light/dark setting and updates live.
        </p>
        <div class="chips" role="group" aria-label="Theme">
          {#each themes as option (option.mode)}
            <button
              type="button"
              class="chip"
              class:active={theme.mode === option.mode}
              onclick={() => setTheme(option.mode)}
              aria-pressed={theme.mode === option.mode}
            >
              {option.label}
            </button>
          {/each}
        </div>
      </div>
    </section>

    <section class="card">
      <h3>Browser sign-in</h3>
      <p class="callout">
        Browser sources sign in through your own browser so an existing Google or vendor session
        is reused. Only the vendor's session cookies are imported; they are stored as a secret.
      </p>

      <div class="form-grid" style="margin-top: 14px;">
        <div class="field span-2">
          <label for="settings-browser-signin">Sign-in method</label>
          <select
            id="settings-browser-signin"
            class="select"
            bind:value={form.browserSignIn}
          >
            <option value="default">Use my default browser (recommended)</option>
            <option value="isolated">Open an isolated browser</option>
          </select>
          <span class="field-hint">
            The default method opens a new tab in your current browser and imports the vendor's
            session after you sign in. The isolated method opens a separate Playwright window.
          </span>
        </div>

        <div class="field span-2">
          <label for="settings-browser-runtime">Browser for isolated sign-in and sync</label>
          <select
            id="settings-browser-runtime"
            class="select"
            bind:value={form.browserRuntime}
          >
            {#each runtimeStatus?.options ?? [] as option (option.id)}
              <option value={option.id} disabled={!option.installed}>
                {option.name}{option.installed ? "" : " (not installed)"}
              </option>
            {/each}
          </select>
          <span class="field-hint">
            Automatic prefers installed Chrome, then Brave or Chromium, and falls back to bundled
            Chromium. Headless sync uses this browser even when sign-in uses your own browser.
          </span>
          {#if runtimeStatus?.error}
            <span class="field-hint error-text">{runtimeStatus.error}</span>
          {:else if runtimeStatus?.resolvedName}
            <span class="field-hint">Currently resolving to {runtimeStatus.resolvedName}.</span>
          {/if}
        </div>
      </div>
    </section>

    <section class="card">
      <h3>Local extraction model</h3>
      <p class="callout">
        Substrackt only talks to an OpenAI-compatible endpoint on your machine or private
        network. Public hosts are rejected so subscription data never leaves your control.
      </p>

      <div class="row" style="margin: 14px 0;">
        {#each presets as preset (preset.label)}
          <button type="button" class="chip" onclick={() => applyPreset(preset)}>
            Use {preset.label}
          </button>
        {/each}
      </div>

      <div class="form-grid">
        <div class="field span-2">
          <label for="settings-llm-url">Endpoint URL</label>
          <input
            id="settings-llm-url"
            class="input mono"
            bind:value={form.llmBaseUrl}
            placeholder="http://127.0.0.1:11434/v1"
          />
          <span class="field-hint">Leave empty to use local rules only.</span>
        </div>

        <div class="field">
          <label for="settings-llm-model">Model</label>
          <input
            id="settings-llm-model"
            class="input"
            bind:value={form.llmModel}
            placeholder="qwen2.5"
          />
        </div>

        <div class="field">
          <label for="settings-llm-timeout">Timeout (seconds)</label>
          <input
            id="settings-llm-timeout"
            class="input"
            type="number"
            min="1"
            max="600"
            bind:value={form.llmTimeoutSeconds}
          />
        </div>
      </div>

      <div class="row" style="margin-top: 14px; align-items: center;">
        <button type="button" class="btn" onclick={testConnection} disabled={testing}>
          {testing ? "Testing…" : "Test connection"}
        </button>
        <span class="field-hint">Tests the saved settings. Save after editing, then test.</span>
      </div>

      {#if testResult}
        {#if testResult.ok}
          <div class="test-result ok">
            <strong>Connected</strong>
            <span>
              {(testResult.models ?? []).length > 0
                ? `Models: ${(testResult.models ?? []).join(", ")}`
                : "The endpoint is reachable but listed no models."}
            </span>
          </div>
        {:else}
          <div class="test-result bad">
            <strong>Not reachable</strong>
            <span>{testResult.error}</span>
          </div>
        {/if}
      {/if}
    </section>

    {#if formError}
      <p class="banner error" role="alert">{formError}</p>
    {/if}
    {#if notice}
      <p class="banner ok" role="status">{notice}</p>
    {/if}

    <div class="row" style="justify-content: flex-end;">
      <button type="submit" class="btn primary" disabled={saving}>
        {saving ? "Saving…" : "Save settings"}
      </button>
    </div>
  </form>

  <section class="card">
    <div class="row" style="justify-content: space-between; margin-bottom: 12px;">
      <h3 style="margin: 0;">Recent model calls</h3>
      <button class="btn ghost sm" onclick={() => void loadLLMUsage()}>Refresh</button>
    </div>

    {#if usage.length === 0}
      <div class="empty">
        <strong>No model calls yet</strong>
        <span>Calls are logged when an import uses the local model. Prompts and responses are never stored.</span>
      </div>
    {:else}
      <div class="table-wrap flat">
        <table>
          <thead>
            <tr>
              <th>When</th>
              <th>Model</th>
              <th class="num">Tokens</th>
              <th class="num">Latency</th>
              <th>Result</th>
            </tr>
          </thead>
          <tbody>
            {#each usage as row (row.id)}
              <tr>
                <td class="cell-sub">{when(row.createdAt)}</td>
                <td>
                  <div>{row.model || "—"}</div>
                  <div class="cell-sub mono">{row.baseUrl}</div>
                </td>
                <td class="num mono">{row.promptTokens + row.completionTokens}</td>
                <td class="num mono">{row.latencyMs} ms</td>
                <td>
                  {#if row.cached}
                    <span class="badge">Cache hit</span>
                  {:else if row.ok}
                    <span class="badge ok">OK</span>
                  {:else}
                    <span class="badge failed">Failed</span>
                    {#if row.error}<div class="cell-sub error-text">{row.error}</div>{/if}
                  {/if}
                </td>
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
    {/if}
  </section>
</div>
