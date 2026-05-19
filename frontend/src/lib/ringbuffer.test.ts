import {describe, expect, test} from 'vitest';
import {BandwidthRing} from './ringbuffer';

describe('BandwidthRing', () => {
  test('starts empty', () => {
    const r = new BandwidthRing(4);
    expect(r.count).toBe(0);
    expect(r.capacity).toBe(4);
    expect(r.newest()).toBeNull();
    expect(r.snapshot().t).toHaveLength(0);
  });

  test('push grows count up to capacity', () => {
    const r = new BandwidthRing(3);
    r.push(1, 10, 1);
    expect(r.count).toBe(1);
    r.push(2, 20, 2);
    r.push(3, 30, 3);
    expect(r.count).toBe(3);
    r.push(4, 40, 4);
    expect(r.count).toBe(3); // capped
  });

  test('newest returns the most recently pushed sample', () => {
    const r = new BandwidthRing(2);
    r.push(1, 10, 1);
    expect(r.newest()).toEqual({t: 1, down: 10, up: 1});
    r.push(2, 20, 2);
    expect(r.newest()).toEqual({t: 2, down: 20, up: 2});
    r.push(3, 30, 3); // overwrites slot 0
    expect(r.newest()).toEqual({t: 3, down: 30, up: 3});
  });

  test('snapshot returns elements oldest to newest', () => {
    const r = new BandwidthRing(4);
    r.push(1, 10, 100);
    r.push(2, 20, 200);
    r.push(3, 30, 300);
    const v = r.snapshot();
    expect(Array.from(v.t)).toEqual([1, 2, 3]);
    expect(Array.from(v.down)).toEqual([10, 20, 30]);
    expect(Array.from(v.up)).toEqual([100, 200, 300]);
  });

  test('snapshot stays in order after wrap-around', () => {
    const r = new BandwidthRing(3);
    // Push 5 into a cap-3 ring: oldest two (1, 2) drop off.
    for (let i = 1; i <= 5; i++) r.push(i, i * 10, i);
    const v = r.snapshot();
    expect(Array.from(v.t)).toEqual([3, 4, 5]);
    expect(Array.from(v.down)).toEqual([30, 40, 50]);
  });

  test('many wraps keep the buffer consistent', () => {
    const r = new BandwidthRing(10);
    for (let i = 0; i < 1000; i++) r.push(i, i, i);
    expect(r.count).toBe(10);
    const v = r.snapshot();
    expect(Array.from(v.t)).toEqual([990, 991, 992, 993, 994, 995, 996, 997, 998, 999]);
    expect(r.newest()).toEqual({t: 999, down: 999, up: 999});
  });

  test('clear drops all elements without changing capacity', () => {
    const r = new BandwidthRing(4);
    r.push(1, 1, 1);
    r.push(2, 2, 2);
    r.clear();
    expect(r.count).toBe(0);
    expect(r.capacity).toBe(4);
    expect(r.newest()).toBeNull();
    // Reusable after clear, and indices reset cleanly.
    r.push(9, 90, 900);
    expect(r.count).toBe(1);
    expect(Array.from(r.snapshot().t)).toEqual([9]);
    expect(r.newest()).toEqual({t: 9, down: 90, up: 900});
  });
});
