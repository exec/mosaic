import {createSignal, For, Show} from 'solid-js';
import {Plus, Trash2, Pencil, Check, X} from 'lucide-solid';
import {toast} from 'solid-sonner';
import type {ScheduleRuleDTO} from '../../lib/bindings';
import {Button} from '../ui/Button';

type Props = {
  rules: ScheduleRuleDTO[];
  onCreate: (r: ScheduleRuleDTO) => Promise<void>;
  onUpdate: (r: ScheduleRuleDTO) => Promise<void>;
  onDelete: (id: number) => Promise<void>;
};

const DAY_LABELS = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'];

function dayLabels(mask: number): string {
  if (mask === 0) return 'Never';
  if (mask === 0b1111111) return 'Every day';
  if (mask === 0b0111110) return 'Mon-Fri';
  if (mask === 0b1000001) return 'Weekends';
  const days: string[] = [];
  for (let i = 0; i < 7; i++) {
    if (mask & (1 << i)) days.push(DAY_LABELS[i]);
  }
  return days.join(', ');
}

function fmtMin(m: number): string {
  const hh = Math.floor(m / 60).toString().padStart(2, '0');
  const mm = (m % 60).toString().padStart(2, '0');
  return `${hh}:${mm}`;
}

function parseTime(t: string): number {
  const [h, m] = t.split(':').map((x) => parseInt(x, 10));
  if (Number.isNaN(h) || Number.isNaN(m)) return 0;
  return h * 60 + m;
}

export function SchedulePane(props: Props) {
  const [creating, setCreating] = createSignal(false);
  const [editingID, setEditingID] = createSignal<number | null>(null);

  return (
    <div class="mx-auto max-w-3xl px-6 py-6">
      <div class="mb-4 flex items-center justify-between border-b border-white/[.04] pb-3">
        <div>
          <h2 class="text-lg font-semibold text-zinc-100">Schedule</h2>
          <p class="mt-0.5 text-sm text-zinc-500">Apply bandwidth limits automatically by day and time.</p>
        </div>
        <Button variant="primary" onClick={() => setCreating(true)}>
          <Plus class="h-3.5 w-3.5" />
          New rule
        </Button>
      </div>

      <Show when={creating()}>
        <RuleForm
          initial={{id: 0, days_mask: 0b0111110, start_min: 22 * 60, end_min: 6 * 60, down_kbps: 0, up_kbps: 0, alt_only: true, enabled: true}}
          onCancel={() => setCreating(false)}
          onSubmit={async (r) => {
            try {
              await props.onCreate(r);
              setCreating(false);
              toast.success('Schedule rule created');
            } catch (e) { toast.error(String(e)); }
          }}
        />
      </Show>

      <Show when={!creating() && props.rules.length === 0}>
        <p class="py-6 text-center text-sm text-zinc-500">No schedule rules. Click <kbd>New rule</kbd> to add one.</p>
      </Show>

      <ul class="flex flex-col gap-px">
        <For each={props.rules}>
          {(r) => (
            <li class="border-b border-white/[.03]">
              <Show
                when={editingID() === r.id}
                fallback={
                  <div class="flex items-center justify-between py-2.5 px-2 hover:bg-white/[.02]">
                    <div class="flex items-center gap-3">
                      <span
                        class="h-2 w-2 rounded-full"
                        classList={{'bg-seed': r.enabled, 'bg-zinc-600': !r.enabled}}
                        title={r.enabled ? 'Enabled' : 'Disabled'}
                      />
                      <span class="text-sm text-zinc-100 font-mono tabular-nums">{fmtMin(r.start_min)}–{fmtMin(r.end_min)}</span>
                      <span class="text-xs text-zinc-400">{dayLabels(r.days_mask)}</span>
                      <span class="text-xs text-zinc-500">
                        {r.alt_only ? 'Alt-speed' : `${r.down_kbps} ↓ / ${r.up_kbps} ↑ kbps`}
                      </span>
                    </div>
                    <div class="flex gap-1">
                      <button class="grid h-7 w-7 place-items-center rounded text-zinc-500 hover:bg-white/[.04] hover:text-zinc-100" onClick={() => setEditingID(r.id)} title="Edit">
                        <Pencil class="h-3 w-3" />
                      </button>
                      <button
                        class="grid h-7 w-7 place-items-center rounded text-zinc-500 hover:bg-rose-500/20 hover:text-rose-300"
                        onClick={async () => {
                          if (!confirm('Delete this schedule rule?')) return;
                          try {
                            await props.onDelete(r.id);
                            toast.success('Rule deleted');
                          } catch (e) { toast.error(String(e)); }
                        }}
                        title="Delete"
                      >
                        <Trash2 class="h-3 w-3" />
                      </button>
                    </div>
                  </div>
                }
              >
                <RuleForm
                  initial={r}
                  onCancel={() => setEditingID(null)}
                  onSubmit={async (next) => {
                    try {
                      await props.onUpdate(next);
                      setEditingID(null);
                      toast.success('Rule updated');
                    } catch (e) { toast.error(String(e)); }
                  }}
                />
              </Show>
            </li>
          )}
        </For>
      </ul>
    </div>
  );
}

function RuleForm(props: {
  initial: ScheduleRuleDTO;
  onCancel: () => void;
  onSubmit: (r: ScheduleRuleDTO) => Promise<void>;
}) {
  const [daysMask, setDaysMask] = createSignal(props.initial.days_mask);
  const [start, setStart] = createSignal(fmtMin(props.initial.start_min));
  const [end, setEnd] = createSignal(fmtMin(props.initial.end_min));
  const [down, setDown] = createSignal(props.initial.down_kbps);
  const [up, setUp] = createSignal(props.initial.up_kbps);
  const [altOnly, setAltOnly] = createSignal(props.initial.alt_only);
  const [enabled, setEnabled] = createSignal(props.initial.enabled);

  return (
    <form
      class="flex flex-col gap-2 rounded-md border border-white/[.06] bg-white/[.02] p-3 my-2"
      onSubmit={async (e) => {
        e.preventDefault();
        if (daysMask() === 0) return;
        await props.onSubmit({
          id: props.initial.id,
          days_mask: daysMask(),
          start_min: parseTime(start()),
          end_min: parseTime(end()),
          down_kbps: down(),
          up_kbps: up(),
          alt_only: altOnly(),
          enabled: enabled(),
        });
      }}
    >
      <div class="grid grid-cols-[80px_1fr] items-center gap-2">
        <label class="text-xs text-zinc-500">Days</label>
        <div class="flex flex-wrap gap-1">
          <For each={DAY_LABELS}>
            {(day, i) => (
              <button
                type="button"
                class="rounded px-2 py-1 text-xs border border-white/[.06] text-zinc-300 hover:bg-white/[.04]"
                classList={{'bg-accent-500/20 border-accent-500/40 text-accent-200': (daysMask() & (1 << i())) !== 0}}
                onClick={() => setDaysMask(daysMask() ^ (1 << i()))}
              >
                {day}
              </button>
            )}
          </For>
        </div>
      </div>
      <div class="grid grid-cols-[80px_1fr] items-center gap-2">
        <label class="text-xs text-zinc-500">Start</label>
        <input
          type="time"
          class="w-32 rounded border border-white/[.06] bg-black/30 px-2 py-1 text-sm text-zinc-100 focus:border-accent-500/50 focus:outline-none"
          value={start()}
          onInput={(e) => setStart(e.currentTarget.value)}
        />
      </div>
      <div class="grid grid-cols-[80px_1fr] items-center gap-2">
        <label class="text-xs text-zinc-500">End</label>
        <input
          type="time"
          class="w-32 rounded border border-white/[.06] bg-black/30 px-2 py-1 text-sm text-zinc-100 focus:border-accent-500/50 focus:outline-none"
          value={end()}
          onInput={(e) => setEnd(e.currentTarget.value)}
        />
      </div>
      <div class="grid grid-cols-[80px_1fr] items-center gap-2">
        <label class="text-xs text-zinc-500">Mode</label>
        <label class="inline-flex items-center gap-2 text-sm text-zinc-200">
          <input type="checkbox" checked={altOnly()} onChange={(e) => setAltOnly(e.currentTarget.checked)} />
          Use alt-speed values
        </label>
      </div>
      <Show when={!altOnly()}>
        <div class="grid grid-cols-[80px_1fr] items-center gap-2">
          <label class="text-xs text-zinc-500">Down (kbps)</label>
          <input
            type="number"
            min="0"
            class="w-32 rounded border border-white/[.06] bg-black/30 px-2 py-1 text-sm text-zinc-100 focus:border-accent-500/50 focus:outline-none"
            value={down()}
            onInput={(e) => setDown(parseInt(e.currentTarget.value || '0', 10))}
          />
        </div>
        <div class="grid grid-cols-[80px_1fr] items-center gap-2">
          <label class="text-xs text-zinc-500">Up (kbps)</label>
          <input
            type="number"
            min="0"
            class="w-32 rounded border border-white/[.06] bg-black/30 px-2 py-1 text-sm text-zinc-100 focus:border-accent-500/50 focus:outline-none"
            value={up()}
            onInput={(e) => setUp(parseInt(e.currentTarget.value || '0', 10))}
          />
        </div>
      </Show>
      <div class="grid grid-cols-[80px_1fr] items-center gap-2">
        <label class="text-xs text-zinc-500">Status</label>
        <label class="inline-flex items-center gap-2 text-sm text-zinc-200">
          <input type="checkbox" checked={enabled()} onChange={(e) => setEnabled(e.currentTarget.checked)} />
          Enabled
        </label>
      </div>
      <div class="flex justify-end gap-2 mt-1">
        <Button type="button" variant="ghost" onClick={props.onCancel}>
          <X class="h-3.5 w-3.5" />
          Cancel
        </Button>
        <Button type="submit" variant="primary" disabled={daysMask() === 0}>
          <Check class="h-3.5 w-3.5" />
          Save
        </Button>
      </div>
    </form>
  );
}
