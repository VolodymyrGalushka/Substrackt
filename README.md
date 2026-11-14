# Substrackt

An empty [Wails v3](https://v3.wails.io/) desktop application scaffold (vanilla
TypeScript frontend), ready for feature work.

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
wails3 dev              # run with hot reload
wails3 build            # production build -> bin/substrackt
wails3 build -tags gtk3 # explicit tags (already the default here)
wails3 generate bindings # regenerate TS bindings after changing Go services
```

## Layout

- `main.go` — application entry point (window, services, assets)
- `greetservice.go` — example service bound to the frontend
- `internal/store/` — bbolt-backed local database (`accounts`, `settings` buckets)
- `internal/mail/` — go-imap v2 client wrapper (dial, list mailboxes, select)
- `frontend/` — Vite + TypeScript frontend (`src/`, `bindings/`)
- `build/` — platform build assets and Taskfiles
- `Taskfile.yml` — build/dev tasks used by `wails3`

## Dependencies

- `go.etcd.io/bbolt` v1.5.0 — embedded key/value store
- `github.com/emersion/go-imap/v2` v2.0.0-beta.9 — IMAP client

Both are used by the `internal/` packages above; `wails3 build` runs `go mod tidy`,
so anything nothing imports gets pruned.
