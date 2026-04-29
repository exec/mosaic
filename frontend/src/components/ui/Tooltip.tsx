import {Tooltip as KTooltip} from '@kobalte/core/tooltip';
import type {JSX} from 'solid-js';

export function Tooltip(props: {label: string; children: JSX.Element; placement?: 'top' | 'right' | 'bottom' | 'left'}) {
  return (
    <KTooltip placement={props.placement ?? 'top'} openDelay={200} closeDelay={0}>
      <KTooltip.Trigger as="span">{props.children}</KTooltip.Trigger>
      <KTooltip.Portal>
        <KTooltip.Content class="z-50 rounded-md border border-white/10 bg-zinc-900/95 px-2 py-1 text-xs text-zinc-100 shadow-lg backdrop-blur-md animate-in fade-in zoom-in-95">
          {props.label}
          <KTooltip.Arrow class="fill-zinc-900/95" />
        </KTooltip.Content>
      </KTooltip.Portal>
    </KTooltip>
  );
}
