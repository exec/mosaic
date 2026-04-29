import {createSignal, createEffect} from 'solid-js';
import {toast} from 'solid-sonner';
import type {LimitsDTO, QueueLimitsDTO} from '../../lib/bindings';
import {Button} from '../ui/Button';

type Props = {
  limits: LimitsDTO;
  queueLimits: QueueLimitsDTO;
  onSetLimits: (l: LimitsDTO) => Promise<void>;
  onSetQueueLimits: (q: QueueLimitsDTO) => Promise<void>;
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

function NumberInput(props: {value: number; onInput: (n: number) => void; suffix?: string; placeholder?: string}) {
  return (
    <div class="inline-flex items-center gap-1.5">
      <input
        type="number"
        min={0}
        class="w-28 rounded border border-white/[.06] bg-black/30 px-2 py-1 text-right font-mono text-sm tabular-nums text-zinc-100 focus:border-accent-500/50 focus:outline-none"
        value={props.value || ''}
        placeholder={props.placeholder ?? '0'}
        onInput={(e) => props.onInput(parseInt(e.currentTarget.value || '0', 10))}
      />
      {props.suffix && <span class="text-xs text-zinc-500">{props.suffix}</span>}
    </div>
  );
}

export function ConnectionPane(props: Props) {
  const [down, setDown] = createSignal(props.limits.down_kbps);
  const [up, setUp] = createSignal(props.limits.up_kbps);
  const [altDown, setAltDown] = createSignal(props.limits.alt_down_kbps);
  const [altUp, setAltUp] = createSignal(props.limits.alt_up_kbps);
  const [maxDL, setMaxDL] = createSignal(props.queueLimits.max_active_downloads);
  const [maxSeeds, setMaxSeeds] = createSignal(props.queueLimits.max_active_seeds);

  // Re-sync when prop changes (initial fetch races)
  createEffect(() => { setDown(props.limits.down_kbps); });
  createEffect(() => { setUp(props.limits.up_kbps); });
  createEffect(() => { setAltDown(props.limits.alt_down_kbps); });
  createEffect(() => { setAltUp(props.limits.alt_up_kbps); });
  createEffect(() => { setMaxDL(props.queueLimits.max_active_downloads); });
  createEffect(() => { setMaxSeeds(props.queueLimits.max_active_seeds); });

  const saveLimits = async () => {
    try {
      await props.onSetLimits({
        down_kbps: down(),
        up_kbps: up(),
        alt_down_kbps: altDown(),
        alt_up_kbps: altUp(),
        alt_active: props.limits.alt_active,
      });
      toast.success('Bandwidth limits saved');
    } catch (e) { toast.error(String(e)); }
  };

  const saveQueue = async () => {
    try {
      await props.onSetQueueLimits({max_active_downloads: maxDL(), max_active_seeds: maxSeeds()});
      toast.success('Queue limits saved');
    } catch (e) { toast.error(String(e)); }
  };

  return (
    <div class="mx-auto max-w-2xl px-6 py-6">
      <PaneHeader title="Connection" subtitle="Bandwidth limits, alt-speed, and queue slots." />

      <div class="text-xs uppercase tracking-wider text-zinc-500 mt-2 mb-1 px-1">Bandwidth</div>
      <Field label="Download limit" help="0 = unlimited">
        <NumberInput value={down()} onInput={setDown} suffix="KB/s" />
      </Field>
      <Field label="Upload limit" help="0 = unlimited">
        <NumberInput value={up()} onInput={setUp} suffix="KB/s" />
      </Field>
      <Field label="Alt download" help="When alt-speed is on (toolbar Zap button)">
        <NumberInput value={altDown()} onInput={setAltDown} suffix="KB/s" />
      </Field>
      <Field label="Alt upload">
        <NumberInput value={altUp()} onInput={setAltUp} suffix="KB/s" />
      </Field>
      <div class="flex justify-end mt-3">
        <Button variant="primary" onClick={saveLimits}>Save bandwidth</Button>
      </div>

      <div class="text-xs uppercase tracking-wider text-zinc-500 mt-6 mb-1 px-1">Queue</div>
      <Field label="Max active downloads" help="0 = unlimited">
        <NumberInput value={maxDL()} onInput={setMaxDL} suffix="torrents" />
      </Field>
      <Field label="Max active seeds" help="0 = unlimited">
        <NumberInput value={maxSeeds()} onInput={setMaxSeeds} suffix="torrents" />
      </Field>
      <div class="flex justify-end mt-3">
        <Button variant="primary" onClick={saveQueue}>Save queue</Button>
      </div>
    </div>
  );
}
