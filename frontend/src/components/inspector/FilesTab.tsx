import {createEffect, createMemo, createSignal, For, Show} from 'solid-js';
import {ChevronDown, Folder, FolderOpen, ExternalLink, Copy} from 'lucide-solid';
import type {DetailDTO, FileDTO} from '../../lib/bindings';
import {api} from '../../lib/bindings';
import {fmtBytes, fmtPercent} from '../../lib/format';
import {DropdownMenu} from '../ui/DropdownMenu';
import {isWailsRuntime} from '../../lib/runtime';
import {toast} from 'solid-sonner';
import {userErr} from '../../lib/errors';

type Priority = 'skip' | 'normal' | 'high' | 'max';
type Props = {
  detail: DetailDTO | null;
  canEdit: boolean;
  onSetPriority: (index: number, priority: Priority) => void;
  onSetPriorities?: (priorities: Record<number, Priority>) => void;
  preview?: boolean;
};
type Node = {name: string; path: string; indices: number[]; children: Node[]; index?: number};
export function fileTree(files: Pick<FileDTO, 'index' | 'path'>[]): Node {
  const root: Node = {name: '', path: '', indices: [], children: []};
  for (const f of files) {
    let parent = root;
    const parts = f.path.split('/');
    parts.forEach((name, i) => {
      parent.indices.push(f.index);
      const path = parts.slice(0, i + 1).join('/');
      let node = parent.children.find((n) => n.path === path);
      if (!node) { node = {name, path, indices: [], children: [], ...(i === parts.length - 1 ? {index: f.index} : {})}; parent.children.push(node); }
      parent = node;
    });
    parent.indices.push(f.index);
  }
  return root;
}

export function FilesTab(props: Props) {
  const [query, setQuery] = createSignal('');
  // Stable paths keep folders and menus open when progress ticks replace file objects.
  const structure = createMemo(() => JSON.stringify((props.detail?.files ?? []).map(({index, path}) => ({index, path}))));
  const tree = createMemo(() => fileTree(JSON.parse(structure())));
  const files = createMemo(() => new Map((props.detail?.files ?? []).map((f) => [f.index, f])));
  const selected = createMemo(() => (props.detail?.files ?? []).filter((f) => f.priority !== 'skip'));
  const matches = (node: Node) => node.indices.some((i) => files().get(i)?.path.toLowerCase().includes(query().trim().toLowerCase()));
  const set = (indices: number[], priority: Priority) => {
    if (!props.canEdit) return;
    if (props.onSetPriorities) props.onSetPriorities(Object.fromEntries(indices.map((i) => [i, priority])));
    else indices.forEach((i) => props.onSetPriority(i, priority));
  };
  const PriorityMenu = (p: {node: Node}) => {
    const priority = () => {
      const values = new Set(p.node.indices.map((i) => files().get(i)?.priority));
      return values.size === 1 ? [...values][0] : 'Mixed';
    };
    return <Show when={props.canEdit} fallback={<span class="text-zinc-500">{priority()}</span>}>
      <DropdownMenu label={`Priority for ${p.node.path}`} triggerClass="inline-flex shrink-0 items-center gap-0.5 rounded px-1.5 py-0.5 text-xs text-zinc-400 hover:bg-white/[.04] hover:text-zinc-100" trigger={<>{priority()}<ChevronDown class="h-3 w-3" /></>}>
        <For each={['skip', 'normal', 'high', 'max'] as Priority[]}>{(value) => <DropdownMenu.Item onSelect={() => set(p.node.indices, value)}>{value[0].toUpperCase() + value.slice(1)}</DropdownMenu.Item>}</For>
      </DropdownMenu>
    </Show>;
  };
  const fileAction = async (index: number, reveal: boolean) => {
    try { await api.openTorrentFile(props.detail!.id, index, reveal); }
    catch (err) { toast.error(`Couldn't open file — ${userErr(err)}`); }
  };
  const Branch = (p: {node: Node}) => <Show when={matches(p.node)}>
    <Show when={p.node.index === undefined} fallback={(() => {
      const f = () => files().get(p.node.index!)!;
      return <div class="min-w-0 border-b border-white/[.03] px-3 py-2 text-xs">
        <div class="flex min-w-0 items-center gap-2">
          <Show when={props.canEdit}><input type="checkbox" aria-label={`Download ${p.node.path}`} checked={f()?.priority !== 'skip'} onChange={(e) => set(p.node.indices, e.currentTarget.checked ? 'normal' : 'skip')} class="shrink-0 accent-accent-500" /></Show>
          <span class="min-w-0 flex-1 truncate text-zinc-200" title={p.node.path}>{p.node.name}</span>
          <span class="shrink-0 font-mono text-zinc-500">{fmtBytes(f()?.size ?? 0)}</span>
        </div>
        <div class="mt-1.5 flex min-w-0 items-center gap-1.5">
          <div class="h-1 min-w-3 flex-1 overflow-hidden rounded bg-white/[.04]"><div class="h-full bg-accent-500" style={{width: `${Math.min(100, (f()?.progress ?? 0) * 100)}%`}} /></div>
          <span class="font-mono text-zinc-500">{fmtPercent(f()?.progress ?? 0)}</span>
          <PriorityMenu node={p.node} />
          <Show when={!props.preview}>
            <button type="button" aria-label={`Copy path for ${p.node.path}`} title="Copy relative path" class="p-1 text-zinc-500 hover:text-zinc-200" onClick={async () => {try {await navigator.clipboard.writeText(p.node.path);toast.success('Path copied');}catch(e){toast.error(userErr(e));}}}><Copy class="h-3 w-3" /></button>
            <Show when={isWailsRuntime()}>
              <button type="button" disabled={f()?.bytes_done < f()?.size} aria-label={`Open ${p.node.path}`} title="Open completed file" class="p-1 text-zinc-500 hover:text-zinc-200 disabled:opacity-30" onClick={() => fileAction(p.node.index!, false)}><ExternalLink class="h-3 w-3" /></button>
              <button type="button" disabled={f()?.bytes_done < f()?.size} aria-label={`Reveal ${p.node.path}`} title="Reveal completed file" class="p-1 text-zinc-500 hover:text-zinc-200 disabled:opacity-30" onClick={() => fileAction(p.node.index!, true)}><FolderOpen class="h-3 w-3" /></button>
            </Show>
          </Show>
        </div>
      </div>;
    })()}>
      <details open ref={(el) => createEffect(() => {if (query()) el.open = true;})} class="group min-w-0">
        <summary class="flex min-w-0 cursor-pointer items-center gap-1.5 border-b border-white/[.04] px-2 py-2 text-xs text-zinc-400 marker:text-zinc-600"><ChevronDown class="h-3 w-3 shrink-0 -rotate-90 group-open:rotate-0" /><Folder class="h-3.5 w-3.5 shrink-0" /><span class="min-w-0 flex-1 truncate" title={p.node.path}>{p.node.name}</span><span class="text-zinc-600">{p.node.indices.length}</span></summary>
        <div class="min-w-0 border-l border-white/[.04] pl-2"><div class="flex justify-end px-2"><PriorityMenu node={p.node} /></div><For each={p.node.children}>{(node) => <Branch node={node} />}</For></div>
      </details>
    </Show>
  </Show>;
  return <Show when={props.detail?.files?.length} fallback={<div class="p-4 text-xs text-zinc-500">No files yet — waiting for metadata.</div>}>
    <div class="p-3 pb-2">
      <input aria-label="Search files" type="search" value={query()} onInput={(e) => setQuery(e.currentTarget.value)} placeholder="Find files…" class="w-full rounded-md border border-white/[.06] bg-black/20 px-2.5 py-1.5 text-xs text-zinc-200 focus:outline-none focus:ring-1 focus:ring-accent-500" />
      <div class="mt-2 flex flex-wrap items-center gap-2 text-[11px] text-zinc-500"><span class="mr-auto">{selected().length} selected · {fmtBytes(selected().reduce((sum, f) => sum + f.size, 0))}</span><Show when={props.canEdit}><button type="button" class="text-accent-300 hover:underline" onClick={() => set(tree().indices, 'normal')}>Select all</button><button type="button" class="text-accent-300 hover:underline" onClick={() => set(tree().indices, 'skip')}>None</button></Show></div>
    </div>
    <div class="min-w-0 px-1"><For each={tree().children}>{(node) => <Branch node={node} />}</For><Show when={query() && !tree().children.some(matches)}><p class="p-3 text-xs text-zinc-500">No matching files.</p></Show></div>
  </Show>;
}
