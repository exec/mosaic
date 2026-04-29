# Mosaic — Plan 4b: Settings Panel + Categories/Tags Management

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add the first-class **Settings panel** the icon rail has been pointing at since Plan 2, plus full CRUD UIs for the Categories and Tags introduced in Plan 4a. Currently those exist only as right-click submenu surfaces — there's no place to *create*, *rename*, *recolor*, or *delete* without going through a torrent's context menu. After this plan, clicking Settings in the icon rail opens a real route with four sub-panes, the Save-target field in AddTorrentModal actually does something, and `Snapshot.Paused` carries the real paused state instead of always being `false`.

**Architecture:** Frontend gets a `view` state in the store (`'torrents' | 'settings'`), with the IconRail driving switches. WindowShell's main pane conditionally renders either `<TorrentList>` (current behavior) or `<SettingsRoute>` (new). Settings has its own internal sidebar with four panes (General / Categories / Tags / About). Backend gains a small `SettingsService` typed wrapper over the existing KV store for the few app-level prefs (default save path, theme override) that need server-side persistence. The carry-over fix for `Snapshot.Paused` adds a `paused` field tracked in AnacrolixBackend.

**Tech additions:** none — built on Plan 1–4a stack.

**Reference spec:** `docs/superpowers/specs/2026-04-28-bittorrent-client-design.md` §4.5 (Settings location), §6 (settings table, already in Plan 1's migration 0001).

**Aesthetic continuity:** Settings panes use the same glass surfaces, accent-violet primary actions, monospace tabular numerals where applicable, and Kobalte primitives. Each pane has a centered max-width content column so it reads well on wide windows. Sidebar within Settings uses the same active-edge indicator as the IconRail.

---

## Out of Scope (deferred to Plan 4c)

- **Queueing** — global active limit, queue priority, force-start
- **Bandwidth scheduling** — time-of-day rate-limit profiles
- **IP filtering / blocklists**
- **Real alt-speed limits** — toolbar Zap button stays inert through Plan 4b
- **Connection / Network sub-pane** in Settings (port, DHT, encryption toggles) — Plan 4c
- **Tracker status refinement** beyond the "always OK" stub — Plan 4c
- **RSS auto-add** — Plan 5

---

## File Structure (final state)

```
backend/
├── api/
│   ├── service.go                  # NEW SettingsService methods (DefaultSavePath getter/setter)
│   └── service_test.go
├── engine/
│   ├── types.go                    # already has Snapshot.Paused; AnacrolixBackend will populate it
│   ├── anacrolix.go                # MODIFIED: track per-torrent paused state explicitly
│   └── engine_test.go              # + test verifying Pause/Resume reflects in Snapshot
app.go                              # NEW bindings: GetDefaultSavePath, SetDefaultSavePath

frontend/src/
├── lib/
│   ├── bindings.ts                 # NEW api methods + getDefaultSavePath/setDefaultSavePath
│   └── store.ts                    # NEW view state ('torrents' | 'settings'), defaultSavePath
└── components/
    ├── shell/
    │   ├── IconRail.tsx            # MODIFIED: Settings is no longer disabled stub; calls store.setView
    │   ├── WindowShell.tsx         # MODIFIED: switch on view between TorrentList and SettingsRoute
    │   └── AddTorrentModal.tsx     # MODIFIED: save-target field actually wired
    └── settings/
        ├── SettingsRoute.tsx       # NEW: top-level layout with sidebar
        ├── SettingsSidebar.tsx     # NEW: 4-pane nav (General / Categories / Tags / About)
        ├── GeneralPane.tsx         # NEW: theme picker + default save path edit
        ├── CategoriesPane.tsx      # NEW: full Category CRUD with color picker
        ├── TagsPane.tsx            # NEW: full Tag CRUD with color picker
        ├── AboutPane.tsx           # NEW: version, repo link, plan progress
        └── ColorPicker.tsx         # NEW: small palette picker reusable for Cat + Tag
```

---

## Section A — Backend: Snapshot.Paused real value + SettingsService

### Task 1: AnacrolixBackend tracks paused state — failing test

**Files:** `backend/engine/engine_test.go` (modify)

- [ ] **Step 1: Append a failing test**

```go
func TestEngine_Pause_ReflectsInSnapshot(t *testing.T) {
	fb := NewFakeBackend()
	eng := NewEngine(fb, 50*time.Millisecond)
	t.Cleanup(func() { _ = eng.Close() })

	id, err := eng.AddMagnet(context.Background(), "magnet:?xt=urn:btih:pause", "/tmp")
	require.NoError(t, err)

	snap, _ := eng.Snapshot(id)
	require.False(t, snap.Paused, "fresh torrent is not paused")

	require.NoError(t, eng.Pause(id))
	snap, _ = eng.Snapshot(id)
	require.True(t, snap.Paused, "after Pause, Snapshot.Paused should be true")

	require.NoError(t, eng.Resume(id))
	snap, _ = eng.Snapshot(id)
	require.False(t, snap.Paused, "after Resume, Snapshot.Paused should be false")
}
```

The `FakeBackend` already updates `t.Paused` correctly in its in-memory store, so this test should fail only on the Anacrolix path — but the unit test goes through FakeBackend, so let's also add an Anacrolix-side check. Actually the FakeBackend correctly toggles; verify the test still fails today (Plan 1 wired Pause/Resume but `snapshotFor` in anacrolix.go has `Paused: false` hard-coded). The fake should pass; the fix is purely on the anacrolix side.

Run: `go test ./backend/engine/ -run TestEngine_Pause_ReflectsInSnapshot -v` — should already PASS via the fake. **The bug is only in production.** So this test is for the fake path; the anacrolix fix is done by inspection.

Reword the goal: ensure the fake correctly reflects Pause/Resume in Snapshot (it does — assertion documents the behavior contract), and fix `anacrolix.go`'s `snapshotFor` to read a real paused-flag map.

- [ ] **Step 2: Run, confirm passes (the fake correctly tracks paused already)**

```bash
go test ./backend/engine/ -run TestEngine_Pause_ReflectsInSnapshot -v
```

Expected: PASS.

---

### Task 2: AnacrolixBackend.snapshotFor reads paused state from a map

**Files:** `backend/engine/anacrolix.go` (modify)

- [ ] **Step 1: Add `paused map[TorrentID]bool` to AnacrolixBackend struct**

```go
type AnacrolixBackend struct {
	client     *torrent.Client
	mu         sync.Mutex
	bySaveTo   map[TorrentID]string

	rateMu     sync.Mutex
	prevRates  map[TorrentID]rateSample

	pausedMu   sync.RWMutex
	paused     map[TorrentID]bool
}
```

Initialize `paused: make(map[TorrentID]bool)` in `NewAnacrolixBackend`.

- [ ] **Step 2: Update Pause/Resume to flip the flag**

```go
func (a *AnacrolixBackend) Pause(id TorrentID) error {
	t, ok := a.find(id)
	if !ok { return errors.New("not found") }
	t.SetMaxEstablishedConns(0)
	a.pausedMu.Lock()
	a.paused[id] = true
	a.pausedMu.Unlock()
	return nil
}

func (a *AnacrolixBackend) Resume(id TorrentID) error {
	t, ok := a.find(id)
	if !ok { return errors.New("not found") }
	t.SetMaxEstablishedConns(80)
	a.pausedMu.Lock()
	a.paused[id] = false
	a.pausedMu.Unlock()
	return nil
}
```

Also clean up the entry in `Remove`:
```go
a.pausedMu.Lock()
delete(a.paused, id)
a.pausedMu.Unlock()
```

- [ ] **Step 3: snapshotFor reads from the map**

`snapshotFor` currently has `Paused: false` hard-coded. Change the signature to take the paused flag (the caller will supply it from the map):

```go
func snapshotFor(t *torrent.Torrent, prev rateSample, paused bool) (Snapshot, rateSample) {
	// ... existing body ...
	snap := Snapshot{
		// ...
		Paused: paused,
		// ...
	}
	return snap, rateSample{at: now, down: bytesDown, up: bytesUp}
}
```

Update the three call sites (Snapshot, List, DetailedSnapshot) to read `a.paused[id]` under RLock.

- [ ] **Step 4: Run all tests**

```bash
go test ./backend/engine/ -v -race
```

Expected: PASS, including the new `TestEngine_Pause_ReflectsInSnapshot`.

- [ ] **Step 5: Commit**

```bash
git add backend/engine/anacrolix.go backend/engine/engine_test.go
git commit -m "fix(engine): AnacrolixBackend tracks paused state for Snapshot.Paused"
```

---

### Task 3: SettingsService typed wrapper — failing test

**Files:** `backend/api/service_test.go` (modify)

- [ ] **Step 1: Append failing tests**

```go
func TestService_DefaultSavePath_Persistence(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	got, err := svc.GetDefaultSavePath(ctx)
	require.NoError(t, err)
	require.Equal(t, "/tmp/dl", got, "no override yet — falls back to constructor default")

	require.NoError(t, svc.SetDefaultSavePath(ctx, "/Volumes/torrents"))

	got, err = svc.GetDefaultSavePath(ctx)
	require.NoError(t, err)
	require.Equal(t, "/Volumes/torrents", got)
}
```

- [ ] **Step 2: Run, confirm fails**

Expected: FAIL on undefined methods.

---

### Task 4: SettingsService implementation

**Files:** `backend/api/service.go` (modify)

- [ ] **Step 1: Add `settings *persistence.Settings` to Service struct + NewService param**

```go
type Service struct {
	engine          *engine.Engine
	torrents        *persistence.Torrents
	categories      *persistence.Categories
	tags            *persistence.Tags
	settings        *persistence.Settings   // NEW
	defaultSavePath string

	// existing focus state
}

func NewService(
	eng *engine.Engine,
	torrents *persistence.Torrents,
	categories *persistence.Categories,
	tags *persistence.Tags,
	settings *persistence.Settings,
	defaultSavePath string,
) *Service { /* update body */ }
```

Update `newTestService` and `main.go` to pass `persistence.NewSettings(db)`.

- [ ] **Step 2: Add the typed methods**

```go
const settingDefaultSavePath = "default_save_path"

func (s *Service) GetDefaultSavePath(ctx context.Context) (string, error) {
	v, err := s.settings.Get(ctx, settingDefaultSavePath)
	if errors.Is(err, persistence.ErrNotFound) {
		return s.defaultSavePath, nil
	}
	if err != nil {
		return "", err
	}
	return v, nil
}

func (s *Service) SetDefaultSavePath(ctx context.Context, path string) error {
	return s.settings.Set(ctx, settingDefaultSavePath, path)
}
```

Add `"errors"` to imports if not already present.

Update `AddMagnet` and `AddTorrentBytes` to honor the persisted setting when no explicit savePath is given:

```go
func (s *Service) defaultPath(ctx context.Context) string {
	if v, err := s.GetDefaultSavePath(ctx); err == nil {
		return v
	}
	return s.defaultSavePath
}

// In AddMagnet:
if savePath == "" { savePath = s.defaultPath(ctx) }
// In AddTorrentBytes:
if savePath == "" { savePath = s.defaultPath(ctx) }
```

- [ ] **Step 3: Run tests**

```bash
go test ./backend/api/ -v -race
```

Expected: PASS (one new test).

- [ ] **Step 4: Commit**

```bash
git add backend/api/service.go backend/api/service_test.go main.go
git commit -m "feat(api): SettingsService — DefaultSavePath get/set with persistence override"
```

---

### Task 5: Wails bindings for SettingsService

**Files:** `app.go`

- [ ] **Step 1: Bind methods**

```go
func (a *App) GetDefaultSavePath() (string, error) {
	return a.svc.GetDefaultSavePath(a.ctx)
}

func (a *App) SetDefaultSavePath(path string) error {
	return a.svc.SetDefaultSavePath(a.ctx, path)
}
```

- [ ] **Step 2: Regenerate Wails bindings**

```bash
$(go env GOPATH)/bin/wails generate module
```

Verify `App.d.ts` exports both.

- [ ] **Step 3: Verify build**

```bash
go build ./...
```

Expected: clean.

- [ ] **Step 4: Commit**

```bash
git add app.go
git commit -m "feat: GetDefaultSavePath / SetDefaultSavePath Wails bindings"
```

---

## Section B — Frontend: View routing + Settings infrastructure

### Task 6: Frontend bindings + store extensions

**Files:** `frontend/src/lib/bindings.ts`, `frontend/src/lib/store.ts`

- [ ] **Step 1: Extend bindings**

In `frontend/src/lib/bindings.ts`:

```ts
import {
  // existing
  GetDefaultSavePath, SetDefaultSavePath,
} from '../../wailsjs/go/main/App';

export const api = {
  // existing
  getDefaultSavePath: () => GetDefaultSavePath() as Promise<string>,
  setDefaultSavePath: (path: string) => SetDefaultSavePath(path),
};
```

- [ ] **Step 2: Extend store**

```ts
export type AppView = 'torrents' | 'settings';

export type AppState = {
  // existing
  view: AppView;
  defaultSavePath: string;
};

// In createTorrentsStore initialization:
api.getDefaultSavePath().then((p) => setState(produce((s) => { s.defaultSavePath = p; })));

// New methods:
return {
  // existing
  setView: (v: AppView) => setState(produce((s) => { s.view = v; })),
  setDefaultSavePath: async (p: string) => {
    await api.setDefaultSavePath(p);
    setState(produce((s) => { s.defaultSavePath = p; }));
  },
  updateCategory: async (id: number, name: string, savePath: string, color: string) => {
    await api.updateCategory(id, name, savePath, color);
    await store.refreshCategories();
  },
  // ... mirror updateTag if missing ...
};
```

(The plan-4a Batch 5 left `updateCategory` as bindings-only; expose it on the store now.)

- [ ] **Step 3: Verify build**

```bash
cd frontend && npm run build && npm test
```

Expected: build clean, 27 tests pass.

- [ ] **Step 4: Commit**

```bash
git add frontend/src/lib/bindings.ts frontend/src/lib/store.ts
git commit -m "feat(frontend): view state + defaultSavePath store, settings bindings"
```

---

### Task 7: ColorPicker UI primitive

**Files:** Create `frontend/src/components/settings/ColorPicker.tsx`

- [ ] **Step 1: Write the component**

A small palette of 8 OKLCH-derived colors (red/orange/amber/emerald/cyan/violet/pink/zinc). Click a swatch → calls `props.onSelect(hex)`.

```tsx
import {For} from 'solid-js';

const palette = [
  '#ef4444', // red
  '#f59e0b', // amber
  '#eab308', // yellow
  '#22c55e', // emerald
  '#06b6d4', // cyan
  '#3b82f6', // blue
  '#a855f7', // violet
  '#ec4899', // pink
  '#71717a', // zinc
];

type Props = {
  value: string;
  onSelect: (hex: string) => void;
};

export function ColorPicker(props: Props) {
  return (
    <div class="inline-flex items-center gap-1 rounded-md border border-white/[.06] bg-white/[.02] p-1">
      <For each={palette}>
        {(hex) => (
          <button
            type="button"
            onClick={() => props.onSelect(hex)}
            class="grid h-5 w-5 place-items-center rounded transition-transform hover:scale-110"
            style={{background: hex}}
            aria-label={`Color ${hex}`}
          >
            {props.value === hex && <span class="h-1.5 w-1.5 rounded-full bg-white/90" />}
          </button>
        )}
      </For>
    </div>
  );
}
```

- [ ] **Step 2: Commit**

```bash
git add frontend/src/components/settings/ColorPicker.tsx
git commit -m "feat(frontend): ColorPicker palette swatch primitive"
```

---

### Task 8: SettingsSidebar component

**Files:** Create `frontend/src/components/settings/SettingsSidebar.tsx`

- [ ] **Step 1: Write the component**

```tsx
import {For} from 'solid-js';
import {Sliders, Folder, Tag, Info} from 'lucide-solid';

export type SettingsPane = 'general' | 'categories' | 'tags' | 'about';

const items: {value: SettingsPane; label: string; icon: typeof Sliders}[] = [
  {value: 'general',    label: 'General',    icon: Sliders},
  {value: 'categories', label: 'Categories', icon: Folder},
  {value: 'tags',       label: 'Tags',       icon: Tag},
  {value: 'about',      label: 'About',      icon: Info},
];

type Props = {
  active: SettingsPane;
  onSelect: (p: SettingsPane) => void;
};

export function SettingsSidebar(props: Props) {
  return (
    <aside class="flex h-full w-56 shrink-0 flex-col border-r border-white/[.04] bg-white/[.01] pt-10 pb-3">
      <ul class="flex flex-col gap-px px-2">
        <For each={items}>
          {(item) => (
            <li>
              <button
                type="button"
                onClick={() => props.onSelect(item.value)}
                class="relative flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm text-zinc-300 transition-colors duration-100 hover:bg-white/[.04] hover:text-zinc-100"
                classList={{'bg-white/[.04] text-zinc-100': props.active === item.value}}
              >
                <item.icon class="h-3.5 w-3.5" />
                {item.label}
                {props.active === item.value && (
                  <span class="absolute left-0 top-1.5 bottom-1.5 w-[2px] rounded-r-full bg-accent-500" />
                )}
              </button>
            </li>
          )}
        </For>
      </ul>
    </aside>
  );
}
```

The `pt-10` matches the IconRail and FilterRail clearance for the macOS title bar.

- [ ] **Step 2: Commit**

```bash
git add frontend/src/components/settings/SettingsSidebar.tsx
git commit -m "feat(frontend): SettingsSidebar 4-pane nav (General/Categories/Tags/About)"
```

---

### Task 9: GeneralPane

**Files:** Create `frontend/src/components/settings/GeneralPane.tsx`

- [ ] **Step 1: Write the component**

Inputs:
- Theme — uses `<ThemeToggle />` from Plan 2 (re-imported)
- Default save path — text input bound to `props.defaultSavePath`, save button calls `props.onSetDefaultSavePath(value)`. Show a small toast.success on save.

```tsx
import {createSignal} from 'solid-js';
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
```

- [ ] **Step 2: Commit**

```bash
git add frontend/src/components/settings/GeneralPane.tsx
git commit -m "feat(frontend): GeneralPane with theme + default save path"
```

---

### Task 10: CategoriesPane (full CRUD UI)

**Files:** Create `frontend/src/components/settings/CategoriesPane.tsx`

- [ ] **Step 1: Write the component**

```tsx
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
```

- [ ] **Step 2: Commit**

```bash
git add frontend/src/components/settings/CategoriesPane.tsx
git commit -m "feat(frontend): CategoriesPane with full CRUD + color picker"
```

---

### Task 11: TagsPane (mirror of Categories, simpler)

**Files:** Create `frontend/src/components/settings/TagsPane.tsx`

- [ ] **Step 1: Write the component**

Tags are simpler than categories — no `default_save_path`, just name + color. Same overall structure as `CategoriesPane` but the form has only Name + Color fields. Re-use `ColorPicker`.

```tsx
import {createSignal, For, Show} from 'solid-js';
import {Plus, Trash2, Pencil, Check, X} from 'lucide-solid';
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
```

> Note: tag editing isn't included in v1 since the only field that could change is the color — we can revisit if it's annoying. Delete + recreate works.

- [ ] **Step 2: Commit**

```bash
git add frontend/src/components/settings/TagsPane.tsx
git commit -m "feat(frontend): TagsPane with create/delete + color picker"
```

---

### Task 12: AboutPane

**Files:** Create `frontend/src/components/settings/AboutPane.tsx`

- [ ] **Step 1: Write the component**

```tsx
import {ExternalLink} from 'lucide-solid';

const PROGRESS = [
  {label: 'Plan 1 — Foundation & first download', done: true},
  {label: 'Plan 2 — Polished window shell', done: true},
  {label: 'Plan 3 — Inspector + Watch IPC', done: true},
  {label: 'Plan 4a — Organization', done: true},
  {label: 'Plan 4b — Settings panel', done: true},
  {label: 'Plan 4c — Bandwidth controls', done: false},
  {label: 'Plan 5 — RSS auto-add', done: false},
  {label: 'Plan 6 — Remote interface', done: false},
  {label: 'Plan 7 — Auto-update', done: false},
  {label: 'Plan 8 — Packaging & signing', done: false},
];

export function AboutPane() {
  return (
    <div class="mx-auto max-w-2xl px-6 py-6">
      <div class="mb-4 border-b border-white/[.04] pb-3">
        <h2 class="text-lg font-semibold text-zinc-100">About Mosaic</h2>
        <p class="mt-0.5 text-sm text-zinc-500">A polished cross-platform BitTorrent client — Go + Wails + anacrolix.</p>
      </div>

      <div class="rounded-md border border-white/[.06] bg-white/[.02] p-4 mb-6">
        <a
          href="https://github.com/exec/mosaic"
          target="_blank"
          rel="noopener noreferrer"
          class="inline-flex items-center gap-1.5 text-sm text-accent-400 hover:text-accent-200"
        >
          github.com/exec/mosaic
          <ExternalLink class="h-3 w-3" />
        </a>
      </div>

      <div class="rounded-md border border-white/[.06] bg-white/[.02] p-4">
        <div class="text-xs uppercase tracking-wider text-zinc-500 mb-3">Roadmap</div>
        <ul class="flex flex-col gap-1.5 text-sm">
          {PROGRESS.map((p) => (
            <li class="flex items-center gap-2">
              <span
                class="h-2 w-2 shrink-0 rounded-full"
                classList={{
                  'bg-seed': p.done,
                  'bg-zinc-700': !p.done,
                }}
              />
              <span classList={{'text-zinc-200': p.done, 'text-zinc-500': !p.done}}>{p.label}</span>
            </li>
          ))}
        </ul>
      </div>
    </div>
  );
}
```

- [ ] **Step 2: Commit**

```bash
git add frontend/src/components/settings/AboutPane.tsx
git commit -m "feat(frontend): AboutPane with repo link + roadmap"
```

---

### Task 13: SettingsRoute (composes the panes)

**Files:** Create `frontend/src/components/settings/SettingsRoute.tsx`

- [ ] **Step 1: Write the component**

```tsx
import {createSignal, Match, Switch} from 'solid-js';
import type {CategoryDTO, TagDTO} from '../../lib/bindings';
import {SettingsSidebar, type SettingsPane} from './SettingsSidebar';
import {GeneralPane} from './GeneralPane';
import {CategoriesPane} from './CategoriesPane';
import {TagsPane} from './TagsPane';
import {AboutPane} from './AboutPane';

type Props = {
  defaultSavePath: string;
  categories: CategoryDTO[];
  tags: TagDTO[];
  onSetDefaultSavePath: (path: string) => Promise<void>;
  onCreateCategory: (name: string, savePath: string, color: string) => Promise<void>;
  onUpdateCategory: (id: number, name: string, savePath: string, color: string) => Promise<void>;
  onDeleteCategory: (id: number) => Promise<void>;
  onCreateTag: (name: string, color: string) => Promise<void>;
  onDeleteTag: (id: number) => Promise<void>;
};

export function SettingsRoute(props: Props) {
  const [pane, setPane] = createSignal<SettingsPane>('general');

  return (
    <div class="flex h-full">
      <SettingsSidebar active={pane()} onSelect={setPane} />
      <div class="flex-1 overflow-auto">
        <Switch>
          <Match when={pane() === 'general'}>
            <GeneralPane defaultSavePath={props.defaultSavePath} onSetDefaultSavePath={props.onSetDefaultSavePath} />
          </Match>
          <Match when={pane() === 'categories'}>
            <CategoriesPane categories={props.categories} onCreate={props.onCreateCategory} onUpdate={props.onUpdateCategory} onDelete={props.onDeleteCategory} />
          </Match>
          <Match when={pane() === 'tags'}>
            <TagsPane tags={props.tags} onCreate={props.onCreateTag} onDelete={props.onDeleteTag} />
          </Match>
          <Match when={pane() === 'about'}>
            <AboutPane />
          </Match>
        </Switch>
      </div>
    </div>
  );
}
```

- [ ] **Step 2: Commit**

```bash
git add frontend/src/components/settings/SettingsRoute.tsx
git commit -m "feat(frontend): SettingsRoute composes sidebar + 4 panes"
```

---

### Task 14: IconRail — Settings is no longer a stub

**Files:** `frontend/src/components/shell/IconRail.tsx` (modify)

- [ ] **Step 1: Update the bottom items**

Settings is no longer a `soon` stub. Remove the `soon: 'Plan 4+'` annotation from the Settings item. Add it to the item-click handler — clicking Settings calls `props.onNavigate('settings')`. Clicking Torrents (top item) calls `props.onNavigate('torrents')`. About stays as a stub for now.

The IconRail's local `active` signal needs to be kept in sync with the actual app view, OR replaced with a controlled `active` prop. Cleaner: pass `view` from props and let App.tsx own the source of truth.

```tsx
// IconRail Props change:
type Props = {
  view: 'torrents' | 'settings';
  onNavigate: (v: 'torrents' | 'settings') => void;
};
```

Update items:
```tsx
const top: Item[] = [
  {id: 'torrents', label: 'Torrents', icon: Activity},
  {id: 'search',   label: 'Search',   icon: Search,   soon: 'Plan 5+'},
  {id: 'schedule', label: 'Schedule', icon: Calendar, soon: 'Plan 4c'},
  {id: 'rss',      label: 'RSS',      icon: Rss,      soon: 'Plan 5'},
];
const bottom: Item[] = [
  {id: 'settings', label: 'Settings', icon: Settings},   // was soon
  {id: 'about',    label: 'About',    icon: Info,       soon: 'soon'},
];
```

In Btn render, replace the `setActive(p.item.id)` with logic that calls `props.onNavigate` when the item is `torrents` or `settings`, otherwise no-op for stubs.

- [ ] **Step 2: WindowShell + App.tsx threading**

WindowShell Props gain `view` and `onNavigate`. Pass them through to IconRail. App.tsx supplies `store.state.view` and `store.setView`.

- [ ] **Step 3: WindowShell main pane switches on view**

```tsx
// WindowShell.tsx — main pane
<main class="flex flex-1 min-w-0 flex-col">
  <Switch>
    <Match when={props.view === 'torrents'}>
      <TopToolbar ... />
      <DropZone ...>
        <div class="h-full overflow-auto">
          {props.children}  {/* TorrentList passed by App.tsx */}
        </div>
      </DropZone>
    </Match>
    <Match when={props.view === 'settings'}>
      {props.settings}  {/* SettingsRoute passed by App.tsx */}
    </Match>
  </Switch>
</main>
```

The `inspector` slot only renders when `view === 'torrents'`.

The FilterRail also only renders in torrents view — when view is `'settings'`, render nothing in its slot or hide it via CSS. Simplest: WindowShell unconditionally renders FilterRail, but FilterRail's contents become moot in Settings view. To avoid visual clutter, conditionally render FilterRail only when `view === 'torrents'`:

```tsx
<Show when={props.view === 'torrents'}>
  <FilterRail ... />
</Show>
```

- [ ] **Step 4: Build verification**

```bash
cd frontend && npm run build
```

Expected: clean.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/components/shell/IconRail.tsx frontend/src/components/shell/WindowShell.tsx frontend/src/App.tsx
git commit -m "feat(frontend): IconRail Settings is functional, WindowShell switches on view"
```

---

### Task 15: Wire SettingsRoute in App.tsx

**Files:** `frontend/src/App.tsx` (modify)

- [ ] **Step 1: Build the SettingsRoute subtree**

```tsx
import {SettingsRoute} from './components/settings/SettingsRoute';

// Inside App():
const settingsView = (
  <SettingsRoute
    defaultSavePath={store.state.defaultSavePath}
    categories={store.state.categories}
    tags={store.state.tags}
    onSetDefaultSavePath={(p) => store.setDefaultSavePath(p)}
    onCreateCategory={(name, sp, color) => store.createCategory(name, sp, color)}
    onUpdateCategory={(id, name, sp, color) => store.updateCategory(id, name, sp, color)}
    onDeleteCategory={(id) => store.deleteCategory(id)}
    onCreateTag={(name, color) => store.createTag(name, color)}
    onDeleteTag={(id) => store.deleteTag(id)}
  />
);

// Pass it to WindowShell:
<WindowShell view={store.state.view} onNavigate={store.setView} settings={settingsView} ...>
  <TorrentList ... />
</WindowShell>
```

- [ ] **Step 2: Build verification**

```bash
cd frontend && npm run build && npm test
```

Expected: build clean, 27 tests pass.

- [ ] **Step 3: Commit**

```bash
git add frontend/src/App.tsx
git commit -m "feat(frontend): App wires SettingsRoute through WindowShell"
```

---

## Section C — AddTorrentModal save-target wiring (carry-over fix)

### Task 16: Save-target field actually does something

**Files:** `frontend/src/components/shell/AddTorrentModal.tsx` (modify), `frontend/src/lib/store.ts` (modify if needed)

- [ ] **Step 1: Update AddTorrentModal**

The Save target input currently renders but doesn't pass its value through. Update the submit handler:

- For magnet: replace `store.addMagnet(magnet)` with a new method on the store: `store.addMagnetWithPath(magnet, savePath)`. The store method calls `api.addMagnet(magnet)` which already accepts a path... wait, looking at the binding, `AddMagnet(magnet string)` doesn't accept a path. The plan-1 service had `AddMagnet(ctx, magnet, savePath string)` that defaults when empty. So we need to either expose `savePath` through the binding OR add a new binding.

Cleanest: add a new binding `AddMagnetWithSavePath(magnet, savePath string)` that calls `Service.AddMagnet(ctx, magnet, savePath)` directly. Or modify the existing `AddMagnet` Wails binding to take a second arg. Latter is simpler — App.AddMagnet's signature is ours to define.

Update `app.go`:
```go
func (a *App) AddMagnet(magnet, savePath string) (string, error) {
	id, err := a.svc.AddMagnet(a.ctx, magnet, savePath)
	if err != nil { return "", err }
	return string(id), nil
}
```

Regenerate Wails. Bindings:
```ts
addMagnet: (magnet: string, savePath: string) => AddMagnet(magnet, savePath),
```

Update the store's existing `addMagnet`:
```ts
addMagnet: (m: string, savePath = '') => api.addMagnet(m, savePath),
```

- For .torrent: `pickAndAddTorrent` already takes no args today; add `pickAndAddTorrentWithSavePath(savePath string) (string, error)` on App, OR just thread the savePath into the existing one. Simpler: change `App.PickAndAddTorrent` to accept a savePath:

```go
func (a *App) PickAndAddTorrent(savePath string) (string, error) {
	path, err := wailsruntime.OpenFileDialog(a.ctx, ...)
	if err != nil || path == "" { return "", err }
	id, err := a.svc.AddTorrentFile(a.ctx, path, savePath) // need to extend AddTorrentFile to honor savePath
	...
}
```

Update `Service.AddTorrentFile` similarly to accept and use savePath, defaulting via `s.defaultPath(ctx)`.

Same for `AddTorrentBytes(blob, savePath)` — already accepts savePath, so frontend just passes it.

- [ ] **Step 2: Modal submit collects savePath**

In AddTorrentModal, add a `savePath` createSignal initialized from `props.defaultSavePath`. On submit, pass it through.

- [ ] **Step 3: Regenerate Wails bindings**

```bash
$(go env GOPATH)/bin/wails generate module
```

- [ ] **Step 4: Update `frontend/src/lib/bindings.ts` to match new signatures**

```ts
addMagnet: (magnet: string, savePath: string) => AddMagnet(magnet, savePath),
pickAndAddTorrent: (savePath: string) => PickAndAddTorrent(savePath),
addTorrentBytes: (bytes: number[], savePath: string) => AddTorrentBytes(bytes, savePath),
```

- [ ] **Step 5: Update App.tsx and DropZone callers**

Wherever `store.addMagnet(m)` was called without a path (drag-drop, keyboard handlers, etc.), pass an empty string explicitly. Same for `pickAndAddTorrent` and `addTorrentBytes`.

- [ ] **Step 6: Verify build + tests**

```bash
go build ./...
go test ./... -race -count=1
cd frontend && npm run build && npm test
```

Expected: all clean.

- [ ] **Step 7: Commit**

```bash
git add backend/api/ app.go frontend/
git commit -m "fix: wire save-target field through AddMagnet/PickAndAddTorrent/AddTorrentBytes"
```

---

## Section D — Smoke test

### Task 17: User-driven smoke

- [ ] **Step 1: Run `~/go/bin/wails dev -skipembedcreate`**

- [ ] **Step 2: Visual + functional check**

- IconRail Settings icon is no longer dimmed/stub-tooltipped — clicking it switches the main pane to Settings.
- Settings pane shows a sidebar with General/Categories/Tags/About items, plus the active accent edge.
- General: theme picker works (already wired); default save path field shows the current value, editing + Save persists (toast confirms; restart and verify it sticks).
- Categories: New button reveals the form (name + save path + color picker). Submitting creates. Pencil edits inline. Trash deletes with confirm.
- Tags: similar (no save-path field, just name + color).
- About: shows roadmap with completed plans in green.
- Click Torrents in IconRail → switches back to torrent list. Inspector still functions.
- Open AddTorrentModal: edit Save target → submit a magnet → torrent goes to the edited path (confirm by `ls` against the path).
- Pause a torrent — its row's "paused" indicator (the gray dot) should now stick across `torrents:tick` updates. Was previously always reverting because `Snapshot.Paused` was hardcoded `false`.

- [ ] **Step 3: Tag**

```bash
git tag plan-4b-settings-complete
git push origin main
git push origin plan-4b-settings-complete
```

---

**End of Plan 4b.**
