import {ContextMenu} from '../ui/ContextMenu';
import {Pause, Play, Trash2, Folder, Copy, RotateCw, Tag, ChevronRight, Check, ListOrdered} from 'lucide-solid';
import type {CategoryDTO, TagDTO, Torrent} from '../../lib/bindings';
import {For, type JSX, Show} from 'solid-js';

export type QueueDirection = 'top' | 'up' | 'down' | 'bottom';

type Props = {
  torrent: Torrent;
  categories: CategoryDTO[];
  tags: TagDTO[];
  onPause: () => void;
  onResume: () => void;
  onRemove: () => void;
  onCopyMagnet: () => void;
  onSetCategory: (id: number | null) => void;
  onToggleTag: (id: number) => void;
  onMoveQueue: (direction: QueueDirection) => void;
  onToggleForceStart: () => void;
  children: JSX.Element;
};

export function TorrentRowMenu(props: Props) {
  const hasTag = (id: number) => props.torrent.tags.some((t) => t.id === id);
  return (
    <ContextMenu trigger={props.children}>
      <Show
        when={!props.torrent.paused}
        fallback={
          <ContextMenu.Item onSelect={props.onResume}>
            <Play class="h-3.5 w-3.5" />
            Resume
          </ContextMenu.Item>
        }
      >
        <ContextMenu.Item onSelect={props.onPause}>
          <Pause class="h-3.5 w-3.5" />
          Pause
        </ContextMenu.Item>
      </Show>
      <ContextMenu.Item disabled>
        <RotateCw class="h-3.5 w-3.5" />
        Recheck
      </ContextMenu.Item>
      <ContextMenu.Separator />
      <ContextMenu.Sub>
        <ContextMenu.SubTrigger>
          <ListOrdered class="h-3.5 w-3.5" />
          Queue
          <ChevronRight class="ml-auto h-3 w-3" />
        </ContextMenu.SubTrigger>
        <ContextMenu.SubContent>
          <ContextMenu.Item onSelect={() => props.onMoveQueue('top')}>
            Move to top
          </ContextMenu.Item>
          <ContextMenu.Item onSelect={() => props.onMoveQueue('up')}>
            Move up
          </ContextMenu.Item>
          <ContextMenu.Item onSelect={() => props.onMoveQueue('down')}>
            Move down
          </ContextMenu.Item>
          <ContextMenu.Item onSelect={() => props.onMoveQueue('bottom')}>
            Move to bottom
          </ContextMenu.Item>
          <ContextMenu.Separator />
          <ContextMenu.Item onSelect={() => props.onToggleForceStart()}>
            <Show when={props.torrent.force_start} fallback={<>Force-start</>}>
              <Check class="h-3.5 w-3.5" />
              Force-start (active)
            </Show>
          </ContextMenu.Item>
        </ContextMenu.SubContent>
      </ContextMenu.Sub>
      <ContextMenu.Sub>
        <ContextMenu.SubTrigger>
          <Folder class="h-3.5 w-3.5" />
          Category
          <ChevronRight class="ml-auto h-3 w-3" />
        </ContextMenu.SubTrigger>
        <ContextMenu.SubContent>
          <ContextMenu.Item onSelect={() => props.onSetCategory(null)}>
            <span class="text-zinc-500">None</span>
          </ContextMenu.Item>
          <Show when={props.categories.length > 0}>
            <ContextMenu.Separator />
          </Show>
          <For each={props.categories}>
            {(cat) => (
              <ContextMenu.Item onSelect={() => props.onSetCategory(cat.id)}>
                <span class="h-2 w-2 rounded-full" style={{background: cat.color}} />
                {cat.name}
                <Show when={props.torrent.category_id === cat.id}>
                  <Check class="ml-auto h-3 w-3" />
                </Show>
              </ContextMenu.Item>
            )}
          </For>
        </ContextMenu.SubContent>
      </ContextMenu.Sub>
      <ContextMenu.Sub>
        <ContextMenu.SubTrigger>
          <Tag class="h-3.5 w-3.5" />
          Tags
          <ChevronRight class="ml-auto h-3 w-3" />
        </ContextMenu.SubTrigger>
        <ContextMenu.SubContent>
          <Show
            when={props.tags.length > 0}
            fallback={
              <ContextMenu.Item disabled>
                <span class="text-zinc-500">No tags yet</span>
              </ContextMenu.Item>
            }
          >
            <For each={props.tags}>
              {(tg) => (
                <ContextMenu.Item onSelect={() => props.onToggleTag(tg.id)}>
                  <span class="h-2 w-2 rounded-full" style={{background: tg.color}} />
                  {tg.name}
                  <Show when={hasTag(tg.id)}>
                    <Check class="ml-auto h-3 w-3" />
                  </Show>
                </ContextMenu.Item>
              )}
            </For>
          </Show>
        </ContextMenu.SubContent>
      </ContextMenu.Sub>
      <ContextMenu.Separator />
      <ContextMenu.Item disabled>
        <Folder class="h-3.5 w-3.5" />
        Open folder
      </ContextMenu.Item>
      <ContextMenu.Item onSelect={props.onCopyMagnet}>
        <Copy class="h-3.5 w-3.5" />
        Copy magnet
      </ContextMenu.Item>
      <ContextMenu.Separator />
      <ContextMenu.Item danger onSelect={props.onRemove}>
        <Trash2 class="h-3.5 w-3.5" />
        Remove
      </ContextMenu.Item>
    </ContextMenu>
  );
}
