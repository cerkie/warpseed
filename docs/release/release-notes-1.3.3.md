warpseed 1.3.3 — undo for cancels, a calmer settings page, and a checksum option

## New

- **Undo for cancels.** When a cancel would throw away part-transferred data,
  warpseed pauses the rows first and offers *Undo* for eight seconds before
  cancelling for real. Closing the app in that window leaves them paused.
- **Filter the queue.** A box in the queue toolbar narrows the list by name or
  destination.
- **Resize the queue.** Drag the grip on its top edge to make it taller
  (double-click to reset). The column dividers are visible now and can be dragged
  from their full height; the File column starts wider.
- **The window remembers its size**, and whether it was maximised.
- **Check files after transfer** (Settings → Transfers, off by default). Compares
  each finished file with the server's copy by SHA-256 over SSH. SFTP servers
  with `sha256sum` only; other sites are skipped. A download that does not match
  is removed and marked failed so you can retry it, and a move never deletes its
  source before the check passes.
- **Remember each site's folder and sort** (off by default).
- **Only transfer at set hours** (off by default). Nothing new starts outside the
  hours you choose; the queue strip says it is waiting.
- **A speed limit per site**, in the site's settings, on top of the overall limit.
- **A low-disk warning** when a batch of downloads will not fit where it is going
  (on; can be turned off). The files still queue.
- **Queue finished toast with Show in folder**, when warpseed is in front.

## Changed

- **Folders in the queue start open**, so every file shows; click to fold one away.
- **Delete in the queue asks first** when two or more rows are selected, so Ctrl+A
  then Delete cannot clear the queue by accident.
- **Settings was redone.** Each section is a card of rows with the controls in one
  column, number boxes with the unit inside, switches without the On/Off word, and
  the same window height on every tab. The long explanations moved behind a small
  **i** (hover or focus); warnings stay on the page. Data & About is split into
  Data, Updates and Help and support. The settings button is a cog.
- **Plainer wording** in the close dialog and the server-key prompt.
- **The Connect window no longer changes size** when you switch protocol.
- **Queue columns line up** with their headings, including files inside a folder.
- The fork note on the About page reads the same at every window size.

## Fixed

- **The queue's column resize handles could barely be grabbed.** They were clipped
  to a 2px sliver and their dividers were invisible.
- **A slow queue.** Progress updates are now applied about four times a second in
  total instead of once per transfer per update.
