import {createEffect, onCleanup, onMount} from 'solid-js';
import uPlot from 'uplot';
import 'uplot/dist/uPlot.min.css';
import type {BandwidthSample} from '../../lib/store';

type Props = {samples: BandwidthSample[]; rangeSeconds: number};

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
      {stroke: '#52525b', grid: {stroke: 'rgba(255,255,255,0.04)', width: 1}, ticks: {show: false}, size: 50,
       values: (_u, splits) => splits.map((v) => `${(v / 1024).toFixed(0)} KB/s`)},
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
