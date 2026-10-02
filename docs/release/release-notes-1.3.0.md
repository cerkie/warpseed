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
- **Drag a file out to Explorer.** Drag a single file from a pane onto an Explorer
  window or the desktop to copy it there. Server files are fetched on demand.
  One file at a time for now; for several files or folders, use the queue.

## Notes

- Dragging out relies on the web view handing Explorer a download link. It has
  been built and tested up to that point but not on every Windows setup; if a
  drag-out does nothing, please open an issue with your Windows version.
- Two servers in two panes cannot yet send files to each other directly.
