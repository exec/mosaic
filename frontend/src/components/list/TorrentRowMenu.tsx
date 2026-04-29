import {ContextMenu} from '../ui/ContextMenu';
import {Pause, Play, Trash2, Folder, Copy, RotateCw} from 'lucide-solid';
import type {Torrent} from '../../lib/bindings';
import type {JSX} from 'solid-js';
import {Show} from 'solid-js';

type Props = {
  torrent: Torrent;
  onPause: () => void;
  onResume: () => void;
  onRemove: () => void;
  onCopyMagnet: () => void;
  children: JSX.Element;
};

export function TorrentRowMenu(props: Props) {
  return (
    <ContextMenu trigger={props.children}>
      <Show
        when={!props.torrent.paused}
        fallback={
          <ContextMenu.Item onSelect={props.onResume}>
            <Play class="h-3.5 w-3.5" />
            Resume
          </ContextMenu.Item>
        }
      >
        <ContextMenu.Item onSelect={props.onPause}>
          <Pause class="h-3.5 w-3.5" />
          Pause
        </ContextMenu.Item>
      </Show>
      <ContextMenu.Item disabled>
        <RotateCw class="h-3.5 w-3.5" />
        Recheck
      </ContextMenu.Item>
      <ContextMenu.Separator />
      <ContextMenu.Item disabled>
        <Folder class="h-3.5 w-3.5" />
        Open folder
      </ContextMenu.Item>
      <ContextMenu.Item onSelect={props.onCopyMagnet}>
        <Copy class="h-3.5 w-3.5" />
        Copy magnet
      </ContextMenu.Item>
      <ContextMenu.Separator />
      <ContextMenu.Item danger onSelect={props.onRemove}>
        <Trash2 class="h-3.5 w-3.5" />
        Remove
      </ContextMenu.Item>
    </ContextMenu>
  );
}
