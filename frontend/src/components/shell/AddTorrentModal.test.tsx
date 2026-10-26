import {afterEach, beforeEach, expect, test, vi} from 'vitest';
import {render} from 'solid-js/web';
import {createSignal} from 'solid-js';
import {AddTorrentModal} from './AddTorrentModal';
import detailFixture from '../../../../backend/api/testdata/dto_snapshots/DetailDTO_full.json';

const mocks = vi.hoisted(() => ({native: false, invoke: vi.fn()}));
vi.mock('../../lib/runtime', () => ({isWailsRuntime: () => mocks.native}));
vi.mock('../../lib/transport', () => ({transport: {invoke: mocks.invoke, on: () => () => {}}}));
let dispose: () => void;
beforeEach(() => {
  mocks.invoke.mockReset();
  mocks.invoke.mockImplementation(async (method: string) => {
    if (method === 'PrepareTorrent') return 'prepared';
    if (method === 'GetTorrentDetail') return {...detailFixture, id: 'prepared', paused: true, metadata_ready: true, verifying: false, files: [detailFixture.files[0], {...detailFixture.files[0], index: 1, path: 'dir/other.bin'}]};
    if (method === 'PickTorrentFile') return btoa('torrent');
    return undefined;
  });
});
afterEach(() => { dispose?.(); document.body.innerHTML = ''; mocks.native = false; });
const input = () => document.querySelector<HTMLInputElement>('[aria-label="Save path"]')!;
const category = (value: string) => {
  const select = document.querySelector<HTMLSelectElement>('[aria-label="Category"]')!;
  select.value = value; select.dispatchEvent(new Event('change', {bubbles: true}));
};
function mount(source: 'magnet' | 'file' = 'magnet') {
  const [open, setOpen] = createSignal(true);
  const onAdded = vi.fn().mockResolvedValue(undefined);
  dispose = render(() => <AddTorrentModal open={open()} initialSource={source} defaultSavePath="/downloads"
    categories={[
      {id: 1, name: 'Linux', default_save_path: '/downloads/linux', color: '#fff'},
      {id: 2, name: 'Other', default_save_path: '', color: '#fff'},
    ]} tags={[]} onClose={() => setOpen(false)} onAdded={onAdded} />, document.body);
  return {setOpen, onAdded};
}
function submit() { document.querySelector('form')!.dispatchEvent(new Event('submit', {bubbles: true, cancelable: true})); }
function enterMagnet() {const magnet = document.querySelector<HTMLTextAreaElement>('[aria-label="Magnet link"]')!;magnet.value = 'magnet:?xt=urn:btih:test';magnet.dispatchEvent(new Event('input', {bubbles: true}));}
const button = (text: string) => Array.from(document.querySelectorAll<HTMLButtonElement>('button')).find((b) => b.textContent === text)!;

test('category destinations follow selection until explicitly edited and reset on reopen', () => {
  const {setOpen} = mount();
  category('1'); expect(input().value).toBe('/downloads/linux');
  category('2'); expect(input().value).toBe('/downloads');
  category('1'); category(''); expect(input().value).toBe('/downloads');
  input().value = '/custom'; input().dispatchEvent(new Event('input', {bubbles: true}));
  category('1'); expect(input().value).toBe('/custom');
  setOpen(false); setOpen(true); category('1'); expect(input().value).toBe('/downloads/linux');
});
test('prepares a magnet at the category destination and only resumes after file confirmation', async () => {
  const {onAdded} = mount();category('1');enterMagnet();submit();
  expect(mocks.invoke).toHaveBeenCalledWith('PrepareTorrent', {magnet: 'magnet:?xt=urn:btih:test', torrent_file: '', save_path: '/downloads/linux'});
  await vi.waitFor(() => expect(button('Start download')).toBeTruthy());
  expect(mocks.invoke).not.toHaveBeenCalledWith('Resume', expect.anything());
  button('None').click();
  expect(button('Start download').disabled).toBe(true);
  const first = document.querySelector<HTMLInputElement>('[aria-label^="Download "]')!;
  first.checked = true;first.dispatchEvent(new Event('change', {bubbles: true}));submit();
  await vi.waitFor(() => expect(onAdded).toHaveBeenCalledWith('prepared', 1, []));
  const calls = mocks.invoke.mock.calls;
  const priorityCall = calls.findIndex(([method]) => method === 'SetFilePriorities');
  expect(calls[priorityCall + 1]).toEqual(['Resume', 'prepared']);
  expect(Object.values(calls[priorityCall][2])).toContain('skip');
});
test('start paused commits choices without resuming', async () => {
  const {onAdded} = mount();enterMagnet();submit();
  await vi.waitFor(() => expect(button('Start download')).toBeTruthy());
  const paused = Array.from(document.querySelectorAll<HTMLInputElement>('input[type="checkbox"]')).find((input) => input.parentElement?.textContent === 'Start paused')!;
  paused.checked = true;paused.dispatchEvent(new Event('change', {bubbles: true}));submit();
  await vi.waitFor(() => expect(onAdded).toHaveBeenCalled());
  expect(mocks.invoke.mock.calls.some(([method]) => method === 'Resume')).toBe(false);
});
test('native picker supplies bytes for preparation without adding immediately', async () => {
  mocks.native = true;mount('file');input().value = '/custom';input().dispatchEvent(new Event('input', {bubbles: true}));category('1');submit();
  await vi.waitFor(() => expect(mocks.invoke).toHaveBeenCalledWith('PrepareTorrent', {magnet: '', torrent_file: btoa('torrent'), save_path: '/custom'}));
  expect(mocks.invoke.mock.calls.some(([method]) => method === 'PickAndAddTorrent')).toBe(false);
});
test('browser uploads are prepared and cancel keeps files', async () => {
  mount('file');category('1');
  const picker = document.querySelector<HTMLInputElement>('input[type="file"]')!;
  Object.defineProperty(picker, 'files', {value: [new File(['torrent'], 'example.torrent')]});picker.dispatchEvent(new Event('change', {bubbles: true}));submit();
  await vi.waitFor(() => expect(button('Start download')).toBeTruthy());
  expect(mocks.invoke).toHaveBeenCalledWith('PrepareTorrent', {magnet: '', torrent_file: btoa('torrent'), save_path: '/downloads/linux'});
  button('Cancel').click();await vi.waitFor(() => expect(mocks.invoke).toHaveBeenCalledWith('Remove', 'prepared', false));
});
test('metadata loading cannot start a download and a failed cancellation leaves the preview paused', async () => {
  mocks.invoke.mockImplementation(async (method) => {if (method === 'PrepareTorrent') return 'prepared';if (method === 'GetTorrentDetail') return {id: 'prepared', metadata_ready: false, verifying: true};if (method === 'Remove') throw Error('offline');return undefined;});
  mount();enterMagnet();submit();await vi.waitFor(() => expect(button('Start download')).toBeTruthy());
  expect(button('Start download').disabled).toBe(true);expect(document.body.textContent).toContain('Finding metadata');
  button('Cancel').click();await vi.waitFor(() => expect(document.body.textContent).toContain('Preview remains paused'));
  expect(document.querySelector('[role="dialog"]')).toBeTruthy();
});

test('an unresolved magnet can be kept paused without starting payload transfers', async () => {
  mocks.invoke.mockImplementation(async (method) => {if (method === 'PrepareTorrent') return 'prepared';if (method === 'GetTorrentDetail') return {id: 'prepared', metadata_ready: false, verifying: true};return undefined;});
  const {onAdded} = mount();enterMagnet();submit();await vi.waitFor(() => expect(button('Start download')).toBeTruthy());
  const paused = Array.from(document.querySelectorAll<HTMLInputElement>('input[type="checkbox"]')).find((input) => input.parentElement?.textContent === 'Start paused')!;
  paused.checked = true;paused.dispatchEvent(new Event('change', {bubbles: true}));
  expect(button('Add paused').disabled).toBe(false);submit();
  await vi.waitFor(() => expect(onAdded).toHaveBeenCalledWith('prepared', null, []));
  expect(mocks.invoke.mock.calls.some(([method]) => method === 'Resume' || method === 'SetFilePriorities')).toBe(false);
});
