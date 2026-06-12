import {createSignal, Index, Show} from 'solid-js';
import {Trash2} from 'lucide-solid';
import {toast} from 'solid-sonner';
import type {DetailDTO} from '../../lib/bindings';
import {api} from '../../lib/bindings';
import {fmtTimestamp} from '../../lib/format';
import {userErr} from '../../lib/errors';

type Props = {detail: DetailDTO | null};

// <Index> instead of <For>: every WS tick replaces inspectorDetail with a
// fresh DTO, so referentially-keyed <For> would unmount+remount every
// tracker row each second. <Index> keeps the row DOM and updates fields
// in place. Matches FilesTab's approach.

export function TrackersTab(props: Props) {
  const [newURL, setNewURL] = createSignal('');
  const [adding, setAdding] = createSignal(false);
  const [addErr, setAddErr] = createSignal('');

  async function handleAdd() {
    const url = newURL().trim();
    if (!url || !props.detail?.id) return;
    setAdding(true);
    setAddErr('');
    try {
      await api.addTracker(props.detail.id, url);
      setNewURL('');
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : String(err);
      setAddErr(msg);
    } finally {
      setAdding(false);
    }
  }

  async function handleRemove(url: string) {
    if (!props.detail?.id) return;
    try {
      await api.removeTracker(props.detail.id, url);
    } catch (err) {
      toast.error(`Couldn't remove tracker — ${userErr(err)}`);
    }
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
            <div class="group border-b border-white/[.03] px-4 py-2 text-xs">
              <div class="flex items-baseline justify-between gap-2">
                <span class="truncate font-mono text-zinc-300" title={t().url}>{t().url}</span>
                <div class="flex shrink-0 items-center gap-1">
                  <span
                    class="rounded px-1.5 py-0.5 text-[10px] uppercase tracking-wider"
                    classList={{
                      'bg-seed/[.10] text-seed': t().status === 'OK',
                      'bg-amber-500/[.10] text-amber-400': t().status === 'Updating',
                      'bg-rose-500/[.10] text-rose-300': t().status.startsWith('Error'),
                      'bg-zinc-700/30 text-zinc-400': !['OK', 'Updating'].includes(t().status) && !t().status.startsWith('Error'),
                    }}
                  >
                    {t().status}
                  </span>
                  <button
                    class="rounded p-0.5 text-zinc-600 opacity-0 transition-opacity hover:text-rose-400 group-hover:opacity-100"
                    title="Remove tracker"
                    onClick={() => void handleRemove(t().url)}
                  >
                    <Trash2 size={12} />
                  </button>
                </div>
              </div>
              <div class="mt-1 flex justify-between font-mono tabular-nums text-zinc-500">
                <span>Seeds {t().seeds} · Peers {t().peers}</span>
                <span>Last {fmtTimestamp(t().last_announce)}</span>
              </div>
            </div>
          )}
        </Index>
      </Show>

      {/* Add tracker form */}
      <div class="border-t border-white/[.04] px-4 py-3">
        <div class="flex gap-2">
          <input
            type="text"
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
          <p class="mt-1 text-[10px] text-rose-400">{addErr()}</p>
        </Show>
      </div>
    </div>
  );
}
