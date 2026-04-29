import {describe, expect, test, vi, beforeEach, afterEach} from 'vitest';
import {render} from 'solid-js/web';
import {StatusBar} from './StatusBar';
import type {GlobalStatsT, WebConfigDTO} from '../../lib/bindings';

const emptyStats: GlobalStatsT = {
  total_torrents: 0,
  active_torrents: 0,
  seeding_torrents: 0,
  total_download_rate: 0,
  total_upload_rate: 0,
  total_peers: 0,
};

const offCfg: WebConfigDTO = {
  enabled: false, port: 8080, bind_all: false, username: 'admin', api_key: '',
};

const onCfg: WebConfigDTO = {
  enabled: true, port: 8080, bind_all: false, username: 'admin', api_key: '',
};

let host: HTMLDivElement;
let dispose: () => void;

beforeEach(() => {
  host = document.createElement('div');
  document.body.appendChild(host);
});

afterEach(() => {
  dispose?.();
  host.remove();
});

describe('StatusBar web indicator', () => {
  test('renders "Web ON :{port}" when webConfig.enabled', () => {
    const onClickWeb = vi.fn();
    dispose = render(
      () => <StatusBar stats={emptyStats} queuedCount={0} webConfig={onCfg} onClickWeb={onClickWeb} />,
      host,
    );
    const pill = host.querySelector('[data-testid="statusbar-web"]');
    expect(pill).toBeTruthy();
    expect(pill!.textContent).toContain('Web ON :8080');
  });

  test('does NOT render the pill when webConfig.enabled is false', () => {
    const onClickWeb = vi.fn();
    dispose = render(
      () => <StatusBar stats={emptyStats} queuedCount={0} webConfig={offCfg} onClickWeb={onClickWeb} />,
      host,
    );
    expect(host.querySelector('[data-testid="statusbar-web"]')).toBeNull();
    expect(host.textContent ?? '').not.toContain('Web ON');
  });

  test('clicking the pill calls onClickWeb', () => {
    const onClickWeb = vi.fn();
    dispose = render(
      () => <StatusBar stats={emptyStats} queuedCount={0} webConfig={onCfg} onClickWeb={onClickWeb} />,
      host,
    );
    const pill = host.querySelector<HTMLButtonElement>('[data-testid="statusbar-web"]')!;
    pill.click();
    expect(onClickWeb).toHaveBeenCalledTimes(1);
  });

  test('reflects port from webConfig', () => {
    const onClickWeb = vi.fn();
    dispose = render(
      () => (
        <StatusBar
          stats={emptyStats}
          queuedCount={0}
          webConfig={{...onCfg, port: 9091}}
          onClickWeb={onClickWeb}
        />
      ),
      host,
    );
    expect(host.textContent).toContain('Web ON :9091');
  });

  test('still renders DHT online indicator', () => {
    const onClickWeb = vi.fn();
    dispose = render(
      () => <StatusBar stats={emptyStats} queuedCount={0} webConfig={offCfg} onClickWeb={onClickWeb} />,
      host,
    );
    expect(host.textContent).toContain('DHT online');
  });
});
