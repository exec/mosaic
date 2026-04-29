export type Theme = 'dark' | 'light' | 'system';
export type ResolvedTheme = 'dark' | 'light';

const STORAGE_KEY = 'mosaic.theme';
const VALID: Theme[] = ['dark', 'light', 'system'];

export function loadStoredTheme(): Theme {
  const v = localStorage.getItem(STORAGE_KEY);
  return VALID.includes(v as Theme) ? (v as Theme) : 'system';
}

export function storeTheme(theme: Theme): void {
  localStorage.setItem(STORAGE_KEY, theme);
}

export function resolveTheme(theme: Theme, systemPrefersDark: boolean): ResolvedTheme {
  if (theme === 'dark') return 'dark';
  if (theme === 'light') return 'light';
  return systemPrefersDark ? 'dark' : 'light';
}
