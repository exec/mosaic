import {ContextMenu} from '../ui/ContextMenu';
import {Pause, Play, Trash2, Folder, Copy, RotateCw, Tag, ChevronRight, Check, ListOrdered, Share2} from 'lucide-solid';
import type {CategoryDTO, TagDTO, Torrent} from '../../lib/bindings';
import {For, type JSX, Show} from 'solid-js';
import {isWailsRuntime} from '../../lib/runtime';

export type QueueDirection = 'top' | 'up' | 'down' | 'bottom';

type Props = {
  torrent: Torrent;
  categories: CategoryDTO[];
  tags: TagDTO[];
  onPause: () => void;
  onResume: () => void;
  onRecheck: () => void;
  onRemove: () => void;
  onCopyMagnet: () => void;
  onOpenFolder: () => void;
  onSetCategory: (id: number | null) => void;
  onToggleTag: (id: number) => void;
  onMoveQueue: (direction: QueueDirection) => void;
  onToggleForceStart: () => void;
  // onShare opens the share dialog. Provided only when the current user may
  // share (owns the torrent and holds the share permission); omitted otherwise.
  onShare?: () => void;
  children: JSX.Element;
};

export function TorrentRowMenu(props: Props) {
  const hasTag = (id: number) => props.torrent.tags.some((t) => t.id === id);
  // Per-torrent access gating: 'viewer' is read-only, 'editor' may control the
  // torrent, 'owner' may additionally remove and share it. Admins see 'owner'.
  const canEdit = () => props.torrent.access === 'editor' || props.torrent.access === 'owner';
  const canOwn = () => props.torrent.access === 'owner';
  return (
    <ContextMenu trigger={props.children}>
      <Show when={canEdit()}>
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
        <ContextMenu.Item onSelect={props.onRecheck}>
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
      </Show>
      <Show when={canOwn() && props.onShare}>
        <ContextMenu.Item onSelect={() => props.onShare?.()}>
          <Share2 class="h-3.5 w-3.5" />
          Share…
        </ContextMenu.Item>
      </Show>
      <Show when={isWailsRuntime()}>
        <ContextMenu.Item onSelect={props.onOpenFolder}>
          <Folder class="h-3.5 w-3.5" />
          Open folder
        </ContextMenu.Item>
      </Show>
      <ContextMenu.Item onSelect={props.onCopyMagnet}>
        <Copy class="h-3.5 w-3.5" />
        Copy magnet
      </ContextMenu.Item>
      <ContextMenu.Separator />
      <ContextMenu.Item danger onSelect={props.onRemove}>
        <Trash2 class="h-3.5 w-3.5" />
        <Show when={canOwn()} fallback={<>Remove from my list</>}>
          Remove
        </Show>
      </ContextMenu.Item>
    </ContextMenu>
  );
}
