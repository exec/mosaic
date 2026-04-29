import {createStore, produce} from 'solid-js/store';
import {
  api, onInspectorTick, onStatsTick, onTorrentsTick, onUpdateAvailable,
  type BlocklistDTO, type CategoryDTO, type DetailDTO, type FeedDTO, type FilterDTO,
  type GlobalStatsT, type InspectorTab,
  type LimitsDTO, type QueueLimitsDTO, type ScheduleRuleDTO, type TagDTO, type Torrent,
  type UpdaterConfigDTO, type UpdateInfoDTO,
  type WebConfigDTO,
} from './bindings';
import type {SettingsPane} from '../components/settings/SettingsSidebar';

export type Density = 'cards' | 'table';
export type StatusFilter = 'all' | 'downloading' | 'seeding' | 'completed' | 'paused' | 'errored';
export type AppView = 'torrents' | 'settings';

export type BandwidthSample = {t: number; down: number; up: number};

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
  bandwidthRing: BandwidthSample[];     // ring buffer for Speed-tab chart, ~1Hz, capped at 24h

  // Organization
  categories: CategoryDTO[];
  tags: TagDTO[];
  selectedCategoryID: number | null;
  selectedTagID: number | null;

  // Bandwidth + queue
  limits: LimitsDTO;
  queueLimits: QueueLimitsDTO;

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
};

const BANDWIDTH_RING_MAX = 60 * 60 * 24; // 24 hours at 1 Hz

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
};

export function createTorrentsStore() {
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
    bandwidthRing: [],

    categories: [],
    tags: [],
    selectedCategoryID: null,
    selectedTagID: null,

    limits: emptyLimits,
    queueLimits: emptyQueueLimits,

    scheduleRules: [],
    blocklist: emptyBlocklist,

    feeds: [],
    filtersByFeed: {},

    webConfig: emptyWebConfig,

    updaterConfig: emptyUpdaterConfig,
    updateInfo: null,
    appVersion: 'dev',
  });

  api.listTorrents()
    .then((rows) => setState({torrents: rows, loading: false}))
    .catch((e) => { console.error(e); setState({loading: false}); });

  api.globalStats().then((s) => setState({stats: s})).catch(console.error);
  api.listCategories().then((cs) => setState(produce((s) => { s.categories = cs; }))).catch(console.error);
  api.listTags().then((ts) => setState(produce((s) => { s.tags = ts; }))).catch(console.error);
  api.getDefaultSavePath().then((p) => setState(produce((s) => { s.defaultSavePath = p; }))).catch(console.error);
  api.getLimits().then((l) => setState(produce((s) => { s.limits = l; }))).catch(console.error);
  api.getQueueLimits().then((q) => setState(produce((s) => { s.queueLimits = q; }))).catch(console.error);
  api.listScheduleRules().then((rs) => setState(produce((s) => { s.scheduleRules = rs ?? []; }))).catch(console.error);
  api.getBlocklist().then((b) => setState(produce((s) => { s.blocklist = b; }))).catch(console.error);
  api.listFeeds().then((fs) => setState(produce((s) => { s.feeds = fs ?? []; }))).catch(console.error);
  api.getWebConfig().then((c) => setState(produce((s) => { s.webConfig = c; }))).catch(console.error);
  api.getUpdaterConfig().then((c) => setState(produce((s) => { s.updaterConfig = c; }))).catch(console.error);
  api.appVersion().then((v) => setState(produce((s) => { s.appVersion = v; }))).catch(console.error);

  const offT = onTorrentsTick((rows) => setState(produce((s) => { s.torrents = rows; })));
  const offS = onStatsTick((stats) => setState(produce((s) => { s.stats = stats; })));
  const offI = onInspectorTick((detail) => {
    setState(produce((s) => {
      s.inspectorDetail = detail;
      s.bandwidthRing.push({
        t: Date.now() / 1000,
        down: s.stats.total_download_rate,
        up: s.stats.total_upload_rate,
      });
      if (s.bandwidthRing.length > BANDWIDTH_RING_MAX) s.bandwidthRing.shift();
    }));
  });
  const offU = onUpdateAvailable((info) => setState(produce((s) => { s.updateInfo = info; })));

  return {
    state,
    addMagnet: (m: string, savePath = '') => api.addMagnet(m, savePath),
    pickAndAddTorrent: (savePath = '') => api.pickAndAddTorrent(savePath),
    addTorrentBytes: (bytes: Uint8Array, savePath: string) => api.addTorrentBytes(bytes, savePath),
    pause: (id: string) => api.pause(id),
    resume: (id: string) => api.resume(id),
    remove: (id: string, deleteFiles: boolean) => api.remove(id, deleteFiles),

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
        s.inspectorDetail = null;
        s.bandwidthRing = [];
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
    setQueuePosition: (infohash: string, pos: number) => api.setQueuePosition(infohash, pos),
    setForceStart: (infohash: string, force: boolean) => api.setForceStart(infohash, force),

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

    dispose: () => { offT(); offS(); offI(); offU(); },
  };
}

export function filterTorrents(
  rows: Torrent[],
  status: StatusFilter,
  query: string,
  categoryID: number | null = null,
  tagID: number | null = null,
): Torrent[] {
  let out = rows;
  if (status !== 'all') {
    out = out.filter((t) => {
      switch (status) {
        case 'downloading': return !t.paused && !t.completed;
        case 'seeding':     return t.completed && !t.paused;
        case 'completed':   return t.completed;
        case 'paused':      return t.paused;
        case 'errored':     return false; // wired in Plan 5 when errors are surfaced
      }
    });
  }
  if (categoryID !== null) {
    out = out.filter((t) => t.category_id === categoryID);
  }
  if (tagID !== null) {
    out = out.filter((t) => t.tags.some((tg) => tg.id === tagID));
  }
  if (query.trim()) {
    const q = query.toLowerCase();
    out = out.filter((t) => t.name.toLowerCase().includes(q));
  }
  return out;
}
