import {createSignal, onCleanup} from 'solid-js';
import {createTorrentsStore} from './lib/store';
import {TorrentList} from './components/TorrentList';
import {AddMagnetModal} from './components/AddMagnetModal';
import './index.css';

export default function App() {
  const store = createTorrentsStore();
  const [modalOpen, setModalOpen] = createSignal(false);
  onCleanup(() => store.dispose());

  return (
    <div class="h-full flex flex-col">
      <header class="flex items-center justify-between px-4 py-2 border-b border-zinc-800">
        <div class="font-semibold">Mosaic</div>
        <div class="flex gap-2">
          <button
            class="px-3 py-1.5 rounded border border-zinc-700 text-sm hover:bg-zinc-900"
            onClick={() => store.pickAndAddTorrent().catch(console.error)}
          >
            + .torrent
          </button>
          <button
            class="px-3 py-1.5 rounded bg-blue-600 text-sm"
            onClick={() => setModalOpen(true)}
          >
            + Add Magnet
          </button>
        </div>
      </header>
      <main class="flex-1 overflow-auto">
        <TorrentList
          torrents={store.state.torrents}
          onPause={(id) => store.pause(id)}
          onResume={(id) => store.resume(id)}
          onRemove={(id) => store.remove(id, false)}
        />
      </main>
      <AddMagnetModal
        open={modalOpen()}
        onClose={() => setModalOpen(false)}
        onSubmit={async (m) => { await store.addMagnet(m); }}
      />
    </div>
  );
}
