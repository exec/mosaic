import {AddMagnet, ListTorrents, Pause, PickAndAddTorrent, Remove, Resume} from '../../wailsjs/go/main/App';
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

export const api = {
  addMagnet: (magnet: string) => AddMagnet(magnet),
  pickAndAddTorrent: () => PickAndAddTorrent(),
  listTorrents: () => ListTorrents() as Promise<Torrent[]>,
  pause: (id: string) => Pause(id),
  resume: (id: string) => Resume(id),
  remove: (id: string, deleteFiles: boolean) => Remove(id, deleteFiles),
};

export function onTorrentsTick(handler: (rows: Torrent[]) => void): () => void {
  return EventsOn('torrents:tick', handler);
}
