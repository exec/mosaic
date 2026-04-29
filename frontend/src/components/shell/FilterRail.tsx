import {For, Show, type Component} from 'solid-js';
import {ChevronDown, ListFilter, Folder, Tag, Globe} from 'lucide-solid';
import type {StatusFilter} from '../../lib/store';
import type {Torrent} from '../../lib/bindings';

type StatusItem = {id: StatusFilter; label: string; count: (t: Torrent[]) => number};

const statusItems: StatusItem[] = [
  {id: 'all',         label: 'All',         count: (t) => t.length},
  {id: 'downloading', label: 'Downloading', count: (t) => t.filter((x) => !x.paused && !x.completed).length},
  {id: 'seeding',     label: 'Seeding',     count: (t) => t.filter((x) => x.completed && !x.paused).length},
  {id: 'completed',   label: 'Completed',   count: (t) => t.filter((x) => x.completed).length},
  {id: 'paused',      label: 'Paused',      count: (t) => t.filter((x) => x.paused).length},
  {id: 'errored',     label: 'Errored',     count: () => 0},
];

type Props = {
  torrents: Torrent[];
  active: StatusFilter;
  onSelect: (s: StatusFilter) => void;
};

const Section: Component<{icon: typeof ListFilter; title: string; count?: number; children?: any}> = (p) => (
  <div class="px-2">
    <div class="flex items-center justify-between px-2 py-1.5 text-[10px] font-semibold uppercase tracking-wider text-zinc-500">
      <span class="inline-flex items-center gap-1.5">
        <p.icon class="h-3 w-3" />
        {p.title}
      </span>
      <ChevronDown class="h-3 w-3 text-zinc-600" />
    </div>
    {p.children}
  </div>
);

export function FilterRail(props: Props) {
  return (
    <aside class="flex h-full w-60 shrink-0 flex-col gap-3 border-r border-white/[.04] bg-white/[.01] py-3">
      <Section icon={ListFilter} title="Status">
        <ul class="flex flex-col gap-px">
          <For each={statusItems}>
            {(it) => {
              const c = () => it.count(props.torrents);
              return (
                <li>
                  <button
                    type="button"
                    onClick={() => props.onSelect(it.id)}
                    class="flex w-full items-center justify-between rounded-md px-2 py-1.5 text-sm transition-colors duration-100 hover:bg-white/[.04]"
                    classList={{'bg-accent-500/[.10] text-accent-200': props.active === it.id, 'text-zinc-300': props.active !== it.id}}
                  >
                    <span>{it.label}</span>
                    <Show when={c() > 0}>
                      <span class="font-mono text-xs tabular-nums text-zinc-500">{c()}</span>
                    </Show>
                  </button>
                </li>
              );
            }}
          </For>
        </ul>
      </Section>

      <Section icon={Folder} title="Categories">
        <p class="px-2 text-xs text-zinc-600">Coming in Plan 4</p>
      </Section>
      <Section icon={Tag} title="Tags">
        <p class="px-2 text-xs text-zinc-600">Coming in Plan 4</p>
      </Section>
      <Section icon={Globe} title="Trackers">
        <p class="px-2 text-xs text-zinc-600">Coming in Plan 4</p>
      </Section>
    </aside>
  );
}
