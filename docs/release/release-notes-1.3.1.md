warpseed 1.3.1 — updates from inside the app, a calmer queue, and safer deleting

## New

- **Update from inside the app.** When a newer release is out, a strip across the
  top and a note in the status bar say so. **What's new** shows that release's
  notes. **Install & restart** downloads the release, checks it against the
  SHA-256 checksum GitHub publishes for it, and restarts into the new version (an
  installed copy runs the installer silently; a portable exe is replaced in
  place). Nothing is downloaded until you press it. If a release has no checksum,
  or warpseed can't write where it lives, it opens the release page instead, and
  a failed install leaves you on the version you had, with the reason.

## Fixed

- **Queue percentages are per file.** A file that had not started yet could show
  a leftover percentage from an earlier transfer, then jump back to 0% when its
  download began. A row now shows its own progress only.
- **Cancelled uploads that never left the list.** Some servers answer "permission
  denied" when asked to delete a leftover file that was never there, which kept the
  cancelled row in the queue through every Clear done. A cancelled row that moved
  no data is now cleared regardless.
- **The "not allowed" cursor flashing between rows.** While dragging files over a
  pane, the cursor briefly showed a red X each time the pointer crossed from one
  row to the next. The pane now accepts the drag the moment it enters a row, so
  the cursor stays steady.
- **Nothing is highlighted in a freshly opened folder.** Each pane used to
  highlight its first row on its own. Now a row is highlighted only after you
  click it, move onto it with the arrow keys (the first press lands on the first
  row), or go up into a folder you came from. Until then F5, F6, Enter and
  Delete have no hidden row to act on.
- **Delete could hit the wrong thing.** F6, Delete and F8 only act when keyboard
  focus is in the pane. Before, pressing Delete while the queue, a dialog or
  nothing had focus deleted whatever the pane still had highlighted. Clicking
  inside the open queue now gives it the keyboard, so Ctrl+A and Delete there
  act on its rows.

## Changed

- **A folder is one row in the queue.** Files queued from one folder (downloads
  or uploads) show as a single row with the folder name, "3 of 9 done", and a
  combined progress bar. Click it to see the files. Its buttons pause, resume or
  cancel the whole folder. Folders queued before this update keep showing their
  files individually.
- **Finished downloads no longer sit in the queue list.** Deck and Activity still
  show them for the session, and Clear done moves them to Transfer history.
- **Long waits fold up.** Single files waiting their turn show the first ten,
  then "+N more waiting" (click to show them all).
- **A quieter status strip.** Needs-a-decision, failed and paused are plain
  coloured words in one line instead of separate pills.
- **Dropping into a folder is deliberate.** When you drag from one pane to another,
  the first 30% of the target pane, counted from the edge you came in by, drops
  into the folder the pane is showing (the whole pane is outlined). Carry the file
  past it and the folder under the pointer lights up instead. The zone is
  invisible and follows whichever side the drag comes from, so it works for any
  pane layout. The pane's status line says where a drop would land ("Drop into ·
  datasets" or "Drop here · Incoming").
- **Dragging out is less twitchy.** A drag only goes out to Windows once the
  pointer is clearly outside the window, so brushing the edge keeps your drag and
  its indicators. When a drag has gone out and comes back, the panes still show
  where a drop would land, and letting go over a pane finishes the drop the way it
  would have inside the window (a copy to that pane, into the folder you aimed at).
- **Updates come from this fork only.** The "Original" choice for where to look
  for releases is gone: this fork's version numbers run ahead of the original's,
  so it could never find anything, and if it ever did it would have offered a
  build without the fork's changes.
- **About says plainly that this is a fork.** Zyra Labs built the engine and the
  idea, so donations (the heart, and the Support button) still go to them. Bug
  reports sent from inside the app go to this fork's issue page, not theirs. The
  About tab links both projects.
