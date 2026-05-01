import {For, type Component} from 'solid-js';
import {Activity, Calendar, Rss, Settings, Info} from 'lucide-solid';
import {Tooltip} from '../ui/Tooltip';
import type {AppView} from '../../lib/store';
import type {SettingsPane} from '../settings/SettingsSidebar';

type Item = {id: string; label: string; icon: typeof Activity};

const top: Item[] = [
  {id: 'torrents', label: 'Torrents', icon: Activity},
  {id: 'schedule', label: 'Schedule', icon: Calendar},
  {id: 'rss',      label: 'RSS',      icon: Rss},
];
const bottom: Item[] = [
  {id: 'settings', label: 'Settings', icon: Settings},
  {id: 'about',    label: 'About',    icon: Info},
];

type Props = {
  view: AppView;
  settingsPane: SettingsPane;
  onNavigate: (v: AppView) => void;
  onNavigateRSS: () => void;
  onNavigateSchedule: () => void;
  onNavigateAbout: () => void;
};

export function IconRail(props: Props) {
  const isActive = (id: string): boolean => {
    if (id === 'rss') return props.view === 'settings' && props.settingsPane === 'rss';
    if (id === 'schedule') return props.view === 'settings' && props.settingsPane === 'schedule';
    if (id === 'about') return props.view === 'settings' && props.settingsPane === 'about';
    if (id === 'settings') {
      return props.view === 'settings' && !['rss', 'schedule', 'about'].includes(props.settingsPane);
    }
    return props.view === id;
  };

  const Btn: Component<{item: Item}> = (p) => (
    <Tooltip label={p.item.label} placement="right">
      <button
        type="button"
        onClick={() => {
          switch (p.item.id) {
            case 'rss':      props.onNavigateRSS(); return;
            case 'schedule': props.onNavigateSchedule(); return;
            case 'about':    props.onNavigateAbout(); return;
            case 'torrents':
            case 'settings':
              props.onNavigate(p.item.id as AppView);
              return;
          }
        }}
        class="relative grid h-10 w-10 place-items-center rounded-lg text-zinc-500 transition-colors duration-150 hover:text-zinc-200"
        classList={{'!text-zinc-100': isActive(p.item.id)}}
      >
        <p.item.icon class="h-4 w-4" />
        {isActive(p.item.id) && (
          <span class="absolute left-0 top-1.5 bottom-1.5 w-[2px] rounded-r-full bg-accent-500" />
        )}
      </button>
    </Tooltip>
  );

  return (
    <nav class="flex h-full w-12 flex-col items-center justify-between border-r border-white/[.04] bg-white/[.01] pt-10 pb-3" style={{'--wails-draggable': 'drag', '-webkit-app-region': 'drag'}}>
      <div class="flex flex-col gap-1" style={{'--wails-draggable': 'no-drag', '-webkit-app-region': 'no-drag'}}>
        <For each={top}>{(it) => <Btn item={it} />}</For>
      </div>
      <div class="flex flex-col gap-1" style={{'--wails-draggable': 'no-drag', '-webkit-app-region': 'no-drag'}}>
        <For each={bottom}>{(it) => <Btn item={it} />}</For>
      </div>
    </nav>
  );
}
