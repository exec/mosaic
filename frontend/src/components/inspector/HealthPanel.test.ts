import {expect, test} from 'vitest';
import {downloadHealth} from './HealthPanel';
import type {DetailDTO} from '../../lib/bindings';
const base = {metadata_ready: true, total_bytes: 10, peers: 2} as DetailDTO;
test('health prioritizes actionable failures above pause, queue, and peer guesses', () => {
  expect(downloadHealth({...base, files_missing: true, paused: true}, 0).title).toBe('Files are missing');
  expect(downloadHealth({...base, error_active: true, last_error: 'disk full', queued: true}, 0).reason).toBe('disk full');
  expect(downloadHealth({...base, verifying: true, paused: true}, 0).title).toBe('Checking existing files');
  expect(downloadHealth({...base, paused: true, queued: true}, 0).title).toBe('Paused');
  expect(downloadHealth({...base, queued: true, peers: 0}, 0).title).toBe('Waiting for an active slot');
});
test('health gives honest waiting and active states without treating metadata as tracker success', () => {
  expect(downloadHealth({...base, metadata_ready: false, total_bytes: 0}, 0).title).toBe('Finding torrent metadata');
  expect(downloadHealth({...base, peers: 0}, 0).action).toBe('trackers');
  expect(downloadHealth(base, 0).title).toBe('Connected, waiting for data');
  expect(downloadHealth(base, 123).title).toBe('Downloading');
  expect(downloadHealth({...base, completed: true, peers: 0}, 0).title).toBe('Ready to seed');
});
