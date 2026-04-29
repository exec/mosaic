import {describe, expect, test, vi, beforeEach, afterEach} from 'vitest';
import {render} from 'solid-js/web';
import {WebInterfacePane} from './WebInterfacePane';
import type {WebConfigDTO} from '../../lib/bindings';

// solid-sonner's toast is fire-and-forget; mock it out so tests don't try to
// mount the Toaster.
vi.mock('solid-sonner', () => ({
  toast: Object.assign(vi.fn(), {success: vi.fn(), error: vi.fn()}),
}));

const baseCfg: WebConfigDTO = {
  enabled: false,
  port: 8080,
  bind_all: false,
  username: 'admin',
  api_key: '',
};

let host: HTMLDivElement;
let dispose: () => void;

function mount(cfg: WebConfigDTO, handlers: Partial<Parameters<typeof WebInterfacePane>[0]> = {}) {
  const onSetWebConfig = handlers.onSetWebConfig ?? vi.fn().mockResolvedValue(undefined);
  const onSetWebPassword = handlers.onSetWebPassword ?? vi.fn().mockResolvedValue(undefined);
  const onRotateAPIKey = handlers.onRotateAPIKey ?? vi.fn().mockResolvedValue('k123');
  dispose = render(
    () => (
      <WebInterfacePane
        webConfig={cfg}
        onSetWebConfig={onSetWebConfig}
        onSetWebPassword={onSetWebPassword}
        onRotateAPIKey={onRotateAPIKey}
      />
    ),
    host,
  );
  return {onSetWebConfig, onSetWebPassword, onRotateAPIKey};
}

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
});

afterEach(() => {
  dispose?.();
  host.remove();
});

function byTestId(id: string): HTMLElement {
  const el = host.querySelector<HTMLElement>(`[data-testid="${id}"]`);
  if (!el) throw new Error(`missing data-testid="${id}"`);
  return el;
}

function input(id: string): HTMLInputElement {
  return byTestId(id) as HTMLInputElement;
}

function clickSave() {
  const btn = Array.from(host.querySelectorAll<HTMLButtonElement>('button')).find((b) => b.textContent?.trim() === 'Save');
  if (!btn) throw new Error('Save button not found');
  btn.click();
}

describe('WebInterfacePane', () => {
  test('renders current config into the form', () => {
    mount({...baseCfg, port: 9090, username: 'remote', enabled: true});
    expect(input('web-port').value).toBe('9090');
    expect(input('web-username').value).toBe('remote');
    expect(input('web-enabled').checked).toBe(true);
  });

  test('changing the port and clicking Save calls onSetWebConfig with the new port', async () => {
    const {onSetWebConfig, onSetWebPassword} = mount(baseCfg);
    const portEl = input('web-port');
    portEl.value = '9091';
    portEl.dispatchEvent(new Event('input', {bubbles: true}));

    clickSave();
    await tickMicrotasks(3);

    expect(onSetWebConfig).toHaveBeenCalledTimes(1);
    expect(onSetWebConfig).toHaveBeenCalledWith({
      enabled: false, port: 9091, bind_all: false, username: 'admin', api_key: '',
    });
    expect(onSetWebPassword).not.toHaveBeenCalled();
  });

  test('typing a password and clicking Save calls both setters and clears the password field', async () => {
    const {onSetWebConfig, onSetWebPassword} = mount(baseCfg);
    const pw = input('web-password');
    pw.value = 's3cret';
    pw.dispatchEvent(new Event('input', {bubbles: true}));

    clickSave();
    await tickMicrotasks(3);

    expect(onSetWebConfig).toHaveBeenCalledTimes(1);
    expect(onSetWebPassword).toHaveBeenCalledWith('s3cret');
    // Field should be cleared after save.
    expect(input('web-password').value).toBe('');
  });

  test('Generate API key reveals the returned key on screen', async () => {
    mount(baseCfg, {onRotateAPIKey: vi.fn().mockResolvedValue('abc123')});
    byTestId('web-rotate-key').click();
    await tickMicrotasks(3);

    const revealed = byTestId('web-revealed-key');
    expect(revealed.textContent).toContain('abc123');
  });

  test('Save is disabled when port is out of range', () => {
    mount(baseCfg);
    const portEl = input('web-port');
    portEl.value = '80';
    portEl.dispatchEvent(new Event('input', {bubbles: true}));

    const save = Array.from(host.querySelectorAll<HTMLButtonElement>('button')).find((b) => b.textContent?.trim() === 'Save')!;
    expect(save.disabled).toBe(true);
  });

  test('Discard restores form values from props', () => {
    mount({...baseCfg, port: 8080});
    const portEl = input('web-port');
    portEl.value = '9999';
    portEl.dispatchEvent(new Event('input', {bubbles: true}));
    expect(portEl.value).toBe('9999');

    const discard = Array.from(host.querySelectorAll<HTMLButtonElement>('button')).find((b) => b.textContent?.trim() === 'Discard')!;
    discard.click();
    // After Discard, the local signal resets — re-read via the live element.
    expect(input('web-port').value).toBe('8080');
  });
});
