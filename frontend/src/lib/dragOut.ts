import { on, startDragOut, type DragOutItem } from "../ipc";
import type { PaneSide } from "../store";

/* Dragging files out of a pane onto Explorer or any other file manager. The
   web view cannot hand files to other programs, so a pane drag stays an
   ordinary in-window drag until the pointer has clearly left the window; then
   the app hands it to Windows to continue as a native drag.

   While that native drag runs, Go streams the pointer's position back, so the
   panes can keep showing where a drop would land, and if the pointer is let go
   over warpseed itself the page finishes the drop from the drag it remembers. */

export interface Handoff {
  side: PaneSide;
  siteId: number | null;
  base: string;
  items: DragOutItem[];
}

let pending: Handoff | null = null;
let active: Handoff | null = null;
let recent: { h: Handoff; at: number } | null = null;

/** Remember what the drag that just started is carrying. */
export function armDragOut(side: PaneSide, siteId: number | null, base: string, items: DragOutItem[]) {
  pending = items.length ? { side, siteId, base, items } : null;
}

/** The drag now running natively, if any. */
export const activeHandoff = (): Handoff | null => active;

/** The native drag that is running, or one that ended a moment ago, which is
    how a drop back onto the window finds out what was being dragged. */
export function recentHandoff(): Handoff | null {
  if (active) return active;
  return recent && Date.now() - recent.at < 4000 ? recent.h : null;
}

export function initDragOut() {
  const disarm = () => {
    if (!active) pending = null;
  };
  window.addEventListener("dragend", disarm);
  window.addEventListener("drop", disarm);
  window.addEventListener("dragleave", (ev) => {
    if (!pending || active || ev.relatedTarget) return;
    // Leaving the window reports the pointer at an edge (Chromium uses 0,0).
    const edge = ev.clientX <= 0 || ev.clientY <= 0 || ev.clientX >= innerWidth - 1 || ev.clientY >= innerHeight - 1;
    if (!edge) return;
    // Go decides whether the pointer has really left; if it comes back, the
    // in-window drag carries on and this can fire again.
    void startDragOut(pending.siteId ?? 0, pending.items).catch(() => undefined);
  });
  on("dragout:begin", () => {
    active = pending;
  });
  on<{ x: number; y: number } | null>("dragout:pos", (pos) => {
    window.dispatchEvent(new CustomEvent("ws:handoffhover", { detail: pos }));
  });
  on("dragout:end", () => {
    if (active) recent = { h: active, at: Date.now() };
    active = null;
    pending = null;
    window.dispatchEvent(new CustomEvent("ws:handoffhover", { detail: null }));
    window.dispatchEvent(new CustomEvent("ws:handoffend"));
  });
}
