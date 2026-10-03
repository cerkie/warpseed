import { useEffect, useState } from "react";
import { checkForUpdate, dismissUpdate, on, type UpdateInfo } from "../ipc";
import { useUiStore } from "../store";
import { Close, Warning } from "./Icon";

/** "An update is available" strip across the top: a calm advisory with the
    action on the left and a dismiss on the right. Deliberately NOT a dialog; an
    update is never urgent enough to take the window, and a modal on launch is
    the thing people learn to close without reading.

    It only ever appears when Go says there is genuinely a newer release. A
    failed check shows nothing at all. Dismissing hides the strip until the next
    release; the status bar keeps a quiet reminder. */
export default function UpdateBanner({
  onShownChange,
}: {
  /** Told when the strip appears or goes, so the app grid can add its row. */
  onShownChange: (shown: boolean) => void;
}) {
  const info = useUiStore((s) => s.update);
  const setUpdate = useUiStore((s) => s.setUpdate);
  const setOpen = useUiStore((s) => s.setUpdateOpen);
  const [hidden, setHidden] = useState(false);

  const shown = info !== null && info.available && !hidden;
  useEffect(() => {
    onShownChange(shown);
  }, [shown, onShownChange]);

  useEffect(() => {
    // Go does the automatic check and emits only on a real find, so there is
    // no polling and no "checking…" state to render here.
    const off = on<UpdateInfo>("update:available", (u) => {
      if (!u.available) return;
      setUpdate(u);
      setHidden(u.dismissed);
    });
    return off;
  }, [setUpdate]);

  if (!info || !shown) return null;

  return (
    <div className="updbar" role="status">
      <button className="updbar__cta" onClick={() => setOpen(true)}>
        What&rsquo;s new
      </button>
      <Warning size={13} className="updbar__icon" />
      <span className="updbar__text">
        warpseed {info.latest} is available — you have {info.current}.
      </span>
      <span className="grow" />
      <button
        className="updbar__dismiss"
        title="Hide this until the next release"
        onClick={() => {
          void dismissUpdate(info.latest).catch(() => undefined);
          setHidden(true);
        }}
      >
        <Close size={12} />
        Dismiss
      </button>
    </div>
  );
}

/** Settings' "Check now": returns the result so the caller can report it,
    including the "you are up to date" case an automatic check stays silent
    about. */
export async function checkNow(): Promise<UpdateInfo> {
  return checkForUpdate();
}
