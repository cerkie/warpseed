import { pickFile } from "../ipc";
import { DEFAULT_PORT, type Auth, type SiteForm, type SiteMode } from "../lib/protocol";

const HINTS: Record<SiteMode, string> = {
  sftp: "SSH file transfer. Most seedboxes want this.",
  ftps: "FTP upgraded with TLS on the usual port. Not the same as SFTP.",
  "ftps-implicit": "FTP over TLS from the first byte, usually port 990.",
  ftp: "Plain FTP. Your password and files are sent unencrypted, so only use it on a network you trust.",
};

/** Protocol and (for SFTP) login-method fields. Moving to another protocol
    follows its default port, but never overwrites one the user typed. */
export default function ProtocolField({
  form,
  port,
  onChange,
}: {
  form: SiteForm;
  port: number;
  onChange: (form: SiteForm, port: number) => void;
}) {
  const setMode = (mode: SiteMode) =>
    onChange({ ...form, mode }, port === DEFAULT_PORT[form.mode] ? DEFAULT_PORT[mode] : port);
  return (
    <>
      <div className="field wide">
        <label>Protocol</label>
        <select value={form.mode} onChange={(e) => setMode(e.target.value as SiteMode)}>
          <option value="sftp">SFTP (SSH)</option>
          <option value="ftps">FTPS (explicit TLS)</option>
          <option value="ftps-implicit">FTPS (implicit TLS)</option>
          <option value="ftp">FTP (no encryption)</option>
        </select>
        {/* Two lines reserved, so switching protocol never resizes the dialog. */}
        <span className="note note--two">{HINTS[form.mode]}</span>
      </div>
      {form.mode !== "sftp" ? (
        <div className="field wide">
          <label>Log in with</label>
          <select value="password" disabled aria-label="Log in with">
            <option value="password">Username and password</option>
          </select>
        </div>
      ) : (
        <div className="field wide">
          <label>Log in with</label>
          <select
            value={form.auth}
            onChange={(e) => onChange({ ...form, auth: e.target.value as Auth }, port)}
          >
            <option value="password">Password</option>
            <option value="key">Private key file</option>
            <option value="agent">SSH agent</option>
          </select>
          {form.auth === "key" && (
            <div className="field-row">
              <input
                value={form.keyPath}
                placeholder="C:\Users\you\.ssh\id_ed25519"
                onChange={(e) => onChange({ ...form, keyPath: e.target.value }, port)}
              />
              <button
                className="btn"
                type="button"
                onClick={() =>
                  void pickFile("Choose your private key").then((p) => {
                    if (p) onChange({ ...form, keyPath: p }, port);
                  })
                }
              >
                Browse…
              </button>
            </div>
          )}
        </div>
      )}
    </>
  );
}
