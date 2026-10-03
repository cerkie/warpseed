import { startDragOut, type DragOutItem } from "../ipc";

/* Dragging files out of a pane onto Explorer or any other file manager. The
   web view cannot hand files to other programs, so a pane drag stays an
   ordinary in-window drag until the pointer leaves the window; then the app
   hands it to Windows to continue as a native drag. */

let pending: { siteId: number | null; items: DragOutItem[] } | null = null;

/** Remember what the drag that just started is carrying. */
export function armDragOut(siteId: number | null, items: DragOutItem[]) {
  pending = { siteId, items };
}

export function initDragOut() {
  const disarm = () => {
    pending = null;
  };
  window.addEventListener("dragend", disarm);
  window.addEventListener("drop", disarm);
  window.addEventListener("dragleave", (ev) => {
    if (!pending || ev.relatedTarget) return;
    // Leaving the window reports the pointer at an edge (Chromium uses 0,0).
    const edge = ev.clientX <= 0 || ev.clientY <= 0 || ev.clientX >= innerWidth - 1 || ev.clientY >= innerHeight - 1;
    if (!edge) return;
    const { siteId, items } = pending;
    pending = null;
    void startDragOut(siteId ?? 0, items).catch(() => undefined);
  });
}
