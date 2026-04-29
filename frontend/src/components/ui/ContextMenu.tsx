import {ContextMenu as KContextMenu} from '@kobalte/core/context-menu';
import type {JSX} from 'solid-js';

const contentClass = 'z-50 min-w-[200px] rounded-md border border-white/10 bg-zinc-900/95 p-1 text-sm shadow-2xl backdrop-blur-md animate-in fade-in zoom-in-95';
const itemClass = 'flex cursor-pointer items-center gap-2 rounded px-2 py-1.5 text-zinc-200 outline-none transition-colors duration-100 data-[highlighted]:bg-white/10 data-[disabled]:opacity-40 data-[disabled]:pointer-events-none';

export const ContextMenu = Object.assign(
  function (props: {trigger: JSX.Element; children: JSX.Element}) {
    return (
      <KContextMenu>
        <KContextMenu.Trigger as="div">{props.trigger}</KContextMenu.Trigger>
        <KContextMenu.Portal>
          <KContextMenu.Content class={contentClass}>{props.children}</KContextMenu.Content>
        </KContextMenu.Portal>
      </KContextMenu>
    );
  },
  {
    Item: (props: {children: JSX.Element; onSelect?: () => void; disabled?: boolean; danger?: boolean}) => (
      <KContextMenu.Item
        class={`${itemClass} ${props.danger ? 'data-[highlighted]:bg-rose-500/20 data-[highlighted]:text-rose-300' : ''}`}
        onSelect={props.onSelect}
        disabled={props.disabled}
      >
        {props.children}
      </KContextMenu.Item>
    ),
    Separator: () => <KContextMenu.Separator class="my-1 h-px bg-white/10" />,
  },
);
