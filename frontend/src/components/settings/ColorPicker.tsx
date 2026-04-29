import {For} from 'solid-js';

const palette = [
  '#ef4444', // red
  '#f59e0b', // amber
  '#eab308', // yellow
  '#22c55e', // emerald
  '#06b6d4', // cyan
  '#3b82f6', // blue
  '#a855f7', // violet
  '#ec4899', // pink
  '#71717a', // zinc
];

type Props = {
  value: string;
  onSelect: (hex: string) => void;
};

export function ColorPicker(props: Props) {
  return (
    <div class="inline-flex items-center gap-1 rounded-md border border-white/[.06] bg-white/[.02] p-1">
      <For each={palette}>
        {(hex) => (
          <button
            type="button"
            onClick={() => props.onSelect(hex)}
            class="grid h-5 w-5 place-items-center rounded transition-transform hover:scale-110"
            style={{background: hex}}
            aria-label={`Color ${hex}`}
          >
            {props.value === hex && <span class="h-1.5 w-1.5 rounded-full bg-white/90" />}
          </button>
        )}
      </For>
    </div>
  );
}
