import { OnFileDrop } from "../../wailsjs/runtime/runtime";
import { enqueueDownloads, enqueueUploads, enqueueUploadsFromPaths } from "../ipc";
import { useUiStore, type PaneSide } from "../store";
import { recentHandoff, type Handoff } from "./dragOut";
import { inFolderZone } from "./dropZone";
import { importSitesFromFile } from "./sites";
import { toast } from "./toast";

/** Files dragged in from Explorer. A drop on a server pane uploads them into
    the folder it shows, or into the folder row the pointer is over. A
    FileZilla (.xml) or WinSCP (.ini) export dropped on the Connect or
    Settings window imports its sites. Targets mark themselves as drop targets
    in CSS (pane.css, overlays.css); a drop anywhere else is ignored by the
    runtime.

    A drag that warpseed itself took out of the window and that comes back
    carries no useful paths (a server file is only a placeholder), so it is
    finished from what the page remembers of it instead. */
export function installFileDrop() {
  OnFileDrop((x, y, paths) => {
    if (paths.length === 0) return;
    const hit = document.elementFromPoint(x, y);

    const handoff = recentHandoff();
    if (handoff) {
      dropHandoff(handoff, x, y, hit);
      return;
    }

    if (hit?.closest(".dialog")) {
      const file = paths.find((p) => /\.(xml|ini)$/i.test(p));
      if (!file) {
        toast("error", "Drop a FileZilla (.xml) or WinSCP (.ini) export to import its sites");
        return;
      }
      void importSitesFromFile(file)
        .then((m) => m && toast("success", m))
        .catch((err: unknown) => toast("error", String(err)));
      return;
    }

    const pane = hit?.closest<HTMLElement>("[data-pane]");
    if (!pane) return;
    const side = Number(pane.dataset.pane) as PaneSide;
    const target = useUiStore.getState().panes[side];
    if (typeof target.source !== "number") return;
    // A remote path is always POSIX, unless the server serves drive letters.
    const sep = /^[A-Za-z]:/.test(target.path) ? "\\" : "/";
    const folder = hit?.closest<HTMLElement>("[data-dir]")?.dataset.dir;
    const base = target.path.endsWith(sep) ? target.path : target.path + sep;
    const dest = folder ? base + folder : target.path;
    void enqueueUploadsFromPaths(target.source, paths, dest)
      .then(() => {
        useUiStore.getState().setQueueOpen(true);
        toast("success", `Queued ${paths.length} item${paths.length === 1 ? "" : "s"} for upload`);
      })
      .catch((err: unknown) => toast("error", String(err)));
  }, true);
}

/** Finish a drag that went out of the window and was let go back over a pane,
    the way the same drop would have gone had it never left. */
function dropHandoff(h: Handoff, x: number, y: number, hit: Element | null) {
  const pane = hit?.closest<HTMLElement>("[data-pane]");
  if (!pane) return;
  const side = Number(pane.dataset.pane) as PaneSide;
  if (side === h.side) return;
  const target = useUiStore.getState().panes[side];
  const fromServer = h.siteId !== null;
  if ((target.source === "local") === !fromServer) {
    toast("info", "Drop onto a pane for the other kind of place: This PC to a server, or the reverse");
    return;
  }
  const row = hit?.closest<HTMLElement>("[data-dir]");
  const aimed = row && inFolderZone(x, row.getBoundingClientRect(), side, h.side);
  const sep = target.source === "local" || /^[A-Za-z]:/.test(target.path) ? "\\" : "/";
  const base = target.path.endsWith(sep) ? target.path : target.path + sep;
  const dst = aimed && row?.dataset.dir ? base + row.dataset.dir : target.path;
  const items = h.items.map((e) => ({ src: e.path, size: e.size, isDir: e.isDir, modTime: e.modTime, move: false }));
  const queued = (what: string) => {
    useUiStore.getState().setQueueOpen(true);
    toast("success", `Queued ${items.length} item${items.length === 1 ? "" : "s"} for ${what}`);
  };
  if (fromServer && h.siteId !== null) {
    void enqueueDownloads(h.siteId, items, dst)
      .then(() => queued("download"))
      .catch((err: unknown) => toast("error", String(err)));
  } else if (typeof target.source === "number") {
    void enqueueUploads(
      target.source,
      items.map(({ src, size, isDir }) => ({ src, size, isDir, move: false })),
      dst,
    )
      .then(() => queued("upload"))
      .catch((err: unknown) => toast("error", String(err)));
  }
}
