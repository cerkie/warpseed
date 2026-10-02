import { useEffect, useState } from "react";
import CommandPalette from "./components/CommandPalette";
import PaneArea from "./components/PaneArea";
import DeckView from "./components/DeckView";
import MiniView from "./components/MiniView";
import TimelineView from "./components/TimelineView";
import FlightView from "./components/FlightView";
import { invalidateDir, purgeSource } from "./lib/treeCache";
import CloseGuardDialog from "./components/CloseGuardDialog";
import ConfirmDialog from "./components/ConfirmDialog";
import UpdateBanner from "./components/UpdateBanner";
import HostKeyDialog from "./components/HostKeyDialog";
import HistoryDialog from "./components/HistoryDialog";
import { Columns, Heart, Search, Shrink, Sliders, Slipstream } from "./components/Icon";
import QueueDock from "./components/QueueDock";
import QuickConnect from "./components/QuickConnect";
import SettingsDialog from "./components/SettingsDialog";
import Sparkline from "./components/Sparkline";
import Toasts from "./components/Toasts";
import { COMPANY, DONATE_URL } from "./lib/branding";
import { applyTheme, type ThemePref } from "./lib/theme";
import { getPref, onPrefsHydrated, remoteSide, setPref } from "./lib/prefs";
import { formOf } from "./lib/protocol";
import { installFileDrop } from "./lib/fileDrop";
import { initDragOut } from "./lib/dragOut";
import {
  connectAndHome,
  list,
  localStart,
  notify,
  on,
  openExternal,
  setMiniMode as ipcSetMiniMode,
  schemaVersion,
  setSetting,
  sites as fetchSites,
  type ConnState,
  type PaneSource,
  type TransferState,
  type FsChanged,
} from "./ipc";
import { useUiStore, type PaneSide } from "./store";
import "./App.css";

// Set once the saved panes are back, so the defaults never get written over them.
let savePanes = false;


export default function App() {
  const setPane = useUiStore((s) => s.setPane);
  const paneCount = useUiStore((s) => s.paneCount);
  const setPaneCount = useUiStore((s) => s.setPaneCount);
  const activePane = useUiStore((s) => s.activePane);
  const setActivePane = useUiStore((s) => s.setActivePane);
  const dbVersion = useUiStore((s) => s.dbSchemaVersion);
  const setDbSchemaVersion = useUiStore((s) => s.setDbSchemaVersion);
  const setSites = useUiStore((s) => s.setSites);
  const setConnState = useUiStore((s) => s.setConnState);
  const setPaletteOpen = useUiStore((s) => s.setPaletteOpen);
  const setQuickConnect = useUiStore((s) => s.setQuickConnect);
  const setSettingsOpen = useUiStore((s) => s.setSettingsOpen);
  const connStates = useUiStore((s) => s.connStates);
  const siteList = useUiStore((s) => s.sites);
  const transfers = useUiStore((s) => s.transfers);
  const viewMode = useUiStore((s) => s.viewMode);
  const miniMode = useUiStore((s) => s.miniMode);
  // The banner owns whether it is showing; App only needs to know so the grid
  // gains its row.
  const [updateShown, setUpdateShown] = useState(false);
  // Left pane width as a percentage of the browse area; persisted on release.

  // A desktop notification when the queue runs dry while the window is not in
  // front, so a long run can be left alone.
  useEffect(() => {
    const seen = new Map<number, string>();
    let busy = false;
    let finished = 0;
    let failed = 0;
    return useUiStore.subscribe((s) => {
      for (const t of s.transfers) {
        const prev = seen.get(t.id);
        if (prev === t.state) continue;
        seen.set(t.id, t.state);
        if (prev && t.state === "completed") finished++;
        if (prev && t.state === "failed") failed++;
      }
      const active = s.transfers.some((t) => ["pending", "dispatched", "active"].includes(t.state));
      if (active) {
        busy = true;
      } else if (busy) {
        busy = false;
        if (finished + failed > 0 && !document.hasFocus() && getPref("ui.notify") !== "0") {
          const text = failed > 0 ? `${finished} finished, ${failed} failed` : `${finished} finished`;
          void notify("warpseed — queue finished", text);
        }
        finished = failed = 0;
      }
    });
  }, []);

  // Remember which folder each pane was showing, once the saved ones have
  // been restored (saving earlier would overwrite them with the defaults).
  useEffect(() => {
    let timer: number | undefined;
    const off = useUiStore.subscribe((s, prev) => {
      if (!savePanes || s.panes === prev.panes) return;
      const remote = s.panes.findIndex((p) => typeof p.source === "number");
      if (remote >= 0 && String(remote) !== getPref("ui.remote_side")) setPref("ui.remote_side", String(remote));
      window.clearTimeout(timer);
      timer = window.setTimeout(() => setPref("ui.pane_state", JSON.stringify(s.panes)), 400);
    });
    return () => {
      off();
      window.clearTimeout(timer);
    };
  }, []);
  const setMiniMode = useUiStore((s) => s.setMiniMode);
  const setViewMode = useUiStore((s) => s.setViewMode);

  // A file dragged in from Explorer is uploaded when it lands on a server pane
  // (see lib/fileDrop.ts). Anywhere else it is not wanted, but WITHOUT this the
  // webview takes the drop as a navigation and replaces the whole app with the
  // file. There is no address bar to come back from, so the window is dead
  // until the user restarts it. The panes stopPropagation on the drags they
  // DO handle, so this only ever sees the ones nothing wanted.
  useEffect(() => {
    // A TEXT drop into an input is wanted, and no JS handler exists to claim
    // it on the field's behalf, so those are left to the browser. A drop
    // carrying FILES never is — an input is a big, central, inviting target,
    // and exempting it there would reopen the very hole this closes.
    const hasFiles = (e: DragEvent) => e.dataTransfer?.types.includes("Files") ?? false;
    const editable = (t: EventTarget | null) =>
      t instanceof HTMLElement &&
      (t.isContentEditable || t.tagName === "INPUT" || t.tagName === "TEXTAREA");
    const leaveAlone = (e: DragEvent) => editable(e.target) && !hasFiles(e);
    const block = (e: DragEvent) => {
      if (leaveAlone(e)) return;
      e.preventDefault();
      // Keep saying "no drop": preventDefault alone would offer a copy
      // cursor and promise something that is not going to happen.
      if (e.dataTransfer) e.dataTransfer.dropEffect = "none";
    };
    const swallow = (e: DragEvent) => {
      if (leaveAlone(e)) return;
      e.preventDefault();
    };
    window.addEventListener("dragover", block);
    window.addEventListener("drop", swallow);
    return () => {
      window.removeEventListener("dragover", block);
      window.removeEventListener("drop", swallow);
    };
  }, []);

  // Boot: home dirs, schema health, saved sites, backend event subscriptions.
  useEffect(() => {
    void schemaVersion().then(setDbSchemaVersion).catch(() => setDbSchemaVersion(0));
    void fetchSites().then(setSites).catch(() => undefined);
    // null = still hydrating. A completion arriving before settings resolve
    // is buffered, so a transfer finishing at launch cannot swallow the
    // one-time nudge — while a failed settings read (donateNudged stays
    // null) still never re-nags a long-time user.
    let donateNudged: boolean | null = null;
    let sawCompletion = false;
    const maybeNudge = () => {
      if (donateNudged !== false || !sawCompletion) return;
      donateNudged = true;
      window.dispatchEvent(
        new CustomEvent("ws:toast", {
          detail: {
            kind: "success",
            text: "Enjoying warpseed? It's free forever — the ♥ in the status bar buys us a coffee.",
          },
        }),
      );
      void setSetting("ui.donate_nudged", "1").catch(() => undefined);
    };
    // Settings are the source of truth for the theme and the folder local
    // panes open in; both are read once at boot.
    void import("./lib/prefs").then(({ hydratePrefs }) =>
      hydratePrefs()
        .then(async (cfg) => {
          // applyTheme coerces legacy and unknown values itself.
          if (cfg["ui.theme"]) applyTheme(cfg["ui.theme"] as ThemePref);
          donateNudged = cfg["ui.donate_nudged"] === "1";
          maybeNudge();

          initDragOut();
          installFileDrop();
          const start = await localStart(cfg["ui.local_default"]);
          const saved = readSavedPanes();
          for (const side of [0, 1, 2] as const) {
            const prev = saved[side];
            const folder =
              prev?.source === "local" && (await list("local", prev.path).then(() => true, () => false))
                ? prev.path
                : start;
            setPane(side, "local", folder);
            // A remote pane reconnects in the background; the local folder
            // above stands in until (and unless) it does.
            if (typeof prev?.source === "number") void restoreRemote(side, prev.source, prev.path, folder);
          }
          savePanes = true;
          // Sites marked "connect on launch" come up in the background; a
          // failure is not worth interrupting startup for. With no remote
          // pane to restore, the first one takes the pane remotes live in.
          const hadRemote = saved.some((p) => typeof p?.source === "number");
          void fetchSites().then((all) =>
            all.filter((s) => formOf(s).autoConnect).forEach(
              (s, i) =>
                void connectAndHome(s.id)
                  .then((home) => {
                    const side = remoteSide();
                    if (i === 0 && !hadRemote && useUiStore.getState().panes[side].source === "local") {
                      setPane(side, s.id, home);
                    }
                  })
                  .catch(() => undefined),
            ),
          );
        })
        .catch(async () => {
          savePanes = true;
          const home = await localStart();
          setPane(0, "local", home);
          setPane(1, "local", home);
          setPane(2, "local", home);
        }),
    );
    const lastConn: Record<number, string> = { ...useUiStore.getState().connStates };
    const offConn = on<ConnState>("site:connstate", (c) => {
      if (lastConn[c.siteId] !== c.state && c.state !== "connecting") {
        const site = useUiStore.getState().sites.find((x) => x.id === c.siteId);
        useUiStore
          .getState()
          .pushSessionEvent(c.state === "error" ? "err" : "info", `${site?.name ?? `site ${c.siteId}`} ${c.state}`);
      }
      lastConn[c.siteId] = c.state;
      setConnState(c.siteId, c.state);
      // A tree remembered across a disconnect can be a picture of a server we
      // are no longer talking to — and site ids are recycled, so it could even
      // belong to a different server. Purge on both edges.
      if (c.state === "disconnected" || c.state === "error" || c.state === "connected") {
        purgeSource(c.siteId);
      }
    });
    // The tree sidebar is unmounted whenever a pane closes it, so its cache
    // cannot own this subscription — it would miss every change made while
    // the sidebar was shut and then show them as though nothing had happened.
    const offTreeFs = on<FsChanged>("fs:changed", (ev) => {
      invalidateDir(ev.source === "local" ? "local" : ev.siteId, ev.dir);
    });
    // One-time nudge after the first transfer ever completes: point at the
    // status-bar heart, then never mention it again.
    const offDonate = on<TransferState>("transfer:state", (s) => {
      if (s.state !== "completed") return;
      sawCompletion = true;
      maybeNudge();
    });
    return () => {
      offConn();
      offTreeFs();
      offDonate();
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // Global keys: Tab pane switch, Ctrl+K palette (ux-spec §3.1).
  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      // The pill has one shortcut: Escape restores the window. Everything
      // else must not act on the hidden UI underneath.
      if (useUiStore.getState().miniMode) {
        if (e.key === "Escape") {
          // Always give the full UI back; a failed restore call only means
          // the window kept the pill size, which the user can fix by hand.
          useUiStore.getState().setMiniMode(false);
          void ipcSetMiniMode(false).catch(() =>
            window.dispatchEvent(
              new CustomEvent("ws:toast", {
                detail: { kind: "error", text: "Could not restore the window size" },
              }),
            ),
          );
        }
        return;
      }
      const inField =
        e.target instanceof HTMLInputElement ||
        e.target instanceof HTMLSelectElement ||
        e.target instanceof HTMLTextAreaElement;
      if (e.ctrlKey && e.key.toLowerCase() === "k") {
        e.preventDefault();
        setPaletteOpen(!useUiStore.getState().paletteOpen);
      } else if (e.key === "Tab" && !inField && !useUiStore.getState().settingsOpen) {
        // Never hijack Tab while a dialog is open — that would trap keyboard
        // users inside it with no way to reach its buttons.
        e.preventDefault();
        setActivePane(((useUiStore.getState().activePane + 1) % useUiStore.getState().paneCount) as PaneSide);
      } else if (e.ctrlKey && e.key === ",") {
        e.preventDefault();
        setSettingsOpen(true);
      } else if (e.key === "Escape") {
        if (useUiStore.getState().settingsOpen) setSettingsOpen(false);
        else if (useUiStore.getState().quickConnect.open) setQuickConnect(false);
      }
    };
    window.addEventListener("keydown", handler);
    return () => window.removeEventListener("keydown", handler);
  }, [setActivePane, setPaletteOpen, setQuickConnect, setSettingsOpen]);

  // The Browse/Flight toggle exists only while the queue holds live work.
  // Leaving flight mode is always the user's call — except when the toggle
  // itself disappears (queue emptied + cleared), which would strand them
  // on a view with no way back.
  const flightAvailable = transfers.some(
    (t) => t.state !== "completed" && t.state !== "cancelled",
  );
  const anyActive = transfers.some((t) => t.state === "active");
  useEffect(() => {
    if (!flightAvailable && useUiStore.getState().viewMode === "flight") {
      setViewMode("browse");
    }
  }, [flightAvailable, setViewMode]);

  const connectedCount = Object.values(connStates).filter((s) => s === "connected").length;
  const connecting = siteList.filter((s) => connStates[s.id] === "connecting");

  return (
    <div
      className={`app${miniMode ? " app--mini" : ""}${updateShown ? " app--update" : ""}`}
    >
      {/* First row when present, so it pushes the app down rather than
          covering the header. */}
      <UpdateBanner onShownChange={setUpdateShown} />
      {/* Mini mode overlays the pill and CSS-hides the rest of the tree —
          everything stays MOUNTED so pane state, the queue dock's event
          subscriptions, and scroll positions survive the round trip. */}
      {miniMode && <MiniView />}
      <header className="app__header">
        <span className="app__mark">
          <Slipstream size={18} className="app__glyph" />
          warp<span className="app__mark-accent">seed</span>
        </span>
        <span className="app__spacer" />
        <div className="viewseg" aria-label="View mode">
          <button
            className={`viewseg__btn${viewMode === "deck" ? " viewseg__btn--active" : ""}`}
            aria-pressed={viewMode === "deck"}
            onClick={() => setViewMode("deck")}
          >
            Deck
          </button>
          <button
            className={`viewseg__btn${viewMode === "browse" ? " viewseg__btn--active" : ""}`}
            aria-pressed={viewMode === "browse"}
            onClick={() => setViewMode("browse")}
          >
            Browse
          </button>
          <button
            className={`viewseg__btn${viewMode === "timeline" ? " viewseg__btn--active" : ""}`}
            aria-pressed={viewMode === "timeline"}
            onClick={() => setViewMode("timeline")}
          >
            Activity
          </button>
          {flightAvailable && (
            <button
              className={`viewseg__btn${viewMode === "flight" ? " viewseg__btn--active" : ""}`}
              aria-pressed={viewMode === "flight"}
              onClick={() => setViewMode("flight")}
            >
              Flight
              {anyActive && <span className="viewseg__dot" aria-hidden="true" />}
            </button>
          )}
        </div>
        <button
          className="omnibar"
          onClick={() => setPaletteOpen(true)}
          aria-label="Search files, sites — or type a command (Ctrl+K)"
        >
          <Search size={14} className="omnibar__icon" />
          <span className="omnibar__hint">Search files, sites — or type a command…</span>
          <span className="kbd">Ctrl K</span>
        </button>
        <button className="btn btn--primary" onClick={() => setQuickConnect(true, remoteSide())}>
          Connect
        </button>
        <button
          className="btn btn--icon"
          title={paneCount === 3 ? "Hide the third pane" : "Show a third pane"}
          aria-label={paneCount === 3 ? "Hide the third pane" : "Show a third pane"}
          aria-pressed={paneCount === 3}
          onClick={() => setPaneCount(paneCount === 3 ? 2 : 3)}
        >
          <Columns size={15} />
        </button>
        <button
          className="btn btn--icon"
          title="Minimize to pill"
          aria-label="Minimize to an always-on-top pill"
          onClick={() => {
            // Hide the UI only once the window really shrank — on IPC
            // failure the full window keeps its full UI.
            void ipcSetMiniMode(true)
              .then(() => setMiniMode(true))
              .catch(() =>
                window.dispatchEvent(
                  new CustomEvent("ws:toast", {
                    detail: { kind: "error", text: "Could not enter mini mode" },
                  }),
                ),
              );
          }}
        >
          <Shrink size={14} />
        </button>
        <button
          className="btn btn--icon"
          title="Settings (Ctrl+,)"
          aria-label="Settings"
          onClick={() => setSettingsOpen(true)}
        >
          <Sliders size={15} />
        </button>
      </header>

      <main className="app__main">
        {connecting.length > 0 && <div className="app__connecting" role="progressbar" aria-label="Connecting" />}
        {/* Panes stay mounted (display:none) in flight mode so pane state,
            scroll position and virtualizer measurements survive the trip. */}
        <PaneArea hidden={viewMode !== "browse"} />
        {viewMode === "flight" && <FlightView />}
        {viewMode === "deck" && <DeckView />}
        {viewMode === "timeline" && <TimelineView />}
      </main>

      <QueueDock />

      <footer className="app__statusbar">
        <span className="statusbar__conn">
          {connecting.length > 0 ? (
            <>
              <span className="conn-dot conn-dot--connecting" />
              Connecting to {connecting.map((c) => c.name).join(", ")}…
            </>
          ) : connectedCount > 0 ? (
            <>
              <span className="conn-dot conn-dot--connected" />
              {connectedCount} site{connectedCount > 1 ? "s" : ""} connected
            </>
          ) : (
            <>
              <span className="conn-dot" />
              offline · {siteList.length} saved site{siteList.length === 1 ? "" : "s"}
            </>
          )}
        </span>
        <Sparkline />
        <span className="spacer" />
        <button
          className="statusbar__heart"
          title={`Support warpseed — buy ${COMPANY} a coffee`}
          aria-label="Support warpseed development"
          onClick={() => openExternal(DONATE_URL)}
        >
          <Heart size={13} />
        </button>
        <span className={`statusbar__db${dbVersion > 0 ? "" : " status--warn"}`}>
          {dbVersion > 0 ? `db v${dbVersion}` : "db unavailable"}
        </span>
      </footer>

      <CommandPalette />
      <QuickConnect />
      <SettingsDialog />
      <HistoryDialog />
      <HostKeyDialog />
      <CloseGuardDialog />
      <ConfirmDialog />
      <Toasts />
    </div>
  );
}

interface SavedPane {
  source: PaneSource;
  path: string;
}

function readSavedPanes(): (SavedPane | undefined)[] {
  try {
    const v = JSON.parse(getPref("ui.pane_state") ?? "[]");
    return Array.isArray(v) ? v : [];
  } catch {
    return [];
  }
}

/** Put a pane back on the server folder it was showing, if the site still
    exists and the connection works; otherwise leave it where it is. */
async function restoreRemote(side: PaneSide, siteId: number, path: string, standIn: string) {
  try {
    if (!(await fetchSites()).some((s) => s.id === siteId)) return;
    const home = await connectAndHome(siteId, path);
    const cur = useUiStore.getState().panes[side];
    // The user may have moved on while the connection came up.
    if (cur.source === "local" && cur.path === standIn) useUiStore.getState().setPane(side, siteId, home);
  } catch {
    /* stay on the local folder */
  }
}
