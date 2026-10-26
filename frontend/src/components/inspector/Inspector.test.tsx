import {afterEach, beforeEach, expect, test, vi} from 'vitest';
import {createSignal} from 'solid-js';
import {render} from 'solid-js/web';
import {Inspector} from './Inspector';
import {api, type DetailDTO} from '../../lib/bindings';
import {BandwidthRing} from '../../lib/ringbuffer';
import fixture from '../../../../backend/api/testdata/dto_snapshots/DetailDTO_full.json';

vi.mock('../../lib/bindings', () => ({api: {
  getTorrentRateLimits: vi.fn(), getTorrentSeedPolicy: vi.fn(), setTorrentRateLimits: vi.fn(), setTorrentSeedPolicy: vi.fn(),
}}));
vi.mock('solid-sonner', () => ({toast: {success: vi.fn(), error: vi.fn()}}));
let dispose: () => void;
beforeEach(() => {
  vi.resetAllMocks();
  vi.mocked(api.getTorrentRateLimits).mockResolvedValue({down_kbps: 10, up_kbps: 20});
  vi.mocked(api.getTorrentSeedPolicy).mockResolvedValue({use_global: true, ratio_limit: null, time_min_limit: null});
});
afterEach(() => { dispose?.(); document.body.innerHTML = ''; });

test('overview values remain readable while all torrent editing is disabled for viewers', async () => {
  const [canEdit, setCanEdit] = createSignal(false);
  const onToggleSequential = vi.fn();
  const [detail, setDetail] = createSignal({...fixture, completed: true} as DetailDTO);
  dispose = render(() => <Inspector open canEdit={canEdit()} canBlockPeers={false} detail={detail()} downloadRate={0} tab="overview"
    bandwidthRing={new BandwidthRing(60)} bandwidthTick={0} sequential={false} onTabChange={() => {}} onClose={() => {}}
    onSetFilePriority={() => {}} onToggleSequential={onToggleSequential} />, document.body);
  await vi.waitFor(() => expect(document.querySelector<HTMLInputElement>('input[type="number"]')?.value).toBe('10'));
  expect(document.body.textContent).toContain('Read-only access');
  expect(Array.from(document.querySelectorAll('fieldset')).every((el) => el.disabled)).toBe(true);
  expect(document.querySelector('[role="switch"]')!.closest('fieldset')?.disabled).toBe(true);
  const toggle = document.querySelector<HTMLButtonElement>('[role="switch"]')!;
  toggle.click();
  expect(onToggleSequential).not.toHaveBeenCalled();
  expect(document.querySelector('[title="Copy magnet link"]')!.matches(':disabled')).toBe(false);
  setCanEdit(true);
  expect(document.body.textContent).not.toContain('Read-only access');
  toggle.click(); expect(onToggleSequential).toHaveBeenCalledTimes(1);
  const input = document.querySelector<HTMLInputElement>('input[type="number"]')!;
  input.value = '100'; input.dispatchEvent(new Event('input', {bubbles: true}));
  const save = Array.from(document.querySelectorAll('button')).find((b) => b.textContent === 'Save seeding policy')!;
  setCanEdit(false);
  input.dispatchEvent(new Event('blur'));
  save.click();
  expect(api.setTorrentRateLimits).not.toHaveBeenCalled();
  expect(api.setTorrentSeedPolicy).not.toHaveBeenCalled();
  setDetail({...detail(), progress: 0.75});
  expect(api.getTorrentRateLimits).toHaveBeenCalledTimes(1);
  expect(api.getTorrentSeedPolicy).toHaveBeenCalledTimes(1);
});
