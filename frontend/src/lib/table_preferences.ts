import type {SortingState, VisibilityState} from '@tanstack/solid-table';

export const TABLE_PREFERENCES_KEY = 'mosaic.table-preferences';
export const TABLE_COLUMNS = ['name', 'total_bytes', 'progress', 'download_rate', 'upload_rate', 'eta', 'peers', 'added_at', 'ratio', 'total_up', 'seeds', 'queue_position'] as const;
const defaultVisibility: VisibilityState = {ratio: false, total_up: false, seeds: false, queue_position: false};
export function loadTablePreferences(): {sorting: SortingState; visibility: VisibilityState} {
  const fallback = {sorting: [{id: 'added_at', desc: true}], visibility: {...defaultVisibility}};
  try {
    const value = JSON.parse(localStorage.getItem(TABLE_PREFERENCES_KEY) ?? 'null');
    if (!value || !Array.isArray(value.sorting) || !value.visibility || typeof value.visibility !== 'object') return fallback;
    const sorting: SortingState = value.sorting.filter((s: any) => s && TABLE_COLUMNS.includes(s.id) && typeof s.desc === 'boolean').slice(0, TABLE_COLUMNS.length);
    const visibility = {...defaultVisibility};
    for (const id of TABLE_COLUMNS) if (typeof value.visibility[id] === 'boolean') visibility[id] = value.visibility[id];
    visibility.name = true;
    return {sorting, visibility};
  } catch { return fallback; }
}
