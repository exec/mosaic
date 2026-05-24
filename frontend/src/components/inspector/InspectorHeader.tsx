import {X} from 'lucide-solid';
import {fmtBytes, fmtETA, fmtPercent, fmtRate} from '../../lib/format';
import {ProgressBar} from '../ui/ProgressBar';
import type {DetailDTO} from '../../lib/bindings';

type Props = {
  detail: DetailDTO | null;
  // Live B/s from the matching Torrent in the list. DetailDTO carries
  // cumulative totals but not instantaneous rate, so the header pulls
  // this via the same find-in-store pattern App.tsx uses for `sequential`.
  downloadRate: number;
  onClose: () => void;
};

export function InspectorHeader(props: Props) {
  return (
    <header class="border-b border-white/[.04] px-4 py-3">
      <div class="flex items-start justify-between gap-2">
        <div class="min-w-0">
          <div class="truncate text-sm font-semibold text-zinc-100">
            {props.detail?.name ?? '—'}
          </div>
          <div class="mt-0.5 font-mono text-xs tabular-nums text-zinc-500">
            {props.detail ? fmtBytes(props.detail.total_bytes) : '—'}
          </div>
        </div>
        <button
          type="button"
          onClick={props.onClose}
          class="grid h-7 w-7 shrink-0 place-items-center rounded-md text-zinc-500 hover:bg-white/[.04] hover:text-zinc-100"
          aria-label="Close inspector"
        >
          <X class="h-4 w-4" />
        </button>
      </div>
      <div class="mt-3">
        <ProgressBar
          value={props.detail?.progress ?? 0}
          active={!!props.detail && !props.detail.paused && !props.detail.completed && props.detail.progress < 1}
          status={
            props.detail?.files_missing ? 'error'
              : props.detail?.paused ? 'paused'
              : props.detail?.completed ? 'completed'
              : 'downloading'
          }
        />
      </div>
      <div class="mt-2 flex items-center justify-between font-mono text-xs tabular-nums text-zinc-500">
        <span>{fmtPercent(props.detail?.progress ?? 0)}</span>
        <span>
          {(() => {
            const d = props.detail;
            if (!d) return '—';
            const remaining = d.total_bytes - d.bytes_done;
            // Hide the "↓ rate · ETA" half once the torrent is at 100% —
            // both numbers are meaningless then and the previous code
            // happily printed "0 B/s · ETA ∞" forever, which read as a bug.
            if (d.completed || remaining <= 0) return '✓ Complete';
            if (d.paused) return 'Paused';
            return `↓ ${fmtRate(props.downloadRate)} · ETA ${fmtETA(remaining, props.downloadRate)}`;
          })()}
        </span>
      </div>
    </header>
  );
}
