import {Match, Show, Switch, type JSX} from 'solid-js';
import type {AppView, Density, StatusFilter, TorrentCounts} from '../../lib/store';
import type {CategoryDTO, GlobalStatsT, ServerFlavor, TagDTO, Torrent, UserDTO, WebConfigDTO} from '../../lib/bindings';
import type {SettingsPane} from '../settings/SettingsSidebar';
import {IconRail} from './IconRail';
import {FilterRail} from './FilterRail';
import {TopToolbar} from './TopToolbar';
import {StatusBar} from './StatusBar';
import {DropZone} from './DropZone';
import {WindowControls} from './WindowControls';

type Props = {
  // True on platforms where we hide the OS title bar entirely and render
  // our own minimize/maximize/close controls (Windows + Linux). macOS
  // keeps its native traffic-lights via Wails's hidden-inset titlebar.
  frameless: boolean;
  view: AppView;
  settingsPane: SettingsPane;
  onNavigate: (v: AppView) => void;
  // Generic settings-pane navigator. Replaces the per-pane callbacks
  // (onNavigateRSS/Schedule/About) that existed back when the IconRail
  // only had three settings shortcuts hardcoded; the rail is now
  // user-customizable so it needs to jump to arbitrary panes.
  onNavigateSettingsPane: (p: SettingsPane) => void;
  flavor: ServerFlavor;
  currentUser: UserDTO | null;
  onLogout?: () => void;
  filteredTorrents: Torrent[];
  stats: GlobalStatsT;
  density: Density;
  statusFilter: StatusFilter;
  searchQuery: string;
  categories: CategoryDTO[];
  tags: TagDTO[];
  selectedCategoryID: number | null;
  selectedTagID: number | null;
  onDensityChange: (d: Density) => void;
  onStatusFilter: (s: StatusFilter) => void;
  onSearchQuery: (q: string) => void;
  onSelectCategory: (id: number | null) => void;
  onSelectTag: (id: number | null) => void;
  onAddMagnet: () => void;
  onAddTorrent: () => void;
  onMagnetDropped: (m: string) => Promise<void>;
  onTorrentBytesDropped: (bytes: Uint8Array) => Promise<void>;
  altSpeedActive: boolean;
  onToggleAltSpeed: () => void;
  // All torrent badge tallies, computed once per tick in App.tsx.
  counts: TorrentCounts;
  webConfig: WebConfigDTO;
  dhtEnabled: boolean;
  onNavigateWebSettings: () => void;
  children: JSX.Element; // the main pane (TorrentList)
  inspector?: JSX.Element;
  settings?: JSX.Element;
};

// Pixel distance the cursor must travel from a mousedown in the top drag
// strip before we treat the gesture as a window drag rather than a click.
// 4px is the OS-standard slop on macOS / Windows — anything smaller and
// users who twitch slightly while clicking a button would unintentionally
// drag the window; anything larger and the drag feels lazy.
const DRAG_MOTION_THRESHOLD_PX = 4;

// The top-of-window drag strip height. Mirrors h-7 (28px) on the overlay
// below — clicks beyond this Y are normal clicks, no drag handling.
const TOP_DRAG_STRIP_PX = 28;

export function WindowShell(props: Props) {
  // Motion-threshold drag handler. Attached to the WindowShell root so it
  // sees mousedowns anywhere in the top strip, INCLUDING on the search
  // input and buttons inside TopToolbar. A bare click without movement
  // proceeds normally (the click event fires on its real target); moving
  // the cursor past DRAG_MOTION_THRESHOLD_PX while still held flips the
  // gesture into a window drag via WailsInvoke('drag'). This means users
  // can grab the window from anywhere in the titlebar zone — buttons,
  // search bar, empty space — without sacrificing the click semantics on
  // those interactive elements.
  const onTopStripMouseDown = (e: MouseEvent) => {
    if (e.button !== 0) return;
    if (e.clientY >= TOP_DRAG_STRIP_PX) return;
    const startX = e.clientX;
    const startY = e.clientY;
    let dispatched = false;
    const onMove = (mEv: MouseEvent) => {
      if (dispatched) return;
      const dx = Math.abs(mEv.clientX - startX);
      const dy = Math.abs(mEv.clientY - startY);
      if (dx >= DRAG_MOTION_THRESHOLD_PX || dy >= DRAG_MOTION_THRESHOLD_PX) {
        dispatched = true;
        // Swallow the click that would otherwise fire on the eventual
        // mouseup so the underlying button / row doesn't activate. Click
        // bubbles AFTER mousemove, so registering here in capture phase
        // intercepts it before any onClick handler on the real target
        // gets to run. One-shot — removes itself after firing OR after
        // the next mousedown (in case the click never arrives, e.g. user
        // releases outside the window).
        const swallow = (cEv: MouseEvent) => {
          cEv.stopPropagation();
          cEv.preventDefault();
          document.removeEventListener('click', swallow, true);
          document.removeEventListener('mousedown', clearSwallow, true);
        };
        const clearSwallow = () => {
          document.removeEventListener('click', swallow, true);
          document.removeEventListener('mousedown', clearSwallow, true);
        };
        document.addEventListener('click', swallow, true);
        document.addEventListener('mousedown', clearSwallow, true);
        try { (window as any).WailsInvoke?.('drag'); } catch {}
        cleanup();
      }
    };
    const onUp = () => cleanup();
    const cleanup = () => {
      document.removeEventListener('mousemove', onMove);
      document.removeEventListener('mouseup', onUp);
    };
    document.addEventListener('mousemove', onMove);
    document.addEventListener('mouseup', onUp);
  };

  return (
    // `relative` anchors the absolutely-positioned drag overlay below.
    // The body row fills the entire window now so sidebars paint
    // edge-to-edge top → bottom; the drag bar sits over the top h-7
    // strip via z-index instead of stealing layout space.
    <div class="relative flex h-full flex-col" onMouseDown={onTopStripMouseDown}>
      <div class="flex flex-1 min-h-0">
        <IconRail
          view={props.view}
          settingsPane={props.settingsPane}
          onNavigate={props.onNavigate}
          onNavigateSettingsPane={props.onNavigateSettingsPane}
          flavor={props.flavor}
          currentUser={props.currentUser}
          onLogout={props.onLogout}
        />
        <div class="flex flex-1 min-w-0 flex-col">
          <div class="flex flex-1 min-h-0">
          <Show when={props.view === 'torrents'}>
            <FilterRail
              counts={props.counts}
              active={props.statusFilter}
              categories={props.categories}
              tags={props.tags}
              selectedCategoryID={props.selectedCategoryID}
              selectedTagID={props.selectedTagID}
              onSelect={props.onStatusFilter}
              onSelectCategory={props.onSelectCategory}
              onSelectTag={props.onSelectTag}
              onNavigateSettingsPane={props.onNavigateSettingsPane}
            />
          </Show>
          {/* Sidebars (IconRail, FilterRail, SettingsSidebar inside
              settings panes) already have their own pt-10 — their
              backgrounds paint to the top edge while interactive content
              sits below the drag strip. For non-sidebar content (torrent
              list, settings pane content) we add pt-7 just inside `<main>`
              so TopToolbar / pane headers don't get covered by the drag
              overlay. */}
          <main class="flex flex-1 min-w-0 flex-col">
            <Switch>
              <Match when={props.view === 'torrents'}>
                {/* TopToolbar extends to the very top — its background
                    paints up into the drag-overlay zone, matching how the
                    sidebars do it. Interactive children inside the toolbar
                    (search input, buttons) opt out of dragging via
                    -webkit-app-region: no-drag AND lift to z-30 so the
                    drag overlay (z-20) doesn't intercept their clicks. */}
                <div class="flex flex-1 min-h-0 flex-col">
                  <TopToolbar
                    searchQuery={props.searchQuery}
                    onSearch={props.onSearchQuery}
                    onAddMagnet={props.onAddMagnet}
                    onAddTorrent={props.onAddTorrent}
                    density={props.density}
                    onDensityChange={props.onDensityChange}
                    altSpeedActive={props.altSpeedActive}
                    onToggleAltSpeed={props.onToggleAltSpeed}
                  />
                  <DropZone onMagnet={props.onMagnetDropped} onTorrentBytes={props.onTorrentBytesDropped}>
                    {/* The table view owns its scrollport (TorrentTable's
                        h-full overflow-auto container, which its row
                        virtualizer and sticky header measure against), so
                        this wrapper must NOT scroll there — two nested
                        scrollports would defeat the virtualization. The
                        cards view (CardList) also scrolls itself; keep
                        overflow-auto here only as the fallback for the
                        non-virtualized empty state. */}
                    <div class={props.density === 'table' ? 'h-full overflow-hidden' : 'h-full overflow-auto'}>
                      {props.children}
                    </div>
                  </DropZone>
                </div>
              </Match>
              <Match when={props.view === 'settings'}>
                {props.settings}
              </Match>
            </Switch>
          </main>
          <Show when={props.view === 'torrents'}>{props.inspector}</Show>
        </div>
        <StatusBar
          stats={props.stats}
          queuedCount={props.counts.queued}
          webConfig={props.webConfig}
          dhtEnabled={props.dhtEnabled}
          onClickWeb={props.onNavigateWebSettings}
        />
        </div>
      </div>

      {/* Drag overlay — absolute, z-20, h-7. Just a visual frame for the
          WindowControls (Windows + Linux); the actual drag behavior is
          driven by the onTopStripMouseDown handler attached to the
          WindowShell root above, so users can grab the window from
          anywhere in the top 28px — including over the TopToolbar's
          search bar and buttons — without losing click semantics on
          those interactive elements.
          pointer-events: none here lets clicks pass through to whatever
          is underneath; WindowControls flips back to pointer-events-auto
          so min/max/close still receive clicks. */}
      <div class="pointer-events-none absolute inset-x-0 top-0 z-20 flex h-7">
        <div class="flex-1" />
        <Show when={props.frameless}>
          <div class="pointer-events-auto">
            <WindowControls />
          </div>
        </Show>
      </div>
    </div>
  );
}
