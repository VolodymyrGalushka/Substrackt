# Substrackt — architecture & plan

A desktop app (Wails v3 + Go + Svelte) that tracks subscriptions: what you pay,
how often, with which payment method, and where the information came from.

## Decisions

| Area | Decision |
| --- | --- |
| Storage | **SQLite** (`modernc.org/sqlite`, pure Go), WAL mode, embedded migrations |
| Frontend | **Svelte 5 + TypeScript + Vite** |
| LLM | **OpenAI-compatible API, local/self-hosted only** (Ollama, LM Studio, llama.cpp). No cloud calls; no vendor-hosted keys. |
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
│   ├── api/     declarative HTTP connectors (Stripe, Paddle, …)             │
│   ├── browser/ Playwright sessions + per-vendor scrapers                   │
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
- `artifacts` — raw evidence (email, HTML, JSON, screenshot) for audit/debug.
- `extractions` — review-queue proposals with confidence and model.
- `jobs`, `mailbox_cursors`, `settings`, `schema_migrations`.

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
| `Sources` (M3) | CRUD, test, sync now, interactive login |
| `Review` (M2) | pending list, accept/reject, bulk accept |
| `Jobs` (M2) | list, cancel, retry |
| `Settings` (M2) | LLM endpoint/model config, timezone, base currency |

Events: `subscription:changed`, `job:progress`, `job:finished`, `review:new`,
`source:status`.

## Ingestion pipeline

1. **Fetch** — per channel, incremental cursors (IMAP UID, API `since`, scraper
   last-seen id).
2. **Normalize** — MIME/HTML → canonical text, cap length, keep artifact.
3. **Dedupe** — content hash + external id.
4. **Classify** — cheap rules first (vendor domains, receipt/renewal subjects),
   LLM only for the ambiguous tail.
5. **Extract** — schema-validated JSON (vendor, plan, price, currency, interval,
   renewal, payment-method hint, confidence, quoted evidence), one repair retry,
   cached by content hash. Regex heuristics when no LLM is configured.
6. **Match** — vendor domain/alias → external id → amount ±5% / date ±3 days /
   name similarity.
7. **Apply** — auto-apply only when confidence and match are unambiguous;
   otherwise a review item. Price changes and cancellations always need review.

## Milestones

| Milestone | Status | Contents |
| --- | --- | --- |
| **M1 — core** | in progress | SQLite store + migrations, domain + billing tests, subscriptions / payment methods / vendors services, Svelte shell, dashboard, subscription editor |
| M2 | next | jobs + events, settings + local LLM client, review queue, manual ingestion through the pipeline |
| M3 | | email source (IMAP cursors, artifacts), classification/extraction, heuristics fallback |
| M4 | | Playwright framework, interactive login, first vendor connector |
| M5 | | declarative API connectors, OAuth loopback |
| M6 | | renewals/notifications, spend history from charges, export/import |

## Non-goals for M1

- No ingestion channels yet (email/API/browser) — the UI is read/write over
  manual data only.
- No charge history, no notifications, no multi-currency conversion (totals are
  reported per currency rather than silently summed).
- No cloud LLM support of any kind.
