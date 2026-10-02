import { useEffect, useRef, useState } from "react";
import { getPref, onPrefsHydrated, setPref } from "../lib/prefs";
import { useUiStore, type PaneSide } from "../store";
import FilePane from "./FilePane";

const MIN_WIDTH = 15; // percent of the area, per pane

const equal = (n: number) => Array.from({ length: n }, () => 100 / n);

/** Pane widths as percentages that add up to 100, remembered per pane count. */
function readWidths(count: 2 | 3): number[] {
  try {
    const saved = JSON.parse(getPref("ui.pane_widths") ?? "{}")[count];
    if (Array.isArray(saved) && saved.length === count && saved.every((w) => w >= MIN_WIDTH)) return saved;
  } catch {
    /* fall through */
  }
  if (count === 2) {
    // The two-pane divider used to be saved as one number: the left pane's share.
    const old = Number(getPref("ui.pane_split"));
    if (old >= 20 && old <= 80) return [old, 100 - old];
  }
  return equal(count);
}

function saveWidths(count: number, widths: number[]) {
  let all: Record<string, number[]> = {};
  try {
    all = JSON.parse(getPref("ui.pane_widths") ?? "{}") ?? {};
  } catch {
    /* start fresh */
  }
  all[count] = widths.map((w) => Math.round(w * 10) / 10);
  setPref("ui.pane_widths", JSON.stringify(all));
}

/** The file panes with a draggable divider between each pair. */
export default function PaneArea({ hidden }: { hidden: boolean }) {
  const count = useUiStore((s) => s.paneCount);
  const [widths, setWidths] = useState(() => readWidths(count));
  const area = useRef<HTMLDivElement>(null);

  // A different number of panes has its own remembered layout.
  useEffect(() => setWidths(readWidths(count)), [count]);
  useEffect(() => onPrefsHydrated(() => setWidths(readWidths(count))), [count]);

  /** Move divider `i` (between pane i and i+1) so the boundary sits at `to` percent. */
  const moveDivider = (i: number, to: number, from: number[]): number[] => {
    const before = from.slice(0, i).reduce((a, b) => a + b, 0);
    const after = before + from[i] + from[i + 1];
    const boundary = Math.min(after - MIN_WIDTH, Math.max(before + MIN_WIDTH, to));
    const next = [...from];
    next[i] = boundary - before;
    next[i + 1] = after - boundary;
    return next;
  };

  const startDrag = (i: number, e: React.PointerEvent<HTMLDivElement>) => {
    const box = area.current?.getBoundingClientRect();
    if (!box || box.width <= 0) return;
    e.preventDefault();
    const el = e.currentTarget;
    el.setPointerCapture(e.pointerId);
    const start = widths;
    let last = widths;
    const move = (ev: PointerEvent) => {
      last = moveDivider(i, ((ev.clientX - box.left) / box.width) * 100, start);
      setWidths(last);
    };
    const up = () => {
      el.removeEventListener("pointermove", move);
      el.removeEventListener("pointerup", up);
      el.removeEventListener("pointercancel", up);
      saveWidths(count, last);
    };
    el.addEventListener("pointermove", move);
    el.addEventListener("pointerup", up);
    el.addEventListener("pointercancel", up);
  };

  const nudge = (i: number, e: React.KeyboardEvent) => {
    if (e.key !== "ArrowLeft" && e.key !== "ArrowRight") return;
    e.preventDefault();
    const boundary = widths.slice(0, i + 1).reduce((a, b) => a + b, 0);
    const next = moveDivider(i, boundary + (e.key === "ArrowLeft" ? -2 : 2), widths);
    setWidths(next);
    saveWidths(count, next);
  };

  const template = widths.map((w) => `minmax(0, ${w}fr)`).join(" 8px ");
  const sides: PaneSide[] = count === 3 ? [0, 1, 2] : [0, 1];

  return (
    <div className="app__panes" ref={area} hidden={hidden} style={{ gridTemplateColumns: template }}>
      {sides.map((side, i) => (
        <PaneSlot
          key={side}
          side={side}
          divider={
            i < count - 1 ? (
              <div
                className="app__splitter"
                role="separator"
                aria-orientation="vertical"
                aria-label="Resize panes"
                aria-valuenow={Math.round(widths.slice(0, i + 1).reduce((a, b) => a + b, 0))}
                tabIndex={0}
                title="Drag to resize, double-click to reset"
                onPointerDown={(e) => startDrag(i, e)}
                onDoubleClick={() => {
                  setWidths(equal(count));
                  saveWidths(count, equal(count));
                }}
                onKeyDown={(e) => nudge(i, e)}
              />
            ) : null
          }
        />
      ))}
    </div>
  );
}

function PaneSlot({ side, divider }: { side: PaneSide; divider: React.ReactNode }) {
  return (
    <>
      <FilePane side={side} />
      {divider}
    </>
  );
}
