import {createEffect, createMemo, createSignal, on, Show} from 'solid-js';
import type {DetailDTO} from '../../lib/bindings';
import {api} from '../../lib/bindings';
import {fmtBytes} from '../../lib/format';
import {Button} from '../ui/Button';
import {toast} from 'solid-sonner';
import {userErr} from '../../lib/errors';

export function StorageSection(props: {detail: DetailDTO; canRelocate: boolean; canChangeSettings: boolean}) {
  const [path, setPath] = createSignal('');
  const [storage, setStorage] = createSignal<{free_bytes: number; preallocate: boolean} | null>(null);
  const [busy, setBusy] = createSignal(false);
  const [error, setError] = createSignal('');
  const identity = createMemo(() => `${props.detail.id}:${props.detail.save_path}`);
  const refresh = async () => {const id = props.detail.id;try {const value = await api.getStorage(id);if (id === props.detail.id) setStorage(value);}catch(e){setError(userErr(e));}};
  createEffect(on(identity, () => {setPath('');setStorage(null);setError('');void refresh();}));
  const relocate = async (move: boolean) => {
    if (busy() || !props.canRelocate) return;
    setBusy(true);setError('');
    try {await api.relocateTorrent(props.detail.id, path().trim(), move);setPath('');await refresh();toast.success(move ? 'Data moved · verifying new location' : 'Location updated · verifying existing files');}
    catch(e){setError(userErr(e));}finally{setBusy(false);}
  };
  return <details class="my-3 border-t border-white/[.05] pt-3">
    <summary class="cursor-pointer text-xs font-medium text-zinc-300">Storage & location</summary>
    <div class="mt-3 space-y-3 text-xs text-zinc-500">
      <div class="flex items-center justify-between gap-2"><span>{storage() ? storage()!.free_bytes >= 0 ? `${fmtBytes(storage()!.free_bytes)} available` : 'Available space unknown' : 'Checking space…'}</span><button type="button" class="text-accent-300 hover:underline" onClick={refresh}>Refresh</button></div>
      <Show when={props.canRelocate}>
        <p>Choose the parent folder containing this torrent. Moving copies and syncs the data before removing the original. Locate checks files already in the chosen folder.</p>
        <input aria-label="New data folder" value={path()} onInput={(e) => setPath(e.currentTarget.value)} placeholder="/path/to/downloads" disabled={busy()} class="w-full rounded border border-white/[.06] bg-black/20 px-2 py-1.5 text-zinc-200 focus:outline-none focus:ring-1 focus:ring-accent-500" />
        <div class="flex flex-wrap gap-2"><Button disabled={busy() || !path().trim() || !props.detail.paused || props.detail.verifying} onClick={() => relocate(true)}>{busy() ? 'Working…' : 'Move data'}</Button><Button disabled={busy() || !path().trim() || !props.detail.paused || props.detail.verifying} onClick={() => relocate(false)}>Locate existing</Button></div>
        <Show when={!props.detail.paused || props.detail.verifying}><p>Pause this torrent and wait for verification to finish before changing location.</p></Show>
      </Show>
      <Show when={props.canChangeSettings && storage()}><label class="flex items-start gap-2"><input type="checkbox" class="mt-0.5 accent-accent-500" checked={storage()!.preallocate} disabled={busy()} onChange={async (e) => {const enabled = e.currentTarget.checked;setBusy(true);try {await api.setPreallocate(enabled);setStorage((s) => s && {...s, preallocate: enabled});toast.success('Preallocation saved · restart Mosaic to apply');}catch(e){toast.error(userErr(e));await refresh();}finally{setBusy(false);}}} /><span>Preallocate full files<span class="mt-1 block text-[10px]">Global setting · reserves full file sizes instead of using .part files. Applies after restart.</span></span></label></Show>
      <Show when={error()}><p role="alert" class="break-words text-rose-300">{error()}</p></Show>
    </div>
  </details>;
}
