import {createEffect, createSignal} from 'solid-js';
import {toast} from 'solid-sonner';
import {ThemeToggle} from '../theme/ThemeToggle';
import {Button} from '../ui/Button';

type Props = {
  defaultSavePath: string;
  onSetDefaultSavePath: (path: string) => Promise<void>;
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

export function GeneralPane(props: Props) {
  const [savePath, setSavePath] = createSignal(props.defaultSavePath);
  // Re-sync when the prop arrives later — the boot fetch in store.ts is
  // async, so a user landing here first sees an empty input until it lands.
  createEffect(() => { setSavePath(props.defaultSavePath); });
  const dirty = () => savePath() !== props.defaultSavePath;

  const save = async () => {
    try {
      await props.onSetDefaultSavePath(savePath());
      toast.success('Default save path updated');
    } catch (e) {
      toast.error(String(e));
    }
  };

  return (
    <div class="mx-auto max-w-2xl px-6 py-6">
      <PaneHeader title="General" subtitle="App-wide preferences for theme and default save target." />
      <Field label="Theme" help="Mosaic follows system by default.">
        <ThemeToggle />
      </Field>
      <Field label="Default save path" help="New torrents land here unless you override per-add in the modal.">
        <div class="flex items-center gap-2">
          <input
            type="text"
            class="flex-1 rounded-md border border-white/[.06] bg-black/30 px-2 py-1.5 font-mono text-xs text-zinc-100 focus:border-accent-500/50 focus:outline-none focus:ring-1 focus:ring-accent-500/30"
            value={savePath()}
            onInput={(e) => setSavePath(e.currentTarget.value)}
          />
          <Button variant="primary" onClick={save} disabled={!dirty() || !savePath().trim()}>
            Save
          </Button>
        </div>
      </Field>
    </div>
  );
}
