# The transfer harness

`cmd/harness` runs warpseed's real queue, dispatcher and SFTP engine against a
real server, and says plainly what passed. It exists because the automated
tests prove the logic but not the platform: they run on Linux, and the app
ships to Windows, where NTFS preallocation, file locking and path handling are
the things most likely to go wrong.

Run it on Windows after any change to transfers, cancelling, pausing or
placeholder handling.

## The quickest way

`update.ps1` builds the harness beside `warpseed.exe` on every run, so it is
never a stale copy of an older commit:

```powershell
.\update.ps1 -Harness        # update, build both, run the self-test
.\update.ps1                 # update, build both, print how to run it
.\update.ps1 -NoHarness      # app only
```

Or build and run it by hand:

```powershell
go build -o harness.exe ./cmd/harness
.\harness.exe -self-test
```

`-self-test` starts a real SSH server with a real SFTP subsystem inside the
harness process, on loopback, with a throwaway host key and a single password.
Nothing to install, no credentials, no network. It still exercises NTFS, the
Windows file APIs, a real SSH handshake and real SFTP — everything except
distance.

A full run takes about half a minute at the default size and prints a line per
scenario. It exits non-zero if anything failed, and writes the same log it
prints to `warpseed-harness.log`, which can be sent on as-is.

## Against a real server

```powershell
.\harness.exe -host seedbox.example -user me -remote-dir /home/me/wstest -trust-new-host
```

The first run prints the host key fingerprint; pass it as `-hostkey SHA256:...`
on every run after that. There is deliberately no accept-anything mode — the
engine refuses to dial without a host key, and a tool that taught the habit of
skipping the check would be worse than no tool.

The password comes from `-pass` or, better, the `WARPSEED_HARNESS_PASSWORD`
environment variable.

`-remote-dir` must already exist and be writable. Everything the harness
creates goes inside it and inside `-local-dir`, and is removed afterwards
unless `-keep` is passed.

## What it checks

| Scenario | What would break if it failed |
|---|---|
| `connect` | credentials, host key, the working directory |
| `round-trip-verify` | a file survives upload and download byte for byte |
| `pause-resume-download` | pausing keeps the part file and spends no retry attempt, and resuming produces the original file |
| `cancel-download` | cancelling leaves nothing behind on this machine |
| `cancel-upload` | cancelling leaves nothing behind on the server |
| `bulk-cancel-uploads` | cancelling a folder reaches every row, and costs one connection rather than one per row |
| `hyperlane-download` | a multi-connection download assembles correctly |

Every check compares SHA-256 of content, never just size. Preallocation makes
size meaningless, which is the whole reason the resume guards exist.

Useful flags: `-size-mb` raises the test file size, which gives the timing
scenarios more room on a fast link; `-only` runs the scenarios whose name
contains a string.

## Also worth running on Windows

```powershell
go test ./...
```

The NTFS sparse-file tests in `internal/engine/sftpfast` are build-tagged for
Windows and never execute anywhere else. They cover the preallocation
behaviour behind the stall reported in 1.1.1.
