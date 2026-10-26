import {expect, test, vi} from 'vitest';
import {createSignal} from 'solid-js';
import {render} from 'solid-js/web';
import {TorrentCard} from './TorrentCard';
import type {Torrent} from '../../lib/bindings';
import fixture from '../../../../backend/api/testdata/dto_snapshots/TorrentDTO_null_category.json';

test('viewer cards hide pause and resume controls while keeping personal removal', () => {
  const [torrent, setTorrent] = createSignal({...fixture, access: 'viewer'} as Torrent);
  const dispose = render(() => <TorrentCard torrent={torrent()} selected={false} onSelect={() => {}} onPause={vi.fn()} onResume={vi.fn()} onRemove={vi.fn()} />, document.body);
  try {
    expect(document.querySelector('[title="Pause"]')).toBeNull();
    expect(document.querySelector('[title="Resume"]')).toBeNull();
    expect(document.querySelector('[title="Remove"]')).toBeTruthy();
    setTorrent({...torrent(), access: 'editor'});
    expect(document.querySelector('[title="Pause"]')).toBeTruthy();
    setTorrent({...torrent(), paused: true});
    expect(document.querySelector('[title="Resume"]')).toBeTruthy();
  } finally { dispose(); document.body.innerHTML = ''; }
});
