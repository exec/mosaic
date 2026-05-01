import {Match, Show, Switch, type JSX} from 'solid-js';
import type {AppView, Density, StatusFilter} from '../../lib/store';
import type {CategoryDTO, GlobalStatsT, TagDTO, Torrent, WebConfigDTO} from '../../lib/bindings';
import type {SettingsPane} from '../settings/SettingsSidebar';
import {IconRail} from './IconRail';
import {FilterRail} from './FilterRail';
import {TopToolbar} from './TopToolbar';
import {StatusBar} from './StatusBar';
import {DropZone} from './DropZone';
import {WindowControls} from './WindowControls';

type Props = {
  isWindows: boolean;
  view: AppView;
  settingsPane: SettingsPane;
  onNavigate: (v: AppView) => void;
  onNavigateRSS: () => void;
  onNavigateSchedule: () => void;
  onNavigateAbout: () => void;
  torrents: Torrent[];
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
  queuedCount: number;
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
          left of WindowControls on Windows. */}
      <div class="flex h-7 shrink-0">
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
        <Show when={props.isWindows}>
          <WindowControls />
        </Show>
      </div>
      <div class="flex flex-1 min-h-0">
        <IconRail
          view={props.view}
          settingsPane={props.settingsPane}
          onNavigate={props.onNavigate}
          onNavigateRSS={props.onNavigateRSS}
          onNavigateSchedule={props.onNavigateSchedule}
          onNavigateAbout={props.onNavigateAbout}
        />
        <div class="flex flex-1 min-w-0 flex-col">
          <div class="flex flex-1 min-h-0">
          <Show when={props.view === 'torrents'}>
            <FilterRail
              torrents={props.torrents}
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
          queuedCount={props.queuedCount}
          webConfig={props.webConfig}
          onClickWeb={props.onNavigateWebSettings}
        />
        </div>
      </div>
    </div>
  );
}
