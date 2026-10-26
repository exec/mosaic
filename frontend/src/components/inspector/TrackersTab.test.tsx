import {afterEach, expect, test, vi} from 'vitest';
import {createSignal} from 'solid-js';
import {render} from 'solid-js/web';
import {TrackersTab} from './TrackersTab';
import {api, type DetailDTO} from '../../lib/bindings';

vi.mock('../../lib/bindings', () => ({api: {addTracker: vi.fn(), removeTracker: vi.fn()}}));
vi.mock('solid-sonner', () => ({toast: {success: vi.fn(), error: vi.fn()}}));
let dispose: (() => void) | undefined;
afterEach(() => { dispose?.(); document.body.innerHTML = ''; vi.resetAllMocks(); });

test('tracker drafts survive live ticks, reject duplicate submits, and reset when switching torrents', async () => {
  const [detail, setDetail] = createSignal({id: 'a'} as DetailDTO);
  let finish!: () => void;
  vi.mocked(api.addTracker).mockReturnValue(new Promise<void>((resolve) => { finish = resolve; }));
  dispose = render(() => <TrackersTab canEdit={true} detail={detail()} />, document.body);
  const input = document.querySelector('input')!;
  input.value = 'https://tracker.example/announce';
  input.dispatchEvent(new Event('input', {bubbles: true}));
  setDetail({id: 'a', progress: 0.5} as DetailDTO);
  expect(input.value).toBe('https://tracker.example/announce');
  input.dispatchEvent(new KeyboardEvent('keydown', {key: 'Enter', bubbles: true}));
  input.dispatchEvent(new KeyboardEvent('keydown', {key: 'Enter', bubbles: true}));
  expect(api.addTracker).toHaveBeenCalledTimes(1);
  expect(api.addTracker).toHaveBeenCalledWith('a', input.value);
  setDetail({id: 'b'} as DetailDTO);
  expect(input.value).toBe('');
  finish();
  await vi.waitFor(() => expect(input.disabled).toBe(false));
});

test('revoking edit access removes mutation controls while preserving tracker details and copying', () => {
  const [canEdit, setCanEdit] = createSignal(true);
  const detail = {id: 'a', trackers: [{url: 'http://tracker.example/announce', status: 'Status unavailable', last_announce: 0}]} as DetailDTO;
  dispose = render(() => <TrackersTab detail={detail} canEdit={canEdit()} />, document.body);
  const input = document.querySelector('input')!;
  input.value = 'http://another.example/announce'; input.dispatchEvent(new Event('input', {bubbles: true}));
  setCanEdit(false);
  expect(document.querySelector('input')).toBeNull();
  expect(document.querySelector('[aria-label^="Remove tracker"]')).toBeNull();
  expect(document.querySelector('[title^="Copy tracker URL"]')).toBeTruthy();
  expect(document.body.textContent).toContain('Status unavailable');
  expect(document.body.textContent).toContain('Announce statistics unavailable');
  input.dispatchEvent(new KeyboardEvent('keydown', {key: 'Enter', bubbles: true}));
  expect(api.addTracker).not.toHaveBeenCalled();
  expect(api.removeTracker).not.toHaveBeenCalled();
});
