import {createSignal, For, Show} from 'solid-js';
import {Pause, Play, Trash2, X, ChevronDown} from 'lucide-solid';
import type {CategoryDTO, TagDTO, Torrent} from '../../lib/bindings';
import {api} from '../../lib/bindings';
import {DropdownMenu} from '../ui/DropdownMenu';
import {Button} from '../ui/Button';
import {toast} from 'solid-sonner';
import {userErr} from '../../lib/errors';

export function BulkActions(props: {torrents: Torrent[]; categories: CategoryDTO[]; tags: TagDTO[]; onRemove: () => void; onClear: () => void}) {
  const [busy, setBusy] = createSignal(false);
  const [limitsOpen, setLimitsOpen] = createSignal(false);
  const [down, setDown] = createSignal('');
  const [up, setUp] = createSignal('');
  const editable = () => props.torrents.filter((t) => t.access === 'owner' || t.access === 'editor');
  const run = async (label: string, action: (id: string) => Promise<unknown>) => {
    if (busy()) return;
    const selected = editable();if (!selected.length) return;
    const skipped = props.torrents.length - selected.length;
    setBusy(true);
    const results = await Promise.allSettled(selected.map((t) => action(t.id)));
    setBusy(false);
    const failed = results.flatMap((r, i) => r.status === 'rejected' ? [`${selected[i].name}: ${userErr(r.reason)}`] : []);
    const message = `${label}: ${results.length - failed.length} updated${skipped ? ` · ${skipped} read-only skipped` : ''}`;
    if (failed.length) toast.error(`${message} · ${failed.length} failed: ${failed.slice(0, 3).join('; ')}`);else toast.success(message);
  };
  const button = 'inline-flex items-center gap-1 rounded px-1.5 py-1.5 text-xs text-zinc-300 hover:bg-white/[.05] disabled:opacity-30';
  return <Show when={props.torrents.length}>
    <div role="toolbar" aria-label="Selected torrent actions" class="shrink-0 border-b border-white/[.05] bg-accent-500/[.03] px-3 py-1.5">
      <div class="flex flex-wrap items-center gap-1">
        <button type="button" class={button} disabled={busy() || !editable().length} onClick={() => run('Paused', api.pause)}><Pause class="h-3 w-3" />Pause</button>
        <button type="button" class={button} disabled={busy() || !editable().length} onClick={() => run('Resumed', api.resume)}><Play class="h-3 w-3" />Resume</button>
        <Show when={editable().length}>
          <DropdownMenu label="Set category for selection" triggerClass={button} trigger={<>Category<ChevronDown class="h-3 w-3" /></>}>
            <DropdownMenu.Item disabled={busy()} onSelect={() => run('Category changed', (id) => api.setTorrentCategory(id, null))}>None</DropdownMenu.Item>
            <For each={props.categories}>{(cat) => <DropdownMenu.Item disabled={busy()} onSelect={() => run('Category changed', (id) => api.setTorrentCategory(id, cat.id))}>{cat.name}</DropdownMenu.Item>}</For>
          </DropdownMenu>
          <Show when={props.tags.length}><DropdownMenu label="Change tags for selection" triggerClass={button} trigger={<>Tags<ChevronDown class="h-3 w-3" /></>}><For each={props.tags}>{(tag) => <><DropdownMenu.Item disabled={busy()} onSelect={() => run('Tag added', (id) => api.assignTag(id, tag.id))}>Add {tag.name}</DropdownMenu.Item><DropdownMenu.Item disabled={busy()} onSelect={() => run('Tag removed', (id) => api.unassignTag(id, tag.id))}>Remove {tag.name}</DropdownMenu.Item></>}</For></DropdownMenu></Show>
          <button type="button" aria-expanded={limitsOpen()} class={button} disabled={busy()} onClick={() => setLimitsOpen((v) => !v)}>Limits<ChevronDown class="h-3 w-3" /></button>
        </Show>
        <button type="button" disabled={busy()} class={`${button} text-rose-300`} onClick={props.onRemove}><Trash2 class="h-3 w-3" />Remove</button>
        <button type="button" disabled={busy()} aria-label="Clear selection" class={`${button} ml-auto`} onClick={props.onClear}><X class="h-3.5 w-3.5" /></button>
      </div>
      <Show when={limitsOpen()}><form class="mt-2 flex flex-wrap items-center gap-2 pb-1 text-xs text-zinc-400" onSubmit={async (e) => {e.preventDefault();const d = Number(down()), u = Number(up());if (!Number.isSafeInteger(d) || !Number.isSafeInteger(u) || d < 0 || u < 0) {toast.error('Enter whole, non-negative KB/s limits');return;}await run('Limits saved', (id) => api.setTorrentRateLimits(id, d, u));}}><label>↓ <input aria-label="Selection download limit" type="number" min="0" step="1" value={down()} onInput={(e) => setDown(e.currentTarget.value)} placeholder="Unlimited" class="w-24 rounded border border-white/[.06] bg-black/20 p-1 text-zinc-200" /></label><label>↑ <input aria-label="Selection upload limit" type="number" min="0" step="1" value={up()} onInput={(e) => setUp(e.currentTarget.value)} placeholder="Unlimited" class="w-24 rounded border border-white/[.06] bg-black/20 p-1 text-zinc-200" /></label><span>KB/s each · 0 = unlimited</span><Button type="submit" disabled={busy()} class="text-xs">Apply to selection</Button></form></Show>
    </div>
  </Show>;
}
