import type { PaneSide } from "../store";

/** How much of the pane's width, counted from the edge a drag came in by, drops
    into the pane's own folder. Past it, folder rows become targets. So pulling
    a file only a little way in drops it in the current folder, and carrying it
    further in lets you aim at a folder. */
export const ROOT_ZONE = 0.3;

/** The edge of a pane a drag enters from: the side facing the pane it started
    in. Panes sit left to right in side order. null for a drag within one pane. */
export function entryEdgeOf(from: PaneSide | null, dest: PaneSide): "left" | "right" | null {
  if (from === null || from === dest) return null;
  return from < dest ? "left" : "right";
}

/** Whether a pointer at clientX over a folder row (whose box is given) is aimed
    at the folder, for a drag that started in pane `from`. */
export function inFolderZone(clientX: number, box: { left: number; width: number }, dest: PaneSide, from: PaneSide | null): boolean {
  const edge = entryEdgeOf(from, dest);
  if (!edge) return true;
  const frac = (clientX - box.left) / box.width;
  return edge === "left" ? frac > ROOT_ZONE : frac < 1 - ROOT_ZONE;
}
