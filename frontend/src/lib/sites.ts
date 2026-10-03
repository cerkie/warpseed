import { deleteSite, importSites, pickFile, sites as fetchSites, type Site } from "../ipc";
import { forgetSource } from "./recents";
import { useUiStore } from "../store";

/** Delete a saved site, always after asking: it carries a saved password, a
    pinned host key and bookmarks, and none of it comes back. Deliberately has
    no "don't ask again". */
export function askDeleteSite(s: Pick<Site, "id" | "name">, onDone: () => void, onError: (err: unknown) => void) {
  useUiStore.getState().askConfirm({
    title: `Delete ${s.name}?`,
    body: "Its saved password, bookmarks and pinned host key go with it. Queued transfers for this site are not affected.",
    confirmLabel: "Delete site",
    danger: true,
    onConfirm: () => {
      void (async () => {
        try {
          await deleteSite(s.id);
          // SQLite recycles rowids, so this site's jump list must go too.
          forgetSource(s.id);
          useUiStore.getState().setSites(await fetchSites());
          onDone();
        } catch (err) {
          onError(err);
        }
      })();
    },
  });
}

/** Add the sites in a FileZilla or WinSCP export (the dropped file, or one the user picks). Resolves to a short
    message for a toast, or "" if the user cancelled the file dialog. */
export async function importSitesFromFile(dropped?: string): Promise<string> {
  const file = dropped ?? (await pickFile("Import sites from FileZilla (.xml) or WinSCP (.ini)"));
  if (!file) return "";
  const r = await importSites(file);
  useUiStore.getState().setSites(await fetchSites());
  const parts = [`Imported ${r.added} site${r.added === 1 ? "" : "s"}`];
  if (r.duplicates) parts.push(`${r.duplicates} already saved`);
  if (r.unsupported) parts.push(`${r.unsupported} skipped (not SFTP, FTP or FTPS)`);
  let msg = parts.join(", ");
  if (r.added > r.passwords) msg += ". Passwords were not included for some, so enter them when you connect";
  if (r.ppkKeys) msg += `. ${r.ppkKeys} used a PuTTY .ppk key, which warpseed cannot read: convert it to an OpenSSH key`;
  return msg;
}
