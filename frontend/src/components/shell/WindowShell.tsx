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

export function WindowShell(props: Props) {
  return (
    // `relative` anchors the absolutely-positioned drag overlay below.
    // The body row fills the entire window now so sidebars paint
    // edge-to-edge top → bottom; the drag bar sits over the top h-7
    // strip via z-index instead of stealing layout space.
    <div class="relative flex h-full flex-col">
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
                    <div class="h-full overflow-auto">
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

      {/* Drag overlay — absolute, z-20, h-7. Sits ON TOP of sidebars and
          main content so the window's top edge is always draggable
          regardless of what's painted below. Wails's native drag uses the
          `--wails-draggable` custom property; we also keep
          -webkit-app-region:drag for WKWebView's title-bar inset, plus an
          explicit onMouseDown that calls window.WailsInvoke('drag') —
          without the imperative path, focused-window drags get dropped on
          macOS because Wails's default `deferDragToMouseMove` flag waits
          for a follow-up mousemove that doesn't always arrive when the
          window is already key. Parley hit this on Tauri and solved it the
          same way. h-7 covers the traffic-lights inset on macOS and hosts
          WindowControls on Windows + Linux. */}
      <div class="absolute inset-x-0 top-0 z-20 flex h-7">
        <div
          class="flex-1"
          style={{
            '--wails-draggable': 'drag',
            '-webkit-app-region': 'drag',
          }}
          onMouseDown={(e) => {
            if (e.button !== 0) return;
            try {
              (window as any).WailsInvoke?.('drag');
            } catch {
              // browser mode or non-Wails host — no-op
            }
          }}
        />
        <Show when={props.frameless}>
          <WindowControls />
        </Show>
        {/* Centered wordmark — see hidden-on-macOS reasoning below. */}
        <Show when={props.frameless}>
          <div class="pointer-events-none absolute inset-0 flex items-center justify-center">
            <span
              class="select-none bg-gradient-to-b from-zinc-200 to-zinc-500 bg-clip-text text-[11px] font-light uppercase text-transparent"
              style={{
                'letter-spacing': '0.42em',
                'padding-left': '0.42em',
                filter: 'drop-shadow(0 0 8px rgba(255,255,255,0.05))',
              }}
            >
              Mosaic
            </span>
          </div>
        </Show>
      </div>
    </div>
  );
}
