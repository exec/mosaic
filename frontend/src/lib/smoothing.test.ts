import {describe, expect, test} from 'vitest';
import {emaStep, emaSeries, SMOOTHING_ALPHA} from './smoothing';

describe('emaStep', () => {
  test('moves prev toward raw by alpha', () => {
    expect(emaStep(0, 10, 0.5)).toBe(5);
    expect(emaStep(10, 20, 0.25)).toBe(12.5);
  });

  test('is a no-op when prev already equals raw', () => {
    expect(emaStep(42, 42)).toBe(42);
  });

  test('alpha=1 snaps straight to the raw value', () => {
    expect(emaStep(0, 999, 1)).toBe(999);
  });
});

describe('emaSeries', () => {
  test('empty input yields empty output', () => {
    expect(emaSeries([])).toEqual([]);
  });

  test('passes the first sample through unchanged (no ramp from zero)', () => {
    expect(emaSeries([500])).toEqual([500]);
    expect(emaSeries([500, 500, 500])[0]).toBe(500);
  });

  test('a constant series stays constant', () => {
    expect(emaSeries([7, 7, 7, 7])).toEqual([7, 7, 7, 7]);
  });

  test('preserves length', () => {
    const input = [1, 2, 3, 4, 5, 6];
    expect(emaSeries(input)).toHaveLength(input.length);
  });

  test('dampens variance — smoothed values stay within the input range', () => {
    const spiky = [0, 100, 0, 100, 0, 100];
    const smoothed = emaSeries(spiky);
    for (const v of smoothed) {
      expect(v).toBeGreaterThanOrEqual(0);
      expect(v).toBeLessThanOrEqual(100);
    }
    // The alternating spikes should be visibly pulled toward the mean.
    expect(Math.max(...smoothed.slice(1))).toBeLessThan(100);
    expect(Math.min(...smoothed.slice(1))).toBeGreaterThan(0);
  });

  test('default alpha is the exported constant', () => {
    expect(emaSeries([0, 10])[1]).toBeCloseTo(emaStep(0, 10, SMOOTHING_ALPHA));
  });
});
