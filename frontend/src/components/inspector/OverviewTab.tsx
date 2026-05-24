import {createEffect, createSignal, Show} from 'solid-js';
import {Copy} from 'lucide-solid';
import {toast} from 'solid-sonner';
import type {DetailDTO, SeedPolicyDTO} from '../../lib/bindings';
import {api} from '../../lib/bindings';
import {fmtBytes, fmtPercent, fmtTimestamp} from '../../lib/format';
import {userErr} from '../../lib/errors';

type Props = {
  detail: DetailDTO | null;
  sequential: boolean;
  onToggleSequential: () => void;
};

function Row(props: {label: string; children: any}) {
  return (
    <div class="flex justify-between gap-3 border-b border-white/[.03] py-2 text-xs">
      <span class="text-zinc-500">{props.label}</span>
      <span class="text-right font-mono tabular-nums text-zinc-200 break-all">{props.children}</span>
    </div>
  );
}

function RateLimitRow(props: {
  label: string;
  kbps: number;
  onSave: (kbps: number) => Promise<void>;
}) {
  const [input, setInput] = createSignal(props.kbps > 0 ? String(props.kbps) : '');
  createEffect(() => {
    setInput(props.kbps > 0 ? String(props.kbps) : '');
  });

  const commit = async () => {
    const raw = input().trim();
    const val = raw === '' ? 0 : parseInt(raw, 10);
    if (isNaN(val) || val < 0) {
      setInput(props.kbps > 0 ? String(props.kbps) : '');
      return;
    }
    if (val === props.kbps) return;
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
          class="w-20 rounded border border-white/[.06] bg-black/30 px-1.5 py-0.5 font-mono text-xs text-zinc-100 focus:border-accent-500/60 focus:outline-none focus:ring-2 focus:ring-accent-500/40 [appearance:textfield] [&::-webkit-outer-spin-button]:appearance-none [&::-webkit-inner-spin-button]:appearance-none"
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

function ToggleRow(props: {label: string; description: string; checked: boolean; onChange: () => void}) {
  return (
    <div class="flex items-center justify-between gap-3 border-b border-white/[.03] py-2 text-xs">
      <div class="flex flex-col gap-0.5">
        <span class="text-zinc-400">{props.label}</span>
        <span class="text-zinc-600">{props.description}</span>
      </div>
      <button
        type="button"
        role="switch"
        aria-checked={props.checked}
        onClick={props.onChange}
        class={`relative inline-flex h-4 w-7 shrink-0 cursor-pointer items-center rounded-full transition-colors focus:outline-none focus-visible:ring-2 focus-visible:ring-white/20 ${props.checked ? 'bg-accent-500' : 'bg-white/10'}`}
      >
        <span
          class={`inline-block h-3 w-3 transform rounded-full bg-white shadow transition-transform ${props.checked ? 'translate-x-3.5' : 'translate-x-0.5'}`}
        />
      </button>
    </div>
  );
}

function SeedPolicySection(props: {infohash: string; completed: boolean}) {
  const [, setPolicy] = createSignal<SeedPolicyDTO | null>(null);
  const [useGlobal, setUseGlobal] = createSignal(true);
  const [noLimit, setNoLimit] = createSignal(false);
  const [ratioEnabled, setRatioEnabled] = createSignal(false);
  const [ratio, setRatio] = createSignal('');
  const [timeEnabled, setTimeEnabled] = createSignal(false);
  const [timeMin, setTimeMin] = createSignal('');

  createEffect(() => {
    const hash = props.infohash;
    if (!hash) return;
    api.getTorrentSeedPolicy(hash).then((p) => {
      setPolicy(p);
      if (p.use_global) {
        setUseGlobal(true);
        setNoLimit(false);
        setRatioEnabled(false);
        setTimeEnabled(false);
      } else {
        setUseGlobal(false);
        const hasRatio = p.ratio_limit !== null && p.ratio_limit !== undefined;
        const hasTime = p.time_min_limit !== null && p.time_min_limit !== undefined;
        if (!hasRatio && !hasTime) {
          setNoLimit(true);
          setRatioEnabled(false);
          setTimeEnabled(false);
        } else {
          setNoLimit(false);
          setRatioEnabled(hasRatio);
          setRatio(hasRatio ? String(p.ratio_limit) : '');
          setTimeEnabled(hasTime);
          setTimeMin(hasTime ? String(p.time_min_limit) : '');
        }
      }
    }).catch(() => {});
  });

  const save = async () => {
    const hash = props.infohash;
    if (!hash) return;
    let dto: SeedPolicyDTO;
    if (useGlobal()) {
      dto = {use_global: true, ratio_limit: null, time_min_limit: null};
    } else if (noLimit()) {
      dto = {use_global: false, ratio_limit: null, time_min_limit: null};
    } else {
      const r = ratioEnabled() && ratio() !== '' ? parseFloat(ratio()) : null;
      const t = timeEnabled() && timeMin() !== '' ? parseInt(timeMin(), 10) : null;
      dto = {use_global: false, ratio_limit: r, time_min_limit: t};
    }
    try {
      await api.setTorrentSeedPolicy(hash, dto);
      setPolicy(dto);
      toast.success('Seed policy saved');
    } catch (e) {
      toast.error(`Couldn't save seed policy — ${String(e)}`);
    }
  };

  return (
    <Show when={props.completed}>
      <div class="mt-3 border-t border-white/[.04] pt-3">
        <div class="mb-2 text-[10px] uppercase tracking-wider text-zinc-500">Seeding limits</div>
        <div class="flex flex-col gap-1 mb-2">
          <label class="flex items-center gap-2 text-xs text-zinc-300 cursor-pointer">
            <input type="radio" name="seed-mode" checked={useGlobal()}
              onChange={() => { setUseGlobal(true); setNoLimit(false); }}
              class="accent-accent-500" />
            Use global defaults
          </label>
          <label class="flex items-center gap-2 text-xs text-zinc-300 cursor-pointer">
            <input type="radio" name="seed-mode" checked={!useGlobal() && noLimit()}
              onChange={() => { setUseGlobal(false); setNoLimit(true); }}
              class="accent-accent-500" />
            No limit
          </label>
          <label class="flex items-center gap-2 text-xs text-zinc-300 cursor-pointer">
            <input type="radio" name="seed-mode" checked={!useGlobal() && !noLimit()}
              onChange={() => { setUseGlobal(false); setNoLimit(false); }}
              class="accent-accent-500" />
            Custom
          </label>
        </div>
        <Show when={!useGlobal() && !noLimit()}>
          <div class="flex flex-col gap-2 pl-1 mb-2">
            <label class="flex items-center gap-2 text-xs text-zinc-300">
              <input type="checkbox" checked={ratioEnabled()}
                onChange={(e) => setRatioEnabled(e.currentTarget.checked)}
                class="accent-accent-500" />
              Stop at ratio
              <Show when={ratioEnabled()}>
                <input type="number" min={0} step={0.1}
                  class="w-20 rounded border border-white/[.06] bg-black/30 px-2 py-0.5 text-right font-mono text-xs tabular-nums text-zinc-100 focus:border-accent-500/60 focus:outline-none"
                  value={ratio()} placeholder="e.g. 2.0"
                  onInput={(e) => setRatio(e.currentTarget.value)} />
              </Show>
            </label>
            <label class="flex items-center gap-2 text-xs text-zinc-300">
              <input type="checkbox" checked={timeEnabled()}
                onChange={(e) => setTimeEnabled(e.currentTarget.checked)}
                class="accent-accent-500" />
              Stop after
              <Show when={timeEnabled()}>
                <input type="number" min={1}
                  class="w-20 rounded border border-white/[.06] bg-black/30 px-2 py-0.5 text-right font-mono text-xs tabular-nums text-zinc-100 focus:border-accent-500/60 focus:outline-none"
                  value={timeMin()} placeholder="minutes"
                  onInput={(e) => setTimeMin(e.currentTarget.value)} />
                <span class="text-zinc-500">min</span>
              </Show>
            </label>
          </div>
        </Show>
        <button type="button" onClick={save}
          class="mt-1 rounded bg-accent-500/20 px-3 py-1 text-xs font-medium text-accent-400 hover:bg-accent-500/30 transition-colors">
          Save
        </button>
      </div>
    </Show>
  );
}

export function OverviewTab(props: Props) {
  const [downKbps, setDownKbps] = createSignal(0);
  const [upKbps, setUpKbps] = createSignal(0);

  createEffect(() => {
    const d = props.detail;
    if (!d) return;
    const id = d.id;
    api.getTorrentRateLimits(id).then((limits) => {
      setDownKbps(limits.down_kbps);
      setUpKbps(limits.up_kbps);
    }).catch(() => {});
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
          <ToggleRow
            label="Sequential download"
            description="Download pieces in order (useful for streaming media)"
            checked={props.sequential}
            onChange={props.onToggleSequential}
          />
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
          <SeedPolicySection infohash={d().id} completed={d().completed} />
        </div>
      )}
    </Show>
  );
}
