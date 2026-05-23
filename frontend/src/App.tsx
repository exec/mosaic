import {createMemo, createSignal, lazy, onCleanup, onMount, Suspense} from 'solid-js';
import {Toaster, toast} from 'solid-sonner';
import {createTorrentsStore, filterTorrents, computeCounts} from './lib/store';
import {api, onLaunchNotice} from './lib/bindings';
import {transport} from './lib/transport';
import {isWailsRuntime} from './lib/runtime';
// Importing the appearance module here ensures the persisted theme is applied
// to <html data-theme> before the first render — no need for a context provider.
import './lib/appearance';
import {BrowserAuthGate} from './components/auth/BrowserAuthGate';
import {WindowShell} from './components/shell/WindowShell';
import {GnomeTrayPrompt} from './components/shell/GnomeTrayPrompt';
import {AddTorrentModal} from './components/shell/AddTorrentModal';
import {ShareTorrentModal} from './components/shell/ShareTorrentModal';
import {UpdateToast} from './components/shell/UpdateToast';
import {TorrentList} from './components/list/TorrentList';
import {canShare} from './lib/permissions';
import {userErr} from './lib/errors';
import {Inspector} from './components/inspector/Inspector';
import './index.css';

// The settings panes (RSS, schedule, users, blocklist, about, …) are only
// reached when the user opens Settings — lazy-load the whole route so its
// bundle stays off the initial torrents-view load.
const SettingsRoute = lazy(() => import('./components/settings/SettingsRoute').then((m) => ({default: m.SettingsRoute})));

export default function App() {
  if (isWailsRuntime()) {
    return (
      <>
        <Toaster position="bottom-right" toastOptions={{style: {background: 'rgba(24,24,27,0.95)', border: '1px solid rgba(255,255,255,0.1)', color: '#e7e7e9', 'backdrop-filter': 'blur(12px)'}}} />
        <AuthenticatedApp />
      </>
    );
  }
  return (
    <>
      <Toaster position="bottom-right" toastOptions={{style: {background: 'rgba(24,24,27,0.95)', border: '1px solid rgba(255,255,255,0.1)', color: '#e7e7e9', 'backdrop-filter': 'blur(12px)'}}} />
      <BrowserAuthGate>
        <AuthenticatedApp />
      </BrowserAuthGate>
    </>
  );
}

function AuthenticatedApp() {
  const store = createTorrentsStore();
  const [addModalOpen, setAddModalOpen] = createSignal(false);
  const [addModalSource, setAddModalSource] = createSignal<'magnet' | 'file'>('magnet');
  const [platform, setPlatform] = createSignal('');
  // shareId holds the torrent currently open in the Share dialog (null = closed).
  const [shareId, setShareId] = createSignal<string | null>(null);
  onCleanup(() => store.dispose());

  // Decide whether to render Win11-style custom controls. Browser mode and
  // macOS keep their native (or hidden-inset) titlebar — only Wails+Windows
  // is frameless and needs us to draw min/max/close.
  onMount(async () => {
    if (!isWailsRuntime()) return;
    try { setPlatform(await api.platform()); } catch (err) { console.error(err); }
  });

  // Surface launch-arg outcomes (magnet click in browser → OS launches Mosaic
  // with the URL; double-click .torrent in Explorer/Finder → OS launches
  // Mosaic with the path) as toasts so the user sees feedback even if the
  // torrents:tick hasn't refreshed yet.
  const offLaunch = onLaunchNotice((n) => {
    switch (n.event) {
      case 'magnet_added':  toast.success('Magnet added'); break;
      case 'torrent_added': toast.success('Torrent added'); break;
      case 'magnet_error':  toast.error(`Couldn't add magnet — ${n.error}`); break;
      case 'torrent_error': toast.error(`Couldn't add torrent — ${n.error}`); break;
      // 'received' is debug-only; don't toast it.
    }
  });
  onCleanup(() => offLaunch());

  // The backend tray menu's "Settings…" item emits this event. Switch to the
  // settings view; we land on the Desktop pane since the tray is the most
  // likely entry point. Backend may also re-show the window — that's its job.
  const offNavSettings = transport.on('navigate:settings', () => {
    store.setView('settings');
    store.setSettingsPane('desktop');
  });
  onCleanup(() => offNavSettings());

  const applyOrganization = async (id: string, categoryID: number | null, tagIDs: number[]) => {
    const failures: string[] = [];
    if (categoryID !== null) {
      try { await store.setTorrentCategory(id, categoryID); }
      catch (err) { console.error(err); failures.push(`category: ${String(err)}`); }
    }
    for (const tagID of tagIDs) {
      try { await store.assignTag(id, tagID); }
      catch (err) { console.error(err); failures.push(`tag #${tagID}: ${String(err)}`); }
    }
    if (failures.length > 0) {
      toast.error(`Couldn't apply ${failures.length} ${failures.length === 1 ? 'rule' : 'rules'}: ${failures.join('; ')}`);
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

  // All badge tallies (5 status + per-category + per-tag + queued) computed
  // in one pass over the torrent list per tick, instead of one .filter()
  // scan per badge in FilterRail / StatusBar.
  const counts = createMemo(() => computeCounts(store.state.torrents));

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
    try {
      await Promise.all(sorted.map((t, i) => store.setQueuePosition(t.id, i)));
    } catch (err) {
      toast.error(`Couldn't reorder — ${userErr(err)}`);
    }
  };

  const onToggleForceStart = async (id: string, current: boolean) => {
    try {
      await store.setForceStart(id, !current);
    } catch (err) {
      toast.error(`Force-start failed — ${userErr(err)}`);
    }
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
        // Pause/resume the entire selection. Aggregate any failures into a
        // single toast so partial-success isn't invisible.
        const failures: string[] = [];
        Promise.all([...store.state.selection].map(async (id) => {
          const t = store.state.torrents.find((x) => x.id === id);
          if (!t) return;
          try {
            await (t.paused ? store.resume(id) : store.pause(id));
          } catch (err) {
            failures.push(`${t.name}: ${String(err)}`);
          }
        })).then(() => {
          if (failures.length > 0) {
            toast.error(`${failures.length} failed: ${failures.slice(0, 3).join('; ')}${failures.length > 3 ? '…' : ''}`);
          }
        });
      } else if (e.key === 'Delete' || e.key === 'Backspace') {
        if (store.state.selection.size === 0) return;
        e.preventDefault();
        const failures: string[] = [];
        Promise.all([...store.state.selection].map(async (id) => {
          try { await store.remove(id, false); }
          catch (err) { failures.push(`${id.slice(0, 8)}: ${String(err)}`); }
        })).then(() => {
          if (failures.length > 0) {
            toast.error(`${failures.length} remove failed: ${failures.slice(0, 3).join('; ')}${failures.length > 3 ? '…' : ''}`);
          }
        });
        store.clearSelection();
      }
    };
    window.addEventListener('keydown', handler);
    onCleanup(() => window.removeEventListener('keydown', handler));
  });

  return (
    <>
      <WindowShell
        frameless={platform() === 'windows' || platform() === 'linux'}
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
        onLogout={
          store.state.currentUser
            ? async () => {
                try {
                  await api.logout();
                } catch (err) {
                  // Surface the error so a user who clicked Sign Out and the
                  // request silently failed (e.g. the cookie was already
                  // gone, but more importantly the server is unreachable
                  // and the click did nothing) isn't left wondering. The
                  // reload still runs — client-side session state drops on
                  // refresh regardless of the API response.
                  toast.error(`Sign-out call failed — ${userErr(err)}. Reloading anyway.`);
                }
                window.location.reload();
              }
            : undefined
        }
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
          } catch (err) { toast.error(`Couldn't add torrent — ${userErr(err)}`); }
        }}
        altSpeedActive={store.state.limits.alt_active}
        onToggleAltSpeed={() => store.toggleAltSpeed()}
        counts={counts()}
        webConfig={store.state.webConfig}
        onNavigateWebSettings={() => {
          store.setView('settings');
          store.setSettingsPane('web');
        }}
        settings={
          <Suspense fallback={<div class="p-6 text-sm text-zinc-500">Loading settings…</div>}>
          <SettingsRoute
            pane={store.state.settingsPane}
            onPaneChange={store.setSettingsPane}
            serverFlavor={store.state.serverFlavor}
            currentUser={store.state.currentUser}
            defaultSavePath={store.state.defaultSavePath}
            watchFolder={store.state.watchFolder}
            categories={store.state.categories}
            tags={store.state.tags}
            limits={store.state.limits}
            queueLimits={store.state.queueLimits}
            peerLimits={store.state.peerLimits}
            seedingDefaults={store.state.seedingDefaults}
            scheduleRules={store.state.scheduleRules}
            blocklist={store.state.blocklist}
            feeds={store.state.feeds}
            filtersByFeed={store.state.filtersByFeed}
            webConfig={store.state.webConfig}
            updaterConfig={store.state.updaterConfig}
            updateInfo={store.state.updateInfo}
            appVersion={store.state.appVersion}
            desktopIntegration={store.state.desktopIntegration}
            onSetDefaultSavePath={(p) => store.setDefaultSavePath(p)}
            onSetWatchFolder={(c) => store.setWatchFolder(c)}
            onSetWebConfig={(c) => store.setWebConfig(c)}
            onSetWebPassword={(p) => store.setWebPassword(p)}
            onRotateAPIKey={() => store.rotateAPIKey()}
            onSetUpdaterConfig={(c) => store.setUpdaterConfig(c)}
            onCheckForUpdate={() => store.checkForUpdate()}
            onInstallUpdate={() => store.installUpdate()}
            onSetDesktopIntegration={(d) => store.setDesktopIntegration(d)}
            onSetLimits={(l) => store.setLimits(l)}
            onSetQueueLimits={(q) => store.setQueueLimits(q)}
            onSetPeerLimits={(p) => store.setPeerLimits(p)}
            onSetSeedingDefaults={(d) => store.setSeedingDefaults(d)}
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
            onPollFeed={(id) => store.pollFeed(id)}
            onLoadFiltersForFeed={(feedID) => store.refreshFiltersForFeed(feedID)}
            onCreateFilter={(f) => store.createFilter(f)}
            onUpdateFilter={(f) => store.updateFilter(f)}
            onDeleteFilter={(feedID, id) => store.deleteFilter(feedID, id)}
          />
          </Suspense>
        }
        inspector={
          <Inspector
            open={store.state.inspectorOpenId !== null}
            detail={store.state.inspectorDetail}
            tab={store.state.inspectorTab}
            bandwidthRing={store.bandwidthRing}
            bandwidthTick={store.state.bandwidthTick}
            onTabChange={(t) => store.setInspectorTab(t)}
            onClose={() => store.closeInspector()}
            onSetFilePriority={async (index, priority) => {
              const id = store.state.inspectorOpenId;
              if (!id) return;
              try {
                await store.setFilePriorities(id, {[index]: priority});
              } catch (err) { toast.error(`Couldn't set file priority — ${userErr(err)}`); }
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
          onRecheck={async (id) => {
            try { await api.recheck(id); toast.success('Recheck started'); }
            catch (err) { toast.error(`Recheck failed — ${userErr(err)}`); }
          }}
          onRemove={(id) => { store.remove(id, false); toast.success('Torrent removed'); }}
          onSetCategory={async (id, categoryID) => {
            try {
              await store.setTorrentCategory(id, categoryID);
            } catch (err) { toast.error(`Couldn't set category — ${userErr(err)}`); }
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
            } catch (err) { toast.error(`Couldn't update tag — ${userErr(err)}`); }
          }}
          onMoveQueue={onMoveQueue}
          onToggleForceStart={onToggleForceStart}
          onOpenFolder={async (savePath) => {
            try { await api.openFolder(savePath); }
            catch (err) { toast.error(`Couldn't open folder — ${userErr(err)}`); }
          }}
          onShare={canShare(store.state.currentUser) ? (id) => setShareId(id) : undefined}
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
      <ShareTorrentModal
        open={shareId() !== null}
        torrentID={shareId()}
        torrentName={store.state.torrents.find((t) => t.id === shareId())?.name ?? ''}
        currentUserID={store.state.currentUser?.id ?? 0}
        onClose={() => setShareId(null)}
      />
      <UpdateToast
        info={store.state.updateInfo}
        onInstall={() => { store.setView('settings'); store.setSettingsPane('updates'); }}
        onDismiss={() => {}}
      />
      <GnomeTrayPrompt />
    </>
  );
}
