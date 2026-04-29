import {createStore, produce} from 'solid-js/store';
import {
  api, onInspectorTick, onStatsTick, onTorrentsTick,
  type CategoryDTO, type DetailDTO, type GlobalStatsT, type InspectorTab, type TagDTO, type Torrent,
} from './bindings';

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
    defaultSavePath: '',

    inspectorOpenId: null,
    inspectorTab: 'overview',
    inspectorDetail: null,
    bandwidthRing: [],

    categories: [],
    tags: [],
    selectedCategoryID: null,
    selectedTagID: null,
  });

  api.listTorrents()
    .then((rows) => setState({torrents: rows, loading: false}))
    .catch((e) => { console.error(e); setState({loading: false}); });

  api.globalStats().then((s) => setState({stats: s})).catch(console.error);
  api.listCategories().then((cs) => setState(produce((s) => { s.categories = cs; }))).catch(console.error);
  api.listTags().then((ts) => setState(produce((s) => { s.tags = ts; }))).catch(console.error);
  api.getDefaultSavePath().then((p) => setState(produce((s) => { s.defaultSavePath = p; }))).catch(console.error);

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

    dispose: () => { offT(); offS(); offI(); },
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
