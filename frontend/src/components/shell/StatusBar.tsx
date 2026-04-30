import {ArrowDown, ArrowUp, Globe, Wifi} from 'lucide-solid';
import {Show} from 'solid-js';
import type {GlobalStatsT, WebConfigDTO} from '../../lib/bindings';
import {fmtRate} from '../../lib/format';

type Props = {
  stats: GlobalStatsT;
  queuedCount: number;
  webConfig: WebConfigDTO;
  onClickWeb: () => void;
};

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
      <span class="font-mono tabular-nums">{props.queuedCount} queued</span>
      <span class="font-mono tabular-nums">{s().seeding_torrents} seeding</span>
      <span class="font-mono tabular-nums">{s().total_peers} peers</span>

      <div class="ml-auto flex items-center gap-3">
        <Show when={props.webConfig.enabled}>
          <button
            type="button"
            onClick={props.onClickWeb}
            class="inline-flex items-center gap-1.5 rounded-full px-2 py-0.5 text-accent-300 transition-colors hover:bg-white/[.04]"
            data-testid="statusbar-web"
          >
            <Globe class="h-3 w-3" />
            <span class="font-mono tabular-nums">Web ON :{props.webConfig.port}</span>
          </button>
        </Show>
        <span class="inline-flex items-center gap-1.5">
          <Wifi class="h-3 w-3 text-seed" />
          <span class="text-zinc-500">DHT online</span>
        </span>
      </div>
    </footer>
  );
}
