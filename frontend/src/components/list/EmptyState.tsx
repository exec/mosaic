import {Magnet, FileDown} from 'lucide-solid';

export function EmptyState() {
  return (
    <div class="flex h-full flex-col items-center justify-center gap-4 p-12 text-center">
      <div class="relative grid h-20 w-20 place-items-center rounded-2xl border border-white/[.06] bg-white/[.02]">
        <Magnet class="h-9 w-9 text-zinc-500" />
        <FileDown class="absolute -bottom-1 -right-1 h-7 w-7 rounded-md border border-white/[.06] bg-zinc-900 p-1 text-zinc-400" />
      </div>
      <div class="max-w-sm">
        <h2 class="text-base font-semibold text-zinc-200">Drop a torrent to begin</h2>
        <p class="mt-1 text-sm text-zinc-500">
          Drag a <span class="font-mono text-zinc-400">.torrent</span> file or magnet link onto this window, or use the buttons up top.
        </p>
      </div>
    </div>
  );
}
