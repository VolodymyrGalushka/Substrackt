# AGENTS.md

Guidance for AI agents and contributors working in this repository.

## Project

**Substrackt** is a [Wails v3](https://v3.wails.io/) desktop application (Go
backend + Svelte 5/TypeScript frontend) that tracks subscriptions and the payment
methods behind them. Milestones 1–5 (local CRUD, ingestion pipeline plus review
queue, email sources, browser sources, API sources) are implemented; renewals
and notifications remain. OAuth loopback support exists in `internal/api`, but
the built-in Stripe/Paddle connectors use API keys. The roadmap lives in
`docs/plan.md`.

Keep changes idiomatic for Wails v3 — it differs substantially from v2.

### Hard product constraints

- **No cloud LLM.** The LLM client must talk to a local / self-hosted
  OpenAI-compatible endpoint only. Never add vendor keys or send subscription
  data to a hosted API.
- **Never store secrets or PANs.** Payment methods hold metadata only (label,
  issuer, network, last4, expiry, wallet address + chain). Source passwords live
  only in the secret store (`internal/secrets`) and are never exposed through
  DTOs, events or logs. Never accept or persist full card numbers or CVVs.
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
wails3 generate bindings -ts -i   # regenerate TS bindings after changing Go services
go build -tags gtk3 . ./internal/...
go vet -tags gtk3 . ./internal/...
go test -tags gtk3 ./internal/...
go test -tags gtk3 -race ./internal/...   # before committing concurrency changes
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
internal/ingest/        normalization, dedupe, classification, heuristics, extraction pipeline
internal/jobs/          persisted job queue with a cancellable worker pool
internal/llm/           local OpenAI-compatible client (public hosts rejected)
internal/mail/          go-imap/v2 client and MIME parsing (text/plain preferred)
internal/browser/       Playwright engine and the vendor connector registry
internal/api/           declarative API connectors (Stripe, Paddle) and OAuth loopback
internal/secrets/       OS keyring with an AES-GCM encrypted 0600-file fallback
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
  `payment-method:changed`, `vendor:changed`, `source:changed`). Keep DTOs
  separate from domain types; the frontend only ever sees DTOs.
- Vendors are created on demand from the subscription editor (by name,
  case-insensitive), so a subscription may carry a vendor name without an ID.
- `Subscription.NextRenewal` is always recomputed from the original anchor date
  via `Interval.NextRenewal(anchor, today)` — never by repeatedly adding to the
  previous renewal, which drifts at month ends.
- Empty optional foreign keys (`vendor_id`, `payment_method_id`) must be written
  as SQL `NULL` (see `store.nullString`), otherwise the foreign keys fail.
- SQLite runs with `SetMaxOpenConns(1)` and WAL; new tables go in a **new**
  numbered migration, because `0001_init.sql` is already applied everywhere
  (`0002_ingestion.sql` adds jobs, artifacts, extractions, settings and LLM
  cache/usage; `0003_sources.sql` adds sources and mailbox cursors).
- The ingestion pipeline (`internal/ingest`) is channel-agnostic: every source
  produces a `RawItem` and shares normalize → dedupe (SHA-256) → classify →
  extract → match. Email is classified first and obvious non-billing mail
  (newsletters, shipping, security alerts) is dropped before any model call.
  Heuristic confidence is capped at 0.85, below the default 0.9 auto-apply
  threshold, so rules-only results always go to review.
- `internal/llm` only accepts loopback, private/link-local addresses, bare
  service names and local suffixes; never public hosts, credentials or
  query/fragment. Keep it that way.
- Background jobs are persisted and claimed atomically. Handlers must return
  promptly when their context is cancelled. The bound `IngestionService` only
  queues manual imports; `IngestionHandler` and `EmailHandler` (not Wails
  services) execute `ingest.manual` and `email.sync`, so function-typed methods
  never appear in the bindings.
- Email sync is incremental per mailbox: `mailbox_cursors` stores
  `(source_id, mailbox, uid_validity, last_uid)`. The cursor advances after each
  successfully pipelined message (including ignored mail) and stops before
  advancing on a fetch or pipeline error. A changed `UIDVALIDITY` triggers a
  bounded rescan with `last_uid = 0`. The IMAP client has no context support, so
  cancellation aborts the connection.
- `SourcesService` is the only bound service for channels. Email `Create` and
  enabled `Update` require a password; browser sources are created signed out
  and gain a session through `BeginSignIn`. `Test(id, in)` reuses the stored
  password or session when the input is empty. Email passwords are written under
  the secret ref `source:<id>`, browser sessions under `source:<id>:session`;
  DTOs expose only `HasSecret`. `RevealPassword(id)` exists solely for the
  explicit **Reveal saved** action, refuses browser sources, and must never be
  called implicitly or put in DTOs, events or logs.
- Browser sources use Playwright (`internal/browser`) with a connector registry.
  A connector declares its login/billing URL, authenticated JavaScript check,
  ready selector and extraction script; the first entry is GitHub billing.
  `BeginSignIn` opens a headed Chromium window, polls the authenticated check,
  and stores the Playwright storage state as a secret. `BrowserManager` serialises
  sign-ins per source, emits `browser:login` (`starting`, `installing`, `waiting`,
  `saved`, `failed`, `cancelled`) and clears the session when a sync reports
  `ErrSessionExpired`. Browser syncs are a `browser.sync` job that runs headless
  and feeds the shared pipeline; `SourceScheduler` skips sources without a
  session. The Playwright driver/Chromium are installed on demand into the local
  Playwright cache, never bundled. `Settings.BrowserRuntime` selects the binary:
  `auto` prefers installed Chrome, Brave, Chromium or Edge and falls back to
  bundled Chromium. System browsers still need the Playwright driver but not the
  Chromium download; `BrowserManager` resolves the preference per launch and
  passes it to both headed sign-in and headless sync. Prefer an installed Chrome
  for Google SSO, which rejects Playwright's Chrome-for-Testing build; never add
  stealth or anti-bot flags to work around a vendor block.
- `Settings.BrowserSignIn` selects the sign-in method. `default` opens a new tab
  in the user's own browser (`xdg-open`, `open` or `rundll32`) and polls the
  cookie store with `github.com/ChristopherDavenport/cookiecracker` until
  `Connector.SessionCookie` appears. Only cookies that apply to the vendor page
  are copied into the Playwright storage state; values are never logged. On
  Linux the source comes from `xdg-settings` (including Flatpak profile roots);
  other platforms try the supported browser profiles. Schema-24 cookies need the
  `SHA-256(host_key)` prefix stripped, which `stripHostHash` does.
  `isolated` keeps the Playwright window flow. If cookie import is unavailable
  (for example Windows App-Bound Encryption), report it and let the user choose
  `isolated` rather than silently opening an isolated browser.
- IMAP config has `Security` (`tls` implicit, `starttls`, `none` for
  loopback/private hosts only) and `Auth` (`auto`, `login`, `plain`, `xoauth2`).
  `auto` prefers LOGIN, honours `LOGINDISABLED` and only falls back to SASL when
  LOGIN is unavailable, so credential rejections are never retried. With
  `xoauth2` the credential is an OAuth2 access token. Certificate verification
  is never disabled; `TLSHost` only overrides the certificate hostname. Email
  sync calls `EmailSourceConfig.Validate()` after `Normalize()` before dialing.
  Provider presets in the UI only fill the address and encryption; they must not
  force an app password or XOAUTH2, which stay optional user choices.
- `SourceScheduler` polls enabled sources on their schedule with an in-memory
  cooldown, so an in-flight sync is not queued twice. It is best-effort; manual
  **Sync now** is always available. Syncs emit `source:status`, CRUD emits
  `source:changed`.
- `Source` stores three typed configs (`Email`, `Browser`, `API`) but only the one
  for its kind is marshalled. Email rows keep the historical flat JSON shape; do
  not add a shared union envelope without a migration.
- API sources use the declarative registry in `internal/api`. The built-in
  Stripe and Paddle connectors use bearer API keys, stored under `source:<id>`
  like email passwords; DTOs expose only `HasSecret`. API hosts must be https
  (loopback http only for tests); keys are never included in errors. `api.sync`
  fetches structured subscriptions and feeds the shared pipeline with
  `RawItem.Proposal`, which skips classification/extraction but still stores an
  artifact, matches and applies. `ConnectorFor` owns connector validation.
- `internal/api` also contains a loopback OAuth authorization-code helper (PKCE,
  state, 127.0.0.1 callback). No built-in connector uses OAuth yet because that
  needs a registered vendor client; wire it only through a connector's declared
  OAuth config.
- API source configs (`APISourceConfig`) are stored in the existing `sources`
  JSON column, separate from email and browser configs. Changing the API
  connector or auth mode clears the stored credential.
- Artifacts are redacted before storage; never persist PANs or CVVs.

## Conventions

- The Go module path is `substrackt`. Bindings are generated under
  `frontend/bindings/substrackt`; `frontend/src/lib/api.ts` is the only file that
  imports them, so the generated surface changes in one place.
- Any Go change that adds/removes/renames a bound method or DTO requires
  regenerating bindings (`wails3 generate bindings -ts -i`) or the frontend
  build breaks.
- Product metadata lives in `build/config.yml`; after editing `info` or
  `fileAssociations`, run `wails3 task common:update:build-assets`.
- Frontend entry points: `src/main.ts` mounts `App.svelte`; routing is hash-based
  (`lib/router.svelte.ts`); shared state lives in `lib/state.svelte.ts`. Theme is
  `system`/`light`/`dark` in `lib/theme.svelte.ts`: it writes
  `<html data-theme>` and persists to `localStorage` under `substrackt:theme`.
  `app.css` defines both palettes as CSS variables, so new UI must use the
  variables, not hard-coded colors. "System" calls
  `SettingsService.SystemDarkMode()` — on Linux that checks `GTK_THEME`, the
  GNOME/Budgie colour-scheme and GTK theme, then falls back to Wails'
  freedesktop-portal value — and polls while active. `main` calls
  `PrepareSystemTheme()` before `application.New` so a dark desktop sets
  `GTK_THEME` and WebKitGTK's `prefers-color-scheme` agrees with the app.
- The review queue supports bulk actions: `ReviewService.BulkAccept` and
  `BulkReject` apply each ID independently and report per-item failures in
  `ReviewBulkResult` (`accepted`/`rejected` plus `failed`).

## Dependency notes

- `modernc.org/sqlite` — pure-Go SQLite driver (no cgo). Used by `internal/store`.
- `github.com/emersion/go-imap/v2` v2.0.0-beta.9 — used by `internal/mail`
  (implicit TLS by default, `Config.Insecure` for local test servers). The v2
  API is pre-1.0: check signatures in the module before upgrading.
- `github.com/emersion/go-message` v0.18.2 — MIME parsing for email bodies.
  `mail.ParseMessage` prefers `text/plain`, strips HTML as a fallback, ignores
  attachments and returns the `Message-ID` without angle brackets.
- `github.com/emersion/go-sasl` — SASL PLAIN/LOGIN and the in-package XOAUTH2
  client used by `internal/mail`.
- `github.com/zalando/go-keyring` v0.2.8 — OS keyring lookup for source
  passwords, with the encrypted file backend in `internal/secrets` as fallback.
- `github.com/ChristopherDavenport/cookiecracker` v0.0.1 — reads and decrypts
  cookies from the user's default browser for default-mode sign-in. Linux v11
  cookies use the Secret Service; Flatpak profile roots are passed explicitly.
  It does not yet strip Chromium schema-24 host hashes, so `session.go` does.
  Windows App-Bound Encryption (`v20`) is unsupported; fall back to isolated.
- `github.com/mxschmitt/playwright-go` v0.6401.0 — Playwright 1.64.1 driver used
  by `internal/browser` for headed sign-in and headless billing-page fetches.
  The driver and Chromium live in the local Playwright cache and are installed
  on demand; they are not shipped in the binary. Chromium runs as a separate
  process, so browser syncs must close it on cancellation.
- `wails3 build` runs `go mod tidy` first (`common:go:mod:tidy`), so a module
  that nothing imports is silently removed from `go.mod` on the next build. If
  you add a library, make sure real code imports it.

## Verification checklist

1. `go vet -tags gtk3 . ./internal/...` (with the shell environment described
   above; `./...` does not work — see Commands).
2. `go test -tags gtk3 ./internal/...` — domain billing, store CRUD/migrations,
   secrets, email sync and service behaviour must stay green. Run it with
   `-race` before committing changes to the job manager, scheduler or secret
   store.
3. `npm --prefix frontend run check` — 0 errors.
4. `npm --prefix frontend run build` — vite production build succeeds.
5. `wails3 build` — must produce `bin/substrackt` with no pkg-config errors.
