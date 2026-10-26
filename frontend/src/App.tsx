import {DAEMON_BUILD} from './lib/build';
import {createMemo, createSignal, lazy, onCleanup, onMount, Show, Suspense} from 'solid-js';
import {Toaster, toast} from 'solid-sonner';
import {createTorrentsStore, filterTorrents, computeCounts} from './lib/store';
import {api, onLaunchNotice, type UpdaterConfigDTO} from './lib/bindings';
import {transport} from './lib/transport';
import {isWailsRuntime} from './lib/runtime';
// Importing the appearance module here ensures the persisted theme is applied
// to <html data-theme> before the first render — no need for a context provider.
import './lib/appearance';
import {BrowserAuthGate} from './components/auth/BrowserAuthGate';
import {WindowShell} from './components/shell/WindowShell';
import {GnomeTrayPrompt} from './components/shell/GnomeTrayPrompt';
import {TransferStatisticsModal} from './components/shell/TransferStatisticsModal';
import {BulkActions} from './components/shell/BulkActions';
import {RemoveTorrentModal} from './components/shell/RemoveTorrentModal';
import {AddTorrentModal} from './components/shell/AddTorrentModal';
import {ShareTorrentModal} from './components/shell/ShareTorrentModal';
import {UpdateToast} from './components/shell/UpdateToast';
import {TorrentList} from './components/list/TorrentList';
import {canChangeSettings, canShare} from './lib/permissions';
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
  const desktopUpdaterProps = () => ({
    get updaterConfig() { return store.state.updaterConfig; },
    get updateInfo() { return store.state.updateInfo; },
    onSetUpdaterConfig: (c: UpdaterConfigDTO) => store.setUpdaterConfig(c),
    onCheckForUpdate: () => store.checkForUpdate(),
    onInstallUpdate: () => store.installUpdate(),
  });

  const [statisticsOpen, setStatisticsOpen] = createSignal(false);
  const [droppedFilename,setDroppedFilename]=createSignal('');
  let droppedResolve:((added:boolean)=>void)|undefined,dropSucceeded=false;
  const closeAddModal=()=>{setAddModalOpen(false);const resolve=droppedResolve;droppedResolve=undefined;resolve?.(dropSucceeded);};
  onCleanup(()=>{droppedResolve?.(false);droppedResolve=undefined;});
  const [addModalOpen, setAddModalOpen] = createSignal(false);
  const [addModalSource, setAddModalSource] = createSignal<'magnet' | 'file'>('magnet');
  const [droppedMagnet, setDroppedMagnet] = createSignal('');
  const [droppedBytes, setDroppedBytes] = createSignal<Uint8Array>();
  const [platform, setPlatform] = createSignal('');
  // shareId holds the torrent currently open in the Share dialog (null = closed).
  const [removeIDs, setRemoveIDs] = createSignal<string[]>([]);
  const removeTorrents = createMemo(() => store.state.torrents.filter((t) => removeIDs().includes(t.id)));
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

  const visibleSelection = createMemo(() => new Set(
    filtered().filter((t) => store.state.selection.has(t.id)).map((t) => t.id),
  ));
  const canEditInspector = () => {
    const torrent = store.state.torrents.find((t) => t.id === store.state.inspectorOpenId);
    return torrent?.access === 'owner' || torrent?.access === 'editor';
  };

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
      // The backend stores each torrent's position verbatim (no server-side
      // reshuffle), so every row whose index changed needs a write — but
      // rows already at their position (the common case: all but two for a
      // one-step move) don't need to be re-sent.
      await Promise.all(
        sorted.flatMap((t, i) => (t.queue_position === i ? [] : [store.setQueuePosition(t.id, i)])),
      );
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

  const handleSelect = (id: string, e: MouseEvent, visibleIds: string[]) => {
    if (e.metaKey || e.ctrlKey) store.toggleSelect(id);
    else if (e.shiftKey) store.extendSelectTo(id, visibleIds);
    else {
      store.select(id);
      store.openInspector(id);
    }
  };

  const handleAddTorrent = () => {
    setDroppedMagnet('');setDroppedBytes(undefined);
    setAddModalSource('file');
    setAddModalOpen(true);
  };

  const handleAddMagnet = () => {
    setDroppedMagnet('');setDroppedBytes(undefined);
    setAddModalSource('magnet');
    setAddModalOpen(true);
  };

  const handleMagnetDropped = async (m: string) => {
    setDroppedMagnet(m);setDroppedBytes(undefined);setAddModalSource('magnet');setAddModalOpen(true);
  };

  // Global keyboard shortcuts
  onMount(() => {
    const handler = (e: KeyboardEvent) => {
      if (e.defaultPrevented || e.isComposing || e.repeat || e.altKey || store.state.view !== 'torrents' || addModalOpen() || shareId() || removeIDs().length || statisticsOpen()) return;
      if (e.target instanceof Element && e.target.closest(
        'input, textarea, select, button, a[href], [contenteditable]:not([contenteditable="false"]), [role="button"], [role="switch"], [role="combobox"], [role="menu"], [role="menuitem"], [role="dialog"], [role="separator"]',
      )) return;
      if ((e.metaKey || e.ctrlKey) && e.key === 'a') {
        e.preventDefault();
        store.selectAll();
      } else if (e.metaKey || e.ctrlKey) {
        return;
      } else if (e.key === 'Escape') {
        if (store.state.inspectorOpenId) store.closeInspector();
        else store.clearSelection();
      } else if (e.key === ' ') {
        e.preventDefault();
        // Pause/resume the visible selection. Aggregate any failures into a
        // single toast so partial-success isn't invisible.
        const failures: string[] = [];
        Promise.all([...visibleSelection()].map(async (id) => {
          const t = store.state.torrents.find((x) => x.id === id);
          if (!t || (t.access !== 'owner' && t.access !== 'editor')) return;
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
        const ids = [...visibleSelection()];
        if (ids.length === 0) return;
        e.preventDefault();
        setRemoveIDs(ids);
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
        onNavigateSettingsPane={(p) => {
          store.setView('settings');
          store.setSettingsPane(p);
        }}
        flavor={store.state.serverFlavor}
        currentUser={store.state.currentUser}
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
        onOpenStatistics={() => setStatisticsOpen(true)}
        connection={store.state.connection}
        onRetryConnection={() => void store.refreshConnection()}
        selectedCount={visibleSelection().size}
        selectionToolbar={<BulkActions torrents={filtered().filter((t) => visibleSelection().has(t.id))} categories={store.state.categories} tags={store.state.tags} onClear={store.clearSelection} onRemove={() => setRemoveIDs([...visibleSelection()])} />}
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
        onTorrentBytesDropped={(bytes,name) => new Promise<boolean>(resolve=>{
          if(addModalOpen()){resolve(false);return;}
          droppedResolve=resolve;dropSucceeded=false;setDroppedFilename(name??'');
          setDroppedMagnet('');setDroppedBytes(bytes);setAddModalSource('file');setAddModalOpen(true);
        })}
        altSpeedActive={store.state.limits.alt_active}
        onToggleAltSpeed={() => store.toggleAltSpeed()}
        counts={counts()}
        webConfig={store.state.webConfig}
        dhtEnabled={store.state.peerLimits.dht_enabled}
        onNavigateConnectionSettings={()=>{store.setView('settings');store.setSettingsPane('connection');}}
        onNavigateWebSettings={() => {
          store.setView('settings');
          store.setSettingsPane('web');
        }}
        settings={
          <Suspense fallback={<div class="p-6 text-sm text-zinc-500">Loading settings…</div>}>
          <SettingsRoute
            {...(DAEMON_BUILD ? {} as ReturnType<typeof desktopUpdaterProps> : desktopUpdaterProps())}
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
            appVersion={store.state.appVersion}
            desktopIntegration={store.state.desktopIntegration}
            onSetDefaultSavePath={(p) => store.setDefaultSavePath(p)}
            onSetWatchFolder={(c) => store.setWatchFolder(c)}
            onSetWebConfig={(c) => store.setWebConfig(c)}
            onSetWebPassword={(p) => store.setWebPassword(p)}
            onRotateAPIKey={() => store.rotateAPIKey()}
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
            onGetFeedItems={(feedID) => api.getFeedItems(feedID)}
            onAddFeedItem={(url) => api.addFeedItem(url, '')}
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
            seedingDefaults={store.state.seedingDefaults}
            canEdit={canEditInspector()}
            canRelocate={store.state.torrents.find((t) => t.id === store.state.inspectorOpenId)?.access === 'owner'}
            canBlockPeers={canChangeSettings(store.state.currentUser)}
            detail={store.state.inspectorDetail}
            downloadRate={store.state.torrents.find((t) => t.id === store.state.inspectorOpenId)?.download_rate ?? 0}
            tab={store.state.inspectorTab}
            bandwidthRing={store.bandwidthRing}
            bandwidthTick={store.state.bandwidthTick}
            sequential={store.state.torrents.find((t) => t.id === store.state.inspectorOpenId)?.sequential ?? false}
            onTabChange={(t) => store.setInspectorTab(t)}
            onClose={() => store.closeInspector()}
            onSetFilePriority={async (index, priority) => {
              const id = store.state.inspectorOpenId;
              if (!id || !canEditInspector()) return;
              try {
                await store.setFilePriorities(id, {[index]: priority});
              } catch (err) { toast.error(`Couldn't set file priority — ${userErr(err)}`); }
            }}
            onToggleSequential={async () => {
              const id = store.state.inspectorOpenId;
              if (!id || !canEditInspector()) return;
              const t = store.state.torrents.find((x) => x.id === id);
              if (!t) return;
              try {
                await store.setSequential(id, !t.sequential);
              } catch (err) { toast.error(`Sequential download failed — ${userErr(err)}`); }
            }}
          />
        }
      >
        <TorrentList
          torrents={filtered()}
          density={store.state.density}
          selection={visibleSelection()}
          categories={store.state.categories}
          tags={store.state.tags}
          onSelect={handleSelect}
          onKeyboardSelect={(id,extend,ids)=>extend?store.extendSelectTo(id,ids):store.select(id)}
          onInspect={id=>store.openInspector(id)}
          onPause={(id) => store.pause(id)}
          onResume={(id) => store.resume(id)}
          onRecheck={async (id) => {
            try { await api.recheck(id); toast.success('Recheck started'); }
            catch (err) { toast.error(`Recheck failed — ${userErr(err)}`); }
          }}
          onRemove={(id) => setRemoveIDs([id])}
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
      <TransferStatisticsModal open={statisticsOpen()} onClose={() => setStatisticsOpen(false)} />
      <AddTorrentModal
        open={addModalOpen()}
        initialSource={addModalSource()}
        defaultSavePath={store.state.defaultSavePath}
        categories={store.state.categories}
        tags={store.state.tags}
        onClose={closeAddModal}
        initialMagnet={droppedMagnet()}
        initialBytes={droppedBytes()}
        initialFilename={droppedFilename()}
        onAdded={async (id, categoryID, tagIDs) => {
          await applyOrganization(id, categoryID, tagIDs);
          dropSucceeded=true;toast.success('Torrent added');
        }}
      />
      <RemoveTorrentModal torrents={removeTorrents()} onClose={() => setRemoveIDs([])} onRemove={async (deleteFiles) => {
        const selected = [...removeTorrents()];
        const results = await Promise.allSettled(selected.map((t) => store.remove(t.id, deleteFiles)));
        const failed = results.flatMap((r, i) => r.status === 'rejected' ? [{id: selected[i].id, reason: `${selected[i].name}: ${userErr(r.reason)}`}] : []);
        setRemoveIDs(failed.map((f) => f.id));
        if (failed.length) toast.error(`${failed.length} removal failed: ${failed.slice(0, 3).map((f) => f.reason).join('; ')}`);
        else {store.clearSelection();toast.success(`${selected.length} removed${deleteFiles ? ' with downloaded files' : ' · files kept'}`);}
      }} />
      <ShareTorrentModal
        open={shareId() !== null}
        torrentID={shareId()}
        torrentName={store.state.torrents.find((t) => t.id === shareId())?.name ?? ''}
        currentUserID={store.state.currentUser?.id ?? 0}
        onClose={() => setShareId(null)}
      />
      {!DAEMON_BUILD && <Show when={store.state.serverFlavor !== 'daemon'}><UpdateToast
        info={store.state.updateInfo}
        onInstall={() => { store.setView('settings'); store.setSettingsPane('updates'); }}
        onDismiss={() => {}}
      /></Show>}
      <GnomeTrayPrompt />
    </>
  );
}
