import {ArrowDown, ArrowUp, Globe, Wifi, ChartNoAxesCombined} from 'lucide-solid';
import {Show, createSignal, createEffect} from 'solid-js';
import type {GlobalStatsT, WebConfigDTO} from '../../lib/bindings';
import {fmtRate} from '../../lib/format';
import {emaStep} from '../../lib/smoothing';

type Props = {
  stats: GlobalStatsT;
  stale?: boolean;
  onOpenStatistics?: () => void;
  queuedCount: number;
  webConfig: WebConfigDTO;
  // Configuration is distinct from the observed state in Connection diagnostics.
  dhtEnabled: boolean;
  onOpenConnection?:()=>void;
  onClickWeb: () => void;
};

export function StatusBar(props: Props) {
  const s = () => props.stats;
  // The engine reports raw ~1 Hz rates that flicker frame-to-frame. Show an
  // EMA-smoothed value instead so the readout settles; the first sample
  // seeds the average so it doesn't visibly ramp up from zero on mount.
  const [downRate, setDownRate] = createSignal(props.stats.total_download_rate);
  const [upRate, setUpRate] = createSignal(props.stats.total_upload_rate);
  let seeded = false;
  createEffect(() => {
    const d = props.stats.total_download_rate;
    const u = props.stats.total_upload_rate;
    if (!seeded) {
      seeded = true;
      setDownRate(d);
      setUpRate(u);
      return;
    }
    setDownRate((p) => d === 0 ? 0 : emaStep(p, d));
    setUpRate((p) => u === 0 ? 0 : emaStep(p, u));
  });
  return (
    <footer class="flex h-7 shrink-0 items-center gap-4 border-t border-white/[.04] bg-zinc-950/60 px-3 text-[11px] text-zinc-400">
      {/* Symmetric arrow treatment: each arrow colors only when its rate is
          live (> 0). Previously ↓ was always accent-colored while ↑ was
          always muted, making upload look perpetually secondary even when
          actively seeding. */}
      <span class="inline-flex items-center gap-1.5">
        <ArrowDown class={`h-3 w-3 ${downRate() > 0 ? 'text-down' : 'text-zinc-500'}`} />
        <span class="font-mono tabular-nums">{props.stale ? '—' : fmtRate(downRate())}</span>
      </span>
      <span class="inline-flex items-center gap-1.5">
        <ArrowUp class={`h-3 w-3 ${upRate() > 0 ? 'text-seed' : 'text-zinc-500'}`} />
        <span class="font-mono tabular-nums">{props.stale ? '—' : fmtRate(upRate())}</span>
      </span>

      <span class="h-3 w-px bg-white/[.06]" />

      <span class="font-mono tabular-nums">{s().total_torrents} torrents</span>
      <span class="font-mono tabular-nums">{s().active_torrents} active</span>
      <span class="font-mono tabular-nums">{props.queuedCount} queued</span>
      <span class="font-mono tabular-nums">{s().seeding_torrents} seeding</span>
      <span class="font-mono tabular-nums">{s().total_peers} peers</span>

      <div class="ml-auto flex items-center gap-3">
        <Show when={props.onOpenStatistics}><button type="button" onClick={props.onOpenStatistics} aria-label="Transfer statistics" title="Transfer statistics" class="rounded p-1 text-zinc-400 hover:bg-white/[.05] hover:text-zinc-200"><ChartNoAxesCombined class="h-3.5 w-3.5" /></button></Show>
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
        <Show when={props.dhtEnabled}>
          <button type="button" onClick={props.onOpenConnection} title="DHT configured · open live connection diagnostics" class="inline-flex items-center gap-1.5 rounded p-1 text-zinc-500 hover:text-zinc-300">
            <Wifi class="h-3 w-3" /><span>DHT configured</span>
          </button>
        </Show>
      </div>
    </footer>
  );
}
