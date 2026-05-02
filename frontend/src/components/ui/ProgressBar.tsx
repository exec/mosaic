import {Show} from 'solid-js';

export type ProgressStatus = 'downloading' | 'completed' | 'paused' | 'error';

type Props = {
  value: number; // 0..1
  active?: boolean; // when true, adds shimmer (only meaningful while downloading)
  status?: ProgressStatus; // defaults to 'downloading' (the historical purple)
};

// Tailwind v4 reads --color-* tokens directly via bg-{name}, so seed/down/
// paused/fail are already first-class colors — `bg-seed` etc. resolve via
// the @theme block in index.css. The status circle dot in TorrentCard uses
// the same tokens so progress + dot stay in lockstep.
const fillFor = (status: ProgressStatus) => {
  switch (status) {
    case 'completed': return 'bg-seed';
    case 'paused':    return 'bg-paused';
    case 'error':     return 'bg-fail';
    default:          return 'bg-gradient-to-r from-accent-600 to-accent-400';
  }
};

export function ProgressBar(props: Props) {
  const status = () => props.status ?? 'downloading';
  return (
    <div class="relative h-1.5 overflow-hidden rounded-full bg-white/[.04]">
      <div
        class={`absolute inset-y-0 left-0 rounded-full transition-[width,background-color] duration-500 ease-[var(--ease-app)] ${fillFor(status())}`}
        style={{width: `${Math.min(100, props.value * 100).toFixed(2)}%`}}
      />
      <Show when={props.active && status() === 'downloading' && props.value > 0 && props.value < 1}>
        <div
          class="absolute inset-y-0 w-1/3 animate-[shimmer_2s_ease-in-out_infinite] bg-gradient-to-r from-transparent via-white/15 to-transparent"
          style={{left: '-33%'}}
        />
      </Show>
    </div>
  );
}
