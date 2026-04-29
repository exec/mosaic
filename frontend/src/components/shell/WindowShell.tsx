import {type JSX} from 'solid-js';
import type {Density, StatusFilter} from '../../lib/store';
import type {GlobalStatsT, Torrent} from '../../lib/bindings';
import {IconRail} from './IconRail';
import {FilterRail} from './FilterRail';
import {TopToolbar} from './TopToolbar';
import {StatusBar} from './StatusBar';
import {DropZone} from './DropZone';

type Props = {
  torrents: Torrent[];
  filteredTorrents: Torrent[];
  stats: GlobalStatsT;
  density: Density;
  statusFilter: StatusFilter;
  searchQuery: string;
  onDensityChange: (d: Density) => void;
  onStatusFilter: (s: StatusFilter) => void;
  onSearchQuery: (q: string) => void;
  onAddMagnet: () => void;
  onAddTorrent: () => void;
  onMagnetDropped: (m: string) => Promise<void>;
  children: JSX.Element; // the main pane (TorrentList)
};

export function WindowShell(props: Props) {
  return (
    <div class="flex h-full flex-col">
      <div class="flex flex-1 min-h-0">
        <IconRail />
        <FilterRail
          torrents={props.torrents}
          active={props.statusFilter}
          onSelect={props.onStatusFilter}
        />
        <main class="flex flex-1 min-w-0 flex-col">
          <TopToolbar
            searchQuery={props.searchQuery}
            onSearch={props.onSearchQuery}
            onAddMagnet={props.onAddMagnet}
            onAddTorrent={props.onAddTorrent}
            density={props.density}
            onDensityChange={props.onDensityChange}
          />
          <DropZone onMagnet={props.onMagnetDropped}>
            <div class="h-full overflow-auto">
              {props.children}
            </div>
          </DropZone>
        </main>
      </div>
      <StatusBar stats={props.stats} />
    </div>
  );
}
