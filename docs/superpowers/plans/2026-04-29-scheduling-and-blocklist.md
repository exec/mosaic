# Mosaic — Plan 4d: Bandwidth Scheduling + IP Filtering + Tracker Status

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add the rule-driven controls — time-of-day bandwidth schedules and IP blocklists — that complete the "controls" half of the design spec, plus a best-effort refinement of tracker status (currently always "OK"). After this plan: alt-speed kicks in automatically at night without you touching the Zap button; you can drop a PeerGuardian-format URL in Settings and the engine refuses connections to listed ranges; the Trackers tab in the inspector shows real announce state.

**Architecture:** Backend grows a `ScheduleEngine` goroutine that ticks once a minute, finds the active rule (if any) for now, and applies it via the existing `Service.SetLimits` / `Service.ToggleAltSpeed` plumbing. IP filter wires anacrolix's `IPBlocklist` (a subnet trie) — Service gains `LoadBlocklist(url, enabled)`. Tracker refinement reaches into `t.AnnounceList()` results where available; falls back to "OK" when anacrolix doesn't surface per-tracker state cleanly. Frontend adds two new Settings panes (Schedule, Blocklist) plus refines TrackersTab to render the real status badges.

**Tech additions:**
- `github.com/anacrolix/torrent/iplist` — already a transitive dep; use directly for blocklist parsing.

**Reference spec:** `docs/superpowers/specs/2026-04-28-bittorrent-client-design.md` §6 (`schedule_rules` schema), §4.4 (Trackers tab).

**Aesthetic continuity:** Schedule pane shows a weekly grid (7 rows × 24 columns) with rule blocks overlaid; click+drag to define a rule. Blocklist pane is a single URL field + Enabled toggle + a "Last loaded" timestamp + a Refresh button. Tracker status badges expand the existing `bg-seed/[.10] text-seed` pattern to also handle `bg-amber-500/[.10] text-amber-400` (Updating) and `bg-rose-500/[.10] text-rose-300` (Error).

---

## Out of Scope (deferred to Plan 5+)

- **RSS auto-add** — Plan 5
- **HTTPS+WS remote** — Plan 6
- **Auto-update** — Plan 7
- **Packaging / signing / CI** — Plan 8
- **Schedule grid drag-to-define UX** — Plan 4d ships a list-style add-rule form; the visual weekly grid is a Plan 5 polish (calls it out in About roadmap)

---

## File Structure (final state)

```
backend/
├── persistence/
│   ├── migrations/0004_schedule_rules.sql      # NEW
│   ├── schedule_rules.go                       # NEW
│   └── schedule_rules_test.go                  # NEW
├── engine/
│   └── anacrolix.go                            # MODIFIED: IPBlocklist wiring + tracker refinement
└── api/
    ├── service.go                              # MODIFIED: schedule + blocklist methods
    ├── service_test.go
    └── schedule_engine.go                      # NEW: time-of-day scheduler goroutine
app.go                                          # MODIFIED: schedule + blocklist bindings

frontend/src/
├── lib/
│   ├── bindings.ts                             # MODIFIED
│   └── store.ts                                # MODIFIED
└── components/
    ├── settings/
    │   ├── SchedulePane.tsx                    # NEW
    │   ├── BlocklistPane.tsx                   # NEW
    │   ├── SettingsSidebar.tsx                 # MODIFIED: add Schedule + Blocklist
    │   └── SettingsRoute.tsx                   # MODIFIED
    └── inspector/
        └── TrackersTab.tsx                     # MODIFIED: real status badges
```

---

## Section A — Persistence: schedule_rules

### Task 1: Migration 0004

`backend/persistence/migrations/0004_schedule_rules.sql`:

```sql
-- +goose Up
CREATE TABLE schedule_rules (
  id          INTEGER PRIMARY KEY,
  days_mask   INTEGER NOT NULL,    -- bit 0 = Sunday, bit 6 = Saturday
  start_min   INTEGER NOT NULL,    -- minutes since midnight (0..1439)
  end_min     INTEGER NOT NULL,
  down_kbps   INTEGER NOT NULL,    -- 0 = unlimited
  up_kbps     INTEGER NOT NULL,
  alt_only    INTEGER NOT NULL DEFAULT 0,  -- 1 = use alt-speed values, ignore down/up_kbps
  enabled     INTEGER NOT NULL DEFAULT 1
);

-- +goose Down
DROP TABLE schedule_rules;
```

- [ ] Add `pragma_table_info` assertion to `TestOpen_RunsMigrations`. Run, confirm pass. Commit: `feat(persistence): migration 0004 — schedule_rules`.

---

### Task 2: ScheduleRule DAO

`backend/persistence/schedule_rules.go`:

```go
package persistence

import (
	"context"
	"database/sql"
	"errors"
)

type ScheduleRule struct {
	ID        int
	DaysMask  int
	StartMin  int
	EndMin    int
	DownKbps  int
	UpKbps    int
	AltOnly   bool
	Enabled   bool
}

type ScheduleRules struct{ db *DB }

func NewScheduleRules(db *DB) *ScheduleRules { return &ScheduleRules{db: db} }

func (s *ScheduleRules) Create(ctx context.Context, r ScheduleRule) (int, error) {
	res, err := s.db.SQL().ExecContext(ctx, `
INSERT INTO schedule_rules (days_mask, start_min, end_min, down_kbps, up_kbps, alt_only, enabled)
VALUES (?, ?, ?, ?, ?, ?, ?)`,
		r.DaysMask, r.StartMin, r.EndMin, r.DownKbps, r.UpKbps, boolToInt(r.AltOnly), boolToInt(r.Enabled))
	if err != nil { return 0, err }
	id, err := res.LastInsertId()
	return int(id), err
}

func (s *ScheduleRules) Get(ctx context.Context, id int) (ScheduleRule, error) {
	var r ScheduleRule
	var altOnly, enabled int
	err := s.db.SQL().QueryRowContext(ctx,
		`SELECT id, days_mask, start_min, end_min, down_kbps, up_kbps, alt_only, enabled FROM schedule_rules WHERE id = ?`, id).
		Scan(&r.ID, &r.DaysMask, &r.StartMin, &r.EndMin, &r.DownKbps, &r.UpKbps, &altOnly, &enabled)
	if errors.Is(err, sql.ErrNoRows) { return r, ErrNotFound }
	r.AltOnly = altOnly == 1
	r.Enabled = enabled == 1
	return r, err
}

func (s *ScheduleRules) List(ctx context.Context) ([]ScheduleRule, error) {
	rows, err := s.db.SQL().QueryContext(ctx,
		`SELECT id, days_mask, start_min, end_min, down_kbps, up_kbps, alt_only, enabled FROM schedule_rules ORDER BY days_mask, start_min`)
	if err != nil { return nil, err }
	defer rows.Close()
	var out []ScheduleRule
	for rows.Next() {
		var r ScheduleRule
		var altOnly, enabled int
		if err := rows.Scan(&r.ID, &r.DaysMask, &r.StartMin, &r.EndMin, &r.DownKbps, &r.UpKbps, &altOnly, &enabled); err != nil {
			return nil, err
		}
		r.AltOnly = altOnly == 1
		r.Enabled = enabled == 1
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *ScheduleRules) Update(ctx context.Context, r ScheduleRule) error {
	_, err := s.db.SQL().ExecContext(ctx,
		`UPDATE schedule_rules SET days_mask = ?, start_min = ?, end_min = ?, down_kbps = ?, up_kbps = ?, alt_only = ?, enabled = ? WHERE id = ?`,
		r.DaysMask, r.StartMin, r.EndMin, r.DownKbps, r.UpKbps, boolToInt(r.AltOnly), boolToInt(r.Enabled), r.ID)
	return err
}

func (s *ScheduleRules) Delete(ctx context.Context, id int) error {
	_, err := s.db.SQL().ExecContext(ctx, `DELETE FROM schedule_rules WHERE id = ?`, id)
	return err
}

func boolToInt(b bool) int {
	if b { return 1 }
	return 0
}
```

- [ ] Add tests for CRUD round-trip and List ordering. Commit: `feat(persistence): ScheduleRules DAO (CRUD)`.

---

## Section B — Schedule engine

### Task 3: ScheduleEngine goroutine

`backend/api/schedule_engine.go`:

```go
package api

import (
	"context"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
	"mosaic/backend/persistence"
)

// ScheduleEngine ticks once a minute, finds the active schedule rule for now,
// and applies it to the api Service (which propagates to engine rate limits).
type ScheduleEngine struct {
	svc       *Service
	rules     *persistence.ScheduleRules
	location  *time.Location

	mu        sync.RWMutex
	lastApplied int  // rule ID we last applied (0 = none/cleared)

	stop chan struct{}
}

func NewScheduleEngine(svc *Service, rules *persistence.ScheduleRules, loc *time.Location) *ScheduleEngine {
	if loc == nil { loc = time.Local }
	se := &ScheduleEngine{svc: svc, rules: rules, location: loc, stop: make(chan struct{})}
	go se.run()
	return se
}

func (se *ScheduleEngine) Close() { close(se.stop) }

func (se *ScheduleEngine) run() {
	t := time.NewTicker(60 * time.Second)
	defer t.Stop()
	se.tick(context.Background()) // immediate
	for {
		select {
		case <-se.stop: return
		case <-t.C:    se.tick(context.Background())
		}
	}
}

func (se *ScheduleEngine) tick(ctx context.Context) {
	rules, err := se.rules.List(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("schedule_engine: list rules")
		return
	}
	now := time.Now().In(se.location)
	dayBit := 1 << int(now.Weekday())
	minutes := now.Hour()*60 + now.Minute()

	var active *persistence.ScheduleRule
	for i := range rules {
		r := &rules[i]
		if !r.Enabled { continue }
		if r.DaysMask & dayBit == 0 { continue }
		if minutes < r.StartMin || minutes >= r.EndMin { continue }
		active = r
		break // first match wins; later iteration could prefer most-recent or highest-priority
	}

	se.mu.Lock()
	prevID := se.lastApplied
	nextID := 0
	if active != nil { nextID = active.ID }
	se.lastApplied = nextID
	se.mu.Unlock()

	if prevID == nextID { return } // no-op

	if active == nil {
		log.Info().Msg("schedule_engine: no active rule, restoring user-configured limits")
		_ = se.svc.applyLimits(ctx)
		return
	}

	if active.AltOnly {
		// Apply user's alt-speed values regardless of alt_active toggle
		l, _ := se.svc.GetLimits(ctx)
		_ = se.svc.engine.SetGlobalRateLimits(l.AltDownKbps*1024, l.AltUpKbps*1024)
		log.Info().Int("rule_id", active.ID).Msg("schedule_engine: applied alt-only rule")
		return
	}

	// Apply rule's own values
	_ = se.svc.engine.SetGlobalRateLimits(active.DownKbps*1024, active.UpKbps*1024)
	log.Info().Int("rule_id", active.ID).Int("down", active.DownKbps).Int("up", active.UpKbps).
		Msg("schedule_engine: applied rule")
}
```

- [ ] Service: add CRUD methods that wrap ScheduleRules DAO + DTO type. Commit: `feat(api): ScheduleEngine + ScheduleRule CRUD`.

---

### Task 4: Wails bindings for schedules

App.go gains: `ListScheduleRules`, `CreateScheduleRule`, `UpdateScheduleRule`, `DeleteScheduleRule`. Regenerate. Commit: `feat: Wails bindings for schedule rules`.

---

## Section C — IP filtering

### Task 5: Anacrolix IPBlocklist integration

`backend/engine/anacrolix.go`:

```go
import "github.com/anacrolix/torrent/iplist"

func (a *AnacrolixBackend) SetIPBlocklist(reader io.Reader) error {
	if reader == nil {
		a.client.SetIPBlockList(nil)
		return nil
	}
	list, err := iplist.NewFromReader(reader)
	if err != nil { return err }
	a.client.SetIPBlockList(list)
	return nil
}
```

(Verify `client.SetIPBlockList` exists in v1.61. If not, set via `tcfg.IPBlocklist` at construction — but runtime updates would require a different path. Flag if drift.)

`backend/engine/types.go` Backend interface adds `SetIPBlocklist(reader io.Reader) error`. FakeBackend no-ops. Engine wrapper passes through.

- [ ] Commit: `feat(engine): SetIPBlocklist via anacrolix iplist`.

---

### Task 6: Service blocklist methods

```go
const (
	settingBlocklistURL     = "blocklist_url"
	settingBlocklistEnabled = "blocklist_enabled"
)

type BlocklistDTO struct {
	URL          string `json:"url"`
	Enabled      bool   `json:"enabled"`
	LastLoadedAt int64  `json:"last_loaded_at"`
	Entries      int    `json:"entries"`
	Error        string `json:"error,omitempty"`
}

// In-memory state since blocklist text content can be large
type blocklistState struct {
	loadedAt time.Time
	entries  int
	lastErr  string
}

// Service struct gains:
//   blocklistMu sync.RWMutex
//   blocklist   blocklistState

func (s *Service) GetBlocklist(ctx context.Context) BlocklistDTO {
	url, _ := s.settings.Get(ctx, settingBlocklistURL)
	en := s.boolSetting(ctx, settingBlocklistEnabled)
	s.blocklistMu.RLock()
	defer s.blocklistMu.RUnlock()
	return BlocklistDTO{URL: url, Enabled: en, LastLoadedAt: s.blocklist.loadedAt.Unix(), Entries: s.blocklist.entries, Error: s.blocklist.lastErr}
}

func (s *Service) SetBlocklistURL(ctx context.Context, url string, enabled bool) error {
	if err := s.settings.Set(ctx, settingBlocklistURL, url); err != nil { return err }
	if err := s.setBoolSetting(ctx, settingBlocklistEnabled, enabled); err != nil { return err }
	if !enabled || url == "" {
		_ = s.engine.SetIPBlocklist(nil)
		s.blocklistMu.Lock()
		s.blocklist = blocklistState{}
		s.blocklistMu.Unlock()
		return nil
	}
	return s.RefreshBlocklist(ctx)
}

func (s *Service) RefreshBlocklist(ctx context.Context) error {
	url, _ := s.settings.Get(ctx, settingBlocklistURL)
	if url == "" { return errors.New("no blocklist URL configured") }

	httpCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(httpCtx, "GET", url, nil)
	if err != nil { return err }
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		s.blocklistMu.Lock()
		s.blocklist.lastErr = err.Error()
		s.blocklistMu.Unlock()
		return err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 50*1024*1024)) // 50MB safety cap
	if err != nil { return err }

	if err := s.engine.SetIPBlocklist(bytes.NewReader(body)); err != nil {
		s.blocklistMu.Lock()
		s.blocklist.lastErr = err.Error()
		s.blocklistMu.Unlock()
		return err
	}

	s.blocklistMu.Lock()
	s.blocklist = blocklistState{loadedAt: time.Now(), entries: countLines(body), lastErr: ""}
	s.blocklistMu.Unlock()
	return nil
}

func countLines(b []byte) int {
	n := 0
	for _, x := range b { if x == '\n' { n++ } }
	return n
}
```

Add `"bytes"`, `"io"`, `"net/http"`, `"errors"`, `"time"` imports.

- [ ] Bind on App: `GetBlocklist`, `SetBlocklistURL`, `RefreshBlocklist`. Regenerate. Commit: `feat(api): blocklist URL + refresh + IP filter wiring`.

---

## Section D — Frontend: Schedule + Blocklist panes

### Task 7: Frontend bindings + store

bindings.ts: `ScheduleRuleDTO`, `BlocklistDTO`, plus 8 new api methods (4 schedule CRUD + 3 blocklist + GetBlocklist).

store.ts: `scheduleRules`, `blocklist` state. Initial fetch. Mutating methods: createScheduleRule, updateScheduleRule, deleteScheduleRule, setBlocklistURL, refreshBlocklist.

- [ ] Commit: `feat(frontend): bindings + store for schedule rules + blocklist`.

---

### Task 8: SchedulePane

`frontend/src/components/settings/SchedulePane.tsx`. Lists existing rules in a table-style layout. "+ New rule" button reveals an inline form: 7 day-of-week checkboxes, start time + end time (HH:MM inputs), down/up kbps, alt_only checkbox, enabled checkbox. On save, call store.createScheduleRule. Edit-in-place on row click. Delete with confirm.

For day-of-week storage: `Sun=1, Mon=2, Tue=4, Wed=8, Thu=16, Fri=32, Sat=64`. Helper `dayLabels(mask)` renders `'Mon-Fri'` for 0b0111110, individual day names for sparse, etc.

- [ ] Commit: `feat(frontend): SchedulePane with rule CRUD`.

---

### Task 9: BlocklistPane

```tsx
import {createSignal, createEffect, Show} from 'solid-js';
import {RefreshCw} from 'lucide-solid';
import type {BlocklistDTO} from '../../lib/bindings';
import {fmtTimestamp} from '../../lib/format';
import {Button} from '../ui/Button';

type Props = {
  blocklist: BlocklistDTO;
  onSetBlocklistURL: (url: string, enabled: boolean) => Promise<void>;
  onRefreshBlocklist: () => Promise<void>;
};

export function BlocklistPane(props: Props) {
  const [url, setUrl] = createSignal(props.blocklist.url);
  const [enabled, setEnabled] = createSignal(props.blocklist.enabled);
  const [refreshing, setRefreshing] = createSignal(false);

  createEffect(() => { setUrl(props.blocklist.url); setEnabled(props.blocklist.enabled); });

  return (
    <div class="mx-auto max-w-2xl px-6 py-6">
      {/* PaneHeader */}
      <div class="mb-4 border-b border-white/[.04] pb-3">
        <h2 class="text-lg font-semibold text-zinc-100">IP Blocklist</h2>
        <p class="mt-0.5 text-sm text-zinc-500">Block peers in PeerGuardian-format ranges.</p>
      </div>

      <div class="space-y-3">
        <div>
          <label class="text-xs text-zinc-500 mb-1 block">Blocklist URL</label>
          <input
            type="text"
            class="w-full rounded border border-white/[.06] bg-black/30 px-2 py-1.5 font-mono text-xs text-zinc-100"
            value={url()}
            onInput={(e) => setUrl(e.currentTarget.value)}
            placeholder="https://example.com/blocklist.gz"
          />
        </div>
        <label class="inline-flex items-center gap-2 text-sm text-zinc-200">
          <input type="checkbox" checked={enabled()} onChange={(e) => setEnabled(e.currentTarget.checked)} />
          Enable blocklist
        </label>

        <div class="flex justify-between items-center pt-3 border-t border-white/[.04]">
          <div class="text-xs text-zinc-500">
            <Show when={props.blocklist.last_loaded_at > 0} fallback={<span>Never loaded</span>}>
              <span>Last loaded {fmtTimestamp(props.blocklist.last_loaded_at)} · {props.blocklist.entries} entries</span>
            </Show>
            <Show when={props.blocklist.error}>
              <div class="text-rose-400 mt-1">Error: {props.blocklist.error}</div>
            </Show>
          </div>
          <div class="flex gap-2">
            <Button
              variant="secondary"
              disabled={!enabled() || !url() || refreshing()}
              onClick={async () => {
                setRefreshing(true);
                try { await props.onRefreshBlocklist(); }
                finally { setRefreshing(false); }
              }}
            >
              <RefreshCw class={`h-3.5 w-3.5 ${refreshing() ? 'animate-spin' : ''}`} />
              Refresh
            </Button>
            <Button variant="primary" onClick={() => props.onSetBlocklistURL(url(), enabled())}>
              Save
            </Button>
          </div>
        </div>
      </div>
    </div>
  );
}
```

- [ ] Commit: `feat(frontend): BlocklistPane with URL config + refresh`.

---

### Task 10: Wire panes into SettingsSidebar + SettingsRoute + App.tsx

Add `'schedule'` and `'blocklist'` to SettingsPane union. Add Calendar (Schedule) + Shield (Blocklist) icons in the sidebar between Connection and Categories.

SettingsRoute Match for both.

App.tsx threads `store.state.scheduleRules` + `store.state.blocklist` plus mutating handlers.

- [ ] Commit: `feat(frontend): SettingsRoute wires Schedule + Blocklist panes`.

---

## Section E — Tracker status refinement

### Task 11: AnacrolixBackend tracker status — best-effort

In `DetailedSnapshot`, replace the hardcoded `Status: "OK"`. anacrolix's `t.AnnounceList()` returns the announce list but per-announce result state is internal. Two practical options:

**A.** Keep "OK" if the torrent has non-zero `t.Stats().ConnectedSeeders + ConnectedPeers` (a heuristic — it implies *some* tracker worked).

**B.** "Updating" if metadata hasn't loaded yet (`t.Info() == nil`), "OK" otherwise.

Combine both:
```go
status := "OK"
if t.Info() == nil { status = "Updating" }
// Note: per-tracker error state isn't exposed by v1.61; "OK" is best-effort.
```

Comment the limitation. Plan 6+ may add an upstream PR or alternate engine.

- [ ] Commit: `feat(engine): tracker status — Updating when metadata pending`.

---

### Task 12: TrackersTab status badge variants

`frontend/src/components/inspector/TrackersTab.tsx` — extend the classList for status:

```tsx
classList={{
  'bg-seed/[.10] text-seed': t.status === 'OK',
  'bg-amber-500/[.10] text-amber-400': t.status === 'Updating',
  'bg-rose-500/[.10] text-rose-300': t.status.startsWith('Error'),
  'bg-zinc-700/30 text-zinc-400': !['OK', 'Updating'].includes(t.status) && !t.status.startsWith('Error'),
}}
```

- [ ] Commit: `feat(frontend): TrackersTab status badge variants`.

---

## Section F — Smoke test

### Task 13: User-driven smoke

- [ ] **Step 1: Run `~/go/bin/wails dev -skipembedcreate`**
- [ ] **Step 2: Settings → Schedule** → New rule with Mon-Fri 22:00-06:00, alt-only checked, enabled. Save. Verify list shows it.
- [ ] **Step 3: Settings → Blocklist** → URL `https://example.com/missing.txt` + enabled + Save → expect Error in status. Replace URL with a real PeerGuardian URL (or leave blank and toggle Disabled).
- [ ] **Step 4: Inspector → Trackers** on a torrent before metadata loads → "Updating" amber badge; after → "OK" emerald.
- [ ] **Step 5: Tag** `plan-4d-scheduling-and-blocklist-complete`, push.

---

**End of Plan 4d.**
