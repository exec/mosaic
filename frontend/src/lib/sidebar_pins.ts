// User-customizable IconRail pin state. The set of items in the left rail
// is no longer hardcoded — users pin/unpin from each row in the settings
// sidebar, drag to reorder, and right-click for unpin. Torrents is always
// first and always pinned (it's the app's main view, you can't navigate
// without it). The order array is the source of truth for both
// pinned-ness AND ordering; persisted to localStorage so settings stick
// across launches.

import {createSignal, type Component} from 'solid-js';
import {
  Activity, Settings, Calendar, Rss, Info,
  Sliders, Palette, Wifi, TrendingUp, Globe, Users, Download,
  MonitorSmartphone, Shield, Folder, Tag,
} from 'lucide-solid';
import {isWailsRuntime} from './runtime';
import {canChangeSettings, canManageRSS, canManageCatTags} from './permissions';
import type {ServerFlavor, UserDTO} from './bindings';
import type {SettingsPane} from '../components/settings/SettingsSidebar';

// The full pinnable surface. 'torrents' is the app's top-level torrent
// list view. 'settings' is a generic shortcut into the settings view
// (keeps whatever pane was last open). All other ids are direct shortcuts
// to a specific settings pane, navigating to the settings view + that
// pane in one click.
export type PinnableID = 'torrents' | 'settings' | SettingsPane;

export type VisibilityCtx = {flavor: ServerFlavor; user: UserDTO | null};

export type ItemMeta = {
  id: PinnableID;
  label: string;
  icon: Component<{class?: string}>;
  // Optional gate — same logic the SettingsSidebar uses. If absent,
  // always visible. Pinned items that fail their gate (e.g. an admin pane
  // on a member account, or 'users' on the desktop flavor) get hidden
  // from the rail at render time without removing them from the order —
  // so an admin's pin set survives the round-trip through a member login.
  visible?: (ctx: VisibilityCtx) => boolean;
};

export const ITEM_REGISTRY: Record<PinnableID, ItemMeta> = {
  torrents:   {id: 'torrents',   label: 'Torrents',      icon: Activity},
  settings:   {id: 'settings',   label: 'Settings',      icon: Settings},
  general:    {id: 'general',    label: 'General',       icon: Sliders},
  appearance: {id: 'appearance', label: 'Appearance',    icon: Palette},
  connection: {id: 'connection', label: 'Connection',    icon: Wifi,             visible: ({user}) => canChangeSettings(user)},
  seeding:    {id: 'seeding',    label: 'Seeding',       icon: TrendingUp,       visible: ({user}) => canChangeSettings(user)},
  web:        {id: 'web',        label: 'Web Interface', icon: Globe,            visible: ({flavor}) => flavor === 'desktop'},
  users:      {id: 'users',      label: 'Users',         icon: Users,            visible: ({flavor}) => flavor === 'daemon'},
  updates:    {id: 'updates',    label: 'Updates',       icon: Download},
  desktop:    {id: 'desktop',    label: 'Desktop',       icon: MonitorSmartphone, visible: () => isWailsRuntime()},
  schedule:   {id: 'schedule',   label: 'Schedule',      icon: Calendar,         visible: ({user}) => canChangeSettings(user)},
  blocklist:  {id: 'blocklist',  label: 'Blocklist',     icon: Shield,           visible: ({user}) => canChangeSettings(user)},
  rss:        {id: 'rss',        label: 'RSS',           icon: Rss,              visible: ({user}) => canManageRSS(user)},
  categories: {id: 'categories', label: 'Categories',    icon: Folder,           visible: ({user}) => canManageCatTags(user)},
  tags:       {id: 'tags',       label: 'Tags',          icon: Tag,              visible: ({user}) => canManageCatTags(user)},
  about:      {id: 'about',      label: 'About',         icon: Info},
};

export function isItemVisible(id: PinnableID, ctx: VisibilityCtx): boolean {
  const meta = ITEM_REGISTRY[id];
  return !meta?.visible || meta.visible(ctx);
}

// 'torrents' is non-negotiable; the rest matches the IconRail layout
// users had pre-customization (Schedule + RSS up top, Settings + About
// at the bottom) so first-launch looks identical to the old hardcoded
// rail.
const DEFAULT_PINS: PinnableID[] = ['torrents', 'schedule', 'rss', 'settings', 'about'];
const STORAGE_KEY = 'mosaic.sidebar_pins.v1';

function loadInitial(): PinnableID[] {
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    if (!raw) return DEFAULT_PINS;
    const parsed = JSON.parse(raw);
    if (!Array.isArray(parsed)) return DEFAULT_PINS;
    // Filter to known ids; dedupe; force torrents to lead.
    const known = parsed.filter((id): id is PinnableID => typeof id === 'string' && id in ITEM_REGISTRY);
    const dedup = Array.from(new Set(known));
    const rest = dedup.filter((id) => id !== 'torrents');
    return ['torrents', ...rest];
  } catch {
    return DEFAULT_PINS;
  }
}

const [pinsSignal, setPinsSignal] = createSignal<PinnableID[]>(loadInitial());

function persist(next: PinnableID[]) {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(next));
  } catch {
    // Quota / private-mode — non-fatal; the in-memory signal still drives
    // the UI for the current session.
  }
}

function applyInvariants(input: PinnableID[]): PinnableID[] {
  const dedup = Array.from(new Set(input));
  const rest = dedup.filter((id) => id !== 'torrents');
  return ['torrents', ...rest];
}

export const sidebarPins = pinsSignal;

export function setPins(next: PinnableID[]) {
  const final = applyInvariants(next);
  setPinsSignal(final);
  persist(final);
}

export function isPinned(id: PinnableID): boolean {
  return pinsSignal().includes(id);
}

export function pinItem(id: PinnableID) {
  if (isPinned(id)) return;
  setPins([...pinsSignal(), id]);
}

export function unpinItem(id: PinnableID) {
  if (id === 'torrents') return; // Locked.
  setPins(pinsSignal().filter((x) => x !== id));
}

export function togglePin(id: PinnableID) {
  if (id === 'torrents') return;
  if (isPinned(id)) unpinItem(id);
  else pinItem(id);
}

// Move the item currently at fromIdx so it lands at toIdx in the new
// array. Both indices refer to the CURRENT ordered list. Index 0 is
// reserved for 'torrents' — callers can't move into or out of it.
export function reorderPins(fromIdx: number, toIdx: number) {
  if (fromIdx <= 0 || toIdx <= 0) return;
  const cur = pinsSignal();
  if (fromIdx >= cur.length || toIdx >= cur.length) return;
  if (fromIdx === toIdx) return;
  const next = [...cur];
  const [moved] = next.splice(fromIdx, 1);
  next.splice(toIdx, 0, moved);
  setPins(next);
}

// Resolves a pinnable id to a navigation intent. Used by IconRail click
// handlers and by App.tsx's onNavigateSettingsPane wrapper to keep the
// view/pane setters out of every component that wants to navigate.
export type NavIntent =
  | {view: 'torrents'}
  | {view: 'settings'; pane?: SettingsPane};

export function navIntentFor(id: PinnableID): NavIntent {
  if (id === 'torrents') return {view: 'torrents'};
  if (id === 'settings') return {view: 'settings'}; // keep current pane
  return {view: 'settings', pane: id as SettingsPane};
}
