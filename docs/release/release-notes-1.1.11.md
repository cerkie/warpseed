warpseed 1.1.11 — drag files between the panes

**Drag a file from one pane to the other and it transfers.** Drag from the
site to This PC and it downloads; drag the other way and it uploads. The
same thing F5 has always done, now reachable with the mouse.

## New

- **Drag and drop between the panes.** Pick up one file or a whole marked
  selection and drop it on the other pane. Dropping on the pane puts the
  files in the folder it is showing; dropping straight onto a folder row
  puts them in that folder instead, and the row says so while you hover it.
  Folders come too, with everything inside them.

- Dragging a row that is not part of your selection drags just that row, the
  way right-click already behaves. Drag one that *is* part of the selection
  and the whole selection goes.

## Notes

- **Dragging within one pane does nothing, and neither does dragging between
  two panes of the same kind.** That would be a move, which deletes the file
  it came from, and warpseed does not do that yet. Rather than accept the
  drop and then refuse it, those drags simply are not offered: you get the
  "no drop" cursor and nothing happens.
- The listing does not scroll while you are dragging over it, so if the
  folder you want is off screen, scroll the destination pane to it before you
  pick the files up. Every folder row you can see is a target, and the pane
  itself always is.
- Dragging in from Explorer, or out to it, is still not supported. That needs
  something the window framework does not expose. A file dropped in from
  Explorer is now ignored rather than replacing the window with the file,
  which is what used to happen.
- A drop never overwrites silently. The same rules you set under *When the
  file already exists* apply exactly as they do to F5.
