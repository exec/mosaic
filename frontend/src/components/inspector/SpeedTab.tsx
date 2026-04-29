import {createSignal} from 'solid-js';
import {ToggleGroup} from '@kobalte/core/toggle-group';
import type {BandwidthSample} from '../../lib/store';
import {BandwidthChart} from './BandwidthChart';

const ranges: {value: number; label: string}[] = [
  {value: 5 * 60,        label: '5m'},
  {value: 60 * 60,       label: '1h'},
  {value: 24 * 60 * 60,  label: '24h'},
];

type Props = {samples: BandwidthSample[]};

export function SpeedTab(props: Props) {
  const [range, setRange] = createSignal(5 * 60);

  return (
    <div class="flex h-full flex-col gap-3 p-4">
      <ToggleGroup
        class="inline-flex w-fit items-center gap-px rounded-md border border-white/[.06] bg-white/[.02] p-0.5"
        value={String(range())}
        onChange={(v) => v && setRange(parseInt(v, 10))}
      >
        {ranges.map((r) => (
          <ToggleGroup.Item
            value={String(r.value)}
            class="rounded px-2 py-1 text-xs text-zinc-400 transition-colors duration-100 hover:text-zinc-100 data-[pressed]:bg-white/10 data-[pressed]:text-zinc-100"
          >
            {r.label}
          </ToggleGroup.Item>
        ))}
      </ToggleGroup>
      <div class="flex-1 min-h-0">
        <BandwidthChart samples={props.samples} rangeSeconds={range()} />
      </div>
      <div class="flex items-center justify-between text-[10px] text-zinc-500">
        <span class="inline-flex items-center gap-1.5">
          <span class="h-2 w-2 rounded-full bg-down" /> Download
        </span>
        <span class="inline-flex items-center gap-1.5">
          <span class="h-2 w-2 rounded-full bg-zinc-500" /> Upload
        </span>
      </div>
    </div>
  );
}
