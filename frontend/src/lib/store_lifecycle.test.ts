import {describe, expect, test, vi} from 'vitest';
import {createTorrentsStore} from './store';
import type {DetailDTO} from './bindings';

const {listeners} = vi.hoisted(() => ({listeners: new Map<string, (payload: any) => void>()}));

vi.mock('./transport', () => ({
  transport: {
    invoke: async (method: string) => method === 'Bootstrap' ? {flavor: 'daemon'} : [],
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
