import {Dialog} from '@kobalte/core/dialog';
import {createEffect, createMemo, createSignal, Index, on, onCleanup, Show} from 'solid-js';
import {X} from 'lucide-solid';
import {api, type TransferStatisticsDTO} from '../../lib/bindings';
import {fmtBytes, fmtTimestamp} from '../../lib/format';
import {userErr} from '../../lib/errors';
import {Button} from '../ui/Button';

export function TransferStatisticsModal(props: {open: boolean; onClose: () => void}) {
  const [data, setData] = createSignal<TransferStatisticsDTO | null>(null);
  const [busy, setBusy] = createSignal(false), [error, setError] = createSignal('');
  const [range, setRange] = createSignal(30), [selectedDay, setSelectedDay] = createSignal<string | null>(null);
  let refresh = async () => {};
  createEffect(on(() => props.open, (open) => {
    if (!open) return;
    let disposed = false, pending = false;
    setData(null);setError('');setSelectedDay(null);
    refresh = async () => {
      if (pending) return;pending = true;setBusy(true);
      try {const result = await api.getTransferStatistics();if (!disposed) {setData(result);setError('');}}
      catch(e){if (!disposed) setError(userErr(e));}
      finally{pending = false;if (!disposed) setBusy(false);}
    };
    void refresh();const timer = setInterval(refresh, 30_000);
    onCleanup(() => {disposed = true;clearInterval(timer);});
  }));
  const days = createMemo(() => {
    const result = data();if (!result) return [];
    const known = new Map(result.days.map((day) => [day.day, day]));
    const end = new Date(result.sampled_at * 1000);end.setUTCHours(0, 0, 0, 0);
    return Array.from({length: range()}, (_, i) => {
      const day = new Date(end.getTime() - (range() - 1 - i) * 86400000).toISOString().slice(0, 10);
      return known.get(day) ?? {day, downloaded: 0, uploaded: 0};
    });
  });
  const peak = () => Math.max(1, ...days().map((d) => d.downloaded + d.uploaded));
  const selection = () => days().find((d) => d.day === selectedDay());
  return <Dialog open={props.open} onOpenChange={(open) => {if (!open) props.onClose();}}>
    <Dialog.Portal><Dialog.Overlay class="fixed inset-0 z-40 bg-black/60 backdrop-blur-sm" /><div class="fixed inset-0 z-50 grid place-items-center p-4"><Dialog.Content class="max-h-[90vh] w-full max-w-2xl overflow-y-auto rounded-xl border border-white/10 bg-zinc-900 p-5 shadow-2xl">
      <div class="flex items-start justify-between gap-3"><div><Dialog.Title class="text-base font-semibold text-zinc-100">Transfer statistics</Dialog.Title><Dialog.Description class="mt-1 text-xs text-zinc-500">Your accessible library · across connections and app restarts</Dialog.Description></div><Dialog.CloseButton aria-label="Close transfer statistics" class="rounded p-1 text-zinc-500 hover:bg-white/[.05] hover:text-zinc-200"><X class="h-4 w-4" /></Dialog.CloseButton></div>
      <Show when={error()}><p role="alert" class="mt-3 text-xs text-rose-300">{error()} · Displayed totals may be out of date.</p></Show>
      <Show when={data()} fallback={<p class="my-6 text-xs text-zinc-500">{busy() ? 'Loading transfer history…' : 'Transfer history unavailable.'}</p>}>
        {(stats) => <>
          <table class="mt-4 w-full text-left text-xs"><thead class="text-zinc-500"><tr><th class="py-2 font-normal">Period</th><th class="py-2 text-right font-normal">Downloaded</th><th class="py-2 text-right font-normal">Uploaded</th><th class="py-2 text-right font-normal">Ratio</th></tr></thead><tbody>
            <Index each={[
              {label: 'This app session', value: stats().session},
              {label: 'Today · UTC', value: stats().today},
              {label: 'Library lifetime', value: stats().lifetime},
            ]}>{(row) => <tr class="border-t border-white/[.05] text-zinc-300"><td class="py-3">{row().label}</td><td class="py-3 text-right font-mono">{fmtBytes(row().value.downloaded)}</td><td class="py-3 text-right font-mono">{fmtBytes(row().value.uploaded)}</td><td class="py-3 text-right font-mono">{row().value.downloaded > 0 ? (row().value.uploaded / row().value.downloaded).toFixed(2) : '—'}</td></tr>}</Index>
          </tbody></table>
          <div class="mt-5 flex flex-wrap items-center gap-3 text-xs"><span class="mr-auto text-zinc-400">Daily transfer history</span><span class="inline-flex items-center gap-1 text-zinc-500"><span class="h-1.5 w-1.5 rounded-full bg-down" />Downloaded</span><span class="inline-flex items-center gap-1 text-zinc-500"><span class="h-1.5 w-1.5 rounded-full bg-seed" />Uploaded</span><select aria-label="History date range" class="rounded border border-white/[.06] bg-zinc-900 px-2 py-1 text-zinc-300" value={range()} onChange={(e) => {setRange(Number(e.currentTarget.value));setSelectedDay(null);}}><option value="7">7 days</option><option value="30">30 days</option><option value="90">90 days</option></select></div>
          <div class="mt-3 flex h-36 items-end gap-1 rounded-lg border border-white/[.05] bg-black/10 px-3 pb-2 pt-3" role="group" aria-label="Daily transferred bytes">
            <Index each={days()}>{(day) => <button type="button" aria-label={`${day().day}: ${fmtBytes(day().downloaded)} downloaded, ${fmtBytes(day().uploaded)} uploaded`} title={`${day().day} · ↓ ${fmtBytes(day().downloaded)} · ↑ ${fmtBytes(day().uploaded)}`} aria-pressed={selectedDay() === day().day} onClick={() => setSelectedDay(day().day)} class="flex h-full min-w-0 flex-1 flex-col justify-end rounded-sm outline-none focus-visible:ring-2 focus-visible:ring-accent-400" classList={{'ring-1 ring-accent-400': selectedDay() === day().day}}><span class="block w-full rounded-t-sm bg-seed/80" style={{height: `${day().uploaded / peak() * 100}%`}} /><span class="block w-full bg-down/80" style={{height: `${day().downloaded / peak() * 100}%`, 'min-height': '2px', opacity: day().uploaded + day().downloaded ? 1 : 0.15}} /></button>}</Index>
          </div>
          <div class="mt-2 flex justify-between text-[10px] text-zinc-600"><span>{days()[0]?.day}</span><span>{days().at(-1)?.day} · UTC</span></div>
          <p class="mt-2 text-xs text-zinc-400">{selection() ? `${selection()!.day} · ↓ ${fmtBytes(selection()!.downloaded)} · ↑ ${fmtBytes(selection()!.uploaded)}` : 'Select a day to inspect its totals.'}</p>
          <p class="mt-4 text-[10px] leading-relaxed text-zinc-500">Session started {fmtTimestamp(stats().session_started_at)}. Removing a torrent removes its statistics. Daily history starts with this version and is retained for 90 days; earlier days are not reconstructed. Background samples are saved about every 30 seconds.</p>
          <p class="mt-1 text-[10px] text-zinc-600">Updated {fmtTimestamp(stats().sampled_at)}</p>
        </>}
      </Show>
      <div class="mt-4 flex justify-end"><Button type="button" class="text-xs" disabled={busy()} onClick={() => void refresh()}>{busy() ? 'Refreshing…' : 'Refresh statistics'}</Button></div>
    </Dialog.Content></div></Dialog.Portal>
  </Dialog>;
}
