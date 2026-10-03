import { useEffect, useState } from "react";
import {
  connectAndHome,
  saveSite,
  sites as fetchSites,
  type Site,
} from "../ipc";
import { friendlyError } from "../lib/errors";
import { askDeleteSite } from "../lib/sites";
import { toast } from "../lib/toast";
import ImportHint from "./ImportHint";
import { useUiStore } from "../store";
import { Close, Pencil } from "./Icon";
import ProtocolField from "./ProtocolField";
import AutoConnectField from "./AutoConnectField";
import { DEFAULT_FORM, DEFAULT_PORT, formFields, formOf, secretLabel } from "../lib/protocol";

const EMPTY = { name: "", host: "", port: 22, username: "", password: "", ...DEFAULT_FORM };

/** Quick-connect: saved sites + a new-site form (ux-spec §5.1, popover-not-
    wizard). Password goes to Windows Credential Manager, never the DB. */
export default function QuickConnect() {
  const { open, side } = useUiStore((s) => s.quickConnect);
  const setQuickConnect = useUiStore((s) => s.setQuickConnect);
  const setPane = useUiStore((s) => s.setPane);
  const siteList = useUiStore((s) => s.sites);
  const setSites = useUiStore((s) => s.setSites);

  const [form, setForm] = useState(EMPTY);
  const [editing, setEditing] = useState<Site | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    if (open) {
      setForm(EMPTY);
      setEditing(null);
      setError("");
      void fetchSites().then(setSites).catch(() => undefined);
    }
  }, [open, setSites]);

  if (!open) return null;

  const close = () => setQuickConnect(false);

  const connectExisting = async (s: Site) => {
    setBusy(true);
    setError("");
    try {
      const home = await connectAndHome(s.id, s.remotePath);
      setPane(side, s.id, home);
      close();
    } catch (err) {
      setError(friendlyError(err));
    } finally {
      setBusy(false);
    }
  };

  const startEdit = (e: React.MouseEvent, s: Site) => {
    e.stopPropagation();
    setEditing(s);
    setForm({ name: s.name, host: s.host, port: s.port, username: s.username, password: "", ...formOf(s) });
    setError("");
  };

  const saveAndConnect = async () => {
    setBusy(true);
    setError("");
    try {
      const saved = await saveSite(
        {
          id: editing?.id,
          remotePath: editing?.remotePath,
          maxTransfers: editing?.maxTransfers,
          name: form.name.trim() || form.host.trim(),
          ...formFields(form),
          host: form.host.trim(),
          port: Number(form.port) || DEFAULT_PORT[form.mode],
          username: form.username.trim(),
        },
        form.password,
      );
      setSites(await fetchSites());
      const home = await connectAndHome(saved.id);
      setPane(side, saved.id, home);
      close();
    } catch (err) {
      setError(friendlyError(err));
    } finally {
      setBusy(false);
    }
  };

  const removeSite = (e: React.MouseEvent, s: Site) => {
    e.stopPropagation();
    askDeleteSite(s, () => undefined, (err) => setError(friendlyError(err)));
  };

  return (
    <div className="scrim scrim--center" onMouseDown={close}>
      <div className="dialog" onMouseDown={(e) => e.stopPropagation()} role="dialog" aria-label="Connect">
        <h2>Connect</h2>

        {siteList.length > 0 && (
          <div className="site-list">
            {siteList.map((s) => (
              <div key={s.id} className="site-row" onClick={() => void connectExisting(s)}>
                <span className="name">{s.name}</span>
                <span className="host">
                  {s.protocol}://{s.username}@{s.host}:{s.port}
                </span>
                <button className="edit" title="Edit site" aria-label={`Edit ${s.name}`} onClick={(e) => startEdit(e, s)}>
                  <Pencil size={13} />
                </button>
                <button className="del" title="Delete site" aria-label={`Delete ${s.name}`} onClick={(e) => void removeSite(e, s)}>
                  <Close size={13} />
                </button>
              </div>
            ))}
          </div>
        )}

        <div className="form-grid">
          <ProtocolField form={form} port={Number(form.port)} onChange={(next, port) => setForm({ ...form, ...next, port })} />
          <div className="field wide">
            <label>Host</label>
            <input
              autoFocus
              value={form.host}
              placeholder="seedbox.example.com"
              onChange={(e) => setForm({ ...form, host: e.target.value })}
            />
          </div>
          <div className="field">
            <label>Port</label>
            <input
              value={form.port}
              inputMode="numeric"
              onChange={(e) => setForm({ ...form, port: Number(e.target.value) || 0 })}
            />
          </div>
          <div className="field">
            <label>Site name</label>
            <input
              value={form.name}
              placeholder="(defaults to host)"
              onChange={(e) => setForm({ ...form, name: e.target.value })}
            />
          </div>
          <div className="field">
            <label>Username</label>
            <input
              value={form.username}
              onChange={(e) => setForm({ ...form, username: e.target.value })}
            />
          </div>
          {secretLabel(form) && (
          <div className="field">
            <label>{secretLabel(form)}</label>
            <input
              type="password"
              value={form.password}
              onChange={(e) => setForm({ ...form, password: e.target.value })}
              onKeyDown={(e) => e.key === "Enter" && void saveAndConnect()}
            />
            <span className="note">{editing ? "leave blank to keep the saved password" : "stored in Windows Credential Manager, never on disk"}</span>
          </div>
          )}
          <AutoConnectField checked={form.autoConnect} onChange={(autoConnect) => setForm({ ...form, autoConnect })} />
        </div>

        {error && <div className="form-error">{error}</div>}
        <ImportHint onMessage={(m, bad) => toast(bad ? "error" : "success", m)} />

        <div className="dialog__actions">
          <button className="btn" onClick={close}>
            Cancel
          </button>
          <button
            className="btn btn--primary"
            disabled={busy || !form.host || !form.username}
            onClick={() => void saveAndConnect()}
          >
            {busy ? "Connecting…" : editing ? "Save changes & connect" : "Save & connect"}
          </button>
        </div>
      </div>
    </div>
  );
}
