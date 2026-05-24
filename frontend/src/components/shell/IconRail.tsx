import {For, Show, createMemo, createSignal, type Component} from 'solid-js';
import {LogOut} from 'lucide-solid';
import {Tooltip} from '../ui/Tooltip';
import {ContextMenu} from '../ui/ContextMenu';
import type {AppView} from '../../lib/store';
import type {SettingsPane} from '../settings/SettingsSidebar';
import type {ServerFlavor, UserDTO} from '../../lib/bindings';
import {
  sidebarPins,
  unpinItem,
  reorderPins,
  isItemVisible,
  ITEM_REGISTRY,
  type PinnableID,
} from '../../lib/sidebar_pins';

type Props = {
  view: AppView;
  settingsPane: SettingsPane;
  onNavigate: (v: AppView) => void;
  onNavigateSettingsPane: (p: SettingsPane) => void;
  flavor: ServerFlavor;
  currentUser: UserDTO | null;
  // Present only for an authenticated web session (mosaicd); absent on the
  // Wails desktop build, which has no login.
  onLogout?: () => void;
};

export function IconRail(props: Props) {
  // Drag state. draggingIdx is the source row's position in the current
  // pins() array (1-based effectively, since 0 is Torrents which is
  // non-draggable). dropIdx is where the dragged item would LAND if
  // released right now — separate from draggingIdx because the mouse can
  // hover anywhere. dropAbove distinguishes "land before the hovered
  // item" from "land after" so the visual indicator (a thin accent line)
  // can sit at the correct edge.
  const [draggingIdx, setDraggingIdx] = createSignal<number | null>(null);
  const [dropIdx, setDropIdx] = createSignal<number | null>(null);
  const [dropAbove, setDropAbove] = createSignal<boolean>(true);

  const ctx = () => ({flavor: props.flavor, user: props.currentUser});

  // Visible pins: filter the order array against per-item visibility
  // gates so an admin's pin set doesn't leak admin icons onto a member
  // account's rail (the underlying order array stays intact in storage —
  // gates only suppress at render). 'torrents' is implicitly visible.
  const visiblePins = createMemo(() => sidebarPins().filter((id) => isItemVisible(id, ctx())));

  // Active-state predicate. The rail can host arbitrary settings panes
  // now (any pinned id), so this resolves to true when:
  // - id === 'torrents' AND we're on the torrents view
  // - id === 'settings' AND we're on settings AND the active pane is NOT
  //   independently pinned (otherwise pinning, say, RSS would leave
  //   Settings highlighted as well while you're on the RSS pane)
  // - id is a SettingsPane AND we're on settings on that pane
  const isActive = (id: PinnableID): boolean => {
    if (id === 'torrents') return props.view === 'torrents';
    if (id === 'settings') {
      if (props.view !== 'settings') return false;
      // If the current pane has its own pinned icon, defer to it.
      return !sidebarPins().includes(props.settingsPane as PinnableID);
    }
    return props.view === 'settings' && props.settingsPane === id;
  };

  const navigate = (id: PinnableID) => {
    if (id === 'torrents') { props.onNavigate('torrents'); return; }
    if (id === 'settings') { props.onNavigate('settings'); return; }
    props.onNavigateSettingsPane(id as SettingsPane);
  };

  // Apply the pending drop by translating "land at dropIdx, above-or-below"
  // into a single target index for reorderPins, then clear drag state.
  const commitDrop = () => {
    const from = draggingIdx();
    const drop = dropIdx();
    if (from != null && drop != null) {
      // dropIdx is the index of the row the mouse was over; converting
      // to an insertion index depends on whether we were above (insert
      // at drop) or below (insert at drop+1). Then for an in-list move,
      // if the source is BEFORE the destination, the splice math shifts
      // by one because removing the source first compresses the array.
      let target = dropAbove() ? drop : drop + 1;
      if (from < target) target -= 1;
      // reorderPins enforces the >=1 invariant; an attempt to land at 0
      // (above Torrents) becomes a no-op.
      const clamped = Math.max(1, Math.min(target, sidebarPins().length - 1));
      reorderPins(from, clamped);
    }
    setDraggingIdx(null);
    setDropIdx(null);
  };

  const Btn: Component<{id: PinnableID; idx: number}> = (p) => {
    const meta = ITEM_REGISTRY[p.id];
    const draggable = p.id !== 'torrents';
    const isDragging = () => draggingIdx() === p.idx;
    const showIndicatorAbove = () => dropIdx() === p.idx && dropAbove() && draggingIdx() !== null && draggingIdx() !== p.idx;
    const showIndicatorBelow = () => dropIdx() === p.idx && !dropAbove() && draggingIdx() !== null && draggingIdx() !== p.idx;

    // The visible button. Wrapped in ContextMenu for right-click unpin,
    // and wrapped in a position-relative container so the drop indicator
    // can be absolutely positioned at the top/bottom edge.
    const button = (
      <div
        class="relative"
        draggable={draggable}
        onDragStart={(e) => {
          if (!draggable) { e.preventDefault(); return; }
          setDraggingIdx(p.idx);
          // setData is required for Firefox to actually start the drag.
          e.dataTransfer?.setData('text/plain', p.id);
          if (e.dataTransfer) e.dataTransfer.effectAllowed = 'move';
        }}
        onDragEnd={() => {
          // Cleanup if the drop didn't land on a valid target.
          setDraggingIdx(null);
          setDropIdx(null);
        }}
        onDragOver={(e) => {
          if (draggingIdx() == null) return;
          // preventDefault enables drop. Without it the browser refuses.
          e.preventDefault();
          if (e.dataTransfer) e.dataTransfer.dropEffect = 'move';
          // Top-half vs bottom-half of this row decides insert position.
          const rect = e.currentTarget.getBoundingClientRect();
          const above = (e.clientY - rect.top) < rect.height / 2;
          // Can't land above Torrents (idx 0): force "below" when
          // hovering Torrents.
          if (p.idx === 0) {
            setDropIdx(0);
            setDropAbove(false);
          } else {
            setDropIdx(p.idx);
            setDropAbove(above);
          }
        }}
        onDrop={(e) => {
          e.preventDefault();
          commitDrop();
        }}
      >
        {/* Drop indicators: a thin accent-colored line at the edge where
            the dragged item will land. Two separate elements because we
            want it to sit ABOVE this row or BELOW it depending on which
            half the mouse is in. */}
        <Show when={showIndicatorAbove()}>
          <span class="pointer-events-none absolute -top-0.5 left-1 right-1 h-0.5 rounded-full bg-accent-500" />
        </Show>
        <Show when={showIndicatorBelow()}>
          <span class="pointer-events-none absolute -bottom-0.5 left-1 right-1 h-0.5 rounded-full bg-accent-500" />
        </Show>
        <Tooltip label={meta.label} placement="right">
          <button
            type="button"
            onClick={() => navigate(p.id)}
            class="relative grid h-10 w-10 place-items-center rounded-lg text-zinc-500 transition-all duration-150 hover:text-zinc-200"
            classList={{
              '!text-zinc-100': isActive(p.id),
              'opacity-30': isDragging(),
            }}
          >
            <meta.icon class="h-4 w-4" />
            {isActive(p.id) && (
              <span class="absolute left-0 top-1.5 bottom-1.5 w-[2px] rounded-r-full bg-accent-500" />
            )}
          </button>
        </Tooltip>
      </div>
    );

    // Torrents has no unpin option, so it skips the context menu wrapper.
    if (p.id === 'torrents') return button;

    return (
      <ContextMenu trigger={button}>
        <ContextMenu.Item onSelect={() => unpinItem(p.id)}>
          Unpin from Sidebar
        </ContextMenu.Item>
      </ContextMenu>
    );
  };

  return (
    <nav
      class="flex h-full w-12 flex-col items-center border-r border-white/[.04] bg-white/[.01] pt-10 pb-3"
      style={{'--wails-draggable': 'drag', '-webkit-app-region': 'drag'}}
    >
      {/* Single flat list now — the old top/bottom split made sense when
          the rail was hardcoded (nav vs settings), but user-pinned items
          don't carry that semantic split, and forcing them into one
          group is much simpler for drag/drop reordering. Logout still
          pins to the very bottom because it's a session-level action,
          not a navigation target. */}
      <div class="flex flex-1 flex-col gap-1" style={{'--wails-draggable': 'no-drag', '-webkit-app-region': 'no-drag'}}>
        <For each={visiblePins()}>
          {(id, idx) => <Btn id={id} idx={idx()} />}
        </For>
      </div>
      <Show when={props.onLogout}>
        <div class="flex flex-col" style={{'--wails-draggable': 'no-drag', '-webkit-app-region': 'no-drag'}}>
          <Tooltip label="Sign out" placement="right">
            <button
              type="button"
              onClick={() => props.onLogout?.()}
              class="grid h-10 w-10 place-items-center rounded-lg text-zinc-500 transition-colors duration-150 hover:text-zinc-200"
            >
              <LogOut class="h-4 w-4" />
            </button>
          </Tooltip>
        </div>
      </Show>
    </nav>
  );
}
