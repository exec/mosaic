// Fixed-capacity ring buffer for the ~1 Hz bandwidth history that backs the
// Speed-tab chart. The previous implementation was a plain array capped at
// 86,400 (24 h) with push()/shift(); once full, every tick paid an O(n)
// memmove to drop the oldest element. This structure preallocates three
// parallel Float64Arrays (timestamp / down / up) plus a head index, so a
// push is O(1) regardless of how full the buffer is — the head simply wraps.
//
// Reads return an in-order (oldest → newest) copy via snapshot(), which the
// chart slices and smooths. snapshot() is O(count) but only runs on mount /
// range switch, not per tick.

export type BandwidthSample = {t: number; down: number; up: number};

// In-order view of the ring: parallel arrays sized to the live element count.
export type RingView = {
  t: Float64Array;
  down: Float64Array;
  up: Float64Array;
};

export class BandwidthRing {
  private readonly cap: number;
  private readonly ts: Float64Array;
  private readonly down: Float64Array;
  private readonly up: Float64Array;
  // head points at the slot the NEXT push will write. When count === cap the
  // oldest element lives at head (it's the slot about to be overwritten).
  private head = 0;
  private len = 0;

  constructor(capacity: number) {
    this.cap = capacity;
    this.ts = new Float64Array(capacity);
    this.down = new Float64Array(capacity);
    this.up = new Float64Array(capacity);
  }

  get count(): number {
    return this.len;
  }

  get capacity(): number {
    return this.cap;
  }

  // push appends one sample, overwriting the oldest once full. O(1).
  push(t: number, down: number, up: number): void {
    this.ts[this.head] = t;
    this.down[this.head] = down;
    this.up[this.head] = up;
    this.head = (this.head + 1) % this.cap;
    if (this.len < this.cap) this.len++;
  }

  // clear drops every element without reallocating the backing arrays.
  clear(): void {
    this.head = 0;
    this.len = 0;
  }

  // newest returns the most recently pushed sample, or null when empty.
  newest(): BandwidthSample | null {
    if (this.len === 0) return null;
    const i = (this.head - 1 + this.cap) % this.cap;
    return {t: this.ts[i], down: this.down[i], up: this.up[i]};
  }

  // snapshot copies the live elements into fresh in-order arrays. O(count).
  // Used for the chart's one-time full build; not called per tick.
  snapshot(): RingView {
    const n = this.len;
    const t = new Float64Array(n);
    const down = new Float64Array(n);
    const up = new Float64Array(n);
    // The oldest element is at (head - len); walk forward, wrapping.
    const start = (this.head - this.len + this.cap * 2) % this.cap;
    for (let i = 0; i < n; i++) {
      const j = (start + i) % this.cap;
      t[i] = this.ts[j];
      down[i] = this.down[j];
      up[i] = this.up[j];
    }
    return {t, down, up};
  }
}
