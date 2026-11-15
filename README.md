# Substrackt

A desktop app for tracking subscriptions: what you pay, how often, with which
payment method, and when it renews next. Built with
[Wails v3](https://v3.wails.io/) (Go backend) and Svelte 5 + TypeScript.

Data lives in a local **SQLite** database — nothing leaves the machine. Milestone 2
adds a background job queue, a review queue and manual ingestion (paste a receipt
or email), optionally parsed by a **local** OpenAI-compatible model. The longer-term
plan (email ingestion, API connectors, browser scraping) is in
[`docs/plan.md`](docs/plan.md).

## Requirements

- **Go 1.27.4** — pinned in `.tool-versions` for asdf. Run `asdf install` in this
  directory to install it.
- **Node.js + npm** — used by Vite to build the frontend.
- **Wails 3 CLI** (`wails3`, v3.1.0):

  ```sh
  go install -tags gtk3 github.com/wailsapp/wails/v3/cmd/wails3@v3.1.0
  ```

### Linux / GTK note

This project defaults to the **GTK3 + WebKit2GTK 4.1** backend because
`gtk4-devel` and `webkitgtk6.0-devel` are not installed on this machine.
`build/linux/Taskfile.yml` sets `EXTRA_TAGS: gtk3` so plain `wails3 build` and
`wails3 dev` work as-is.

If you install the GTK4 packages (`sudo dnf install gtk4-devel webkitgtk6.0-devel`),
delete that `EXTRA_TAGS` default to use Wails' upstream GTK4 / WebKitGTK 6.0 backend.

## Commands

```sh
wails3 dev               # run with hot reload
wails3 build             # production build -> bin/substrackt
wails3 generate bindings -ts -i  # regenerate TS bindings after Go service changes
npm --prefix frontend run check   # svelte-check (types + a11y)
go test -tags gtk3 ./internal/... # domain, store and service tests
```

## Where data is stored

`<user config dir>/Substrackt/substrackt.db` (e.g. `~/.config/Substrackt/` on
Linux). Set `SUBSTRACKT_HOME` to use a different directory.

## Layout

```
main.go                 app entry: DB path, services, window, embedded assets
internal/domain/        models, Money, calendar Date, billing math, ULID ids
internal/store/         SQLite (modernc.org/sqlite), WAL, embedded migrations
internal/app/           Wails-bound services, DTOs, mappers, tests
internal/ingest/        normalize, dedupe, heuristics, pipeline, apply decisions
internal/jobs/          persisted worker pool with cooperative cancellation
internal/llm/           local OpenAI-compatible client (no cloud endpoints)
internal/mail/          go-imap v2 client wrapper (kept for email ingestion)
frontend/src/           Svelte 5 UI: routes, components, lib helpers
frontend/bindings/      generated TS bindings (do not hand-edit)
build/                  platform build assets and Taskfiles
docs/plan.md            architecture, decisions and milestones
```

## Schema

Migration `internal/store/migrations/0001_init.sql` creates `vendors`,
`payment_methods` and `subscriptions`. Migration `0002_ingestion.sql` adds
`jobs`, `artifacts`, `extractions`, `settings`, `llm_cache` and `llm_usage`.
Amounts are stored as integer minor units with a currency and scale (2 for USD,
8 for BTC); billing dates are calendar dates, so month-end clamping
(Jan 31 → Feb 28) does not drift. Payment methods keep only non-sensitive
metadata — never a full card number or CVV. Artifacts are redacted before they
are stored, so PAN-like numbers and CVVs never reach the database.

## Manual ingestion

**Review queue → Import text** (or **Activity → Import text**) queues a job for
pasted receipt or email text. The pipeline normalises the content, dedupes by
SHA-256, extracts fields with local rules, and — when a local model is configured
in **Settings** — validates a JSON extraction from it. Results land in **Review
queue**, except for unambiguous high-confidence proposals when auto-apply is on.
Price changes and cancellations always need a human decision. The LLM endpoint
must be loopback, private-network or a local service name; public hosts, queries
and credentials are rejected.

## Dependencies

- `modernc.org/sqlite` — pure-Go SQLite driver (no cgo).
- `github.com/emersion/go-imap/v2` — IMAP client for the future email source.
- `wails3 build` runs `go mod tidy`, so anything nothing imports gets pruned.
