import {createEffect, createSignal, Show} from 'solid-js';
import {RefreshCw} from 'lucide-solid';
import {toast} from 'solid-sonner';
import type {BlocklistDTO} from '../../lib/bindings';
import {fmtTimestamp} from '../../lib/format';
import {Button} from '../ui/Button';

type Props = {
  blocklist: BlocklistDTO;
  onSetBlocklistURL: (url: string, enabled: boolean) => Promise<void>;
  onRefreshBlocklist: () => Promise<void>;
};

export function BlocklistPane(props: Props) {
  const [url, setUrl] = createSignal(props.blocklist.url);
  const [enabled, setEnabled] = createSignal(props.blocklist.enabled);
  const [refreshing, setRefreshing] = createSignal(false);

  createEffect(() => {
    setUrl(props.blocklist.url);
    setEnabled(props.blocklist.enabled);
  });

  return (
    <div class="mx-auto max-w-2xl px-6 py-6">
      <div class="mb-4 border-b border-white/[.04] pb-3">
        <h2 class="text-lg font-semibold text-zinc-100">IP Blocklist</h2>
        <p class="mt-0.5 text-sm text-zinc-500">Block peers in PeerGuardian-format ranges.</p>
      </div>

      <div class="space-y-3">
        <div>
          <label class="text-xs text-zinc-500 mb-1 block">Blocklist URL</label>
          <input
            type="text"
            class="w-full rounded border border-white/[.06] bg-black/30 px-2 py-1.5 font-mono text-xs text-zinc-100 focus:border-accent-500/50 focus:outline-none"
            value={url()}
            onInput={(e) => setUrl(e.currentTarget.value)}
            placeholder="https://example.com/blocklist.p2p"
          />
        </div>
        <label class="inline-flex items-center gap-2 text-sm text-zinc-200">
          <input type="checkbox" checked={enabled()} onChange={(e) => setEnabled(e.currentTarget.checked)} />
          Enable blocklist
        </label>

        <div class="flex justify-between items-center pt-3 border-t border-white/[.04]">
          <div class="text-xs text-zinc-500">
            <Show when={props.blocklist.last_loaded_at > 0} fallback={<span>Never loaded</span>}>
              <span>Last loaded {fmtTimestamp(props.blocklist.last_loaded_at)} · {props.blocklist.entries} entries</span>
            </Show>
            <Show when={props.blocklist.error}>
              <div class="text-rose-400 mt-1">Error: {props.blocklist.error}</div>
            </Show>
          </div>
          <div class="flex gap-2">
            <Button
              type="button"
              variant="ghost"
              disabled={!enabled() || !url() || refreshing()}
              onClick={async () => {
                setRefreshing(true);
                try {
                  await props.onRefreshBlocklist();
                  toast.success('Blocklist refreshed');
                } catch (e) {
                  toast.error(String(e));
                } finally {
                  setRefreshing(false);
                }
              }}
            >
              <RefreshCw class={`h-3.5 w-3.5 ${refreshing() ? 'animate-spin' : ''}`} />
              Refresh
            </Button>
            <Button
              type="button"
              variant="primary"
              onClick={async () => {
                try {
                  await props.onSetBlocklistURL(url(), enabled());
                  toast.success('Blocklist saved');
                } catch (e) {
                  toast.error(String(e));
                }
              }}
            >
              Save
            </Button>
          </div>
        </div>
      </div>
    </div>
  );
}
