import {createMemo, For, Show} from 'solid-js';
import {createSolidTable, getCoreRowModel, getSortedRowModel, flexRender, type ColumnDef, type SortingState} from '@tanstack/solid-table';
import {createSignal} from 'solid-js';
import {ChevronDown, ChevronUp} from 'lucide-solid';
import type {Torrent} from '../../lib/bindings';
import {fmtBytes, fmtETA, fmtPercent, fmtRate} from '../../lib/format';

type Props = {
  torrents: Torrent[];
  selection: Set<string>;
  onRowClick: (id: string, e: MouseEvent) => void;
};

export function TorrentTable(props: Props) {
  const [sorting, setSorting] = createSignal<SortingState>([{id: 'added_at', desc: true}]);

  const columns = createMemo<ColumnDef<Torrent>[]>(() => [
    {
      accessorKey: 'name',
      header: 'Name',
      cell: (info) => (
        <div class="flex items-center gap-2 min-w-0">
          <span class={`h-1.5 w-1.5 shrink-0 rounded-full ${info.row.original.paused ? 'bg-paused' : info.row.original.completed ? 'bg-seed' : 'bg-down animate-pulse'}`} />
          <span class="truncate text-zinc-100">{info.getValue() as string}</span>
        </div>
      ),
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
    {accessorKey: 'added_at', header: 'Added', cell: (info) => <span class="text-zinc-500">{new Date((info.getValue() as number) * 1000).toLocaleDateString()}</span>, size: 110},
  ]);

  const table = createSolidTable<Torrent>({
    get data() { return props.torrents; },
    get columns() { return columns(); },
    state: { get sorting() { return sorting(); } },
    onSortingChange: setSorting,
    getCoreRowModel: getCoreRowModel(),
    getSortedRowModel: getSortedRowModel(),
  });

  return (
    <div class="overflow-auto">
      <table class="w-full text-sm">
        <thead class="sticky top-0 z-10 bg-zinc-950/80 backdrop-blur-md text-xs font-medium uppercase tracking-wider text-zinc-500">
          <For each={table.getHeaderGroups()}>
            {(group) => (
              <tr>
                <For each={group.headers}>
                  {(header) => (
                    <th
                      class="cursor-pointer select-none px-3 py-2 text-left font-medium hover:text-zinc-300"
                      onClick={header.column.getToggleSortingHandler()}
                      style={{width: `${header.getSize()}px`}}
                    >
                      <div class="inline-flex items-center gap-1">
                        {flexRender(header.column.columnDef.header, header.getContext())}
                        <Show when={header.column.getIsSorted() === 'asc'}><ChevronUp class="h-3 w-3" /></Show>
                        <Show when={header.column.getIsSorted() === 'desc'}><ChevronDown class="h-3 w-3" /></Show>
                      </div>
                    </th>
                  )}
                </For>
              </tr>
            )}
          </For>
        </thead>
        <tbody>
          <For each={table.getRowModel().rows}>
            {(row) => (
              <tr
                class="cursor-pointer border-t border-white/[.04] hover:bg-white/[.02]"
                classList={{'!bg-accent-500/[.06]': props.selection.has(row.original.id)}}
                onClick={(e) => props.onRowClick(row.original.id, e)}
              >
                <For each={row.getVisibleCells()}>
                  {(cell) => (
                    <td class="px-3 py-2 truncate" style={{'max-width': `${cell.column.getSize()}px`}}>
                      {flexRender(cell.column.columnDef.cell, cell.getContext())}
                    </td>
                  )}
                </For>
              </tr>
            )}
          </For>
        </tbody>
      </table>
    </div>
  );
}
