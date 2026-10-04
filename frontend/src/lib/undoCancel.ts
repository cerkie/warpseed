import { cancelTransfers, pauseTransfer, resumeTransfer } from "../ipc";
import { toast } from "./toast";

/** How long "Undo" stays on offer. The cancel itself waits this long. */
export const UNDO_MS = 8000;

interface Row {
  id: number;
  state: string;
  conflict?: string | null;
}

/** Cancel transfers, but give the user a few seconds to take it back.
 *
 *  Cancelling throws away part-transferred data, so nothing is cancelled yet:
 *  running and waiting rows are paused (which keeps their progress) and the
 *  real cancel happens when the time is up. Undo just resumes them. Closing
 *  the app inside the window leaves the rows paused, never half-cancelled. */
export function cancelWithUndo(rows: Row[], after: () => void = () => undefined) {
  const ids = rows.map((r) => r.id);
  if (ids.length === 0) return;
  const paused = rows.filter((r) => (r.state === "active" || r.state === "pending") && !r.conflict).map((r) => r.id);
  void Promise.allSettled(paused.map((id) => pauseTransfer(id)));

  let settled = false;
  const timer = window.setTimeout(() => {
    if (settled) return;
    settled = true;
    void cancelTransfers(ids)
      .then((n) => toast("success", `Cancelled ${n} transfer${n === 1 ? "" : "s"}`))
      .catch((err: unknown) => toast("error", String(err)))
      .finally(after);
  }, UNDO_MS);

  const n = ids.length;
  toast("info", `Cancelling ${n} transfer${n === 1 ? "" : "s"}…`, {
    ms: UNDO_MS,
    action: {
      label: "Undo",
      run: () => {
        if (settled) return;
        settled = true;
        window.clearTimeout(timer);
        void Promise.allSettled(paused.map((id) => resumeTransfer(id))).then(() => {
          toast("success", `Kept ${n} transfer${n === 1 ? "" : "s"}`);
          after();
        });
      },
    },
  });
}
