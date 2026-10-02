import type { Site } from "../ipc";

/** What the connect and settings forms offer; stored as protocol + options. */
export type SiteMode = "sftp" | "ftps" | "ftps-implicit" | "ftp";
export type Auth = "password" | "key" | "agent";

export const DEFAULT_PORT: Record<SiteMode, number> = { sftp: 22, ftps: 21, "ftps-implicit": 990, ftp: 21 };

/** The connection choices a form edits, separate from name, host and login. */
export interface SiteForm {
  mode: SiteMode;
  auth: Auth;
  keyPath: string;
  autoConnect: boolean;
}

export const DEFAULT_FORM: SiteForm = { mode: "sftp", auth: "password", keyPath: "", autoConnect: false };

interface Options {
  implicit?: boolean;
  keyPath?: string;
  useAgent?: boolean;
  autoConnect?: boolean;
}

function options(s: Pick<Site, "optionsJson">): Options {
  try {
    return JSON.parse(s.optionsJson || "{}") ?? {};
  } catch {
    return {};
  }
}

export function formOf(s: Site): SiteForm {
  const o = options(s);
  return {
    mode: s.protocol === "ftp" ? "ftp" : s.protocol !== "ftps" ? "sftp" : o.implicit ? "ftps-implicit" : "ftps",
    auth: o.useAgent ? "agent" : o.keyPath ? "key" : "password",
    keyPath: o.keyPath ?? "",
    autoConnect: !!o.autoConnect,
  };
}

/** The Site fields a form's connection choices determine. */
export function formFields(f: SiteForm): Pick<Site, "protocol" | "optionsJson"> {
  const sftp = f.mode === "sftp";
  return {
    protocol: sftp ? "sftp" : f.mode === "ftp" ? "ftp" : "ftps",
    optionsJson: JSON.stringify({
      ...(f.mode === "ftps-implicit" && { implicit: true }),
      ...(sftp && f.auth === "key" && f.keyPath && { keyPath: f.keyPath }),
      ...(sftp && f.auth === "agent" && { useAgent: true }),
      ...(f.autoConnect && { autoConnect: true }),
    }),
  };
}

/** The label for the secret field, or null when the choices need none. */
export function secretLabel(f: SiteForm): string | null {
  if (f.mode !== "sftp" || f.auth === "password") return "Password";
  return f.auth === "key" ? "Key passphrase (if it has one)" : null;
}
