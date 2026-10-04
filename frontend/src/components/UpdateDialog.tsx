import { useEffect, useState } from "react";
import { installUpdate, on, type UpdateState } from "../ipc";
import { formatSize } from "../lib/format";
import { friendlyError } from "../lib/errors";
import { useUiStore } from "../store";
import Markdown from "./Markdown";

/** "What's new" for the newest release, with the install button. Opened from
    the banner, the status bar or Settings. It shows the release's own notes,
    and installs from inside the app when the release can be checked against its
    published checksum; otherwise the only action is the release page. */
export default function UpdateDialog() {
  const info = useUiStore((s) => s.update);
  const open = useUiStore((s) => s.updateOpen);
  const setOpen = useUiStore((s) => s.setUpdateOpen);
  const running = useUiStore((s) => s.transfers.filter((t) => t.state === "active").length);
  const [state, setState] = useState<UpdateState | null>(null);

  useEffect(() => on<UpdateState>("update:state", setState), []);

  if (!open || !info) return null;
  const busy = state !== null && state.phase !== "failed";
  const close = () => {
    if (!busy) setOpen(false);
  };
  const pct = state?.total ? Math.min(100, Math.round((state.done / state.total) * 100)) : 0;
  const date = info.published ? new Date(info.published).toLocaleDateString() : "";

  const install = () => {
    setState({ phase: "downloading", done: 0, total: 0 });
    installUpdate().catch((err: unknown) =>
      setState({ phase: "failed", done: 0, total: 0, error: friendlyError(err) }),
    );
  };

  return (
    <div className="scrim scrim--center" onMouseDown={close}>
      <div
        className="dialog dialog--update"
        role="dialog"
        aria-label="What's new"
        onMouseDown={(e) => e.stopPropagation()}
        onKeyDown={(e) => e.key === "Escape" && close()}
      >
        <h2>warpseed {info.latest} is available</h2>
        <p className="set-note">
          You have {info.current}.{date && ` Released ${date}.`}
          {info.name && info.name !== `warpseed ${info.latest}` && ` ${info.name}`}
        </p>

        <div className="upd-notes">
          {info.notes.trim() ? <Markdown text={info.notes} /> : <p>No release notes were published.</p>}
        </div>

        {running > 0 && !busy && (
          <p className="set-note">
            {running} transfer{running === 1 ? " is" : "s are"} running. {running === 1 ? "It stops" : "They stop"} while warpseed
            restarts, then {running === 1 ? "carries" : "carry"} on.
          </p>
        )}
        {state?.phase === "downloading" && (
          <div className="upd-progress" role="progressbar" aria-valuenow={pct}>
            <div style={{ width: `${pct}%` }} />
            <span>
              Downloading… {state.total ? `${formatSize(state.done)} of ${formatSize(state.total)}` : ""}
            </span>
          </div>
        )}
        {state?.phase === "installing" && <p className="set-note">Installing. warpseed will restart in a moment.</p>}
        {state?.phase === "failed" && <div className="form-error">{state.error}</div>}
        {!info.canInstall && (
          <p className="set-note">
            This release can&rsquo;t be installed from here, so the button opens its page instead.
          </p>
        )}

        <div className="dialog__actions">
          <button className="btn" onClick={close} disabled={busy}>
            Not now
          </button>
          <button className="btn" onClick={() => window.open(info.url, "_blank", "noopener")} disabled={busy}>
            Release page
          </button>
          {info.canInstall && (
            <button className="btn btn--primary" onClick={install} disabled={busy}>
              {busy ? "Working…" : "Install & restart"}
            </button>
          )}
        </div>
      </div>
    </div>
  );
}
