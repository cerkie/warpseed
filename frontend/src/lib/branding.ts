/** Release identity. The version is NOT here: it lives in wails.json, is read
    from there by the Go side, and reaches the frontend through ipc.appVersion.
    A copy in this file drifted six releases behind and went out in the subject
    line of every emailed bug report. */
export const COMPANY = "Zyra Labs";
export const WEBSITE_URL = "https://zyralabs.tech";
export const DONATE_URL = "https://buymeacoffee.com/zyralabs";
/** This fork. The original is github.com/ZyraLabs/warpseed. */
export const REPO_URL = "https://github.com/cerkie/warpseed";

/**
 * A link to a new issue on the fork's repository with the version and
 * platform pre-filled, so a bug report arrives with the facts we always ask for.
 */
export function bugReportUrl(version: string): string {
  const body = [
    "**What happened:**",
    "",
    "**What I expected:**",
    "",
    "**Steps to reproduce:**",
    "1.",
    "",
    "---",
    `warpseed ${version}`,
    typeof navigator !== "undefined" ? navigator.userAgent : "",
  ].join("\n");
  const title = `Bug report (warpseed ${version})`;
  return `${REPO_URL}/issues/new?title=${encodeURIComponent(title)}&body=${encodeURIComponent(body)}`;
}
