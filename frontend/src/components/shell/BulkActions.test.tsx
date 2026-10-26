import {afterEach, expect, test, vi} from 'vitest';
import {render} from 'solid-js/web';
import {BulkActions} from './BulkActions';
import {RemoveTorrentModal} from './RemoveTorrentModal';
import type {Torrent} from '../../lib/bindings';
const mocks = vi.hoisted(() => ({invoke: vi.fn(), success: vi.fn(), error: vi.fn()}));
vi.mock('../../lib/transport', () => ({transport: {invoke: mocks.invoke}}));
vi.mock('solid-sonner', () => ({toast: {success: mocks.success, error: mocks.error}}));
let dispose: () => void;
afterEach(() => {dispose?.();document.body.innerHTML = '';vi.resetAllMocks();});
test('bulk actions skip viewers and report each failed torrent by name', async () => {
  mocks.invoke.mockImplementation(async (_method, id) => {if (id === 'b') throw Error('offline');});
  const torrents = [{id: 'a', name: 'A', access: 'owner'}, {id: 'b', name: 'B', access: 'editor'}, {id: 'c', name: 'C', access: 'viewer'}] as Torrent[];
  dispose = render(() => <BulkActions torrents={torrents} categories={[]} tags={[]} onRemove={() => {}} onClear={() => {}} />, document.body);
  Array.from(document.querySelectorAll<HTMLButtonElement>('button')).find((b) => b.textContent === 'Pause')!.click();
  await vi.waitFor(() => expect(mocks.error).toHaveBeenCalled());
  expect(mocks.invoke.mock.calls).toEqual([['Pause', 'a'], ['Pause', 'b']]);
  expect(mocks.error.mock.calls[0][0]).toContain('1 read-only skipped');
  expect(mocks.error.mock.calls[0][0]).toContain('B: offline');
});
test('removal defaults to keeping files and delete requires explicit selection', async () => {
  const onRemove = vi.fn().mockResolvedValue(undefined);
  dispose = render(() => <RemoveTorrentModal torrents={[{id: 'a', name: 'A', access: 'owner'} as Torrent]} onClose={() => {}} onRemove={onRemove} />, document.body);
  const checkbox = document.querySelector<HTMLInputElement>('[aria-label="Delete downloaded files"]')!;
  expect(checkbox.checked).toBe(false);
  checkbox.checked = true;checkbox.dispatchEvent(new Event('change', {bubbles: true}));
  Array.from(document.querySelectorAll<HTMLButtonElement>('button')).find((b) => b.textContent === 'Remove & delete files')!.click();
  expect(onRemove).toHaveBeenCalledWith(true);
});
