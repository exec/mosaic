import {For} from 'solid-js';
import {Sliders, Wifi, Globe, Download, MonitorSmartphone, Calendar, Shield, Rss, Folder, Tag, Info} from 'lucide-solid';
import {isWailsRuntime} from '../../lib/runtime';

export type SettingsPane = 'general' | 'connection' | 'web' | 'updates' | 'desktop' | 'schedule' | 'blocklist' | 'rss' | 'categories' | 'tags' | 'about';

const allItems: {value: SettingsPane; label: string; icon: typeof Sliders; wailsOnly?: boolean}[] = [
  {value: 'general',    label: 'General',        icon: Sliders},
  {value: 'connection', label: 'Connection',     icon: Wifi},
  {value: 'web',        label: 'Web Interface',  icon: Globe},
  {value: 'updates',    label: 'Updates',        icon: Download},
  // Desktop integration (tray, notifications, close-to-tray) only applies to
  // the local desktop session running the Mosaic binary, so we hide it when
  // the SPA is running over the remote HTTPS+WS transport.
  {value: 'desktop',    label: 'Desktop',        icon: MonitorSmartphone, wailsOnly: true},
  {value: 'schedule',   label: 'Schedule',       icon: Calendar},
  {value: 'blocklist',  label: 'Blocklist',      icon: Shield},
  {value: 'rss',        label: 'RSS',            icon: Rss},
  {value: 'categories', label: 'Categories',     icon: Folder},
  {value: 'tags',       label: 'Tags',           icon: Tag},
  {value: 'about',      label: 'About',          icon: Info},
];

const items = allItems.filter((i) => !i.wailsOnly || isWailsRuntime());

type Props = {
  active: SettingsPane;
  onSelect: (p: SettingsPane) => void;
};

export function SettingsSidebar(props: Props) {
  return (
    <aside class="flex h-full w-56 shrink-0 flex-col border-r border-white/[.04] bg-white/[.01] pt-10 pb-3">
      <ul class="flex flex-col gap-px px-2">
        <For each={items}>
          {(item) => (
            <li>
              <button
                type="button"
                onClick={() => props.onSelect(item.value)}
                class="relative flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm text-zinc-300 transition-colors duration-100 hover:bg-white/[.04] hover:text-zinc-100"
                classList={{'bg-white/[.04] text-zinc-100': props.active === item.value}}
              >
                <item.icon class="h-3.5 w-3.5" />
                {item.label}
                {props.active === item.value && (
                  <span class="absolute left-0 top-1.5 bottom-1.5 w-[2px] rounded-r-full bg-accent-500" />
                )}
              </button>
            </li>
          )}
        </For>
      </ul>
    </aside>
  );
}
