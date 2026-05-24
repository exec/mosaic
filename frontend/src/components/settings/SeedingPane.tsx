import {createEffect, createSignal, Show} from 'solid-js';
import {toast} from 'solid-sonner';
import type {SeedingDefaultsDTO} from '../../lib/bindings';
import {Button} from '../ui/Button';

type Props = {
  seedingDefaults: SeedingDefaultsDTO;
  onSetSeedingDefaults: (d: SeedingDefaultsDTO) => Promise<void>;
};

function PaneHeader(props: {title: string; subtitle?: string}) {
  return (
    <div class="mb-4 border-b border-white/[.04] pb-3">
      <h2 class="text-lg font-semibold text-zinc-100">{props.title}</h2>
      {props.subtitle && <p class="mt-0.5 text-sm text-zinc-500">{props.subtitle}</p>}
    </div>
  );
}

function Field(props: {label: string; help?: string; children: any}) {
  return (
    <div class="grid grid-cols-[200px_1fr] items-start gap-4 py-3 border-b border-white/[.03]">
      <div>
        <div class="text-sm text-zinc-200">{props.label}</div>
        {props.help && <div class="mt-0.5 text-xs text-zinc-500">{props.help}</div>}
      </div>
      <div>{props.children}</div>
    </div>
  );
}

export function SeedingPane(props: Props) {
  const [ratioEnabled, setRatioEnabled] = createSignal(props.seedingDefaults.ratio_limit !== null);
  const [ratio, setRatio] = createSignal(props.seedingDefaults.ratio_limit !== null ? String(props.seedingDefaults.ratio_limit) : '');
  const [timeEnabled, setTimeEnabled] = createSignal(props.seedingDefaults.time_min_limit !== null);
  const [timeMin, setTimeMin] = createSignal(props.seedingDefaults.time_min_limit !== null ? String(props.seedingDefaults.time_min_limit) : '');

  // Re-sync when prop arrives from boot fetch
  createEffect(() => {
    const d = props.seedingDefaults;
    setRatioEnabled(d.ratio_limit !== null);
    setRatio(d.ratio_limit !== null ? String(d.ratio_limit) : '');
    setTimeEnabled(d.time_min_limit !== null);
    setTimeMin(d.time_min_limit !== null ? String(d.time_min_limit) : '');
  });

  const save = async () => {
    const dto: SeedingDefaultsDTO = {
      ratio_limit: ratioEnabled() && ratio() !== '' ? parseFloat(ratio()) : null,
      time_min_limit: timeEnabled() && timeMin() !== '' ? parseInt(timeMin(), 10) : null,
    };
    try {
      await props.onSetSeedingDefaults(dto);
      toast.success('Seeding defaults saved');
    } catch (e) {
      toast.error(`Couldn't save seeding defaults — ${String(e)}`);
    }
  };

  return (
    <div class="mx-auto max-w-2xl px-6 py-6">
      <PaneHeader
        title="Seeding"
        subtitle="Global default stop-conditions for seeding. Individual torrents can override these in the inspector."
      />

      <div class="text-xs uppercase tracking-wider text-zinc-500 mt-2 mb-1 px-1">Stop conditions</div>

      <Field
        label="Ratio limit"
        help="Stop seeding when uploaded / downloaded ≥ this value. 0 = disabled."
      >
        <div class="flex items-center gap-2">
          <label class="flex items-center gap-2 text-sm text-zinc-200">
            <input
              type="checkbox"
              checked={ratioEnabled()}
              onChange={(e) => setRatioEnabled(e.currentTarget.checked)}
              class="accent-accent-500"
            />
            Enabled
          </label>
          <Show when={ratioEnabled()}>
            <input
              type="number"
              min={0}
              step={0.1}
              class="w-24 rounded border border-white/[.06] bg-black/30 px-2 py-1 text-right font-mono text-sm tabular-nums text-zinc-100 focus:border-accent-500/60 focus:outline-none focus:ring-2 focus:ring-accent-500/40"
              value={ratio()}
              placeholder="e.g. 2.0"
              onInput={(e) => setRatio(e.currentTarget.value)}
            />
          </Show>
        </div>
      </Field>

      <Field
        label="Time limit"
        help="Stop seeding after this many minutes of seeding. 0 = disabled."
      >
        <div class="flex items-center gap-2">
          <label class="flex items-center gap-2 text-sm text-zinc-200">
            <input
              type="checkbox"
              checked={timeEnabled()}
              onChange={(e) => setTimeEnabled(e.currentTarget.checked)}
              class="accent-accent-500"
            />
            Enabled
          </label>
          <Show when={timeEnabled()}>
            <div class="inline-flex items-center gap-1.5">
              <input
                type="number"
                min={1}
                class="w-24 rounded border border-white/[.06] bg-black/30 px-2 py-1 text-right font-mono text-sm tabular-nums text-zinc-100 focus:border-accent-500/60 focus:outline-none focus:ring-2 focus:ring-accent-500/40"
                value={timeMin()}
                placeholder="minutes"
                onInput={(e) => setTimeMin(e.currentTarget.value)}
              />
              <span class="text-xs text-zinc-500">min</span>
            </div>
          </Show>
        </div>
      </Field>

      <div class="flex justify-end mt-3">
        <Button variant="primary" onClick={save}>Save seeding defaults</Button>
      </div>
    </div>
  );
}
