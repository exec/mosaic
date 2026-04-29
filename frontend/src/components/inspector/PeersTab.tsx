import {For, Show} from 'solid-js';
import type {DetailDTO} from '../../lib/bindings';
import {fmtPercent, fmtRate} from '../../lib/format';

type Props = {detail: DetailDTO | null};

export function PeersTab(props: Props) {
  return (
    <Show
      when={props.detail?.peers_list?.length}
      fallback={<div class="p-4 text-xs text-zinc-500">No connected peers.</div>}
    >
      <table class="w-full text-xs">
        <thead class="sticky top-0 bg-zinc-950/80 backdrop-blur-md text-[10px] uppercase tracking-wider text-zinc-500">
          <tr class="border-b border-white/[.04]">
            <th class="px-3 py-1.5 text-left font-medium">IP</th>
            <th class="px-2 py-1.5 text-left font-medium">Client</th>
            <th class="px-2 py-1.5 text-left font-medium">Flags</th>
            <th class="px-2 py-1.5 text-right font-medium">%</th>
            <th class="px-2 py-1.5 text-right font-medium">↓</th>
            <th class="px-3 py-1.5 text-right font-medium">↑</th>
          </tr>
        </thead>
        <tbody>
          <For each={props.detail!.peers_list!}>
            {(p) => (
              <tr class="border-b border-white/[.03] hover:bg-white/[.02]">
                <td class="px-3 py-1.5 font-mono tabular-nums text-zinc-300">{p.ip}</td>
                <td class="truncate px-2 py-1.5 text-zinc-400" style={{'max-width': '120px'}} title={p.client}>{p.client || '—'}</td>
                <td class="px-2 py-1.5 font-mono text-zinc-500">{p.flags || '—'}</td>
                <td class="px-2 py-1.5 text-right font-mono tabular-nums text-zinc-400">{fmtPercent(p.progress)}</td>
                <td class="px-2 py-1.5 text-right font-mono tabular-nums text-zinc-400">{fmtRate(p.download_rate)}</td>
                <td class="px-3 py-1.5 text-right font-mono tabular-nums text-zinc-400">{fmtRate(p.upload_rate)}</td>
              </tr>
            )}
          </For>
        </tbody>
      </table>
    </Show>
  );
}
