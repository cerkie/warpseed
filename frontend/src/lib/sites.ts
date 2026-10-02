import { deleteSite, sites as fetchSites, type Site } from "../ipc";
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
