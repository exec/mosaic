import {For, createMemo} from 'solid-js';
import {Sliders, Palette, Wifi, Globe, Users, Download, MonitorSmartphone, Calendar, Shield, Rss, Folder, Tag, Info, TrendingUp, Pin, PinOff} from 'lucide-solid';
import {isWailsRuntime} from '../../lib/runtime';
import type {ServerFlavor, UserDTO} from '../../lib/bindings';
import {canChangeSettings, canManageRSS, canManageCatTags} from '../../lib/permissions';
import {sidebarPins, togglePin, type PinnableID} from '../../lib/sidebar_pins';
import {Tooltip} from '../ui/Tooltip';

export type SettingsPane =
  | 'general' | 'appearance' | 'connection' | 'seeding' | 'web' | 'users' | 'updates' | 'desktop'
  | 'schedule' | 'blocklist' | 'rss' | 'categories' | 'tags' | 'about';

type VisibilityCtx = {flavor: ServerFlavor; user: UserDTO | null};

type Item = {
  value: SettingsPane;
  label: string;
  icon: typeof Sliders;
  // visible defaults to always-shown when omitted.
  visible?: (ctx: VisibilityCtx) => boolean;
};

const allItems: Item[] = [
  {value: 'general', label: 'General', icon: Sliders},
  {value: 'appearance', label: 'Appearance', icon: Palette},
  {value: 'connection', label: 'Connection', icon: Wifi, visible: ({user}) => canChangeSettings(user)},
  {value: 'seeding', label: 'Seeding', icon: TrendingUp, visible: ({user}) => canChangeSettings(user)},
  // The "Web Interface" pane configures the optional embedded server. That
  // only makes sense on the desktop build — mosaicd *is* the web interface
  // and exposes the "Users" pane instead.
  {value: 'web', label: 'Web Interface', icon: Globe, visible: ({flavor}) => flavor === 'desktop'},
  // Multi-user account management — only the headless mosaicd daemon.
  {value: 'users', label: 'Users', icon: Users, visible: ({flavor}) => flavor === 'daemon'},
  {value: 'updates', label: 'Updates', icon: Download},
  // Desktop integration (tray, notifications, close-to-tray) only applies to
  // the local desktop session running the Mosaic binary.
  {value: 'desktop', label: 'Desktop', icon: MonitorSmartphone, visible: () => isWailsRuntime()},
  {value: 'schedule', label: 'Schedule', icon: Calendar, visible: ({user}) => canChangeSettings(user)},
  {value: 'blocklist', label: 'Blocklist', icon: Shield, visible: ({user}) => canChangeSettings(user)},
  {value: 'rss', label: 'RSS', icon: Rss, visible: ({user}) => canManageRSS(user)},
  {value: 'categories', label: 'Categories', icon: Folder, visible: ({user}) => canManageCatTags(user)},
  {value: 'tags', label: 'Tags', icon: Tag, visible: ({user}) => canManageCatTags(user)},
  {value: 'about', label: 'About', icon: Info},
];

type Props = {
  active: SettingsPane;
  onSelect: (p: SettingsPane) => void;
  flavor: ServerFlavor;
  currentUser: UserDTO | null;
};

export function SettingsSidebar(props: Props) {
  const items = createMemo(() => {
    const ctx: VisibilityCtx = {flavor: props.flavor, user: props.currentUser};
    return allItems.filter((i) => !i.visible || i.visible(ctx));
  });
  const isPinned = (id: PinnableID): boolean => sidebarPins().includes(id);

  return (
    <aside class="flex h-full w-56 shrink-0 flex-col border-r border-white/[.04] bg-white/[.01] pt-10 pb-3">
      <ul class="flex flex-col gap-px px-2">
        <For each={items()}>
          {(item) => {
            const pinned = () => isPinned(item.value as PinnableID);
            return (
              <li class="group/row relative">
                <button
                  type="button"
                  onClick={() => props.onSelect(item.value)}
                  class="relative flex w-full items-center gap-2 rounded-md px-2 py-1.5 pr-8 text-sm text-zinc-300 transition-colors duration-100 hover:bg-white/[.04] hover:text-zinc-100"
                  classList={{'bg-white/[.04] text-zinc-100': props.active === item.value}}
                >
                  <item.icon class="h-3.5 w-3.5" />
                  {item.label}
                  {props.active === item.value && (
                    <span class="absolute left-0 top-1.5 bottom-1.5 w-[2px] rounded-r-full bg-accent-500" />
                  )}
                </button>
                {/* Pin toggle. Lives inside the row but absolute-positioned
                    so the row's hit area stays a clean rectangle for the
                    underlying navigate-on-click. Visible on hover-of-row
                    OR when already pinned (the user needs to see the
                    state at a glance, not just on hover). */}
                <Tooltip label={pinned() ? 'Unpin from sidebar' : 'Pin to sidebar'} placement="right">
                  <button
                    type="button"
                    aria-label={pinned() ? 'Unpin from sidebar' : 'Pin to sidebar'}
                    aria-pressed={pinned()}
                    onClick={(e) => {
                      e.stopPropagation();
                      togglePin(item.value as PinnableID);
                    }}
                    class="absolute right-1.5 top-1/2 -translate-y-1/2 grid h-6 w-6 place-items-center rounded text-zinc-500 transition-all duration-100 hover:bg-white/[.06] hover:text-zinc-200"
                    classList={{
                      'opacity-100 text-accent-400': pinned(),
                      'opacity-0 group-hover/row:opacity-100': !pinned(),
                    }}
                  >
                    {pinned() ? <Pin class="h-3 w-3 fill-current" /> : <PinOff class="h-3 w-3" />}
                  </button>
                </Tooltip>
              </li>
            );
          }}
        </For>
      </ul>
    </aside>
  );
}
