import {Show} from 'solid-js';
import {Copy} from 'lucide-solid';
import {toast} from 'solid-sonner';
import type {DetailDTO} from '../../lib/bindings';
import {fmtBytes, fmtPercent, fmtTimestamp} from '../../lib/format';

type Props = {detail: DetailDTO | null};

function Row(props: {label: string; children: any}) {
  return (
    <div class="flex justify-between gap-3 border-b border-white/[.03] py-2 text-xs">
      <span class="text-zinc-500">{props.label}</span>
      <span class="text-right font-mono tabular-nums text-zinc-200 break-all">{props.children}</span>
    </div>
  );
}

export function OverviewTab(props: Props) {
  return (
    <Show
      when={props.detail}
      fallback={<div class="p-4 text-xs text-zinc-500">Loading…</div>}
    >
      {(d) => (
        <div class="px-4 py-2">
          <Row label="Save path">{d().save_path}</Row>
          <Row label="Size">{fmtBytes(d().total_bytes)}</Row>
          <Row label="Done">
            {fmtBytes(d().bytes_done)} ({fmtPercent(d().progress)})
          </Row>
          <Row label="Ratio">{d().ratio.toFixed(2)}</Row>
          <Row label="Total ↓ / ↑">
            {fmtBytes(d().total_down)} / {fmtBytes(d().total_up)}
          </Row>
          <Row label="Peers / Seeds">
            {d().peers} / {d().seeds}
          </Row>
          <Row label="Added">{fmtTimestamp(d().added_at)}</Row>
          <Show when={d().completed_at}>
            <Row label="Completed">{fmtTimestamp(d().completed_at!)}</Row>
          </Show>
          <Row label="Magnet">
            <span class="inline-flex items-center gap-1.5">
              <button
                type="button"
                class="grid h-5 w-5 place-items-center rounded text-zinc-500 hover:bg-white/[.06] hover:text-zinc-200"
                onClick={() => {
                  navigator.clipboard.writeText(d().magnet);
                  toast.success('Magnet copied');
                }}
                title="Copy magnet"
              >
                <Copy class="h-3 w-3" />
              </button>
              <span class="max-w-[180px] truncate text-zinc-400">{d().magnet || '—'}</span>
            </span>
          </Row>
        </div>
      )}
    </Show>
  );
}
