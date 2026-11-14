# AGENTS.md

Guidance for AI agents and contributors working in this repository.

## Project

**Substrackt** is a [Wails v3](https://v3.wails.io/) desktop application (Go
backend + Svelte 5/TypeScript frontend) that tracks subscriptions and the payment
methods behind them. Milestone 1 (local CRUD over SQLite) is implemented; the
roadmap (email ingestion with a local LLM, API connectors, browser scraping,
review queue) lives in `docs/plan.md`.

Keep changes idiomatic for Wails v3 — it differs substantially from v2.

### Hard product constraints

- **No cloud LLM.** The LLM client (once built) must talk to a local /
  self-hosted OpenAI-compatible endpoint only. Never add vendor keys or send
  subscription data to a hosted API.
- **Never store secrets or PANs.** Payment methods hold metadata only (label,
  issuer, network, last4, expiry, wallet address + chain). Never accept or
  persist full card numbers or CVVs.
- Amounts are integer minor units + currency + scale. Billing dates are calendar
  dates (`domain.Date`), never timestamps.

## Toolchain

| Tool | Version | Source |
| --- | --- | --- |
| Go | 1.27.4 | asdf (`.tool-versions`) |
| Wails CLI | v3.1.0 (`wails3`) | `go install -tags gtk3 github.com/wailsapp/wails/v3/cmd/wails3@v3.1.0` |
| Node / npm | 24.x / 11.x | system + npm |

`asdf install` in this directory installs the pinned Go version. The Wails CLI
lives in `~/.local/bin`.

### Shell environment gotcha (important)

The asdf golang plugin is wired into `~/.bashrc` via `set-env.bash`, which exports
`GOROOT`, `GOPATH` and `GOBIN` **at shell startup**. If a terminal was opened
outside this directory, those variables still point at the older Go install and
builds fail with:

```
compile: version "go1.23.6" does not match go tool version "go1.27.4"
```

Fix: open a fresh shell inside the project, `asdf reshim`, or run builds with
`env -u GOROOT -u GOPATH -u GOBIN`. Do not "fix" this by editing `go.mod`.

## Linux backend: GTK3, not GTK4

`build/linux/Taskfile.yml` sets `EXTRA_TAGS: '{{.EXTRA_TAGS | default "gtk3"}}'`
because this host has `gtk+-3.0` + `webkit2gtk-4.1` dev packages but **not**
`gtk4-devel` / `webkitgtk6.0-devel`. Without that default, compilation fails on
`pkg-config: gtk4 webkitgtk-6.0`.

- Do not remove the `gtk3` default unless those GTK4 packages are installed.
- Removing it also requires switching from `webkit2gtk-4.1` runtime, which is what
  is currently installed.

## Commands

```sh
wails3 dev                        # dev mode (hot reload, vite on WAILS_VITE_PORT)
wails3 build                      # production build -> bin/substrackt
wails3 generate bindings          # regenerate TS bindings after changing Go services
go build -tags gtk3 . ./internal/...
go vet -tags gtk3 . ./internal/...
go test -tags gtk3 ./internal/...
npm --prefix frontend run check   # svelte-check
npm --prefix frontend run build   # vite production build
```

Do **not** use `go build ./...`: `build/ios` and `build/android` contain stub
`package main` files that do not compile for the Linux host.

## Layout

```
main.go                 app entry: DB path, services, window, embedded assets
internal/domain/        models, Money, calendar Date, billing math, ULID ids
internal/store/         SQLite store: open/WAL, migrations, repositories
internal/store/migrations/*.sql   embedded schema migrations (add new files, never edit applied ones)
internal/app/           Wails-bound services, DTOs, mappers, tests
internal/mail/          go-imap/v2 client wrapper (for the future email source)
frontend/src/           Svelte 5 UI: routes/, components/, lib/
frontend/bindings/      generated TS bindings (do not hand-edit)
frontend/dist/          vite output, embedded via //go:embed all:frontend/dist
build/                  per-platform build assets, icons, Taskfiles, config.yml
docs/plan.md            architecture and milestones
```

## Architecture notes

- `main.go` opens the store at `<user config dir>/Substrackt/substrackt.db`
  (`SUBSTRACKT_HOME` overrides), registers the services and creates the window.
- Services in `internal/app` are plain structs. They validate input, translate
  between domain types and DTOs, and emit events (`subscription:changed`,
  `payment-method:changed`, `vendor:changed`). Keep DTOs separate from domain
  types; the frontend only ever sees DTOs.
- Vendors are created on demand from the subscription editor (by name,
  case-insensitive), so a subscription may carry a vendor name without an ID.
- `Subscription.NextRenewal` is always recomputed from the original anchor date
  via `Interval.NextRenewal(anchor, today)` — never by repeatedly adding to the
  previous renewal, which drifts at month ends.
- Empty optional foreign keys (`vendor_id`, `payment_method_id`) must be written
  as SQL `NULL` (see `store.nullString`), otherwise the foreign keys fail.
- SQLite runs with `SetMaxOpenConns(1)` and WAL; new tables go in a **new**
  numbered migration, because `0001_init.sql` is already applied everywhere.

## Conventions

- The Go module path is `substrackt`. Bindings are generated under
  `frontend/bindings/substrackt`; `frontend/src/lib/api.ts` is the only file that
  imports them, so the generated surface changes in one place.
- Any Go change that adds/removes/renames a bound method or DTO requires
  regenerating bindings (`wails3 generate bindings`) or the frontend build breaks.
- Product metadata lives in `build/config.yml`; after editing `info` or
  `fileAssociations`, run `wails3 task common:update:build-assets`.
- Frontend entry points: `src/main.ts` mounts `App.svelte`; routing is hash-based
  (`lib/router.svelte.ts`); shared state lives in `lib/state.svelte.ts`.

## Dependency notes

- `modernc.org/sqlite` — pure-Go SQLite driver (no cgo). Used by `internal/store`.
- `github.com/emersion/go-imap/v2` v2.0.0-beta.9 — used by `internal/mail`
  (implicit TLS by default, `Config.Insecure` for local test servers). The v2
  API is pre-1.0: check signatures in the module before upgrading. It is kept
  for the email milestone; make sure the package still compiles if you touch it.
- `wails3 build` runs `go mod tidy` first (`common:go:mod:tidy`), so a module
  that nothing imports is silently removed from `go.mod` on the next build. If
  you add a library, make sure real code imports it.

## Verification checklist

1. `go vet -tags gtk3 . ./internal/...` (with the shell environment described
   above; `./...` does not work — see Commands).
2. `go test -tags gtk3 ./internal/...` — domain billing, store CRUD/migrations
   and service behaviour must stay green.
3. `npm --prefix frontend run check` — 0 errors.
4. `wails3 build` — must produce `bin/substrackt` with no pkg-config errors.
