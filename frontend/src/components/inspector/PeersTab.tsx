import {createEffect, createMemo, createSignal, Index, Show} from 'solid-js';
import {ChevronDown, Copy, ShieldBan} from 'lucide-solid';
import {toast} from 'solid-sonner';
import {api, type DetailDTO, type PeerDTO} from '../../lib/bindings';
import {fmtBytes, fmtPercent, fmtRate} from '../../lib/format';
import {userErr} from '../../lib/errors';

type Props = {detail: DetailDTO | null; canBlock: boolean};

export function PeersTab(props: Props) {
  let root!: HTMLDivElement;
  const peerKey = (peer: PeerDTO) => `${props.detail?.id}:${peer.ip}:${peer.port}`;
  const [query, setQuery] = createSignal('');
  const [sort, setSort] = createSignal('download_rate');
  const [expanded, setExpanded] = createSignal('');
  const [blocked, setBlocked] = createSignal<string[]>([]);
  const [busy, setBusy] = createSignal(false);
  const [loaded, setLoaded] = createSignal(false);
  createEffect(() => {
    if (!props.canBlock) return;
    api.listBlockedPeers().then((ips) => { setBlocked(ips); setLoaded(true); })
      .catch((err) => toast.error(`Couldn't load blocked peers — ${userErr(err)}`));
  });
  const peers = createMemo(() => {
    const search = query().trim().toLowerCase();
    return (props.detail?.peers_list ?? [])
      .filter((p) => !blocked().includes(p.ip) && `${p.ip} ${p.client}`.toLowerCase().includes(search))
      .sort((a, b) => {
        const key = sort() as 'download_rate' | 'upload_rate' | 'uploaded' | 'downloaded';
        const pinned = Number(expanded() === peerKey(b)) - Number(expanded() === peerKey(a));
        return pinned || (b[key] ?? 0) - (a[key] ?? 0) || a.ip.localeCompare(b.ip) || a.port - b.port;
      });
  });

  async function setBlock(ip: string, value: boolean) {
    if (busy()) return;
    setBusy(true);
    try {
      await api.setPeerBlocked(ip, value);
      setBlocked((ips) => value ? [...ips, ip] : ips.filter((value) => value !== ip));
      toast.success(value ? `${ip} blocked across all torrents` : `${ip} unblocked`);
    } catch (err) {
      toast.error(`Couldn't ${value ? 'block' : 'unblock'} peer — ${userErr(err)}`);
    } finally { setBusy(false); }
  }

  return (
    <div ref={root} class="min-w-0 text-xs">
      <div class="sticky top-0 z-10 space-y-2 border-b border-white/[.04] bg-zinc-950/95 px-4 py-3 backdrop-blur-md">
        <div class="flex items-center justify-between gap-2">
          <span class="text-zinc-400">{peers().length}{query().trim() ? ` of ${props.detail?.peers_list?.length ?? 0}` : ''} connected peers</span>
          <span class="text-[10px] text-zinc-500">Select a peer for details</span>
        </div>
        <div class="flex gap-2">
          <input type="search" aria-label="Search peers" placeholder="Search IP or client…" value={query()}
            onInput={(e) => setQuery(e.currentTarget.value)}
            class="min-w-0 flex-1 rounded border border-white/[.08] bg-white/[.04] px-2 py-1.5 text-zinc-200 outline-none focus:border-accent-500" />
          <select aria-label="Sort peers" value={sort()} onChange={(e) => setSort(e.currentTarget.value)}
            class="min-w-0 max-w-[46%] rounded border border-white/[.08] bg-zinc-900 px-1 text-zinc-300">
            <option value="download_rate">Download speed</option>
            <option value="upload_rate">Upload speed</option>
            <option value="downloaded">Downloaded</option>
            <option value="uploaded">Uploaded</option>
          </select>
        </div>
      </div>
      <Show when={peers().length} fallback={<p class="p-4 text-zinc-500">{query() ? 'No matching peers.' : 'No connected peers.'}</p>}>
        <Index each={peers()}>{(p) => {
          const key = () => peerKey(p());
          return (
            <div class="border-b border-white/[.04]">
              <button type="button" aria-expanded={expanded() === key()}
                onClick={() => {
                  const next = expanded() === key() ? '' : key();
                  setExpanded(next);
                  if (next) {
                    root.scrollIntoView({block: 'start'});
                    root.querySelector<HTMLButtonElement>('button[aria-expanded="true"]')?.focus({preventScroll: true});
                  }
                }}
                class="flex w-full min-w-0 items-center gap-2 px-4 py-3 text-left hover:bg-white/[.03] focus-visible:outline-accent-500">
                <ChevronDown size={12} class="shrink-0 text-zinc-500 transition-transform" classList={{'-rotate-90': expanded() !== key()}} />
                <span class="min-w-0 flex-1">
                  <span class="block truncate font-mono text-zinc-200" title={p().ip}>{p().ip}</span>
                  <span class="mt-1 block truncate text-[10px] text-zinc-500" title={p().client}>{p().client || 'Unknown client'} · {fmtPercent(p().progress)}</span>
                </span>
                <span class="shrink-0 text-right font-mono text-[10px] tabular-nums">
                  <span class="block text-accent-300">↓ {fmtRate(p().download_rate)}</span>
                  <span class="mt-1 block text-seed">↑ {fmtRate(p().upload_rate)}</span>
                </span>
              </button>
              <Show when={expanded() === key()}>
                <div class="space-y-3 bg-white/[.02] px-4 pb-3 pt-1">
                  <div class="grid grid-cols-2 gap-3 rounded-md border border-white/[.05] p-3">
                    <div><div class="text-[10px] text-zinc-500">Downloaded from peer</div><div class="mt-1 font-mono text-zinc-200">{fmtBytes(p().downloaded ?? 0)}</div></div>
                    <div><div class="text-[10px] text-zinc-500">Uploaded to peer</div><div class="mt-1 font-mono text-zinc-200">{fmtBytes(p().uploaded ?? 0)}</div></div>
                    <p class="col-span-2 text-[10px] text-zinc-500">Payload totals for this connection; reset on reconnect.</p>
                  </div>
                  <p class="break-all text-zinc-400">{p().client || 'Unknown client'} · Port {p().port}<Show when={p().flags}> · Flags {p().flags}</Show></p>
                  <div class="flex flex-wrap items-center gap-3">
                    <button type="button" class="inline-flex items-center gap-1 text-zinc-400 hover:text-zinc-100" onClick={async () => {
                      try { await navigator.clipboard.writeText(p().ip); toast.success('Peer IP copied'); }
                      catch (err) { toast.error(`Couldn't copy IP — ${userErr(err)}`); }
                    }}><Copy size={12} /> Copy IP</button>
                    <Show when={props.canBlock}>
                      <button type="button" disabled={busy() || !loaded()} title="Disconnect and block this IP across all torrents, including after restart. Unblock below."
                        class="inline-flex items-center gap-1 text-rose-300 hover:text-rose-200 disabled:opacity-40" onClick={() => void setBlock(p().ip, true)}>
                        <ShieldBan size={12} /> Block IP
                      </button>
                    </Show>
                  </div>
                  <Show when={props.canBlock}><p class="text-[10px] text-zinc-500">Blocking disconnects this IP across all torrents and persists after restart. Low sharing alone does not indicate abuse.</p></Show>
                </div>
              </Show>
            </div>
          );
        }}</Index>
      </Show>
      <Show when={props.canBlock && blocked().length}>
        <details class="border-t border-white/[.04] px-4 py-3">
          <summary class="cursor-pointer text-zinc-400">Blocked IPs · {blocked().length}</summary>
          <p class="my-2 text-[10px] text-zinc-500">Applies to all torrents. Unblock to allow connections again.</p>
          <Index each={blocked()}>{(ip) => <div class="flex min-w-0 items-center justify-between gap-2 py-1.5">
            <span class="min-w-0 break-all font-mono text-zinc-300">{ip()}</span>
            <button type="button" disabled={busy()} class="shrink-0 text-accent-300 hover:underline disabled:opacity-40" onClick={() => void setBlock(ip(), false)}>Unblock</button>
          </div>}</Index>
        </details>
      </Show>
    </div>
  );
}
