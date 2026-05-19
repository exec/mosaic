import {Match, Show, Switch, For} from 'solid-js';
import {createVirtualizer} from '@tanstack/solid-virtual';
import {toast} from 'solid-sonner';
import type {CategoryDTO, TagDTO, Torrent} from '../../lib/bindings';
import type {Density} from '../../lib/store';
import {TorrentCard} from './TorrentCard';
import {TorrentTable} from './TorrentTable';
import {EmptyState} from './EmptyState';
import {TorrentRowMenu, type QueueDirection} from './TorrentRowMenu';

type Props = {
  torrents: Torrent[];
  density: Density;
  selection: Set<string>;
  categories: CategoryDTO[];
  tags: TagDTO[];
  onSelect: (id: string, e: MouseEvent) => void;
  onPause: (id: string) => void;
  onResume: (id: string) => void;
  onRecheck: (id: string) => void;
  onRemove: (id: string) => void;
  onOpenFolder: (savePath: string) => void;
  onSetCategory: (id: string, categoryID: number | null) => void;
  onToggleTag: (id: string, tagID: number) => void;
  onMoveQueue: (id: string, direction: QueueDirection) => void;
  onToggleForceStart: (id: string, current: boolean) => void;
  // onShare is provided only when the current user may share torrents.
  onShare?: (id: string) => void;
};

// Estimated card height incl. the 8px (gap-2) gap below it. Cards are a
// fixed three-row layout so this estimate is close; the virtualizer also
// measures each rendered card and corrects any drift.
const CARD_HEIGHT = 106;

// CardList virtualizes the cards layout: only cards in (and near) the
// viewport render into the DOM. The scroll container is this component's
// own element; cards are absolutely positioned inside a spacer sized to
// the full list height so the scrollbar is accurate.
function CardList(props: Props) {
  let scrollEl: HTMLDivElement | undefined;

  const virtualizer = createVirtualizer({
    get count() { return props.torrents.length; },
    getScrollElement: () => scrollEl ?? null,
    estimateSize: () => CARD_HEIGHT,
    overscan: 6,
  });

  return (
    <div class="h-full overflow-auto" ref={scrollEl}>
      {/* Spacer sized to the full list height; p-3 gives the list its
          gutter. Cards are absolutely positioned within the padding box
          (left-0/right-0 = inset by the padding), translated to their
          virtual offset. */}
      <div class="relative p-3" style={{height: `${virtualizer.getTotalSize() + 24}px`}}>
        <For each={virtualizer.getVirtualItems()}>
          {(vItem) => {
            const t = () => props.torrents[vItem.index];
            return (
              <Show when={t()}>
                <div
                  data-index={vItem.index}
                  ref={(el) => queueMicrotask(() => virtualizer.measureElement(el))}
                  class="absolute left-0 right-0"
                  style={{transform: `translateY(${vItem.start}px)`}}
                >
                  <TorrentRowMenu
                    torrent={t()}
                    categories={props.categories}
                    tags={props.tags}
                    onPause={() => props.onPause(t().id)}
                    onResume={() => props.onResume(t().id)}
                    onRecheck={() => props.onRecheck(t().id)}
                    onRemove={() => props.onRemove(t().id)}
                    onCopyMagnet={() => {
                      if (t().magnet) {
                        navigator.clipboard.writeText(t().magnet);
                        toast.success('Magnet copied');
                      }
                    }}
                    onOpenFolder={() => props.onOpenFolder(t().save_path)}
                    onSetCategory={(categoryID) => props.onSetCategory(t().id, categoryID)}
                    onToggleTag={(tagID) => props.onToggleTag(t().id, tagID)}
                    onMoveQueue={(direction) => props.onMoveQueue(t().id, direction)}
                    onToggleForceStart={() => props.onToggleForceStart(t().id, t().force_start)}
                    onShare={props.onShare ? () => props.onShare!(t().id) : undefined}
                  >
                    <TorrentCard
                      torrent={t()}
                      selected={props.selection.has(t().id)}
                      onSelect={(e) => props.onSelect(t().id, e)}
                      onPause={() => props.onPause(t().id)}
                      onResume={() => props.onResume(t().id)}
                      onRemove={() => props.onRemove(t().id)}
                    />
                  </TorrentRowMenu>
                </div>
              </Show>
            );
          }}
        </For>
      </div>
    </div>
  );
}

export function TorrentList(props: Props) {
  return (
    <Show when={props.torrents.length > 0} fallback={<EmptyState />}>
      <Switch>
        <Match when={props.density === 'cards'}>
          <CardList {...props} />
        </Match>
        <Match when={props.density === 'table'}>
          <TorrentTable
            torrents={props.torrents}
            selection={props.selection}
            onRowClick={props.onSelect}
          />
        </Match>
      </Switch>
    </Show>
  );
}
