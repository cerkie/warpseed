warpseed 1.2.0 — FTPS, moving files, key login, and a lot of polish

This is the first release of the **cerkie/warpseed** fork of
[ZyraLabs/warpseed](https://github.com/ZyraLabs/warpseed). It keeps everything
in 1.1.11 and adds the changes below. See `docs/fork.md` for the technical
side.

## New

- **FTPS.** Pick SFTP, FTPS (explicit TLS) or FTPS (implicit TLS) when adding a
  connection; the port follows the protocol until you type your own. Browse,
  download, upload, rename, create folders and delete all work. The server's
  TLS certificate is pinned on first use, exactly like an SSH host key, so the
  self-signed certificates most seedboxes use work and a changed one is refused.
- **Hyperlane for FTPS downloads.** A large file is split across several
  connections that each resume at their own offset, using the same settings as
  SFTP. FTPS uploads stay on one connection per file, because FTP cannot write
  to arbitrary positions in a file.
- **Move files.** Hold **Shift** while dragging, press **F6**, or use right-click
  > Move to other pane. Between This PC and a server the original is deleted
  only after the copy has arrived complete, and folders it empties are removed.
  Within one place (This PC, or one server) a move is a rename and transfers
  nothing. Dragging without Shift where a move is the only option shows a note
  beside the cursor explaining how.
- **SSH key and agent login** for SFTP: a private key file (the password field
  becomes its passphrase) or the Windows SSH agent.
- **Connect on launch** for any saved site, and the app remembers which pane you
  keep servers in, so new connections always open there.
- **Reopens where you left off.** Each pane returns to its last folder (a pane
  that was on a server reconnects in the background), and the divider position
  is remembered.
- **Resizable panes.** Drag the divider between the two browse panes; double-click
  it to reset.
- **Edit and manage sites anywhere.** The Connect tab can edit saved sites
  (including the protocol), and Settings can add, edit and delete them. Deleting
  always asks first, with no "don't ask again".
- **Update checks can use this fork or the original** (Settings, Data & About),
  defaulting to this fork. warpseed still never replaces itself; it only opens
  a release page.
- **Bandwidth schedule:** slow transfers down between two hours of the day. It
  can only tighten a limit you already set.
- **Desktop notification** when the queue finishes while warpseed is in the
  background.
- **Transfer history:** finished transfers you have cleared from the queue
  (Settings, Data & About, or Ctrl+K).
- **Hideable columns:** right-click the column headings.
- **Graphite theme, now the default** for new installs (existing choices are kept): a dark theme with a teal accent. Dropdown lists now follow
  the active theme.
- **Installer.** A normal Windows installer is published alongside the portable exe.

## Faster to start

- A transfer's connections now open at the same time instead of one after
  another (SFTP still spaces the starts slightly so servers do not mistake it
  for a scan). On FTPS, file lookups use quick SIZE/MDTM requests instead of
  reading a whole folder listing.

## Safer

- **Resume checks verify content for SFTP and FTPS.** The last 64 KiB of a
  partial file is compared with the other side before continuing, so a part that
  merely has the right length is restarted instead of completed.
- **Deleting on This PC sends files to the Recycle Bin.**
- A second launch brings the running window forward instead of opening a second
  one on the same database.

## Interface fixes

- Long file names end in an ellipsis; long errors in Flight view keep the file
  name; Settings is split into tabs; dropdown arrows are inset and each pane's
  source button shows an arrow; the pane filter (Ctrl+F) closes when you click
  away while empty and has a close button; toasts can be clicked away and errors
  stay longer; the site list is alphabetical.
- A "connecting" bar and status-bar note show while a server connection is made.
- Plainer messages for common connection failures, with the original text kept.
- Bug reports go to this repository's issues.

## Notes

- The exe and installer are unsigned, so SmartScreen may prompt on first run
  ("More info" > "Run anyway"). GitHub shows each file's SHA-256 digest beside it on this page; compare it with the file you downloaded.
- The database gains two small migrations (014, 015) on first launch. Existing
  data is untouched; take a backup first (Settings, Data) if you want to be able
  to go back to 1.1.x.
- FTPS has been tested against an in-process test server, not against every
  server in the wild. If a server misbehaves, open an issue with the log
  (Settings, Data & About, Open log folder; turn on Verbose log first).
