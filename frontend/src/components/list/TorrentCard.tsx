import {Show} from 'solid-js';
import {AlertTriangle, Loader2, Pause as PauseIcon, Play, Star, Trash2} from 'lucide-solid';
import type {Torrent} from '../../lib/bindings';
import {fmtBytes, fmtETA, fmtPercent, fmtRate} from '../../lib/format';
import {ProgressBar} from '../ui/ProgressBar';

type Props = {
  torrent: Torrent;
  selected: boolean;
  onSelect: (e: MouseEvent) => void;
  onPause: () => void;
  onResume: () => void;
  onRemove: () => void;
};

export function TorrentCard(props: Props) {
  const t = () => props.torrent;
  const remaining = () => t().total_bytes - t().bytes_done;
  const statusColor = () => {
    if (t().paused) return 'text-paused';
    if (t().completed) return 'text-seed';
    return 'text-down';
  };

  return (
    <div
      class="group rounded-lg border border-white/[.06] bg-white/[.02] p-3 transition-colors duration-150 hover:border-white/[.10] hover:bg-white/[.04]"
      classList={{'!border-accent-500/40 !bg-accent-500/[.04]': props.selected}}
      onClick={props.onSelect}
    >
      <div class="flex items-baseline justify-between gap-3">
        <div class="flex items-center gap-2 min-w-0">
          <span class={`h-1.5 w-1.5 shrink-0 rounded-full ${t().paused ? 'bg-paused' : t().completed ? 'bg-seed' : 'bg-down animate-pulse'}`} />
          <div class="truncate font-medium text-zinc-100">{t().name}</div>
        </div>
        <div class="flex shrink-0 items-center gap-1.5">
          <Show when={t().verifying}>
            <span class="inline-flex items-center gap-1 rounded bg-amber-500/[.10] px-1.5 py-0.5 text-[10px] uppercase tracking-wider text-amber-300">
              <Loader2 class="h-2.5 w-2.5 animate-spin" />
              Verifying
            </span>
          </Show>
          <Show when={t().files_missing}>
            <span class="inline-flex items-center gap-1 rounded bg-rose-500/[.10] px-1.5 py-0.5 text-[10px] uppercase tracking-wider text-rose-300">
              <AlertTriangle class="h-2.5 w-2.5" />
              Files missing
            </span>
          </Show>
          <Show when={t().queued && !t().completed}>
            <span class="inline-flex items-center gap-1 rounded bg-zinc-800/60 px-1.5 py-0.5 font-mono text-[10px] tabular-nums text-zinc-400">
              Q{t().queue_position + 1}
            </span>
          </Show>
          <Show when={t().force_start}>
            <Star class="h-3 w-3 text-amber-400" fill="currentColor" />
          </Show>
          <span class="font-mono text-xs tabular-nums text-zinc-500">{fmtBytes(t().total_bytes)}</span>
        </div>
      </div>

      <div class="mt-2.5">
        <ProgressBar
          value={t().progress}
          active={!t().paused && !t().completed}
          status={t().files_missing ? 'error' : t().paused ? 'paused' : t().completed ? 'completed' : 'downloading'}
        />
      </div>

      <div class="mt-2 flex items-center justify-between text-xs text-zinc-400">
        <span class="font-mono tabular-nums">
          <span class={statusColor()}>{fmtPercent(t().progress)}</span>
          <Show when={!t().completed}>
            <span class="mx-1.5 text-zinc-600">·</span>
            <span>↓ {fmtRate(t().download_rate)}</span>
          </Show>
          <span class="mx-1.5 text-zinc-600">·</span>
          <span>↑ {fmtRate(t().upload_rate)}</span>
          <span class="mx-1.5 text-zinc-600">·</span>
          <span>{t().peers} peers</span>
          <Show when={!t().completed && t().download_rate > 0}>
            <span class="mx-1.5 text-zinc-600">·</span>
            <span>{fmtETA(remaining(), t().download_rate)}</span>
          </Show>
        </span>
        <span class="flex shrink-0 gap-1 opacity-0 transition-opacity duration-150 group-hover:opacity-100">
          <Show
            when={!t().paused}
            fallback={
              <button
                class="grid h-6 w-6 place-items-center rounded text-zinc-400 hover:bg-white/10 hover:text-zinc-100"
                onClick={(e) => { e.stopPropagation(); props.onResume(); }}
                title="Resume"
              >
                <Play class="h-3 w-3" />
              </button>
            }
          >
            <button
              class="grid h-6 w-6 place-items-center rounded text-zinc-400 hover:bg-white/10 hover:text-zinc-100"
              onClick={(e) => { e.stopPropagation(); props.onPause(); }}
              title="Pause"
            >
              <PauseIcon class="h-3 w-3" />
            </button>
          </Show>
          <button
            class="grid h-6 w-6 place-items-center rounded text-zinc-400 hover:bg-rose-500/20 hover:text-rose-300"
            onClick={(e) => { e.stopPropagation(); props.onRemove(); }}
            title="Remove"
          >
            <Trash2 class="h-3 w-3" />
          </button>
        </span>
      </div>
    </div>
  );
}
