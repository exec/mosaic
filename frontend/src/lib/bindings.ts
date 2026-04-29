import {
  AddMagnet, GlobalStats, ListTorrents, Pause, PickAndAddTorrent, Remove, Resume,
  SetInspectorFocus, ClearInspectorFocus,
  ListCategories, CreateCategory, UpdateCategory, DeleteCategory,
  ListTags, CreateTag, DeleteTag, AssignTag, UnassignTag,
  SetTorrentCategory, SetFilePriorities, AddTorrentBytes,
  GetDefaultSavePath, SetDefaultSavePath,
  GetLimits, SetLimits, ToggleAltSpeed,
  GetQueueLimits, SetQueueLimits, SetQueuePosition, SetForceStart,
  ListScheduleRules, CreateScheduleRule, UpdateScheduleRule, DeleteScheduleRule,
  GetBlocklist, SetBlocklistURL, RefreshBlocklist,
  ListFeeds, CreateFeed, UpdateFeed, DeleteFeed,
  ListFiltersByFeed, CreateFilter, UpdateFilter, DeleteFilter,
} from '../../wailsjs/go/main/App';
import {EventsOn} from '../../wailsjs/runtime/runtime';

export type CategoryDTO = {
  id: number;
  name: string;
  default_save_path: string;
  color: string;
};

export type TagDTO = {
  id: number;
  name: string;
  color: string;
};

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
  category_id: number | null;
  tags: TagDTO[];
  queue_position: number;
  force_start: boolean;
  queued: boolean;
};

export type LimitsDTO = {
  down_kbps: number;
  up_kbps: number;
  alt_down_kbps: number;
  alt_up_kbps: number;
  alt_active: boolean;
};

export type QueueLimitsDTO = {
  max_active_downloads: number;
  max_active_seeds: number;
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

export type ScheduleRuleDTO = {
  id: number;
  days_mask: number;   // bit 0=Sun, bit 6=Sat
  start_min: number;   // 0..1439
  end_min: number;
  down_kbps: number;
  up_kbps: number;
  alt_only: boolean;
  enabled: boolean;
};

export type BlocklistDTO = {
  url: string;
  enabled: boolean;
  last_loaded_at: number; // unix seconds, 0 if never
  entries: number;
  error?: string;
};

export type FeedDTO = {
  id: number;
  url: string;
  name: string;
  interval_min: number;
  last_polled: number; // unix seconds, 0 if never
  etag: string;
  enabled: boolean;
};

export type FilterDTO = {
  id: number;
  feed_id: number;
  regex: string;
  category_id: number | null;
  save_path: string;
  enabled: boolean;
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
  addMagnet: (magnet: string, savePath: string) => AddMagnet(magnet, savePath),
  pickAndAddTorrent: (savePath: string) => PickAndAddTorrent(savePath),
  listTorrents: () => ListTorrents() as Promise<Torrent[]>,
  globalStats: () => GlobalStats() as Promise<GlobalStatsT>,
  pause: (id: string) => Pause(id),
  resume: (id: string) => Resume(id),
  remove: (id: string, deleteFiles: boolean) => Remove(id, deleteFiles),
  setInspectorFocus: (id: string, tabs: InspectorTab[]) => SetInspectorFocus(id, tabs),
  clearInspectorFocus: () => ClearInspectorFocus(),
  listCategories: () => ListCategories() as Promise<CategoryDTO[]>,
  createCategory: (name: string, savePath: string, color: string) => CreateCategory(name, savePath, color),
  updateCategory: (id: number, name: string, savePath: string, color: string) => UpdateCategory(id, name, savePath, color),
  deleteCategory: (id: number) => DeleteCategory(id),
  listTags: () => ListTags() as Promise<TagDTO[]>,
  createTag: (name: string, color: string) => CreateTag(name, color),
  deleteTag: (id: number) => DeleteTag(id),
  assignTag: (infohash: string, tagID: number) => AssignTag(infohash, tagID),
  unassignTag: (infohash: string, tagID: number) => UnassignTag(infohash, tagID),
  setTorrentCategory: (infohash: string, categoryID: number | null) => SetTorrentCategory(infohash, categoryID),
  setFilePriorities: (infohash: string, prios: Record<number, 'skip' | 'normal' | 'high' | 'max'>) => SetFilePriorities(infohash, prios),
  addTorrentBytes: (bytes: Uint8Array, savePath: string) => AddTorrentBytes(Array.from(bytes), savePath),
  getDefaultSavePath: () => GetDefaultSavePath() as Promise<string>,
  setDefaultSavePath: (path: string) => SetDefaultSavePath(path),
  getLimits: () => GetLimits() as Promise<LimitsDTO>,
  setLimits: (l: LimitsDTO) => SetLimits(l),
  toggleAltSpeed: () => ToggleAltSpeed() as Promise<boolean>,
  getQueueLimits: () => GetQueueLimits() as Promise<QueueLimitsDTO>,
  setQueueLimits: (q: QueueLimitsDTO) => SetQueueLimits(q),
  setQueuePosition: (infohash: string, pos: number) => SetQueuePosition(infohash, pos),
  setForceStart: (infohash: string, force: boolean) => SetForceStart(infohash, force),
  listScheduleRules: () => ListScheduleRules() as Promise<ScheduleRuleDTO[]>,
  createScheduleRule: (r: ScheduleRuleDTO) => CreateScheduleRule(r as any),
  updateScheduleRule: (r: ScheduleRuleDTO) => UpdateScheduleRule(r as any),
  deleteScheduleRule: (id: number) => DeleteScheduleRule(id),
  getBlocklist: () => GetBlocklist() as Promise<BlocklistDTO>,
  setBlocklistURL: (url: string, enabled: boolean) => SetBlocklistURL(url, enabled),
  refreshBlocklist: () => RefreshBlocklist(),
  listFeeds: () => ListFeeds() as Promise<FeedDTO[]>,
  createFeed: (f: FeedDTO) => CreateFeed(f as any),
  updateFeed: (f: FeedDTO) => UpdateFeed(f as any),
  deleteFeed: (id: number) => DeleteFeed(id),
  listFiltersByFeed: (feedID: number) => ListFiltersByFeed(feedID) as Promise<FilterDTO[]>,
  createFilter: (f: FilterDTO) => CreateFilter(f as any),
  updateFilter: (f: FilterDTO) => UpdateFilter(f as any),
  deleteFilter: (id: number) => DeleteFilter(id),
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
