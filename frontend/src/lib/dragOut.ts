import { dragBase } from "../ipc";

/* Dragging a file out of a pane onto Explorer. The app serves files on a local
   address; the drag hands Explorer a link to it (the DownloadURL drag type),
   and Explorer fetches it on drop. Single files only: the drag type carries
   one link, and a folder has no single file to link to. */

let base = "";
export const initDragOut = () => void dragBase().then((b) => (base = b)).catch(() => undefined);

/** The DownloadURL value for a file, or null when drag-out is unavailable. */
export function downloadUrl(siteId: number | null, fullPath: string, name: string): string | null {
  if (!base) return null;
  const safe = name.replace(/[:\/]/g, "_"); // the format splits on colons
  const url = `${base}?site=${siteId ?? 0}&path=${encodeURIComponent(fullPath)}`;
  return `application/octet-stream:${safe}:${url}`;
}
