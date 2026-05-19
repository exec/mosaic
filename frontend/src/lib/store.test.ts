import {describe, expect, it} from 'vitest';
import {filterTorrents} from './store';
import type {Torrent} from './bindings';

function mk(over: Partial<Torrent>): Torrent {
  return {
    id: 'id', name: 'name', magnet: '', save_path: '',
    total_bytes: 0, bytes_done: 0, progress: 0,
    download_rate: 0, upload_rate: 0, peers: 0, seeds: 0,
    paused: false, completed: false, added_at: 0,
    category_id: null, tags: [], queue_position: 0,
    force_start: false, queued: false, verifying: false,
    files_missing: false, access: 'owner',
    ...over,
  };
}

describe('filterTorrents', () => {
  const downloading = mk({id: 'd', name: 'Ubuntu ISO', paused: false, completed: false});
  const seeding = mk({id: 's', name: 'Debian', paused: false, completed: true});
  const pausedDone = mk({id: 'p', name: 'Arch', paused: true, completed: true});
  const pausedIncomplete = mk({id: 'pi', name: 'Fedora', paused: true, completed: false});
  const all = [downloading, seeding, pausedDone, pausedIncomplete];

  it('returns everything for status "all" and empty query', () => {
    expect(filterTorrents(all, 'all', '')).toEqual(all);
  });

  it('filters downloading: not paused, not completed', () => {
    expect(filterTorrents(all, 'downloading', '')).toEqual([downloading]);
  });

  it('filters seeding: completed and not paused', () => {
    expect(filterTorrents(all, 'seeding', '')).toEqual([seeding]);
  });

  it('filters completed regardless of paused', () => {
    expect(filterTorrents(all, 'completed', '')).toEqual([seeding, pausedDone]);
  });

  it('filters paused regardless of completed', () => {
    expect(filterTorrents(all, 'paused', '')).toEqual([pausedDone, pausedIncomplete]);
  });

  it('matches the search query case-insensitively', () => {
    expect(filterTorrents(all, 'all', 'ubuntu')).toEqual([downloading]);
    expect(filterTorrents(all, 'all', 'DEB')).toEqual([seeding]);
  });

  it('ignores whitespace-only queries', () => {
    expect(filterTorrents(all, 'all', '   ')).toEqual(all);
  });

  it('filters by category id', () => {
    const a = mk({id: 'a', category_id: 1});
    const b = mk({id: 'b', category_id: 2});
    expect(filterTorrents([a, b], 'all', '', 1)).toEqual([a]);
  });

  it('filters by tag id', () => {
    const tagged = mk({id: 't', tags: [{id: 5, name: 'fav', color: '#fff'}]});
    const untagged = mk({id: 'u', tags: []});
    expect(filterTorrents([tagged, untagged], 'all', '', null, 5)).toEqual([tagged]);
  });

  it('applies all criteria together in one pass', () => {
    const match = mk({id: 'm', name: 'Linux Mint', completed: true, paused: false, category_id: 3, tags: [{id: 9, name: 'iso', color: '#000'}]});
    const wrongName = mk({id: 'wn', name: 'Windows', completed: true, paused: false, category_id: 3, tags: [{id: 9, name: 'iso', color: '#000'}]});
    const wrongCat = mk({id: 'wc', name: 'Linux Mint', completed: true, paused: false, category_id: 4, tags: [{id: 9, name: 'iso', color: '#000'}]});
    const result = filterTorrents([match, wrongName, wrongCat], 'seeding', 'linux', 3, 9);
    expect(result).toEqual([match]);
  });
});
