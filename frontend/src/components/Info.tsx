import { useRef, useState } from "react";
import { createPortal } from "react-dom";
import { InfoCircle } from "./Icon";

/** A small "i" that explains something on hover or keyboard focus.
 *
 *  For the longer "what does this do" text that used to sit under a setting.
 *  It is a button so the keyboard can reach it, and the text goes in a
 *  portal so a scrolling dialog cannot clip it. Things the user must not miss
 *  (warnings, errors) stay on the page instead. */
export default function Info({ children }: { children: React.ReactNode }) {
  const ref = useRef<HTMLButtonElement>(null);
  const [pos, setPos] = useState<{ left: number; top: number; above: boolean } | null>(null);

  const show = () => {
    const r = ref.current?.getBoundingClientRect();
    if (!r) return;
    const W = 280;
    const left = Math.max(8, Math.min(window.innerWidth - W - 8, r.left + r.width / 2 - W / 2));
    // Below the icon unless there is no room for it there.
    const above = r.bottom + 160 > window.innerHeight && r.top > 160;
    setPos({ left, top: above ? r.top - 6 : r.bottom + 6, above });
  };
  const hide = () => setPos(null);

  return (
    <>
      <button
        type="button"
        ref={ref}
        className="info"
        aria-label="More information"
        onMouseEnter={show}
        onMouseLeave={hide}
        onFocus={show}
        onBlur={hide}
        onKeyDown={(e) => e.key === "Escape" && pos && (e.stopPropagation(), hide())}
      >
        <InfoCircle size={14} />
      </button>
      {pos &&
        createPortal(
          <div
            className={`info__pop ${pos.above ? "info__pop--above" : ""}`}
            style={{ left: pos.left, top: pos.top }}
            role="tooltip"
          >
            {children}
          </div>,
          document.body,
        )}
    </>
  );
}
