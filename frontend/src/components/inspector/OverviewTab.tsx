import {createEffect, createSignal, Show} from 'solid-js';
import {Copy} from 'lucide-solid';
import {toast} from 'solid-sonner';
import {api} from '../../lib/bindings';
import type {DetailDTO} from '../../lib/bindings';
import {fmtBytes, fmtPercent, fmtTimestamp} from '../../lib/format';
import {userErr} from '../../lib/errors';

type Props = {detail: DetailDTO | null};

function Row(props: {label: string; children: any}) {
  return (
    <div class="flex justify-between gap-3 border-b border-white/[.03] py-2 text-xs">
      <span class="text-zinc-500">{props.label}</span>
      <span class="text-right font-mono tabular-nums text-zinc-200 break-all">{props.children}</span>
    </div>
  );
}

// RateLimitRow renders a KB/s number input. 0 shows as empty with placeholder
// "Unlimited". On blur or Enter the value is committed if it changed.
function RateLimitRow(props: {
  label: string;
  kbps: number;
  onSave: (kbps: number) => Promise<void>;
}) {
  const [input, setInput] = createSignal(props.kbps > 0 ? String(props.kbps) : '');
  // Re-sync when the external value changes (e.g. the inspector switches torrent).
  createEffect(() => {
    setInput(props.kbps > 0 ? String(props.kbps) : '');
  });

  const commit = async () => {
    const raw = input().trim();
    const val = raw === '' ? 0 : parseInt(raw, 10);
    if (isNaN(val) || val < 0) {
      // Reset to persisted value on invalid input.
      setInput(props.kbps > 0 ? String(props.kbps) : '');
      return;
    }
    if (val === props.kbps) return; // no change
    try {
      await props.onSave(val);
    } catch (e) {
      toast.error(`Couldn't set rate limit — ${userErr(e)}`);
      setInput(props.kbps > 0 ? String(props.kbps) : '');
    }
  };

  return (
    <div class="flex justify-between gap-3 border-b border-white/[.03] py-2 text-xs items-center">
      <span class="text-zinc-500">{props.label}</span>
      <span class="flex items-center gap-1.5">
        <input
          type="number"
          min="0"
          step="1"
          class="w-20 rounded border border-white/[.06] bg-black/30 px-1.5 py-0.5 font-mono text-xs text-zinc-100 focus:border-accent-500/50 focus:outline-none focus:ring-1 focus:ring-accent-500/30 [appearance:textfield] [&::-webkit-outer-spin-button]:appearance-none [&::-webkit-inner-spin-button]:appearance-none"
          placeholder="Unlimited"
          value={input()}
          onInput={(e) => setInput(e.currentTarget.value)}
          onBlur={commit}
          onKeyDown={(e) => { if (e.key === 'Enter') { e.currentTarget.blur(); } }}
        />
        <span class="text-zinc-600">KB/s</span>
      </span>
    </div>
  );
}

export function OverviewTab(props: Props) {
  // Per-torrent rate limits loaded from the backend. Reset when the focused
  // torrent changes (detail.id changes). On the first render the limits
  // default to 0 (unlimited) until the fetch returns.
  const [downKbps, setDownKbps] = createSignal(0);
  const [upKbps, setUpKbps] = createSignal(0);

  createEffect(() => {
    const d = props.detail;
    if (!d) return;
    const id = d.id;
    api.getTorrentRateLimits(id).then((limits) => {
      setDownKbps(limits.down_kbps);
      setUpKbps(limits.up_kbps);
    }).catch(() => {
      // If the fetch fails (e.g. torrent just removed), leave at 0.
    });
  });

  const saveDown = async (kbps: number) => {
    const id = props.detail?.id;
    if (!id) return;
    await api.setTorrentRateLimits(id, kbps, upKbps());
    setDownKbps(kbps);
    toast.success(kbps === 0 ? 'Download limit removed' : `Download limited to ${kbps} KB/s`);
  };

  const saveUp = async (kbps: number) => {
    const id = props.detail?.id;
    if (!id) return;
    await api.setTorrentRateLimits(id, downKbps(), kbps);
    setUpKbps(kbps);
    toast.success(kbps === 0 ? 'Upload limit removed' : `Upload limited to ${kbps} KB/s`);
  };

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
          <RateLimitRow label="↓ Speed limit" kbps={downKbps()} onSave={saveDown} />
          <RateLimitRow label="↑ Speed limit" kbps={upKbps()} onSave={saveUp} />
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
