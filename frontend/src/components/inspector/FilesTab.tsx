import {For, Show} from 'solid-js';
import {ChevronDown} from 'lucide-solid';
import type {DetailDTO} from '../../lib/bindings';
import {fmtBytes, fmtPercent} from '../../lib/format';
import {DropdownMenu} from '../ui/DropdownMenu';

type Priority = 'skip' | 'normal' | 'high' | 'max';

type Props = {
  detail: DetailDTO | null;
  onSetPriority: (index: number, priority: Priority) => void;
};

export function FilesTab(props: Props) {
  return (
    <Show
      when={props.detail?.files?.length}
      fallback={<div class="p-4 text-xs text-zinc-500">No files yet — waiting for metadata.</div>}
    >
      <div class="flex flex-col">
        <For each={props.detail!.files!}>
          {(f) => (
            <div class="border-b border-white/[.03] px-4 py-2 text-xs">
              <div class="flex items-baseline justify-between gap-2">
                <span class="truncate text-zinc-200" title={f.path}>{f.path}</span>
                <span class="shrink-0 font-mono tabular-nums text-zinc-500">{fmtBytes(f.size)}</span>
              </div>
              <div class="mt-1 flex items-center gap-2">
                <div class="relative h-1 flex-1 overflow-hidden rounded-full bg-white/[.04]">
                  <div
                    class="absolute inset-y-0 left-0 rounded-full bg-gradient-to-r from-accent-600 to-accent-400"
                    style={{width: `${(f.progress * 100).toFixed(2)}%`}}
                  />
                </div>
                <span class="font-mono tabular-nums text-zinc-500">{fmtPercent(f.progress)}</span>
                <DropdownMenu trigger={
                  <button class="inline-flex items-center gap-0.5 rounded px-1.5 py-0.5 text-xs text-zinc-500 transition-colors hover:bg-white/[.04] hover:text-zinc-200">
                    {f.priority}
                    <ChevronDown class="h-3 w-3" />
                  </button>
                }>
                  <DropdownMenu.Item onSelect={() => props.onSetPriority(f.index, 'skip')}>Skip</DropdownMenu.Item>
                  <DropdownMenu.Item onSelect={() => props.onSetPriority(f.index, 'normal')}>Normal</DropdownMenu.Item>
                  <DropdownMenu.Item onSelect={() => props.onSetPriority(f.index, 'high')}>High</DropdownMenu.Item>
                  <DropdownMenu.Item onSelect={() => props.onSetPriority(f.index, 'max')}>Max</DropdownMenu.Item>
                </DropdownMenu>
              </div>
            </div>
          )}
        </For>
      </div>
    </Show>
  );
}
