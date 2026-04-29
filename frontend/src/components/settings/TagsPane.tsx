import {createSignal, For, Show} from 'solid-js';
import {Plus, Trash2, Check, X} from 'lucide-solid';
import {toast} from 'solid-sonner';
import type {TagDTO} from '../../lib/bindings';
import {Button} from '../ui/Button';
import {ColorPicker} from './ColorPicker';

type Props = {
  tags: TagDTO[];
  onCreate: (name: string, color: string) => Promise<void>;
  onDelete: (id: number) => Promise<void>;
};

export function TagsPane(props: Props) {
  const [creating, setCreating] = createSignal(false);

  return (
    <div class="mx-auto max-w-3xl px-6 py-6">
      <div class="mb-4 flex items-center justify-between border-b border-white/[.04] pb-3">
        <div>
          <h2 class="text-lg font-semibold text-zinc-100">Tags</h2>
          <p class="mt-0.5 text-sm text-zinc-500">Lightweight labels you can stack on a torrent.</p>
        </div>
        <Button variant="primary" onClick={() => setCreating(true)}>
          <Plus class="h-3.5 w-3.5" />
          New
        </Button>
      </div>

      <Show when={creating()}>
        <TagForm
          onCancel={() => setCreating(false)}
          onSubmit={async (name, color) => {
            try {
              await props.onCreate(name, color);
              setCreating(false);
              toast.success('Tag created');
            } catch (e) { toast.error(String(e)); }
          }}
        />
      </Show>

      <Show when={!creating() && props.tags.length === 0}>
        <p class="py-6 text-center text-sm text-zinc-500">No tags yet. Click <kbd>New</kbd> to add one.</p>
      </Show>

      <ul class="flex flex-col gap-px">
        <For each={props.tags}>
          {(tag) => (
            <li class="flex items-center justify-between border-b border-white/[.03] py-2.5 px-2 hover:bg-white/[.02]">
              <div class="flex items-center gap-3">
                <span class="h-2.5 w-2.5 rounded-full" style={{background: tag.color}} />
                <span class="text-sm text-zinc-100">{tag.name}</span>
              </div>
              <button
                class="grid h-7 w-7 place-items-center rounded text-zinc-500 hover:bg-rose-500/20 hover:text-rose-300"
                onClick={async () => {
                  if (!confirm(`Delete tag "${tag.name}"?`)) return;
                  try {
                    await props.onDelete(tag.id);
                    toast.success('Tag deleted');
                  } catch (e) { toast.error(String(e)); }
                }}
                title="Delete"
              >
                <Trash2 class="h-3 w-3" />
              </button>
            </li>
          )}
        </For>
      </ul>
    </div>
  );
}

function TagForm(props: {
  onCancel: () => void;
  onSubmit: (name: string, color: string) => Promise<void>;
}) {
  const [name, setName] = createSignal('');
  const [color, setColor] = createSignal('#71717a');
  return (
    <form
      class="flex flex-col gap-2 rounded-md border border-white/[.06] bg-white/[.02] p-3 my-2"
      onSubmit={async (e) => {
        e.preventDefault();
        if (!name().trim()) return;
        await props.onSubmit(name().trim(), color());
      }}
    >
      <div class="grid grid-cols-[80px_1fr] items-center gap-2">
        <label class="text-xs text-zinc-500">Name</label>
        <input class="rounded border border-white/[.06] bg-black/30 px-2 py-1 text-sm text-zinc-100 focus:border-accent-500/50 focus:outline-none" value={name()} onInput={(e) => setName(e.currentTarget.value)} autofocus />
      </div>
      <div class="grid grid-cols-[80px_1fr] items-center gap-2">
        <label class="text-xs text-zinc-500">Color</label>
        <ColorPicker value={color()} onSelect={setColor} />
      </div>
      <div class="flex justify-end gap-2 mt-1">
        <Button type="button" variant="ghost" onClick={props.onCancel}>
          <X class="h-3.5 w-3.5" />
          Cancel
        </Button>
        <Button type="submit" variant="primary" disabled={!name().trim()}>
          <Check class="h-3.5 w-3.5" />
          Save
        </Button>
      </div>
    </form>
  );
}
