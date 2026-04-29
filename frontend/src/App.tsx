import {createMemo, createSignal, onCleanup, onMount} from 'solid-js';
import {Toaster, toast} from 'solid-sonner';
import {createTorrentsStore, filterTorrents} from './lib/store';
import {ThemeProvider} from './components/theme/ThemeProvider';
import {WindowShell} from './components/shell/WindowShell';
import {AddTorrentModal} from './components/shell/AddTorrentModal';
import {TorrentList} from './components/list/TorrentList';
import {Inspector} from './components/inspector/Inspector';
import {SettingsRoute} from './components/settings/SettingsRoute';
import './index.css';

export default function App() {
  const store = createTorrentsStore();
  const [addModalOpen, setAddModalOpen] = createSignal(false);
  const [addModalSource, setAddModalSource] = createSignal<'magnet' | 'file'>('magnet');
  onCleanup(() => store.dispose());

  const applyOrganization = async (id: string, categoryID: number | null, tagIDs: number[]) => {
    if (categoryID !== null) {
      try { await store.setTorrentCategory(id, categoryID); } catch (err) { console.error(err); }
    }
    for (const tagID of tagIDs) {
      try { await store.assignTag(id, tagID); } catch (err) { console.error(err); }
    }
  };

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

  const handleAddTorrent = () => {
    setAddModalSource('file');
    setAddModalOpen(true);
  };

  const handleAddMagnet = () => {
    setAddModalSource('magnet');
    setAddModalOpen(true);
  };

  const handleMagnetDropped = async (m: string) => {
    await store.addMagnet(m, '');
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
        view={store.state.view}
        onNavigate={store.setView}
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
        onAddMagnet={handleAddMagnet}
        onAddTorrent={handleAddTorrent}
        onMagnetDropped={handleMagnetDropped}
        onTorrentBytesDropped={async (bytes) => {
          try {
            await store.addTorrentBytes(bytes, '');
            toast.success('Torrent added');
          } catch (err) { toast.error(String(err)); }
        }}
        settings={
          <SettingsRoute
            defaultSavePath={store.state.defaultSavePath}
            categories={store.state.categories}
            tags={store.state.tags}
            onSetDefaultSavePath={(p) => store.setDefaultSavePath(p)}
            onCreateCategory={(name, sp, color) => store.createCategory(name, sp, color)}
            onUpdateCategory={(id, name, sp, color) => store.updateCategory(id, name, sp, color)}
            onDeleteCategory={(id) => store.deleteCategory(id)}
            onCreateTag={(name, color) => store.createTag(name, color)}
            onDeleteTag={(id) => store.deleteTag(id)}
          />
        }
        inspector={
          <Inspector
            open={store.state.inspectorOpenId !== null}
            detail={store.state.inspectorDetail}
            tab={store.state.inspectorTab}
            bandwidth={store.state.bandwidthRing}
            onTabChange={(t) => store.setInspectorTab(t)}
            onClose={() => store.closeInspector()}
            onSetFilePriority={async (index, priority) => {
              const id = store.state.inspectorOpenId;
              if (!id) return;
              try {
                await store.setFilePriorities(id, {[index]: priority});
              } catch (err) { toast.error(String(err)); }
            }}
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
      <AddTorrentModal
        open={addModalOpen()}
        initialSource={addModalSource()}
        defaultSavePath={store.state.defaultSavePath}
        categories={store.state.categories}
        tags={store.state.tags}
        onClose={() => setAddModalOpen(false)}
        onSubmitMagnet={async (m, savePath, categoryID, tagIDs) => {
          const id = await store.addMagnet(m, savePath);
          await applyOrganization(id, categoryID, tagIDs);
          toast.success('Magnet added');
        }}
        onPickAndAddTorrent={async (savePath, categoryID, tagIDs) => {
          const id = await store.pickAndAddTorrent(savePath);
          if (!id) return; // user cancelled
          await applyOrganization(id, categoryID, tagIDs);
          toast.success('Torrent added');
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
