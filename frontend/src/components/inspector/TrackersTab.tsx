import {createEffect, createMemo, createSignal, on, Index, Show} from 'solid-js';
import {Trash2} from 'lucide-solid';
import {toast} from 'solid-sonner';
import type {DetailDTO} from '../../lib/bindings';
import {api} from '../../lib/bindings';
import {fmtTimestamp} from '../../lib/format';
import {userErr} from '../../lib/errors';

type Props = {detail: DetailDTO | null; canEdit: boolean};

// <Index> instead of <For>: every WS tick replaces inspectorDetail with a
// fresh DTO, so referentially-keyed <For> would unmount+remount every
// tracker row each second. <Index> keeps the row DOM and updates fields
// in place. Matches FilesTab's approach.

export function TrackersTab(props: Props) {
  const [newURL, setNewURL] = createSignal('');
  const [adding, setAdding] = createSignal(false);
  const [addErr, setAddErr] = createSignal('');
  const [removing, setRemoving] = createSignal(false);
  const torrentID = createMemo(() => props.detail?.id);
  createEffect(on(torrentID, () => { setNewURL(''); setAddErr(''); }));

  async function handleAdd() {
    const url = newURL().trim();
    if (!props.canEdit || !url || !props.detail?.id || adding()) return;
    const id = props.detail.id;
    setAdding(true);
    setAddErr('');
    try {
      await api.addTracker(id, url);
      if (props.detail?.id === id) setNewURL('');
      toast.success('Tracker added');
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : String(err);
      if (props.detail?.id === id) setAddErr(msg);
    } finally {
      setAdding(false);
    }
  }

  async function handleRemove(url: string) {
    if (!props.canEdit || !props.detail?.id || removing()) return;
    setRemoving(true);
    try {
      await api.removeTracker(props.detail.id, url);
    } catch (err) {
      toast.error(`Couldn't remove tracker — ${userErr(err)}`);
    } finally { setRemoving(false); }
  }

  function handleKeyDown(e: KeyboardEvent) {
    if (e.key === 'Enter') void handleAdd();
  }

  return (
    <div class="flex flex-col">
      <Show
        when={props.detail?.trackers?.length}
        fallback={<div class="p-4 text-xs text-zinc-500">No trackers known.</div>}
      >
        <Index each={props.detail!.trackers!}>
          {(t) => (
            <div class="border-b border-white/[.03] px-4 py-2 text-xs">
              <div class="flex min-w-0 items-center justify-between gap-2">
                <button type="button" class="min-w-0 truncate text-left font-mono text-zinc-300 hover:text-accent-300" title={`Copy tracker URL: ${t().url}`} onClick={async () => {
                  try { await navigator.clipboard.writeText(t().url); toast.success('Tracker URL copied'); }
                  catch (err) { toast.error(`Couldn't copy tracker — ${userErr(err)}`); }
                }}>{t().url}</button>
                <Show when={props.canEdit}>
                  <button type="button" disabled={removing()} aria-label={`Remove tracker ${t().url}`}
                    class="shrink-0 rounded p-1.5 text-zinc-500 hover:bg-white/[.04] hover:text-rose-400 focus-visible:text-rose-400 disabled:opacity-40"
                    title="Remove tracker" onClick={() => void handleRemove(t().url)}><Trash2 size={13} /></button>
                </Show>
              </div>
              <div class={`mt-1 break-words text-[10px] [overflow-wrap:anywhere] ${t().status.startsWith('Error') ? 'text-rose-300' : 'text-zinc-400'}`}>
                {t().status}
              </div>
              <div class="mt-2 flex flex-wrap justify-between gap-1 font-mono text-[10px] tabular-nums text-zinc-500">
                <Show when={t().last_announce > 0} fallback={<span>Announce statistics unavailable</span>}>
                  <span>Seeds {t().seeds} · Peers {t().peers}</span>
                  <span>Last {fmtTimestamp(t().last_announce)}</span>
                </Show>
              </div>
            </div>
          )}
        </Index>
      </Show>

      <Show when={props.canEdit}>
        {/* Add tracker form */}
        <div class="border-t border-white/[.04] px-4 py-3">
          <div class="flex gap-2">
            <input
              type="text"
              aria-label="Tracker URL"
              placeholder="https://tracker.example/announce"
              class="min-w-0 flex-1 rounded border border-white/[.08] bg-white/[.04] px-2 py-1 font-mono text-xs text-zinc-200 placeholder-zinc-600 outline-none focus:border-white/20 focus:bg-white/[.06]"
              value={newURL()}
              onInput={(e) => { setNewURL(e.currentTarget.value); setAddErr(''); }}
              onKeyDown={handleKeyDown}
              disabled={adding()}
            />
            <button
              class="shrink-0 rounded border border-white/[.08] bg-white/[.04] px-3 py-1 text-xs text-zinc-300 transition-colors hover:border-white/20 hover:bg-white/[.08] disabled:opacity-40"
              onClick={() => void handleAdd()}
              disabled={adding() || !newURL().trim()}
            >
              {adding() ? 'Adding…' : 'Add'}
            </button>
          </div>
          <Show when={addErr()}>
            <p role="alert" class="mt-1 break-words text-[10px] text-rose-400 [overflow-wrap:anywhere]">{addErr()}</p>
          </Show>
        </div>
      </Show>
    </div>
  );
}
