import {Search, Magnet, FileDown, LayoutGrid, List, Zap, Settings} from 'lucide-solid';
import {Button} from '../ui/Button';
import {Tooltip} from '../ui/Tooltip';
import {ThemeToggle} from '../theme/ThemeToggle';
import type {Density} from '../../lib/store';

type Props = {
  searchQuery: string;
  onSearch: (q: string) => void;
  onAddMagnet: () => void;
  onAddTorrent: () => void;
  density: Density;
  onDensityChange: (d: Density) => void;
  altSpeedActive: boolean;
  onToggleAltSpeed: () => void;
  searchInputRef?: (el: HTMLInputElement) => void;
};

export function TopToolbar(props: Props) {
  return (
    <header
      class="flex h-12 shrink-0 items-center gap-3 border-b border-white/[.04] bg-zinc-950/80 px-3 backdrop-blur-md"
      style={{'-webkit-app-region': 'drag'}}
    >
      {/* Drag affordance — invisible but full-height area; the toolbar IS the drag region */}
      <div class="relative flex-1 max-w-md" style={{'-webkit-app-region': 'no-drag'}}>
        <Search class="pointer-events-none absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-zinc-500" />
        <input
          type="text"
          placeholder="Search torrents…"
          value={props.searchQuery}
          onInput={(e) => props.onSearch(e.currentTarget.value)}
          ref={(el) => props.searchInputRef?.(el)}
          class="w-full rounded-md border border-white/[.06] bg-white/[.02] py-1.5 pl-8 pr-3 text-sm text-zinc-100 placeholder:text-zinc-500 focus:border-accent-500/50 focus:bg-white/[.04] focus:outline-none focus:ring-1 focus:ring-accent-500/30"
        />
      </div>

      <div class="flex items-center gap-1.5" style={{'-webkit-app-region': 'no-drag'}}>
        <Button variant="secondary" onClick={props.onAddTorrent}>
          <FileDown class="h-3.5 w-3.5" />
          .torrent
        </Button>
        <Button variant="primary" onClick={props.onAddMagnet}>
          <Magnet class="h-3.5 w-3.5" />
          Magnet
        </Button>

        <span class="mx-1 h-5 w-px bg-white/[.06]" />

        <Tooltip label={`Alt-speed limits ${props.altSpeedActive ? 'on' : 'off'}`}>
          <button
            class="grid h-7 w-7 place-items-center rounded-md transition-colors duration-150"
            classList={{
              'bg-accent-500/[.15] text-accent-300': props.altSpeedActive,
              'text-zinc-400 hover:bg-white/[.04] hover:text-zinc-100': !props.altSpeedActive,
            }}
            onClick={props.onToggleAltSpeed}
          >
            <Zap class="h-3.5 w-3.5" />
          </button>
        </Tooltip>

        <Tooltip label="Density: cards / table">
          <div class="inline-flex items-center rounded-md border border-white/[.06] bg-white/[.02] p-0.5">
            <button
              class="grid h-6 w-6 place-items-center rounded text-zinc-400 transition-colors hover:text-zinc-100"
              classList={{'!bg-white/10 !text-zinc-100': props.density === 'cards'}}
              onClick={() => props.onDensityChange('cards')}
              aria-label="Cards"
            >
              <LayoutGrid class="h-3 w-3" />
            </button>
            <button
              class="grid h-6 w-6 place-items-center rounded text-zinc-400 transition-colors hover:text-zinc-100"
              classList={{'!bg-white/10 !text-zinc-100': props.density === 'table'}}
              onClick={() => props.onDensityChange('table')}
              aria-label="Table"
            >
              <List class="h-3 w-3" />
            </button>
          </div>
        </Tooltip>

        <ThemeToggle />

        <Tooltip label="Settings">
          <button class="grid h-7 w-7 place-items-center rounded-md text-zinc-400 hover:bg-white/[.04] hover:text-zinc-100" disabled>
            <Settings class="h-3.5 w-3.5" />
          </button>
        </Tooltip>
      </div>
    </header>
  );
}
