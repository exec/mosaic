import {Dialog} from '@kobalte/core/dialog';
import {Magnet, X} from 'lucide-solid';
import {createSignal, Show} from 'solid-js';
import {Button} from '../ui/Button';

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
    <Dialog open={props.open} onOpenChange={(o) => { if (!o) props.onClose(); }}>
      <Dialog.Portal>
        <Dialog.Overlay class="fixed inset-0 z-40 bg-black/60 backdrop-blur-sm animate-in fade-in" />
        <div class="fixed inset-0 z-50 grid place-items-center p-4">
          <Dialog.Content class="w-full max-w-lg rounded-xl border border-white/10 bg-zinc-900/95 backdrop-blur-xl shadow-2xl animate-in fade-in zoom-in-95">
            <form onSubmit={submit} class="flex flex-col gap-4 p-5">
              <div class="flex items-center justify-between">
                <Dialog.Title class="inline-flex items-center gap-2 text-base font-semibold text-zinc-100">
                  <Magnet class="h-4 w-4 text-accent-500" />
                  Add magnet link
                </Dialog.Title>
                <Dialog.CloseButton class="grid h-7 w-7 place-items-center rounded-md text-zinc-500 hover:bg-white/[.06] hover:text-zinc-100">
                  <X class="h-4 w-4" />
                </Dialog.CloseButton>
              </div>
              <textarea
                class="h-28 resize-none rounded-md border border-white/[.06] bg-black/30 p-3 font-mono text-xs text-zinc-200 placeholder:text-zinc-600 focus:border-accent-500/50 focus:outline-none focus:ring-1 focus:ring-accent-500/30"
                placeholder="magnet:?xt=urn:btih:..."
                value={value()}
                onInput={(e) => setValue(e.currentTarget.value)}
                autofocus
                disabled={busy()}
              />
              <Show when={error()}>
                <div class="rounded-md border border-rose-500/20 bg-rose-500/10 px-3 py-2 text-sm text-rose-300">{error()}</div>
              </Show>
              <div class="flex justify-end gap-2">
                <Button type="button" variant="ghost" onClick={props.onClose}>Cancel</Button>
                <Button type="submit" variant="primary" disabled={busy() || !value().trim()}>
                  {busy() ? 'Adding…' : 'Add'}
                </Button>
              </div>
            </form>
          </Dialog.Content>
        </div>
      </Dialog.Portal>
    </Dialog>
  );
}
