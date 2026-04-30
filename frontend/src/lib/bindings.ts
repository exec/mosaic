import {transport} from './transport';

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

export type WebConfigDTO = {
  enabled: boolean;
  port: number;
  bind_all: boolean;
  username: string;
  api_key: string;
};

export type UpdaterConfigDTO = {
  enabled: boolean;
  channel: 'stable' | 'beta';
  last_checked_at: number;
  last_seen_version: string;
};

export type UpdateInfoDTO = {
  available: boolean;
  latest_version: string;
  asset_url: string;
  asset_filename: string;
  checked_at: number;
  current_version: string;
};

export const api = {
  addMagnet: (magnet: string, savePath: string) => transport.invoke<string>('AddMagnet', magnet, savePath),
  pickAndAddTorrent: (savePath: string) => transport.invoke<string>('PickAndAddTorrent', savePath),
  listTorrents: () => transport.invoke<Torrent[]>('ListTorrents'),
  globalStats: () => transport.invoke<GlobalStatsT>('GlobalStats'),
  pause: (id: string) => transport.invoke<void>('Pause', id),
  resume: (id: string) => transport.invoke<void>('Resume', id),
  remove: (id: string, deleteFiles: boolean) => transport.invoke<void>('Remove', id, deleteFiles),
  setInspectorFocus: (id: string, tabs: InspectorTab[]) => transport.invoke<void>('SetInspectorFocus', id, tabs),
  clearInspectorFocus: () => transport.invoke<void>('ClearInspectorFocus'),
  listCategories: () => transport.invoke<CategoryDTO[]>('ListCategories'),
  createCategory: (name: string, savePath: string, color: string) =>
    transport.invoke<number>('CreateCategory', name, savePath, color),
  updateCategory: (id: number, name: string, savePath: string, color: string) =>
    transport.invoke<void>('UpdateCategory', id, name, savePath, color),
  deleteCategory: (id: number) => transport.invoke<void>('DeleteCategory', id),
  listTags: () => transport.invoke<TagDTO[]>('ListTags'),
  createTag: (name: string, color: string) => transport.invoke<number>('CreateTag', name, color),
  deleteTag: (id: number) => transport.invoke<void>('DeleteTag', id),
  assignTag: (infohash: string, tagID: number) => transport.invoke<void>('AssignTag', infohash, tagID),
  unassignTag: (infohash: string, tagID: number) => transport.invoke<void>('UnassignTag', infohash, tagID),
  setTorrentCategory: (infohash: string, categoryID: number | null) =>
    transport.invoke<void>('SetTorrentCategory', infohash, categoryID),
  setFilePriorities: (infohash: string, prios: Record<number, 'skip' | 'normal' | 'high' | 'max'>) =>
    transport.invoke<void>('SetFilePriorities', infohash, prios),
  addTorrentBytes: (bytes: Uint8Array, savePath: string) =>
    transport.invoke<string>('AddTorrentBytes', bytes, savePath),
  getDefaultSavePath: () => transport.invoke<string>('GetDefaultSavePath'),
  setDefaultSavePath: (path: string) => transport.invoke<void>('SetDefaultSavePath', path),
  getLimits: () => transport.invoke<LimitsDTO>('GetLimits'),
  setLimits: (l: LimitsDTO) => transport.invoke<void>('SetLimits', l),
  toggleAltSpeed: () => transport.invoke<boolean>('ToggleAltSpeed'),
  getQueueLimits: () => transport.invoke<QueueLimitsDTO>('GetQueueLimits'),
  setQueueLimits: (q: QueueLimitsDTO) => transport.invoke<void>('SetQueueLimits', q),
  setQueuePosition: (infohash: string, pos: number) => transport.invoke<void>('SetQueuePosition', infohash, pos),
  setForceStart: (infohash: string, force: boolean) => transport.invoke<void>('SetForceStart', infohash, force),
  listScheduleRules: () => transport.invoke<ScheduleRuleDTO[]>('ListScheduleRules'),
  createScheduleRule: (r: ScheduleRuleDTO) => transport.invoke<number>('CreateScheduleRule', r),
  updateScheduleRule: (r: ScheduleRuleDTO) => transport.invoke<void>('UpdateScheduleRule', r),
  deleteScheduleRule: (id: number) => transport.invoke<void>('DeleteScheduleRule', id),
  getBlocklist: () => transport.invoke<BlocklistDTO>('GetBlocklist'),
  setBlocklistURL: (url: string, enabled: boolean) => transport.invoke<void>('SetBlocklistURL', url, enabled),
  refreshBlocklist: () => transport.invoke<void>('RefreshBlocklist'),
  listFeeds: () => transport.invoke<FeedDTO[]>('ListFeeds'),
  createFeed: (f: FeedDTO) => transport.invoke<number>('CreateFeed', f),
  updateFeed: (f: FeedDTO) => transport.invoke<void>('UpdateFeed', f),
  deleteFeed: (id: number) => transport.invoke<void>('DeleteFeed', id),
  listFiltersByFeed: (feedID: number) => transport.invoke<FilterDTO[]>('ListFiltersByFeed', feedID),
  createFilter: (f: FilterDTO) => transport.invoke<number>('CreateFilter', f),
  updateFilter: (f: FilterDTO) => transport.invoke<void>('UpdateFilter', f),
  deleteFilter: (id: number) => transport.invoke<void>('DeleteFilter', id),
  getWebConfig: () => transport.invoke<WebConfigDTO>('GetWebConfig'),
  setWebConfig: (c: WebConfigDTO) => transport.invoke<void>('SetWebConfig', c),
  setWebPassword: (plain: string) => transport.invoke<void>('SetWebPassword', plain),
  rotateAPIKey: () => transport.invoke<string>('RotateAPIKey'),
  appVersion: () => transport.invoke<string>('AppVersion'),
  getUpdaterConfig: () => transport.invoke<UpdaterConfigDTO>('GetUpdaterConfig'),
  setUpdaterConfig: (c: UpdaterConfigDTO) => transport.invoke<void>('SetUpdaterConfig', c),
  checkForUpdate: () => transport.invoke<UpdateInfoDTO>('CheckForUpdate'),
  installUpdate: () => transport.invoke<void>('InstallUpdate'),
  openFolder: (path: string) => transport.invoke<void>('OpenFolder', path),
  login: (username: string, password: string) => transport.invoke<void>('Login', username, password),
  logout: () => transport.invoke<void>('Logout'),
};

export function onTorrentsTick(handler: (rows: Torrent[]) => void): () => void {
  return transport.on('torrents:tick', handler);
}

export function onStatsTick(handler: (stats: GlobalStatsT) => void): () => void {
  return transport.on('stats:tick', handler);
}

export function onInspectorTick(handler: (detail: DetailDTO) => void): () => void {
  return transport.on('inspector:tick', handler);
}

export function onUpdateAvailable(handler: (info: UpdateInfoDTO) => void): () => void {
  return transport.on('update:available', handler);
}
