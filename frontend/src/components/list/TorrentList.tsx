import {Match, Show, Switch, For} from 'solid-js';
import {toast} from 'solid-sonner';
import type {Torrent} from '../../lib/bindings';
import type {Density} from '../../lib/store';
import {TorrentCard} from './TorrentCard';
import {TorrentTable} from './TorrentTable';
import {EmptyState} from './EmptyState';
import {TorrentRowMenu} from './TorrentRowMenu';

type Props = {
  torrents: Torrent[];
  density: Density;
  selection: Set<string>;
  onSelect: (id: string, e: MouseEvent) => void;
  onPause: (id: string) => void;
  onResume: (id: string) => void;
  onRemove: (id: string) => void;
};

export function TorrentList(props: Props) {
  return (
    <Show when={props.torrents.length > 0} fallback={<EmptyState />}>
      <Switch>
        <Match when={props.density === 'cards'}>
          <div class="flex flex-col gap-2 p-3">
            <For each={props.torrents}>
              {(t) => (
                <TorrentRowMenu
                  torrent={t}
                  onPause={() => props.onPause(t.id)}
                  onResume={() => props.onResume(t.id)}
                  onRemove={() => props.onRemove(t.id)}
                  onCopyMagnet={() => {
                    if (t.magnet) {
                      navigator.clipboard.writeText(t.magnet);
                      toast.success('Magnet copied');
                    }
                  }}
                >
                  <TorrentCard
                    torrent={t}
                    selected={props.selection.has(t.id)}
                    onSelect={(e) => props.onSelect(t.id, e)}
                    onPause={() => props.onPause(t.id)}
                    onResume={() => props.onResume(t.id)}
                    onRemove={() => props.onRemove(t.id)}
                  />
                </TorrentRowMenu>
              )}
            </For>
          </div>
        </Match>
        <Match when={props.density === 'table'}>
          <TorrentTable
            torrents={props.torrents}
            selection={props.selection}
            onRowClick={props.onSelect}
          />
        </Match>
      </Switch>
    </Show>
  );
}
