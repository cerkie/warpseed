/* UI state only — directory contents stay in the pane hook (server state).
   Transfers are a live event-driven mirror of the queue (refetched on
   queue:changed; progress overlaid from transfer:progress events). */
import { create } from "zustand";
import { transfersList, type PaneSource, type Site, type Transfer } from "./ipc";
import { getPref, setPref } from "./lib/prefs";

function readPaneCount(): 2 | 3 {
  return getPref("ui.pane_count") === "3" ? 3 : 2;
}

function readHiddenCols(): string[] {
  try {
    const v = JSON.parse(getPref("ui.pane_hidden") ?? "[]");
    return Array.isArray(v) ? v : [];
  } catch {
    return [];
  }
}
import type { PromptSpec } from "./components/PromptDialog";

interface ProgressSample {
  bytes: number;
  at: number; // ms timestamp of last sample
  rate: number; // bytes/sec: smoothed live speed, or the average (ui.speed_mode)
  t0?: number; // start of the average window
  b0?: number; // bytes at the start of it
  chunks?: number[]; // per-connection completion fractions
}

export type PaneSide = 0 | 1 | 2;

/** One line of the flight-view session log (ring buffer, newest first). */
export interface SessionEvent {
  at: number; // epoch ms
  kind: "info" | "ok" | "err";
  text: string;
}

const SESSION_LOG_CAP = 100;

/** Every refresh read gets a ticket; only the newest ticket may land, so
    two reads resolving out of order cannot roll the list back. A read that
    started before a transfer:state patch would also roll that row back, so
    patches are remembered with the ticket current at the time and overlaid
    on any list whose read started earlier. Dropping such reads instead
    would, under sustained churn, mean no list ever lands. */
let refreshTicket = 0;
interface Patch {
  state: string;
  error?: string;
  ticket: number;
}
const patches = new Map<number, Patch>();

interface PaneState {
  source: PaneSource;
  path: string;
}

interface UiState {
  /** Three slots; only the first `paneCount` are shown. */
  panes: [PaneState, PaneState, PaneState];
  paneCount: 2 | 3;
  activePane: PaneSide;
  /** Panes from most to least recently active, for picking a transfer target. */
  activeOrder: PaneSide[];
  dbSchemaVersion: number;
  sites: Site[];
  connStates: Record<number, string>;
  paletteOpen: boolean;
  quickConnect: { open: boolean; side: PaneSide };
  transfers: Transfer[];
  progress: Record<number, ProgressSample>;
  queueOpen: boolean;
  /** Queue-wide pause, mirrored from the backend (queue:paused). */
  queuePaused: boolean;
  settingsOpen: boolean;
  historyOpen: boolean;
  /** File-list columns the user has hidden ("psize", "pdate"), shared by both panes. */
  hiddenCols: string[];
  closeGuardOpen: boolean;
  viewMode: "browse" | "flight" | "deck" | "timeline";
  confirm: PromptSpec | null;
  miniMode: boolean;
  sessionLog: SessionEvent[];

  setPane: (side: PaneSide, source: PaneSource, path: string) => void;
  setPath: (side: PaneSide, path: string) => void;
  setActivePane: (side: PaneSide) => void;
  setPaneCount: (n: 2 | 3) => void;
  setDbSchemaVersion: (v: number) => void;
  setSites: (s: Site[]) => void;
  setConnState: (siteId: number, state: string) => void;
  setPaletteOpen: (open: boolean) => void;
  setQuickConnect: (open: boolean, side?: PaneSide) => void;
  /** Refetch the queue list — the only write path for it; stale or
      superseded responses are dropped. */
  refreshTransfers: () => Promise<void>;
  applyProgress: (id: number, bytes: number, size: number, chunks?: number[]) => void;
  patchTransferState: (id: number, state: string, error?: string) => void;
  setQueueOpen: (open: boolean) => void;
  setQueuePaused: (on: boolean) => void;
  setSettingsOpen: (open: boolean) => void;
  setHistoryOpen: (open: boolean) => void;
  toggleColumn: (id: string) => void;
  setCloseGuardOpen: (open: boolean) => void;
  /** Raise a confirmation. Destructive actions go through this rather than
      calling their IPC directly; see askConfirm's note. */
  askConfirm: (spec: PromptSpec) => void;
  closeConfirm: () => void;
  setViewMode: (mode: "browse" | "flight" | "deck" | "timeline") => void;
  setMiniMode: (on: boolean) => void;
  pushSessionEvent: (kind: SessionEvent["kind"], text: string) => void;
}

// Warnings the user has silenced for this run. Module scope, not persisted:
// see askConfirm.
const suppressed = new Set<string>();

export const useUiStore = create<UiState>((set) => ({
  panes: [
    { source: "local", path: "" },
    { source: "local", path: "" },
    { source: "local", path: "" },
  ],
  paneCount: readPaneCount(),
  activePane: 0,
  activeOrder: [0, 1, 2],
  dbSchemaVersion: 0,
  sites: [],
  connStates: {},
  paletteOpen: false,
  quickConnect: { open: false, side: 1 },
  transfers: [],
  progress: {},
  queueOpen: false,
  queuePaused: false,
  settingsOpen: false,
  historyOpen: false,
  hiddenCols: readHiddenCols(),
  closeGuardOpen: false,
  confirm: null,
  viewMode: "browse",
  miniMode: false,
  sessionLog: [],

  setPane: (side, source, path) =>
    set((s) => {
      const panes = [...s.panes] as UiState["panes"];
      panes[side] = { source, path };
      return { panes };
    }),
  setPath: (side, path) =>
    set((s) => {
      const panes = [...s.panes] as UiState["panes"];
      panes[side] = { ...panes[side], path };
      return { panes };
    }),
  setActivePane: (side) =>
    set((s) => ({ activePane: side, activeOrder: [side, ...s.activeOrder.filter((p) => p !== side)] })),
  setPaneCount: (paneCount) => {
    setPref("ui.pane_count", String(paneCount));
    set((s) => ({ paneCount, activePane: s.activePane >= paneCount ? 0 : s.activePane }));
  },
  setDbSchemaVersion: (v) => set({ dbSchemaVersion: v }),
  setSites: (sites) => set({ sites }),
  setConnState: (siteId, state) =>
    set((s) => ({ connStates: { ...s.connStates, [siteId]: state } })),
  setPaletteOpen: (open) => set({ paletteOpen: open }),
  setQuickConnect: (open, side) =>
    set((s) => ({ quickConnect: { open, side: side ?? s.quickConnect.side } })),
  refreshTransfers: () => {
    const mine = ++refreshTicket;
    return transfersList()
      .then((list) => {
        if (mine !== refreshTicket) return;
        // The dispatcher writes a state before it emits it, so a patch
        // received before this read started is already in the rows; only
        // later ones need overlaying, and the earlier ones can be forgotten.
        for (const [id, p] of patches) if (p.ticket <= mine) patches.delete(id);
        const transfers = patches.size
          ? list.map((t) => {
              const p = patches.get(t.id);
              return p ? { ...t, state: p.state, error: p.error ?? t.error } : t;
            })
          : list;
        set({ transfers });
      })
      .catch(() => undefined);
  },
  applyProgress: (id, bytes, _size, chunks) =>
    set((s) => {
      const now = performance.now();
      const prev = s.progress[id];
      let rate = prev?.rate ?? 0;
      // The average counts from the first sample, or from the first one after
      // a pause (a gap of several seconds), so a paused transfer does not
      // drag its own average down.
      let t0 = prev?.t0 ?? now;
      let b0 = prev?.b0 ?? bytes;
      if (prev && now - prev.at > 5000) {
        t0 = now;
        b0 = bytes;
      }
      if (prev && now > prev.at) {
        const inst = ((bytes - prev.bytes) * 1000) / (now - prev.at);
        rate = prev.rate === 0 ? inst : prev.rate * 0.7 + inst * 0.3; // EMA smoothing
        if (getPref("ui.speed_mode") === "average" && now - t0 > 2000) {
          rate = ((bytes - b0) * 1000) / (now - t0);
        }
      }
      return {
        progress: { ...s.progress, [id]: { bytes, at: now, rate, t0, b0, chunks: chunks ?? prev?.chunks } },
      };
    }),
  patchTransferState: (id, state, error) => {
    patches.set(id, { state, error, ticket: refreshTicket });
    set((s) => ({
      transfers: s.transfers.map((t) =>
        t.id === id ? { ...t, state, error: error ?? t.error } : t,
      ),
    }));
  },
  setQueueOpen: (queueOpen) => set({ queueOpen }),
  setQueuePaused: (queuePaused) => set({ queuePaused }),
  setSettingsOpen: (settingsOpen) => set({ settingsOpen }),
  setHistoryOpen: (historyOpen) => set({ historyOpen }),
  toggleColumn: (id) =>
    set((s) => {
      const hiddenCols = s.hiddenCols.includes(id) ? s.hiddenCols.filter((c) => c !== id) : [...s.hiddenCols, id];
      setPref("ui.pane_hidden", JSON.stringify(hiddenCols));
      return { hiddenCols };
    }),
  setCloseGuardOpen: (closeGuardOpen) => set({ closeGuardOpen }),

  // Every destructive action asks first, from one place, so the wording and
  // the escape hatch stay consistent and no new delete button can quietly
  // ship without one. A suppressKey lets the user silence that ONE kind of
  // warning for the rest of the run — agreeing to skip the file-delete
  // prompt says nothing about cancelling a transfer. Suppression lives in
  // memory on purpose: a relaunch asks again, because a habit formed during
  // one session should not follow someone into the next.
  askConfirm: (spec) => {
    if (spec.suppressKey && suppressed.has(spec.suppressKey)) {
      spec.onConfirm("");
      return;
    }
    set({
      confirm: {
        ...spec,
        onSuppress: (on) => {
          if (on && spec.suppressKey) suppressed.add(spec.suppressKey);
        },
      },
    });
  },
  closeConfirm: () => set({ confirm: null }),
  setViewMode: (viewMode) => set({ viewMode }),
  setMiniMode: (miniMode) => set({ miniMode }),
  pushSessionEvent: (kind, text) =>
    set((s) => ({
      sessionLog: [{ at: Date.now(), kind, text }, ...s.sessionLog].slice(0, SESSION_LOG_CAP),
    })),
}));

/** The pane a transfer or move from `side` should go to: the most recently
    used visible pane other than itself. */
export function otherSide(side: PaneSide): PaneSide {
  const { activeOrder, paneCount } = useUiStore.getState();
  return activeOrder.find((p) => p !== side && p < paneCount) ?? (side === 0 ? 1 : 0);
}
