import {Show} from 'solid-js';

type Props = {
  value: number; // 0..1
  active?: boolean; // when true, adds shimmer
};

export function ProgressBar(props: Props) {
  return (
    <div class="relative h-1.5 overflow-hidden rounded-full bg-white/[.04]">
      <div
        class="absolute inset-y-0 left-0 rounded-full bg-gradient-to-r from-accent-600 to-accent-400 transition-[width] duration-500 ease-[var(--ease-app)]"
        style={{width: `${Math.min(100, props.value * 100).toFixed(2)}%`}}
      />
      <Show when={props.active && props.value > 0 && props.value < 1}>
        <div
          class="absolute inset-y-0 w-1/3 animate-[shimmer_2s_ease-in-out_infinite] bg-gradient-to-r from-transparent via-white/15 to-transparent"
          style={{left: '-33%'}}
        />
      </Show>
    </div>
  );
}
