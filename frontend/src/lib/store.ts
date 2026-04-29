import {createStore, produce} from 'solid-js/store';
import {api, onStatsTick, onTorrentsTick, type GlobalStatsT, type Torrent} from './bindings';

export type Density = 'cards' | 'table';
export type StatusFilter = 'all' | 'downloading' | 'seeding' | 'completed' | 'paused' | 'errored';

export type AppState = {
  torrents: Torrent[];
  stats: GlobalStatsT;
  selection: Set<string>;
  density: Density;
  statusFilter: StatusFilter;
  searchQuery: string;
  loading: boolean;
};

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
  });

  api.listTorrents()
    .then((rows) => setState({torrents: rows, loading: false}))
    .catch((e) => { console.error(e); setState({loading: false}); });

  api.globalStats().then((s) => setState({stats: s})).catch(console.error);

  const offT = onTorrentsTick((rows) => setState(produce((s) => { s.torrents = rows; })));
  const offS = onStatsTick((stats) => setState(produce((s) => { s.stats = stats; })));

  return {
    state,
    addMagnet: (m: string) => api.addMagnet(m),
    pickAndAddTorrent: () => api.pickAndAddTorrent(),
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

    // View
    setDensity: (d: Density) => {
      localStorage.setItem(DENSITY_KEY, d);
      setState(produce((s) => { s.density = d; }));
    },
    setStatusFilter: (f: StatusFilter) => setState(produce((s) => { s.statusFilter = f; })),
    setSearchQuery: (q: string) => setState(produce((s) => { s.searchQuery = q; })),

    dispose: () => { offT(); offS(); },
  };
}

export function filterTorrents(rows: Torrent[], status: StatusFilter, query: string): Torrent[] {
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
  if (query.trim()) {
    const q = query.toLowerCase();
    out = out.filter((t) => t.name.toLowerCase().includes(q));
  }
  return out;
}
