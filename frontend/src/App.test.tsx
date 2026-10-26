import {afterEach, beforeEach, expect, test, vi} from 'vitest';
import {render} from 'solid-js/web';
import App from './App';
import torrentFixture from '../../backend/api/testdata/dto_snapshots/TorrentDTO_null_category.json';
import type {Torrent} from './lib/bindings';

const mocks = vi.hoisted(() => ({invoke: vi.fn(), listeners: new Map<string, (value: any) => void>()}));
vi.mock('./lib/transport', () => ({transport: {
  invoke: mocks.invoke,
  on: (event: string, callback: (value: any) => void) => { mocks.listeners.set(event, callback); return () => mocks.listeners.delete(event); },
}}));
vi.mock('./lib/runtime', () => ({isWailsRuntime: () => true}));
vi.mock('solid-sonner', () => ({Toaster: () => null, toast: Object.assign(vi.fn(), {success: vi.fn(), error: vi.fn()})}));
// Keep these tests about application selection and keyboard handling, independent of list virtualization.
vi.mock('./components/list/TorrentList', () => ({TorrentList: (props: Parameters<typeof import('./components/list/TorrentList').TorrentList>[0]) => (
  <div>{props.torrents.map((t) => <div data-torrent={t.id} onClick={(e) => props.onSelect(t.id, e, props.torrents.map((t) => t.id))}>{t.name}</div>)}</div>
)}));
let dispose: () => void;
const rows = [
  {...torrentFixture, id: 'a', name: 'Linux A', access: 'owner'},
  {...torrentFixture, id: 'b', name: 'Other', access: 'owner'},
  {...torrentFixture, id: 'c', name: 'Linux C', access: 'owner'},
] as Torrent[];
const key = (target: Element, key: string, ctrlKey = false) => target.dispatchEvent(new KeyboardEvent('keydown', {key, ctrlKey, bubbles: true, cancelable: true}));
function search(text: string) {
  const input = document.querySelector<HTMLInputElement>('[aria-label="Search torrents"]')!;
  input.value = text; input.dispatchEvent(new Event('input', {bubbles: true}));
}
beforeEach(() => {
  mocks.invoke.mockReset(); localStorage.clear();
  mocks.invoke.mockImplementation(async (method: string) => {
    if (method === 'ListTorrents') return structuredClone(rows);
    if (method.startsWith('List')) return [];
    if (method === 'Platform') return 'darwin';
    if (method === 'GnomeTrayStatus') return 'not_applicable';
    if (method === 'GetTorrentRateLimits') return {down_kbps: 0, up_kbps: 0};
    if (method === 'GetTorrentSeedPolicy') return {use_global: true};
    return {};
  });
  dispose = render(() => <App />, document.body);
});
afterEach(() => { dispose?.(); document.body.innerHTML = ''; vi.restoreAllMocks(); vi.unstubAllGlobals(); });

test('select all and its count match visible results', async () => {
  await vi.waitFor(() => expect(document.querySelector('[data-torrent="a"]')).toBeTruthy());
  search('Linux'); key(document.body, 'a', true);
  expect(document.querySelector('[role="status"]')?.textContent).toBe('2 selected');
  key(document.body, ' ');
  expect(mocks.invoke.mock.calls.filter(([method]) => method === 'Pause').map(([, id]) => id)).toEqual(['a', 'c']);
});

test('filtering an existing selection cannot remove hidden torrents', async () => {
  await vi.waitFor(() => expect(document.querySelector('[data-torrent="a"]')).toBeTruthy());
  key(document.body, 'a', true); search('Linux');
  key(document.body, 'Delete');
  expect(document.querySelector('[role="dialog"]')?.textContent).not.toContain('Other');
  Array.from(document.querySelectorAll<HTMLButtonElement>('button')).find((b) => b.textContent === 'Remove · keep files')!.click();
  expect(mocks.invoke.mock.calls.filter(([method]) => method === 'Remove').map(([, id]) => id)).toEqual(['a', 'c']);
});

test('focused controls, editable content, dialogs, and repeated keys keep their own shortcuts', async () => {
  await vi.waitFor(() => expect(document.querySelector('[data-torrent="a"]')).toBeTruthy());
  key(document.body, 'a', true);
  for (const html of ['<input>', '<textarea></textarea>', '<select><option>A</option></select>', '<button>Action</button>', '<div role="separator" tabindex="0">Resize</div>', '<div contenteditable="true"><span>Text</span></div>', '<div role="menu"><span>Item</span></div>']) {
    const host = document.createElement('div'); host.innerHTML = html; document.body.appendChild(host);
    const target = host.querySelector('span') ?? host.firstElementChild!;
    key(target, ' '); key(target, 'Delete'); key(target, 'a', true); host.remove();
  }
  document.body.dispatchEvent(new KeyboardEvent('keydown', {key: 'Delete', repeat: true, bubbles: true}));
  Array.from(document.querySelectorAll('button')).find((b) => b.textContent?.includes('Magnet'))!.click();
  key(document.body, 'Delete');
  expect(mocks.invoke.mock.calls.filter(([method]) => ['Pause', 'Resume', 'Remove'].includes(method))).toEqual([]);
});

test('inspector access follows the selected row and updates when permission changes', async () => {
  await vi.waitFor(() => expect(document.querySelector('[data-torrent="c"]')).toBeTruthy());
  mocks.listeners.get('torrents:tick')!([rows[0], rows[1], {...rows[2], access: 'viewer'}]);
  document.querySelector<HTMLElement>('[data-torrent="c"]')!.click();
  expect(document.body.textContent).toContain('Read-only access');
  key(document.body, ' ');
  expect(mocks.invoke.mock.calls.filter(([method]) => method === 'Pause')).toEqual([]);
  mocks.listeners.get('torrents:tick')!([rows[0], rows[1], {...rows[2], access: 'editor'}]);
  expect(document.body.textContent).not.toContain('Read-only access');
  key(document.body, ' ');
  expect(mocks.invoke).toHaveBeenCalledWith('Pause', 'c');
});
