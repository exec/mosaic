import {describe, expect, test, beforeEach} from 'vitest';
import {loadStoredTheme, storeTheme, applyTheme} from './appearance';
import {THEMES, isThemeID} from './themes';

describe('theme catalog', () => {
  test('contains 6 themes', () => {
    expect(THEMES).toHaveLength(6);
  });

  test('every theme has a unique id', () => {
    const ids = THEMES.map((t) => t.id);
    expect(new Set(ids).size).toBe(ids.length);
  });

  test('isThemeID gates valid ids and rejects unknown values', () => {
    expect(isThemeID('amethyst')).toBe(true);
    expect(isThemeID('mono')).toBe(true);
    expect(isThemeID('not-a-theme')).toBe(false);
    expect(isThemeID('')).toBe(false);
    expect(isThemeID(null)).toBe(false);
    expect(isThemeID(undefined)).toBe(false);
  });
});

describe('theme storage', () => {
  beforeEach(() => localStorage.clear());

  test('defaults to amethyst when nothing is stored', () => {
    expect(loadStoredTheme()).toBe('amethyst');
  });

  test('round-trips a valid choice through localStorage', () => {
    storeTheme('verdant');
    expect(loadStoredTheme()).toBe('verdant');
  });

  test('rejects an unknown stored value and falls back to default', () => {
    localStorage.setItem('mosaic.appearance', 'banana');
    expect(loadStoredTheme()).toBe('amethyst');
  });
});

describe('applyTheme', () => {
  test('writes the id to <html data-theme>', () => {
    applyTheme('cobalt');
    expect(document.documentElement.dataset.theme).toBe('cobalt');
    applyTheme('ember');
    expect(document.documentElement.dataset.theme).toBe('ember');
  });
});
