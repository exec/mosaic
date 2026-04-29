import {createSignal, Show} from 'solid-js';

type Props = {
  open: boolean;
  onClose: () => void;
  onSubmit: (magnet: string) => Promise<void>;
};

export function AddMagnetModal(props: Props) {
  const [value, setValue] = createSignal('');
  const [busy, setBusy] = createSignal(false);
  const [error, setError] = createSignal<string | null>(null);

  const submit = async (e: SubmitEvent) => {
    e.preventDefault();
    if (!value().trim()) return;
    setBusy(true);
    setError(null);
    try {
      await props.onSubmit(value().trim());
      setValue('');
      props.onClose();
    } catch (err) {
      setError(String(err));
    } finally {
      setBusy(false);
    }
  };

  return (
    <Show when={props.open}>
      <div class="fixed inset-0 bg-black/60 grid place-items-center z-50" onClick={props.onClose}>
        <form
          class="w-[560px] rounded-lg bg-zinc-900 border border-zinc-800 p-4 flex flex-col gap-3"
          onClick={(e) => e.stopPropagation()}
          onSubmit={submit}
        >
          <h2 class="text-lg font-semibold">Add Magnet</h2>
          <textarea
            class="bg-zinc-950 border border-zinc-800 rounded p-2 font-mono text-sm h-24"
            placeholder="magnet:?xt=urn:btih:..."
            value={value()}
            onInput={(e) => setValue(e.currentTarget.value)}
            autofocus
            disabled={busy()}
          />
          <Show when={error()}>
            <div class="text-sm text-red-400">{error()}</div>
          </Show>
          <div class="flex justify-end gap-2">
            <button type="button" class="px-3 py-1.5 rounded border border-zinc-700" onClick={props.onClose}>
              Cancel
            </button>
            <button
              type="submit"
              class="px-3 py-1.5 rounded bg-blue-600 disabled:opacity-50"
              disabled={busy() || !value().trim()}
            >
              {busy() ? 'Adding...' : 'Add'}
            </button>
          </div>
        </form>
      </div>
    </Show>
  );
}
