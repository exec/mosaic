import {describe, expect, test, vi, beforeEach, afterEach} from 'vitest';
import {createSignal} from 'solid-js';
import {render} from 'solid-js/web';
import {UpdateToast, __resetUpdateToastDedupeForTesting} from './UpdateToast';
import type {UpdateInfoDTO} from '../../lib/bindings';

const {toastMock} = vi.hoisted(() => ({toastMock: vi.fn()}));
vi.mock('solid-sonner', () => ({
  toast: Object.assign(toastMock, {success: vi.fn(), error: vi.fn()}),
}));

const updateAvailableInfo: UpdateInfoDTO = {
  available: true,
  latest_version: 'v0.8.0',
  asset_url: 'https://example.com/release',
  asset_filename: 'mosaic_v0.8.0_darwin_arm64.tar.gz',
  checked_at: 1700000000,
  current_version: 'v0.7.0',
};

let host: HTMLDivElement;
let dispose: () => void;

beforeEach(() => {
  toastMock.mockClear();
  __resetUpdateToastDedupeForTesting();
  host = document.createElement('div');
  document.body.appendChild(host);
});

afterEach(() => {
  dispose?.();
  host.remove();
});

function tick() {
  return new Promise<void>((r) => queueMicrotask(() => r()));
}

describe('UpdateToast', () => {
  test('does not toast when info is null', () => {
    dispose = render(
      () => <UpdateToast info={null} onInstall={vi.fn()} onDismiss={vi.fn()} />,
      host,
    );
    expect(toastMock).not.toHaveBeenCalled();
  });

  test('does not toast when info is unavailable', () => {
    dispose = render(
      () => (
        <UpdateToast
          info={{...updateAvailableInfo, available: false}}
          onInstall={vi.fn()}
          onDismiss={vi.fn()}
        />
      ),
      host,
    );
    expect(toastMock).not.toHaveBeenCalled();
  });

  test('toasts once when info transitions to available', async () => {
    const [info, setInfo] = createSignal<UpdateInfoDTO | null>(null);
    dispose = render(
      () => <UpdateToast info={info()} onInstall={vi.fn()} onDismiss={vi.fn()} />,
      host,
    );
    expect(toastMock).not.toHaveBeenCalled();

    setInfo(updateAvailableInfo);
    await tick();
    expect(toastMock).toHaveBeenCalledTimes(1);
    expect(toastMock).toHaveBeenCalledWith(
      'Update available — v0.8.0',
      expect.objectContaining({
        action: expect.objectContaining({label: 'Install'}),
        cancel: expect.objectContaining({label: 'Later'}),
      }),
    );
  });

  test('dedupe: same latest_version does not toast twice', async () => {
    const [info, setInfo] = createSignal<UpdateInfoDTO | null>(null);
    dispose = render(
      () => <UpdateToast info={info()} onInstall={vi.fn()} onDismiss={vi.fn()} />,
      host,
    );
    setInfo(updateAvailableInfo);
    await tick();
    setInfo({...updateAvailableInfo}); // identical version, fresh object
    await tick();
    expect(toastMock).toHaveBeenCalledTimes(1);
  });

  test('Install action callback fires onInstall', async () => {
    const onInstall = vi.fn();
    const [info, setInfo] = createSignal<UpdateInfoDTO | null>(null);
    dispose = render(
      () => <UpdateToast info={info()} onInstall={onInstall} onDismiss={vi.fn()} />,
      host,
    );
    setInfo(updateAvailableInfo);
    await tick();
    const opts = toastMock.mock.calls[0][1];
    opts.action.onClick(new MouseEvent('click'));
    expect(onInstall).toHaveBeenCalledTimes(1);
  });
});
