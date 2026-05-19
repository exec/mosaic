import {createEffect, onCleanup, onMount} from 'solid-js';
import uPlot from 'uplot';
import 'uplot/dist/uPlot.min.css';
import type {BandwidthSample} from '../../lib/store';
import {emaSeries} from '../../lib/smoothing';

type Props = {
  samples: BandwidthSample[];
  rangeSeconds: number;
  // Visibility of the raw and EMA-smoothed line pairs, driven by the
  // clickable legend in SpeedTab.
  showRaw: boolean;
  showSmoothed: boolean;
};

// Line colours. The smoothed lines carry the canonical download/upload
// colours; the raw lines are a faded variant of each so they read as a
// ghost behind the smoothed line — lighter for download, darker for upload.
const COLOR_DOWN = 'oklch(0.65 0.25 290)'; // --color-down (purple)
const COLOR_DOWN_RAW = 'oklch(0.82 0.11 290)'; // lighter, desaturated purple
const COLOR_UP = '#71717a'; // zinc-500
const COLOR_UP_RAW = '#52525b'; // zinc-600, darker
const FILL_DOWN = 'oklch(0.65 0.25 290 / 0.12)';

// Series indices in the uPlot data/series arrays (index 0 is the x axis).
const S_DOWN_RAW = 1;
const S_UP_RAW = 2;
const S_DOWN_SMOOTH = 3;
const S_UP_SMOOTH = 4;

// Pick a unit (B/s | KB/s | MB/s | GB/s) based on the largest tick value
// uPlot is asking us to label, then format every split with that same
// unit so the axis reads cleanly instead of "1024 KB/s" or "0 KB/s" for
// a tick range spanning kilo-to-mega. Pre-fix the formatter hardcoded
// KB/s and dropped fractional precision via toFixed(0) — at 20+ Mbps
// the axis read like "2441 KB/s" instead of "2.4 MB/s", and the user
// (correctly) read that as "wrong units."
const formatRateAxis = (splits: number[]) => {
  const max = Math.max(...splits.map((s) => Math.abs(s)), 1);
  let div = 1;
  let unit = 'B/s';
  if (max >= 1024 * 1024 * 1024) { div = 1024 ** 3; unit = 'GB/s'; }
  else if (max >= 1024 * 1024)   { div = 1024 ** 2; unit = 'MB/s'; }
  else if (max >= 1024)          { div = 1024;       unit = 'KB/s'; }
  // 1 decimal place when scaled, 0 when raw bytes — keeps axis tight.
  const decimals = div === 1 ? 0 : 1;
  return splits.map((v) => `${(v / div).toFixed(decimals)} ${unit}`);
};

export function BandwidthChart(props: Props) {
  let container: HTMLDivElement | undefined;
  let chart: uPlot | undefined;

  const buildOptions = (width: number, height: number): uPlot.Options => ({
    width,
    height,
    cursor: {show: false},
    legend: {show: false},
    axes: [
      {stroke: '#52525b', grid: {show: false}, ticks: {show: false}},
      {stroke: '#52525b', grid: {stroke: 'rgba(255,255,255,0.04)', width: 1}, ticks: {show: false}, size: 56,
       values: (_u, splits) => formatRateAxis(splits)},
    ],
    // Order matters: raw pair first so the smoothed pair paints on top.
    series: [
      {},
      {label: 'Download (raw)', stroke: COLOR_DOWN_RAW, width: 1, show: props.showRaw},
      {label: 'Upload (raw)',   stroke: COLOR_UP_RAW,   width: 1, show: props.showRaw},
      {label: 'Download',       stroke: COLOR_DOWN, width: 1.75, fill: FILL_DOWN, show: props.showSmoothed},
      {label: 'Upload',         stroke: COLOR_UP,   width: 1.75, show: props.showSmoothed},
    ],
    scales: {x: {time: true}},
  });

  const sliceForRange = (): uPlot.AlignedData => {
    const cutoff = Date.now() / 1000 - props.rangeSeconds;
    const filtered = props.samples.filter((s) => s.t >= cutoff);
    const down = filtered.map((s) => s.down);
    const up = filtered.map((s) => s.up);
    return [
      filtered.map((s) => s.t),
      down,
      up,
      emaSeries(down),
      emaSeries(up),
    ];
  };

  onMount(() => {
    if (!container) return;
    const rect = container.getBoundingClientRect();
    chart = new uPlot(buildOptions(rect.width, rect.height), sliceForRange(), container);
  });

  // Re-feed data when the sample set or selected range changes.
  createEffect(() => {
    if (!chart) return;
    chart.setData(sliceForRange());
  });

  // Toggle the raw / smoothed line pairs from the legend.
  createEffect(() => {
    if (!chart) return;
    chart.setSeries(S_DOWN_RAW, {show: props.showRaw});
    chart.setSeries(S_UP_RAW, {show: props.showRaw});
    chart.setSeries(S_DOWN_SMOOTH, {show: props.showSmoothed});
    chart.setSeries(S_UP_SMOOTH, {show: props.showSmoothed});
  });

  onCleanup(() => chart?.destroy());

  return <div ref={container} class="h-full w-full" />;
}
