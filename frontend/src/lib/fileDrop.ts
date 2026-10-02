import { OnFileDrop } from "../../wailsjs/runtime/runtime";
import { enqueueUploadsFromPaths } from "../ipc";
import { useUiStore } from "../store";
import { toast } from "./toast";

/** Files dragged in from Explorer. A drop on a server pane uploads them into
    the folder it shows, or into the folder row the pointer is over. Panes
    mark themselves as drop targets in CSS (pane.css); a drop anywhere else is
    ignored by the runtime. */
export function installFileDrop() {
  OnFileDrop((x, y, paths) => {
    const hit = document.elementFromPoint(x, y);
    const pane = hit?.closest<HTMLElement>("[data-pane]");
    if (!pane || paths.length === 0) return;
    const side = Number(pane.dataset.pane) as 0 | 1 | 2;
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
