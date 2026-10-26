import {createEffect, createMemo, For, Show} from 'solid-js';
import {createSolidTable, getCoreRowModel, getSortedRowModel, flexRender, type ColumnDef, type SortingState, type VisibilityState} from '@tanstack/solid-table';
import {createSignal} from 'solid-js';
import {createVirtualizer} from '@tanstack/solid-virtual';
import {ChevronDown, ChevronUp, Star, Columns3} from 'lucide-solid';
import type {Torrent} from '../../lib/bindings';
import {toast} from 'solid-sonner';
import {loadTablePreferences, TABLE_PREFERENCES_KEY} from '../../lib/table_preferences';
import {fmtBytes, fmtETA, fmtPercent, fmtRate} from '../../lib/format';

type Props = {
  torrents: Torrent[];
  selection: Set<string>;
  // visibleIds is the id list in the on-screen (sorted) order, so shift-click
  // range select can extend over exactly the rows the user sees.
  onKeyboardSelect?: (id:string,extend:boolean,ids:string[])=>void;
  onInspect?: (id:string)=>void;
  onRowClick: (id: string, e: MouseEvent, visibleIds: string[]) => void;
};

// Fixed table row height (px). Rows are single-line with truncation so the
// height is constant — a fixed estimate keeps the virtualizer exact.
const ROW_HEIGHT = 37;

export function TorrentTable(props: Props) {
  const preferences = loadTablePreferences();
  const [sorting, setSorting] = createSignal<SortingState>(preferences.sorting);
  const [visibility, setVisibility] = createSignal<VisibilityState>(preferences.visibility);
  let storageWarningShown = false;
  createEffect(() => {
    try {localStorage.setItem(TABLE_PREFERENCES_KEY, JSON.stringify({sorting: sorting(), visibility: visibility()}));}
    catch {if (!storageWarningShown) {storageWarningShown = true;toast.error("Couldn't save table preferences in this browser");}}
  });

  const columns = createMemo<ColumnDef<Torrent>[]>(() => [
    {
      accessorKey: 'name',
      enableHiding: false,
      header: 'Name',
      cell: (info) => {
        const t = info.row.original;
        return (
          <div class="flex items-center gap-2 min-w-0">
            <span class={`h-1.5 w-1.5 shrink-0 rounded-full ${t.paused ? 'bg-paused' : t.completed ? 'bg-seed' : 'bg-down animate-pulse'}`} />
            <span class="truncate text-zinc-100">{info.getValue() as string}</span>
            <Show when={t.queued && !t.completed}>
              <span class="inline-flex shrink-0 items-center gap-1 rounded bg-zinc-800/60 px-1.5 py-0.5 font-mono text-[10px] tabular-nums text-zinc-400">
                Q{t.queue_position + 1}
              </span>
            </Show>
            <Show when={t.force_start}>
              <Star class="h-3 w-3 shrink-0 text-amber-400" fill="currentColor" />
            </Show>
          </div>
        );
      },
      size: 360,
    },
    {accessorKey: 'total_bytes', header: 'Size', cell: (info) => <span class="font-mono tabular-nums text-zinc-400">{fmtBytes(info.getValue() as number)}</span>, size: 90},
    {accessorKey: 'progress', header: 'Progress', cell: (info) => <span class="font-mono tabular-nums text-zinc-300">{fmtPercent(info.getValue() as number)}</span>, size: 80},
    {accessorKey: 'download_rate', header: '↓', cell: (info) => <span class="font-mono tabular-nums text-zinc-400">{fmtRate(info.getValue() as number)}</span>, size: 100},
    {accessorKey: 'upload_rate', header: '↑', cell: (info) => <span class="font-mono tabular-nums text-zinc-400">{fmtRate(info.getValue() as number)}</span>, size: 100},
    {
      id: 'eta',
      header: 'ETA',
      accessorFn: (t) => t.completed || t.download_rate === 0 ? Number.MAX_SAFE_INTEGER : (t.total_bytes - t.bytes_done) / t.download_rate,
      cell: (info) => {
        const t = info.row.original;
        if (t.completed) return <span class="text-zinc-600">—</span>;
        return <span class="font-mono tabular-nums text-zinc-400">{fmtETA(t.total_bytes - t.bytes_done, t.download_rate)}</span>;
      },
      size: 80,
    },
    {accessorKey: 'peers', header: 'Peers', cell: (info) => <span class="font-mono tabular-nums text-zinc-400">{info.getValue() as number}</span>, size: 70},
    {id: 'ratio', accessorFn: (t) => t.ratio ?? 0, header: 'Ratio', cell: (info) => <span class="font-mono tabular-nums text-zinc-400">{(info.getValue() as number).toFixed(2)}</span>, size: 80},
    {id: 'total_up', accessorFn: (t) => t.total_up ?? 0, header: 'Uploaded', cell: (info) => <span class="font-mono tabular-nums text-zinc-400">{fmtBytes(info.getValue() as number)}</span>, size: 100},
    {accessorKey: 'seeds', header: 'Seeds', cell: (info) => <span class="font-mono tabular-nums text-zinc-400">{info.getValue() as number}</span>, size: 70},
    {accessorKey: 'queue_position', header: 'Queue', cell: (info) => <span class="font-mono tabular-nums text-zinc-400">Q{(info.getValue() as number) + 1}</span>, size: 75},
    {accessorKey: 'added_at', header: 'Added', cell: (info) => <span class="text-zinc-500">{new Date((info.getValue() as number) * 1000).toLocaleDateString()}</span>, size: 110},
  ]);

  const [focusedID,setFocusedID]=createSignal('');
  const table = createSolidTable<Torrent>({
    get data() { return props.torrents; },
    get columns() { return columns(); },
    state: { get sorting() { return sorting(); }, get columnVisibility() { return visibility(); } },
    onColumnVisibilityChange: setVisibility,
    onSortingChange: setSorting,
    getCoreRowModel: getCoreRowModel(),
    getSortedRowModel: getSortedRowModel(),
  });

  const navigate=(event:KeyboardEvent)=>{
    if(event.target!==event.currentTarget)return;
    const rows=table.getRowModel().rows,ids=rows.map(r=>r.original.id);if(!ids.length)return;
    const current=ids.indexOf(focusedID()||[...props.selection].at(-1)||'');
    if(event.key==='Enter'){
      event.preventDefault();event.stopPropagation();const id=ids[current<0?0:current];props.onInspect?.(id);return;
    }
    if(!['ArrowDown','ArrowUp','Home','End'].includes(event.key))return;
    event.preventDefault();event.stopPropagation();
    const next=event.key==='Home'?0:event.key==='End'?ids.length-1:current<0?(event.key==='ArrowUp'?ids.length-1:0):Math.max(0,Math.min(ids.length-1,current+(event.key==='ArrowDown'?1:-1)));
    setFocusedID(ids[next]);props.onKeyboardSelect?.(ids[next],event.shiftKey,ids);rowVirtualizer.scrollToIndex(next,{align:'auto'});
  };

  // The scroll container the virtualizer measures against. Set via ref below.
  let scrollEl: HTMLDivElement | undefined;

  // Virtualize the sorted rows: only the visible window renders into the
  // tbody. Two spacer <tr>s (top/bottom) reserve the off-screen height so
  // the scrollbar and the sticky header stay correct.
  const rowVirtualizer = createVirtualizer({
    get count() { return table.getRowModel().rows.length; },
    getScrollElement: () => scrollEl ?? null,
    estimateSize: () => ROW_HEIGHT,
    overscan: 8,
  });

  // Height of the rows scrolled off below the viewport — reserved by the
  // bottom spacer <tr> so the scrollbar matches the full list.
  const bottomPad = createMemo(() => {
    const items = rowVirtualizer.getVirtualItems();
    if (items.length === 0) return 0;
    const last = items[items.length - 1];
    return rowVirtualizer.getTotalSize() - (last.start + last.size);
  });

  return (
    // h-full constrains this element to the pane so IT is the scrollport
    // (mirroring CardList). Without the height cap the div grows to content
    // height, the ancestor scrolls instead, the virtualizer sees a viewport
    // equal to the full list and renders every row, and the sticky header
    // sticks to the wrong scroll container.
    <div class="flex h-full min-h-0 flex-col">
      <div class="flex shrink-0 justify-end border-b border-white/[.04] px-3 py-1">
        <details class="relative">
          <summary class="inline-flex cursor-pointer items-center gap-1.5 rounded px-2 py-1 text-xs text-zinc-500 hover:bg-white/[.04] hover:text-zinc-200"><Columns3 class="h-3 w-3" />Columns</summary>
          <div class="absolute right-0 top-full z-30 grid max-h-80 min-w-48 gap-2 overflow-auto rounded-lg border border-white/10 bg-zinc-900 p-3 shadow-xl" role="group" aria-label="Table columns">
            <For each={table.getAllLeafColumns()}>{(column) => <label class="flex items-center gap-2 text-xs text-zinc-300"><input type="checkbox" aria-label={`Show ${column.columnDef.header === '↓' ? 'download speed' : column.columnDef.header === '↑' ? 'upload speed' : column.columnDef.header} column`} class="accent-accent-500" disabled={!column.getCanHide()} checked={column.getIsVisible()} onChange={column.getToggleVisibilityHandler()} />{column.columnDef.header === '↓' ? 'Download speed' : column.columnDef.header === '↑' ? 'Upload speed' : column.columnDef.header as string}</label>}</For>
            <button type="button" class="mt-1 border-t border-white/[.05] pt-2 text-left text-xs text-accent-300" onClick={() => {setSorting([{id: 'added_at', desc: true}]);setVisibility({ratio: false, total_up: false, seeds: false, queue_position: false});}}>Reset table preferences</button>
          </div>
        </details>
      </div>
      <div class="min-h-0 flex-1 overflow-auto" ref={scrollEl}>
      <table role="grid" aria-label="Torrents" aria-rowcount={table.getRowModel().rows.length+1} aria-activedescendant={rowVirtualizer.getVirtualItems().some(v=>table.getRowModel().rows[v.index]?.original.id===focusedID())?`torrent-row-${focusedID()}`:undefined} tabIndex={0} onKeyDown={navigate} class="w-full text-sm focus:outline-none focus-visible:ring-1 focus-visible:ring-inset focus-visible:ring-accent-500/50">
        <thead class="sticky top-0 z-10 bg-zinc-950/80 backdrop-blur-md text-xs font-medium uppercase tracking-wider text-zinc-500">
          <For each={table.getHeaderGroups()}>
            {(group) => (
              <tr>
                <For each={group.headers}>
                  {(header) => (
                    <th
                      class="cursor-pointer select-none px-3 py-2 text-left font-medium hover:text-zinc-300"
                      aria-sort={header.column.getIsSorted() === 'asc' ? 'ascending' : header.column.getIsSorted() === 'desc' ? 'descending' : 'none'}
                      style={{width: `${header.getSize()}px`}}
                    >
                      <button type="button" class="inline-flex items-center gap-1" onClick={header.column.getToggleSortingHandler()}>
                        {flexRender(header.column.columnDef.header, header.getContext())}
                        <Show when={header.column.getIsSorted() === 'asc'}><ChevronUp class="h-3 w-3" /></Show>
                        <Show when={header.column.getIsSorted() === 'desc'}><ChevronDown class="h-3 w-3" /></Show>
                      </button>
                    </th>
                  )}
                </For>
              </tr>
            )}
          </For>
        </thead>
        <tbody>
          {/* Top spacer: reserves the height of rows scrolled off above. */}
          <Show when={(rowVirtualizer.getVirtualItems()[0]?.start ?? 0) > 0}>
            <tr aria-hidden="true">
              <td style={{height: `${rowVirtualizer.getVirtualItems()[0]!.start}px`, padding: '0'}} />
            </tr>
          </Show>
          <For each={rowVirtualizer.getVirtualItems()}>
            {(vItem) => {
              const row = () => table.getRowModel().rows[vItem.index];
              return (
                <Show when={row()}>
                  <tr id={`torrent-row-${row().original.id}`} aria-selected={props.selection.has(row().original.id)} aria-rowindex={vItem.index+2}
                    class="cursor-pointer border-t border-white/[.04] hover:bg-white/[.02]"
                    classList={{'!bg-accent-500/[.06]': props.selection.has(row().original.id)}}
                    onClick={(e) => {setFocusedID(row().original.id);props.onRowClick(row().original.id, e, table.getRowModel().rows.map((r) => r.original.id));}}
                  >
                    <For each={row().getVisibleCells()}>
                      {(cell) => (
                        <td class="px-3 py-2 truncate" style={{'max-width': `${cell.column.getSize()}px`}}>
                          {flexRender(cell.column.columnDef.cell, cell.getContext())}
                        </td>
                      )}
                    </For>
                  </tr>
                </Show>
              );
            }}
          </For>
          {/* Bottom spacer: reserves the height of rows scrolled off below. */}
          <Show when={bottomPad() > 0}>
            <tr aria-hidden="true"><td style={{height: `${bottomPad()}px`, padding: '0'}} /></tr>
          </Show>
        </tbody>
      </table>
      </div>
    </div>
  );
}
