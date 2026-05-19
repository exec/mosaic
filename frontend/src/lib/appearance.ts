// Runtime appearance state — picks the active theme and mirrors it onto
// `<html data-theme="...">` so the per-theme CSS variable overrides in
// index.css take effect. No provider is needed; the signal is module-level
// because there is only ever one application-wide appearance.

import {createSignal} from 'solid-js';
import {DEFAULT_THEME, isThemeID, type ThemeID} from './themes';

const STORAGE_KEY = 'mosaic.appearance';

// loadStoredTheme reads the persisted choice from localStorage, falling
// back to the default for missing/unknown values. Kept as a pure helper so
// it stays straightforward to test.
export function loadStoredTheme(): ThemeID {
  try {
    const v = localStorage.getItem(STORAGE_KEY);
    return isThemeID(v) ? v : DEFAULT_THEME;
  } catch {
    return DEFAULT_THEME;
  }
}

export function storeTheme(id: ThemeID): void {
  try { localStorage.setItem(STORAGE_KEY, id); } catch { /* private mode, full disk, etc. */ }
}

// applyTheme mirrors the choice onto <html data-theme="..."> so the CSS
// overrides in index.css resolve. Guarded so importing this module under
// SSR-like test environments doesn't blow up if document is missing.
export function applyTheme(id: ThemeID): void {
  if (typeof document !== 'undefined') {
    document.documentElement.dataset.theme = id;
  }
}

const [themeSignal, setThemeSignal] = createSignal<ThemeID>(loadStoredTheme());

// Initialise the DOM the moment this module is loaded so the chosen theme
// is live before the first paint, instead of flashing the default first.
applyTheme(themeSignal());

// theme is the reactive accessor consumers subscribe to.
export const theme = themeSignal;

// setTheme is the single mutator — updates the signal, persists, applies.
export function setTheme(id: ThemeID): void {
  setThemeSignal(id);
  storeTheme(id);
  applyTheme(id);
}
