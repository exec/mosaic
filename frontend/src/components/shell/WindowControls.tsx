import {api} from '../../lib/bindings';

// Win11-style caption controls — three 46×32 buttons in the top-right.
// Used on Wails+Windows and Wails+Linux where we hide the OS title bar.
export function WindowControls() {
  return (
    <div
      class="flex h-full shrink-0"
      style={{'-webkit-app-region': 'no-drag'}}
    >
      <button
        type="button"
        onClick={() => api.windowMinimise().catch(() => {})}
        class="grid h-full w-[46px] place-items-center text-zinc-400 transition-colors duration-100 hover:bg-white/10 hover:text-zinc-100"
        aria-label="Minimize"
      >
        <svg width="10" height="10" viewBox="0 0 10 10">
          <line x1="0" y1="5.5" x2="10" y2="5.5" stroke="currentColor" stroke-width="1" />
        </svg>
      </button>
      <button
        type="button"
        onClick={() => api.windowMaximise().catch(() => {})}
        class="grid h-full w-[46px] place-items-center text-zinc-400 transition-colors duration-100 hover:bg-white/10 hover:text-zinc-100"
        aria-label="Maximize"
      >
        <svg width="10" height="10" viewBox="0 0 10 10">
          <rect x="0.5" y="0.5" width="9" height="9" stroke="currentColor" fill="none" stroke-width="1" />
        </svg>
      </button>
      <button
        type="button"
        onClick={() => api.windowClose().catch(() => {})}
        class="grid h-full w-[46px] place-items-center text-zinc-400 transition-colors duration-100 hover:bg-rose-600 hover:text-white"
        aria-label="Close"
      >
        <svg width="10" height="10" viewBox="0 0 10 10">
          <line x1="0" y1="0" x2="10" y2="10" stroke="currentColor" stroke-width="1" />
          <line x1="0" y1="10" x2="10" y2="0" stroke="currentColor" stroke-width="1" />
        </svg>
      </button>
    </div>
  );
}
