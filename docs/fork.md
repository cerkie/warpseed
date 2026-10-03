# About this fork

`cerkie/warpseed` is a fork of [ZyraLabs/warpseed](https://github.com/ZyraLabs/warpseed)
(MIT). It is developed independently and does not send changes upstream.
Credit for the engine, the design and the UI goes to Zyra Labs; the LICENSE is unchanged.

Issues and pull requests for this fork are welcome here.

Releases are published at https://github.com/cerkie/warpseed/releases. The
in-app update check points at this repository by default (Settings, Data &
About), and can be switched to the original.

## What the fork adds (since 1.1.11)

User-facing notes are in `docs/release/release-notes-1.2.0.md`. This is where
each change lives in the code.

| Change | Where |
| --- | --- |
| FTPS (explicit and implicit TLS), TLS certificate pinning | `internal/engine/sftpfast/ftps.go`, `internal/hostkeys` (`Verify`, `CertFingerprint`), `app.go` (`dialSite`) |
| Hyperlane for FTPS downloads (REST-offset ranges); FTPS uploads stay single-connection | `ftps.go` (`ftpReader`), `chunked.go` (`openReaderAt`), `internal/dispatch` (`streamsFor`) |
| Parallel connection setup; quick SIZE/MDTM stat on FTPS | `app.go` (`dialAll`), `ftps.go` (`statLocked`, `sizeLocked`) |
| Resume verifies the last 64 KiB on SFTP and FTPS | `download.go` (`tailMatches`), `upload.go`, `ftps.go` |
| Moves: Shift-drag, F6; PC-to-server is copy-then-delete (server-to-PC is copy only), rename within one place | `FilePane.tsx`, `app.go` (`MoveRemote`, `moveRoot`), `internal/dispatch` (`removeMovedSource`), migration 014 (`move_root`) |
| SSH key file and SSH agent login | `client.go` (`authMethods`), `agent_windows.go`, `agent_other.go`, `ProtocolField.tsx` |
| Per-site options (`implicit`, `keyPath`, `useAgent`, `autoConnect`) in `options_json` | `app.go` (`siteOptions`), `frontend/src/lib/protocol.ts` |
| Connect on launch; remembered server pane; pane folders and divider restored | `App.tsx`, `lib/prefs.ts`, `ui.pane_state`, `ui.remote_side`, `ui.pane_split` |
| Site add/edit/delete in Settings and Connect; shared always-ask delete | `SettingsDialog.tsx`, `QuickConnect.tsx`, `lib/sites.ts` |
| Bandwidth schedule | `internal/dispatch` (`inScheduleWindow`), `bw.sched_*` settings |
| Desktop notification on queue finish | `app.go` (`Notify`), `App.tsx` |
| Transfer history | migration 015, `internal/queue/transfers.go` (`ClearCompleted`, `History`), `HistoryDialog.tsx` |
| Recycle Bin deletes | `internal/localfs/trash_windows.go` |
| Single running instance | `main.go` (`SingleInstanceLock`) |
| Graphite theme; themed dropdown lists | `tokens.css`, `lib/theme.ts` |
| Plain FTP | `ftps.go` (`FTPSConfig.Plain`), `app.go` (`dialSite`), `lib/protocol.ts` |
| Third pane, per-count pane widths, transfer target = last-used other pane | `store.ts` (`paneCount`, `otherSide`), `PaneArea.tsx` |
| Drag in from Explorer | `main.go` (`EnableFileDrop`), `lib/fileDrop.ts`, `app.go`/`dragout.go` (`EnqueueUploadsFromPaths`) |
| Drag files and folders out to Explorer and other file managers | `dragout_windows.go` (native OLE drag), `dragout_watch_windows.go` (finds the drop folder), `lib/dragOut.ts`, `FilePane.tsx` (`armDragOut`) |
| Import sites from FileZilla / WinSCP | `siteimport.go`, `lib/sites.ts`, `QuickConnect.tsx`, `SettingsDialog.tsx` |
| Update from inside the app (check, notes, install) | `update.go`, `update_install.go`, `update_helper_windows.go`, `UpdateBanner.tsx`, `UpdateDialog.tsx` |
| Installer build | `wails build -nsis` (needs NSIS) |

## Settings that need allow-listing

`SetSetting` rejects any key not in `settingValidators` in `app.go`. A new
persisted setting needs an entry there, and a `PrefKey` in `lib/prefs.ts` if it
is read synchronously at startup. (Forgetting this silently dropped the pane
divider and the Graphite theme once.)

## Building

```
export PATH="/c/Program Files/Go/bin:$HOME/go/bin:$PATH"
go vet ./... && go test ./...
cd frontend && npm ci && npm run build && cd ..
wails build -clean -trimpath -nsis        # warpseed.exe + installer in build/bin
```

`-trimpath` keeps the builder's local paths out of the binary. The installer
needs NSIS (`winget install NSIS.NSIS`).

## Not done / known limits

- FTPS is tested against an in-process server only.
- FTPS uploads are one connection per file.
- Moving directly between two servers is not supported.
- `release.ps1`, `push.sh` and the `release.yml` workflow are the original
  author's tooling for the ZyraLabs account and are not used by this fork.
