import {describe, expect, test} from 'vitest';
import {fmtBytes, fmtRate, fmtETA, fmtPercent, fmtDuration, fmtTimestamp} from './format';

describe('fmtBytes', () => {
  test('bytes', () => expect(fmtBytes(0)).toBe('0 B'));
  test('kilobytes', () => expect(fmtBytes(1500)).toBe('1.5 KB'));
  test('megabytes', () => expect(fmtBytes(1_572_864)).toBe('1.5 MB'));
  test('gigabytes', () => expect(fmtBytes(1_610_612_736)).toBe('1.50 GB'));
});

describe('fmtRate', () => {
  test('idle', () => expect(fmtRate(0)).toBe('—'));
  test('active', () => expect(fmtRate(1500)).toBe('1.5 KB/s'));
});

describe('fmtETA', () => {
  test('infinite when zero rate', () => expect(fmtETA(1000, 0)).toBe('∞'));
  test('seconds', () => expect(fmtETA(500, 100)).toBe('5s'));
  test('minutes', () => expect(fmtETA(60_000, 100)).toBe('10m'));
  test('hours', () => expect(fmtETA(3_600_000, 100)).toBe('10h'));
  test('days', () => expect(fmtETA(86_400_000, 100)).toBe('10d'));
});

describe('fmtPercent', () => {
  test('zero', () => expect(fmtPercent(0)).toBe('0.0%'));
  test('partial', () => expect(fmtPercent(0.7234)).toBe('72.3%'));
  test('complete', () => expect(fmtPercent(1)).toBe('100%'));
});

describe('fmtDuration', () => {
  test('seconds', () => expect(fmtDuration(45)).toBe('45s'));
  test('minutes', () => expect(fmtDuration(125)).toBe('2m'));
  test('hours+minutes', () => expect(fmtDuration(3725)).toBe('1h 2m'));
  test('days+hours', () => expect(fmtDuration(90_061)).toBe('1d 1h'));
});

describe('fmtTimestamp', () => {
  test('zero is em-dash', () => expect(fmtTimestamp(0)).toBe('—'));
  test('renders a date string', () => {
    const out = fmtTimestamp(1700000000);
    expect(out).toMatch(/2023/); // sanity check the year
  });
});
