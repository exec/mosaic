import {Dialog} from '@kobalte/core/dialog';
import {RadioGroup} from '@kobalte/core/radio-group';
import {ChevronDown, FileDown, FolderOpen, Magnet, X} from 'lucide-solid';
import {createEffect, createMemo, createSignal, For, on, onCleanup, Show} from 'solid-js';
import {Button} from '../ui/Button';
import {api} from '../../lib/bindings';
import {FilesTab} from '../inspector/FilesTab';
import {userErr} from '../../lib/errors';
import type {CategoryDTO, TagDTO, DetailDTO} from '../../lib/bindings';
import {isWailsRuntime} from '../../lib/runtime';

type Source = 'magnet' | 'file';

type Props = {
  open: boolean;
  initialSource?: Source;
  defaultSavePath: string;
  categories: CategoryDTO[];
  tags: TagDTO[];
  onClose: () => void;
  initialMagnet?: string;
  initialBytes?: Uint8Array;
  initialFilename?: string;
  onAdded: (id: string, categoryID: number | null, tagIDs: number[]) => Promise<void>;
};

const sectionClass = 'rounded-lg border border-white/[.06] bg-white/[.01] p-3';
const labelClass = 'text-[10px] font-semibold uppercase tracking-wider text-zinc-500';
const inputClass = 'w-full rounded-md border border-white/[.06] bg-black/30 px-3 py-1.5 text-sm text-zinc-200 placeholder:text-zinc-600 focus:border-accent-500/60 focus:outline-none focus:ring-2 focus:ring-accent-500/40';

export function AddTorrentModal(props: Props) {
  const [preparedID, setPreparedID] = createSignal('');
  const [detail, setDetail] = createSignal<DetailDTO | null>(null);
  const [draft, setDraft] = createSignal<Record<number, 'skip' | 'normal' | 'high' | 'max'>>({});
  const [startPaused, setStartPaused] = createSignal(false);
  const preview = createMemo(() => {const d = detail();return d ? {...d, files: d.files?.map((f) => ({...f, priority: draft()[f.index] ?? f.priority}))} : null;});
  const ready = () => !!detail()?.metadata_ready && !detail()?.verifying && !detail()?.error_active;

  const [source, setSource] = createSignal<Source>(props.initialSource ?? 'magnet');
  const [magnet, setMagnet] = createSignal('');
  const [savePath, setSavePath] = createSignal(props.defaultSavePath);
  const [pathEdited, setPathEdited] = createSignal(false);
  const [categoryID, setCategoryID] = createSignal<number | null>(null);
  const [selectedTags, setSelectedTags] = createSignal<Set<number>>(new Set());
  const [busy, setBusy] = createSignal(false);
  const [error, setError] = createSignal<string | null>(null);
  const [pickedFile, setPickedFile] = createSignal<File | null>(null);
  const isWails = isWailsRuntime();

  // Reset state when the modal opens — especially `source` if initialSource
  // changes. Keyed on `open` only via on(): a bare createEffect also tracked
  // initialSource and defaultSavePath, so a late-arriving default save path
  // (boot fetch resolving after the user opened the modal) wiped whatever
  // they had already typed.
  createEffect(on(() => props.open, (open) => {
    if (open) {
      setSource(props.initialSource ?? 'magnet');
      setMagnet(props.initialMagnet ?? '');
      setPreparedID('');setDetail(null);setDraft({});setStartPaused(false);
      setSavePath(props.defaultSavePath);
      setPathEdited(false);
      setCategoryID(null);
      setSelectedTags(new Set<number>());
      setError(null);
      setPickedFile(null);
    }
  }));

  const toggleTag = (id: number) => {
    setSelectedTags((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id); else next.add(id);
      return next;
    });
  };

  createEffect(on(() => preparedID(), (id) => {
    if (!id) return;
    let pending = false;
    const poll = async () => {
      if (pending) return;pending = true;
      try {const d = await api.getTorrentDetail(id);if (preparedID() === id) setDetail(d);}
      catch(e) {if (preparedID() === id) setError(userErr(e));}
      finally {pending = false;}
    };
    void poll(); const timer = setInterval(poll, 1000);onCleanup(() => clearInterval(timer));
  }));
  const close = async () => {
    if (busy()) return;
    setBusy(true);setError(null);
    try {if (preparedID()) await api.remove(preparedID(), false);setPreparedID('');props.onClose();}
    catch(e) {setError(`Preview remains paused. Couldn't remove it: ${userErr(e)}`);}
    finally {setBusy(false);}
  };
  const submit = async (e: SubmitEvent) => {
    e.preventDefault();if (busy()) return;setBusy(true);setError(null);
    try {
      if (preparedID()) {
        if (!ready() && !startPaused()) return;
        const paused = startPaused(), category = categoryID(), tags = [...selectedTags()];
        const files = preview()?.files ?? [];
        if (ready()) await api.setFilePriorities(preparedID(), Object.fromEntries(files.map((f) => [f.index, f.priority])));
        if (!paused) await api.resume(preparedID());
        const id = preparedID();setPreparedID('');
        await props.onAdded(id, category, tags);props.onClose();
      } else {
        let blob = '';
        if (source() === 'file') {
          if (props.initialBytes) blob = encodeTorrent(props.initialBytes);
          else if (isWails) {blob = await api.pickTorrentFile();if (!blob) return;}
          else {const file = pickedFile();if (!file) throw new Error('Choose a .torrent file first');blob = encodeTorrent(new Uint8Array(await file.arrayBuffer()));}
        }
        const id = await api.prepareTorrent({magnet: source() === 'magnet' ? magnet().trim() : '', torrent_file: blob, save_path: savePath().trim()});
        setPreparedID(id);
      }
    } catch (err) {setError(userErr(err));}
    finally {setBusy(false);}
  };

  return (
    <Dialog open={props.open} onOpenChange={(o) => { if (!o) void close(); }}>
      <Dialog.Portal>
        <Dialog.Overlay class="fixed inset-0 z-40 bg-black/60 backdrop-blur-sm animate-in fade-in" />
        <div class="fixed inset-0 z-50 grid place-items-center p-4">
          <Dialog.Content class="max-h-[90vh] w-full max-w-xl overflow-y-auto rounded-xl border border-white/10 bg-zinc-900/95 backdrop-blur-xl shadow-2xl modal-in">
            <form onSubmit={submit} class="flex flex-col gap-3 p-5">
              <div class="flex items-center justify-between">
                <Dialog.Title class="inline-flex items-center gap-2 text-base font-semibold text-zinc-100">
                  <FileDown class="h-4 w-4 text-accent-500" />
                  Add torrent
                </Dialog.Title>
                <button type="button" disabled={busy()} onClick={close} aria-label="Close add torrent" class="grid h-7 w-7 place-items-center rounded-md text-zinc-500 hover:bg-white/[.06] hover:text-zinc-100">
                  <X class="h-4 w-4" />
                </button>
              </div>

              <Show when={!preparedID()}>
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
                    aria-label="Magnet link"
                    class="mt-3 h-24 w-full resize-none rounded-md border border-white/[.06] bg-black/30 p-3 font-mono text-xs text-zinc-200 placeholder:text-zinc-600 focus:border-accent-500/60 focus:outline-none focus:ring-2 focus:ring-accent-500/40"
                    placeholder="magnet:?xt=urn:btih:..."
                    value={magnet()}
                    onInput={(e) => setMagnet(e.currentTarget.value)}
                    disabled={busy()}
                  />
                </Show>
                <Show when={source() === 'file' && props.initialBytes}><p class="mt-3 break-all text-xs text-zinc-400">{props.initialFilename || 'Dropped torrent file'} · ready to review</p></Show>
                <Show when={source() === 'file' && isWails && !props.initialBytes}>
                  <p class="mt-3 text-xs text-zinc-500">
                    Click <span class="text-zinc-300">Choose file…</span> below to open the file picker. You can review and select files before starting any data transfer.
                  </p>
                </Show>
                <Show when={source() === 'file' && !isWails && !props.initialBytes}>
                  <input
                    type="file"
                    aria-label="Torrent file"
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
                <label class={`${labelClass} mb-1.5 block`}>Save path</label>
                <div class="flex items-center gap-2">
                  <FolderOpen class="h-3.5 w-3.5 text-zinc-500" />
                  <input
                    type="text"
                    aria-label="Save path"
                    class={inputClass}
                    value={savePath()}
                    onInput={(e) => { setSavePath(e.currentTarget.value); setPathEdited(true); }}
                    placeholder="/Users/me/Downloads"
                    disabled={busy()}
                  />
                </div>
              </section>

              </Show>
              <Show when={preparedID()}>
                <section class={sectionClass}>
                  <div class={labelClass}>Choose files</div>
                  <p class="mt-2 break-all text-xs text-zinc-500">Save to <span class="text-zinc-300">{savePath()}</span></p>
                  <Show when={!ready()}><p class="mt-2 text-xs leading-relaxed text-zinc-400">{detail()?.error_active ? detail()?.last_error : detail()?.metadata_ready ? 'Checking existing files…' : 'Finding metadata from peers…'} No payload data will download until you confirm.</p></Show>
                  <div class="mt-2 max-h-64 overflow-auto"><FilesTab preview detail={preview()} canEdit={ready() && !busy()} onSetPriority={(index, priority) => setDraft((p) => ({...p, [index]: priority}))} onSetPriorities={(prios) => setDraft((p) => ({...p, ...prios}))} /></div>
                  <label class="mt-3 flex items-center gap-2 text-xs text-zinc-300"><input type="checkbox" disabled={busy()} checked={startPaused()} onChange={(e) => setStartPaused(e.currentTarget.checked)} class="accent-accent-500" />Start paused</label>
                  <p class="mt-2 text-[10px] text-zinc-500">BitTorrent pieces can span adjacent files; a few bytes of skipped files may be needed.</p>
                </section>
              </Show>
              <details class={sectionClass} open={!preparedID()}>
                <summary class={`${labelClass} cursor-pointer`}>Organization <span class="ml-2 normal-case font-normal tracking-normal text-zinc-400">{props.categories.find((c) => c.id === categoryID())?.name ?? 'None'}{selectedTags().size ? ` · ${selectedTags().size} tags` : ''}</span></summary>
                <div class="mt-3 flex flex-col gap-3">
                  <div>
                    <label class="mb-1 block text-xs text-zinc-400">Category</label>
                    <div class="relative">
                      <select
                        aria-label="Category"
                        class={`${inputClass} appearance-none pr-8`}
                        value={categoryID() ?? ''}
                        onChange={(e) => {
                          const v = e.currentTarget.value;
                          const id = v === '' ? null : Number(v);
                          setCategoryID(id);
                          if (!preparedID() && !pathEdited()) {
                            setSavePath(props.categories.find((c) => c.id === id)?.default_save_path || props.defaultSavePath);
                          }
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
                              disabled={busy()}
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
              </details>

              <Show when={error()}>
                <div class="rounded-md border border-rose-500/20 bg-rose-500/10 px-3 py-2 text-sm text-rose-300">{error()}</div>
              </Show>

              <div class="flex justify-end gap-2">
                <Show when={preparedID() && error()}><Button type="button" variant="ghost" disabled={busy()} onClick={() => {setPreparedID('');props.onClose();}}>Keep paused & close</Button></Show>
                <Button type="button" variant="ghost" disabled={busy()} onClick={close}>Cancel</Button>
                <Button
                  type="submit"
                  variant="primary"
                  disabled={busy() || (preparedID() ? !startPaused() && (!ready() || !preview()?.files?.some((f) => f.priority !== 'skip')) : source() === 'magnet' ? !magnet().trim() : !isWails && !pickedFile() && !props.initialBytes)}
                >
                  {busy() ? 'Working…' : preparedID() ? (startPaused() ? 'Add paused' : 'Start download') : source() === 'file' && isWails && !props.initialBytes ? 'Choose file & review…' : 'Review files'}
                </Button>
              </div>
            </form>
          </Dialog.Content>
        </div>
      </Dialog.Portal>
    </Dialog>
  );
}

function encodeTorrent(bytes: Uint8Array): string {
  let binary = '';for (let i = 0; i < bytes.length; i += 32768) binary += String.fromCharCode(...bytes.subarray(i, i + 32768));return btoa(binary);
}
