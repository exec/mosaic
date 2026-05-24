import {Magnet, FileDown} from 'lucide-solid';

export function EmptyState() {
  return (
    <div class="flex h-full flex-col items-center justify-center gap-4 p-12 text-center">
      {/* Container picks up an accent tint when the user hovers the empty
          surface — same visual vocabulary the DropZone overlay uses on
          dragenter, so hovering "near" the affordance already foreshadows
          the drop target. The Magnet glyph breathes gently (drop-hint,
          3s ease) so the affordance feels alive without being noisy. */}
      <div class="group/empty relative grid h-20 w-20 place-items-center rounded-2xl border border-white/[.06] bg-white/[.02] transition-colors duration-200 hover:border-accent-500/30 hover:bg-accent-500/[.04]">
        <Magnet class="h-9 w-9 text-zinc-500 [animation:drop-hint_3s_ease-in-out_infinite] group-hover/empty:text-accent-400 transition-colors duration-200" />
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
