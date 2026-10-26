import {createMemo, createSignal, For, onMount, Show} from 'solid-js';
import {api} from '../../lib/bindings';
import {userErr} from '../../lib/errors';
import {Button} from '../ui/Button';
import {toast} from 'solid-sonner';

export function ManualPeerBlocks() {
  const [ips, setIPs] = createSignal<string[]>([]);
  const [query, setQuery] = createSignal(''), [address, setAddress] = createSignal('');
  const [busy, setBusy] = createSignal(false), [loading, setLoading] = createSignal(true), [error, setError] = createSignal('');
  const visible = createMemo(() => ips().filter((ip) => ip.toLowerCase().includes(query().trim().toLowerCase())));
  const refresh = async () => {
    setLoading(true);setError('');
    try {setIPs(await api.listBlockedPeers());}catch(e){setError(userErr(e));}finally{setLoading(false);}
  };
  onMount(refresh);
  const change = async (ip: string, blocked: boolean) => {
    if (busy() || loading()) return;setBusy(true);setError('');
    try {await api.setPeerBlocked(ip, blocked);if (blocked) setAddress('');await refresh();toast.success(blocked ? 'Peer blocked on all torrents' : 'Manual peer block removed');}
    catch(e){setError(userErr(e));}finally{setBusy(false);}
  };
  const input = 'min-w-0 rounded border border-white/[.06] bg-black/30 px-2 py-1.5 text-xs text-zinc-200 focus:outline-none focus:ring-1 focus:ring-accent-500';
  return <section class="mt-7 border-t border-white/[.05] pt-5" aria-label="Manual peer blocks">
    <div class="flex items-center justify-between gap-2"><h3 class="text-sm font-medium text-zinc-200">Manually blocked peers <span class="text-zinc-500">({ips().length})</span></h3><Button class="text-xs" variant="ghost" disabled={busy() || loading()} onClick={refresh}>Refresh</Button></div>
    <p class="mt-1 text-xs leading-relaxed text-zinc-500">These persistent IP blocks apply to every torrent. Removing a manual block does not override the downloaded range list.</p>
    <form class="mt-3 flex gap-2" onSubmit={(e) => {e.preventDefault();void change(address().trim(), true);}}>
      <input class={`${input} flex-1 font-mono`} aria-label="Peer IP to block" placeholder="IPv4 or IPv6 address" value={address()} disabled={busy() || loading()} onInput={(e) => setAddress(e.currentTarget.value)} />
      <Button type="submit" class="text-xs" disabled={busy() || loading() || !address().trim()}>Block IP</Button>
    </form>
    <input type="search" class={`${input} mt-3 w-full`} aria-label="Search blocked peers" placeholder="Search blocked IPs…" value={query()} onInput={(e) => setQuery(e.currentTarget.value)} />
    <Show when={error()}><p role="alert" class="mt-2 text-xs text-rose-300">{error()}</p></Show>
    <div class="mt-2 max-h-72 overflow-auto rounded border border-white/[.04]">
      <Show when={!loading()} fallback={<p class="p-3 text-xs text-zinc-500">Loading blocks…</p>}>
        <For each={visible()} fallback={<p class="p-3 text-xs text-zinc-500">{query() ? 'No matching blocked peers.' : 'No manually blocked peers.'}</p>}>
          {(ip) => <div class="flex items-center gap-3 border-b border-white/[.04] px-3 py-2 text-xs"><span class="min-w-0 flex-1 break-all font-mono text-zinc-300">{ip}</span><Button class="shrink-0 text-xs" variant="ghost" disabled={busy()} onClick={() => change(ip, false)}>Unblock</Button></div>}
        </For>
      </Show>
    </div>
  </section>;
}
