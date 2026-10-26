import {describe, expect, test, beforeEach, vi} from 'vitest';
import {makeHTTPTransport} from './http_transport';

type FetchCall = {url: string; init: RequestInit};

interface MockResp {
  ok?: boolean;
  status?: number;
  body?: any;
}

function makeMockFetch(responses: MockResp[]) {
  const calls: FetchCall[] = [];
  let i = 0;
  const fetchImpl = (async (url: string | URL | Request, init: RequestInit = {}) => {
    calls.push({url: String(url), init});
    const r = responses[Math.min(i++, responses.length - 1)] ?? {ok: true, body: {}};
    const bodyText = r.body === undefined ? '' : (typeof r.body === 'string' ? r.body : JSON.stringify(r.body));
    return {
      ok: r.ok ?? true,
      status: r.status ?? 200,
      text: async () => bodyText,
    } as Response;
  }) as unknown as typeof fetch;
  return {fetchImpl, calls};
}

class MockWS {
  static instances: MockWS[] = [];
  url: string;
  onopen: ((ev: Event) => void) | null = null;
  onmessage: ((ev: MessageEvent) => void) | null = null;
  onclose: ((ev: CloseEvent) => void) | null = null;
  onerror: ((ev: Event) => void) | null = null;
  closed = false;
  constructor(url: string) {
    this.url = url;
    MockWS.instances.push(this);
  }
  // Simulate a server frame.
  push(envelope: any) {
    this.onmessage?.({data: JSON.stringify(envelope)} as MessageEvent);
  }
  close() {
    this.closed = true;
    this.onclose?.({} as CloseEvent);
  }
}

describe('http_transport.invoke', () => {
  beforeEach(() => { MockWS.instances = []; });

  test('AddMagnet → POST /api/torrents/magnet, unwraps {id}', async () => {
    const {fetchImpl, calls} = makeMockFetch([{ok: true, body: {id: 'abc'}}]);
    const t = makeHTTPTransport('http://localhost', {fetchImpl, wsCtor: MockWS as any});
    const id = await t.invoke<string>('AddMagnet', 'magnet:?xt=urn:btih:abc', '/tmp');
    expect(id).toBe('abc');
    expect(calls).toHaveLength(1);
    expect(calls[0].url).toBe('http://localhost/api/torrents/magnet');
    expect(calls[0].init.method).toBe('POST');
    expect(calls[0].init.headers).toMatchObject({'Content-Type': 'application/json'});
    expect(JSON.parse(calls[0].init.body as string)).toEqual({magnet: 'magnet:?xt=urn:btih:abc', save_path: '/tmp'});
    expect(calls[0].init.credentials).toBe('include');
  });

  test('ListTorrents → GET /api/torrents (no body)', async () => {
    const {fetchImpl, calls} = makeMockFetch([{ok: true, body: [{id: 'x'}]}]);
    const t = makeHTTPTransport('http://localhost', {fetchImpl, wsCtor: MockWS as any});
    const rows = await t.invoke<any[]>('ListTorrents');
    expect(rows).toEqual([{id: 'x'}]);
    expect(calls[0].url).toBe('http://localhost/api/torrents');
    expect(calls[0].init.method).toBe('GET');
    expect(calls[0].init.body).toBeUndefined();
  });

  test('Pause → POST /api/torrents/{id}/pause', async () => {
    const {fetchImpl, calls} = makeMockFetch([{ok: true, body: {ok: true}}]);
    const t = makeHTTPTransport('http://localhost', {fetchImpl, wsCtor: MockWS as any});
    const result = await t.invoke<void>('Pause', 'abc123');
    expect(result).toBeUndefined();
    expect(calls[0].url).toBe('http://localhost/api/torrents/abc123/pause');
    expect(calls[0].init.method).toBe('POST');
  });

  test('Remove with deleteFiles=true appends ?delete=1', async () => {
    const {fetchImpl, calls} = makeMockFetch([{ok: true, body: {ok: true}}]);
    const t = makeHTTPTransport('http://localhost', {fetchImpl, wsCtor: MockWS as any});
    await t.invoke<void>('Remove', 'abc', true);
    expect(calls[0].url).toBe('http://localhost/api/torrents/abc?delete=1');
    expect(calls[0].init.method).toBe('DELETE');
  });

  test('AddTorrentBytes uses multipart and unwraps {id}', async () => {
    const {fetchImpl, calls} = makeMockFetch([{ok: true, body: {id: 'fromfile'}}]);
    const t = makeHTTPTransport('http://localhost', {fetchImpl, wsCtor: MockWS as any});
    const bytes = new Uint8Array([1, 2, 3, 4]);
    const id = await t.invoke<string>('AddTorrentBytes', bytes, '/tmp');
    expect(id).toBe('fromfile');
    expect(calls[0].init.body).toBeInstanceOf(FormData);
    const fd = calls[0].init.body as FormData;
    expect(fd.get('save_path')).toBe('/tmp');
    expect(fd.get('file')).toBeInstanceOf(File);
  });

  test('ToggleAltSpeed unwraps to boolean', async () => {
    const {fetchImpl} = makeMockFetch([{ok: true, body: {alt_active: true}}]);
    const t = makeHTTPTransport('http://localhost', {fetchImpl, wsCtor: MockWS as any});
    await expect(t.invoke<boolean>('ToggleAltSpeed')).resolves.toBe(true);
  });

  test('GetDefaultSavePath unwraps {path}', async () => {
    const {fetchImpl} = makeMockFetch([{ok: true, body: {path: '/downloads'}}]);
    const t = makeHTTPTransport('http://localhost', {fetchImpl, wsCtor: MockWS as any});
    await expect(t.invoke<string>('GetDefaultSavePath')).resolves.toBe('/downloads');
  });

  test('RotateAPIKey unwraps {api_key}', async () => {
    const {fetchImpl} = makeMockFetch([{ok: true, body: {api_key: 'k123'}}]);
    const t = makeHTTPTransport('http://localhost', {fetchImpl, wsCtor: MockWS as any});
    await expect(t.invoke<string>('RotateAPIKey')).resolves.toBe('k123');
  });

  test('ListFiltersByFeed builds /api/feeds/{id}/filters', async () => {
    const {fetchImpl, calls} = makeMockFetch([{ok: true, body: []}]);
    const t = makeHTTPTransport('http://localhost', {fetchImpl, wsCtor: MockWS as any});
    await t.invoke('ListFiltersByFeed', 7);
    expect(calls[0].url).toBe('http://localhost/api/feeds/7/filters');
  });

  test('CreateCategory unwraps {id} as number', async () => {
    const {fetchImpl} = makeMockFetch([{ok: true, body: {id: 42}}]);
    const t = makeHTTPTransport('http://localhost', {fetchImpl, wsCtor: MockWS as any});
    await expect(t.invoke<number>('CreateCategory', 'movies', '/m', '#abc')).resolves.toBe(42);
  });

  test('AppVersion unwraps {version} as bare string', async () => {
    const {fetchImpl, calls} = makeMockFetch([{ok: true, body: {version: 'v0.7.0'}}]);
    const t = makeHTTPTransport('http://localhost', {fetchImpl, wsCtor: MockWS as any});
    await expect(t.invoke<string>('AppVersion')).resolves.toBe('v0.7.0');
    expect(calls[0].url).toBe('http://localhost/api/version');
    expect(calls[0].init.method).toBe('GET');
  });

  test('CheckForUpdate POSTs /api/updater/check and returns full UpdateInfoDTO', async () => {
    const payload = {
      available: true,
      latest_version: 'v0.8.0',
      asset_url: 'https://example.com/release',
      asset_filename: 'mosaic_v0.8.0_darwin_arm64.tar.gz',
      checked_at: 1700000000,
      current_version: 'v0.7.0',
    };
    const {fetchImpl, calls} = makeMockFetch([{ok: true, body: payload}]);
    const t = makeHTTPTransport('http://localhost', {fetchImpl, wsCtor: MockWS as any});
    const got = await t.invoke<typeof payload>('CheckForUpdate');
    expect(got).toEqual(payload);
    expect(calls[0].url).toBe('http://localhost/api/updater/check');
    expect(calls[0].init.method).toBe('POST');
  });

  test('SetUpdaterConfig PUTs JSON body and unwraps OK', async () => {
    const {fetchImpl, calls} = makeMockFetch([{ok: true, body: {ok: true}}]);
    const t = makeHTTPTransport('http://localhost', {fetchImpl, wsCtor: MockWS as any});
    const cfg = {enabled: false, channel: 'beta', last_checked_at: 0, last_seen_version: ''};
    const result = await t.invoke<void>('SetUpdaterConfig', cfg);
    expect(result).toBeUndefined();
    expect(calls[0].url).toBe('http://localhost/api/settings/updater');
    expect(calls[0].init.method).toBe('PUT');
    expect(JSON.parse(calls[0].init.body as string)).toEqual(cfg);
  });

  test('InstallUpdate POSTs /api/updater/install', async () => {
    const {fetchImpl, calls} = makeMockFetch([{ok: true, body: {ok: true}}]);
    const t = makeHTTPTransport('http://localhost', {fetchImpl, wsCtor: MockWS as any});
    await t.invoke<void>('InstallUpdate');
    expect(calls[0].url).toBe('http://localhost/api/updater/install');
    expect(calls[0].init.method).toBe('POST');
  });

  test('error response surfaces server error message', async () => {
    const {fetchImpl} = makeMockFetch([{ok: false, status: 400, body: {error: 'bad request'}}]);
    const t = makeHTTPTransport('http://localhost', {fetchImpl, wsCtor: MockWS as any});
    await expect(t.invoke('AddMagnet', 'x', '/t')).rejects.toThrow('bad request');
  });

  test('unknown method rejects', async () => {
    const t = makeHTTPTransport('http://localhost', {fetchImpl: makeMockFetch([]).fetchImpl, wsCtor: MockWS as any});
    await expect(t.invoke('Bogus' as any)).rejects.toThrow(/no route/i);
  });
});

describe('http_transport.on (WebSocket)', () => {
  beforeEach(() => { MockWS.instances = []; });

  test('opens a single shared WS and demuxes by event type', () => {
    const {fetchImpl} = makeMockFetch([]);
    const t = makeHTTPTransport('http://localhost', {fetchImpl, wsCtor: MockWS as any});

    const torrents: any[] = [];
    const stats: any[] = [];
    const offT = t.on('torrents:tick', (p) => torrents.push(p));
    const offS = t.on('stats:tick', (p) => stats.push(p));

    expect(MockWS.instances).toHaveLength(1); // single shared connection
    const ws = MockWS.instances[0];
    expect(ws.url).toBe('ws://localhost/api/ws');

    ws.push({type: 'torrents:tick', payload: [{id: 'a'}]});
    ws.push({type: 'stats:tick', payload: {total_torrents: 3}});
    ws.push({type: 'unrelated', payload: 'x'});

    expect(torrents).toEqual([[{id: 'a'}]]);
    expect(stats).toEqual([{total_torrents: 3}]);

    offT();
    offS();
  });

  test('https origin upgrades to wss', () => {
    const t = makeHTTPTransport('https://example.com', {
      fetchImpl: makeMockFetch([]).fetchImpl,
      wsCtor: MockWS as any,
    });
    t.on('torrents:tick', () => {});
    expect(MockWS.instances[0].url).toBe('wss://example.com/api/ws');
  });

  test('unsubscribing the last handler closes the socket', () => {
    const t = makeHTTPTransport('http://localhost', {
      fetchImpl: makeMockFetch([]).fetchImpl,
      wsCtor: MockWS as any,
    });
    const off = t.on('stats:tick', () => {});
    expect(MockWS.instances[0].closed).toBe(false);
    off();
    expect(MockWS.instances[0].closed).toBe(true);
  });

  test('a delayed close from the previous subscription cannot replace the live socket', () => {
    vi.useFakeTimers();
    try {
      const t = makeHTTPTransport('http://localhost', {wsCtor: MockWS as any});
      const off = t.on('stats:tick', () => {});
      const old = MockWS.instances[0];
      off();
      const received: any[] = [];
      const offNew = t.on('stats:tick', (p) => received.push(p));
      old.onclose?.({} as CloseEvent);
      old.push({type: 'stats:tick', payload: 'stale'});
      vi.advanceTimersByTime(30_000);
      expect(MockWS.instances).toHaveLength(2);
      MockWS.instances[1].push({type: 'stats:tick', payload: 'current'});
      expect(received).toEqual(['current']);
      offNew();
      expect(MockWS.instances[1].closed).toBe(true);
    } finally {
      vi.useRealTimers();
    }
  });

  test('a failed connection reconnects with backoff and cancels on unsubscribe', () => {
    vi.useFakeTimers();
    try {
      const t = makeHTTPTransport('http://localhost', {wsCtor: MockWS as any});
      const off = t.on('stats:tick', () => {});
      MockWS.instances[0].close();
      vi.advanceTimersByTime(999);
      expect(MockWS.instances).toHaveLength(1);
      vi.advanceTimersByTime(1);
      expect(MockWS.instances).toHaveLength(2);
      MockWS.instances[1].close();
      vi.advanceTimersByTime(1999);
      expect(MockWS.instances).toHaveLength(2);
      off();
      vi.advanceTimersByTime(30_000);
      expect(MockWS.instances).toHaveLength(2);
    } finally {
      vi.useRealTimers();
    }
  });
});

test('peer block methods use the shared web endpoint', async () => {
  const {fetchImpl, calls} = makeMockFetch([{body: ['2001:db8::1']}, {body: {ok: true}}]);
  const t = makeHTTPTransport('http://localhost', {fetchImpl, wsCtor: MockWS as any});
  expect(await t.invoke('ListBlockedPeers')).toEqual(['2001:db8::1']);
  await t.invoke('SetPeerBlocked', '2001:db8::1', false);
  expect(calls.map((c) => [c.init.method, c.url])).toEqual([
    ['GET', 'http://localhost/api/peers/blocked'], ['PUT', 'http://localhost/api/peers/blocked'],
  ]);
  expect(JSON.parse(calls[1].init.body as string)).toEqual({ip: '2001:db8::1', blocked: false});
});

test('preparation and storage APIs use matching web routes and preserve options', async () => {
  const {fetchImpl, calls} = makeMockFetch([{body: {id: 'prepared'}}, {body: {id: 'prepared', metadata_ready: true}}, {body: {free_bytes: 123, preallocate: false}}, {body: {ok: true}}, {body: {ok: true}}]);
  const t = makeHTTPTransport('http://localhost', {fetchImpl, wsCtor: MockWS as any});
  const request = {magnet: '', torrent_file: btoa('torrent'), save_path: '/downloads'};
  expect(await t.invoke('PrepareTorrent', request)).toBe('prepared');
  await t.invoke('GetTorrentDetail', 'prepared');await t.invoke('GetStorage', 'prepared');
  await t.invoke('RelocateTorrent', 'prepared', '/new', false);await t.invoke('SetPreallocate', true);
  expect(calls.map((c) => [c.init.method, c.url])).toEqual([
    ['POST', 'http://localhost/api/torrents/prepare'], ['GET', 'http://localhost/api/torrents/prepared/detail'], ['GET', 'http://localhost/api/torrents/prepared/storage'], ['POST', 'http://localhost/api/torrents/prepared/relocate'], ['PUT', 'http://localhost/api/storage/preallocate'],
  ]);
  expect(JSON.parse(calls[0].init.body as string)).toEqual(request);
  expect(JSON.parse(calls[3].init.body as string)).toEqual({path: '/new', move: false});
  expect(JSON.parse(calls[4].init.body as string)).toEqual({enabled: true});
});


test('connection state tracks recovery, last data, silent sockets, and timer cleanup', () => {
 vi.useFakeTimers();MockWS.instances=[];
 try {
  const t=makeHTTPTransport('http://localhost',{wsCtor:MockWS as any});
  const states:any[]=[];const off=t.on('connection:state',v=>states.push(v));
  expect(states.at(-1).status).toBe('connecting');
  const first=MockWS.instances[0];first.onopen?.({} as Event);
  expect(states.at(-1)).toMatchObject({status:'connected',epoch:1,lastUpdateAt:0});
  vi.advanceTimersByTime(10_000);first.push({type:'torrents:tick',payload:[]});
  const last=states.at(-1).lastUpdateAt;expect(last).toBe(Date.now());
  vi.advanceTimersByTime(10_000);expect(first.closed).toBe(false);
  vi.advanceTimersByTime(5_000);expect(first.closed).toBe(true);
  expect(states.at(-1)).toMatchObject({status:'reconnecting',lastUpdateAt:last});
  vi.advanceTimersByTime(1000);const second=MockWS.instances[1];second.onopen?.({} as Event);
  expect(states.at(-1)).toMatchObject({status:'connected',epoch:2});
  first.push({type:'torrents:tick',payload:[]});expect(states.at(-1).lastUpdateAt).toBe(last);
  off();expect(vi.getTimerCount()).toBe(0);expect(second.closed).toBe(true);
 } finally {vi.useRealTimers();}
});
