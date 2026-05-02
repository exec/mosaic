import {createEffect, onCleanup, onMount} from 'solid-js';
import uPlot from 'uplot';
import 'uplot/dist/uPlot.min.css';
import type {BandwidthSample} from '../../lib/store';

type Props = {samples: BandwidthSample[]; rangeSeconds: number};

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
    series: [
      {},
      {label: 'Down', stroke: 'oklch(0.65 0.25 290)', width: 1.5, fill: 'oklch(0.65 0.25 290 / 0.15)'},
      {label: 'Up',   stroke: '#71717a',              width: 1, fill: 'rgba(113,113,122,0.10)'},
    ],
    scales: {x: {time: true}},
  });

  const sliceForRange = () => {
    const cutoff = Date.now() / 1000 - props.rangeSeconds;
    const filtered = props.samples.filter((s) => s.t >= cutoff);
    return [
      filtered.map((s) => s.t),
      filtered.map((s) => s.down),
      filtered.map((s) => s.up),
    ] as uPlot.AlignedData;
  };

  onMount(() => {
    if (!container) return;
    const rect = container.getBoundingClientRect();
    chart = new uPlot(buildOptions(rect.width, rect.height), sliceForRange(), container);
  });

  createEffect(() => {
    if (!chart) return;
    chart.setData(sliceForRange());
  });

  onCleanup(() => chart?.destroy());

  return <div ref={container} class="h-full w-full" />;
}
