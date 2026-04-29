import {describe, expect, test, vi, beforeEach, afterEach} from 'vitest';
import {render} from 'solid-js/web';
import {UpdatesPane} from './UpdatesPane';
import type {UpdaterConfigDTO, UpdateInfoDTO} from '../../lib/bindings';

vi.mock('solid-sonner', () => ({
  toast: Object.assign(vi.fn(), {success: vi.fn(), error: vi.fn()}),
}));

const baseCfg: UpdaterConfigDTO = {
  enabled: true,
  channel: 'stable',
  last_checked_at: 0,
  last_seen_version: '',
};

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

function tickMicrotasks(n = 1) {
  return new Promise<void>((resolve) => {
    let count = n;
    const step = () => {
      if (count-- <= 0) resolve();
      else queueMicrotask(step);
    };
    step();
  });
}

beforeEach(() => {
  host = document.createElement('div');
  document.body.appendChild(host);
  // Ensure isWailsRuntime() returns false unless a test sets it.
  delete (window as any).runtime;
});

afterEach(() => {
  dispose?.();
  host.remove();
  delete (window as any).runtime;
});

function byTestId(id: string): HTMLElement {
  const el = host.querySelector<HTMLElement>(`[data-testid="${id}"]`);
  if (!el) throw new Error(`missing data-testid="${id}"`);
  return el;
}

describe('UpdatesPane', () => {
  test('shows current version and "never" when last_checked_at is 0', () => {
    dispose = render(
      () => (
        <UpdatesPane
          config={baseCfg}
          info={null}
          appVersion="v0.7.0"
          onSet={vi.fn()}
          onCheck={vi.fn()}
          onInstall={vi.fn()}
        />
      ),
      host,
    );
    expect(byTestId('updater-version').textContent).toBe('v0.7.0');
    expect(byTestId('updater-last-checked').textContent).toBe('never');
  });

  test('Save calls onSet with channel change', async () => {
    const onSet = vi.fn().mockResolvedValue(undefined);
    dispose = render(
      () => (
        <UpdatesPane
          config={baseCfg}
          info={null}
          appVersion="v0.7.0"
          onSet={onSet}
          onCheck={vi.fn()}
          onInstall={vi.fn()}
        />
      ),
      host,
    );
    (byTestId('updater-channel-beta') as HTMLInputElement).click();
    (byTestId('updater-save') as HTMLButtonElement).click();
    await tickMicrotasks(3);
    expect(onSet).toHaveBeenCalledTimes(1);
    expect(onSet).toHaveBeenCalledWith(expect.objectContaining({channel: 'beta', enabled: true}));
  });

  test('Check now calls onCheck', async () => {
    const onCheck = vi.fn().mockResolvedValue({
      available: false,
      latest_version: '',
      asset_url: '',
      asset_filename: '',
      checked_at: 0,
      current_version: 'v0.7.0',
    });
    dispose = render(
      () => (
        <UpdatesPane
          config={baseCfg}
          info={null}
          appVersion="v0.7.0"
          onSet={vi.fn()}
          onCheck={onCheck}
          onInstall={vi.fn()}
        />
      ),
      host,
    );
    (byTestId('updater-check') as HTMLButtonElement).click();
    await tickMicrotasks(3);
    expect(onCheck).toHaveBeenCalled();
  });

  test('Install button rendered when update available; click invokes onInstall in Wails', async () => {
    (window as any).runtime = {};
    const onInstall = vi.fn().mockResolvedValue(undefined);
    dispose = render(
      () => (
        <UpdatesPane
          config={baseCfg}
          info={updateAvailableInfo}
          appVersion="v0.7.0"
          onSet={vi.fn()}
          onCheck={vi.fn()}
          onInstall={onInstall}
        />
      ),
      host,
    );
    const btn = byTestId('updater-install') as HTMLButtonElement;
    expect(btn.disabled).toBe(false);
    btn.click();
    await tickMicrotasks(3);
    expect(onInstall).toHaveBeenCalledTimes(1);
  });

  test('Install button disabled outside Wails (browser shell)', () => {
    // Note: beforeEach already deletes window.runtime.
    dispose = render(
      () => (
        <UpdatesPane
          config={baseCfg}
          info={updateAvailableInfo}
          appVersion="v0.7.0"
          onSet={vi.fn()}
          onCheck={vi.fn()}
          onInstall={vi.fn()}
        />
      ),
      host,
    );
    const btn = byTestId('updater-install') as HTMLButtonElement;
    expect(btn.disabled).toBe(true);
    expect(host.textContent).toContain('Install must run from the desktop app');
  });
});
