import {
  AddMagnet, GlobalStats, ListTorrents, Pause, PickAndAddTorrent, Remove, Resume,
  SetInspectorFocus, ClearInspectorFocus,
} from '../../wailsjs/go/main/App';
import {EventsOn} from '../../wailsjs/runtime/runtime';

export type Torrent = {
  id: string;
  name: string;
  magnet: string;
  save_path: string;
  total_bytes: number;
  bytes_done: number;
  progress: number;
  download_rate: number;
  upload_rate: number;
  peers: number;
  seeds: number;
  paused: boolean;
  completed: boolean;
  added_at: number;
};

export type GlobalStatsT = {
  total_torrents: number;
  active_torrents: number;
  seeding_torrents: number;
  total_download_rate: number;
  total_upload_rate: number;
  total_peers: number;
};

export type InspectorTab = 'overview' | 'files' | 'peers' | 'trackers' | 'speed';

export type FileDTO = {
  index: number;
  path: string;
  size: number;
  bytes_done: number;
  progress: number;
  priority: 'skip' | 'normal' | 'high' | 'max';
};

export type PeerDTO = {
  ip: string;
  port: number;
  client: string;
  flags: string;
  progress: number;
  download_rate: number;
  upload_rate: number;
  country: string;
};

export type TrackerDTO = {
  url: string;
  status: string;
  seeds: number;
  peers: number;
  downloaded: number;
  last_announce: number; // unix seconds
  next_announce: number;
};

export type DetailDTO = {
  id: string;
  name: string;
  magnet: string;
  save_path: string;
  total_bytes: number;
  bytes_done: number;
  progress: number;
  ratio: number;
  total_down: number;
  total_up: number;
  peers: number;
  seeds: number;
  added_at: number;
  completed_at?: number;
  files?: FileDTO[];
  peers_list?: PeerDTO[];
  trackers?: TrackerDTO[];
};

export const api = {
  addMagnet: (magnet: string) => AddMagnet(magnet),
  pickAndAddTorrent: () => PickAndAddTorrent(),
  listTorrents: () => ListTorrents() as Promise<Torrent[]>,
  globalStats: () => GlobalStats() as Promise<GlobalStatsT>,
  pause: (id: string) => Pause(id),
  resume: (id: string) => Resume(id),
  remove: (id: string, deleteFiles: boolean) => Remove(id, deleteFiles),
  setInspectorFocus: (id: string, tabs: InspectorTab[]) => SetInspectorFocus(id, tabs),
  clearInspectorFocus: () => ClearInspectorFocus(),
};

export function onTorrentsTick(handler: (rows: Torrent[]) => void): () => void {
  return EventsOn('torrents:tick', handler);
}

export function onStatsTick(handler: (stats: GlobalStatsT) => void): () => void {
  return EventsOn('stats:tick', handler);
}

export function onInspectorTick(handler: (detail: DetailDTO) => void): () => void {
  return EventsOn('inspector:tick', handler);
}
