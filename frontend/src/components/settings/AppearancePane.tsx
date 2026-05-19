import {For} from 'solid-js';
import {Check} from 'lucide-solid';
import {THEMES} from '../../lib/themes';
import {theme as currentTheme, setTheme} from '../../lib/appearance';

function PaneHeader(props: {title: string; subtitle?: string}) {
  return (
    <div class="mb-4 border-b border-white/[.04] pb-3">
      <h2 class="text-lg font-semibold text-zinc-100">{props.title}</h2>
      {props.subtitle && <p class="mt-0.5 text-sm text-zinc-500">{props.subtitle}</p>}
    </div>
  );
}

export function AppearancePane() {
  return (
    <div class="mx-auto max-w-2xl px-6 py-6">
      <PaneHeader
        title="Appearance"
        subtitle="Pick an accent. Affects buttons, links, focus rings, selection highlights, and the download series in the speed graph."
      />
      <div class="grid grid-cols-2 gap-3" data-testid="theme-grid">
        <For each={THEMES}>
          {(t) => {
            // currentTheme is a reactive signal — re-evaluates per render.
            const active = () => currentTheme() === t.id;
            return (
              <button
                type="button"
                onClick={() => setTheme(t.id)}
                aria-pressed={active()}
                data-testid={`theme-${t.id}`}
                class="group relative flex items-center gap-3 rounded-lg border bg-white/[.02] p-3 text-left transition-colors hover:bg-white/[.04] focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-500/40"
                classList={{
                  'border-accent-500/60 bg-white/[.04]': active(),
                  'border-white/[.06]': !active(),
                }}
              >
                <span
                  aria-hidden
                  class="h-8 w-8 shrink-0 rounded-full border border-white/10 shadow-inner"
                  style={{background: t.swatch}}
                />
                <span class="flex flex-col">
                  <span class="text-sm font-medium text-zinc-100">{t.label}</span>
                  <span class="text-xs text-zinc-500">{t.blurb}</span>
                </span>
                {active() && (
                  <Check class="absolute right-3 top-3 h-4 w-4 text-accent-400" />
                )}
              </button>
            );
          }}
        </For>
      </div>
    </div>
  );
}
