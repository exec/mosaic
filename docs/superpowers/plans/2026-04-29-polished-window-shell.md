# Mosaic — Plan 2: Polished Window Shell + Main List

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Take Plan 1's working app and dress it like Linear meets Transmission Mac. Icon rail, filter rail skeleton, top toolbar, status bar, dual-mode (cards/table) main list, theme system, accessible Kobalte primitives, motion. After this plan the app *looks* like the design spec — torrent inspection (Plan 3) and category management (Plan 4) aren't yet wired but the chrome is finished.

**Architecture:** All frontend. The backend gains one small addition (a `Stats` method on the API service for the status bar), nothing structural. The frontend gets a real component hierarchy: a `ThemeProvider` at top, `WindowShell` decomposing into IconRail / FilterRail / TopToolbar / Main / StatusBar, with TorrentList orchestrating between TorrentCard (default) and TorrentTable (TanStack). Kobalte handles all menus, dropdowns, dialogs, and tooltips so we don't reinvent accessibility. Motion is declarative via Motion One's Solid bindings.

**Tech additions:**
- `@kobalte/core` — headless accessible primitives
- `@tanstack/solid-table` + `@tanstack/solid-virtual` — table mode (and ready for Plan 3's peer/file tables)
- `lucide-solid` — icons
- `solid-sonner` — toast notifications
- Inter Variable + JetBrains Mono — self-hosted

(Motion One was considered but Plan 2 is fine on Kobalte's built-in data-state animations + CSS keyframes. Adding `@motionone/solid` is deferred to Plan 3 when the inspector slide-in needs spring-quality motion.)

**Reference spec:** `docs/superpowers/specs/2026-04-28-bittorrent-client-design.md` §4.2, §4.3, §4.5, §4.7.

**Aesthetic principles** (apply to every visual choice in this plan):
- Single accent: electric violet `oklch(0.65 0.25 290)`. Used sparingly — active states, primary buttons, focus rings, progress fill. Never decorative.
- Backgrounds: gradient near-black (`from-zinc-950 to-zinc-900`), never flat.
- Surfaces: subtle glass — `bg-white/[.02] backdrop-blur-sm border border-white/[.06]`.
- Numerals: `tabular-nums font-mono` for every rate/size/percentage.
- Status colors: emerald (seeding), violet (downloading), amber (paused), red (error), zinc (idle).
- Motion: 150 ms feedback / 300 ms layout / `cubic-bezier(0.32, 0.72, 0, 1)`. No springs (too playful for this app).
- Typography: Inter Variable for UI; JetBrains Mono for hashes, magnets, ports.

---

## Out of Scope (deferred to later plans)

- **Right inspector with 5 tabs (Overview/Files/Peers/Trackers/Speed)** — Plan 3
- **Polished add-modal with file tree, category, tags** — Plan 3
- **Subscription-driven IPC (`Watch(id, tabs[])`)** — Plan 3
- **Categories/Tags/Trackers data wiring** in the filter rail (sections render as "Coming soon" placeholders) — Plan 4
- **Alt-speed limits real implementation** — Plan 4 (button is visible but inert in Plan 2)
- **Settings page** — multiple later plans (button is visible but inert in Plan 2)
- **Magnet/.torrent drag-drop onto the OS dock icon** — Plan 6 (deep-link / single-instance)

---

## File Structure (final state at end of plan)

```
frontend/src/
├── App.tsx                              # ThemeProvider → WindowShell
├── index.tsx                            # render entry (unchanged)
├── index.css                            # base body/html, font @font-face decls
├── lib/
│   ├── bindings.ts                      # adds Stats() binding
│   ├── store.ts                         # adds selection state, density mode, view filter
│   ├── theme.ts                         # ThemeProvider + theme types
│   └── format.ts                        # fmtBytes/fmtRate/fmtETA — extracted from TorrentList
├── components/
│   ├── shell/
│   │   ├── WindowShell.tsx              # composes regions
│   │   ├── IconRail.tsx                 # left 48px nav
│   │   ├── FilterRail.tsx               # 240px filter sidebar
│   │   ├── TopToolbar.tsx               # search + add buttons + density + theme + settings
│   │   ├── StatusBar.tsx                # bottom 28px live stats
│   │   └── DropZone.tsx                 # window-wide drag target
│   ├── list/
│   │   ├── TorrentList.tsx              # orchestrator (cards vs table)
│   │   ├── TorrentCard.tsx              # card mode row
│   │   ├── TorrentTable.tsx             # TanStack table
│   │   ├── TorrentRowMenu.tsx           # Kobalte ContextMenu
│   │   └── EmptyState.tsx               # illustrated empty state
│   ├── theme/
│   │   ├── ThemeProvider.tsx            # context + system detection
│   │   └── ThemeToggle.tsx              # segmented Dark/Light/System
│   ├── ui/
│   │   ├── Button.tsx                   # primary/secondary/ghost variants
│   │   ├── DropdownMenu.tsx             # Kobalte wrapper, themed
│   │   ├── ContextMenu.tsx              # Kobalte wrapper, themed
│   │   ├── Tooltip.tsx                  # Kobalte wrapper, themed
│   │   ├── ToggleGroup.tsx              # Kobalte wrapper, themed
│   │   └── ProgressBar.tsx              # gradient fill with optional shimmer
│   └── AddMagnetModal.tsx               # carried over from Plan 1, restyled
└── (existing) wailsjs/                  # generated, gitignored
```

Backend gets one new method:
```
backend/api/service.go      # adds GlobalStats() for status bar
backend/api/service_test.go # adds GlobalStats test
app.go                      # binds GlobalStats
```

---

## Task 1: Install frontend deps

**Files:** `frontend/package.json` (modified)

- [ ] **Step 1: Install runtime deps**

```bash
cd frontend
npm install @kobalte/core @tanstack/solid-table @tanstack/solid-virtual lucide-solid solid-sonner
```

Expected: package.json adds those five packages under `dependencies`.

- [ ] **Step 2: Verify build still works**

```bash
cd frontend && npm run build
```

Expected: exit 0. Bundle size grows from ~19 KB to ~50 KB JS.

- [ ] **Step 3: Commit**

```bash
git add frontend/package.json frontend/package-lock.json
git commit -m "chore(frontend): add Kobalte + TanStack + Lucide + Sonner"
```

---

## Task 2: Self-host Inter + JetBrains Mono fonts

**Files:**
- Create: `frontend/src/assets/fonts/Inter-Variable.woff2`
- Create: `frontend/src/assets/fonts/JetBrainsMono-Variable.ttf`
- Modify: `frontend/src/index.css`

> Self-hosted to keep the app offline-capable and avoid Google Fonts dependency.

- [ ] **Step 1: Download font files**

```bash
cd frontend/src/assets/fonts
curl -L -o Inter-Variable.woff2 https://github.com/rsms/inter/raw/v4.0/docs/font-files/InterVariable.woff2
curl -L -o JetBrainsMono-Variable.ttf https://github.com/JetBrains/JetBrainsMono/raw/v2.304/fonts/variable/JetBrainsMono%5Bwght%5D.ttf
```

Expected: an `Inter-Variable.woff2` (~340 KB) and a `JetBrainsMono-Variable.ttf` (~300 KB). v2.304 of JetBrainsMono only ships variable fonts as TTF; verify with `file frontend/src/assets/fonts/JetBrainsMono-Variable.ttf` (should report TrueType, not HTML — `curl -L` will silently write a 404 HTML page if the URL is wrong).

- [ ] **Step 2: Add @font-face declarations to `frontend/src/index.css`**

Replace the file with:
```css
@import "tailwindcss";

@font-face {
  font-family: "Inter";
  font-weight: 100 900;
  font-style: normal;
  font-display: swap;
  src: url("./assets/fonts/Inter-Variable.woff2") format("woff2-variations");
}

@font-face {
  font-family: "JetBrains Mono";
  font-weight: 100 800;
  font-style: normal;
  font-display: swap;
  src: url("./assets/fonts/JetBrainsMono-Variable.ttf") format("truetype-variations");
}

:root {
  color-scheme: dark;
}

html, body, #app {
  height: 100%;
  margin: 0;
  font-family: "Inter", ui-sans-serif, system-ui, -apple-system, "Segoe UI", Roboto, sans-serif;
  background: linear-gradient(180deg, #0a0a0c 0%, #0f0f12 100%);
  color: #e7e7e9;
  font-feature-settings: "cv11", "ss01", "ss03"; /* Inter alternates */
  -webkit-font-smoothing: antialiased;
}

/* Tabular numerals helper (Tailwind also has this; this is a fallback selector) */
.tabular-nums { font-variant-numeric: tabular-nums; }
```

(Color tokens, font-family vars, and the easing curve are declared inside the `@theme` block in Task 3 — that's the Tailwind v4 way to wire design tokens into utility classes.)

- [ ] **Step 3: Verify fonts resolve**

```bash
cd frontend && npm run build
```

Expected: exit 0. dist/assets contains the two woff2 files (Vite copies them by import).

- [ ] **Step 4: Commit**

```bash
git add frontend/src/assets/fonts/ frontend/src/index.css
git commit -m "feat(frontend): self-host Inter + JetBrains Mono variable fonts"
```

---

## Task 3: Tailwind theme extension

**Files:** Create `frontend/tailwind.config.css` (Tailwind v4 uses `@theme` blocks via CSS, but a config file isn't needed — the variables in index.css handle it). Verify the existing setup picks up the OKLCH variables.

> Tailwind v4 reads `--color-*` CSS variables automatically. `bg-accent-500` will Just Work referring to `--color-accent-500`. No tailwind.config.js needed.

- [ ] **Step 1: Append `@theme` block to `frontend/src/index.css`**

Add at the end of the file (after the existing content):
```css
@theme {
  --font-sans: "Inter", ui-sans-serif, system-ui, -apple-system, "Segoe UI", Roboto, sans-serif;
  --font-mono: "JetBrains Mono", ui-monospace, SFMono-Regular, Menlo, monospace;

  --color-accent-50:   oklch(0.97 0.02 290);
  --color-accent-200:  oklch(0.85 0.10 290);
  --color-accent-400:  oklch(0.75 0.18 290);
  --color-accent-500:  oklch(0.65 0.25 290);
  --color-accent-600:  oklch(0.55 0.25 290);
  --color-accent-700:  oklch(0.45 0.22 290);

  --color-seed:    oklch(0.78 0.18 155);
  --color-down:    oklch(0.65 0.25 290);
  --color-paused:  oklch(0.78 0.16 75);
  --color-fail:    oklch(0.65 0.24 25);

  --ease-app: cubic-bezier(0.32, 0.72, 0, 1);
}
```

- [ ] **Step 2: Verify Tailwind picks up the classes**

Add a temporary test class to `frontend/src/App.tsx`'s root div: `class="h-full flex flex-col bg-accent-500"`. Run `npm run build`, then revert the class. The build should succeed (proving the class is valid).

- [ ] **Step 3: Commit**

```bash
git add frontend/src/index.css
git commit -m "feat(frontend): theme tokens (accent palette, status colors, easing)"
```

---

## Task 4: Format helpers — failing tests

**Files:**
- Test: `frontend/src/lib/format.test.ts`

> Vitest is already a transitive dep via Vite; install it if not present.

- [ ] **Step 1: Add Vitest + happy-dom**

```bash
cd frontend
npm install -D vitest@^2 happy-dom
```

Pin vitest to v2 — vitest@4+ bundles Vite 8 internally, which crashes against the project's pinned vite-plugin-solid@2.11 (uses Vite 5/6 API: "TypeError: defaultServerConditions is not iterable"). happy-dom is required up-front because vite-plugin-solid auto-injects a DOM environment into vitest's resolved config — pure-logic tests won't run without one. happy-dom is faster than jsdom and has fewer compatibility surprises with vitest 2.

Note: vitest@2 + vite-plugin-solid does NOT expose a working `localStorage` global under either jsdom or happy-dom (only a plain Object stub without Storage's prototype methods). A small `frontend/test/setup.ts` polyfill is required and is wired up via vite.config.ts's `test.setupFiles` — see Task 7's setup.ts.

- [ ] **Step 2: Add test script to `frontend/package.json`**

In `scripts`, add: `"test": "vitest run --environment happy-dom"`.

- [ ] **Step 3: Write failing tests**

Create `frontend/src/lib/format.test.ts`:
```ts
import {describe, expect, test} from 'vitest';
import {fmtBytes, fmtRate, fmtETA, fmtPercent} from './format';

describe('fmtBytes', () => {
  test('bytes', () => expect(fmtBytes(0)).toBe('0 B'));
  test('kilobytes', () => expect(fmtBytes(1500)).toBe('1.5 KB'));
  test('megabytes', () => expect(fmtBytes(1_572_864)).toBe('1.5 MB'));
  test('gigabytes', () => expect(fmtBytes(1_610_612_736)).toBe('1.50 GB'));
});

describe('fmtRate', () => {
  test('idle', () => expect(fmtRate(0)).toBe('—'));
  test('active', () => expect(fmtRate(1500)).toBe('1.5 KB/s'));
});

describe('fmtETA', () => {
  test('infinite when zero rate', () => expect(fmtETA(1000, 0)).toBe('∞'));
  test('seconds', () => expect(fmtETA(500, 100)).toBe('5s'));
  test('minutes', () => expect(fmtETA(60_000, 100)).toBe('10m'));
  test('hours', () => expect(fmtETA(3_600_000, 100)).toBe('10h'));
  test('days', () => expect(fmtETA(86_400_000, 100)).toBe('10d'));
});

describe('fmtPercent', () => {
  test('zero', () => expect(fmtPercent(0)).toBe('0.0%'));
  test('partial', () => expect(fmtPercent(0.7234)).toBe('72.3%'));
  test('complete', () => expect(fmtPercent(1)).toBe('100%'));
});
```

- [ ] **Step 4: Run tests, confirm they fail**

```bash
cd frontend && npm test
```

Expected: FAIL with "Cannot find module './format'" or "fmtBytes is not a function".

---

## Task 5: Format helpers — implementation

**Files:** Create `frontend/src/lib/format.ts`

- [ ] **Step 1: Write the implementation**

```ts
export function fmtBytes(n: number): string {
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
  if (n < 1024 * 1024 * 1024) return `${(n / 1024 / 1024).toFixed(1)} MB`;
  return `${(n / 1024 / 1024 / 1024).toFixed(2)} GB`;
}

export function fmtRate(bytesPerSec: number): string {
  if (bytesPerSec === 0) return '—';
  return `${fmtBytes(bytesPerSec)}/s`;
}

export function fmtETA(remainingBytes: number, bytesPerSec: number): string {
  if (bytesPerSec === 0) return '∞';
  const seconds = remainingBytes / bytesPerSec;
  if (seconds < 60) return `${Math.round(seconds)}s`;
  if (seconds < 3600) return `${Math.round(seconds / 60)}m`;
  if (seconds < 86_400) return `${Math.round(seconds / 3600)}h`;
  return `${Math.round(seconds / 86_400)}d`;
}

export function fmtPercent(progress: number): string {
  if (progress >= 1) return '100%';
  return `${(progress * 100).toFixed(1)}%`;
}
```

- [ ] **Step 2: Run tests, confirm they pass**

```bash
cd frontend && npm test
```

Expected: PASS (15 tests).

- [ ] **Step 3: Commit**

```bash
git add frontend/src/lib/format.ts frontend/src/lib/format.test.ts frontend/package.json frontend/package-lock.json
git commit -m "feat(frontend): format helpers (bytes/rate/ETA/percent) with vitest"
```

---

## Task 6: Theme provider — failing test

**Files:** Test: `frontend/src/lib/theme.test.ts`

- [ ] **Step 1: Write failing test**

```ts
import {describe, expect, test, beforeEach} from 'vitest';
import {resolveTheme, loadStoredTheme, storeTheme, type Theme} from './theme';

describe('theme storage', () => {
  beforeEach(() => localStorage.clear());

  test('default is system when none stored', () => {
    expect(loadStoredTheme()).toBe('system');
  });

  test('persists chosen theme', () => {
    storeTheme('dark');
    expect(loadStoredTheme()).toBe('dark');
  });

  test('rejects invalid stored values', () => {
    localStorage.setItem('mosaic.theme', 'not-a-theme');
    expect(loadStoredTheme()).toBe('system');
  });
});

describe('resolveTheme', () => {
  test('passes through dark', () => expect(resolveTheme('dark', false)).toBe('dark'));
  test('passes through light', () => expect(resolveTheme('light', false)).toBe('light'));
  test('system → dark when system prefers dark', () => expect(resolveTheme('system', true)).toBe('dark'));
  test('system → light when system does not prefer dark', () => expect(resolveTheme('system', false)).toBe('light'));
});
```

- [ ] **Step 2: Verify vitest is configured for happy-dom**

The test script in `frontend/package.json` should already be `"vitest run --environment happy-dom"` from Task 4 Step 2, and `happy-dom` should already be installed (Task 4 Step 1). No action needed here unless one of those is missing.

- [ ] **Step 3: Run tests, confirm they fail**

Expected: FAIL with "Cannot find module './theme'".

---

## Task 7: Theme provider — implementation

**Files:**
- Create: `frontend/src/lib/theme.ts`
- Create: `frontend/src/components/theme/ThemeProvider.tsx`

- [ ] **Step 1: Write `lib/theme.ts`**

```ts
export type Theme = 'dark' | 'light' | 'system';
export type ResolvedTheme = 'dark' | 'light';

const STORAGE_KEY = 'mosaic.theme';
const VALID: Theme[] = ['dark', 'light', 'system'];

export function loadStoredTheme(): Theme {
  const v = localStorage.getItem(STORAGE_KEY);
  return VALID.includes(v as Theme) ? (v as Theme) : 'system';
}

export function storeTheme(theme: Theme): void {
  localStorage.setItem(STORAGE_KEY, theme);
}

export function resolveTheme(theme: Theme, systemPrefersDark: boolean): ResolvedTheme {
  if (theme === 'dark') return 'dark';
  if (theme === 'light') return 'light';
  return systemPrefersDark ? 'dark' : 'light';
}
```

- [ ] **Step 2: Run tests, confirm they pass**

Expected: PASS (7 tests across format + theme = 22 total).

- [ ] **Step 3: Write the provider**

Create `frontend/src/components/theme/ThemeProvider.tsx`:
```tsx
import {createContext, createSignal, createEffect, useContext, type JSX, type Accessor} from 'solid-js';
import {loadStoredTheme, storeTheme, resolveTheme, type Theme, type ResolvedTheme} from '../../lib/theme';

type ThemeContextValue = {
  theme: Accessor<Theme>;
  resolved: Accessor<ResolvedTheme>;
  setTheme: (t: Theme) => void;
};

const ThemeContext = createContext<ThemeContextValue>();

export function ThemeProvider(props: {children: JSX.Element}) {
  const [theme, setThemeSignal] = createSignal<Theme>(loadStoredTheme());

  const mq = window.matchMedia('(prefers-color-scheme: dark)');
  const [systemDark, setSystemDark] = createSignal(mq.matches);
  mq.addEventListener('change', (e) => setSystemDark(e.matches));

  const resolved = () => resolveTheme(theme(), systemDark());

  createEffect(() => {
    const r = resolved();
    document.documentElement.dataset.theme = r;
    document.documentElement.style.colorScheme = r;
  });

  const setTheme = (t: Theme) => {
    setThemeSignal(t);
    storeTheme(t);
  };

  return (
    <ThemeContext.Provider value={{theme, resolved, setTheme}}>
      {props.children}
    </ThemeContext.Provider>
  );
}

export function useTheme(): ThemeContextValue {
  const ctx = useContext(ThemeContext);
  if (!ctx) throw new Error('useTheme must be used inside <ThemeProvider>');
  return ctx;
}
```

- [ ] **Step 4: Verify build**

```bash
cd frontend && npm run build
```

Expected: exit 0.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/lib/theme.ts frontend/src/lib/theme.test.ts frontend/src/components/theme/ThemeProvider.tsx
git commit -m "feat(frontend): theme provider with system detection + persistence"
```

---

## Task 8: ThemeToggle component

**Files:** Create `frontend/src/components/theme/ThemeToggle.tsx`

- [ ] **Step 1: Write the component**

```tsx
import {ToggleGroup} from '@kobalte/core/toggle-group';
import {Monitor, Moon, Sun} from 'lucide-solid';
import {useTheme} from './ThemeProvider';
import type {Theme} from '../../lib/theme';

const items: {value: Theme; label: string; icon: typeof Sun}[] = [
  {value: 'light', label: 'Light', icon: Sun},
  {value: 'dark', label: 'Dark', icon: Moon},
  {value: 'system', label: 'System', icon: Monitor},
];

export function ThemeToggle() {
  const {theme, setTheme} = useTheme();

  return (
    <ToggleGroup
      class="inline-flex items-center rounded-md border border-white/10 bg-white/[.02] p-0.5"
      value={theme()}
      onChange={(v) => v && setTheme(v as Theme)}
    >
      {items.map((item) => (
        <ToggleGroup.Item
          value={item.value}
          aria-label={item.label}
          class="grid h-7 w-7 place-items-center rounded text-zinc-400 transition-colors duration-150 data-[pressed]:bg-white/10 data-[pressed]:text-zinc-100 hover:text-zinc-100"
        >
          <item.icon class="h-3.5 w-3.5" />
        </ToggleGroup.Item>
      ))}
    </ToggleGroup>
  );
}
```

- [ ] **Step 2: Verify build**

```bash
cd frontend && npm run build
```

Expected: exit 0.

- [ ] **Step 3: Commit**

```bash
git add frontend/src/components/theme/ThemeToggle.tsx
git commit -m "feat(frontend): ThemeToggle segmented control (Kobalte ToggleGroup)"
```

---

## Task 9: Shared UI primitives — Button + Tooltip + DropdownMenu + ContextMenu + ToggleGroup + ProgressBar

**Files:** Create:
- `frontend/src/components/ui/Button.tsx`
- `frontend/src/components/ui/Tooltip.tsx`
- `frontend/src/components/ui/DropdownMenu.tsx`
- `frontend/src/components/ui/ContextMenu.tsx`
- `frontend/src/components/ui/ProgressBar.tsx`

> These are thin themed wrappers over Kobalte. We own the styling so every menu/dropdown/tooltip in the app shares identical chrome.

- [ ] **Step 1: Button**

`frontend/src/components/ui/Button.tsx`:
```tsx
import {splitProps, type ComponentProps} from 'solid-js';

type Variant = 'primary' | 'secondary' | 'ghost' | 'danger';

const styles: Record<Variant, string> = {
  primary:   'bg-accent-500 text-white shadow-sm hover:bg-accent-400 focus-visible:ring-accent-400',
  secondary: 'border border-white/10 bg-white/[.02] text-zinc-100 hover:bg-white/[.04] focus-visible:ring-white/30',
  ghost:     'text-zinc-300 hover:bg-white/[.04] hover:text-zinc-100 focus-visible:ring-white/30',
  danger:    'bg-rose-600 text-white hover:bg-rose-500 focus-visible:ring-rose-400',
};

export function Button(props: ComponentProps<'button'> & {variant?: Variant}) {
  const [local, rest] = splitProps(props, ['variant', 'class', 'children']);
  const variant = local.variant ?? 'secondary';
  return (
    <button
      {...rest}
      class={`inline-flex items-center justify-center gap-1.5 rounded-md px-3 py-1.5 text-sm font-medium transition-colors duration-150 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-offset-2 focus-visible:ring-offset-zinc-950 disabled:opacity-50 disabled:pointer-events-none ${styles[variant]} ${local.class ?? ''}`}
    >
      {local.children}
    </button>
  );
}
```

- [ ] **Step 2: Tooltip**

`frontend/src/components/ui/Tooltip.tsx`:
```tsx
import {Tooltip as KTooltip} from '@kobalte/core/tooltip';
import type {JSX} from 'solid-js';

export function Tooltip(props: {label: string; children: JSX.Element; placement?: 'top' | 'right' | 'bottom' | 'left'}) {
  return (
    <KTooltip placement={props.placement ?? 'top'} openDelay={200} closeDelay={0}>
      <KTooltip.Trigger as="span">{props.children}</KTooltip.Trigger>
      <KTooltip.Portal>
        <KTooltip.Content class="z-50 rounded-md border border-white/10 bg-zinc-900/95 px-2 py-1 text-xs text-zinc-100 shadow-lg backdrop-blur-md animate-in fade-in zoom-in-95">
          {props.label}
          <KTooltip.Arrow class="fill-zinc-900/95" />
        </KTooltip.Content>
      </KTooltip.Portal>
    </KTooltip>
  );
}
```

- [ ] **Step 3: DropdownMenu**

`frontend/src/components/ui/DropdownMenu.tsx`:
```tsx
import {DropdownMenu as KDropdownMenu} from '@kobalte/core/dropdown-menu';
import type {JSX} from 'solid-js';

const contentClass = 'z-50 min-w-[180px] rounded-md border border-white/10 bg-zinc-900/95 p-1 text-sm shadow-2xl backdrop-blur-md animate-in fade-in zoom-in-95';
const itemClass = 'flex cursor-pointer items-center gap-2 rounded px-2 py-1.5 text-zinc-200 outline-none transition-colors duration-100 data-[highlighted]:bg-white/10 data-[disabled]:opacity-40 data-[disabled]:pointer-events-none';

export const DropdownMenu = Object.assign(
  function (props: {trigger: JSX.Element; children: JSX.Element}) {
    return (
      <KDropdownMenu>
        <KDropdownMenu.Trigger as="span">{props.trigger}</KDropdownMenu.Trigger>
        <KDropdownMenu.Portal>
          <KDropdownMenu.Content class={contentClass}>{props.children}</KDropdownMenu.Content>
        </KDropdownMenu.Portal>
      </KDropdownMenu>
    );
  },
  {
    Item: (props: {children: JSX.Element; onSelect?: () => void; disabled?: boolean}) => (
      <KDropdownMenu.Item class={itemClass} onSelect={props.onSelect} disabled={props.disabled}>
        {props.children}
      </KDropdownMenu.Item>
    ),
    Separator: () => <KDropdownMenu.Separator class="my-1 h-px bg-white/10" />,
  },
);
```

- [ ] **Step 4: ContextMenu** (mirror of DropdownMenu, attached to right-click)

`frontend/src/components/ui/ContextMenu.tsx`:
```tsx
import {ContextMenu as KContextMenu} from '@kobalte/core/context-menu';
import type {JSX} from 'solid-js';

const contentClass = 'z-50 min-w-[200px] rounded-md border border-white/10 bg-zinc-900/95 p-1 text-sm shadow-2xl backdrop-blur-md animate-in fade-in zoom-in-95';
const itemClass = 'flex cursor-pointer items-center gap-2 rounded px-2 py-1.5 text-zinc-200 outline-none transition-colors duration-100 data-[highlighted]:bg-white/10 data-[disabled]:opacity-40 data-[disabled]:pointer-events-none';

export const ContextMenu = Object.assign(
  function (props: {trigger: JSX.Element; children: JSX.Element}) {
    return (
      <KContextMenu>
        <KContextMenu.Trigger as="div">{props.trigger}</KContextMenu.Trigger>
        <KContextMenu.Portal>
          <KContextMenu.Content class={contentClass}>{props.children}</KContextMenu.Content>
        </KContextMenu.Portal>
      </KContextMenu>
    );
  },
  {
    Item: (props: {children: JSX.Element; onSelect?: () => void; disabled?: boolean; danger?: boolean}) => (
      <KContextMenu.Item
        class={`${itemClass} ${props.danger ? 'data-[highlighted]:bg-rose-500/20 data-[highlighted]:text-rose-300' : ''}`}
        onSelect={props.onSelect}
        disabled={props.disabled}
      >
        {props.children}
      </KContextMenu.Item>
    ),
    Separator: () => <KContextMenu.Separator class="my-1 h-px bg-white/10" />,
  },
);
```

- [ ] **Step 5: ProgressBar**

`frontend/src/components/ui/ProgressBar.tsx`:
```tsx
import {Show} from 'solid-js';

type Props = {
  value: number; // 0..1
  active?: boolean; // when true, adds shimmer
};

export function ProgressBar(props: Props) {
  return (
    <div class="relative h-1.5 overflow-hidden rounded-full bg-white/[.04]">
      <div
        class="absolute inset-y-0 left-0 rounded-full bg-gradient-to-r from-accent-600 to-accent-400 transition-[width] duration-500 ease-[var(--ease-app)]"
        style={{width: `${Math.min(100, props.value * 100).toFixed(2)}%`}}
      />
      <Show when={props.active && props.value > 0 && props.value < 1}>
        <div
          class="absolute inset-y-0 w-1/3 animate-[shimmer_2s_ease-in-out_infinite] bg-gradient-to-r from-transparent via-white/15 to-transparent"
          style={{left: '-33%'}}
        />
      </Show>
    </div>
  );
}
```

Add the keyframe to `frontend/src/index.css` (append at end):
```css
@keyframes shimmer {
  0% { transform: translateX(0); }
  100% { transform: translateX(400%); }
}
```

- [ ] **Step 6: Verify build**

```bash
cd frontend && npm run build
```

Expected: exit 0.

- [ ] **Step 7: Commit**

```bash
git add frontend/src/components/ui/ frontend/src/index.css
git commit -m "feat(frontend): UI primitives (Button, Tooltip, DropdownMenu, ContextMenu, ProgressBar)"
```

---

## Task 10: Backend — GlobalStats() failing test

**Files:** Test: `backend/api/service_test.go` (modify — add a new test function)

- [ ] **Step 1: Add failing test**

Append to `backend/api/service_test.go`:
```go
func TestService_GlobalStats(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	// Empty state
	stats, err := svc.GlobalStats(ctx)
	require.NoError(t, err)
	require.Equal(t, 0, stats.TotalTorrents)
	require.Equal(t, 0, stats.ActiveTorrents)
	require.Equal(t, 0, stats.SeedingTorrents)

	// Add two torrents, one paused
	id1, _ := svc.AddMagnet(ctx, "magnet:?xt=urn:btih:abc", "")
	_, _ = svc.AddMagnet(ctx, "magnet:?xt=urn:btih:def", "")
	require.NoError(t, svc.Pause(id1))

	stats, err = svc.GlobalStats(ctx)
	require.NoError(t, err)
	require.Equal(t, 2, stats.TotalTorrents)
}
```

- [ ] **Step 2: Run test, confirm fails**

```bash
go test ./backend/api/ -run TestService_GlobalStats -v
```

Expected: FAIL with "service.GlobalStats undefined".

---

## Task 11: Backend — GlobalStats() implementation

**Files:** Modify `backend/api/service.go`

- [ ] **Step 1: Add the method**

Append to `backend/api/service.go`:
```go
// GlobalStats is the snapshot displayed in the status bar.
type GlobalStats struct {
	TotalTorrents      int   `json:"total_torrents"`
	ActiveTorrents     int   `json:"active_torrents"`
	SeedingTorrents    int   `json:"seeding_torrents"`
	TotalDownloadRate  int64 `json:"total_download_rate"`
	TotalUploadRate    int64 `json:"total_upload_rate"`
	TotalPeers         int   `json:"total_peers"`
}

func (s *Service) GlobalStats(ctx context.Context) (GlobalStats, error) {
	snaps := s.engine.List()
	var st GlobalStats
	st.TotalTorrents = len(snaps)
	for _, snap := range snaps {
		if !snap.Paused && !snap.Completed {
			st.ActiveTorrents++
		}
		if snap.Completed {
			st.SeedingTorrents++
		}
		st.TotalDownloadRate += snap.DownloadRate
		st.TotalUploadRate += snap.UploadRate
		st.TotalPeers += snap.Peers
	}
	return st, nil
}
```

- [ ] **Step 2: Run tests, confirm pass**

```bash
go test ./backend/api/ -v -race
```

Expected: PASS.

- [ ] **Step 3: Bind it on App**

Modify `app.go` — add method:
```go
func (a *App) GlobalStats() (api.GlobalStats, error) {
	return a.svc.GlobalStats(a.ctx)
}
```

Also extend the streamTicks goroutine to emit `stats:tick` every 1s alongside the existing 500ms torrents:tick. Replace `streamTicks` with:
```go
func (a *App) streamTicks(ctx context.Context) {
	torrents := time.NewTicker(500 * time.Millisecond)
	stats := time.NewTicker(1 * time.Second)
	defer torrents.Stop()
	defer stats.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-torrents.C:
			rows, err := a.svc.ListTorrents(ctx)
			if err != nil {
				log.Error().Err(err).Msg("list torrents during tick")
				continue
			}
			wailsruntime.EventsEmit(ctx, "torrents:tick", rows)
		case <-stats.C:
			s, err := a.svc.GlobalStats(ctx)
			if err != nil {
				log.Error().Err(err).Msg("global stats during tick")
				continue
			}
			wailsruntime.EventsEmit(ctx, "stats:tick", s)
		}
	}
}
```

- [ ] **Step 4: Regenerate bindings**

```bash
$(go env GOPATH)/bin/wails generate module
```

Verify `frontend/wailsjs/go/main/App.d.ts` now exports `GlobalStats`.

- [ ] **Step 5: Verify build**

```bash
go build ./...
```

Expected: exit 0.

- [ ] **Step 6: Commit**

```bash
git add backend/api/service.go backend/api/service_test.go app.go
git commit -m "feat(api): GlobalStats() for status bar (1s tick alongside 500ms torrents tick)"
```

---

## Task 12: Bindings + store updates

**Files:**
- Modify: `frontend/src/lib/bindings.ts`
- Modify: `frontend/src/lib/store.ts`

- [ ] **Step 1: Extend bindings.ts**

Replace `frontend/src/lib/bindings.ts`:
```ts
import {AddMagnet, GlobalStats, ListTorrents, Pause, PickAndAddTorrent, Remove, Resume} from '../../wailsjs/go/main/App';
import {EventsOn} from '../../wailsjs/runtime/runtime';

export type Torrent = {
  id: string;
  name: string;
  magnet: string;
  save_path: string;
  total_bytes: number;
  bytes_done: number;
  progress: number;
  download_rate: number;
  upload_rate: number;
  peers: number;
  seeds: number;
  paused: boolean;
  completed: boolean;
  added_at: number;
};

export type GlobalStatsT = {
  total_torrents: number;
  active_torrents: number;
  seeding_torrents: number;
  total_download_rate: number;
  total_upload_rate: number;
  total_peers: number;
};

export const api = {
  addMagnet: (magnet: string) => AddMagnet(magnet),
  pickAndAddTorrent: () => PickAndAddTorrent(),
  listTorrents: () => ListTorrents() as Promise<Torrent[]>,
  globalStats: () => GlobalStats() as Promise<GlobalStatsT>,
  pause: (id: string) => Pause(id),
  resume: (id: string) => Resume(id),
  remove: (id: string, deleteFiles: boolean) => Remove(id, deleteFiles),
};

export function onTorrentsTick(handler: (rows: Torrent[]) => void): () => void {
  return EventsOn('torrents:tick', handler);
}

export function onStatsTick(handler: (stats: GlobalStatsT) => void): () => void {
  return EventsOn('stats:tick', handler);
}
```

- [ ] **Step 2: Extend store.ts**

Replace `frontend/src/lib/store.ts`:
```ts
import {createStore, produce} from 'solid-js/store';
import {api, onStatsTick, onTorrentsTick, type GlobalStatsT, type Torrent} from './bindings';

export type Density = 'cards' | 'table';
export type StatusFilter = 'all' | 'downloading' | 'seeding' | 'completed' | 'paused' | 'errored';

export type AppState = {
  torrents: Torrent[];
  stats: GlobalStatsT;
  selection: Set<string>;
  density: Density;
  statusFilter: StatusFilter;
  searchQuery: string;
  loading: boolean;
};

const DENSITY_KEY = 'mosaic.density';

function loadDensity(): Density {
  return (localStorage.getItem(DENSITY_KEY) as Density) === 'table' ? 'table' : 'cards';
}

const emptyStats: GlobalStatsT = {
  total_torrents: 0,
  active_torrents: 0,
  seeding_torrents: 0,
  total_download_rate: 0,
  total_upload_rate: 0,
  total_peers: 0,
};

export function createTorrentsStore() {
  const [state, setState] = createStore<AppState>({
    torrents: [],
    stats: emptyStats,
    selection: new Set(),
    density: loadDensity(),
    statusFilter: 'all',
    searchQuery: '',
    loading: true,
  });

  api.listTorrents()
    .then((rows) => setState({torrents: rows, loading: false}))
    .catch((e) => { console.error(e); setState({loading: false}); });

  api.globalStats().then((s) => setState({stats: s})).catch(console.error);

  const offT = onTorrentsTick((rows) => setState(produce((s) => { s.torrents = rows; })));
  const offS = onStatsTick((stats) => setState(produce((s) => { s.stats = stats; })));

  return {
    state,
    addMagnet: (m: string) => api.addMagnet(m),
    pickAndAddTorrent: () => api.pickAndAddTorrent(),
    pause: (id: string) => api.pause(id),
    resume: (id: string) => api.resume(id),
    remove: (id: string, deleteFiles: boolean) => api.remove(id, deleteFiles),

    // Selection
    select: (id: string) => setState(produce((s) => { s.selection = new Set([id]); })),
    toggleSelect: (id: string) => setState(produce((s) => {
      const next = new Set(s.selection);
      if (next.has(id)) next.delete(id); else next.add(id);
      s.selection = next;
    })),
    extendSelectTo: (id: string) => setState(produce((s) => {
      // Range select: from last-selected to id within the current visible list order.
      const visible = s.torrents.map((t) => t.id);
      const last = [...s.selection].pop();
      if (!last) { s.selection = new Set([id]); return; }
      const a = visible.indexOf(last);
      const b = visible.indexOf(id);
      if (a < 0 || b < 0) { s.selection = new Set([id]); return; }
      const [lo, hi] = a < b ? [a, b] : [b, a];
      s.selection = new Set(visible.slice(lo, hi + 1));
    })),
    selectAll: () => setState(produce((s) => { s.selection = new Set(s.torrents.map((t) => t.id)); })),
    clearSelection: () => setState(produce((s) => { s.selection = new Set(); })),

    // View
    setDensity: (d: Density) => {
      localStorage.setItem(DENSITY_KEY, d);
      setState(produce((s) => { s.density = d; }));
    },
    setStatusFilter: (f: StatusFilter) => setState(produce((s) => { s.statusFilter = f; })),
    setSearchQuery: (q: string) => setState(produce((s) => { s.searchQuery = q; })),

    dispose: () => { offT(); offS(); },
  };
}

export function filterTorrents(rows: Torrent[], status: StatusFilter, query: string): Torrent[] {
  let out = rows;
  if (status !== 'all') {
    out = out.filter((t) => {
      switch (status) {
        case 'downloading': return !t.paused && !t.completed;
        case 'seeding':     return t.completed && !t.paused;
        case 'completed':   return t.completed;
        case 'paused':      return t.paused;
        case 'errored':     return false; // wired in Plan 5 when errors are surfaced
      }
    });
  }
  if (query.trim()) {
    const q = query.toLowerCase();
    out = out.filter((t) => t.name.toLowerCase().includes(q));
  }
  return out;
}
```

- [ ] **Step 3: Verify build**

```bash
cd frontend && npm run build
```

Expected: exit 0.

- [ ] **Step 4: Commit**

```bash
git add frontend/src/lib/bindings.ts frontend/src/lib/store.ts
git commit -m "feat(frontend): extend store with selection, density, filters, stats"
```

---

## Task 13: TorrentCard component

**Files:** Create `frontend/src/components/list/TorrentCard.tsx`

- [ ] **Step 1: Write the component**

```tsx
import {Show} from 'solid-js';
import {Pause as PauseIcon, Play, Trash2} from 'lucide-solid';
import type {Torrent} from '../../lib/bindings';
import {fmtBytes, fmtETA, fmtPercent, fmtRate} from '../../lib/format';
import {ProgressBar} from '../ui/ProgressBar';

type Props = {
  torrent: Torrent;
  selected: boolean;
  onSelect: (e: MouseEvent) => void;
  onPause: () => void;
  onResume: () => void;
  onRemove: () => void;
};

export function TorrentCard(props: Props) {
  const t = () => props.torrent;
  const remaining = () => t().total_bytes - t().bytes_done;
  const statusColor = () => {
    if (t().paused) return 'text-paused';
    if (t().completed) return 'text-seed';
    return 'text-down';
  };

  return (
    <div
      class="group rounded-lg border border-white/[.06] bg-white/[.02] p-3 transition-colors duration-150 hover:border-white/[.10] hover:bg-white/[.04]"
      classList={{'!border-accent-500/40 !bg-accent-500/[.04]': props.selected}}
      onClick={props.onSelect}
    >
      <div class="flex items-baseline justify-between gap-3">
        <div class="flex items-center gap-2 min-w-0">
          <span class={`h-1.5 w-1.5 shrink-0 rounded-full ${t().paused ? 'bg-paused' : t().completed ? 'bg-seed' : 'bg-down animate-pulse'}`} />
          <div class="truncate font-medium text-zinc-100">{t().name}</div>
        </div>
        <div class="shrink-0 font-mono text-xs tabular-nums text-zinc-500">{fmtBytes(t().total_bytes)}</div>
      </div>

      <div class="mt-2.5">
        <ProgressBar value={t().progress} active={!t().paused && !t().completed} />
      </div>

      <div class="mt-2 flex items-center justify-between text-xs text-zinc-400">
        <span class="font-mono tabular-nums">
          <span class={statusColor()}>{fmtPercent(t().progress)}</span>
          <Show when={!t().completed}>
            <span class="mx-1.5 text-zinc-600">·</span>
            <span>↓ {fmtRate(t().download_rate)}</span>
          </Show>
          <span class="mx-1.5 text-zinc-600">·</span>
          <span>↑ {fmtRate(t().upload_rate)}</span>
          <span class="mx-1.5 text-zinc-600">·</span>
          <span>{t().peers} peers</span>
          <Show when={!t().completed && t().download_rate > 0}>
            <span class="mx-1.5 text-zinc-600">·</span>
            <span>{fmtETA(remaining(), t().download_rate)}</span>
          </Show>
        </span>
        <span class="flex shrink-0 gap-1 opacity-0 transition-opacity duration-150 group-hover:opacity-100">
          <Show
            when={!t().paused}
            fallback={
              <button
                class="grid h-6 w-6 place-items-center rounded text-zinc-400 hover:bg-white/10 hover:text-zinc-100"
                onClick={(e) => { e.stopPropagation(); props.onResume(); }}
                title="Resume"
              >
                <Play class="h-3 w-3" />
              </button>
            }
          >
            <button
              class="grid h-6 w-6 place-items-center rounded text-zinc-400 hover:bg-white/10 hover:text-zinc-100"
              onClick={(e) => { e.stopPropagation(); props.onPause(); }}
              title="Pause"
            >
              <PauseIcon class="h-3 w-3" />
            </button>
          </Show>
          <button
            class="grid h-6 w-6 place-items-center rounded text-zinc-400 hover:bg-rose-500/20 hover:text-rose-300"
            onClick={(e) => { e.stopPropagation(); props.onRemove(); }}
            title="Remove"
          >
            <Trash2 class="h-3 w-3" />
          </button>
        </span>
      </div>
    </div>
  );
}
```

- [ ] **Step 2: Verify build**

```bash
cd frontend && npm run build
```

Expected: exit 0.

- [ ] **Step 3: Commit**

```bash
git add frontend/src/components/list/TorrentCard.tsx
git commit -m "feat(frontend): TorrentCard with status pulse, progress shimmer, hover actions"
```

---

## Task 14: TorrentTable component (TanStack)

**Files:** Create `frontend/src/components/list/TorrentTable.tsx`

- [ ] **Step 1: Write the component**

```tsx
import {createMemo, For, Show} from 'solid-js';
import {createSolidTable, getCoreRowModel, getSortedRowModel, flexRender, type ColumnDef, type SortingState} from '@tanstack/solid-table';
import {createSignal} from 'solid-js';
import {ChevronDown, ChevronUp} from 'lucide-solid';
import type {Torrent} from '../../lib/bindings';
import {fmtBytes, fmtETA, fmtPercent, fmtRate} from '../../lib/format';

type Props = {
  torrents: Torrent[];
  selection: Set<string>;
  onRowClick: (id: string, e: MouseEvent) => void;
};

export function TorrentTable(props: Props) {
  const [sorting, setSorting] = createSignal<SortingState>([{id: 'added_at', desc: true}]);

  const columns = createMemo<ColumnDef<Torrent>[]>(() => [
    {
      accessorKey: 'name',
      header: 'Name',
      cell: (info) => (
        <div class="flex items-center gap-2 min-w-0">
          <span class={`h-1.5 w-1.5 shrink-0 rounded-full ${info.row.original.paused ? 'bg-paused' : info.row.original.completed ? 'bg-seed' : 'bg-down animate-pulse'}`} />
          <span class="truncate text-zinc-100">{info.getValue() as string}</span>
        </div>
      ),
      size: 360,
    },
    {accessorKey: 'total_bytes', header: 'Size', cell: (info) => <span class="font-mono tabular-nums text-zinc-400">{fmtBytes(info.getValue() as number)}</span>, size: 90},
    {accessorKey: 'progress', header: 'Progress', cell: (info) => <span class="font-mono tabular-nums text-zinc-300">{fmtPercent(info.getValue() as number)}</span>, size: 80},
    {accessorKey: 'download_rate', header: '↓', cell: (info) => <span class="font-mono tabular-nums text-zinc-400">{fmtRate(info.getValue() as number)}</span>, size: 100},
    {accessorKey: 'upload_rate', header: '↑', cell: (info) => <span class="font-mono tabular-nums text-zinc-400">{fmtRate(info.getValue() as number)}</span>, size: 100},
    {
      id: 'eta',
      header: 'ETA',
      accessorFn: (t) => t.completed || t.download_rate === 0 ? Number.MAX_SAFE_INTEGER : (t.total_bytes - t.bytes_done) / t.download_rate,
      cell: (info) => {
        const t = info.row.original;
        if (t.completed) return <span class="text-zinc-600">—</span>;
        return <span class="font-mono tabular-nums text-zinc-400">{fmtETA(t.total_bytes - t.bytes_done, t.download_rate)}</span>;
      },
      size: 80,
    },
    {accessorKey: 'peers', header: 'Peers', cell: (info) => <span class="font-mono tabular-nums text-zinc-400">{info.getValue() as number}</span>, size: 70},
    {accessorKey: 'added_at', header: 'Added', cell: (info) => <span class="text-zinc-500">{new Date((info.getValue() as number) * 1000).toLocaleDateString()}</span>, size: 110},
  ]);

  const table = createSolidTable<Torrent>({
    get data() { return props.torrents; },
    get columns() { return columns(); },
    state: { get sorting() { return sorting(); } },
    onSortingChange: setSorting,
    getCoreRowModel: getCoreRowModel(),
    getSortedRowModel: getSortedRowModel(),
  });

  return (
    <div class="overflow-auto">
      <table class="w-full text-sm">
        <thead class="sticky top-0 z-10 bg-zinc-950/80 backdrop-blur-md text-xs font-medium uppercase tracking-wider text-zinc-500">
          <For each={table.getHeaderGroups()}>
            {(group) => (
              <tr>
                <For each={group.headers}>
                  {(header) => (
                    <th
                      class="cursor-pointer select-none px-3 py-2 text-left font-medium hover:text-zinc-300"
                      onClick={header.column.getToggleSortingHandler()}
                      style={{width: `${header.getSize()}px`}}
                    >
                      <div class="inline-flex items-center gap-1">
                        {flexRender(header.column.columnDef.header, header.getContext())}
                        <Show when={header.column.getIsSorted() === 'asc'}><ChevronUp class="h-3 w-3" /></Show>
                        <Show when={header.column.getIsSorted() === 'desc'}><ChevronDown class="h-3 w-3" /></Show>
                      </div>
                    </th>
                  )}
                </For>
              </tr>
            )}
          </For>
        </thead>
        <tbody>
          <For each={table.getRowModel().rows}>
            {(row) => (
              <tr
                class="cursor-pointer border-t border-white/[.04] hover:bg-white/[.02]"
                classList={{'!bg-accent-500/[.06]': props.selection.has(row.original.id)}}
                onClick={(e) => props.onRowClick(row.original.id, e)}
              >
                <For each={row.getVisibleCells()}>
                  {(cell) => (
                    <td class="px-3 py-2 truncate" style={{'max-width': `${cell.column.getSize()}px`}}>
                      {flexRender(cell.column.columnDef.cell, cell.getContext())}
                    </td>
                  )}
                </For>
              </tr>
            )}
          </For>
        </tbody>
      </table>
    </div>
  );
}
```

- [ ] **Step 2: Verify build**

```bash
cd frontend && npm run build
```

Expected: exit 0. (TanStack adds ~12 KB.)

- [ ] **Step 3: Commit**

```bash
git add frontend/src/components/list/TorrentTable.tsx
git commit -m "feat(frontend): TorrentTable (TanStack sortable, sticky header)"
```

---

## Task 15: EmptyState component

**Files:** Create `frontend/src/components/list/EmptyState.tsx`

- [ ] **Step 1: Write the component**

```tsx
import {Magnet, FileDown} from 'lucide-solid';

export function EmptyState() {
  return (
    <div class="flex h-full flex-col items-center justify-center gap-4 p-12 text-center">
      <div class="relative grid h-20 w-20 place-items-center rounded-2xl border border-white/[.06] bg-white/[.02]">
        <Magnet class="h-9 w-9 text-zinc-500" />
        <FileDown class="absolute -bottom-1 -right-1 h-7 w-7 rounded-md border border-white/[.06] bg-zinc-900 p-1 text-zinc-400" />
      </div>
      <div class="max-w-sm">
        <h2 class="text-base font-semibold text-zinc-200">Drop a torrent to begin</h2>
        <p class="mt-1 text-sm text-zinc-500">
          Drag a <span class="font-mono text-zinc-400">.torrent</span> file or magnet link onto this window, or use the buttons up top.
        </p>
      </div>
    </div>
  );
}
```

- [ ] **Step 2: Commit**

```bash
git add frontend/src/components/list/EmptyState.tsx
git commit -m "feat(frontend): EmptyState illustration with drag hint"
```

---

## Task 16: TorrentRowMenu (right-click context menu)

**Files:** Create `frontend/src/components/list/TorrentRowMenu.tsx`

- [ ] **Step 1: Write the component**

```tsx
import {ContextMenu} from '../ui/ContextMenu';
import {Pause, Play, Trash2, Folder, Copy, RotateCw} from 'lucide-solid';
import type {Torrent} from '../../lib/bindings';
import type {JSX} from 'solid-js';
import {Show} from 'solid-js';

type Props = {
  torrent: Torrent;
  onPause: () => void;
  onResume: () => void;
  onRemove: () => void;
  onCopyMagnet: () => void;
  children: JSX.Element;
};

export function TorrentRowMenu(props: Props) {
  return (
    <ContextMenu trigger={props.children}>
      <Show
        when={!props.torrent.paused}
        fallback={
          <ContextMenu.Item onSelect={props.onResume}>
            <Play class="h-3.5 w-3.5" />
            Resume
          </ContextMenu.Item>
        }
      >
        <ContextMenu.Item onSelect={props.onPause}>
          <Pause class="h-3.5 w-3.5" />
          Pause
        </ContextMenu.Item>
      </Show>
      <ContextMenu.Item disabled>
        <RotateCw class="h-3.5 w-3.5" />
        Recheck
      </ContextMenu.Item>
      <ContextMenu.Separator />
      <ContextMenu.Item disabled>
        <Folder class="h-3.5 w-3.5" />
        Open folder
      </ContextMenu.Item>
      <ContextMenu.Item onSelect={props.onCopyMagnet}>
        <Copy class="h-3.5 w-3.5" />
        Copy magnet
      </ContextMenu.Item>
      <ContextMenu.Separator />
      <ContextMenu.Item danger onSelect={props.onRemove}>
        <Trash2 class="h-3.5 w-3.5" />
        Remove
      </ContextMenu.Item>
    </ContextMenu>
  );
}
```

- [ ] **Step 2: Commit**

```bash
git add frontend/src/components/list/TorrentRowMenu.tsx
git commit -m "feat(frontend): TorrentRowMenu (Kobalte ContextMenu, danger remove action)"
```

---

## Task 17: TorrentList orchestrator

**Files:** Replace `frontend/src/components/TorrentList.tsx` → move to `frontend/src/components/list/TorrentList.tsx` and rewrite.

- [ ] **Step 1: Delete old TorrentList (stages deletion)**

```bash
git rm frontend/src/components/TorrentList.tsx
```

- [ ] **Step 2: Write the new orchestrator**

Create `frontend/src/components/list/TorrentList.tsx`:
```tsx
import {Match, Show, Switch, For} from 'solid-js';
import {toast} from 'solid-sonner';
import type {Torrent} from '../../lib/bindings';
import type {Density} from '../../lib/store';
import {TorrentCard} from './TorrentCard';
import {TorrentTable} from './TorrentTable';
import {EmptyState} from './EmptyState';
import {TorrentRowMenu} from './TorrentRowMenu';

type Props = {
  torrents: Torrent[];
  density: Density;
  selection: Set<string>;
  onSelect: (id: string, e: MouseEvent) => void;
  onPause: (id: string) => void;
  onResume: (id: string) => void;
  onRemove: (id: string) => void;
};

export function TorrentList(props: Props) {
  return (
    <Show when={props.torrents.length > 0} fallback={<EmptyState />}>
      <Switch>
        <Match when={props.density === 'cards'}>
          <div class="flex flex-col gap-2 p-3">
            <For each={props.torrents}>
              {(t) => (
                <TorrentRowMenu
                  torrent={t}
                  onPause={() => props.onPause(t.id)}
                  onResume={() => props.onResume(t.id)}
                  onRemove={() => props.onRemove(t.id)}
                  onCopyMagnet={() => {
                    if (t.magnet) {
                      navigator.clipboard.writeText(t.magnet);
                      toast.success('Magnet copied');
                    }
                  }}
                >
                  <TorrentCard
                    torrent={t}
                    selected={props.selection.has(t.id)}
                    onSelect={(e) => props.onSelect(t.id, e)}
                    onPause={() => props.onPause(t.id)}
                    onResume={() => props.onResume(t.id)}
                    onRemove={() => props.onRemove(t.id)}
                  />
                </TorrentRowMenu>
              )}
            </For>
          </div>
        </Match>
        <Match when={props.density === 'table'}>
          <TorrentTable
            torrents={props.torrents}
            selection={props.selection}
            onRowClick={props.onSelect}
          />
        </Match>
      </Switch>
    </Show>
  );
}
```

- [ ] **Step 3: Commit**

```bash
git add frontend/src/components/list/TorrentList.tsx
git commit -m "feat(frontend): TorrentList orchestrator (cards/table density switch + menus)"
```

(The deletion of the old `frontend/src/components/TorrentList.tsx` was already staged by `git rm` in Step 1.)

---

## Task 18: IconRail

**Files:** Create `frontend/src/components/shell/IconRail.tsx`

- [ ] **Step 1: Write the component**

```tsx
import {createSignal, For, type Component} from 'solid-js';
import {Activity, Plus, Search, Calendar, Rss, Settings, Info} from 'lucide-solid';
import {Tooltip} from '../ui/Tooltip';

type Item = {id: string; label: string; icon: typeof Activity; disabled?: boolean};

const top: Item[] = [
  {id: 'torrents', label: 'Torrents', icon: Activity},
  {id: 'add',      label: 'Add',      icon: Plus,     disabled: true},
  {id: 'search',   label: 'Search',   icon: Search,   disabled: true},
  {id: 'schedule', label: 'Schedule', icon: Calendar, disabled: true},
  {id: 'rss',      label: 'RSS',      icon: Rss,      disabled: true},
];
const bottom: Item[] = [
  {id: 'settings', label: 'Settings', icon: Settings, disabled: true},
  {id: 'about',    label: 'About',    icon: Info,     disabled: true},
];

export function IconRail() {
  const [active, setActive] = createSignal('torrents');

  const Btn: Component<{item: Item}> = (p) => (
    <Tooltip label={p.item.label} placement="right">
      <button
        type="button"
        disabled={p.item.disabled}
        onClick={() => !p.item.disabled && setActive(p.item.id)}
        class="relative grid h-10 w-10 place-items-center rounded-lg text-zinc-500 transition-colors duration-150 hover:text-zinc-200 disabled:opacity-30 disabled:hover:text-zinc-500"
        classList={{'!text-zinc-100': active() === p.item.id}}
      >
        <p.item.icon class="h-4 w-4" />
        {active() === p.item.id && (
          <span class="absolute left-0 top-1.5 bottom-1.5 w-[2px] rounded-r-full bg-accent-500" />
        )}
      </button>
    </Tooltip>
  );

  return (
    <nav class="flex h-full w-12 flex-col items-center justify-between border-r border-white/[.04] bg-white/[.01] py-3" style={{'-webkit-app-region': 'drag'}}>
      <div class="flex flex-col gap-1" style={{'-webkit-app-region': 'no-drag'}}>
        <For each={top}>{(it) => <Btn item={it} />}</For>
      </div>
      <div class="flex flex-col gap-1" style={{'-webkit-app-region': 'no-drag'}}>
        <For each={bottom}>{(it) => <Btn item={it} />}</For>
      </div>
    </nav>
  );
}
```

- [ ] **Step 2: Commit**

```bash
git add frontend/src/components/shell/IconRail.tsx
git commit -m "feat(frontend): IconRail with active accent indicator + tooltips"
```

---

## Task 19: FilterRail

**Files:** Create `frontend/src/components/shell/FilterRail.tsx`

- [ ] **Step 1: Write the component**

```tsx
import {For, Show, type Component} from 'solid-js';
import {ChevronDown, ListFilter, Folder, Tag, Globe} from 'lucide-solid';
import type {StatusFilter} from '../../lib/store';
import type {Torrent} from '../../lib/bindings';

type StatusItem = {id: StatusFilter; label: string; count: (t: Torrent[]) => number};

const statusItems: StatusItem[] = [
  {id: 'all',         label: 'All',         count: (t) => t.length},
  {id: 'downloading', label: 'Downloading', count: (t) => t.filter((x) => !x.paused && !x.completed).length},
  {id: 'seeding',     label: 'Seeding',     count: (t) => t.filter((x) => x.completed && !x.paused).length},
  {id: 'completed',   label: 'Completed',   count: (t) => t.filter((x) => x.completed).length},
  {id: 'paused',      label: 'Paused',      count: (t) => t.filter((x) => x.paused).length},
  {id: 'errored',     label: 'Errored',     count: () => 0},
];

type Props = {
  torrents: Torrent[];
  active: StatusFilter;
  onSelect: (s: StatusFilter) => void;
};

const Section: Component<{icon: typeof ListFilter; title: string; count?: number; children?: any}> = (p) => (
  <div class="px-2">
    <div class="flex items-center justify-between px-2 py-1.5 text-[10px] font-semibold uppercase tracking-wider text-zinc-500">
      <span class="inline-flex items-center gap-1.5">
        <p.icon class="h-3 w-3" />
        {p.title}
      </span>
      <ChevronDown class="h-3 w-3 text-zinc-600" />
    </div>
    {p.children}
  </div>
);

export function FilterRail(props: Props) {
  return (
    <aside class="flex h-full w-60 shrink-0 flex-col gap-3 border-r border-white/[.04] bg-white/[.01] py-3">
      <Section icon={ListFilter} title="Status">
        <ul class="flex flex-col gap-px">
          <For each={statusItems}>
            {(it) => {
              const c = () => it.count(props.torrents);
              return (
                <li>
                  <button
                    type="button"
                    onClick={() => props.onSelect(it.id)}
                    class="flex w-full items-center justify-between rounded-md px-2 py-1.5 text-sm transition-colors duration-100 hover:bg-white/[.04]"
                    classList={{'bg-accent-500/[.10] text-accent-200': props.active === it.id, 'text-zinc-300': props.active !== it.id}}
                  >
                    <span>{it.label}</span>
                    <Show when={c() > 0}>
                      <span class="font-mono text-xs tabular-nums text-zinc-500">{c()}</span>
                    </Show>
                  </button>
                </li>
              );
            }}
          </For>
        </ul>
      </Section>

      <Section icon={Folder} title="Categories">
        <p class="px-2 text-xs text-zinc-600">Coming in Plan 4</p>
      </Section>
      <Section icon={Tag} title="Tags">
        <p class="px-2 text-xs text-zinc-600">Coming in Plan 4</p>
      </Section>
      <Section icon={Globe} title="Trackers">
        <p class="px-2 text-xs text-zinc-600">Coming in Plan 4</p>
      </Section>
    </aside>
  );
}
```

- [ ] **Step 2: Commit**

```bash
git add frontend/src/components/shell/FilterRail.tsx
git commit -m "feat(frontend): FilterRail with live status counts (categories/tags/trackers stub)"
```

---

## Task 20: TopToolbar

**Files:** Create `frontend/src/components/shell/TopToolbar.tsx`

- [ ] **Step 1: Write the component**

```tsx
import {Search, Magnet, FileDown, LayoutGrid, List, Zap, Settings} from 'lucide-solid';
import {Button} from '../ui/Button';
import {Tooltip} from '../ui/Tooltip';
import {ThemeToggle} from '../theme/ThemeToggle';
import type {Density} from '../../lib/store';

type Props = {
  searchQuery: string;
  onSearch: (q: string) => void;
  onAddMagnet: () => void;
  onAddTorrent: () => void;
  density: Density;
  onDensityChange: (d: Density) => void;
};

export function TopToolbar(props: Props) {
  return (
    <header
      class="flex h-12 shrink-0 items-center gap-3 border-b border-white/[.04] bg-zinc-950/80 px-3 backdrop-blur-md"
      style={{'-webkit-app-region': 'drag'}}
    >
      {/* Drag affordance — invisible but full-height area; the toolbar IS the drag region */}
      <div class="relative flex-1 max-w-md" style={{'-webkit-app-region': 'no-drag'}}>
        <Search class="pointer-events-none absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-zinc-500" />
        <input
          type="text"
          placeholder="Search torrents…"
          value={props.searchQuery}
          onInput={(e) => props.onSearch(e.currentTarget.value)}
          class="w-full rounded-md border border-white/[.06] bg-white/[.02] py-1.5 pl-8 pr-3 text-sm text-zinc-100 placeholder:text-zinc-500 focus:border-accent-500/50 focus:bg-white/[.04] focus:outline-none focus:ring-1 focus:ring-accent-500/30"
        />
      </div>

      <div class="flex items-center gap-1.5" style={{'-webkit-app-region': 'no-drag'}}>
        <Button variant="secondary" onClick={props.onAddTorrent}>
          <FileDown class="h-3.5 w-3.5" />
          .torrent
        </Button>
        <Button variant="primary" onClick={props.onAddMagnet}>
          <Magnet class="h-3.5 w-3.5" />
          Magnet
        </Button>

        <span class="mx-1 h-5 w-px bg-white/[.06]" />

        <Tooltip label="Toggle alt-speed limits">
          <button class="grid h-7 w-7 place-items-center rounded-md text-zinc-400 hover:bg-white/[.04] hover:text-zinc-100" disabled>
            <Zap class="h-3.5 w-3.5" />
          </button>
        </Tooltip>

        <Tooltip label="Density: cards / table">
          <div class="inline-flex items-center rounded-md border border-white/[.06] bg-white/[.02] p-0.5">
            <button
              class="grid h-6 w-6 place-items-center rounded text-zinc-400 transition-colors hover:text-zinc-100"
              classList={{'!bg-white/10 !text-zinc-100': props.density === 'cards'}}
              onClick={() => props.onDensityChange('cards')}
              aria-label="Cards"
            >
              <LayoutGrid class="h-3 w-3" />
            </button>
            <button
              class="grid h-6 w-6 place-items-center rounded text-zinc-400 transition-colors hover:text-zinc-100"
              classList={{'!bg-white/10 !text-zinc-100': props.density === 'table'}}
              onClick={() => props.onDensityChange('table')}
              aria-label="Table"
            >
              <List class="h-3 w-3" />
            </button>
          </div>
        </Tooltip>

        <ThemeToggle />

        <Tooltip label="Settings">
          <button class="grid h-7 w-7 place-items-center rounded-md text-zinc-400 hover:bg-white/[.04] hover:text-zinc-100" disabled>
            <Settings class="h-3.5 w-3.5" />
          </button>
        </Tooltip>
      </div>
    </header>
  );
}
```

- [ ] **Step 2: Commit**

```bash
git add frontend/src/components/shell/TopToolbar.tsx
git commit -m "feat(frontend): TopToolbar with search, add buttons, density toggle, theme toggle"
```

---

## Task 21: StatusBar

**Files:** Create `frontend/src/components/shell/StatusBar.tsx`

- [ ] **Step 1: Write the component**

```tsx
import {ArrowDown, ArrowUp, Wifi} from 'lucide-solid';
import type {GlobalStatsT} from '../../lib/bindings';
import {fmtRate} from '../../lib/format';

type Props = {stats: GlobalStatsT};

export function StatusBar(props: Props) {
  const s = () => props.stats;
  return (
    <footer class="flex h-7 shrink-0 items-center gap-4 border-t border-white/[.04] bg-zinc-950/60 px-3 text-[11px] text-zinc-400">
      <span class="inline-flex items-center gap-1.5">
        <ArrowDown class="h-3 w-3 text-down" />
        <span class="font-mono tabular-nums">{fmtRate(s().total_download_rate)}</span>
      </span>
      <span class="inline-flex items-center gap-1.5">
        <ArrowUp class="h-3 w-3 text-zinc-500" />
        <span class="font-mono tabular-nums">{fmtRate(s().total_upload_rate)}</span>
      </span>

      <span class="h-3 w-px bg-white/[.06]" />

      <span class="font-mono tabular-nums">{s().total_torrents} torrents</span>
      <span class="font-mono tabular-nums">{s().active_torrents} active</span>
      <span class="font-mono tabular-nums">{s().seeding_torrents} seeding</span>
      <span class="font-mono tabular-nums">{s().total_peers} peers</span>

      <span class="ml-auto inline-flex items-center gap-1.5">
        <Wifi class="h-3 w-3 text-seed" />
        <span class="text-zinc-500">DHT online</span>
      </span>
    </footer>
  );
}
```

- [ ] **Step 2: Commit**

```bash
git add frontend/src/components/shell/StatusBar.tsx
git commit -m "feat(frontend): StatusBar with live rates, totals, DHT indicator"
```

---

## Task 22: WindowShell + DropZone + AddMagnetModal restyle

**Files:**
- Create: `frontend/src/components/shell/WindowShell.tsx`
- Create: `frontend/src/components/shell/DropZone.tsx`
- Modify: `frontend/src/components/AddMagnetModal.tsx` (restyle, no behavior change)

- [ ] **Step 1: Create the new AddMagnetModal at the shell location**

(The old `frontend/src/components/AddMagnetModal.tsx` stays in place for now so App.tsx — still on its Plan 1 import path — keeps building. It will be deleted in Task 23 when App.tsx switches imports to the shell location.)

`frontend/src/components/shell/AddMagnetModal.tsx`:
```tsx
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
```

- [ ] **Step 2: DropZone — window-wide drag target**

`frontend/src/components/shell/DropZone.tsx`:
```tsx
import {createSignal, onCleanup, onMount, Show, type JSX} from 'solid-js';
import {Magnet, FileDown} from 'lucide-solid';
import {toast} from 'solid-sonner';

type Props = {
  onMagnet: (m: string) => Promise<void>;
  children: JSX.Element;
};

export function DropZone(props: Props) {
  const [active, setActive] = createSignal(false);

  onMount(() => {
    const onDragOver = (e: DragEvent) => {
      e.preventDefault();
      if (e.dataTransfer) e.dataTransfer.dropEffect = 'copy';
      setActive(true);
    };
    const onDragLeave = (e: DragEvent) => {
      // only deactivate if leaving window
      if ((e as any).relatedTarget == null) setActive(false);
    };
    const onDrop = async (e: DragEvent) => {
      e.preventDefault();
      setActive(false);
      const text = e.dataTransfer?.getData('text/plain') ?? '';
      if (text.startsWith('magnet:?')) {
        try { await props.onMagnet(text); }
        catch (err) { toast.error(String(err)); }
        return;
      }
      if (e.dataTransfer?.files.length) {
        toast.error('.torrent file drop coming in Plan 3 — use the .torrent button for now');
      }
    };
    window.addEventListener('dragover', onDragOver);
    window.addEventListener('dragleave', onDragLeave);
    window.addEventListener('drop', onDrop);
    onCleanup(() => {
      window.removeEventListener('dragover', onDragOver);
      window.removeEventListener('dragleave', onDragLeave);
      window.removeEventListener('drop', onDrop);
    });
  });

  return (
    <div class="relative h-full">
      {props.children}
      <Show when={active()}>
        <div class="pointer-events-none absolute inset-2 z-50 grid place-items-center rounded-2xl border-2 border-dashed border-accent-500/60 bg-accent-500/[.06] backdrop-blur-sm animate-in fade-in">
          <div class="flex flex-col items-center gap-3 text-accent-200">
            <div class="flex gap-2">
              <Magnet class="h-8 w-8" />
              <FileDown class="h-8 w-8 opacity-50" />
            </div>
            <div class="text-base font-semibold">Drop to add torrent</div>
          </div>
        </div>
      </Show>
    </div>
  );
}
```

- [ ] **Step 3: WindowShell — composes the regions**

`frontend/src/components/shell/WindowShell.tsx`:
```tsx
import {createSignal, type JSX} from 'solid-js';
import type {Density, StatusFilter} from '../../lib/store';
import type {GlobalStatsT, Torrent} from '../../lib/bindings';
import {IconRail} from './IconRail';
import {FilterRail} from './FilterRail';
import {TopToolbar} from './TopToolbar';
import {StatusBar} from './StatusBar';
import {DropZone} from './DropZone';

type Props = {
  torrents: Torrent[];
  filteredTorrents: Torrent[];
  stats: GlobalStatsT;
  density: Density;
  statusFilter: StatusFilter;
  searchQuery: string;
  onDensityChange: (d: Density) => void;
  onStatusFilter: (s: StatusFilter) => void;
  onSearchQuery: (q: string) => void;
  onAddMagnet: () => void;
  onAddTorrent: () => void;
  onMagnetDropped: (m: string) => Promise<void>;
  children: JSX.Element; // the main pane (TorrentList)
};

export function WindowShell(props: Props) {
  return (
    <div class="flex h-full flex-col">
      <div class="flex flex-1 min-h-0">
        <IconRail />
        <FilterRail
          torrents={props.torrents}
          active={props.statusFilter}
          onSelect={props.onStatusFilter}
        />
        <main class="flex flex-1 min-w-0 flex-col">
          <TopToolbar
            searchQuery={props.searchQuery}
            onSearch={props.onSearchQuery}
            onAddMagnet={props.onAddMagnet}
            onAddTorrent={props.onAddTorrent}
            density={props.density}
            onDensityChange={props.onDensityChange}
          />
          <DropZone onMagnet={props.onMagnetDropped}>
            <div class="h-full overflow-auto">
              {props.children}
            </div>
          </DropZone>
        </main>
      </div>
      <StatusBar stats={props.stats} />
    </div>
  );
}
```

- [ ] **Step 4: Verify build**

```bash
cd frontend && npm run build
```

Expected: exit 0. (The old `frontend/src/components/AddMagnetModal.tsx` is still present and still imported by App.tsx; nothing imports the new `shell/AddMagnetModal.tsx` yet. Both compile fine.)

- [ ] **Step 5: Commit**

```bash
git add frontend/src/components/shell/
git commit -m "feat(frontend): WindowShell composing IconRail/FilterRail/Toolbar/StatusBar + DropZone"
```

---

## Task 23: Wire it all together in App.tsx

**Files:**
- Modify: `frontend/src/App.tsx`
- Delete: `frontend/src/components/AddMagnetModal.tsx` (now superseded by `shell/AddMagnetModal.tsx`)

- [ ] **Step 1: Delete the old AddMagnetModal location**

```bash
git rm frontend/src/components/AddMagnetModal.tsx
```

- [ ] **Step 2: Write the new App**

```tsx
import {createMemo, createSignal, onCleanup, onMount} from 'solid-js';
import {Toaster, toast} from 'solid-sonner';
import {createTorrentsStore, filterTorrents} from './lib/store';
import {ThemeProvider} from './components/theme/ThemeProvider';
import {WindowShell} from './components/shell/WindowShell';
import {AddMagnetModal} from './components/shell/AddMagnetModal';
import {TorrentList} from './components/list/TorrentList';
import './index.css';

export default function App() {
  const store = createTorrentsStore();
  const [magnetModal, setMagnetModal] = createSignal(false);
  onCleanup(() => store.dispose());

  const filtered = createMemo(() =>
    filterTorrents(store.state.torrents, store.state.statusFilter, store.state.searchQuery)
  );

  const handleSelect = (id: string, e: MouseEvent) => {
    if (e.metaKey || e.ctrlKey) store.toggleSelect(id);
    else if (e.shiftKey) store.extendSelectTo(id);
    else store.select(id);
  };

  const handleAddTorrent = async () => {
    try {
      const id = await store.pickAndAddTorrent();
      if (id) toast.success('Torrent added');
    } catch (err) { toast.error(String(err)); }
  };

  const handleMagnetDropped = async (m: string) => {
    await store.addMagnet(m);
    toast.success('Magnet added');
  };

  // Global keyboard shortcuts
  onMount(() => {
    const handler = (e: KeyboardEvent) => {
      if (e.target instanceof HTMLInputElement || e.target instanceof HTMLTextAreaElement) return;
      if ((e.metaKey || e.ctrlKey) && e.key === 'a') {
        e.preventDefault();
        store.selectAll();
      } else if (e.key === 'Escape') {
        store.clearSelection();
      } else if (e.key === ' ') {
        e.preventDefault();
        for (const id of store.state.selection) {
          const t = store.state.torrents.find((x) => x.id === id);
          if (!t) continue;
          if (t.paused) store.resume(id); else store.pause(id);
        }
      } else if (e.key === 'Delete' || e.key === 'Backspace') {
        if (store.state.selection.size === 0) return;
        e.preventDefault();
        for (const id of store.state.selection) store.remove(id, false);
        store.clearSelection();
      }
    };
    window.addEventListener('keydown', handler);
    onCleanup(() => window.removeEventListener('keydown', handler));
  });

  return (
    <ThemeProvider>
      <WindowShell
        torrents={store.state.torrents}
        filteredTorrents={filtered()}
        stats={store.state.stats}
        density={store.state.density}
        statusFilter={store.state.statusFilter}
        searchQuery={store.state.searchQuery}
        onDensityChange={store.setDensity}
        onStatusFilter={store.setStatusFilter}
        onSearchQuery={store.setSearchQuery}
        onAddMagnet={() => setMagnetModal(true)}
        onAddTorrent={handleAddTorrent}
        onMagnetDropped={handleMagnetDropped}
      >
        <TorrentList
          torrents={filtered()}
          density={store.state.density}
          selection={store.state.selection}
          onSelect={handleSelect}
          onPause={(id) => store.pause(id)}
          onResume={(id) => store.resume(id)}
          onRemove={(id) => { store.remove(id, false); toast('Removed'); }}
        />
      </WindowShell>
      <AddMagnetModal
        open={magnetModal()}
        onClose={() => setMagnetModal(false)}
        onSubmit={async (m) => {
          await store.addMagnet(m);
          toast.success('Magnet added');
        }}
      />
      <Toaster
        position="bottom-right"
        toastOptions={{
          style: {
            background: 'rgba(24, 24, 27, 0.95)',
            border: '1px solid rgba(255,255,255,0.1)',
            color: '#e7e7e9',
            'backdrop-filter': 'blur(12px)',
          },
        }}
      />
    </ThemeProvider>
  );
}
```

- [ ] **Step 3: Verify build**

```bash
cd frontend && npm run build
```

Expected: exit 0. Bundle ~50–60 KB JS, ~12 KB CSS.

- [ ] **Step 4: Verify Wails dev still launches**

```bash
$(go env GOPATH)/bin/wails dev -skipembedcreate
```

Smoke check: window opens, dark theme, icon rail visible, filter rail visible with status counts, top toolbar with search + buttons, status bar at bottom. Existing torrents render in cards mode by default. Close.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/App.tsx
git commit -m "feat(frontend): wire ThemeProvider + WindowShell + keyboard shortcuts + toasts"
```

(The deletion of `frontend/src/components/AddMagnetModal.tsx` was already staged by `git rm` in Step 1.)

---

## Task 24: Polish pass — focus rings, motion micro-interactions, scroll polish

**Files:**
- Modify: `frontend/src/index.css` (add scroll/focus styling)

- [ ] **Step 1: Append polish CSS**

Append to `frontend/src/index.css`:
```css
/* Custom scrollbars — subtle on dark */
*::-webkit-scrollbar { width: 10px; height: 10px; }
*::-webkit-scrollbar-track { background: transparent; }
*::-webkit-scrollbar-thumb { background: rgba(255,255,255,0.06); border-radius: 5px; border: 2px solid transparent; background-clip: padding-box; }
*::-webkit-scrollbar-thumb:hover { background: rgba(255,255,255,0.12); border: 2px solid transparent; background-clip: padding-box; }

/* Selection — accent tint */
::selection { background: oklch(0.65 0.25 290 / 0.35); color: #fff; }

/* Focus-visible default for everything we don't ring explicitly */
:focus-visible { outline: 2px solid oklch(0.65 0.25 290 / 0.6); outline-offset: 2px; border-radius: 4px; }

/* Disable spurious focus rings on click */
:focus:not(:focus-visible) { outline: none; }

/* Tailwind-like animate-in helpers (we use solid-sonner + Kobalte data states) */
.animate-in { animation-duration: 150ms; animation-fill-mode: both; animation-timing-function: var(--ease-app); }
.fade-in { animation-name: fade-in; }
.zoom-in-95 { animation-name: zoom-in-95; }
@keyframes fade-in { from { opacity: 0; } to { opacity: 1; } }
@keyframes zoom-in-95 { from { opacity: 0; transform: scale(0.95); } to { opacity: 1; transform: scale(1); } }
```

- [ ] **Step 2: Verify build + dev**

```bash
cd frontend && npm run build
```

Then re-run `wails dev -skipembedcreate`, confirm:
- Tab navigation shows accent focus rings
- Scrollbars are subtle dark
- Selection highlight is violet

- [ ] **Step 3: Commit**

```bash
git add frontend/src/index.css
git commit -m "polish: custom scrollbars, accent selection, focus rings, animation utilities"
```

---

## Task 25: End-to-end smoke test (user-driven)

- [ ] **Step 1: Run `wails dev -skipembedcreate`.** Window opens with new shell.

- [ ] **Step 2: Visual check**:
  - Icon rail (left, 48px): 5 nav items at top (Torrents active with violet edge, others disabled), 2 at bottom (Settings, About — disabled).
  - Filter rail (240px): Status section with 6 items + live counts; "Categories / Tags / Trackers" sections show "Coming in Plan 4" placeholders.
  - Top toolbar: search box (left), `+ .torrent`, `+ Magnet` (primary violet), zap (alt-speed stub), density toggle, theme toggle, settings (stub).
  - Status bar: ↓/↑ rates, totals, DHT online indicator.
  - Empty state: large magnet+filedown icons, "Drop a torrent to begin" message.

- [ ] **Step 2: Functional check**:
  - Click "+ Magnet" → modal opens with restyled chrome, paste magnet → toast appears
  - Drag a magnet text from another app onto the window → DropZone activates, drops → torrent added with toast
  - Right-click a torrent row → context menu (Pause/Recheck/Folder/Copy magnet/Remove)
  - ⌘-click multiple cards → multi-select shows accent border on each
  - Press Space with selection → toggles pause/resume on all selected
  - Press Delete → removes all selected with confirm-via-toast
  - Click "Downloading" filter → list filters
  - Type in search → list filters by name
  - Toggle theme to Light → app inverts (chrome stays consistent)
  - Toggle density to Table → switches to TanStack table; click column headers to sort
  - Quit and re-launch → density and theme persist (localStorage)

- [ ] **Step 3: If everything works, tag.**

```bash
git tag plan-2-shell-complete
git push origin plan-2-shell-complete
git push origin main
```

- [ ] **Step 4: If issues, surface them via `team-lead` for triage.**

---

**End of Plan 2.**
