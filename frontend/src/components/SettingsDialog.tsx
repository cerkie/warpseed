import { useEffect, useState } from "react";
import {
  backupData,
  dataLocation,
  getSettings,
  openDataFolder,
  logDir,
  openExternal,
  type DataInfo,
  saveSite,
  setSetting,
  sites as fetchSites,
  type Site,
  appVersion,
  checkForUpdate,
  updateRepo,
} from "../ipc";
import { COMPANY, DONATE_URL, WEBSITE_URL, bugReportUrl } from "../lib/branding";
import { formatSize } from "../lib/format";
import { applyTheme, coerceTheme, THEMES, type ThemePref } from "../lib/theme";
import { DEFAULT_FORM, DEFAULT_PORT, formFields, formOf, secretLabel, type SiteForm } from "../lib/protocol";
import ProtocolField from "./ProtocolField";
import { askDeleteSite } from "../lib/sites";
import Switch from "./Switch";
import { friendlyError } from "../lib/errors";
import { setPref } from "../lib/prefs";
import AutoConnectField from "./AutoConnectField";
import { useUiStore } from "../store";
import { Bug, ChevronRight, Close, Heart } from "./Icon";

const MIB = 1024 * 1024;

const TABS = [
  { id: "general", label: "General" },
  { id: "transfers", label: "Transfers" },
  { id: "sites", label: "Sites" },
  { id: "about", label: "Data & About" },
] as const;

interface SiteDraft extends SiteForm {
  id: number;
  name: string;
  host: string;
  port: number;
  username: string;
  remotePath: string;
  maxTransfers: number;
  password: string; // empty = leave stored credential unchanged
}

function draftFrom(s: Site): SiteDraft {
  return {
    id: s.id,
    name: s.name,
    host: s.host,
    port: s.port,
    username: s.username,
    remotePath: s.remotePath ?? "",
    maxTransfers: s.maxTransfers ?? 0,
    password: "",
    ...formOf(s),
  };
}

const blankDraft = (): SiteDraft => ({
  id: 0,
  name: "",
  host: "",
  port: DEFAULT_PORT.sftp,
  username: "",
  remotePath: "",
  maxTransfers: 0,
  password: "",
  ...DEFAULT_FORM,
});

/** Settings (Ctrl+,): Appearance, Transfers, Bandwidth, and the site editor
    (feedback batch items 1, 2, 7, 8, 9). Values save on change. */
// Order and wording of the overwrite rules. Mirrors
// queue.ConflictSettingKeys; the defaults here are the same defaults the
// store falls back to, so the dropdown never shows something the backend
// would not do.
const CONFLICT_RULES = [
  { key: "transfers.conflict_newer_larger", def: "overwrite", label: "Incoming is newer and larger" },
  { key: "transfers.conflict_smaller", def: "ask", label: "Incoming is smaller" },
  { key: "transfers.conflict_older", def: "ask", label: "Incoming is older" },
  { key: "transfers.conflict_identical", def: "skip", label: "Identical (same size and time)" },
  { key: "transfers.conflict_other", def: "ask", label: "Anything else" },
];

export default function SettingsDialog() {
  const open = useUiStore((s) => s.settingsOpen);
  const setOpen = useUiStore((s) => s.setSettingsOpen);
  const siteList = useUiStore((s) => s.sites);
  const setSites = useUiStore((s) => s.setSites);

  const [cfg, setCfg] = useState<Record<string, string>>({});
  const [tab, setTab] = useState<(typeof TABS)[number]["id"]>("general");
  const [data, setData] = useState<DataInfo | null>(null);
  const [backingUp, setBackingUp] = useState(false);
  const [draft, setDraft] = useState<SiteDraft | null>(null);
  const [siteMsg, setSiteMsg] = useState("");
  // One source for the running build: the Go side, which reads wails.json.
  const [version, setVersion] = useState("");
  const [updateMsg, setUpdateMsg] = useState("");
  const [checking, setChecking] = useState(false);
  const [repo, setRepo] = useState("");
  useEffect(() => {
    void updateRepo().then(setRepo).catch(() => undefined);
  }, []);

  useEffect(() => {
    if (!open) return;
    setSiteMsg("");
    void getSettings().then(setCfg).catch(() => setCfg({}));
    void appVersion().then(setVersion).catch(() => setVersion("unknown"));
    setUpdateMsg("");
    void fetchSites().then(setSites).catch(() => undefined);
    void dataLocation().then(setData).catch(() => setData(null));
  }, [open, setSites]);

  if (!open) return null;

  // The backend validates and rejects out-of-range values; surface that
  // rather than leaving the field showing something that was never saved.
  const put = (key: string, value: string) => {
    setCfg((c) => ({ ...c, [key]: value }));
    void setSetting(key, value).catch((err: unknown) => {
      setSiteMsg(String(err));
      void getSettings().then(setCfg).catch(() => undefined);
    });
  };

  // Legacy stored ids (v3 themes, "dark"/"light", "system") coerce to v4.
  const theme: ThemePref = coerceTheme(cfg["ui.theme"] ?? null);
  const bwMode = cfg["bw.mode"] || "off";
  const closeAction = cfg["ui.close_action"] || "ask";
  const startPaused = cfg["queue.start_paused"] === "1";

  // Hyperlane draws its lanes from the same connection budget the transfer
  // caps set, so a budget below the lane count silently narrows it. That
  // clamp used to be invisible and cost a tester a week of confused
  // testing; laneNote is how it says so.
  //
  // The cap mirrors dispatcher.streamsFor exactly, per-site override
  // included: a site with its own "Max transfers" ignores the default, so
  // warning off the default alone would both cry wolf and name a field
  // that changes nothing. bestSiteCap is the most permissive site, so the
  // note only fires when NO site can reach the configured lane count.
  const num = (key: string, def: number) => {
    const n = Number(cfg[key]);
    return Number.isFinite(n) && n > 0 ? n : def;
  };
  const globalMax = num("transfers.global_max", 6);
  const siteMax = num("transfers.site_max", 3);
  const siteCaps = siteList.map((s) => (s.maxTransfers > 0 ? s.maxTransfers : siteMax));
  const bestSiteCap = siteCaps.length > 0 ? Math.max(...siteCaps) : siteMax;
  const laneCap = Math.min(globalMax, bestSiteCap);
  // The inputs' own limits, so the note never suggests a value the backend
  // would reject (transfers.site_max validates 1-8, global_max 1-16).
  const MAX_SITE_CONN = 8;
  const MAX_GLOBAL_CONN = 16;
  const laneNote = (key: string, def: number) => {
    const lanes = num(key, def);
    if (lanes <= 1 || lanes <= laneCap) return null;
    // Both budgets can bind at once; naming only one sends the user round
    // the loop a second time.
    const binding: string[] = [];
    if (bestSiteCap < lanes) binding.push("Connections per site");
    if (globalMax < lanes) binding.push("Connections, all sites");
    const reachable = Math.min(lanes, MAX_SITE_CONN, MAX_GLOBAL_CONN);
    return (
      <p className="set-note set-note--warn">
        Only {laneCap} of these {lanes} lanes will be used — {binding.join(" and ")}{" "}
        {binding.length > 1 ? "are" : "is"} lower.{" "}
        {reachable > laneCap ? (
          <>
            Raise {binding.length > 1 ? "both" : "it"} to {reachable} to get all {reachable}
            {reachable < lanes
              ? ` — the most one file can use, since Connections per site tops out at ${MAX_SITE_CONN}`
              : ""}
            .
          </>
        ) : (
          <>
            Connections per site tops out at {MAX_SITE_CONN}, so {laneCap} lanes is the most one
            file can use — lower Lanes per file to {laneCap}.
          </>
        )}
        {siteCaps.length > 1 && new Set(siteCaps).size > 1
          ? " Sites with their own limit may get fewer."
          : ""}
      </p>
    );
  };
  const observedMax = Number(cfg["bw.observed_max"] || 0);

  const pickTheme = (t: ThemePref) => {
    put("ui.theme", t);
    applyTheme(t);
  };

  const saveDraft = async () => {
    if (!draft) return;
    if (!draft.host.trim()) {
      setSiteMsg("Enter a host to connect to.");
      return;
    }
    setSiteMsg("");
    try {
      await saveSite(
        {
          id: draft.id,
          name: draft.name.trim() || draft.host.trim(),
          ...formFields(draft),
          host: draft.host.trim(),
          port: Number(draft.port) || DEFAULT_PORT[draft.mode],
          username: draft.username.trim(),
          remotePath: draft.remotePath.trim(),
          maxTransfers: Number(draft.maxTransfers) || 0,
        },
        draft.password,
      );
      setSites(await fetchSites());
      setDraft(null);
      setSiteMsg("Saved.");
    } catch (err) {
      setSiteMsg(String(err));
    }
  };

  const removeSite = (s: Pick<Site, "id" | "name">) =>
    askDeleteSite(
      s,
      () => {
        setDraft(null);
        setSiteMsg("Site deleted.");
      },
      (err) => setSiteMsg(friendlyError(err)),
    );

  return (
    <div className="scrim scrim--center" onMouseDown={() => setOpen(false)}>
      <div
        className="dialog dialog--settings"
        data-tab={tab}
        onMouseDown={(e) => e.stopPropagation()}
        onKeyDown={(e) => {
          if (e.key === "Escape") {
            e.stopPropagation();
            setOpen(false);
          }
        }}
        role="dialog"
        aria-label="Settings"
      >
        <h2>Settings</h2>

        <div className="set-tabs" role="tablist">
          {TABS.map((t) => (
            <button
              key={t.id}
              role="tab"
              aria-selected={tab === t.id}
              className={tab === t.id ? "set-tab set-tab--on" : "set-tab"}
              onClick={() => setTab(t.id)}
            >
              {t.label}
            </button>
          ))}
        </div>

        <section className="set-section" data-tab="general">
          <h3>Appearance</h3>
          <div className="themes" role="radiogroup" aria-label="Theme">
            {THEMES.map((t) => (
              <button
                key={t.id}
                className={`theme-card ${theme === t.id ? "theme-card--on" : ""}`}
                role="radio"
                aria-checked={theme === t.id}
                onClick={() => pickTheme(t.id)}
              >
                <span className="theme-card__swatch" aria-hidden>
                  {t.swatch.map((c) => (
                    <span key={c} style={{ background: c }} />
                  ))}
                </span>
                <span className="theme-card__name">{t.name}</span>
                <span className="theme-card__blurb">{t.blurb}</span>
              </button>
            ))}
          </div>
          <p className="set-note">
            Each theme carries its own palette, type pairing and row density.
          </p>
        </section>

        <section className="set-section" data-tab="transfers">
          <h3>Transfers</h3>
          <p className="set-note set-blurb">
            These are connection budgets, not file counts. A Hyperlane file spends one
            connection per lane, so 8 connections runs two 4-lane files at once — and a
            budget below the lane count narrows Hyperlane instead of queueing. Files wait
            for their full lane count rather than starting on a spare connection.
          </p>
          <div className="set-row">
            <label>Connections, all sites</label>
            <input
              type="number"
              min={1}
              max={16}
              value={cfg["transfers.global_max"] ?? "6"}
              onChange={(e) => put("transfers.global_max", e.target.value)}
            />
          </div>
          <div className="set-row">
            <label>Connections per site</label>
            <input
              type="number"
              min={1}
              max={8}
              value={cfg["transfers.site_max"] ?? "3"}
              onChange={(e) => put("transfers.site_max", e.target.value)}
            />
          </div>
          <p className="set-note">
            Per-site is the default; a site can override it in its own settings. Keep it at
            or below what your server allows — refused connections show up in the log.
          </p>
        </section>

        <section className="set-section" data-tab="transfers">
          <h3>When the file already exists</h3>
          <p className="set-note set-blurb">
            Checked before the transfer starts, not after it. &ldquo;Ask&rdquo; holds the
            file in the queue with the sizes and dates side by side, so a folder full of
            clashes is one decision rather than a dialog per file. Applies to uploads and
            downloads alike &mdash; &ldquo;incoming&rdquo; is whichever file is being sent.
          </p>
          {CONFLICT_RULES.map((r) => (
            <div className="set-row" key={r.key}>
              <label>{r.label}</label>
              <select
                value={cfg[r.key] ?? r.def}
                onChange={(e) => put(r.key, e.target.value)}
              >
                <option value="ask">Ask me</option>
                <option value="overwrite">Overwrite</option>
                <option value="skip">Skip</option>
                <option value="rename">Keep both</option>
              </select>
            </div>
          ))}
          <p className="set-note">
            Keep both transfers to a free name beside the existing file &mdash;
            &ldquo;ep01.mkv&rdquo; becomes &ldquo;ep01 (1).mkv&rdquo;.
          </p>
        </section>

        <section className="set-section" data-tab="transfers">
          <h3>Hyperlane · Downloads</h3>
          <p className="set-note set-blurb">
            Splits one large file across several connections at once, so a server that
            caps the speed of each connection no longer caps the file. Each direction
            is tuned separately — a link is rarely as fast up as it is down.
          </p>
          <div className="set-row">
            <label>Lanes per file</label>
            <span className="set-inline">
              <input
                type="number"
                min={1}
                max={16}
                value={cfg["transfers.chunk_streams"] ?? "4"}
                onChange={(e) => put("transfers.chunk_streams", e.target.value)}
              />
              <span className="set-note">connections (1 = off)</span>
            </span>
          </div>
          {laneNote("transfers.chunk_streams", 4)}
          <div className="set-row">
            <label>Engage above</label>
            <span className="set-inline">
              <input
                type="number"
                min={0}
                value={cfg["transfers.chunk_min_mb"] ?? "256"}
                onChange={(e) => put("transfers.chunk_min_mb", e.target.value)}
              />
              <span className="set-note">MB</span>
            </span>
          </div>
        </section>

        <section className="set-section" data-tab="transfers">
          <h3>Hyperlane · Uploads</h3>
          <div className="set-row">
            <label>Lanes per file</label>
            <span className="set-inline">
              <input
                type="number"
                min={1}
                max={16}
                value={cfg["transfers.upload_chunk_streams"] ?? "3"}
                onChange={(e) => put("transfers.upload_chunk_streams", e.target.value)}
              />
              <span className="set-note">connections (1 = off)</span>
            </span>
          </div>
          {laneNote("transfers.upload_chunk_streams", 3)}
          <div className="set-row">
            <label>Engage above</label>
            <span className="set-inline">
              <input
                type="number"
                min={0}
                value={cfg["transfers.upload_chunk_min_mb"] ?? "128"}
                onChange={(e) => put("transfers.upload_chunk_min_mb", e.target.value)}
              />
              <span className="set-note">MB</span>
            </span>
          </div>
          <p className="set-note">
            Upload speed usually caps out around 3 lanes — more connections cost
            handshakes without adding throughput.
          </p>
        </section>

        <section className="set-section" data-tab="general">
          <h3>Closing</h3>
          <div
            className="segmented"
            role="radiogroup"
            aria-label="When closing with transfers running"
          >
            {[
              ["ask", "Ask"],
              ["quit", "Close"],
              ["pill", "Minimize to pill"],
            ].map(([v, label]) => (
              <button
                key={v}
                className={closeAction === v ? "seg--on" : ""}
                role="radio"
                aria-checked={closeAction === v}
                onClick={() => put("ui.close_action", v)}
              >
                {label}
              </button>
            ))}
          </div>
          <p className="set-note">
            Unfinished transfers are kept and pick up the next time you open
            warpseed (or wait, if the queue starts paused &mdash; see below).
            Choosing &ldquo;Close&rdquo; skips the confirmation; choosing
            &ldquo;Minimize to pill&rdquo; shrinks the window instead of closing
            it. With nothing transferring, warpseed closes straight away
            whichever you pick.
          </p>
        </section>

        <section className="set-section" data-tab="general">
          <h3>Queue on launch</h3>
          <div className="segmented" role="radiogroup" aria-label="Queue on launch">
            {[
              ["0", "Resume transfers"],
              ["1", "Start paused"],
            ].map(([v, label]) => (
              <button
                key={v}
                className={(startPaused ? "1" : "0") === v ? "seg--on" : ""}
                role="radio"
                aria-checked={(startPaused ? "1" : "0") === v}
                onClick={() => put("queue.start_paused", v)}
              >
                {label}
              </button>
            ))}
          </div>
          <p className="set-note">
            &ldquo;Start paused&rdquo; opens warpseed with the queue stopped:
            everything you queued is still there, but nothing moves until you
            press <strong>Resume queue</strong> in the dock. Pausing the queue
            from the dock is also remembered across a restart.
          </p>
        </section>

        <section className="set-section" data-tab="general">
          <h3>Notifications</h3>
          <label className="set-check">
            <input
              type="checkbox"
              checked={cfg["ui.notify"] !== "0"}
              onChange={(e) => {
                put("ui.notify", e.target.checked ? "1" : "0");
                setPref("ui.notify", e.target.checked ? "1" : "0");
              }}
            />
            Tell me when the queue finishes while warpseed is in the background
          </label>
        </section>

        <section className="set-section" data-tab="transfers">
          <h3>Bandwidth</h3>
          <div className="segmented" role="radiogroup" aria-label="Bandwidth limit mode">
            {[
              ["off", "Off"],
              ["fixed", "Fixed"],
              ["percent", "% of max"],
            ].map(([v, label]) => (
              <button
                key={v}
                className={bwMode === v ? "seg--on" : ""}
                role="radio"
                aria-checked={bwMode === v}
                onClick={() => put("bw.mode", v)}
              >
                {label}
              </button>
            ))}
          </div>
          {bwMode === "fixed" && (
            <div className="set-row">
              <label>Limit (MiB/s)</label>
              <input
                type="number"
                min={1}
                value={
                  Number(cfg["bw.limit_bytes"] || 0) > 0
                    ? Math.round(Number(cfg["bw.limit_bytes"]) / MIB)
                    : ""
                }
                placeholder="no limit"
                onChange={(e) => put("bw.limit_bytes", String(Number(e.target.value) * MIB))}
              />
            </div>
          )}
          {bwMode === "percent" && (
            <div className="set-row">
              <label>Throttle to</label>
              <span className="set-inline">
                <input
                  type="number"
                  min={10}
                  max={95}
                  value={cfg["bw.percent"] ?? "80"}
                  onChange={(e) => put("bw.percent", e.target.value)}
                />
                <span className="set-note">
                  % of measured max
                  {observedMax > 0 ? ` (${formatSize(observedMax)}/s so far)` : " (measuring…)"}
                </span>
              </span>
            </div>
          )}
          <label className="set-check">
            <input
              type="checkbox"
              checked={cfg["bw.sched_on"] === "1"}
              onChange={(e) => put("bw.sched_on", e.target.checked ? "1" : "0")}
            />
            Slow down during set hours
          </label>
          {cfg["bw.sched_on"] === "1" && (
            <div className="set-row">
              <span className="set-inline">
                <input
                  type="number"
                  min={0}
                  max={23}
                  value={cfg["bw.sched_from"] ?? "9"}
                  onChange={(e) => put("bw.sched_from", e.target.value)}
                />
                <span className="set-note">to</span>
                <input
                  type="number"
                  min={0}
                  max={23}
                  value={cfg["bw.sched_to"] ?? "17"}
                  onChange={(e) => put("bw.sched_to", e.target.value)}
                />
                <span className="set-note">o'clock, at most</span>
                <input
                  type="number"
                  min={1}
                  value={
                    Number(cfg["bw.sched_limit_bytes"] || 0) > 0
                      ? Math.round(Number(cfg["bw.sched_limit_bytes"]) / MIB)
                      : ""
                  }
                  placeholder="MiB/s"
                  onChange={(e) => put("bw.sched_limit_bytes", String(Math.max(1, Number(e.target.value)) * MIB))}
                />
                <span className="set-note">MiB/s</span>
              </span>
            </div>
          )}
        </section>

        <section className="set-section" data-tab="sites">
          <h3>Sites</h3>
          {!draft ? (
            <>
            <div className="site-list">
              {siteList.length === 0 && <span className="set-note">No saved sites yet.</span>}
              {siteList.map((s) => (
                <div key={s.id} className="site-row" onClick={() => setDraft(draftFrom(s))}>
                  <span className="name">{s.name}</span>
                  <span className="host">
                    {s.username}@{s.host}:{s.port}
                    {s.remotePath ? ` → ${s.remotePath}` : ""}
                  </span>
                  <span className="set-note site-row__go">edit <ChevronRight size={11} /></span>
                  <button
                    className="del"
                    title="Delete site"
                    aria-label={`Delete ${s.name}`}
                    onClick={(e) => {
                      e.stopPropagation();
                      removeSite(s);
                    }}
                  >
                    <Close size={13} />
                  </button>
                </div>
              ))}
            </div>
            <button className="btn" style={{ marginTop: "var(--sp-2)" }} onClick={() => setDraft(blankDraft())}>
              Add site
            </button>
            </>
          ) : (
            <>
              <div className="form-grid">
                <ProtocolField form={draft} port={Number(draft.port)} onChange={(next, port) => setDraft({ ...draft, ...next, port })} />
                <div className="field">
                  <label>Site name</label>
                  <input value={draft.name} onChange={(e) => setDraft({ ...draft, name: e.target.value })} />
                </div>
                <div className="field">
                  <label>Host</label>
                  <input value={draft.host} onChange={(e) => setDraft({ ...draft, host: e.target.value })} />
                </div>
                <div className="field">
                  <label>Port</label>
                  <input
                    inputMode="numeric"
                    value={draft.port}
                    onChange={(e) => setDraft({ ...draft, port: Number(e.target.value) || 0 })}
                  />
                </div>
                <div className="field">
                  <label>Username</label>
                  <input
                    value={draft.username}
                    onChange={(e) => setDraft({ ...draft, username: e.target.value })}
                  />
                </div>
                {secretLabel(draft) && (
                <div className="field">
                  <label>{secretLabel(draft)}</label>
                  <input
                    type="password"
                    placeholder="(unchanged)"
                    value={draft.password}
                    onChange={(e) => setDraft({ ...draft, password: e.target.value })}
                  />
                </div>
                )}
                <div className="field">
                  <label>Initial remote path</label>
                  <input
                    placeholder="(SFTP home)"
                    value={draft.remotePath}
                    onChange={(e) => setDraft({ ...draft, remotePath: e.target.value })}
                  />
                </div>
                <div className="field">
                  <label>Max transfers (0 = default)</label>
                  <input
                    type="number"
                    min={0}
                    max={8}
                    value={draft.maxTransfers}
                    onChange={(e) => setDraft({ ...draft, maxTransfers: Number(e.target.value) || 0 })}
                  />
                </div>
                <AutoConnectField checked={draft.autoConnect} onChange={(autoConnect) => setDraft({ ...draft, autoConnect })} />
              </div>
              <div className="dialog__actions">
                {draft.id !== 0 && (
                  <button className="btn btn--danger" onClick={() => removeSite(draft)}>
                    Delete site
                  </button>
                )}
                <span style={{ flex: 1 }} />
                <button className="btn" onClick={() => setDraft(null)}>
                  Back
                </button>
                <button className="btn btn--primary" onClick={() => void saveDraft()}>
                  Save site
                </button>
              </div>
            </>
          )}
          {siteMsg && <div className="set-note set-msg">{siteMsg}</div>}
        </section>

        <section className="set-section" data-tab="about">
          <h3>Data</h3>
          <p className="set-note set-blurb">
            Sites, bookmarks, the transfer queue, pinned host keys and every setting
            here live in one file. Passwords are the exception — those stay in Windows
            Credential Manager and are not part of a backup.
          </p>
          <div className="set-row">
            <label>Settings file</label>
            <span className="set-inline">
              <code className="set-path" title={data?.path}>
                {data?.path ?? "…"}
              </code>
            </span>
          </div>
          <div className="dialog__actions" style={{ marginTop: "var(--sp-2)" }}>
            <button className="btn" onClick={() => void openDataFolder().catch(() => undefined)}>
              Open folder
            </button>
            <button
              className="btn"
              onClick={() => {
                setOpen(false);
                useUiStore.getState().setHistoryOpen(true);
              }}
            >
              Transfer history
            </button>
            <span style={{ flex: 1 }} />
            <button
              className="btn btn--primary"
              disabled={backingUp}
              onClick={() => {
                setBackingUp(true);
                void backupData()
                  .then((name) => setSiteMsg(`Backed up to ${name}`))
                  .catch((err: unknown) => setSiteMsg(String(err)))
                  .finally(() => {
                    setBackingUp(false);
                    void dataLocation().then(setData).catch(() => undefined);
                  });
              }}
            >
              {backingUp ? "Backing up…" : "Back up now"}
            </button>
          </div>
          {data && data.backups.length > 0 && (
            <p className="set-note" style={{ marginTop: "var(--sp-2)" }}>
              {data.backups.length} backup{data.backups.length === 1 ? "" : "s"} · newest{" "}
              {data.backups[0]}. To restore one: close warpseed, delete
              warpseed.db along with any warpseed.db-wal and warpseed.db-shm beside it,
              then rename the backup to warpseed.db. Leaving the -wal file behind
              replays old changes over the restored copy.
            </p>
          )}
        </section>

        <section className="set-section" data-tab="about">
          <h3>About</h3>
          <p className="set-note set-blurb">
            warpseed {version} — a free, fast seedbox transfer client by {COMPANY}.
          </p>
          <div className="update-card">
            <Switch
              checked={(cfg["updates.check"] ?? "1") === "1"}
              onChange={(on) => put("updates.check", on ? "1" : "0")}
            >
              Check for updates when warpseed starts
            </Switch>
            <div className="update-card__row">
              <span className="set-note">Look for releases from</span>
              <div className="segmented" role="radiogroup" aria-label="Where to check for updates">
                {[
                  ["fork", "This fork"],
                  ["upstream", "Original"],
                ].map(([v, label]) => (
                  <button
                    key={v}
                    className={(cfg["updates.source"] ?? "fork") === v ? "seg--on" : ""}
                    role="radio"
                    aria-checked={(cfg["updates.source"] ?? "fork") === v}
                    onClick={() => {
                      put("updates.source", v);
                      setUpdateMsg("");
                      setTimeout(() => void updateRepo().then(setRepo), 150);
                    }}
                  >
                    {label}
                  </button>
                ))}
              </div>
            </div>
            <div className="update-card__row">
              <button
                className="btn"
                disabled={checking}
                onClick={() => {
                  setChecking(true);
                  setUpdateMsg("");
                  void checkForUpdate()
                    .then((u) => {
                      // The manual check reports the up-to-date case too; the
                      // automatic one stays silent about it.
                      setUpdateMsg(
                        u.available
                          ? `warpseed ${u.latest} is available.`
                          : `You are on the latest version (${u.current}).`,
                      );
                    })
                    .catch((err: unknown) => setUpdateMsg(friendlyError(err)))
                    .finally(() => setChecking(false));
                }}
              >
                {checking ? "Checking…" : "Check now"}
              </button>
              <span className="set-note">{updateMsg || (repo ? `Checks github.com/${repo}` : "")}</span>
            </div>
          </div>
          <p className="set-note">
            One check per launch. It sends no identifiers and no usage data: the
            request only asks a public page what the latest version is. warpseed
            never downloads or replaces itself; the banner opens the release
            page and you choose. It is free and always will be; if it saves you
            time, a coffee keeps the updates coming.
          </p>
          <div className="dialog__actions" style={{ marginTop: "var(--sp-2)" }}>
            <button className="btn" onClick={() => openExternal(WEBSITE_URL)}>
              zyralabs.tech
            </button>
            <button className="btn" onClick={() => openExternal(bugReportUrl(version))}>
              <Bug size={12} className="btn__ico" /> Report a bug
            </button>
            <button className="btn" onClick={() => void logDir()} title="warpseed.log — attach it to a bug report">
              Open log folder
            </button>
            <label className="set-check" title="Per-lane offsets, first-write timing and cancel latency in warpseed.log. Turn on when reporting a stall, then send the log.">
              <input
                type="checkbox"
                checked={cfg["log.verbose"] === "1"}
                onChange={(e) => put("log.verbose", e.target.checked ? "1" : "0")}
              />
              Verbose log
            </label>
            <span style={{ flex: 1 }} />
            <button className="btn btn--primary" onClick={() => openExternal(DONATE_URL)}>
              <Heart size={12} className="btn__ico" /> Support warpseed
            </button>
          </div>
        </section>

        <div className="dialog__actions">
          <button className="btn btn--primary" onClick={() => setOpen(false)}>
            Done
          </button>
        </div>
      </div>
    </div>
  );
}
