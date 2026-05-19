import {createSignal} from 'solid-js';
import {ToggleGroup} from '@kobalte/core/toggle-group';
import type {BandwidthRing} from '../../lib/ringbuffer';
import {BandwidthChart} from './BandwidthChart';

const ranges: {value: number; label: string}[] = [
  {value: 5 * 60,        label: '5m'},
  {value: 60 * 60,       label: '1h'},
  {value: 24 * 60 * 60,  label: '24h'},
];

type Props = {ring: BandwidthRing; tick: number};

// LegendToggle is a clickable key entry that shows/hides one line variant.
// The swatch is a short line whose thickness mirrors the chart (thin = raw,
// thick = smoothed); the whole chip dims when its variant is hidden.
function LegendToggle(props: {label: string; active: boolean; thick: boolean; onClick: () => void}) {
  return (
    <button
      type="button"
      onClick={props.onClick}
      aria-pressed={props.active}
      class="inline-flex items-center gap-1.5 rounded px-1.5 py-0.5 transition-colors duration-100 hover:bg-white/[.05]"
      classList={{'opacity-35': !props.active}}
    >
      <span
        class="w-3.5 rounded-full bg-zinc-300"
        style={{height: props.thick ? '2.5px' : '1px'}}
      />
      {props.label}
    </button>
  );
}

export function SpeedTab(props: Props) {
  const [range, setRange] = createSignal(5 * 60);
  // Both line variants are shown by default. The guards below keep at least
  // one visible — toggling the last remaining variant off is a no-op.
  const [showRaw, setShowRaw] = createSignal(true);
  const [showSmoothed, setShowSmoothed] = createSignal(true);

  const toggleRaw = () => {
    if (showRaw() && !showSmoothed()) return;
    setShowRaw((v) => !v);
  };
  const toggleSmoothed = () => {
    if (showSmoothed() && !showRaw()) return;
    setShowSmoothed((v) => !v);
  };

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
        <BandwidthChart
          ring={props.ring}
          tick={props.tick}
          rangeSeconds={range()}
          showRaw={showRaw()}
          showSmoothed={showSmoothed()}
        />
      </div>
      <div class="flex items-center justify-between text-[10px] text-zinc-500">
        <div class="flex items-center gap-3">
          <span class="inline-flex items-center gap-1.5">
            <span class="h-2 w-2 rounded-full bg-down" /> Download
          </span>
          <span class="inline-flex items-center gap-1.5">
            <span class="h-2 w-2 rounded-full bg-zinc-500" /> Upload
          </span>
        </div>
        <div class="flex items-center gap-0.5">
          <LegendToggle label="Raw" active={showRaw()} thick={false} onClick={toggleRaw} />
          <LegendToggle label="Smoothed" active={showSmoothed()} thick onClick={toggleSmoothed} />
        </div>
      </div>
    </div>
  );
}
