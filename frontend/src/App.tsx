import {createMemo, createSignal, onCleanup, onMount} from 'solid-js';
import {Toaster, toast} from 'solid-sonner';
import {createTorrentsStore, filterTorrents} from './lib/store';
import {ThemeProvider} from './components/theme/ThemeProvider';
import {WindowShell} from './components/shell/WindowShell';
import {AddMagnetModal} from './components/shell/AddMagnetModal';
import {TorrentList} from './components/list/TorrentList';
import {Inspector} from './components/inspector/Inspector';
import './index.css';

export default function App() {
  const store = createTorrentsStore();
  const [magnetModal, setMagnetModal] = createSignal(false);
  onCleanup(() => store.dispose());

  const filtered = createMemo(() =>
    filterTorrents(
      store.state.torrents,
      store.state.statusFilter,
      store.state.searchQuery,
      store.state.selectedCategoryID,
      store.state.selectedTagID,
    )
  );

  const handleSelect = (id: string, e: MouseEvent) => {
    if (e.metaKey || e.ctrlKey) store.toggleSelect(id);
    else if (e.shiftKey) store.extendSelectTo(id);
    else {
      store.select(id);
      store.openInspector(id);
    }
  };

  const handleAddTorrent = async () => {
    try {
      const id = await store.pickAndAddTorrent();
      if (id) toast.success('Torrent added');
    } catch (err) { toast.error(String(err)); }
  };

  const handleMagnetDropped = async (m: string) => {
    await store.addMagnet(m);
    toast.success('Magnet added');
  };

  // Global keyboard shortcuts
  onMount(() => {
    const handler = (e: KeyboardEvent) => {
      if (e.target instanceof HTMLInputElement || e.target instanceof HTMLTextAreaElement) return;
      if ((e.metaKey || e.ctrlKey) && e.key === 'a') {
        e.preventDefault();
        store.selectAll();
      } else if (e.key === 'Escape') {
        if (store.state.inspectorOpenId) store.closeInspector();
        else store.clearSelection();
      } else if (e.key === ' ') {
        e.preventDefault();
        for (const id of store.state.selection) {
          const t = store.state.torrents.find((x) => x.id === id);
          if (!t) continue;
          if (t.paused) store.resume(id); else store.pause(id);
        }
      } else if (e.key === 'Delete' || e.key === 'Backspace') {
        if (store.state.selection.size === 0) return;
        e.preventDefault();
        for (const id of store.state.selection) store.remove(id, false);
        store.clearSelection();
      }
    };
    window.addEventListener('keydown', handler);
    onCleanup(() => window.removeEventListener('keydown', handler));
  });

  return (
    <ThemeProvider>
      <WindowShell
        torrents={store.state.torrents}
        filteredTorrents={filtered()}
        stats={store.state.stats}
        density={store.state.density}
        statusFilter={store.state.statusFilter}
        searchQuery={store.state.searchQuery}
        categories={store.state.categories}
        tags={store.state.tags}
        selectedCategoryID={store.state.selectedCategoryID}
        selectedTagID={store.state.selectedTagID}
        onDensityChange={store.setDensity}
        onStatusFilter={store.setStatusFilter}
        onSearchQuery={store.setSearchQuery}
        onSelectCategory={store.setSelectedCategory}
        onSelectTag={store.setSelectedTag}
        onAddMagnet={() => setMagnetModal(true)}
        onAddTorrent={handleAddTorrent}
        onMagnetDropped={handleMagnetDropped}
        inspector={
          <Inspector
            open={store.state.inspectorOpenId !== null}
            detail={store.state.inspectorDetail}
            tab={store.state.inspectorTab}
            bandwidth={store.state.bandwidthRing}
            onTabChange={(t) => store.setInspectorTab(t)}
            onClose={() => store.closeInspector()}
          />
        }
      >
        <TorrentList
          torrents={filtered()}
          density={store.state.density}
          selection={store.state.selection}
          categories={store.state.categories}
          tags={store.state.tags}
          onSelect={handleSelect}
          onPause={(id) => store.pause(id)}
          onResume={(id) => store.resume(id)}
          onRemove={(id) => { store.remove(id, false); toast('Removed'); }}
          onSetCategory={async (id, categoryID) => {
            try {
              await store.setTorrentCategory(id, categoryID);
            } catch (err) { toast.error(String(err)); }
          }}
          onToggleTag={async (id, tagID) => {
            const t = store.state.torrents.find((x) => x.id === id);
            if (!t) return;
            try {
              if (t.tags.some((tg) => tg.id === tagID)) {
                await store.unassignTag(id, tagID);
              } else {
                await store.assignTag(id, tagID);
              }
            } catch (err) { toast.error(String(err)); }
          }}
        />
      </WindowShell>
      <AddMagnetModal
        open={magnetModal()}
        onClose={() => setMagnetModal(false)}
        onSubmit={async (m) => {
          await store.addMagnet(m);
          toast.success('Magnet added');
        }}
      />
      <Toaster
        position="bottom-right"
        toastOptions={{
          style: {
            background: 'rgba(24, 24, 27, 0.95)',
            border: '1px solid rgba(255,255,255,0.1)',
            color: '#e7e7e9',
            'backdrop-filter': 'blur(12px)',
          },
        }}
      />
    </ThemeProvider>
  );
}
