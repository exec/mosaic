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
  onNavigateWebSettings: () => void;
  children: JSX.Element; // the main pane (TorrentList)
  inspector?: JSX.Element;
  settings?: JSX.Element;
};

export function WindowShell(props: Props) {
  return (
    <div class="flex h-full flex-col">
      {/* Always-on top drag row. Wails's native drag uses the
          `--wails-draggable: drag` custom property; we also keep
          -webkit-app-region:drag for WKWebView's title-bar inset, plus an
          explicit onMouseDown that calls window.WailsInvoke('drag') —
          without the imperative path, focused-window drags get dropped on
          macOS because Wails's default `deferDragToMouseMove` flag waits
          for a follow-up mousemove that doesn't always arrive when the
          window is already key. Parley hit this on Tauri and solved it the
          same way. h-7 covers the traffic-lights inset on macOS and sits
          left of WindowControls on Windows + Linux. */}
      <div class="relative flex h-7 shrink-0">
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
        {/* Centered wordmark. Thin-tracked uppercase Inter with a silver
            gradient — luxury-brand aesthetic without competing with the
            content below. pointer-events:none and select-none so it stays
            invisible to drag / window-control clicks and text selection.
            The 0.5em right padding offsets letter-spacing's trailing gap
            so the M..C visually balances around the center axis. */}
        <div class="pointer-events-none absolute inset-0 flex items-center justify-center">
          <span
            class="select-none bg-gradient-to-b from-zinc-200 to-zinc-500 bg-clip-text text-[11px] font-extralight uppercase text-transparent"
            style={{'letter-spacing': '0.5em', 'padding-left': '0.5em'}}
          >
            Mosaic
          </span>
        </div>
      </div>
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
            />
          </Show>
          <main class="flex flex-1 min-w-0 flex-col">
            <Switch>
              <Match when={props.view === 'torrents'}>
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
          onClickWeb={props.onNavigateWebSettings}
        />
        </div>
      </div>
    </div>
  );
}
