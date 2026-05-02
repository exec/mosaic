import {createSignal, For, Show} from 'solid-js';
import {Plus, Trash2, Pencil, Check, X, ChevronDown, ChevronRight, RefreshCw} from 'lucide-solid';
import {toast} from 'solid-sonner';
import type {CategoryDTO, FeedDTO, FilterDTO} from '../../lib/bindings';
import {Button} from '../ui/Button';

type Props = {
  feeds: FeedDTO[];
  filtersByFeed: Record<number, FilterDTO[]>;
  categories: CategoryDTO[];
  onCreateFeed: (f: FeedDTO) => Promise<void>;
  onUpdateFeed: (f: FeedDTO) => Promise<void>;
  onDeleteFeed: (id: number) => Promise<void>;
  onPollFeed: (id: number) => Promise<void>;
  onLoadFilters: (feedID: number) => Promise<void>;
  onCreateFilter: (f: FilterDTO) => Promise<void>;
  onUpdateFilter: (f: FilterDTO) => Promise<void>;
  onDeleteFilter: (feedID: number, id: number) => Promise<void>;
};

function fmtLastPolled(unix: number): string {
  if (!unix) return 'Never';
  const d = new Date(unix * 1000);
  const now = Date.now();
  const diffSec = Math.floor((now - d.getTime()) / 1000);
  if (diffSec < 60) return 'Just now';
  if (diffSec < 3600) return `${Math.floor(diffSec / 60)}m ago`;
  if (diffSec < 86400) return `${Math.floor(diffSec / 3600)}h ago`;
  return d.toLocaleDateString();
}

export function RSSPane(props: Props) {
  const [creating, setCreating] = createSignal(false);
  const [editingFeedID, setEditingFeedID] = createSignal<number | null>(null);
  const [expanded, setExpanded] = createSignal<Set<number>>(new Set());
  const [creatingFilterFor, setCreatingFilterFor] = createSignal<number | null>(null);
  const [pollingFeedID, setPollingFeedID] = createSignal<number | null>(null);
  const [editingFilterID, setEditingFilterID] = createSignal<number | null>(null);

  const toggleExpand = async (feedID: number) => {
    const next = new Set(expanded());
    if (next.has(feedID)) {
      next.delete(feedID);
    } else {
      next.add(feedID);
      try { await props.onLoadFilters(feedID); } catch (e) { toast.error(String(e)); }
    }
    setExpanded(next);
  };

  return (
    <div class="mx-auto max-w-3xl px-6 py-6">
      <div class="mb-4 flex items-center justify-between border-b border-white/[.04] pb-3">
        <div>
          <h2 class="text-lg font-semibold text-zinc-100">RSS</h2>
          <p class="mt-0.5 text-sm text-zinc-500">Subscribe to feeds and auto-add torrents matching regex filters.</p>
        </div>
        <Button variant="primary" onClick={() => setCreating(true)}>
          <Plus class="h-3.5 w-3.5" />
          New feed
        </Button>
      </div>

      <Show when={creating()}>
        <FeedForm
          initial={{id: 0, url: '', name: '', interval_min: 30, last_polled: 0, etag: '', enabled: true}}
          onCancel={() => setCreating(false)}
          onSubmit={async (f) => {
            try {
              await props.onCreateFeed(f);
              setCreating(false);
              toast.success('Feed added');
            } catch (e) { toast.error(String(e)); }
          }}
        />
      </Show>

      <Show when={!creating() && props.feeds.length === 0}>
        <p class="py-6 text-center text-sm text-zinc-500">No feeds yet. Click <kbd>New feed</kbd> to subscribe to one.</p>
      </Show>

      <ul class="flex flex-col gap-px">
        <For each={props.feeds}>
          {(feed) => (
            <li class="border-b border-white/[.03]">
              <Show
                when={editingFeedID() === feed.id}
                fallback={
                  <div class="flex flex-col">
                    <div class="flex items-center justify-between py-2.5 px-2 hover:bg-white/[.02]">
                      <button
                        type="button"
                        class="flex items-center gap-2 text-left flex-1"
                        onClick={() => toggleExpand(feed.id)}
                      >
                        <Show when={expanded().has(feed.id)} fallback={<ChevronRight class="h-3 w-3 text-zinc-500" />}>
                          <ChevronDown class="h-3 w-3 text-zinc-500" />
                        </Show>
                        <span
                          class="h-2 w-2 rounded-full"
                          classList={{'bg-seed': feed.enabled, 'bg-zinc-600': !feed.enabled}}
                          title={feed.enabled ? 'Enabled' : 'Disabled'}
                        />
                        <span class="text-sm text-zinc-100">{feed.name}</span>
                        <span class="font-mono text-xs text-zinc-500 truncate max-w-[280px]" title={feed.url}>{feed.url}</span>
                      </button>
                      <div class="flex items-center gap-3">
                        <button
                          type="button"
                          class="flex items-center gap-1 rounded px-1.5 py-0.5 text-xs text-zinc-500 transition-colors hover:bg-white/[.04] hover:text-zinc-200 disabled:opacity-50"
                          title={pollingFeedID() === feed.id ? 'Polling…' : `Refresh now (auto-polls every ${feed.interval_min} min)`}
                          disabled={pollingFeedID() !== null}
                          onClick={async (e) => {
                            e.stopPropagation();
                            setPollingFeedID(feed.id);
                            try {
                              await props.onPollFeed(feed.id);
                              toast.success(`Refreshed "${feed.name}"`);
                            } catch (err) {
                              toast.error(`Refresh failed — ${String(err)}`);
                            } finally {
                              setPollingFeedID(null);
                            }
                          }}
                        >
                          <RefreshCw
                            class="h-3 w-3"
                            classList={{'animate-spin': pollingFeedID() === feed.id}}
                          />
                          {fmtLastPolled(feed.last_polled)}
                        </button>
                        <div class="flex gap-1">
                          <button class="grid h-7 w-7 place-items-center rounded text-zinc-500 hover:bg-white/[.04] hover:text-zinc-100" onClick={() => setEditingFeedID(feed.id)} title="Edit">
                            <Pencil class="h-3 w-3" />
                          </button>
                          <button
                            class="grid h-7 w-7 place-items-center rounded text-zinc-500 hover:bg-rose-500/20 hover:text-rose-300"
                            onClick={async () => {
                              if (!confirm(`Delete feed "${feed.name}"? Its filters will also be removed.`)) return;
                              try {
                                await props.onDeleteFeed(feed.id);
                                toast.success('Feed deleted');
                              } catch (e) { toast.error(String(e)); }
                            }}
                            title="Delete"
                          >
                            <Trash2 class="h-3 w-3" />
                          </button>
                        </div>
                      </div>
                    </div>

                    <Show when={expanded().has(feed.id)}>
                      <div class="ml-6 mb-2 border-l border-white/[.04] pl-3">
                        <div class="flex items-center justify-between py-1.5">
                          <span class="text-xs uppercase tracking-wide text-zinc-500">Filters</span>
                          <Button variant="ghost" onClick={() => setCreatingFilterFor(feed.id)}>
                            <Plus class="h-3 w-3" />
                            Add filter
                          </Button>
                        </div>

                        <Show when={creatingFilterFor() === feed.id}>
                          <FilterForm
                            initial={{id: 0, feed_id: feed.id, regex: '', category_id: null, save_path: '', enabled: true}}
                            categories={props.categories}
                            onCancel={() => setCreatingFilterFor(null)}
                            onSubmit={async (next) => {
                              try {
                                await props.onCreateFilter(next);
                                setCreatingFilterFor(null);
                                toast.success('Filter added');
                              } catch (e) { toast.error(String(e)); }
                            }}
                          />
                        </Show>

                        <Show when={(props.filtersByFeed[feed.id] ?? []).length === 0 && creatingFilterFor() !== feed.id}>
                          <p class="py-3 text-xs text-zinc-500">No filters. Items in this feed won't auto-add until you create one.</p>
                        </Show>

                        <ul class="flex flex-col gap-px">
                          <For each={props.filtersByFeed[feed.id] ?? []}>
                            {(fil) => (
                              <li>
                                <Show
                                  when={editingFilterID() === fil.id}
                                  fallback={
                                    <div class="flex items-center justify-between py-1.5 px-1 hover:bg-white/[.02]">
                                      <div class="flex items-center gap-2 min-w-0">
                                        <span
                                          class="h-1.5 w-1.5 rounded-full shrink-0"
                                          classList={{'bg-seed': fil.enabled, 'bg-zinc-600': !fil.enabled}}
                                        />
                                        <span class="font-mono text-xs text-zinc-200 truncate" title={fil.regex}>{fil.regex || '(no regex)'}</span>
                                        <Show when={fil.category_id !== null}>
                                          <span class="inline-flex items-center gap-1 rounded bg-white/[.04] px-1.5 py-0.5 text-[10px] text-zinc-300">
                                            <span
                                              class="h-1.5 w-1.5 rounded-full"
                                              style={{background: props.categories.find((c) => c.id === fil.category_id)?.color ?? '#71717a'}}
                                            />
                                            {props.categories.find((c) => c.id === fil.category_id)?.name ?? `#${fil.category_id}`}
                                          </span>
                                        </Show>
                                        <Show when={fil.save_path}>
                                          <span class="font-mono text-[10px] text-zinc-500 truncate" title={fil.save_path}>{fil.save_path}</span>
                                        </Show>
                                      </div>
                                      <div class="flex gap-1 shrink-0">
                                        <button class="grid h-6 w-6 place-items-center rounded text-zinc-500 hover:bg-white/[.04] hover:text-zinc-100" onClick={() => setEditingFilterID(fil.id)} title="Edit">
                                          <Pencil class="h-3 w-3" />
                                        </button>
                                        <button
                                          class="grid h-6 w-6 place-items-center rounded text-zinc-500 hover:bg-rose-500/20 hover:text-rose-300"
                                          onClick={async () => {
                                            if (!confirm('Delete this filter?')) return;
                                            try {
                                              await props.onDeleteFilter(feed.id, fil.id);
                                              toast.success('Filter deleted');
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
                                  <FilterForm
                                    initial={fil}
                                    categories={props.categories}
                                    onCancel={() => setEditingFilterID(null)}
                                    onSubmit={async (next) => {
                                      try {
                                        await props.onUpdateFilter(next);
                                        setEditingFilterID(null);
                                        toast.success('Filter updated');
                                      } catch (e) { toast.error(String(e)); }
                                    }}
                                  />
                                </Show>
                              </li>
                            )}
                          </For>
                        </ul>
                      </div>
                    </Show>
                  </div>
                }
              >
                <FeedForm
                  initial={feed}
                  onCancel={() => setEditingFeedID(null)}
                  onSubmit={async (next) => {
                    try {
                      await props.onUpdateFeed(next);
                      setEditingFeedID(null);
                      toast.success('Feed updated');
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

function FeedForm(props: {
  initial: FeedDTO;
  onCancel: () => void;
  onSubmit: (f: FeedDTO) => Promise<void>;
}) {
  const [name, setName] = createSignal(props.initial.name);
  const [url, setUrl] = createSignal(props.initial.url);
  const [interval, setInterval] = createSignal(props.initial.interval_min || 30);
  const [enabled, setEnabled] = createSignal(props.initial.enabled);

  const valid = () => name().trim() !== '' && url().trim() !== '' && interval() > 0;

  return (
    <form
      class="flex flex-col gap-2 rounded-md border border-white/[.06] bg-white/[.02] p-3 my-2"
      onSubmit={async (e) => {
        e.preventDefault();
        if (!valid()) return;
        await props.onSubmit({
          id: props.initial.id,
          url: url().trim(),
          name: name().trim(),
          interval_min: interval(),
          last_polled: props.initial.last_polled,
          etag: props.initial.etag,
          enabled: enabled(),
        });
      }}
    >
      <div class="grid grid-cols-[80px_1fr] items-center gap-2">
        <label class="text-xs text-zinc-500">Name</label>
        <input class="rounded border border-white/[.06] bg-black/30 px-2 py-1 text-sm text-zinc-100 focus:border-accent-500/50 focus:outline-none" value={name()} onInput={(e) => setName(e.currentTarget.value)} autofocus placeholder="e.g. Ubuntu releases" />
      </div>
      <div class="grid grid-cols-[80px_1fr] items-center gap-2">
        <label class="text-xs text-zinc-500">URL</label>
        <input class="rounded border border-white/[.06] bg-black/30 px-2 py-1 font-mono text-xs text-zinc-100 focus:border-accent-500/50 focus:outline-none" value={url()} onInput={(e) => setUrl(e.currentTarget.value)} placeholder="https://example.com/rss.xml" />
      </div>
      <div class="grid grid-cols-[80px_1fr] items-center gap-2">
        <label class="text-xs text-zinc-500">Interval</label>
        <div class="flex items-center gap-2">
          <input
            type="number"
            min="1"
            class="w-24 rounded border border-white/[.06] bg-black/30 px-2 py-1 text-sm text-zinc-100 focus:border-accent-500/50 focus:outline-none"
            value={interval()}
            onInput={(e) => setInterval(parseInt(e.currentTarget.value || '30', 10))}
          />
          <span class="text-xs text-zinc-500">minutes between polls</span>
        </div>
      </div>
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
        <Button type="submit" variant="primary" disabled={!valid()}>
          <Check class="h-3.5 w-3.5" />
          Save
        </Button>
      </div>
    </form>
  );
}

function FilterForm(props: {
  initial: FilterDTO;
  categories: CategoryDTO[];
  onCancel: () => void;
  onSubmit: (f: FilterDTO) => Promise<void>;
}) {
  const [regex, setRegex] = createSignal(props.initial.regex);
  const [categoryID, setCategoryID] = createSignal<number | null>(props.initial.category_id);
  const [savePath, setSavePath] = createSignal(props.initial.save_path);
  const [enabled, setEnabled] = createSignal(props.initial.enabled);

  const regexEmpty = () => regex().trim() === '';
  const regexValid = () => {
    const r = regex().trim();
    if (!r) return false;
    try { new RegExp(r); return true; } catch { return false; }
  };
  // Surfaces *why* Save is disabled. Plain "disabled button" gave no
  // signal — users (correctly) couldn't tell whether the regex, the
  // category dropdown, or something else was blocking submit.
  const disabledReason = () => {
    if (regexEmpty()) return 'Enter a regex to enable Save.';
    if (!regexValid()) return 'Regex is invalid — fix the syntax to enable Save.';
    return null;
  };

  return (
    <form
      class="flex flex-col gap-2 rounded-md border border-white/[.06] bg-white/[.02] p-2 my-1"
      onSubmit={async (e) => {
        e.preventDefault();
        if (!regexValid()) return;
        await props.onSubmit({
          id: props.initial.id,
          feed_id: props.initial.feed_id,
          regex: regex().trim(),
          category_id: categoryID(),
          save_path: savePath(),
          enabled: enabled(),
        });
      }}
    >
      <div class="grid grid-cols-[80px_1fr] items-center gap-2">
        <label class="text-xs text-zinc-500">
          Regex <span class="text-rose-400" aria-label="required">*</span>
        </label>
        <input
          class="rounded border border-white/[.06] bg-black/30 px-2 py-1 font-mono text-xs text-zinc-100 focus:border-accent-500/50 focus:outline-none"
          classList={{'border-rose-500/50': regex().trim() !== '' && !regexValid()}}
          value={regex()}
          onInput={(e) => setRegex(e.currentTarget.value)}
          autofocus
          placeholder={`e.g. (?i)ubuntu.*amd64`}
        />
      </div>
      <div class="grid grid-cols-[80px_1fr] items-center gap-2">
        <label class="text-xs text-zinc-500">Category</label>
        <div class="flex items-center gap-2">
          <select
            class="w-fit rounded border border-white/[.06] bg-black/30 px-2 py-1 text-sm text-zinc-100 focus:border-accent-500/50 focus:outline-none"
            value={categoryID() ?? ''}
            onChange={(e) => {
              const v = e.currentTarget.value;
              setCategoryID(v === '' ? null : parseInt(v, 10));
            }}
          >
            <option value="">— None —</option>
            <For each={props.categories}>
              {(c) => <option value={c.id}>{c.name}</option>}
            </For>
          </select>
          <span class="text-xs text-zinc-500">optional</span>
        </div>
      </div>
      <div class="grid grid-cols-[80px_1fr] items-center gap-2">
        <label class="text-xs text-zinc-500">Save path</label>
        <input
          class="rounded border border-white/[.06] bg-black/30 px-2 py-1 font-mono text-xs text-zinc-100 focus:border-accent-500/50 focus:outline-none"
          value={savePath()}
          onInput={(e) => setSavePath(e.currentTarget.value)}
          placeholder="Optional — overrides category/default"
        />
      </div>
      <div class="grid grid-cols-[80px_1fr] items-center gap-2">
        <label class="text-xs text-zinc-500">Status</label>
        <label class="inline-flex items-center gap-2 text-sm text-zinc-200">
          <input type="checkbox" checked={enabled()} onChange={(e) => setEnabled(e.currentTarget.checked)} />
          Enabled
        </label>
      </div>
      <div class="flex items-center justify-end gap-2 mt-1">
        <Show when={disabledReason()}>
          <span class="mr-auto text-xs text-zinc-500">{disabledReason()}</span>
        </Show>
        <Button type="button" variant="ghost" onClick={props.onCancel}>
          <X class="h-3.5 w-3.5" />
          Cancel
        </Button>
        <Button type="submit" variant="primary" disabled={!regexValid()}>
          <Check class="h-3.5 w-3.5" />
          Save
        </Button>
      </div>
    </form>
  );
}
