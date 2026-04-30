import {Dialog} from '@kobalte/core/dialog';
import {RadioGroup} from '@kobalte/core/radio-group';
import {ChevronDown, FileDown, FolderOpen, Magnet, X} from 'lucide-solid';
import {createEffect, createSignal, For, Show} from 'solid-js';
import {Button} from '../ui/Button';
import type {CategoryDTO, TagDTO} from '../../lib/bindings';
import {isWailsRuntime} from '../../lib/runtime';

type Source = 'magnet' | 'file';

type Props = {
  open: boolean;
  initialSource?: Source;
  defaultSavePath: string;
  categories: CategoryDTO[];
  tags: TagDTO[];
  onClose: () => void;
  onSubmitMagnet: (magnet: string, savePath: string, categoryID: number | null, tagIDs: number[]) => Promise<void>;
  onPickAndAddTorrent: (savePath: string, categoryID: number | null, tagIDs: number[]) => Promise<void>;
  onAddTorrentBytes: (bytes: Uint8Array, savePath: string, categoryID: number | null, tagIDs: number[]) => Promise<void>;
};

const sectionClass = 'rounded-lg border border-white/[.06] bg-white/[.01] p-3';
const labelClass = 'text-[10px] font-semibold uppercase tracking-wider text-zinc-500';
const inputClass = 'w-full rounded-md border border-white/[.06] bg-black/30 px-3 py-1.5 text-sm text-zinc-200 placeholder:text-zinc-600 focus:border-accent-500/50 focus:outline-none focus:ring-1 focus:ring-accent-500/30';

export function AddTorrentModal(props: Props) {
  const [source, setSource] = createSignal<Source>(props.initialSource ?? 'magnet');
  const [magnet, setMagnet] = createSignal('');
  const [savePath, setSavePath] = createSignal(props.defaultSavePath);
  const [categoryID, setCategoryID] = createSignal<number | null>(null);
  const [selectedTags, setSelectedTags] = createSignal<Set<number>>(new Set());
  const [busy, setBusy] = createSignal(false);
  const [error, setError] = createSignal<string | null>(null);
  const [pickedFile, setPickedFile] = createSignal<File | null>(null);
  const isWails = isWailsRuntime();

  // Reset state whenever the modal opens — especially `source` if initialSource changes.
  createEffect(() => {
    if (props.open) {
      setSource(props.initialSource ?? 'magnet');
      setMagnet('');
      setSavePath(props.defaultSavePath);
      setCategoryID(null);
      setSelectedTags(new Set<number>());
      setError(null);
      setPickedFile(null);
    }
  });

  const toggleTag = (id: number) => {
    setSelectedTags((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id); else next.add(id);
      return next;
    });
  };

  const submit = async (e: SubmitEvent) => {
    e.preventDefault();
    if (busy()) return;
    setBusy(true);
    setError(null);
    try {
      const tagIDs = [...selectedTags()];
      if (source() === 'magnet') {
        if (!magnet().trim()) { setError('Magnet link required'); setBusy(false); return; }
        await props.onSubmitMagnet(magnet().trim(), savePath().trim(), categoryID(), tagIDs);
      } else if (isWails) {
        await props.onPickAndAddTorrent(savePath().trim(), categoryID(), tagIDs);
      } else {
        const file = pickedFile();
        if (!file) { setError('Choose a .torrent file first'); setBusy(false); return; }
        const buf = new Uint8Array(await file.arrayBuffer());
        await props.onAddTorrentBytes(buf, savePath().trim(), categoryID(), tagIDs);
      }
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
          <Dialog.Content class="w-full max-w-xl rounded-xl border border-white/10 bg-zinc-900/95 backdrop-blur-xl shadow-2xl animate-in fade-in zoom-in-95">
            <form onSubmit={submit} class="flex flex-col gap-3 p-5">
              <div class="flex items-center justify-between">
                <Dialog.Title class="inline-flex items-center gap-2 text-base font-semibold text-zinc-100">
                  <FileDown class="h-4 w-4 text-accent-500" />
                  Add torrent
                </Dialog.Title>
                <Dialog.CloseButton class="grid h-7 w-7 place-items-center rounded-md text-zinc-500 hover:bg-white/[.06] hover:text-zinc-100">
                  <X class="h-4 w-4" />
                </Dialog.CloseButton>
              </div>

              <section class={sectionClass}>
                <div class={`${labelClass} mb-2`}>Source</div>
                <RadioGroup value={source()} onChange={(v) => setSource(v as Source)} class="flex gap-2">
                  <RadioGroup.Item value="magnet" class="flex-1">
                    <RadioGroup.ItemInput class="sr-only" />
                    <RadioGroup.ItemControl class="hidden" />
                    <RadioGroup.ItemLabel class="flex cursor-pointer items-center gap-2 rounded-md border border-white/[.06] bg-white/[.02] px-3 py-2 text-sm text-zinc-300 transition-colors hover:bg-white/[.04] data-[checked]:border-accent-500/50 data-[checked]:bg-accent-500/[.10] data-[checked]:text-accent-200">
                      <Magnet class="h-3.5 w-3.5" />
                      Magnet link
                    </RadioGroup.ItemLabel>
                  </RadioGroup.Item>
                  <RadioGroup.Item value="file" class="flex-1">
                    <RadioGroup.ItemInput class="sr-only" />
                    <RadioGroup.ItemControl class="hidden" />
                    <RadioGroup.ItemLabel class="flex cursor-pointer items-center gap-2 rounded-md border border-white/[.06] bg-white/[.02] px-3 py-2 text-sm text-zinc-300 transition-colors hover:bg-white/[.04] data-[checked]:border-accent-500/50 data-[checked]:bg-accent-500/[.10] data-[checked]:text-accent-200">
                      <FileDown class="h-3.5 w-3.5" />
                      Torrent file
                    </RadioGroup.ItemLabel>
                  </RadioGroup.Item>
                </RadioGroup>

                <Show when={source() === 'magnet'}>
                  <textarea
                    class="mt-3 h-24 w-full resize-none rounded-md border border-white/[.06] bg-black/30 p-3 font-mono text-xs text-zinc-200 placeholder:text-zinc-600 focus:border-accent-500/50 focus:outline-none focus:ring-1 focus:ring-accent-500/30"
                    placeholder="magnet:?xt=urn:btih:..."
                    value={magnet()}
                    onInput={(e) => setMagnet(e.currentTarget.value)}
                    disabled={busy()}
                  />
                </Show>
                <Show when={source() === 'file' && isWails}>
                  <p class="mt-3 text-xs text-zinc-500">
                    Click <span class="text-zinc-300">Choose file…</span> below to open the file picker. The torrent will be added with the save target and options below.
                  </p>
                </Show>
                <Show when={source() === 'file' && !isWails}>
                  <input
                    type="file"
                    accept=".torrent"
                    class="mt-3 block w-full text-xs text-zinc-300 file:mr-3 file:rounded-md file:border-0 file:bg-white/[.06] file:px-3 file:py-1.5 file:text-xs file:text-zinc-100 hover:file:bg-white/[.10]"
                    onChange={(e) => {
                      const f = e.currentTarget.files?.[0] ?? null;
                      setPickedFile(f);
                    }}
                    disabled={busy()}
                    data-testid="torrent-file-input"
                  />
                  <Show when={pickedFile()}>
                    <p class="mt-1 text-xs text-zinc-500">{pickedFile()!.name}</p>
                  </Show>
                </Show>
              </section>

              <section class={sectionClass}>
                <label class={`${labelClass} mb-1.5 block`}>Save target</label>
                <div class="flex items-center gap-2">
                  <FolderOpen class="h-3.5 w-3.5 text-zinc-500" />
                  <input
                    type="text"
                    class={inputClass}
                    value={savePath()}
                    onInput={(e) => setSavePath(e.currentTarget.value)}
                    placeholder="/Users/me/Downloads"
                    disabled={busy()}
                  />
                </div>
              </section>

              <section class={sectionClass}>
                <div class={`${labelClass} mb-2`}>Files & options</div>
                <div class="flex flex-col gap-3">
                  <div>
                    <label class="mb-1 block text-xs text-zinc-400">Category</label>
                    <div class="relative">
                      <select
                        class={`${inputClass} appearance-none pr-8`}
                        value={categoryID() ?? ''}
                        onChange={(e) => {
                          const v = e.currentTarget.value;
                          setCategoryID(v === '' ? null : Number(v));
                        }}
                        disabled={busy()}
                      >
                        <option value="">None</option>
                        <For each={props.categories}>
                          {(cat) => <option value={cat.id}>{cat.name}</option>}
                        </For>
                      </select>
                      <ChevronDown class="pointer-events-none absolute right-2 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-zinc-500" />
                    </div>
                  </div>

                  <Show when={props.tags.length > 0}>
                    <div>
                      <label class="mb-1 block text-xs text-zinc-400">Tags</label>
                      <div class="flex flex-wrap gap-1.5">
                        <For each={props.tags}>
                          {(tg) => (
                            <button
                              type="button"
                              onClick={() => toggleTag(tg.id)}
                              class="inline-flex items-center gap-1.5 rounded-full border border-white/[.06] bg-white/[.02] px-2 py-0.5 text-xs text-zinc-300 transition-colors hover:bg-white/[.04]"
                              classList={{'border-accent-500/50 bg-accent-500/[.10] text-accent-200': selectedTags().has(tg.id)}}
                            >
                              <span class="h-1.5 w-1.5 rounded-full" style={{background: tg.color}} />
                              {tg.name}
                            </button>
                          )}
                        </For>
                      </div>
                    </div>
                  </Show>

                </div>
              </section>

              <Show when={error()}>
                <div class="rounded-md border border-rose-500/20 bg-rose-500/10 px-3 py-2 text-sm text-rose-300">{error()}</div>
              </Show>

              <div class="flex justify-end gap-2">
                <Button type="button" variant="ghost" onClick={props.onClose}>Cancel</Button>
                <Button
                  type="submit"
                  variant="primary"
                  disabled={
                    busy() ||
                    (source() === 'magnet' && !magnet().trim()) ||
                    (source() === 'file' && !isWails && !pickedFile())
                  }
                >
                  {busy()
                    ? 'Adding…'
                    : source() === 'file'
                      ? (isWails ? 'Choose file…' : 'Add')
                      : 'Add'}
                </Button>
              </div>
            </form>
          </Dialog.Content>
        </div>
      </Dialog.Portal>
    </Dialog>
  );
}
