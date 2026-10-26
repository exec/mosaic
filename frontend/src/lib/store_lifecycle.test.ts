import {describe, expect, test, vi} from 'vitest';
import {createTorrentsStore} from './store';
import torrentFixture from '../../../backend/api/testdata/dto_snapshots/TorrentDTO_null_category.json';
import type {DetailDTO, Torrent} from './bindings';

const {listeners, invoke} = vi.hoisted(() => ({listeners: new Map<string, (payload: any) => void>(), invoke: vi.fn(async (method: string) => method === 'Bootstrap' ? {flavor: 'daemon'} : [])}));

vi.mock('./transport', () => ({
  transport: {
    invoke,
    on: (event: string, handler: (payload: any) => void) => {
      listeners.set(event, handler);
      return () => listeners.delete(event);
    },
  },
}));

describe('inspector ticks', () => {
  test('ignores queued frames for the previous torrent and for a closed inspector', async () => {
    const store = createTorrentsStore();
    try {
      await store.openInspector('current');
      const tick = listeners.get('inspector:tick')!;
      const stale = {id: 'previous'} as DetailDTO;
      const current = {id: 'current'} as DetailDTO;
      tick(stale);
      expect(store.state.inspectorDetail).toBeNull();
      expect(store.state.bandwidthTick).toBe(0);
      tick(current);
      expect(store.state.inspectorDetail?.id).toBe('current');
      expect(store.state.bandwidthTick).toBe(1);
      await store.closeInspector();
      tick(current);
      expect(store.state.inspectorDetail).toBeNull();
      expect(store.state.bandwidthTick).toBe(1);
    } finally {
      store.dispose();
    }
  });
});

test('inspector history records the selected torrent, including pause and selection changes', async () => {
  const store = createTorrentsStore();
  try {
    await Promise.resolve();
    const rows = [
      {...torrentFixture, id: 'a', download_rate: 100, upload_rate: 20},
      {...torrentFixture, id: 'b', download_rate: 900, upload_rate: 80},
    ] as Torrent[];
    listeners.get('torrents:tick')!(rows);
    listeners.get('stats:tick')!({total_download_rate: 1000, total_upload_rate: 100});
    await store.openInspector('a', 'speed');
    listeners.get('inspector:tick')!({id: 'a'});
    expect(store.bandwidthRing.newest()).toMatchObject({down: 100, up: 20});
    listeners.get('torrents:tick')!([{...rows[0], paused: true}, rows[1]]);
    listeners.get('inspector:tick')!({id: 'a'});
    expect(store.bandwidthRing.newest()).toMatchObject({down: 0, up: 0});
    await store.openInspector('b', 'speed');
    expect(store.bandwidthRing.count).toBe(0);
    listeners.get('inspector:tick')!({id: 'a'});
    expect(store.bandwidthRing.count).toBe(0);
    listeners.get('inspector:tick')!({id: 'b'});
    expect(store.bandwidthRing.newest()).toMatchObject({down: 900, up: 80});
  } finally { store.dispose(); }
});

test('select all honors search, status, category, and tag filters', async () => {
  const store = createTorrentsStore();
  try {
    await Promise.resolve();
    listeners.get('torrents:tick')!([
      {...torrentFixture, id: 'wanted', name: 'Linux A', paused: false, category_id: 1, tags: [{id: 5}]},
      {...torrentFixture, id: 'name', name: 'Other', paused: false, category_id: 1, tags: [{id: 5}]},
      {...torrentFixture, id: 'status', name: 'Linux B', paused: true, category_id: 1, tags: [{id: 5}]},
      {...torrentFixture, id: 'category', name: 'Linux C', paused: false, category_id: 2, tags: [{id: 5}]},
      {...torrentFixture, id: 'tag', name: 'Linux D', paused: false, category_id: 1, tags: []},
    ]);
    store.setSearchQuery('linux');
    store.setStatusFilter('downloading');
    store.setSelectedCategory(1);
    store.setSelectedTag(5);
    store.selectAll();
    expect([...store.state.selection]).toEqual(['wanted']);
    store.setSearchQuery('absent');
    store.selectAll();
    expect(store.state.selection.size).toBe(0);
  } finally { store.dispose(); }
});


test('reconnect refreshes data while guarding newer ticks and disconnected responses',async()=>{
 const store=createTorrentsStore();
 try {
  await Promise.resolve();await Promise.resolve();
  const connect=listeners.get('connection:state')!;
  let finish!:(v:any)=>void;
  invoke.mockImplementation(async(method:string)=>method==='ListTorrents'?new Promise(r=>finish=r):method==='Bootstrap'?{flavor:'daemon'}:[]);
  connect({status:'connected',epoch:1,lastUpdateAt:0});
  expect(store.state.connection).toMatchObject({stale:true,syncing:true});
  listeners.get('torrents:tick')!([{...torrentFixture,id:'newest'}]);
  finish([{...torrentFixture,id:'older'}]);
  await vi.waitFor(()=>expect(store.state.connection.stale).toBe(false));
  expect(store.state.torrents.map(t=>t.id)).toEqual(['newest']);
  connect({status:'reconnecting',epoch:1,lastUpdateAt:123});
  expect(store.state.connection.stale).toBe(true);
  connect({status:'connected',epoch:2,lastUpdateAt:123});
  connect({status:'reconnecting',epoch:2,lastUpdateAt:123});
  finish([{...torrentFixture,id:'disconnected'}]);
  await Promise.resolve();await Promise.resolve();
  expect(store.state.connection).toMatchObject({stale:true,syncing:false});
  expect(store.state.torrents.map(t=>t.id)).toEqual(['newest']);
 } finally {store.dispose();invoke.mockImplementation(async(method:string)=>method==='Bootstrap'?{flavor:'daemon'}:[]);}
});


test('daemon boot skips updater configuration requests', async () => {
 invoke.mockClear();
 const store = createTorrentsStore();
 try {
  await vi.waitFor(() => expect(store.state.serverFlavor).toBe('daemon'));
  expect(invoke.mock.calls.some(([method]) => method === 'GetUpdaterConfig')).toBe(false);
 } finally {store.dispose();}
});
