import {createSignal, onCleanup, onMount, Show, type JSX} from 'solid-js';
import {Magnet, FileDown} from 'lucide-solid';
import {toast} from 'solid-sonner';

type Props = {
  onMagnet: (m: string) => Promise<void>;
  onTorrentBytes: (bytes: Uint8Array) => Promise<void>;
  children: JSX.Element;
};

export function DropZone(props: Props) {
  const [active, setActive] = createSignal(false);

  onMount(() => {
    const onDragOver = (e: DragEvent) => {
      e.preventDefault();
      if (e.dataTransfer) e.dataTransfer.dropEffect = 'copy';
      setActive(true);
    };
    const onDragLeave = (e: DragEvent) => {
      // only deactivate if leaving window
      if ((e as any).relatedTarget == null) setActive(false);
    };
    const onDrop = async (e: DragEvent) => {
      e.preventDefault();
      setActive(false);
      const text = e.dataTransfer?.getData('text/plain') ?? '';
      if (text.startsWith('magnet:?')) {
        try { await props.onMagnet(text); }
        catch (err) { toast.error(String(err)); }
        return;
      }
      if (e.dataTransfer?.files.length) {
        const file = e.dataTransfer.files[0];
        if (!file.name.endsWith('.torrent')) {
          toast.error('Only .torrent files are supported');
          return;
        }
        try {
          const bytes = new Uint8Array(await file.arrayBuffer());
          await props.onTorrentBytes(bytes);
        } catch (err) { toast.error(String(err)); }
      }
    };
    window.addEventListener('dragover', onDragOver);
    window.addEventListener('dragleave', onDragLeave);
    window.addEventListener('drop', onDrop);
    onCleanup(() => {
      window.removeEventListener('dragover', onDragOver);
      window.removeEventListener('dragleave', onDragLeave);
      window.removeEventListener('drop', onDrop);
    });
  });

  return (
    <div class="relative h-full">
      {props.children}
      <Show when={active()}>
        <div class="pointer-events-none absolute inset-2 z-50 grid place-items-center rounded-2xl border-2 border-dashed border-accent-500/60 bg-accent-500/[.06] backdrop-blur-sm animate-in fade-in">
          <div class="flex flex-col items-center gap-3 text-accent-200">
            <div class="flex gap-2">
              <Magnet class="h-8 w-8" />
              <FileDown class="h-8 w-8" />
            </div>
            <div class="text-base font-semibold">Drop to add torrent</div>
          </div>
        </div>
      </Show>
    </div>
  );
}
