import {afterEach, beforeEach, describe, expect, test, vi} from 'vitest';
import {render} from 'solid-js/web';
import {createSignal} from 'solid-js';
import {PeersTab} from './PeersTab';
import {api, type DetailDTO, type PeerDTO} from '../../lib/bindings';

vi.mock('../../lib/bindings', () => ({api: {listBlockedPeers: vi.fn(), setPeerBlocked: vi.fn()}}));
vi.mock('solid-sonner', () => ({toast: {success: vi.fn(), error: vi.fn()}}));
const peer: PeerDTO = {ip: '2001:db8::1', port: 6881, client: 'Example client', flags: '', country: '', progress: 0.5,
  download_rate: 1024, upload_rate: 2048, downloaded: 4096, uploaded: 8192};
const detail = (peers: PeerDTO[]) => ({id: 'torrent', peers_list: peers} as DetailDTO);
let host: HTMLDivElement;
let dispose: () => void;
const button = (text: string) => Array.from(host.querySelectorAll('button')).find((b) => b.textContent?.includes(text))!;
beforeEach(() => {
  vi.resetAllMocks();
  vi.mocked(api.listBlockedPeers).mockResolvedValue([]);
  vi.mocked(api.setPeerBlocked).mockResolvedValue();
  host = document.createElement('div'); document.body.appendChild(host);
});
afterEach(() => { dispose?.(); host.remove(); });

describe('peer inspector', () => {
  test('expands details and keeps the selected peer open across reordered ticks', () => {
    const [value, setValue] = createSignal(detail([peer]));
    dispose = render(() => <PeersTab detail={value()} canBlock={false} />, host);
    button('2001:db8::1').click();
    expect(host.textContent).toContain('4.0 KB');
    expect(host.textContent).toContain('8.0 KB');
    expect(host.textContent).toContain('reset on reconnect');
    setValue(detail([{...peer, ip: '192.0.2.2', download_rate: 4096}, {...peer, uploaded: 16384}]));
    expect(button('2001:db8::1').getAttribute('aria-expanded')).toBe('true');
    expect(host.textContent).toContain('16.0 KB');
    expect(host.textContent).not.toContain('Block IP');
    expect(api.listBlockedPeers).not.toHaveBeenCalled();
  });

  test('filters and sorts peers by their live counters', () => {
    dispose = render(() => <PeersTab detail={detail([peer, {...peer, ip: '192.0.2.2', client: 'Other client', uploaded: 16384}])} canBlock={false} />, host);
    const select = host.querySelector('select')!;
    select.value = 'uploaded'; select.dispatchEvent(new Event('change', {bubbles: true}));
    expect(host.querySelector('button')!.textContent).toContain('192.0.2.2');
    const search = host.querySelector('input')!;
    search.value = 'EXAMPLE'; search.dispatchEvent(new Event('input', {bubbles: true}));
    expect(host.textContent).toContain('2001:db8::1');
    expect(host.textContent).not.toContain('192.0.2.2');
    search.value = 'absent'; search.dispatchEvent(new Event('input', {bubbles: true}));
    expect(host.textContent).toContain('No matching peers');
  });

  test('blocks an IP and offers unblock even after the peer disappears', async () => {
    dispose = render(() => <PeersTab detail={detail([peer])} canBlock={true} />, host);
    await vi.waitFor(() => expect(api.listBlockedPeers).toHaveBeenCalled());
    button(peer.ip).click(); button('Block IP').click();
    await vi.waitFor(() => expect(host.textContent).toContain('Blocked IPs · 1'));
    expect(api.setPeerBlocked).toHaveBeenCalledWith(peer.ip, true);
    expect(host.textContent).toContain('No connected peers');
    button('Unblock').click();
    await vi.waitFor(() => expect(api.setPeerBlocked).toHaveBeenLastCalledWith(peer.ip, false));
  });
  test('a rejected block leaves the peer connected and the action available', async () => {
    vi.mocked(api.setPeerBlocked).mockRejectedValue(new Error('forbidden'));
    dispose = render(() => <PeersTab detail={detail([peer])} canBlock={true} />, host);
    button(peer.ip).click();
    await vi.waitFor(() => expect(button('Block IP').disabled).toBe(false));
    button('Block IP').click();
    await vi.waitFor(() => expect(button('Block IP').disabled).toBe(false));
    expect(host.textContent).toContain(peer.ip);
    expect(host.textContent).not.toContain('Blocked IPs');
  });

});
