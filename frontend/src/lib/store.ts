import {createStore, produce} from 'solid-js/store';
import {api, onTorrentsTick, Torrent} from './bindings';

export type TorrentsStore = {
  torrents: Torrent[];
  loading: boolean;
  error: string | null;
};

export function createTorrentsStore() {
  const [state, setState] = createStore<TorrentsStore>({
    torrents: [],
    loading: true,
    error: null,
  });

  // initial load
  api.listTorrents()
    .then((rows) => setState({torrents: rows, loading: false}))
    .catch((e) => setState({error: String(e), loading: false}));

  // live updates
  const off = onTorrentsTick((rows) => {
    setState(produce((s) => { s.torrents = rows; }));
  });

  return {
    state,
    addMagnet: (m: string) => api.addMagnet(m),
    pause: (id: string) => api.pause(id),
    resume: (id: string) => api.resume(id),
    remove: (id: string, deleteFiles: boolean) => api.remove(id, deleteFiles),
    dispose: () => off(),
  };
}
