# Substrackt

A desktop app for tracking subscriptions: what you pay, how often, with which
payment method, and when it renews next. Built with
[Wails v3](https://v3.wails.io/) (Go backend) and Svelte 5 + TypeScript.

Data lives in a local **SQLite** database — nothing leaves the machine. Milestones
3 and 4 add email and browser sources: connect an IMAP mailbox, or sign in to a
vendor billing page, and the evidence flows through the same review queue as
manual imports, optionally parsed by a **local** OpenAI-compatible model. The
longer-term plan (API connectors, renewals) is in [`docs/plan.md`](docs/plan.md).

The interface has light and dark palettes plus **Use system theme**, which
follows the OS setting live; the choice is remembered locally on the device.

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
Linux). Set `SUBSTRACKT_HOME` to use a different directory. Source passwords are
kept separately in the OS keyring, or in `<config dir>/Substrackt/secrets/` as an
encrypted 0600 file when no keyring is available.

## Layout

```
main.go                 app entry: DB path, services, window, embedded assets
internal/domain/        models, Money, calendar Date, billing math, ULID ids
internal/store/         SQLite (modernc.org/sqlite), WAL, embedded migrations
internal/app/           Wails-bound services, DTOs, mappers, tests
internal/ingest/        normalize, dedupe, classify, heuristics, pipeline
internal/jobs/          persisted worker pool with cooperative cancellation
internal/llm/           local OpenAI-compatible client (no cloud endpoints)
internal/mail/          go-imap v2 client and MIME parsing
internal/secrets/       OS keyring with an encrypted 0600-file fallback
frontend/src/           Svelte 5 UI: routes, components, lib helpers
frontend/bindings/      generated TS bindings (do not hand-edit)
build/                  platform build assets and Taskfiles
docs/plan.md            architecture, decisions and milestones
```

## Schema

Migration `internal/store/migrations/0001_init.sql` creates `vendors`,
`payment_methods` and `subscriptions`. Migration `0002_ingestion.sql` adds
`jobs`, `artifacts`, `extractions`, `settings`, `llm_cache` and `llm_usage`.
Migration `0003_sources.sql` adds `sources` and `mailbox_cursors`. Amounts are
stored as integer minor units with a currency and scale (2 for USD, 8 for BTC);
billing dates are calendar dates, so month-end clamping (Jan 31 → Feb 28) does
not drift. Payment methods keep only non-sensitive metadata — never a full card
number or CVV. Artifacts are redacted before they are stored, so PAN-like numbers
and CVVs never reach the database.

## Manual ingestion

**Review queue → Import text** (or **Activity → Import text**) queues a job for
pasted receipt or email text. The pipeline normalises the content, dedupes by
SHA-256, extracts fields with local rules, and — when a local model is configured
in **Settings** — validates a JSON extraction from it. Results land in **Review
queue**, except for unambiguous high-confidence proposals when auto-apply is on.
Price changes and cancellations always need a human decision. The LLM endpoint
must be loopback, private-network or a local service name; public hosts, queries
and credentials are rejected.

## Email sources

**Sources → Add source** configures an IMAP mailbox, starting from a provider
preset (Yahoo, Gmail, iCloud, Fastmail, Outlook/Microsoft 365, or custom).
Encryption is implicit TLS (normally 993), STARTTLS (normally 143), or plain TCP
for local/private test servers only. Authentication is `auto` (LOGIN, falling
back to SASL PLAIN/LOGIN when the server disables LOGIN), `login`, `plain`, or
`xoauth2`, where the credential field holds an OAuth2 access token. Presets fill
only the server address and encryption, so app passwords and API tokens are
optional: try your account password first, and switch to an app password or
XOAUTH2 token only if the provider rejects it. When a provider does reject an
account password (common with two-factor authentication), generate an app
password (Yahoo Account Security → Generate app password, Google Account → App
passwords, an app-specific password for iCloud), or use XOAUTH2 where basic IMAP
auth is disabled, such as Microsoft 365. The password field has
**Show/Hide**, and an existing source can load its saved credential into the
editor explicitly with **Reveal saved**.

Passwords are stored in
the OS keyring when it is available and otherwise in an encrypted 0600 file; they
are never returned to the frontend in DTOs, events or logs, and never written to
SQLite. `RevealPassword` is only called by that explicit user action.

**Test connection** checks the credentials without saving. **Sync now** queues an
email job; enabled sources are also polled on their schedule. Each sync reads a
per-mailbox UID cursor, fetches only newer messages (or a bounded recent window
on the first run), parses MIME text/HTML, and sends each message through the same
pipeline as manual imports. Obvious non-billing mail is skipped before the model;
everything else lands in **Review queue**. A changed `UIDVALIDITY` triggers a
rescan, and failures are recorded on the source without losing the last good
cursor.

## Browser sources

**Sources → Add source → Vendor website (browser)** signs in to a vendor's own
billing page. The first connector is **GitHub**; the page list is extensible in
`internal/browser`. Choose the connector and, if needed, override the billing URL
(it must stay on the vendor's host), then **Add source**. Saving opens a separate
Chromium window where you complete the normal password/2FA/SSO flow. Substrackt
polls until the vendor reports a signed-in page, closes the window and stores the
Playwright session as a secret under `source:<id>:session`. Closing the window or
using **Cancel sign-in** leaves the source signed out.

**Sync now** and the schedule run the page headlessly with that session. The
visible text is redacted and sent through the same pipeline as email and manual
text, so dedupe, extraction, matching and review are shared. If the session
expires, it is removed and the card asks you to sign in again. The Playwright
driver and Chromium are downloaded on first use through **Install browser
runtime** (or automatically on the first sign-in) into the local Playwright
cache; they are not bundled with the app.

## Dependencies

- `modernc.org/sqlite` — pure-Go SQLite driver (no cgo).
- `github.com/emersion/go-imap/v2` + `github.com/emersion/go-message` — IMAP and
  MIME parsing for email sources.
- `github.com/mxschmitt/playwright-go` — headed interactive sign-in and headless
  billing-page reads for browser sources.
- `github.com/zalando/go-keyring` — OS keyring access, with the encrypted file
  fallback in `internal/secrets`.
- `wails3 build` runs `go mod tidy`, so anything nothing imports gets pruned.
