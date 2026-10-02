import { useEffect, useState } from "react";
import { transferHistory, type HistoryEntry } from "../ipc";
import { formatSize } from "../lib/format";
import { useUiStore } from "../store";

const name = (p: string) => p.split(/[\/]/).pop() ?? p;

/** Finished transfers that have been cleared from the queue. */
export default function HistoryDialog() {
  const open = useUiStore((s) => s.historyOpen);
  const setOpen = useUiStore((s) => s.setHistoryOpen);
  const [rows, setRows] = useState<HistoryEntry[] | null>(null);

  useEffect(() => {
    if (open) void transferHistory().then(setRows).catch(() => setRows([]));
    else setRows(null);
  }, [open]);

  if (!open) return null;
  return (
    <div className="scrim scrim--center" onMouseDown={() => setOpen(false)}>
      <div
        className="dialog dialog--history"
        role="dialog"
        aria-label="Transfer history"
        onMouseDown={(e) => e.stopPropagation()}
        onKeyDown={(e) => e.key === "Escape" && setOpen(false)}
      >
        <h2>Transfer history</h2>
        {rows === null ? (
          <p className="set-note">Loading…</p>
        ) : rows.length === 0 ? (
          <p className="set-note">
            Nothing here yet. Finished transfers appear here once you clear them from the queue.
          </p>
        ) : (
          <div className="history-list">
            {rows.map((r, i) => (
              <div key={i} className="history-row" title={`${r.src} → ${r.dst}`}>
                <span className="history-row__dir">{r.direction === "upload" ? "Up" : "Down"}</span>
                <span className="history-row__name">{name(r.src)}</span>
                <span className="history-row__meta">
                  {r.size >= 0 ? formatSize(r.size) : ""} · {r.siteName || "deleted site"} ·{" "}
                  {new Date(r.finishedAt).toLocaleString()}
                </span>
              </div>
            ))}
          </div>
        )}
        <div className="dialog__actions">
          <button className="btn" autoFocus onClick={() => setOpen(false)}>
            Close
          </button>
        </div>
      </div>
    </div>
  );
}
