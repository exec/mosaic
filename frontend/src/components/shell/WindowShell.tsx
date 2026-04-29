import {type JSX} from 'solid-js';
import type {Density, StatusFilter} from '../../lib/store';
import type {CategoryDTO, GlobalStatsT, TagDTO, Torrent} from '../../lib/bindings';
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
  children: JSX.Element; // the main pane (TorrentList)
  inspector?: JSX.Element;
};

export function WindowShell(props: Props) {
  return (
    <div class="flex h-full">
      <IconRail />
      <div class="flex flex-1 min-w-0 flex-col">
        <div class="flex flex-1 min-h-0">
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
          <main class="flex flex-1 min-w-0 flex-col">
            <TopToolbar
              searchQuery={props.searchQuery}
              onSearch={props.onSearchQuery}
              onAddMagnet={props.onAddMagnet}
              onAddTorrent={props.onAddTorrent}
              density={props.density}
              onDensityChange={props.onDensityChange}
            />
            <DropZone onMagnet={props.onMagnetDropped} onTorrentBytes={props.onTorrentBytesDropped}>
              <div class="h-full overflow-auto">
                {props.children}
              </div>
            </DropZone>
          </main>
          {props.inspector}
        </div>
        <StatusBar stats={props.stats} />
      </div>
    </div>
  );
}
