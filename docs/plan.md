# Substrackt — architecture & plan

A desktop app (Wails v3 + Go + Svelte) that tracks subscriptions: what you pay,
how often, with which payment method, and where the information came from.

## Decisions

| Area | Decision |
| --- | --- |
| Storage | **SQLite** (`modernc.org/sqlite`, pure Go), WAL mode, embedded migrations |
| Frontend | **Svelte 5 + TypeScript + Vite** |
| LLM | **OpenAI-compatible API, local/self-hosted by default** (Ollama, LM Studio, llama.cpp). No cloud calls; no vendor-hosted keys. A non-local endpoint works only after the user accepts an explicit data-exposure warning. |
| Browser automation | **Playwright-go**, browsers from the local Playwright cache |
| Secrets | OS keyring where available, 0600 encrypted-file fallback |
| Ingestion channels | email (IMAP + LLM parsing), vendor API, headless browser, manual |

## Shape of the system

```
┌──────────────────────── Wails window (Svelte 5) ──────────────────────────┐
│ Dashboard · Subscriptions · Review queue · Sources · Payment methods ·     │
│ Settings · Activity                                                        │
└───────────────▲───────────────────────────────┬────────────────────────────┘
        bindings│(typed RPC)             events │ job:progress / review:new /
                │                               │ subscription:changed / source:status
┌───────────────┴───────────────────────────────▼────────────────────────────┐
│ internal/app — Wails services (DTOs, validation, event emitters)           │
├────────────────────────────────────────────────────────────────────────────┤
│ domain (models, billing math)   jobs (worker pool, persisted, cancellable) │
│ store (SQLite repos, migrations)  secrets (OS keyring)                     │
├────────────────────────────────────────────────────────────────────────────┤
│ ingest orchestrator: Fetch → Normalize → Dedupe → Classify → Extract →     │
│                      Match → Propose/Auto-apply                            │
│   ├── email/   IMAP + MIME (go-imap v2, go-message)                        │
│   ├── browser/ Playwright sessions + per-vendor connectors                 │
│   ├── api/     declarative HTTP connectors (Stripe, Paddle) + OAuth loopback│
│   └── manual/  direct service calls, same pipeline downstream              │
│ llm/  OpenAI-compatible client only, schema-validated extraction,          │
│       repair retry, content-hash cache, usage log                          │
└────────────────────────────────────────────────────────────────────────────┘
```

Every channel produces the same `RawItem`, so classification, extraction,
matching, dedupe and the review queue are written once.

## Data model

- `vendors` — name, category, domains, aliases, logo.
- `payment_methods` — card / bank / paypal / wallet / crypto metadata only
  (label, issuer, network, last4, expiry, wallet address + chain). PAN and CVV
  are never accepted or stored.
- `subscriptions` — price (minor units + currency + scale), interval
  (count + unit), anchor date, next renewal, status, payment method, provenance
  (`source_id`, `external_id`), confidence, tags, notes.
- `charges` — payment history for analytics (milestone 6).
- `sources` — ingestion channel config (typed JSON), secret reference, schedule,
  health.
- `artifacts` — raw evidence (email, HTML, JSON, screenshot) for audit/debug,
  redacted of PANs/CVVs before storage.
- `extractions` — review-queue proposals with confidence and model.
- `jobs`, `mailbox_cursors`, `settings`, `schema_migrations`.
- `llm_cache` — extraction results keyed by model + prompt version + content hash.
- `llm_usage` — one audit row per model call (counters, latency, cached flag);
  prompts and responses are never stored.

Renewal dates are calendar dates, not instants, and are rendered in the user's
timezone. Billing math handles month-end clamping (Jan 31 → Feb 28) and
monthly-equivalent normalisation per currency.

## Services (Wails bindings)

| Service | Methods |
| --- | --- |
| `Subscriptions` | list/get/create/update/archive/pause/resume/cancel/upcoming |
| `PaymentMethods` | list/create/update/archive/delete |
| `Vendors` | list/create/update/delete |
| `Dashboard` | summary (spend, upcoming, by payment method) |
| `Sources` | email/browser/API source CRUD, connection test, browser sign-in, sync now, automatic schedule |
| `Review` | pending list, get, count, accept/reject, bulk accept |
| `Jobs` | list, get, cancel, retry, counts |
| `Settings` | get/update/reset, LLM endpoint test, usage log |
| `Ingestion` (M2) | manual text import (queues a job) |

Events: `subscription:changed`, `payment-method:changed`, `vendor:changed`,
`job:progress`, `job:finished`, `review:new`, `review:changed`,
`settings:changed`, `source:changed`, `source:status`, `browser:login`.

## Ingestion pipeline

1. **Fetch** — per channel, incremental cursors (IMAP UID + UIDVALIDITY, API
   `since`, scraper last-seen id).
2. **Normalize** — MIME/HTML → canonical text, cap length, keep a redacted
   artifact.
3. **Dedupe** — content hash + external id.
4. **Classify** — cheap rules first. Obvious non-billing mail (newsletters,
   shipping notices, security alerts) is dropped for the email channel before
   any model call; billing and unknown mail continue.
5. **Extract** — schema-validated JSON (vendor, plan, price, currency, interval,
   renewal, payment-method hint, confidence, quoted evidence), one repair retry,
   cached by content hash. Regex heuristics when no LLM is configured or it
   fails.
6. **Match** — vendor domain/alias → external id → amount ±5% / date ±3 days /
   name similarity.
7. **Apply** — auto-apply only when confidence and match are unambiguous;
   otherwise a review item. Price changes and cancellations always need review.

## Email sources

The **Sources** screen connects an IMAP mailbox through a provider preset (Yahoo,
Gmail, iCloud, Fastmail, Outlook/Microsoft 365, custom). Encryption modes are
implicit TLS (normally 993), STARTTLS (normally 143) and plain TCP, the last
restricted to loopback, private or local hostnames. Authentication modes are
`auto` (prefer IMAP LOGIN, honour `LOGINDISABLED`, fall back to SASL PLAIN/LOGIN
only when LOGIN is unavailable), `login`, `plain` and `xoauth2`. Presets fill
only the server address and encryption, so app passwords and API tokens are
optional: try the account password first and switch only if the provider rejects
it. With `xoauth2` the credential field holds a short-lived access token;
Microsoft 365 may require this because basic IMAP password auth is often
disabled. Yahoo, Gmail and iCloud may require an app password when two-factor
authentication is on. Certificate verification is never disabled; an optional TLS hostname override
covers IP addresses and LAN aliases. An `ID` command is sent when the server
advertises it, which some providers expect before `SELECT`.

Passwords are written to the OS keyring when it is reachable and to an encrypted
0600 file under `<config dir>/Substrackt/secrets/` otherwise; they never enter
SQLite, DTOs, logs or events. The editor can reveal the typed value locally, and
**Reveal saved** calls `SourcesService.RevealPassword` explicitly for an existing
source. Each sync:

1. reads the per-mailbox cursor (`uid_validity`, `last_uid`),
2. searches only newer UIDs, or a bounded recent window on the first run,
3. runs each message through the shared pipeline,
4. advances the cursor after every successful message,
5. stores `last_sync_at` / `last_error` and emits `source:status`.

A changed `UIDVALIDITY` triggers a rescan of the recent window. A failed job
(mailbox unreachable, bad password, pipeline error) stops before advancing the
cursor for the failing message and records the error on the source. Enabled
sources are polled on their schedule by a background scheduler; **Sync now** is
always available.

## Browser sources

A browser source signs in to a vendor's own site and reads its billing page.
The connector registry lives in `internal/browser` and currently has one entry:
**GitHub** (`/settings/billing`). Each connector declares its login URL, billing
URL, a JavaScript authenticated check, a ready selector and the extraction
script.

Sign-in is interactive. The default method opens a new tab in the user's own
browser, waits for the connector's session cookie, and imports only the cookies
that apply to the vendor page. The isolated method opens a headed Playwright
window (installed Chrome/Brave when available), lets the user complete
password/2FA/SSO, and waits for the connector's authenticated check. Either way
the resulting storage state is written to the secret store under
`source:<id>:session`; it is never written to SQLite or returned by a bound
method. Cancelling or timing out leaves the source signed out. The Playwright
driver and Chromium are downloaded on first use (or with **Install browser
runtime**) and are reused from the local Playwright cache.

Settings → **Browser sign-in** selects the method and the sync binary. The
default method opens a new tab in the user's own browser and imports only the
vendor's session cookies, so an existing Google session is reused; the isolated
method opens a Playwright window. `auto` prefers an installed Chrome, Brave,
Chromium or Edge and falls back to bundled Chromium; an explicit system browser
still needs the Playwright driver but skips the Chromium download. No stealth or
anti-bot flags are added; a vendor that still blocks automation is handled
through email, API or manual input instead.

**Sync now** and the scheduler run a headless fetch with that session. The page
text is redacted and ingested through the same pipeline as email and manual
text, so extraction, dedupe, matching and review are shared. An expired session
clears the stored state and marks the source so the UI asks for a new sign-in.
Only one browser window is opened per source at a time.

## API sources

**Sources → Add source → Vendor API** selects a connector from `internal/api`.
The built-in connectors are **Stripe** and **Paddle**, both authenticated with a
read-only bearer API key stored under `source:<id>` in the secret store. The
connector registry declares each API's base URL, pagination style, JSON field
mapping, status map and interval map; the client only permits https hosts
(loopback http is reserved for tests) and never logs credentials.

An `api.sync` job fetches up to the configured item limit, maps each subscription
to `ingest.Proposal`, and sends a `RawItem` with structured data through the
shared pipeline. The pipeline still stores a redacted artifact and dedupes it,
but skips classification and extraction; matching and review are unchanged.
Changing the connector or auth mode clears the stored credential. The OAuth
loopback helper in `internal/api` (PKCE, state, 127.0.0.1 callback) is ready for
connectors that have a registered vendor OAuth client; no built-in connector
uses it yet.

## Milestones

| Milestone | Status | Contents |
| --- | --- | --- |
| **M1 — core** | done | SQLite store + migrations, domain + billing tests, subscriptions / payment methods / vendors services, Svelte shell, dashboard, subscription editor |
| **M2 — ingestion core** | done | jobs + events, settings + local LLM client, review queue, manual ingestion through the pipeline, review/activity/settings UI |
| **M3 — email source** | done | sources + secret store, IMAP cursors/artifacts, MIME parsing, email classification, scheduled and manual sync, Sources UI |
| **M4 — browser source** | done | Playwright framework, default-browser cookie import plus isolated sign-in, saved sessions, scheduled browser sync, GitHub billing connector |
| **M5 — API source** | done | declarative Stripe/Paddle connectors, API-key secret storage, structured ingestion, OAuth loopback helper |
| **M6** | next | renewals/notifications, spend history from charges, export/import |

## Non-goals for M4/M5

- No built-in OAuth-backed vendor connector yet; `internal/api` ships the
  loopback helper, but a real connector needs a registered vendor client.
- No headless-browser CAPTCHA solving or anti-bot evasion; if a vendor blocks
  automated sign-in, the user sees the vendor page and can cancel.
- No screenshots in artifacts yet; browser evidence is the redacted page text.
- No charge history, notifications or multi-currency conversion.
- No cloud LLM support of any kind without explicit user consent; a non-local
  endpoint requires accepting the data-exposure warning first.
