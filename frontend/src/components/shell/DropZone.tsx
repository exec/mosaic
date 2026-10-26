import {createSignal, For, onCleanup, onMount, Show, type JSX} from 'solid-js';
import {Magnet, FileDown} from 'lucide-solid';
import {toast} from 'solid-sonner';

type Props = {
  onMagnet: (m: string) => Promise<void>;
  onTorrentBytes: (bytes: Uint8Array, name?: string) => Promise<boolean | void>;
  children: JSX.Element;
};

export function DropZone(props: Props) {
  const [active, setActive] = createSignal(false);
  const [results,setResults]=createSignal<{name:string;result:string}[]>([]);
  const [processing,setProcessing]=createSignal(false);
  let disposed=false;
  onCleanup(()=>{disposed=true;});

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
      if(processing()){toast.error('Finish reviewing the current drop first');return;}
      const text = e.dataTransfer?.getData('text/plain') ?? '';
      if (text.startsWith('magnet:?')) {
        try { await props.onMagnet(text); }
        catch (err) { toast.error(String(err)); }
        return;
      }
      if (e.dataTransfer?.files.length) {
        const files=Array.from(e.dataTransfer.files);setProcessing(true);setResults([]);
        for(const file of files){
          if(disposed)break;
          let result='';
          try {
            if(!file.name.toLowerCase().endsWith('.torrent'))throw new Error('Only .torrent files are supported');
            const bytes=new Uint8Array(await file.arrayBuffer());
            if(disposed)break;
            result=(await props.onTorrentBytes(bytes,file.name))===false?'Skipped / review closed':'Added';
          } catch(err){result=String(err);}
          if(!disposed)setResults(rows=>[...rows,{name:file.name,result}]);
        }
        if(!disposed)setProcessing(false);
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
      <Show when={results().length||processing()}><section aria-label="Dropped file results" aria-live="polite" class="absolute bottom-3 left-3 right-3 z-30 max-h-44 overflow-auto rounded-lg border border-white/10 bg-zinc-900/95 p-3 text-xs shadow-xl">
       <div class="flex items-center justify-between gap-2 text-zinc-300"><span>{processing()?'Reviewing dropped torrents…':'Drop complete'}</span><Show when={!processing()}><button type="button" aria-label="Dismiss drop results" onClick={()=>setResults([])}>Dismiss</button></Show></div>
       <For each={results()}>{row=><p class="mt-1 break-all text-zinc-500">{row.name} · {row.result}</p>}</For>
      </section></Show>
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
