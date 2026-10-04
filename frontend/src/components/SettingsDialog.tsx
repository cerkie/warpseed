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
import { COMPANY, DONATE_URL, REPO_URL, WEBSITE_URL, bugReportUrl } from "../lib/branding";
import { formatSize } from "../lib/format";
import { applyTheme, coerceTheme, THEMES, type ThemePref } from "../lib/theme";
import { DEFAULT_FORM, DEFAULT_PORT, formFields, formOf, secretLabel, type SiteForm } from "../lib/protocol";
import ProtocolField from "./ProtocolField";
import { askDeleteSite } from "../lib/sites";
import ImportHint from "./ImportHint";
import Info from "./Info";
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
  speedMiB: number; // 0 = no limit of its own
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
    speedMiB: s.bandwidthLimit ? Math.round((s.bandwidthLimit / MIB) * 10) / 10 : 0,
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
  speedMiB: 0,
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

/** One setting: its name (and a short hint, or an "i" for more) on the left and
    its control on the right. Every control sits in the same right-hand column. */
function Row({
  label,
  hint,
  info,
  children,
}: {
  label: string;
  hint?: string;
  info?: React.ReactNode;
  children: React.ReactNode;
}) {
  return (
    <div className="set-row">
      <div className="set-row__text">
        <span className="set-row__label">
          {label}
          {info && <Info>{info}</Info>}
        </span>
        {hint && <span className="set-row__hint">{hint}</span>}
      </div>
      <div className="set-row__control">{children}</div>
    </div>
  );
}

/** A number box with its unit inside, so every box is the same width and the
    units line up down the page. */
function NumField({
  label,
  value,
  onChange,
  unit,
  min,
  max,
  placeholder,
}: {
  label: string;
  value: string | number;
  onChange: (v: string) => void;
  unit?: string;
  min?: number;
  max?: number;
  placeholder?: string;
}) {
  return (
    <span className="numfield">
      <input
        type="number"
        aria-label={label}
        min={min}
        max={max}
        value={value}
        placeholder={placeholder}
        onChange={(e) => onChange(e.target.value)}
        style={unit ? { paddingRight: `${16 + unit.length * 7}px` } : undefined}
      />
      {unit && <span className="numfield__unit">{unit}</span>}
    </span>
  );
}

/** A short list of choices where exactly one is picked. */
function Seg({
  label,
  value,
  onChange,
  options,
}: {
  label: string;
  value: string;
  onChange: (v: string) => void;
  options: [string, string][];
}) {
  return (
    <div className="segmented segmented--row" role="radiogroup" aria-label={label}>
      {options.map(([v, text]) => (
        <button key={v} className={value === v ? "seg--on" : ""} role="radio" aria-checked={value === v} onClick={() => onChange(v)}>
          {text}
        </button>
      ))}
    </div>
  );
}

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
  const update = useUiStore((s) => s.update);
  const setUpdate = useUiStore((s) => s.setUpdate);
  const setUpdateOpen = useUiStore((s) => s.setUpdateOpen);
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
  const speedMode = cfg["ui.speed_mode"] || "live";
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
          bandwidthLimit: Math.max(0, Math.round((Number(draft.speedMiB) || 0) * MIB)),
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
        </section>

        <section className="set-section" data-tab="general">
          <h3>When closing and starting</h3>
          <div className="set-card">
            <Row
              label="Closing with transfers running"
              info="Unfinished transfers are kept and carry on next time you open warpseed. “Close” skips the confirmation. “Minimize to pill” shrinks the window instead of closing it. With nothing transferring, warpseed always closes straight away."
            >
              <Seg
                label="When closing with transfers running"
                value={closeAction}
                onChange={(v) => put("ui.close_action", v)}
                options={[
                  ["ask", "Ask"],
                  ["quit", "Close"],
                  ["pill", "Minimize to pill"],
                ]}
              />
            </Row>
            <Row
              label="Queue when warpseed opens"
              info="“Start paused” opens warpseed with the queue stopped. Nothing moves until you press Resume queue. Pausing from the queue is remembered too."
            >
              <Seg
                label="Queue on launch"
                value={startPaused ? "1" : "0"}
                onChange={(v) => put("queue.start_paused", v)}
                options={[
                  ["0", "Resume"],
                  ["1", "Start paused"],
                ]}
              />
            </Row>
          </div>
        </section>

        <section className="set-section" data-tab="general">
          <h3>Schedule and alerts</h3>
          <div className="set-card">
            <Row
              label="Only transfer at set hours"
              info="Outside those hours nothing new starts, and the queue says it is waiting. Transfers already running finish. Nothing is paused or cancelled, so leave warpseed open and it carries on when the hours begin."
            >
              <Switch
                label="Only transfer at set hours"
                checked={cfg["queue.window_on"] === "1"}
                onChange={(on) => put("queue.window_on", on ? "1" : "0")}
              />
            </Row>
            {cfg["queue.window_on"] === "1" && (
              <>
                <Row label="Start at">
                  <NumField label="Start hour" unit=":00" min={0} max={23} value={cfg["queue.window_from"] ?? "1"} onChange={(v) => put("queue.window_from", v)} />
                </Row>
                <Row label="Stop at">
                  <NumField label="Stop hour" unit=":00" min={0} max={23} value={cfg["queue.window_to"] ?? "7"} onChange={(v) => put("queue.window_to", v)} />
                </Row>
              </>
            )}
            <Row label="Tell me when the queue finishes" hint="Only while warpseed is in the background">
              <Switch
                label="Tell me when the queue finishes"
                checked={cfg["ui.notify"] !== "0"}
                onChange={(on) => {
                  put("ui.notify", on ? "1" : "0");
                  setPref("ui.notify", on ? "1" : "0");
                }}
              />
            </Row>
          </div>
        </section>

        <section className="set-section" data-tab="transfers">
          <h3>Connections</h3>
          <div className="set-card">
            <Row
              label="All sites together"
              info="How many connections warpseed opens at once. A Hyperlane file uses one connection per lane. If the limit is lower than the lane count, the file uses fewer lanes."
            >
              <NumField label="Connections, all sites" min={1} max={16} value={cfg["transfers.global_max"] ?? "6"} onChange={(v) => put("transfers.global_max", v)} />
            </Row>
            <Row
              label="Per site"
              info="The most connections open at once to one site. A site can set its own number in its settings. Stay at or below what your server allows; refused connections show up in the log."
            >
              <NumField label="Connections per site" min={1} max={8} value={cfg["transfers.site_max"] ?? "3"} onChange={(v) => put("transfers.site_max", v)} />
            </Row>
          </div>
        </section>

        <section className="set-section" data-tab="transfers">
          <h3>
            When the file already exists
            <Info>
              What to do when a file is already at the destination. &ldquo;Ask&rdquo; holds it in the
              queue so you can compare both versions. &ldquo;Incoming&rdquo; is the file being sent, in
              either direction. &ldquo;Keep both&rdquo; saves the new one under a free name, like
              &ldquo;ep01 (1).mkv&rdquo;.
            </Info>
          </h3>
          <div className="set-card">
            {CONFLICT_RULES.map((r) => (
              <Row key={r.key} label={r.label}>
                <select aria-label={r.label} value={cfg[r.key] ?? r.def} onChange={(e) => put(r.key, e.target.value)}>
                  <option value="ask">Ask me</option>
                  <option value="overwrite">Overwrite</option>
                  <option value="skip">Skip</option>
                  <option value="rename">Keep both</option>
                </select>
              </Row>
            ))}
          </div>
        </section>

        <section className="set-section" data-tab="transfers">
          <h3>Checks and warnings</h3>
          <div className="set-card">
            <Row
              label="Check files after transfer"
              hint="Slower, but catches damaged files"
              info="Compares each finished file with the server's copy, so transfers finish a bit later. It works with SFTP servers that have sha256sum; other sites are skipped. A download that doesn't match is deleted and marked failed so you can retry it."
            >
              <Switch
                label="Check files after transfer"
                checked={cfg["transfers.verify"] === "1"}
                onChange={(on) => put("transfers.verify", on ? "1" : "0")}
              />
            </Row>
            <Row label="Warn when the disk is short" hint="Before a download that won't fit">
              <Switch
                label="Warn when the disk is short"
                checked={cfg["transfers.space_warning"] !== "0"}
                onChange={(on) => put("transfers.space_warning", on ? "1" : "0")}
              />
            </Row>
            <Row
              label="Remember each site's folder and sort"
              info="Opens each site in the folder you last used there. The sort order is shared by both panes, so it follows the site you open."
            >
              <Switch
                label="Remember each site's folder and sort"
                checked={cfg["ui.remember_site_views"] === "1"}
                onChange={(on) => {
                  put("ui.remember_site_views", on ? "1" : "0");
                  setPref("ui.remember_site_views", on ? "1" : "0");
                }}
              />
            </Row>
          </div>
        </section>

        <section className="set-section" data-tab="transfers">
          <h3>
            Hyperlane
            <Info>
              Splits a big file across several connections, so a server that limits each connection
              can&rsquo;t slow the whole file. Downloads and uploads are set separately. Upload speed
              usually stops improving after about 3 lanes.
            </Info>
          </h3>
          <div className="set-card">
            <p className="set-card__sub">Downloads</p>
            <Row label="Lanes per file" hint="1 turns Hyperlane off">
              <NumField label="Download lanes per file" unit="lanes" min={1} max={16} value={cfg["transfers.chunk_streams"] ?? "4"} onChange={(v) => put("transfers.chunk_streams", v)} />
            </Row>
            {laneNote("transfers.chunk_streams", 4)}
            <Row label="Only for files over" hint="Smaller files use one connection">
              <NumField label="Download Hyperlane threshold" unit="MB" min={0} value={cfg["transfers.chunk_min_mb"] ?? "256"} onChange={(v) => put("transfers.chunk_min_mb", v)} />
            </Row>
            <p className="set-card__sub">Uploads</p>
            <Row label="Lanes per file" hint="1 turns Hyperlane off">
              <NumField label="Upload lanes per file" unit="lanes" min={1} max={16} value={cfg["transfers.upload_chunk_streams"] ?? "3"} onChange={(v) => put("transfers.upload_chunk_streams", v)} />
            </Row>
            {laneNote("transfers.upload_chunk_streams", 3)}
            <Row label="Only for files over" hint="Smaller files use one connection">
              <NumField label="Upload Hyperlane threshold" unit="MB" min={0} value={cfg["transfers.upload_chunk_min_mb"] ?? "128"} onChange={(v) => put("transfers.upload_chunk_min_mb", v)} />
            </Row>
          </div>
        </section>

        <section className="set-section" data-tab="transfers">
          <h3>Bandwidth</h3>
          <div className="set-card">
            <Row label="Speed limit" hint="For all transfers together">
              <Seg
                label="Bandwidth limit mode"
                value={bwMode}
                onChange={(v) => put("bw.mode", v)}
                options={[
                  ["off", "Off"],
                  ["fixed", "Fixed"],
                  ["percent", "% of max"],
                ]}
              />
            </Row>
            {bwMode === "fixed" && (
              <Row label="Limit">
                <NumField
                  label="Limit"
                  unit="MiB/s"
                  min={1}
                  placeholder="none"
                  value={Number(cfg["bw.limit_bytes"] || 0) > 0 ? Math.round(Number(cfg["bw.limit_bytes"]) / MIB) : ""}
                  onChange={(v) => put("bw.limit_bytes", String(Number(v) * MIB))}
                />
              </Row>
            )}
            {bwMode === "percent" && (
              <Row
                label="Throttle to"
                hint={`% of the fastest speed seen${observedMax > 0 ? ` (${formatSize(observedMax)}/s so far)` : " (measuring…)"}`}
              >
                <NumField label="Throttle percent" unit="%" min={10} max={95} value={cfg["bw.percent"] ?? "80"} onChange={(v) => put("bw.percent", v)} />
              </Row>
            )}
            <Row label="Slow down at set hours" info="The schedule can only lower a limit you already set, never raise it.">
              <Switch
                label="Slow down at set hours"
                checked={cfg["bw.sched_on"] === "1"}
                onChange={(on) => put("bw.sched_on", on ? "1" : "0")}
              />
            </Row>
            {cfg["bw.sched_on"] === "1" && (
              <>
                <Row label="From">
                  <NumField label="From hour" unit=":00" min={0} max={23} value={cfg["bw.sched_from"] ?? "9"} onChange={(v) => put("bw.sched_from", v)} />
                </Row>
                <Row label="To">
                  <NumField label="To hour" unit=":00" min={0} max={23} value={cfg["bw.sched_to"] ?? "17"} onChange={(v) => put("bw.sched_to", v)} />
                </Row>
                <Row label="Limit during those hours">
                  <NumField
                    label="Limit during those hours"
                    unit="MiB/s"
                    min={1}
                    placeholder="none"
                    value={Number(cfg["bw.sched_limit_bytes"] || 0) > 0 ? Math.round(Number(cfg["bw.sched_limit_bytes"]) / MIB) : ""}
                    onChange={(v) => put("bw.sched_limit_bytes", String(Math.max(1, Number(v)) * MIB))}
                  />
                </Row>
              </>
            )}
            <Row
              label="Speed readout"
              info="Live follows the current speed. Average smooths it over each transfer, so the number stops jumping around."
            >
              <Seg
                label="How speeds are shown"
                value={speedMode}
                onChange={(v) => {
                  put("ui.speed_mode", v);
                  setPref("ui.speed_mode", v);
                }}
                options={[
                  ["live", "Live"],
                  ["average", "Average"],
                ]}
              />
            </Row>
          </div>
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
            <ImportHint onMessage={(m) => setSiteMsg(m)} />
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
                <div className="field">
                  <label title="Caps this site's combined speed. The overall speed limit still applies; the lower of the two wins.">
                    Speed limit, MiB/s (0 = none)
                  </label>
                  <input
                    type="number"
                    min={0}
                    step={0.5}
                    value={draft.speedMiB}
                    onChange={(e) => setDraft({ ...draft, speedMiB: Math.max(0, Number(e.target.value) || 0) })}
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
          <h3>
            Data
            <Info>
              Sites, bookmarks, the queue, saved host keys and your settings live in one file.
              Passwords are kept in Windows Credential Manager instead and are not part of a backup.
            </Info>
          </h3>
          <div className="set-card">
            <Row label="Settings file" hint={data?.path ?? "…"}>
              <button className="btn" onClick={() => void openDataFolder().catch(() => undefined)}>
                Open folder
              </button>
            </Row>
            <Row
              label="Backups"
              hint={
                data && data.backups.length > 0
                  ? `${data.backups.length} saved · newest ${data.backups[0]}`
                  : "None yet"
              }
              info={
                <>
                  To restore one: close warpseed, delete <code>warpseed.db</code> and any{" "}
                  <code>warpseed.db-wal</code> and <code>warpseed.db-shm</code> next to it, then rename the
                  backup to <code>warpseed.db</code>. Leave those two extra files behind and old changes get
                  replayed over the restored copy.
                </>
              }
            >
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
            </Row>
            <Row label="Transfer history" hint="Finished transfers you have cleared from the queue">
              <button
                className="btn"
                onClick={() => {
                  setOpen(false);
                  useUiStore.getState().setHistoryOpen(true);
                }}
              >
                View
              </button>
            </Row>
          </div>
        </section>

        <section className="set-section" data-tab="about">
          <h3>About</h3>
          <p className="set-note set-blurb">
            warpseed {version} — a free, fast file transfer client for Windows.
          </p>
          <div className="about-fork">
            <p>
              <strong>This is a fork.</strong> warpseed was created by {COMPANY}, who built the transfer engine and had
              the original idea. This fork adds FTP and FTPS, a third pane, drag and drop with Explorer, in-app
              updates and more, on top of their work.
            </p>
            <p>
              <strong>Donations go to {COMPANY}</strong>, not to the fork, because they did the hard part.
            </p>
            <p>
              <strong>Bug reports sent from here go to this fork</strong>, not to them, so the fork&rsquo;s issue
              page is where they land.
            </p>
            <div className="about-fork__links">
              <button className="btn" onClick={() => openExternal("https://github.com/ZyraLabs/warpseed")}>
                Original project
              </button>
              <button className="btn" onClick={() => openExternal(REPO_URL)}>
                This fork
              </button>
            </div>
          </div>
        </section>

        <section className="set-section" data-tab="about">
          <h3>Updates</h3>
          <div className="set-card">
            <Row
              label="Check for updates at launch"
              hint="Free, and always will be"
              info={`Checking sends no identifiers or usage data; it only asks a public page for the latest version. Nothing downloads until you press Install, and the download is checked against GitHub's checksum before anything is replaced. If warpseed saves you time, a coffee for ${COMPANY} is the best thank-you.`}
            >
              <Switch
                label="Check for updates at launch"
                checked={(cfg["updates.check"] ?? "1") === "1"}
                onChange={(on) => put("updates.check", on ? "1" : "0")}
              />
            </Row>
            <Row label="Check now" hint={updateMsg || (repo ? `Looks at github.com/${repo}` : "")}>
              {update?.available && (
                <button
                  className="btn btn--primary"
                  onClick={() => {
                    setOpen(false);
                    setUpdateOpen(true);
                  }}
                >
                  What&rsquo;s new &amp; install
                </button>
              )}
              <button
                className="btn"
                disabled={checking}
                onClick={() => {
                  setChecking(true);
                  setUpdateMsg("");
                  void checkForUpdate()
                    .then((u) => {
                      if (u.available) setUpdate(u);
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
            </Row>
          </div>
        </section>

        <section className="set-section" data-tab="about">
          <h3>Help and support</h3>
          <div className="set-card">
            <Row label="Report a bug" hint="Opens an issue on this fork's GitHub page">
              <button className="btn" onClick={() => openExternal(bugReportUrl(version))}>
                <Bug size={12} className="btn__ico" /> Report a bug
              </button>
            </Row>
            <Row label="Log file" hint="Attach warpseed.log to a bug report">
              <button className="btn" onClick={() => void logDir()}>
                Open log folder
              </button>
            </Row>
            <Row
              label="Verbose log"
              hint="Extra detail in the log"
              info="Records per-lane offsets, first-write timing and cancel latency in warpseed.log. Turn it on when reporting a stall, then send the log."
            >
              <Switch
                label="Verbose log"
                checked={cfg["log.verbose"] === "1"}
                onChange={(on) => put("log.verbose", on ? "1" : "0")}
              />
            </Row>
            <Row label={COMPANY} hint="Made warpseed, and where donations go">
              <button className="btn" onClick={() => openExternal(WEBSITE_URL)}>
                Website
              </button>
              <button className="btn btn--primary" onClick={() => openExternal(DONATE_URL)}>
                <Heart size={12} className="btn__ico" /> Support
              </button>
            </Row>
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
