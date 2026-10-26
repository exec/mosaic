import {createSignal, Show} from 'solid-js';
import type {DetailDTO, InspectorTab} from '../../lib/bindings';
import {api} from '../../lib/bindings';
import {fmtTimestamp} from '../../lib/format';
import {toast} from 'solid-sonner';
import {userErr} from '../../lib/errors';

export function downloadHealth(d: DetailDTO, rate: number) {
  if (d.files_missing) return {title: 'Files are missing', reason: 'Previously complete data could not be found. Locate the files below, or resume to download the missing pieces.', action: 'resume'};
  if (d.error_active) return {title: 'Transfer needs attention', reason: d.last_error || 'The engine stopped this transfer. Check storage and retry.', action: 'resume'};
  if (!d.metadata_ready && !d.total_bytes) return {title: 'Finding torrent metadata', reason: 'Waiting for a peer with the file list. Check the tracker and peer tabs if this persists.', action: 'trackers'};
  if (d.verifying) return {title: 'Checking existing files', reason: 'Mosaic is verifying pieces on disk before transferring data. This may take time on large downloads.', action: ''};
  if (d.paused) return {title: 'Paused', reason: 'Transfers are stopped. Resume when you are ready.', action: 'resume'};
  if (d.queued) return {title: 'Waiting for an active slot', reason: 'The queue is holding this torrent until another transfer frees a slot. Queue limits are in Settings.', action: ''};
  if (d.completed) return {title: 'Ready to seed', reason: d.peers ? 'Connected peers can request the completed files.' : 'All data is complete. Waiting for peers that need it.', action: 'peers'};
  if (d.selection_complete) return {title: 'Selected files complete', reason: 'All selected files have finished downloading. Choose more files to continue, or keep seeding the available pieces.', action: 'files'};
  if (d.selection_empty || d.files?.length && d.files.every((f) => f.priority === 'skip')) return {title: 'No files selected', reason: 'Choose at least one file to download.', action: 'files'};
  if (!d.peers) return {title: 'Looking for peers', reason: 'No peers are connected yet. Check tracker addresses and network connectivity.', action: 'trackers'};
  if (!rate) return {title: 'Connected, waiting for data', reason: 'Peers are connected but no download was measured in the latest sample. They may not have the pieces you need.', action: 'peers'};
  return {title: 'Downloading', reason: 'Receiving data from connected peers.', action: 'peers'};
}
export function HealthPanel(props: {detail: DetailDTO; rate: number; canEdit: boolean; onTabChange: (tab: InspectorTab) => void}) {
  const health = () => downloadHealth(props.detail, props.rate);
  const [busy, setBusy] = createSignal(false);
  const act = async () => {
    const action = health().action;
    if (action !== 'resume') {props.onTabChange(action as InspectorTab);return;}
    if (!props.canEdit || busy()) return;
    setBusy(true);
    try {await api.resume(props.detail.id);toast.success('Transfer resumed');}catch(e){toast.error(userErr(e));}finally{setBusy(false);}
  };
  return <div class="my-2 rounded-lg border border-white/[.06] bg-white/[.02] p-3" role="status">
    <div class="flex items-center gap-2 text-xs font-medium text-zinc-200"><span class="h-1.5 w-1.5 shrink-0 rounded-full" classList={{'bg-amber-400': !!props.detail.error_active || props.detail.files_missing, 'bg-accent-400': !props.detail.error_active && !props.detail.files_missing}} />{health().title}</div>
    <p class="mt-1.5 text-xs leading-relaxed text-zinc-500">{health().reason}</p>
    <Show when={health().action && (health().action !== 'resume' || props.canEdit)}><button type="button" disabled={busy()} class="mt-2 text-xs text-accent-300 hover:underline disabled:opacity-50" onClick={act}>{health().action === 'resume' ? 'Resume / retry' : `View ${health().action}`}</button></Show>
    <Show when={props.detail.last_error}><details class="mt-2 border-t border-white/[.05] pt-2 text-xs text-zinc-500"><summary class="cursor-pointer">Last error · {fmtTimestamp(props.detail.error_at ?? 0)}</summary><p class="mt-2 break-words text-amber-200/80">{props.detail.last_error}</p><p class="mt-1 text-[10px]">Error history is kept for this app session.</p><button type="button" class="mt-2 text-accent-300 hover:underline" onClick={async () => {try{await navigator.clipboard.writeText(`${props.detail.last_error}\n${fmtTimestamp(props.detail.error_at ?? 0)}`);toast.success('Error copied');}catch(e){toast.error(userErr(e));}}}>Copy error</button></details></Show>
  </div>;
}
