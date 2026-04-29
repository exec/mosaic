import {For, Show} from 'solid-js';
import type {DetailDTO} from '../../lib/bindings';
import {fmtTimestamp} from '../../lib/format';

type Props = {detail: DetailDTO | null};

export function TrackersTab(props: Props) {
  return (
    <Show
      when={props.detail?.trackers?.length}
      fallback={<div class="p-4 text-xs text-zinc-500">No trackers known.</div>}
    >
      <div class="flex flex-col">
        <For each={props.detail!.trackers!}>
          {(t) => (
            <div class="border-b border-white/[.03] px-4 py-2 text-xs">
              <div class="flex items-baseline justify-between gap-2">
                <span class="truncate font-mono text-zinc-300" title={t.url}>{t.url}</span>
                <span
                  class="shrink-0 rounded px-1.5 py-0.5 text-[10px] uppercase tracking-wider"
                  classList={{
                    'bg-seed/[.10] text-seed': t.status === 'OK',
                    'bg-zinc-700/30 text-zinc-400': t.status !== 'OK',
                  }}
                >
                  {t.status}
                </span>
              </div>
              <div class="mt-1 flex justify-between font-mono tabular-nums text-zinc-500">
                <span>Seeds {t.seeds} · Peers {t.peers}</span>
                <span>Last {fmtTimestamp(t.last_announce)}</span>
              </div>
            </div>
          )}
        </For>
      </div>
    </Show>
  );
}
