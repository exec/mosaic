import {createMemo, createSignal, onCleanup, onMount} from 'solid-js';
import {Toaster, toast} from 'solid-sonner';
import {createTorrentsStore, filterTorrents} from './lib/store';
import {api} from './lib/bindings';
import {isWailsRuntime} from './lib/runtime';
import {ThemeProvider} from './components/theme/ThemeProvider';
import {BrowserAuthGate} from './components/auth/BrowserAuthGate';
import {WindowShell} from './components/shell/WindowShell';
import {AddTorrentModal} from './components/shell/AddTorrentModal';
import {UpdateToast} from './components/shell/UpdateToast';
import {TorrentList} from './components/list/TorrentList';
import {Inspector} from './components/inspector/Inspector';
import {SettingsRoute} from './components/settings/SettingsRoute';
import './index.css';

export default function App() {
  if (isWailsRuntime()) {
    return <ThemeProvider><Toaster position="bottom-right" toastOptions={{style: {background: 'rgba(24,24,27,0.95)', border: '1px solid rgba(255,255,255,0.1)', color: '#e7e7e9', 'backdrop-filter': 'blur(12px)'}}} /><AuthenticatedApp /></ThemeProvider>;
  }
  return (
    <ThemeProvider>
      <Toaster position="bottom-right" toastOptions={{style: {background: 'rgba(24,24,27,0.95)', border: '1px solid rgba(255,255,255,0.1)', color: '#e7e7e9', 'backdrop-filter': 'blur(12px)'}}} />
      <BrowserAuthGate>
        <AuthenticatedApp />
      </BrowserAuthGate>
    </ThemeProvider>
  );
}

function AuthenticatedApp() {
  const store = createTorrentsStore();
  const [addModalOpen, setAddModalOpen] = createSignal(false);
  const [addModalSource, setAddModalSource] = createSignal<'magnet' | 'file'>('magnet');
  const [platform, setPlatform] = createSignal('');
  onCleanup(() => store.dispose());

  // Decide whether to render Win11-style custom controls. Browser mode and
  // macOS keep their native (or hidden-inset) titlebar — only Wails+Windows
  // is frameless and needs us to draw min/max/close.
  onMount(async () => {
    if (!isWailsRuntime()) return;
    try { setPlatform(await api.platform()); } catch (err) { console.error(err); }
  });

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

  const queuedCount = createMemo(() => store.state.torrents.filter((t) => t.queued).length);

  const onMoveQueue = async (id: string, direction: 'top' | 'up' | 'down' | 'bottom') => {
    const sorted = [...store.state.torrents].sort((a, b) => a.queue_position - b.queue_position);
    const currentIdx = sorted.findIndex((t) => t.id === id);
    if (currentIdx < 0) return;
    let targetIdx: number;
    switch (direction) {
      case 'top':    targetIdx = 0; break;
      case 'bottom': targetIdx = sorted.length - 1; break;
      case 'up':     targetIdx = Math.max(0, currentIdx - 1); break;
      case 'down':   targetIdx = Math.min(sorted.length - 1, currentIdx + 1); break;
    }
    if (targetIdx === currentIdx) return;
    const moved = sorted.splice(currentIdx, 1)[0];
    sorted.splice(targetIdx, 0, moved);
    await Promise.all(sorted.map((t, i) => store.setQueuePosition(t.id, i)));
  };

  const onToggleForceStart = async (id: string, current: boolean) => {
    await store.setForceStart(id, !current);
  };

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
    <>
      <WindowShell
        isWindows={platform() === 'windows'}
        view={store.state.view}
        settingsPane={store.state.settingsPane}
        onNavigate={store.setView}
        onNavigateRSS={() => {
          store.setView('settings');
          store.setSettingsPane('rss');
        }}
        onNavigateSchedule={() => {
          store.setView('settings');
          store.setSettingsPane('schedule');
        }}
        onNavigateAbout={() => {
          store.setView('settings');
          store.setSettingsPane('about');
        }}
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
        altSpeedActive={store.state.limits.alt_active}
        onToggleAltSpeed={() => store.toggleAltSpeed()}
        queuedCount={queuedCount()}
        webConfig={store.state.webConfig}
        onNavigateWebSettings={() => {
          store.setView('settings');
          store.setSettingsPane('web');
        }}
        settings={
          <SettingsRoute
            pane={store.state.settingsPane}
            onPaneChange={store.setSettingsPane}
            defaultSavePath={store.state.defaultSavePath}
            categories={store.state.categories}
            tags={store.state.tags}
            limits={store.state.limits}
            queueLimits={store.state.queueLimits}
            scheduleRules={store.state.scheduleRules}
            blocklist={store.state.blocklist}
            feeds={store.state.feeds}
            filtersByFeed={store.state.filtersByFeed}
            webConfig={store.state.webConfig}
            updaterConfig={store.state.updaterConfig}
            updateInfo={store.state.updateInfo}
            appVersion={store.state.appVersion}
            onSetDefaultSavePath={(p) => store.setDefaultSavePath(p)}
            onSetWebConfig={(c) => store.setWebConfig(c)}
            onSetWebPassword={(p) => store.setWebPassword(p)}
            onRotateAPIKey={() => store.rotateAPIKey()}
            onSetUpdaterConfig={(c) => store.setUpdaterConfig(c)}
            onCheckForUpdate={() => store.checkForUpdate()}
            onInstallUpdate={() => store.installUpdate()}
            onSetLimits={(l) => store.setLimits(l)}
            onSetQueueLimits={(q) => store.setQueueLimits(q)}
            onCreateCategory={(name, sp, color) => store.createCategory(name, sp, color)}
            onUpdateCategory={(id, name, sp, color) => store.updateCategory(id, name, sp, color)}
            onDeleteCategory={(id) => store.deleteCategory(id)}
            onCreateTag={(name, color) => store.createTag(name, color)}
            onDeleteTag={(id) => store.deleteTag(id)}
            onCreateScheduleRule={(r) => store.createScheduleRule(r)}
            onUpdateScheduleRule={(r) => store.updateScheduleRule(r)}
            onDeleteScheduleRule={(id) => store.deleteScheduleRule(id)}
            onSetBlocklistURL={(url, en) => store.setBlocklistURL(url, en)}
            onRefreshBlocklist={() => store.refreshBlocklist()}
            onCreateFeed={(f) => store.createFeed(f)}
            onUpdateFeed={(f) => store.updateFeed(f)}
            onDeleteFeed={(id) => store.deleteFeed(id)}
            onLoadFiltersForFeed={(feedID) => store.refreshFiltersForFeed(feedID)}
            onCreateFilter={(f) => store.createFilter(f)}
            onUpdateFilter={(f) => store.updateFilter(f)}
            onDeleteFilter={(feedID, id) => store.deleteFilter(feedID, id)}
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
          onMoveQueue={onMoveQueue}
          onToggleForceStart={onToggleForceStart}
          onOpenFolder={async (savePath) => {
            try { await api.openFolder(savePath); }
            catch (err) { toast.error(String(err)); }
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
        onAddTorrentBytes={async (bytes, savePath, categoryID, tagIDs) => {
          const id = await store.addTorrentBytes(bytes, savePath);
          await applyOrganization(id, categoryID, tagIDs);
          toast.success('Torrent added');
        }}
      />
      <UpdateToast
        info={store.state.updateInfo}
        onInstall={() => { store.setView('settings'); store.setSettingsPane('updates'); }}
        onDismiss={() => {}}
      />
    </>
  );
}
