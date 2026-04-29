import {For, type Component} from 'solid-js';
import {Activity, Search, Calendar, Rss, Settings, Info} from 'lucide-solid';
import {Tooltip} from '../ui/Tooltip';
import type {AppView} from '../../lib/store';

type Item = {id: string; label: string; icon: typeof Activity; soon?: string};

const top: Item[] = [
  {id: 'torrents', label: 'Torrents', icon: Activity},
  {id: 'search',   label: 'Search',   icon: Search,   soon: 'Plan 5+'},
  {id: 'schedule', label: 'Schedule', icon: Calendar, soon: 'Plan 4c'},
  {id: 'rss',      label: 'RSS',      icon: Rss,      soon: 'Plan 5'},
];
const bottom: Item[] = [
  {id: 'settings', label: 'Settings', icon: Settings},
  {id: 'about',    label: 'About',    icon: Info,     soon: 'soon'},
];

type Props = {
  view: AppView;
  onNavigate: (v: AppView) => void;
};

export function IconRail(props: Props) {
  const Btn: Component<{item: Item}> = (p) => (
    <Tooltip label={p.item.soon ? `${p.item.label} — coming ${p.item.soon}` : p.item.label} placement="right">
      <button
        type="button"
        disabled={!!p.item.soon}
        onClick={() => {
          if (p.item.soon) return;
          if (p.item.id === 'torrents' || p.item.id === 'settings') {
            props.onNavigate(p.item.id as AppView);
          }
        }}
        class="relative grid h-10 w-10 place-items-center rounded-lg text-zinc-500 transition-colors duration-150 hover:text-zinc-200 disabled:opacity-30 disabled:cursor-default disabled:hover:text-zinc-500"
        classList={{'!text-zinc-100': props.view === p.item.id}}
      >
        <p.item.icon class="h-4 w-4" />
        {props.view === p.item.id && (
          <span class="absolute left-0 top-1.5 bottom-1.5 w-[2px] rounded-r-full bg-accent-500" />
        )}
      </button>
    </Tooltip>
  );

  return (
    <nav class="flex h-full w-12 flex-col items-center justify-between border-r border-white/[.04] bg-white/[.01] pt-10 pb-3" style={{'-webkit-app-region': 'drag'}}>
      <div class="flex flex-col gap-1" style={{'-webkit-app-region': 'no-drag'}}>
        <For each={top}>{(it) => <Btn item={it} />}</For>
      </div>
      <div class="flex flex-col gap-1" style={{'-webkit-app-region': 'no-drag'}}>
        <For each={bottom}>{(it) => <Btn item={it} />}</For>
      </div>
    </nav>
  );
}
