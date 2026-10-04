import { applyPaneSort, onPaneSortChanged, type SortState } from "../hooks/usePaneSort";
import { getPref, setPref } from "./prefs";
import { startPathHooks } from "../ipc";
import { useUiStore } from "../store";

/* "Remember each site's folder and sort" (Settings, off by default).
 *
 * A saved view per site: the folder it was last showing and the listing sort.
 * Connecting to the site opens that folder instead of the site's start folder,
 * and the sort comes back with it. The sort is one value shared by both panes,
 * so it follows whichever site you open. */

interface SiteView {
  path?: string;
  sort?: SortState;
}
type Views = Record<string, SiteView>;

const enabled = () => getPref("ui.remember_site_views") === "1";

function read(): Views {
  try {
    const v = JSON.parse(getPref("ui.site_views") ?? "{}");
    return v && typeof v === "object" && !Array.isArray(v) ? (v as Views) : {};
  } catch {
    return {};
  }
}

function write(id: number, patch: SiteView) {
  const all = read();
  all[String(id)] = { ...all[String(id)], ...patch };
  setPref("ui.site_views", JSON.stringify(all));
}

/** Wire the feature up once, at startup. */
let started = false;
export function startSiteViews() {
  if (started) return;
  started = true;
  startPathHooks.push((id) => (enabled() ? read()[String(id)]?.path : undefined));

  // Record the folder a pane is showing, and bring a site's sort back when a
  // pane first lands on it.
  let timer: number | undefined;
  useUiStore.subscribe((s, prev) => {
    if (!enabled() || s.panes === prev.panes) return;
    s.panes.forEach((p, i) => {
      const before = prev.panes[i];
      if (typeof p.source !== "number") return;
      if (before?.source !== p.source) {
        const saved = read()[String(p.source)]?.sort;
        if (saved) applyPaneSort(saved);
      }
      if (before?.source === p.source && before.path === p.path) return;
      const site = p.source;
      window.clearTimeout(timer);
      timer = window.setTimeout(() => write(site, { path: p.path }), 400);
    });
  });

  onPaneSortChanged((sort) => {
    if (!enabled()) return;
    const { panes, activePane } = useUiStore.getState();
    const src = panes[activePane]?.source;
    if (typeof src === "number") write(src, { sort });
  });
}
