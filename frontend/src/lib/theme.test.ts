import {describe, expect, test, beforeEach} from 'vitest';
import {resolveTheme, loadStoredTheme, storeTheme} from './theme';

describe('theme storage', () => {
  beforeEach(() => localStorage.clear());

  test('default is system when none stored', () => {
    expect(loadStoredTheme()).toBe('system');
  });

  test('persists chosen theme', () => {
    storeTheme('dark');
    expect(loadStoredTheme()).toBe('dark');
  });

  test('rejects invalid stored values', () => {
    localStorage.setItem('mosaic.theme', 'not-a-theme');
    expect(loadStoredTheme()).toBe('system');
  });
});

describe('resolveTheme', () => {
  test('passes through dark', () => expect(resolveTheme('dark', false)).toBe('dark'));
  test('passes through light', () => expect(resolveTheme('light', false)).toBe('light'));
  test('system → dark when system prefers dark', () => expect(resolveTheme('system', true)).toBe('dark'));
  test('system → light when system does not prefer dark', () => expect(resolveTheme('system', false)).toBe('light'));
});
