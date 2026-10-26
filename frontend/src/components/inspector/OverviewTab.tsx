import {createEffect, createMemo, createSignal, on, Show} from 'solid-js';
import {SeedPolicySection} from './SeedPolicySection';
import {HealthPanel} from './HealthPanel';
import {StorageSection} from './StorageSection';
import {Copy} from 'lucide-solid';
import {toast} from 'solid-sonner';
import type {DetailDTO, InspectorTab, SeedingDefaultsDTO} from '../../lib/bindings';
import {api} from '../../lib/bindings';
import {fmtBytes, fmtPercent, fmtTimestamp} from '../../lib/format';
import {isWailsRuntime} from '../../lib/runtime';
import {userErr} from '../../lib/errors';

type Props = {
  detail: DetailDTO | null;
  canEdit: boolean;
  seedingDefaults?: SeedingDefaultsDTO;
  canRelocate?: boolean;
  canChangeSettings?: boolean;
  downloadRate?: number;
  onTabChange: (tab: InspectorTab) => void;
  sequential: boolean;
  onToggleSequential: () => void;
};

function Row(props: {label: string; children: any}) {
  return (
    <div class="flex justify-between gap-3 border-b border-white/[.03] py-2 text-xs">
      <span class="shrink-0 text-zinc-500">{props.label}</span>
      <span class="min-w-0 text-right font-mono tabular-nums text-zinc-200 break-all">{props.children}</span>
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
      <span class="shrink-0 text-zinc-500">{props.label}</span>
      <span class="flex items-center gap-1.5">
        <input
          type="number"
          aria-label={props.label}
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
        aria-label={props.label}
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

export function OverviewTab(props: Props) {
  const [downKbps, setDownKbps] = createSignal(0);
  const [upKbps, setUpKbps] = createSignal(0);

  // Keyed on the torrent id only — props.detail is replaced wholesale every
  // inspector tick (~1Hz), so tracking it directly re-fetched the rate
  // limits once per second for as long as the tab was open. Responses that
  // arrive after the user switched torrents are discarded so a slow reply
  // for torrent A can't populate B's inputs.
  const torrentID = createMemo(() => props.detail?.id);
  createEffect(on(torrentID, (id) => {
    if (!id) return;
    api.getTorrentRateLimits(id).then((limits) => {
      if (props.detail?.id !== id) return; // stale — torrent changed mid-flight
      setDownKbps(limits.down_kbps);
      setUpKbps(limits.up_kbps);
    }).catch(() => {});
  }));

  const saveDown = async (kbps: number) => {
    const id = props.detail?.id;
    if (!id || !props.canEdit) return;
    await api.setTorrentRateLimits(id, kbps, upKbps());
    setDownKbps(kbps);
    toast.success(kbps === 0 ? 'Download limit removed' : `Download limited to ${kbps} KB/s`);
  };

  const saveUp = async (kbps: number) => {
    const id = props.detail?.id;
    if (!id || !props.canEdit) return;
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
          <HealthPanel detail={d()} rate={props.downloadRate ?? 0} canEdit={props.canEdit} onTabChange={props.onTabChange} />
          <Row label="Save path">
            <button type="button" class="break-all text-accent-300 hover:underline" title={isWailsRuntime() ? 'Open download folder' : 'Copy save path'} onClick={async () => {
              try {
                if (isWailsRuntime()) await api.openFolder(d().save_path);
                else { await navigator.clipboard.writeText(d().save_path); toast.success('Save path copied'); }
              } catch (err) { toast.error(`Couldn't access folder — ${userErr(err)}`); }
            }}>{d().save_path}</button>
          </Row>
          <Row label="Size"><button type="button" class="text-accent-300 hover:underline" title="View files" onClick={() => props.onTabChange('files')}>{fmtBytes(d().total_bytes)} · Files</button></Row>
          <Row label="Done">
            {fmtBytes(d().bytes_done)} ({fmtPercent(d().progress)})
          </Row>
          <Row label="Ratio">{d().ratio.toFixed(2)}</Row>
          <Row label="Total ↓ / ↑">
            {fmtBytes(d().total_down)} / {fmtBytes(d().total_up)}
          </Row>
          <Row label="Peers / Seeds">
            <button type="button" class="text-accent-300 hover:underline" title="View connected peers" onClick={() => props.onTabChange('peers')}>{d().peers} / {d().seeds}</button>
          </Row>
          <Row label="Added">{fmtTimestamp(d().added_at)}</Row>
          <Show when={d().completed_at}>
            <Row label="Completed">{fmtTimestamp(d().completed_at!)}</Row>
          </Show>
          <fieldset disabled={!props.canEdit} aria-label="Torrent transfer settings" class="min-w-0">
            <RateLimitRow label="↓ Speed limit" kbps={downKbps()} onSave={saveDown} />
            <RateLimitRow label="↑ Speed limit" kbps={upKbps()} onSave={saveUp} />
            <ToggleRow
              label="Sequential download"
              description="Download pieces in order (useful for streaming media)"
              checked={props.sequential}
              onChange={() => { if (props.canEdit) props.onToggleSequential(); }}
            />
          </fieldset>
          <Row label="Info hash">
            <button type="button" class="inline-flex max-w-full items-center gap-1.5 text-zinc-400 hover:text-zinc-100" title="Copy info hash" onClick={async () => {
              try { await navigator.clipboard.writeText(d().id); toast.success('Info hash copied'); }
              catch (err) { toast.error(`Couldn't copy info hash — ${userErr(err)}`); }
            }}><span class="truncate">{d().id}</span><Copy class="h-3 w-3 shrink-0" /></button>
          </Row>
          <Row label="Magnet">
            <button type="button" disabled={!d().magnet} class="inline-flex max-w-full items-center gap-1.5 text-accent-300 hover:underline disabled:opacity-40" title="Copy magnet link" onClick={async () => {
              try { await navigator.clipboard.writeText(d().magnet); toast.success('Magnet copied'); }
              catch (err) { toast.error(`Couldn't copy magnet — ${userErr(err)}`); }
            }}><span>Copy magnet link</span><Copy class="h-3 w-3 shrink-0" /></button>
          </Row>
          <StorageSection detail={d()} canRelocate={props.canRelocate ?? false} canChangeSettings={props.canChangeSettings ?? false} />
          <SeedPolicySection canEdit={props.canEdit} infohash={d().id} defaults={props.seedingDefaults} />
        </div>
      )}
    </Show>
  );
}
