import {For, Show} from 'solid-js';
import type {Torrent} from '../lib/bindings';

function fmtBytes(n: number): string {
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
  if (n < 1024 * 1024 * 1024) return `${(n / 1024 / 1024).toFixed(1)} MB`;
  return `${(n / 1024 / 1024 / 1024).toFixed(2)} GB`;
}

function fmtRate(n: number): string {
  return `${fmtBytes(n)}/s`;
}

type Props = {
  torrents: Torrent[];
  onPause: (id: string) => void;
  onResume: (id: string) => void;
  onRemove: (id: string) => void;
};

export function TorrentList(props: Props) {
  return (
    <div class="flex flex-col gap-2 p-3">
      <Show when={props.torrents.length === 0}>
        <div class="text-sm text-zinc-500 p-6 text-center">
          No torrents yet. Use <kbd>Add Magnet</kbd> to start one.
        </div>
      </Show>
      <For each={props.torrents}>
        {(t) => (
          <div class="rounded-lg border border-zinc-800 bg-zinc-900/50 p-3">
            <div class="flex items-baseline justify-between gap-3">
              <div class="font-medium truncate">{t.name}</div>
              <div class="text-xs text-zinc-500">{fmtBytes(t.total_bytes)}</div>
            </div>
            <div class="mt-2 h-1.5 rounded bg-zinc-800 overflow-hidden">
              <div
                class="h-full bg-blue-500 transition-[width] duration-300"
                style={{width: `${(t.progress * 100).toFixed(1)}%`}}
              />
            </div>
            <div class="mt-2 flex items-center justify-between text-xs text-zinc-400">
              <span>
                {(t.progress * 100).toFixed(1)}% · ↓ {fmtRate(t.download_rate)} · ↑ {fmtRate(t.upload_rate)} · peers {t.peers}
              </span>
              <span class="flex gap-2">
                <Show
                  when={!t.paused}
                  fallback={
                    <button class="text-blue-400 hover:underline" onClick={() => props.onResume(t.id)}>
                      Resume
                    </button>
                  }
                >
                  <button class="text-amber-400 hover:underline" onClick={() => props.onPause(t.id)}>
                    Pause
                  </button>
                </Show>
                <button class="text-red-400 hover:underline" onClick={() => props.onRemove(t.id)}>
                  Remove
                </button>
              </span>
            </div>
          </div>
        )}
      </For>
    </div>
  );
}
