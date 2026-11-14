# AGENTS.md

Guidance for AI agents and contributors working in this repository.

## Project

**Substrackt** is a fresh [Wails v3](https://v3.wails.io/) desktop application
(Go backend + TypeScript/Vite frontend). It is still an untouched scaffold: the
only backend logic is the demo `GreetService` and the periodic `time` event.

Keep changes idiomatic for Wails v3 — it differs substantially from v2.

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
wails3 dev                 # dev mode (hot reload, vite on WAILS_VITE_PORT)
wails3 build               # production build -> bin/substrackt
wails3 generate bindings   # regenerate TS bindings after changing Go services
wails3 task <task>         # run a Taskfile task directly
go build -tags gtk3 . ./internal/...
go vet -tags gtk3 . ./internal/...
npm --prefix frontend run build
```

Do **not** use `go build ./...`: `build/ios` and `build/android` contain stub
`package main` files that do not compile for the Linux host.

## Layout

```
main.go             app entry: application.New(), window, services, assets
greetservice.go     example bound service
internal/store/     bbolt-backed local database (accounts + settings buckets)
internal/mail/      go-imap/v2 client wrapper (dial, list mailboxes, select)
frontend/           Vite + TypeScript UI
frontend/bindings/  generated TS bindings (do not hand-edit)
frontend/dist/      vite output, embedded via //go:embed all:frontend/dist
build/              per-platform build assets, icons, Taskfiles, config.yml
Taskfile.yml        wails3 task definitions
```

## Conventions

- Backend code is `package main` at the repo root; services are plain structs
  registered in `application.Options{Services: ...}`.
- The Go module path is `substrackt`. Bindings are generated under
  `frontend/bindings/substrackt`; frontend imports must match that path. Renaming
  the module requires updating `frontend/src/*.ts` imports.
- Any Go change that adds/removes/renames a bound method requires regenerating
  bindings (`wails3 generate bindings`) or the frontend build fails.
- Product metadata lives in `build/config.yml`; after editing `info` or
  `fileAssociations`, run `wails3 task common:update:build-assets`.
- No `git` repo is initialised yet; `.gitignore` already covers `.task`,
  `.wails`, `bin`, `frontend/dist`, `frontend/node_modules`.

## Dependency notes

- `go.etcd.io/bbolt` v1.5.0 — used by `internal/store` (embedded DB at
  `<user config dir>/Substrackt/substrackt.db`, buckets `accounts` and `settings`).
- `github.com/emersion/go-imap/v2` v2.0.0-beta.9 — used by `internal/mail`
  (implicit TLS by default, `Config.Insecure` for local test servers). The v2
  API is pre-1.0: check signatures in the module before upgrading.
- `wails3 build` runs `go mod tidy` first (`common:go:mod:tidy`), so a module
  that nothing imports is silently removed from `go.mod` on the next build. If
  you swap these for other libraries, make sure real code imports them.

## Verification checklist

1. `go vet -tags gtk3 . ./internal/...` (with the shell environment described
   above; `./...` does not work — see Commands).
2. `wails3 build` — must produce `bin/substrackt` with no pkg-config errors.
3. `npm --prefix frontend run build` — must resolve every `frontend/bindings/*`
   import.
