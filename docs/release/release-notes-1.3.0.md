warpseed 1.3.0 — plain FTP, a third pane, and dragging to and from Explorer

## New

- **Plain FTP.** "FTP (no encryption)" joins SFTP and FTPS in the connection
  dialog, for older servers and devices that offer nothing else. It sends your
  password and files unencrypted, so use it only on a network you trust; the
  dialog says so.
- **A third pane.** The columns button in the header (or Ctrl+K, "Show third
  pane") adds a third file pane next to the other two, for example a folder on
  this PC and two servers. Two panes is still the default. Each divider can be
  dragged, the layout is remembered, and F5 / F6 send files to the pane you used
  most recently.
- **Drag files in from Explorer.** Dropping files or folders onto a server pane
  uploads them to the folder it shows, or to the folder row you drop on.
- **Drag files and folders out to Explorer or any file manager.** Drag from a pane
  out of the window and drop in Explorer, OneCommander or similar. This PC's files
  are offered as they are. A server's files and folders are queued as normal
  downloads into the folder you dropped on, so Hyperlane and resume apply.
- **Import sites from FileZilla or WinSCP.** "Import sites…" in the Connect window
  and in Settings > Sites reads a FileZilla export (or sitemanager.xml) or a
  WinSCP .ini export and adds its SFTP, FTP and FTPS sites; you can also drop the
  file onto either window. See the user guide for
  what comes across (FileZilla passwords do, WinSCP's do not).

## Changed

- **Server files are never moved to This PC.** Dragging or F5 copies them and the
  server keeps its originals; the Shift-move and F6 are no longer offered for
  that direction.
- **Single-lane downloads** no longer fail with "permission denied" on servers
  that refuse a size lookup on an open file.
- **Speed readout:** Settings > Transfers > Bandwidth can show each transfer's
  average speed instead of the live one.
- **Bandwidth settings** are laid out as plain label-and-control rows, and the
  Settings text is shorter, with hover tips on the less obvious options.
- The note shown during a drag is pinned to the bottom of the pane instead of
  trailing the cursor.
- **Long names and paths.** Downloads work into folders whose full path is longer
  than 260 characters, and names right up to Windows' 255-character limit keep
  their real name (only the in-progress file gets a stand-in name). A name over
  255 characters cannot exist on Windows at all; warpseed shortens that one
  (the start, a short code, the extension) and says so. The Recycle Bin cannot
  hold a path over 260 characters, so deleting one from This PC asks whether to
  delete it permanently instead.
- The title in the header reads "warpseed" with no gap. The Flight tab is always
  shown (greyed out until something is transferring) so the header no longer
  shifts; secondary text is darker or lighter to be readable in every theme;
  toasts appear under the header instead of over the queue; the status bar only
  mentions the database if it is unavailable.
- The header controls share one height and are vertically centred; the shadows
  on buttons, menus and cards are tighter; Deck cards keep their size and scroll
  instead of squashing; long file names wrap in confirmation dialogs.

## Notes

- Dragging out has been tried with Explorer and OneCommander. Other file managers
  should work if they accept an ordinary file drop; if a drop does nothing, open an
  issue with the file manager and Windows version.
- Dragging a server file out leaves an empty placeholder with the same name in the
  folder you drop on for a moment, until warpseed spots it and queues the download.
- Two servers in two panes cannot yet send files to each other directly.
