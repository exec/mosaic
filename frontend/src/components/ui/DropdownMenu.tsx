import {DropdownMenu as KDropdownMenu} from '@kobalte/core/dropdown-menu';
import type {JSX} from 'solid-js';

const contentClass = 'z-50 min-w-[180px] rounded-md border border-white/10 bg-zinc-900/95 p-1 text-sm shadow-2xl backdrop-blur-md animate-in fade-in zoom-in-95';
const itemClass = 'flex cursor-pointer items-center gap-2 rounded px-2 py-1.5 text-zinc-200 outline-none transition-colors duration-100 data-[highlighted]:bg-white/10 data-[disabled]:opacity-40 data-[disabled]:pointer-events-none';

export const DropdownMenu = Object.assign(
  function (props: {trigger: JSX.Element; children: JSX.Element}) {
    return (
      <KDropdownMenu>
        <KDropdownMenu.Trigger as="span">{props.trigger}</KDropdownMenu.Trigger>
        <KDropdownMenu.Portal>
          <KDropdownMenu.Content class={contentClass}>{props.children}</KDropdownMenu.Content>
        </KDropdownMenu.Portal>
      </KDropdownMenu>
    );
  },
  {
    Item: (props: {children: JSX.Element; onSelect?: () => void; disabled?: boolean}) => (
      <KDropdownMenu.Item class={itemClass} onSelect={props.onSelect} disabled={props.disabled}>
        {props.children}
      </KDropdownMenu.Item>
    ),
    Separator: () => <KDropdownMenu.Separator class="my-1 h-px bg-white/10" />,
  },
);
