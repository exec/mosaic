// Exponential moving average (EMA) smoothing for the speed graph and the
// live status-bar readout. The engine samples bandwidth at ~1 Hz, which is
// jittery enough that a raw line/number flickers; a light EMA settles it
// without hiding real spikes.
//
// alpha ∈ (0,1]: higher = more responsive (less smoothing), lower = smoother
// (more lag). ~0.35 is a light touch at 1 Hz — roughly a 2–3 s settle.
export const SMOOTHING_ALPHA = 0.35;

// emaStep advances a single EMA value: prev is the last smoothed value, raw
// is the new sample. Seed a fresh series by passing the first raw value as
// prev (so it doesn't ramp up from zero).
export function emaStep(prev: number, raw: number, alpha = SMOOTHING_ALPHA): number {
  return prev + alpha * (raw - prev);
}

// emaSeries returns a smoothed copy of values via a forward EMA pass. The
// first point is passed through unchanged so the line starts at the real
// value instead of climbing from zero.
export function emaSeries(values: number[], alpha = SMOOTHING_ALPHA): number[] {
  const out: number[] = new Array(values.length);
  let acc = 0;
  for (let i = 0; i < values.length; i++) {
    acc = i === 0 ? values[i] : emaStep(acc, values[i], alpha);
    out[i] = acc;
  }
  return out;
}
