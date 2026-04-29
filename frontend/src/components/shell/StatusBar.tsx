import {ArrowDown, ArrowUp, Wifi} from 'lucide-solid';
import type {GlobalStatsT} from '../../lib/bindings';
import {fmtRate} from '../../lib/format';

type Props = {stats: GlobalStatsT};

export function StatusBar(props: Props) {
  const s = () => props.stats;
  return (
    <footer class="flex h-7 shrink-0 items-center gap-4 border-t border-white/[.04] bg-zinc-950/60 px-3 text-[11px] text-zinc-400">
      <span class="inline-flex items-center gap-1.5">
        <ArrowDown class="h-3 w-3 text-down" />
        <span class="font-mono tabular-nums">{fmtRate(s().total_download_rate)}</span>
      </span>
      <span class="inline-flex items-center gap-1.5">
        <ArrowUp class="h-3 w-3 text-zinc-500" />
        <span class="font-mono tabular-nums">{fmtRate(s().total_upload_rate)}</span>
      </span>

      <span class="h-3 w-px bg-white/[.06]" />

      <span class="font-mono tabular-nums">{s().total_torrents} torrents</span>
      <span class="font-mono tabular-nums">{s().active_torrents} active</span>
      <span class="font-mono tabular-nums">{s().seeding_torrents} seeding</span>
      <span class="font-mono tabular-nums">{s().total_peers} peers</span>

      <span class="ml-auto inline-flex items-center gap-1.5">
        <Wifi class="h-3 w-3 text-seed" />
        <span class="text-zinc-500">DHT online</span>
      </span>
    </footer>
  );
}
