import {Dialog} from '@kobalte/core/dialog';
import {createEffect, createMemo, createSignal, For, on, Show} from 'solid-js';
import type {Torrent} from '../../lib/bindings';
import {Button} from '../ui/Button';

export function RemoveTorrentModal(props: {torrents: Torrent[]; onClose: () => void; onRemove: (deleteFiles: boolean) => Promise<void>}) {
  const [deleteFiles, setDeleteFiles] = createSignal(false);
  const [busy, setBusy] = createSignal(false);
  const identity = createMemo(() => props.torrents.map((t) => `${t.id}:${t.access}`).join());
  createEffect(on(identity, () => setDeleteFiles(false)));
  return <Dialog open={props.torrents.length > 0} onOpenChange={(open) => {if (!open && !busy()) props.onClose();}}>
    <Dialog.Portal><Dialog.Overlay class="fixed inset-0 z-40 bg-black/60 backdrop-blur-sm" /><div class="fixed inset-0 z-50 grid place-items-center p-4"><Dialog.Content class="w-full max-w-md rounded-xl border border-white/10 bg-zinc-900 p-5 shadow-2xl">
      <Dialog.Title class="text-base font-semibold text-zinc-100">Remove {props.torrents.length === 1 ? 'torrent' : `${props.torrents.length} torrents`}?</Dialog.Title>
      <Dialog.Description class="mt-2 text-xs leading-relaxed text-zinc-400">Downloaded files are kept by default. For torrents shared with other owners, removal may only remove your access.</Dialog.Description>
      <ul class="my-3 max-h-36 space-y-1 overflow-y-auto text-xs text-zinc-300"><For each={props.torrents}>{(t) => <li class="truncate" title={t.name}>{t.name}</li>}</For></ul>
      <Show when={props.torrents.length && props.torrents.every((t) => t.access === 'owner')}><label class="flex items-start gap-2 rounded-md border border-white/[.06] p-3 text-xs text-zinc-300"><input type="checkbox" aria-label="Delete downloaded files" class="mt-0.5 accent-rose-500" checked={deleteFiles()} disabled={busy()} onChange={(e) => setDeleteFiles(e.currentTarget.checked)} /><span>Also delete downloaded files<span class="mt-1 block text-zinc-500">This permanently removes the torrent's data from disk.</span></span></label></Show>
      <div class="mt-4 flex justify-end gap-2"><Button disabled={busy()} variant="ghost" onClick={props.onClose}>Cancel</Button><Button disabled={busy()} variant="danger" onClick={async () => {if (busy()) return;setBusy(true);try {await props.onRemove(deleteFiles());}finally{setBusy(false);}}}>{busy() ? 'Removing…' : deleteFiles() ? 'Remove & delete files' : 'Remove · keep files'}</Button></div>
    </Dialog.Content></div></Dialog.Portal>
  </Dialog>;
}
