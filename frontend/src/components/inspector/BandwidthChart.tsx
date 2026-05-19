import {createEffect, onCleanup, onMount} from 'solid-js';
import uPlot from 'uplot';
import 'uplot/dist/uPlot.min.css';
import type {BandwidthRing} from '../../lib/ringbuffer';
import {emaSeries, emaStep} from '../../lib/smoothing';

type Props = {
  ring: BandwidthRing;
  // Reactive counter from the store: increments once per ~1 Hz inspector
  // tick. The chart subscribes to this rather than to the ring itself so it
  // knows exactly one new sample arrived and can update incrementally.
  tick: number;
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

// Target rendered-point count. Long ranges (1 h, 24 h) are downsampled into
// time buckets so the working set the chart ingests stays roughly this size
// regardless of range — at 24 h the raw history is ~86k samples, but the
// chart only ever holds ~TARGET_POINTS buckets. Each bucket is the mean of
// the raw samples that fall in its window.
const TARGET_POINTS = 1500;

// Pick a unit (B/s | KB/s | MB/s | GB/s) based on the largest tick value
// uPlot is asking us to label, then format every split with that same
// unit so the axis reads cleanly instead of "1024 KB/s" or "0 KB/s" for
// a tick range spanning kilo-to-mega.
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

// bucketSeconds is the time width of one downsample bucket for a given
// range — chosen so a full range holds ~TARGET_POINTS buckets. Always >= 1
// so the 5 m range (300 samples) keeps 1:1 resolution.
const bucketSeconds = (rangeSeconds: number) =>
  Math.max(1, Math.ceil(rangeSeconds / TARGET_POINTS));

// A working chart series: parallel mutable arrays the chart appends into
// per tick and hands straight to uPlot.setData. Plus the bookkeeping needed
// to fold a new raw sample into the in-progress trailing bucket.
type ChartData = {
  t: number[];
  down: number[];
  up: number[];
  downSm: number[];
  upSm: number[];
  // Running mean state for the trailing (most recent) bucket.
  bucketStart: number; // floored bucket key of the last point
  bucketCount: number; // raw samples folded into the last bucket so far
  bucketDownSum: number;
  bucketUpSum: number;
};

export function BandwidthChart(props: Props) {
  let container: HTMLDivElement | undefined;
  let chart: uPlot | undefined;
  let data: ChartData | undefined;
  // Range the working `data` was built for. A change means a full rebuild;
  // an unchanged range with a higher tick means an incremental append.
  let builtRange = -1;
  let lastTick = -1;

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

  // fullBuild downsamples the entire in-range history into bucket means and
  // runs one EMA pass per series. O(visible samples) — only runs on mount
  // and on range switch, never per tick.
  const fullBuild = (rangeSeconds: number): ChartData => {
    const bucket = bucketSeconds(rangeSeconds);
    const cutoff = Date.now() / 1000 - rangeSeconds;
    const view = props.ring.snapshot();
    const n = view.t.length;

    const t: number[] = [];
    const down: number[] = [];
    const up: number[] = [];
    let key = NaN;       // current bucket key
    let count = 0;
    let downSum = 0;
    let upSum = 0;

    const flush = () => {
      if (count === 0) return;
      t.push(key);
      down.push(downSum / count);
      up.push(upSum / count);
    };

    for (let i = 0; i < n; i++) {
      const ts = view.t[i];
      if (ts < cutoff) continue;
      const k = Math.floor(ts / bucket) * bucket;
      if (k !== key) {
        flush();
        key = k;
        count = 0;
        downSum = 0;
        upSum = 0;
      }
      count++;
      downSum += view.down[i];
      upSum += view.up[i];
    }
    // Keep the trailing bucket open: don't flush it, carry its running sums
    // so the next per-tick sample folds into the same averaged point.
    const downSm = emaSeries(down);
    const upSm = emaSeries(up);

    return {
      t, down, up, downSm, upSm,
      bucketStart: Number.isNaN(key) ? NaN : key,
      bucketCount: count,
      bucketDownSum: downSum,
      bucketUpSum: upSum,
    };
  };

  // appendSample folds the single newest ring sample into `data`,
  // incrementally — either updating the trailing bucket in place or
  // starting a fresh bucket and advancing the EMA by one step. O(1).
  const appendSample = (d: ChartData, rangeSeconds: number) => {
    const sample = props.ring.newest();
    if (!sample) return;
    const bucket = bucketSeconds(rangeSeconds);
    const k = Math.floor(sample.t / bucket) * bucket;

    if (k === d.bucketStart && d.t.length > 0) {
      // Same bucket: refold the running mean and rewrite the trailing point.
      d.bucketCount++;
      d.bucketDownSum += sample.down;
      d.bucketUpSum += sample.up;
      const last = d.t.length - 1;
      d.down[last] = d.bucketDownSum / d.bucketCount;
      d.up[last] = d.bucketUpSum / d.bucketCount;
      // Re-derive the trailing smoothed value from the point before it so
      // the EMA reflects the corrected bucket mean (one emaStep, not a pass).
      if (last === 0) {
        d.downSm[last] = d.down[last];
        d.upSm[last] = d.up[last];
      } else {
        d.downSm[last] = emaStep(d.downSm[last - 1], d.down[last]);
        d.upSm[last] = emaStep(d.upSm[last - 1], d.up[last]);
      }
    } else {
      // New bucket: append a fresh point and advance the EMA one step.
      const prevLen = d.t.length;
      d.t.push(k);
      d.down.push(sample.down);
      d.up.push(sample.up);
      if (prevLen === 0) {
        d.downSm.push(sample.down);
        d.upSm.push(sample.up);
      } else {
        d.downSm.push(emaStep(d.downSm[prevLen - 1], sample.down));
        d.upSm.push(emaStep(d.upSm[prevLen - 1], sample.up));
      }
      d.bucketStart = k;
      d.bucketCount = 1;
      d.bucketDownSum = sample.down;
      d.bucketUpSum = sample.up;
    }

    // Drop points that have aged out of the visible range. Buckets leave one
    // at a time, so this shift() runs at most once per tick on a ~1.5k array.
    const cutoff = Date.now() / 1000 - rangeSeconds;
    while (d.t.length > 0 && d.t[0] < cutoff) {
      d.t.shift();
      d.down.shift();
      d.up.shift();
      d.downSm.shift();
      d.upSm.shift();
    }
  };

  const asAligned = (d: ChartData): uPlot.AlignedData =>
    [d.t, d.down, d.up, d.downSm, d.upSm];

  onMount(() => {
    if (!container) return;
    const rect = container.getBoundingClientRect();
    data = fullBuild(props.rangeSeconds);
    builtRange = props.rangeSeconds;
    lastTick = props.tick;
    chart = new uPlot(buildOptions(rect.width, rect.height), asAligned(data), container);
  });

  // Update on range switch (full rebuild) or on a new tick (incremental).
  createEffect(() => {
    const range = props.rangeSeconds;
    const tick = props.tick;
    if (!chart) return;

    if (range !== builtRange || !data) {
      // Range switched — recompute the downsampled series from scratch.
      data = fullBuild(range);
      builtRange = range;
      lastTick = tick;
      chart.setData(asAligned(data));
      return;
    }
    if (tick !== lastTick) {
      // One new sample arrived: fold it in incrementally.
      appendSample(data, range);
      lastTick = tick;
      chart.setData(asAligned(data));
    }
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
