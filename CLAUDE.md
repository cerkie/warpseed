# warpseed — fast SFTP and FTPS transfer client for Windows

A free, portable Windows x64 client for seedbox workloads: multi-connection
("Hyperlane") transfers, byte-level resume that survives errors and restarts,
and a queue you can trust with an overnight 50 GB run. Created by Zyra Labs
(`github.com/ZyraLabs/warpseed`, MIT); this checkout is the `cerkie/warpseed`
fork, where bugs are reported as GitHub issues.

## This fork

This checkout is the `cerkie/warpseed` fork. Its issues and PRs live in that repo.
`docs/fork.md` lists what the fork changed
and where. `push.sh`, `release.ps1` and the `release.yml` workflow are the
upstream author's tooling; the fork releases with `gh release create`.
A new persisted setting needs an entry in `settingValidators` (app.go), or
`SetSetting` rejects it silently.

## Read first

- `docs/design/ux-spec.md` — the design bible: keyboard map (WinSCP muscle
  memory), queue dock, signature moments. Consult before any UI work.
- `docs/planning/roadmap-2026-09.md` — what is next, ordered by what it costs
  the user. Phase 1 (data safety) ships before anything else.
- `docs/user-guide.md` — what the app promises users; keep it true.
- `docs/test-harness.md` — how to exercise real transfers on Windows, which is
  the only place NTFS behaviour and the Windows file APIs are reachable.
- `docs/release/release-notes-*.md` — one file per release, user-facing.

## Stack

Go 1.25 (module `warpseed`), Wails v2.13, React 19 + Zustand + Vite frontend,
SQLite via `modernc.org/sqlite` (pure Go). **CGO is off everywhere** so the app
cross-compiles from Linux. Wails v3 was alpha at the start; every Wails API call
is confined to `app.go` (the Bindings facade), `internal/events`, and
`frontend/src/ipc.ts` so a later migration stays contained.

## Layout

```
app.go                    Wails bindings facade — the only Go file that imports Wails
internal/engine/sftpfast  custom SFTP engine (pkg/sftp + x/crypto/ssh): byte-resume, chunked lanes
internal/dispatch         the transfer dispatcher: lanes, throttle, retries, chunk plans
internal/queue            SQLite-WAL queue of record (.wspart / .wschunk placeholders)
internal/localfs          local filesystem browsing and ops
internal/hostkeys         TOFU host-key pinning
internal/creds            Windows Credential Manager (build-tagged; fake on Linux)
internal/applog           log file (never credentials)
internal/events           Go -> frontend event names, one place
frontend/src/tokens.css   every colour, font and density value; themes are token blocks
frontend/src/mock         browser mock backend: `npm run dev` then `?mock=1&theme=cobalt|iris`
docs/                     design, planning, release notes, user guide, screenshots
cmd/harness             real transfers against a real server; see docs/test-harness.md
update.ps1 / release.ps1  the user's Windows build loop and manual release
push.sh                   push + the gh account switch either side of it
```

## Commands

```
export PATH="$HOME/.local/share/mise/shims:$PATH"   # go/node are mise shims
go vet ./... && go test ./...                        # Go side
cd frontend && npm run build                         # tsc + vite; the type-check
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -o /dev/null .   # per-commit sanity
```

```
go run ./cmd/harness -self-test        # real SSH+SFTP, real transfers, on THIS machine
```

CI (`.github/workflows/ci.yml`) runs exactly those three on every push.
`wails build` only runs on Windows; the user builds via `update.ps1` after each
push and reads the printed commit. A `v*` tag triggers `release.yml`.

## Rules

1. **Never lose or fake a user's bytes.** Resume guards compare real content
   (head/tail bytes), never size alone — preallocation makes size meaningless.
   A placeholder that is not completed is cleaned up, never published.
2. **Emit-and-return on the UI thread.** `OnBeforeClose` and any Wails callback
   must never block; blocking deadlocks the dialog it awaits.
3. **Wails fires listeners in registration order.** Never detect change by
   comparing the store to itself across listeners; snapshot locally.
4. **Components read tokens, never colours.** A theme is a block in
   `tokens.css`. Fonts are bundled via `@fontsource`; no webfont CDNs.
5. **"Hyperlane" is product language** for multi-connection transfers — in
   settings, tooltips and release notes. Keep using it.
6. **No glyph characters in the UI**; icons come from `Icon.tsx`.
7. **Measure layout claims in a real browser** (mock backend + headless
   Chromium, `document.elementFromPoint`); reasoning from CSS source has been
   wrong before.
8. **Log to the log file, never stderr** — a Windows GUI build discards stderr.
   Never log credentials.
9. Bump `wails.json` `productVersion` and write `docs/release/release-notes-<v>.md`
   in the same commit as any change that ships. Stage explicit paths, never `git add -A`.
