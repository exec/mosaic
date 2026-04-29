import {createSignal, For, Show} from 'solid-js';
import {Plus, Trash2, Pencil, Check, X} from 'lucide-solid';
import {toast} from 'solid-sonner';
import type {CategoryDTO} from '../../lib/bindings';
import {Button} from '../ui/Button';
import {ColorPicker} from './ColorPicker';

type Props = {
  categories: CategoryDTO[];
  onCreate: (name: string, savePath: string, color: string) => Promise<void>;
  onUpdate: (id: number, name: string, savePath: string, color: string) => Promise<void>;
  onDelete: (id: number) => Promise<void>;
};

export function CategoriesPane(props: Props) {
  const [creating, setCreating] = createSignal(false);
  const [editingID, setEditingID] = createSignal<number | null>(null);

  return (
    <div class="mx-auto max-w-3xl px-6 py-6">
      <div class="mb-4 flex items-center justify-between border-b border-white/[.04] pb-3">
        <div>
          <h2 class="text-lg font-semibold text-zinc-100">Categories</h2>
          <p class="mt-0.5 text-sm text-zinc-500">Organize torrents into groups with optional save-path defaults.</p>
        </div>
        <Button variant="primary" onClick={() => setCreating(true)}>
          <Plus class="h-3.5 w-3.5" />
          New
        </Button>
      </div>

      <Show when={creating()}>
        <CategoryForm
          initial={{id: 0, name: '', default_save_path: '', color: '#71717a'}}
          onCancel={() => setCreating(false)}
          onSubmit={async (cat) => {
            try {
              await props.onCreate(cat.name, cat.default_save_path, cat.color);
              setCreating(false);
              toast.success('Category created');
            } catch (e) { toast.error(String(e)); }
          }}
        />
      </Show>

      <Show when={!creating() && props.categories.length === 0}>
        <p class="py-6 text-center text-sm text-zinc-500">No categories yet. Click <kbd>New</kbd> to add one.</p>
      </Show>

      <ul class="flex flex-col gap-px">
        <For each={props.categories}>
          {(cat) => (
            <li class="border-b border-white/[.03]">
              <Show
                when={editingID() === cat.id}
                fallback={
                  <div class="flex items-center justify-between py-2.5 px-2 hover:bg-white/[.02]">
                    <div class="flex items-center gap-3">
                      <span class="h-2.5 w-2.5 rounded-full" style={{background: cat.color}} />
                      <span class="text-sm text-zinc-100">{cat.name}</span>
                      <Show when={cat.default_save_path}>
                        <span class="font-mono text-xs text-zinc-500">{cat.default_save_path}</span>
                      </Show>
                    </div>
                    <div class="flex gap-1">
                      <button class="grid h-7 w-7 place-items-center rounded text-zinc-500 hover:bg-white/[.04] hover:text-zinc-100" onClick={() => setEditingID(cat.id)} title="Edit">
                        <Pencil class="h-3 w-3" />
                      </button>
                      <button
                        class="grid h-7 w-7 place-items-center rounded text-zinc-500 hover:bg-rose-500/20 hover:text-rose-300"
                        onClick={async () => {
                          if (!confirm(`Delete category "${cat.name}"? Torrents in this category will be uncategorized.`)) return;
                          try {
                            await props.onDelete(cat.id);
                            toast.success('Category deleted');
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
                <CategoryForm
                  initial={cat}
                  onCancel={() => setEditingID(null)}
                  onSubmit={async (next) => {
                    try {
                      await props.onUpdate(next.id, next.name, next.default_save_path, next.color);
                      setEditingID(null);
                      toast.success('Category updated');
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

function CategoryForm(props: {
  initial: CategoryDTO;
  onCancel: () => void;
  onSubmit: (cat: CategoryDTO) => Promise<void>;
}) {
  const [name, setName] = createSignal(props.initial.name);
  const [savePath, setSavePath] = createSignal(props.initial.default_save_path);
  const [color, setColor] = createSignal(props.initial.color);
  return (
    <form
      class="flex flex-col gap-2 rounded-md border border-white/[.06] bg-white/[.02] p-3 my-2"
      onSubmit={async (e) => {
        e.preventDefault();
        if (!name().trim()) return;
        await props.onSubmit({id: props.initial.id, name: name().trim(), default_save_path: savePath(), color: color()});
      }}
    >
      <div class="grid grid-cols-[80px_1fr] items-center gap-2">
        <label class="text-xs text-zinc-500">Name</label>
        <input class="rounded border border-white/[.06] bg-black/30 px-2 py-1 text-sm text-zinc-100 focus:border-accent-500/50 focus:outline-none" value={name()} onInput={(e) => setName(e.currentTarget.value)} autofocus />
      </div>
      <div class="grid grid-cols-[80px_1fr] items-center gap-2">
        <label class="text-xs text-zinc-500">Save path</label>
        <input class="rounded border border-white/[.06] bg-black/30 px-2 py-1 font-mono text-xs text-zinc-100 focus:border-accent-500/50 focus:outline-none" value={savePath()} onInput={(e) => setSavePath(e.currentTarget.value)} placeholder="Optional default path for new torrents" />
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
