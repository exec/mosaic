import {createStore, produce, reconcile} from 'solid-js/store';
import {toast} from 'solid-sonner';
import {
  api, onInspectorTick, onStatsTick, onTorrentsTick, onUpdateAvailable,
  type BlocklistDTO, type CategoryDTO, type DesktopIntegrationDTO, type DetailDTO,
  type FeedDTO, type FilterDTO,
  type GlobalStatsT, type InspectorTab,
  type LimitsDTO, type PeerLimitsDTO, type QueueLimitsDTO, type ScheduleRuleDTO, type TagDTO, type Torrent,
  type UpdaterConfigDTO, type UpdateInfoDTO,
  type WebConfigDTO, type ServerFlavor, type UserDTO,
} from './bindings';
import type {SettingsPane} from '../components/settings/SettingsSidebar';
import {isWailsRuntime} from './runtime';
import {BandwidthRing} from './ringbuffer';

export type Density = 'cards' | 'table';
export type StatusFilter = 'all' | 'downloading' | 'seeding' | 'completed' | 'paused';
export type AppView = 'torrents' | 'settings';

export type {BandwidthSample} from './ringbuffer';

export type AppState = {
  torrents: Torrent[];
  stats: GlobalStatsT;
  selection: Set<string>;
  density: Density;
  statusFilter: StatusFilter;
  searchQuery: string;
  loading: boolean;

  // View routing
  view: AppView;
  settingsPane: SettingsPane;
  defaultSavePath: string;

  // Inspector
  inspectorOpenId: string | null;       // null = closed
  inspectorTab: InspectorTab;
  inspectorDetail: DetailDTO | null;    // latest tick payload
  // Monotonic counter bumped on every inspector tick and reset to 0 when the
  // bandwidth history is cleared. The live history itself lives in a
  // BandwidthRing held outside the reactive store (a 86k-element store array
  // is needlessly expensive to track); this counter is the reactive handle
  // the Speed-tab chart subscribes to so it knows when to pull a new sample.
  bandwidthTick: number;

  // Organization
  categories: CategoryDTO[];
  tags: TagDTO[];
  selectedCategoryID: number | null;
  selectedTagID: number | null;

  // Bandwidth + queue
  limits: LimitsDTO;
  queueLimits: QueueLimitsDTO;
  peerLimits: PeerLimitsDTO;

  // Scheduling + blocklist
  scheduleRules: ScheduleRuleDTO[];
  blocklist: BlocklistDTO;

  // RSS
  feeds: FeedDTO[];
  filtersByFeed: Record<number, FilterDTO[]>;

  // Web interface
  webConfig: WebConfigDTO;

  // Auto-update
  updaterConfig: UpdaterConfigDTO;
  updateInfo: UpdateInfoDTO | null;
  appVersion: string;

  // Desktop integration (tray + notifications)
  desktopIntegration: DesktopIntegrationDTO;

  // Multi-user. serverFlavor distinguishes the headless mosaicd daemon
  // ('daemon') from the desktop app / its optional web server ('desktop').
  // currentUser is the logged-in account on mosaicd; null on the desktop
  // build, where there is no login and every action runs with full access.
  serverFlavor: ServerFlavor;
  currentUser: UserDTO | null;
};

// 24 hours of ~1 Hz samples. The chart downsamples this on read, so the
// working set it actually renders stays small regardless of the cap.
const BANDWIDTH_RING_MAX = 60 * 60 * 24;

// rowToDetail builds a partial DetailDTO from a Torrent row so the
// inspector can render its overview/header immediately on torrent switch
// without flashing to 0% and animating back up while it waits for the
// first inspector:tick. Files/peers/trackers stay undefined here — those
// tabs already render a "waiting for…" fallback when the data is missing.
function rowToDetail(t: import('./bindings').Torrent): import('./bindings').DetailDTO {
  return {
    id: t.id,
    name: t.name,
    magnet: t.magnet,
    save_path: t.save_path,
    total_bytes: t.total_bytes,
    bytes_done: t.bytes_done,
    progress: t.progress,
    ratio: t.bytes_done > 0 ? t.bytes_done / Math.max(1, t.total_bytes) : 0,
    total_down: 0, // tick will fill in real cumulative counters
    total_up: 0,
    peers: t.peers,
    seeds: t.seeds,
    added_at: t.added_at,
    paused: t.paused,
    completed: t.completed,
    files_missing: t.files_missing,
  };
}

function tabsForActive(tab: InspectorTab): InspectorTab[] {
  return tab === 'overview' ? ['overview'] : ['overview', tab];
}

const DENSITY_KEY = 'mosaic.density';

function loadDensity(): Density {
  return (localStorage.getItem(DENSITY_KEY) as Density) === 'table' ? 'table' : 'cards';
}

const emptyStats: GlobalStatsT = {
  total_torrents: 0,
  active_torrents: 0,
  seeding_torrents: 0,
  total_download_rate: 0,
  total_upload_rate: 0,
  total_peers: 0,
};

const emptyLimits: LimitsDTO = {
  down_kbps: 0,
  up_kbps: 0,
  alt_down_kbps: 0,
  alt_up_kbps: 0,
  alt_active: false,
};

const emptyQueueLimits: QueueLimitsDTO = {
  max_active_downloads: 0,
  max_active_seeds: 0,
};

const emptyPeerLimits: PeerLimitsDTO = {
  listen_port: 0,
  max_peers_per_torrent: 0,
  dht_enabled: true,
  encryption_enabled: true,
};

const emptyBlocklist: BlocklistDTO = {
  url: '',
  enabled: false,
  last_loaded_at: 0,
  entries: 0,
};

const emptyWebConfig: WebConfigDTO = {
  enabled: false,
  port: 8080,
  bind_all: false,
  username: 'admin',
  api_key: '',
};

const emptyUpdaterConfig: UpdaterConfigDTO = {
  enabled: true,
  channel: 'stable',
  last_checked_at: 0,
  last_seen_version: '',
  install_source: 'manual',
};

const defaultDesktopIntegration: DesktopIntegrationDTO = {
  tray_enabled: true,
  close_to_tray: false,
  start_minimized: false,
  notify_on_complete: true,
  notify_on_error: true,
  notify_on_update: true,
};

export function createTorrentsStore() {
  // Live bandwidth history. Held outside the reactive store so per-tick
  // pushes stay O(1) and don't churn a tracked array; the chart reads it
  // imperatively and re-runs off the reactive `bandwidthTick` counter.
  const bandwidthRing = new BandwidthRing(BANDWIDTH_RING_MAX);

  const [state, setState] = createStore<AppState>({
    torrents: [],
    stats: emptyStats,
    selection: new Set(),
    density: loadDensity(),
    statusFilter: 'all',
    searchQuery: '',
    loading: true,

    view: 'torrents',
    settingsPane: 'general',
    defaultSavePath: '',

    inspectorOpenId: null,
    inspectorTab: 'overview',
    inspectorDetail: null,
    bandwidthTick: 0,

    categories: [],
    tags: [],
    selectedCategoryID: null,
    selectedTagID: null,

    limits: emptyLimits,
    queueLimits: emptyQueueLimits,
    peerLimits: emptyPeerLimits,

    scheduleRules: [],
    blocklist: emptyBlocklist,

    feeds: [],
    filtersByFeed: {},

    webConfig: emptyWebConfig,

    updaterConfig: emptyUpdaterConfig,
    updateInfo: null,
    appVersion: 'dev',

    desktopIntegration: defaultDesktopIntegration,

    serverFlavor: 'desktop',
    currentUser: null,
  });

  // Boot fetches. Each failure is logged AND surfaces a single aggregated
  // toast so the user notices when the backend is unreachable instead of
  // sitting in front of an empty UI forever. We don't toast per-failure
  // because a network outage would explode the screen — one toast per boot.
  let bootFailureToasted = false;
  const bootFailed = (label: string) => (e: unknown) => {
    console.error(`boot fetch '${label}' failed:`, e);
    if (bootFailureToasted) return;
    bootFailureToasted = true;
    toast.error(`Couldn't reach Mosaic backend (${label}). Check the desktop app or web server.`);
  };

  api.listTorrents()
    .then((rows) => setState({torrents: rows, loading: false}))
    .catch((e) => { bootFailed('torrents')(e); setState({loading: false}); });

  api.globalStats().then((s) => setState({stats: s})).catch(bootFailed('stats'));
  api.listCategories().then((cs) => setState(produce((s) => { s.categories = cs; }))).catch(bootFailed('categories'));
  api.listTags().then((ts) => setState(produce((s) => { s.tags = ts; }))).catch(bootFailed('tags'));
  api.getDefaultSavePath().then((p) => setState(produce((s) => { s.defaultSavePath = p; }))).catch(bootFailed('default save path'));
  api.getLimits().then((l) => setState(produce((s) => { s.limits = l; }))).catch(bootFailed('limits'));
  api.getQueueLimits().then((q) => setState(produce((s) => { s.queueLimits = q; }))).catch(bootFailed('queue limits'));
  api.getPeerLimits().then((p) => setState(produce((s) => { s.peerLimits = p; }))).catch(bootFailed('peer limits'));
  api.listScheduleRules().then((rs) => setState(produce((s) => { s.scheduleRules = rs ?? []; }))).catch(bootFailed('schedule rules'));
  api.getBlocklist().then((b) => setState(produce((s) => { s.blocklist = b; }))).catch(bootFailed('blocklist'));
  api.listFeeds().then((fs) => setState(produce((s) => { s.feeds = fs ?? []; }))).catch(bootFailed('feeds'));
  api.getWebConfig().then((c) => setState(produce((s) => { s.webConfig = c; }))).catch(bootFailed('web config'));
  api.getUpdaterConfig().then((c) => setState(produce((s) => { s.updaterConfig = c; }))).catch(bootFailed('updater config'));
  api.appVersion().then((v) => setState(produce((s) => { s.appVersion = v; }))).catch(bootFailed('app version'));
  // Desktop integration (tray, notifications, close-to-tray) is inherently
  // tied to the local OS session running the binary. The HTTP transport
  // doesn't mirror these endpoints — fetching from a browser SPA would only
  // surface a "no route" error and the toggles would have no effect on the
  // remote machine. Skip the boot fetch in browser mode; the SettingsSidebar
  // also hides the Desktop pane there.
  if (isWailsRuntime()) {
    api.getDesktopIntegration().then((d) => setState(produce((s) => { s.desktopIntegration = d; }))).catch(bootFailed('desktop integration'));
  } else {
    // Browser mode: learn whether we're talking to the multi-user mosaicd
    // daemon or the desktop app's single-user web server, and load the
    // logged-in account so the UI can gate per-permission controls. Route
    // failures through bootFailed so the user sees a toast — without it
    // these silently console.error'd and a mosaicd unreachable at startup
    // produced an empty Users pane and no UI feedback at all.
    api.bootstrap()
      .then((b) => setState(produce((s) => { s.serverFlavor = b.flavor; })))
      .catch(bootFailed('bootstrap'));
    api.me()
      .then((u) => setState(produce((s) => { s.currentUser = u; })))
      .catch(bootFailed('current user'));
  }

  const offT = onTorrentsTick((rows) => setState('torrents', reconcile(rows, {key: 'id'})));
  const offS = onStatsTick((stats) => setState(produce((s) => { s.stats = stats; })));
  const offI = onInspectorTick((detail) => {
    // Append to the ring imperatively (O(1), no shift), then bump the
    // reactive counter so the Speed-tab chart pulls the new sample.
    bandwidthRing.push(
      Date.now() / 1000,
      state.stats.total_download_rate,
      state.stats.total_upload_rate,
    );
    setState(produce((s) => {
      s.inspectorDetail = detail;
      s.bandwidthTick = s.bandwidthTick + 1;
    }));
  });
  const offU = onUpdateAvailable((info) => setState(produce((s) => { s.updateInfo = info; })));

  return {
    state,
    // The live bandwidth history backing the Speed-tab chart. Read it
    // alongside state.bandwidthTick — the counter is the reactive trigger,
    // this ring holds the actual samples.
    bandwidthRing,
    addMagnet: (m: string, savePath = '') => api.addMagnet(m, savePath),
    pickAndAddTorrent: (savePath = '') => api.pickAndAddTorrent(savePath),
    addTorrentBytes: (bytes: Uint8Array, savePath: string) => api.addTorrentBytes(bytes, savePath),
    pause: (id: string) => api.pause(id),
    resume: (id: string) => api.resume(id),
    remove: async (id: string, deleteFiles: boolean) => {
      await api.remove(id, deleteFiles);
      // Removing the focused torrent should also tear down the inspector
      // pane and drop the row from any active selection — otherwise the
      // pane keeps rendering the last cached detail of a torrent that no
      // longer exists, and bulk operations operate on a phantom selection.
      // The websocket / engine tick is eventually-consistent on the row
      // list, so we proactively patch the local UI state instead of
      // waiting for the next tick to remove the row.
      setState(produce((s) => {
        if (s.inspectorOpenId === id) {
          s.inspectorOpenId = null;
          s.inspectorDetail = null;
          bandwidthRing.clear();
          s.bandwidthTick = 0;
        }
        if (s.selection.has(id)) {
          const next = new Set(s.selection);
          next.delete(id);
          s.selection = next;
        }
      }));
      // Best-effort backend de-focus when we just closed the inspector
      // (matches closeInspector). Failure is non-fatal — the engine
      // simply keeps emitting inspector ticks for a torrent we'll
      // ignore on the frontend until the row drops out of the list.
      if (state.inspectorOpenId === null) {
        try { await api.clearInspectorFocus(); } catch { /* ignore */ }
      }
    },

    // Selection
    select: (id: string) => setState(produce((s) => { s.selection = new Set([id]); })),
    toggleSelect: (id: string) => setState(produce((s) => {
      const next = new Set(s.selection);
      if (next.has(id)) next.delete(id); else next.add(id);
      s.selection = next;
    })),
    extendSelectTo: (id: string) => setState(produce((s) => {
      // Range select: from last-selected to id within the current visible list order.
      const visible = s.torrents.map((t) => t.id);
      const last = [...s.selection].pop();
      if (!last) { s.selection = new Set([id]); return; }
      const a = visible.indexOf(last);
      const b = visible.indexOf(id);
      if (a < 0 || b < 0) { s.selection = new Set([id]); return; }
      const [lo, hi] = a < b ? [a, b] : [b, a];
      s.selection = new Set(visible.slice(lo, hi + 1));
    })),
    selectAll: () => setState(produce((s) => { s.selection = new Set(s.torrents.map((t) => t.id)); })),
    clearSelection: () => setState(produce((s) => { s.selection = new Set(); })),

    // Inspector
    openInspector: async (id: string, tab: InspectorTab = 'overview') => {
      setState(produce((s) => {
        s.inspectorOpenId = id;
        s.inspectorTab = tab;
        // Pre-seed inspectorDetail from the row we already have in the
        // torrents list so the progress bar / header don't flash to 0
        // and animate back up while we wait ~1s for the first
        // inspector:tick. Files / peers / trackers stay undefined until
        // the tick (those tabs already render a spinner / waiting state).
        // Without this seeding the inspector header looked like the new
        // torrent reset to 0% then "shot back up" on each torrent switch.
        const row = s.torrents.find((t) => t.id === id);
        s.inspectorDetail = row ? rowToDetail(row) : null;
        bandwidthRing.clear();
        s.bandwidthTick = 0;
      }));
      await api.setInspectorFocus(id, tabsForActive(tab));
    },
    closeInspector: async () => {
      setState(produce((s) => { s.inspectorOpenId = null; s.inspectorDetail = null; }));
      await api.clearInspectorFocus();
    },
    setInspectorTab: async (tab: InspectorTab) => {
      setState(produce((s) => { s.inspectorTab = tab; }));
      if (state.inspectorOpenId) {
        await api.setInspectorFocus(state.inspectorOpenId, tabsForActive(tab));
      }
    },

    // View
    setDensity: (d: Density) => {
      localStorage.setItem(DENSITY_KEY, d);
      setState(produce((s) => { s.density = d; }));
    },
    setStatusFilter: (f: StatusFilter) => setState(produce((s) => { s.statusFilter = f; })),
    setSearchQuery: (q: string) => setState(produce((s) => { s.searchQuery = q; })),
    setView: (v: AppView) => setState(produce((s) => { s.view = v; })),
    setSettingsPane: (p: SettingsPane) => setState(produce((s) => { s.settingsPane = p; })),
    setDefaultSavePath: async (p: string) => {
      await api.setDefaultSavePath(p);
      setState(produce((s) => { s.defaultSavePath = p; }));
    },

    // Organization
    refreshCategories: async () => {
      const cs = await api.listCategories();
      setState(produce((s) => { s.categories = cs; }));
    },
    refreshTags: async () => {
      const ts = await api.listTags();
      setState(produce((s) => { s.tags = ts; }));
    },
    createCategory: async (name: string, savePath: string, color: string) => {
      await api.createCategory(name, savePath, color);
      const cs = await api.listCategories();
      setState(produce((s) => { s.categories = cs; }));
    },
    updateCategory: async (id: number, name: string, savePath: string, color: string) => {
      await api.updateCategory(id, name, savePath, color);
      const cs = await api.listCategories();
      setState(produce((s) => { s.categories = cs; }));
    },
    deleteCategory: async (id: number) => {
      await api.deleteCategory(id);
      const cs = await api.listCategories();
      setState(produce((s) => {
        s.categories = cs;
        if (s.selectedCategoryID === id) s.selectedCategoryID = null;
      }));
    },
    createTag: async (name: string, color: string) => {
      await api.createTag(name, color);
      const ts = await api.listTags();
      setState(produce((s) => { s.tags = ts; }));
    },
    deleteTag: async (id: number) => {
      await api.deleteTag(id);
      const ts = await api.listTags();
      setState(produce((s) => {
        s.tags = ts;
        if (s.selectedTagID === id) s.selectedTagID = null;
      }));
    },
    setTorrentCategory: (infohash: string, categoryID: number | null) => api.setTorrentCategory(infohash, categoryID),
    assignTag: (infohash: string, tagID: number) => api.assignTag(infohash, tagID),
    unassignTag: (infohash: string, tagID: number) => api.unassignTag(infohash, tagID),
    setFilePriorities: (infohash: string, prios: Record<number, 'skip' | 'normal' | 'high' | 'max'>) =>
      api.setFilePriorities(infohash, prios),
    setSelectedCategory: (id: number | null) => setState(produce((s) => { s.selectedCategoryID = id; })),
    setSelectedTag: (id: number | null) => setState(produce((s) => { s.selectedTagID = id; })),

    // Bandwidth + queue
    setLimits: async (l: LimitsDTO) => {
      await api.setLimits(l);
      setState(produce((s) => { s.limits = l; }));
    },
    toggleAltSpeed: async () => {
      const next = await api.toggleAltSpeed();
      setState(produce((s) => { s.limits = {...s.limits, alt_active: next}; }));
    },
    setQueueLimits: async (q: QueueLimitsDTO) => {
      await api.setQueueLimits(q);
      setState(produce((s) => { s.queueLimits = q; }));
    },
    setPeerLimits: async (p: PeerLimitsDTO) => {
      await api.setPeerLimits(p);
      setState(produce((s) => { s.peerLimits = p; }));
    },
    setQueuePosition: (infohash: string, pos: number) => api.setQueuePosition(infohash, pos),
    setForceStart: (infohash: string, force: boolean) => api.setForceStart(infohash, force),
    setSequential: (infohash: string, enabled: boolean) => api.setSequential(infohash, enabled),

    // Scheduling
    refreshScheduleRules: async () => {
      const rs = await api.listScheduleRules();
      setState(produce((s) => { s.scheduleRules = rs ?? []; }));
    },
    createScheduleRule: async (r: ScheduleRuleDTO) => {
      await api.createScheduleRule(r);
      const rs = await api.listScheduleRules();
      setState(produce((s) => { s.scheduleRules = rs ?? []; }));
    },
    updateScheduleRule: async (r: ScheduleRuleDTO) => {
      await api.updateScheduleRule(r);
      const rs = await api.listScheduleRules();
      setState(produce((s) => { s.scheduleRules = rs ?? []; }));
    },
    deleteScheduleRule: async (id: number) => {
      await api.deleteScheduleRule(id);
      const rs = await api.listScheduleRules();
      setState(produce((s) => { s.scheduleRules = rs ?? []; }));
    },

    // Blocklist
    refreshBlocklistStatus: async () => {
      const b = await api.getBlocklist();
      setState(produce((s) => { s.blocklist = b; }));
    },
    setBlocklistURL: async (url: string, enabled: boolean) => {
      await api.setBlocklistURL(url, enabled);
      const b = await api.getBlocklist();
      setState(produce((s) => { s.blocklist = b; }));
    },
    refreshBlocklist: async () => {
      try {
        await api.refreshBlocklist();
      } finally {
        const b = await api.getBlocklist();
        setState(produce((s) => { s.blocklist = b; }));
      }
    },

    // RSS
    refreshFeeds: async () => {
      const fs = await api.listFeeds();
      setState(produce((s) => { s.feeds = fs ?? []; }));
    },
    createFeed: async (f: FeedDTO) => {
      await api.createFeed(f);
      const fs = await api.listFeeds();
      setState(produce((s) => { s.feeds = fs ?? []; }));
    },
    updateFeed: async (f: FeedDTO) => {
      await api.updateFeed(f);
      const fs = await api.listFeeds();
      setState(produce((s) => { s.feeds = fs ?? []; }));
    },
    deleteFeed: async (id: number) => {
      await api.deleteFeed(id);
      const fs = await api.listFeeds();
      setState(produce((s) => {
        s.feeds = fs ?? [];
        delete s.filtersByFeed[id];
      }));
    },
    pollFeed: async (id: number) => {
      // Triggers an immediate fetch on the backend regardless of the
      // feed's poll-interval schedule. Refresh the feed list afterward
      // so last_polled / etag updates land in state.
      await api.pollFeedNow(id);
      const fs = await api.listFeeds();
      setState(produce((s) => { s.feeds = fs ?? []; }));
    },
    refreshFiltersForFeed: async (feedID: number) => {
      const rows = await api.listFiltersByFeed(feedID);
      setState(produce((s) => { s.filtersByFeed[feedID] = rows ?? []; }));
    },
    createFilter: async (f: FilterDTO) => {
      await api.createFilter(f);
      const rows = await api.listFiltersByFeed(f.feed_id);
      setState(produce((s) => { s.filtersByFeed[f.feed_id] = rows ?? []; }));
    },
    updateFilter: async (f: FilterDTO) => {
      await api.updateFilter(f);
      const rows = await api.listFiltersByFeed(f.feed_id);
      setState(produce((s) => { s.filtersByFeed[f.feed_id] = rows ?? []; }));
    },
    deleteFilter: async (feedID: number, id: number) => {
      await api.deleteFilter(id);
      const rows = await api.listFiltersByFeed(feedID);
      setState(produce((s) => { s.filtersByFeed[feedID] = rows ?? []; }));
    },

    // Web interface
    refreshWebConfig: async () => {
      const c = await api.getWebConfig();
      setState(produce((s) => { s.webConfig = c; }));
    },
    setWebConfig: async (c: WebConfigDTO) => {
      await api.setWebConfig(c);
      const fresh = await api.getWebConfig();
      setState(produce((s) => { s.webConfig = fresh; }));
    },
    setWebPassword: (plain: string) => api.setWebPassword(plain),
    rotateAPIKey: async () => {
      const key = await api.rotateAPIKey();
      setState(produce((s) => { s.webConfig = {...s.webConfig, api_key: key}; }));
      return key;
    },

    // Auto-update
    refreshUpdaterConfig: async () => {
      const c = await api.getUpdaterConfig();
      setState(produce((s) => { s.updaterConfig = c; }));
    },
    setUpdaterConfig: async (c: UpdaterConfigDTO) => {
      await api.setUpdaterConfig(c);
      const fresh = await api.getUpdaterConfig();
      setState(produce((s) => { s.updaterConfig = fresh; }));
    },
    checkForUpdate: async () => {
      const info = await api.checkForUpdate();
      setState(produce((s) => { s.updateInfo = info; }));
      return info;
    },
    installUpdate: () => api.installUpdate(),

    // Desktop integration
    refreshDesktopIntegration: async () => {
      const d = await api.getDesktopIntegration();
      setState(produce((s) => { s.desktopIntegration = d; }));
    },
    setDesktopIntegration: async (d: DesktopIntegrationDTO) => {
      await api.setDesktopIntegration(d);
      setState(produce((s) => { s.desktopIntegration = d; }));
    },

    dispose: () => { offT(); offS(); offI(); offU(); },
  };
}

// TorrentCounts holds every badge tally the shell needs: the five status
// filters, per-category, per-tag, and the queued count. Computed in one
// pass by computeCounts so the FilterRail doesn't re-scan the full list
// once per badge on every tick.
export type TorrentCounts = {
  all: number;
  downloading: number;
  seeding: number;
  completed: number;
  paused: number;
  queued: number;
  byCategory: Record<number, number>;
  byTag: Record<number, number>;
};

// computeCounts tallies all torrent badge counts in a single scan.
export function computeCounts(rows: Torrent[]): TorrentCounts {
  const counts: TorrentCounts = {
    all: rows.length,
    downloading: 0,
    seeding: 0,
    completed: 0,
    paused: 0,
    queued: 0,
    byCategory: {},
    byTag: {},
  };
  for (const t of rows) {
    if (t.paused) counts.paused++;
    if (t.completed) counts.completed++;
    if (t.completed && !t.paused) counts.seeding++;
    if (!t.paused && !t.completed) counts.downloading++;
    if (t.queued) counts.queued++;
    if (t.category_id !== null) {
      counts.byCategory[t.category_id] = (counts.byCategory[t.category_id] ?? 0) + 1;
    }
    for (const tg of t.tags) {
      counts.byTag[tg.id] = (counts.byTag[tg.id] ?? 0) + 1;
    }
  }
  return counts;
}

export function filterTorrents(
  rows: Torrent[],
  status: StatusFilter,
  query: string,
  categoryID: number | null = null,
  tagID: number | null = null,
): Torrent[] {
  // Single pass: lowercase the query once outside the loop, then test
  // every criterion in one predicate instead of chaining .filter() calls.
  const q = query.trim() ? query.toLowerCase() : null;
  return rows.filter((t) => {
    switch (status) {
      case 'downloading': if (t.paused || t.completed) return false; break;
      case 'seeding':     if (!t.completed || t.paused) return false; break;
      case 'completed':   if (!t.completed) return false; break;
      case 'paused':      if (!t.paused) return false; break;
      // 'all' — no status constraint.
    }
    if (categoryID !== null && t.category_id !== categoryID) return false;
    if (tagID !== null && !t.tags.some((tg) => tg.id === tagID)) return false;
    if (q !== null && !t.name.toLowerCase().includes(q)) return false;
    return true;
  });
}
