# Mosaic — Plan 4c: Queueing + Alt-Speed + Connection Settings

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the toolbar Zap button stop being inert and add the queueing controls a power user expects from a torrent client. Queueing pauses torrents past the configured active-slot limit and resumes them when slots free up; force-start lets a specific torrent bypass the queue. Alt-speed limits give a one-toggle "throttle for now" mode with separate rate inputs. The new Settings → Connection pane houses both. After this plan: starting your 50th torrent automatically queues it; clicking Zap throttles all transfers to your night-mode rates; right-clicking a row lets you reorder it in the queue.

**Architecture:** Backend grows a `Scheduler` goroutine that owns the queue invariant — it pauses overflow torrents and resumes them by priority order, distinguishing its own scheduler-pauses from the user's manual pauses. The engine `Backend` interface gains `SetGlobalRateLimits(down, up int)` and a new `Queued`/`ForceStart` field shape on `Snapshot`. The api Service grows queue-management methods (SetQueuePosition, ForceStart, SetMaxActive) and a typed Settings expansion (alt-speed pair). Frontend gets a new Settings → Connection pane, a working toolbar Zap button, queue actions in the row context menu, and a small "Q" queue-position badge on cards.

**Tech additions:** none.

**Reference spec:** `docs/superpowers/specs/2026-04-28-bittorrent-client-design.md` §4.5 (Zap button), §5.4 / §6 (queueing semantics).

**Aesthetic continuity:** Same tokens. Queue badge uses a small zinc-700/30 pill with the position number in tabular monospace. Force-started torrents get an amber star indicator. Active Zap toolbar button uses `text-accent-400` and a subtle background tint when alt-speed is on.

---

## Out of Scope (deferred to Plan 4d)

- **Bandwidth scheduling** — time-of-day rate-limit profiles
- **IP filtering / blocklists**
- **Tracker status refinement** beyond "always OK"
- **DHT / encryption / port runtime toggles** — currently restart-only; Plan 4d may add restart-and-apply
- **RSS auto-add** — Plan 5

---

## File Structure (final state)

```
backend/
├── persistence/
│   ├── migrations/
│   │   ├── 0001_initial.sql
│   │   ├── 0002_categories_tags.sql
│   │   └── 0003_queue_columns.sql        # NEW: ALTER torrents ADD queue_position, force_start
│   └── torrents.go                        # MODIFIED: TorrentRecord adds QueuePosition, ForceStart + setters
├── engine/
│   ├── types.go                           # MODIFIED: Snapshot adds QueuePosition, ForceStart, Queued
│   ├── engine.go                          # MODIFIED: SetGlobalRateLimits passthrough
│   ├── anacrolix.go                       # MODIFIED: SetGlobalRateLimits using client.SetUploadLimit + DownloadLimit
│   ├── fake.go                            # MODIFIED: same shape additions
│   └── scheduler.go                       # NEW: Scheduler goroutine
└── api/
    ├── service.go                         # MODIFIED: Queue + alt-speed methods, settings keys
    └── service_test.go
app.go                                     # NEW bindings

frontend/src/
├── lib/
│   ├── bindings.ts                        # NEW api methods, Torrent fields
│   └── store.ts                           # NEW alt-speed state, queue actions
└── components/
    ├── settings/
    │   ├── ConnectionPane.tsx             # NEW: alt-speed + queue limits
    │   └── SettingsSidebar.tsx            # MODIFIED: Connection pane added
    ├── shell/
    │   ├── TopToolbar.tsx                 # MODIFIED: Zap button is real
    │   └── StatusBar.tsx                  # MODIFIED: queued count
    └── list/
        ├── TorrentCard.tsx                # MODIFIED: queue badge + force-start star
        ├── TorrentTable.tsx               # MODIFIED: same
        └── TorrentRowMenu.tsx             # MODIFIED: queue submenu
```

---

## Section A — Persistence: queue columns

### Task 1: Migration 0003

**Files:** Create `backend/persistence/migrations/0003_queue_columns.sql`

- [ ] **Step 1: Write migration**

```sql
-- +goose Up
ALTER TABLE torrents ADD COLUMN queue_position INTEGER NOT NULL DEFAULT 0;
ALTER TABLE torrents ADD COLUMN force_start INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE torrents DROP COLUMN force_start;
ALTER TABLE torrents DROP COLUMN queue_position;
```

- [ ] **Step 2: Update `TestOpen_RunsMigrations` to confirm columns exist**

In `backend/persistence/db_test.go`, add a query that checks `pragma_table_info(torrents)` for `queue_position` and `force_start`.

```go
require.Eventually(t, func() bool {
	rows, err := db.SQL().Query(`SELECT name FROM pragma_table_info('torrents')`)
	require.NoError(t, err)
	defer rows.Close()
	have := map[string]bool{}
	for rows.Next() {
		var n string
		_ = rows.Scan(&n)
		have[n] = true
	}
	return have["queue_position"] && have["force_start"]
}, time.Second, 50*time.Millisecond)
```

- [ ] **Step 3: Run, confirm pass**
- [ ] **Step 4: Commit**

```bash
git add backend/persistence/migrations/0003_queue_columns.sql backend/persistence/db_test.go
git commit -m "feat(persistence): migration 0003 — queue_position + force_start on torrents"
```

---

### Task 2: TorrentRecord + Torrents DAO extensions

**Files:** `backend/persistence/torrents.go`, `backend/persistence/torrents_test.go`

- [ ] **Step 1: Extend TorrentRecord**

```go
type TorrentRecord struct {
	InfoHash       string
	Name           string
	Magnet         string
	SavePath       string
	CategoryID     *int
	AddedAt        time.Time
	CompletedAt    *time.Time
	Paused         bool
	QueuePosition  int   // 0 = top
	ForceStart     bool
}
```

- [ ] **Step 2: Update Save / Get / List INSERT/SELECT/scan**

Add `queue_position`, `force_start` to the INSERT/UPDATE column list and the SELECT lists. `scanTorrent` reads them as ints.

- [ ] **Step 3: Add `SetQueuePosition` and `SetForceStart` methods**

```go
func (t *Torrents) SetQueuePosition(ctx context.Context, infohash string, pos int) error {
	_, err := t.db.SQL().ExecContext(ctx,
		`UPDATE torrents SET queue_position = ? WHERE infohash = ?`, pos, infohash)
	return err
}

func (t *Torrents) SetForceStart(ctx context.Context, infohash string, force bool) error {
	v := 0
	if force { v = 1 }
	_, err := t.db.SQL().ExecContext(ctx,
		`UPDATE torrents SET force_start = ? WHERE infohash = ?`, v, infohash)
	return err
}
```

- [ ] **Step 4: Add tests**

```go
func TestTorrents_QueueAndForceStart(t *testing.T) {
	db := newTestDB(t)
	tr := NewTorrents(db)
	ctx := context.Background()
	require.NoError(t, tr.Save(ctx, TorrentRecord{InfoHash: "q1", Name: "n", SavePath: "/p", AddedAt: time.Now()}))

	// Default values
	got, _ := tr.Get(ctx, "q1")
	require.Equal(t, 0, got.QueuePosition)
	require.False(t, got.ForceStart)

	require.NoError(t, tr.SetQueuePosition(ctx, "q1", 5))
	require.NoError(t, tr.SetForceStart(ctx, "q1", true))

	got, _ = tr.Get(ctx, "q1")
	require.Equal(t, 5, got.QueuePosition)
	require.True(t, got.ForceStart)
}
```

- [ ] **Step 5: Run + commit**

```bash
go test ./backend/persistence/ -v -race
git add backend/persistence/torrents.go backend/persistence/torrents_test.go
git commit -m "feat(persistence): TorrentRecord grows queue_position + force_start"
```

---

## Section B — Engine: Snapshot + rate limits + scheduler

### Task 3: Snapshot fields

**Files:** `backend/engine/types.go`, `backend/engine/anacrolix.go`, `backend/engine/fake.go`

- [ ] **Step 1: Extend Snapshot**

```go
type Snapshot struct {
	// ... existing ...
	QueuePosition int  // 0 = top of queue
	ForceStart    bool
	Queued        bool // true if scheduler is holding it back
}
```

- [ ] **Step 2: anacrolix snapshotFor signature gains queuePosition, forceStart, queued**

The `snapshotFor` helper takes them as args. AnacrolixBackend reads from new internal maps `queuePos`, `forceStart`, `scheduledPause` (the scheduler's own pause set). Initialize maps in `NewAnacrolixBackend`.

- [ ] **Step 3: Backend interface adds setters**

```go
type Backend interface {
	// ... existing ...
	SetGlobalRateLimits(downBytesPerSec, upBytesPerSec int) error // 0 = unlimited
	SetQueuePosition(id TorrentID, pos int)
	SetForceStart(id TorrentID, force bool)
	ScheduledPause(id TorrentID, paused bool) // distinct from manual Pause
}
```

- [ ] **Step 4: Implement on FakeBackend**

Trivial — store in maps, return them from snapshotFor.

- [ ] **Step 5: Implement on AnacrolixBackend**

In `NewAnacrolixBackend`, install fresh limiters on the `ClientConfig` and stash the pointers on the backend struct so we can mutate them later:

```go
dlLim := rate.NewLimiter(rate.Inf, 256<<10)
ulLim := rate.NewLimiter(rate.Inf, 256<<10)
tcfg.DownloadRateLimiter = dlLim
tcfg.UploadRateLimiter   = ulLim
// ...
a := &AnacrolixBackend{
    // ...
    dlLim: dlLim,
    ulLim: ulLim,
}
```

Then mutate them in place:

```go
func (a *AnacrolixBackend) SetGlobalRateLimits(downBPS, upBPS int) error {
	if downBPS <= 0 {
		a.dlLim.SetLimit(rate.Inf)
		a.dlLim.SetBurst(256 << 10)
	} else {
		a.dlLim.SetLimit(rate.Limit(downBPS))
		a.dlLim.SetBurst(max(downBPS, 256<<10))
	}
	if upBPS <= 0 {
		a.ulLim.SetLimit(rate.Inf)
		a.ulLim.SetBurst(256 << 10)
	} else {
		a.ulLim.SetLimit(rate.Limit(upBPS))
		a.ulLim.SetBurst(max(upBPS, 256<<10))
	}
	return nil
}
```

(Add `"golang.org/x/time/rate"` import. anacrolix v1.61 stores the rate limiters as `*rate.Limiter` fields on `ClientConfig` (`config.DownloadRateLimiter` / `config.UploadRateLimiter`) — there are no `SetDownloadRateLimiter` / `SetUploadRateLimiter` setters on `*torrent.Client`, and the same limiter pointer is referenced from many runtime spots, so we mutate the limiter in place via `SetLimit` + `SetBurst`. The "burst" must be at least the largest peer message; 256 KB is safe.)

`SetQueuePosition`, `SetForceStart`, `ScheduledPause` write to internal maps under their existing mutex pattern.

`ScheduledPause(id, true)` calls `t.SetMaxEstablishedConns(0)` exactly like manual Pause; `ScheduledPause(id, false)` calls `t.SetMaxEstablishedConns(80)`. The internal `scheduledPause` map flag is what `snapshotFor` reads to set `Queued`.

- [ ] **Step 6: Engine wrapper passthroughs**

```go
func (e *Engine) SetGlobalRateLimits(d, u int) error { return e.backend.SetGlobalRateLimits(d, u) }
func (e *Engine) SetQueuePosition(id TorrentID, pos int) { e.backend.SetQueuePosition(id, pos) }
func (e *Engine) SetForceStart(id TorrentID, force bool) { e.backend.SetForceStart(id, force) }
func (e *Engine) ScheduledPause(id TorrentID, paused bool) { e.backend.ScheduledPause(id, paused) }
```

- [ ] **Step 7: Verify build**

```bash
go build ./...
go test ./backend/engine/ -v -race
```

- [ ] **Step 8: Commit**

```bash
git add backend/engine/
git commit -m "feat(engine): Snapshot grows queue/force fields; SetGlobalRateLimits + queue setters"
```

---

### Task 4: Scheduler goroutine

**Files:** Create `backend/engine/scheduler.go`, `backend/engine/scheduler_test.go`

- [ ] **Step 1: Scheduler implementation**

```go
package engine

import (
	"sort"
	"sync"
	"time"
)

// Scheduler enforces the active-torrent-slot limits by ScheduledPause-ing
// overflow torrents and resuming them when slots free up. It does NOT
// touch torrents that the user has manually paused.
type Scheduler struct {
	engine *Engine

	mu                  sync.RWMutex
	maxActiveDownloads  int // 0 = unlimited
	maxActiveSeeds      int

	stop chan struct{}
}

func NewScheduler(eng *Engine, maxDL, maxSeeds int, tickEvery time.Duration) *Scheduler {
	s := &Scheduler{engine: eng, maxActiveDownloads: maxDL, maxActiveSeeds: maxSeeds, stop: make(chan struct{})}
	go s.run(tickEvery)
	return s
}

func (s *Scheduler) SetLimits(maxDL, maxSeeds int) {
	s.mu.Lock()
	s.maxActiveDownloads = maxDL
	s.maxActiveSeeds = maxSeeds
	s.mu.Unlock()
}

func (s *Scheduler) Limits() (int, int) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.maxActiveDownloads, s.maxActiveSeeds
}

func (s *Scheduler) Close() { close(s.stop) }

func (s *Scheduler) run(every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-s.stop:
			return
		case <-t.C:
			s.tick()
		}
	}
}

func (s *Scheduler) tick() {
	s.mu.RLock()
	maxDL, maxSeeds := s.maxActiveDownloads, s.maxActiveSeeds
	s.mu.RUnlock()

	all := s.engine.List()

	// Partition: downloading vs seeding (completed). Skip user-paused torrents.
	var downloading, seeding []Snapshot
	for _, snap := range all {
		if snap.Paused {
			continue // user-paused, leave alone (also clear any scheduler hold)
		}
		if snap.Completed {
			seeding = append(seeding, snap)
		} else {
			downloading = append(downloading, snap)
		}
	}

	apply := func(group []Snapshot, max int) {
		sort.Slice(group, func(i, j int) bool {
			// Force-started first, then by queue_position ascending
			if group[i].ForceStart != group[j].ForceStart {
				return group[i].ForceStart
			}
			return group[i].QueuePosition < group[j].QueuePosition
		})
		// Active count includes force-starts; max=0 means unlimited
		active := 0
		for _, snap := range group {
			shouldRun := snap.ForceStart || max == 0 || active < max
			if shouldRun {
				if snap.Queued { s.engine.ScheduledPause(snap.ID, false) }
				active++
			} else {
				if !snap.Queued { s.engine.ScheduledPause(snap.ID, true) }
			}
		}
	}

	apply(downloading, maxDL)
	apply(seeding, maxSeeds)
}
```

- [ ] **Step 2: Test**

```go
func TestScheduler_PausesOverflow(t *testing.T) {
	fb := NewFakeBackend()
	eng := NewEngine(fb, 50*time.Millisecond)
	t.Cleanup(func() { _ = eng.Close() })

	for i := 0; i < 5; i++ {
		id, _ := eng.AddMagnet(context.Background(), fmt.Sprintf("magnet:?xt=urn:btih:s%d", i), "/tmp")
		fb.SetQueuePosition(id, i)
	}

	s := NewScheduler(eng, 2, 0, 50*time.Millisecond)
	t.Cleanup(s.Close)

	require.Eventually(t, func() bool {
		queued := 0
		for _, snap := range eng.List() {
			if snap.Queued { queued++ }
		}
		return queued == 3 // 5 - 2 active = 3 queued
	}, 2*time.Second, 100*time.Millisecond)
}

func TestScheduler_ForceStartBypassesLimit(t *testing.T) {
	fb := NewFakeBackend()
	eng := NewEngine(fb, 50*time.Millisecond)
	t.Cleanup(func() { _ = eng.Close() })

	id1, _ := eng.AddMagnet(context.Background(), "magnet:?xt=urn:btih:f1", "/tmp")
	id2, _ := eng.AddMagnet(context.Background(), "magnet:?xt=urn:btih:f2", "/tmp")
	id3, _ := eng.AddMagnet(context.Background(), "magnet:?xt=urn:btih:f3", "/tmp")
	fb.SetQueuePosition(id1, 0)
	fb.SetQueuePosition(id2, 1)
	fb.SetQueuePosition(id3, 2)
	fb.SetForceStart(id3, true) // bottom of queue but force-started

	s := NewScheduler(eng, 1, 0, 50*time.Millisecond)
	t.Cleanup(s.Close)

	require.Eventually(t, func() bool {
		var snap1, snap3 Snapshot
		for _, s := range eng.List() {
			if s.ID == id1 { snap1 = s }
			if s.ID == id3 { snap3 = s }
		}
		return !snap1.Queued && !snap3.Queued
	}, 2*time.Second, 100*time.Millisecond)
}
```

- [ ] **Step 3: Run, verify pass**
- [ ] **Step 4: Commit**

```bash
git add backend/engine/scheduler.go backend/engine/scheduler_test.go
git commit -m "feat(engine): Scheduler goroutine — queue overflow with force-start bypass"
```

---

## Section C — API: queue + alt-speed methods

### Task 5: Service queue methods + alt-speed settings

**Files:** `backend/api/service.go`, `backend/api/service_test.go`

- [ ] **Step 1: Add settings keys + service methods**

```go
const (
	settingMaxActiveDL    = "max_active_downloads"
	settingMaxActiveSeeds = "max_active_seeds"
	settingDownKbps       = "down_kbps"
	settingUpKbps         = "up_kbps"
	settingAltDownKbps    = "alt_down_kbps"
	settingAltUpKbps      = "alt_up_kbps"
	settingAltActive      = "alt_active"
)

type LimitsDTO struct {
	DownKbps      int  `json:"down_kbps"`
	UpKbps        int  `json:"up_kbps"`
	AltDownKbps   int  `json:"alt_down_kbps"`
	AltUpKbps     int  `json:"alt_up_kbps"`
	AltActive     bool `json:"alt_active"`
}

type QueueLimitsDTO struct {
	MaxActiveDownloads int `json:"max_active_downloads"`
	MaxActiveSeeds     int `json:"max_active_seeds"`
}

func (s *Service) GetLimits(ctx context.Context) (LimitsDTO, error) {
	return LimitsDTO{
		DownKbps:    s.intSetting(ctx, settingDownKbps),
		UpKbps:      s.intSetting(ctx, settingUpKbps),
		AltDownKbps: s.intSetting(ctx, settingAltDownKbps),
		AltUpKbps:   s.intSetting(ctx, settingAltUpKbps),
		AltActive:   s.boolSetting(ctx, settingAltActive),
	}, nil
}

func (s *Service) SetLimits(ctx context.Context, l LimitsDTO) error {
	if err := s.setIntSetting(ctx, settingDownKbps, l.DownKbps); err != nil { return err }
	if err := s.setIntSetting(ctx, settingUpKbps,   l.UpKbps); err != nil { return err }
	if err := s.setIntSetting(ctx, settingAltDownKbps, l.AltDownKbps); err != nil { return err }
	if err := s.setIntSetting(ctx, settingAltUpKbps,   l.AltUpKbps); err != nil { return err }
	if err := s.setBoolSetting(ctx, settingAltActive, l.AltActive); err != nil { return err }
	return s.applyLimits(ctx)
}

func (s *Service) ToggleAltSpeed(ctx context.Context) (bool, error) {
	cur := s.boolSetting(ctx, settingAltActive)
	next := !cur
	if err := s.setBoolSetting(ctx, settingAltActive, next); err != nil { return cur, err }
	return next, s.applyLimits(ctx)
}

func (s *Service) applyLimits(ctx context.Context) error {
	l, _ := s.GetLimits(ctx)
	down, up := l.DownKbps*1024, l.UpKbps*1024
	if l.AltActive {
		down, up = l.AltDownKbps*1024, l.AltUpKbps*1024
	}
	return s.engine.SetGlobalRateLimits(down, up)
}

// Queue
func (s *Service) GetQueueLimits(ctx context.Context) QueueLimitsDTO {
	return QueueLimitsDTO{
		MaxActiveDownloads: s.intSetting(ctx, settingMaxActiveDL),
		MaxActiveSeeds:     s.intSetting(ctx, settingMaxActiveSeeds),
	}
}

func (s *Service) SetQueueLimits(ctx context.Context, q QueueLimitsDTO) error {
	if err := s.setIntSetting(ctx, settingMaxActiveDL, q.MaxActiveDownloads); err != nil { return err }
	if err := s.setIntSetting(ctx, settingMaxActiveSeeds, q.MaxActiveSeeds); err != nil { return err }
	if s.scheduler != nil {
		s.scheduler.SetLimits(q.MaxActiveDownloads, q.MaxActiveSeeds)
	}
	return nil
}

func (s *Service) SetQueuePosition(ctx context.Context, infohash string, pos int) error {
	if err := s.torrents.SetQueuePosition(ctx, infohash, pos); err != nil { return err }
	s.engine.SetQueuePosition(engine.TorrentID(infohash), pos)
	return nil
}

func (s *Service) SetForceStart(ctx context.Context, infohash string, force bool) error {
	if err := s.torrents.SetForceStart(ctx, infohash, force); err != nil { return err }
	s.engine.SetForceStart(engine.TorrentID(infohash), force)
	return nil
}

func (s *Service) intSetting(ctx context.Context, key string) int {
	v, err := s.settings.Get(ctx, key)
	if err != nil { return 0 }
	n, _ := strconv.Atoi(v)
	return n
}

func (s *Service) setIntSetting(ctx context.Context, key string, n int) error {
	return s.settings.Set(ctx, key, strconv.Itoa(n))
}

func (s *Service) boolSetting(ctx context.Context, key string) bool {
	v, _ := s.settings.Get(ctx, key)
	return v == "true"
}

func (s *Service) setBoolSetting(ctx context.Context, key string, b bool) error {
	v := "false"
	if b { v = "true" }
	return s.settings.Set(ctx, key, v)
}
```

Add `"strconv"` import. Service struct grows `scheduler *engine.Scheduler` field; `NewService` takes it; main.go passes it.

- [ ] **Step 2: Add tests**

```go
func TestService_LimitsRoundTrip(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	require.NoError(t, svc.SetLimits(ctx, LimitsDTO{DownKbps: 1000, UpKbps: 100, AltDownKbps: 200, AltUpKbps: 50}))
	got, _ := svc.GetLimits(ctx)
	require.Equal(t, 1000, got.DownKbps)
	require.Equal(t, 200, got.AltDownKbps)
}

func TestService_ToggleAltSpeed(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	on, err := svc.ToggleAltSpeed(ctx)
	require.NoError(t, err)
	require.True(t, on)
	on, _ = svc.ToggleAltSpeed(ctx)
	require.False(t, on)
}

func TestService_QueuePosition(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	id, _ := svc.AddMagnet(ctx, "magnet:?xt=urn:btih:qp", "")
	require.NoError(t, svc.SetQueuePosition(ctx, string(id), 7))
	rows, _ := svc.ListTorrents(ctx)
	require.Equal(t, 7, rows[0].QueuePosition)
}
```

`TorrentDTO` extends:
```go
QueuePosition int  `json:"queue_position"`
ForceStart    bool `json:"force_start"`
Queued        bool `json:"queued"`
```

`toDTO` populates them from the snapshot.

- [ ] **Step 3: Update `newTestService` signature with the scheduler param (or pass nil for tests that don't exercise it)**

```go
svc := NewService(eng, ..., nil /* no scheduler in tests */, "/tmp/dl")
```

Service.SetQueueLimits should no-op gracefully when scheduler is nil.

- [ ] **Step 4: Update main.go**

```go
sched := engine.NewScheduler(eng, 0, 0, 2*time.Second) // 0/0 = unlimited until user sets
defer sched.Close()
svc := api.NewService(eng, persistence.NewTorrents(db), persistence.NewCategories(db),
                      persistence.NewTags(db), persistence.NewSettings(db),
                      sched,
                      cfg.DefaultSavePath)

// On startup, apply persisted limits
sched.SetLimits(svc.GetQueueLimits(ctx).MaxActiveDownloads, svc.GetQueueLimits(ctx).MaxActiveSeeds)
_ = svc.applyLimits(ctx) // also applies engine rate limits — wait, applyLimits is unexported

// Better: add an exported `Service.RestoreOnStartup(ctx)` method that loads queue limits + rate limits + ScheduledPause flags.
```

Add `RestoreOnStartup(ctx)` to Service:

```go
func (s *Service) RestoreOnStartup(ctx context.Context) error {
	q := s.GetQueueLimits(ctx)
	if s.scheduler != nil { s.scheduler.SetLimits(q.MaxActiveDownloads, q.MaxActiveSeeds) }
	return s.applyLimits(ctx)
}
```

Call from main.go after constructing the service.

- [ ] **Step 5: Run + commit**

```bash
go test ./... -race -count=1
git add backend/api/ main.go
git commit -m "feat(api): queue + alt-speed methods, RestoreOnStartup, TorrentDTO grows queue fields"
```

---

### Task 6: Wails bindings

**Files:** `app.go`

- [ ] **Step 1: Bind 7 methods**

```go
func (a *App) GetLimits() (api.LimitsDTO, error)            { return a.svc.GetLimits(a.ctx) }
func (a *App) SetLimits(l api.LimitsDTO) error              { return a.svc.SetLimits(a.ctx, l) }
func (a *App) ToggleAltSpeed() (bool, error)                { return a.svc.ToggleAltSpeed(a.ctx) }
func (a *App) GetQueueLimits() api.QueueLimitsDTO           { return a.svc.GetQueueLimits(a.ctx) }
func (a *App) SetQueueLimits(q api.QueueLimitsDTO) error    { return a.svc.SetQueueLimits(a.ctx, q) }
func (a *App) SetQueuePosition(infohash string, pos int) error  { return a.svc.SetQueuePosition(a.ctx, infohash, pos) }
func (a *App) SetForceStart(infohash string, force bool) error  { return a.svc.SetForceStart(a.ctx, infohash, force) }
```

- [ ] **Step 2: Regenerate Wails bindings**

```bash
$(go env GOPATH)/bin/wails generate module
```

- [ ] **Step 3: Verify + commit**

```bash
go build ./...
git add app.go
git commit -m "feat: Wails bindings for limits, queue, force-start"
```

---

## Section D — Frontend: store, Connection pane, toolbar Zap, queue UI

### Task 7: Frontend bindings + store

**Files:** `frontend/src/lib/bindings.ts`, `frontend/src/lib/store.ts`

- [ ] **Step 1: Extend bindings**

```ts
export type LimitsDTO = {
  down_kbps: number;
  up_kbps: number;
  alt_down_kbps: number;
  alt_up_kbps: number;
  alt_active: boolean;
};

export type QueueLimitsDTO = {
  max_active_downloads: number;
  max_active_seeds: number;
};

// Torrent type extension:
export type Torrent = {
  // ... existing ...
  queue_position: number;
  force_start: boolean;
  queued: boolean;
};

// api object additions:
getLimits: () => GetLimits() as Promise<LimitsDTO>,
setLimits: (l: LimitsDTO) => SetLimits(l),
toggleAltSpeed: () => ToggleAltSpeed() as Promise<boolean>,
getQueueLimits: () => GetQueueLimits() as Promise<QueueLimitsDTO>,
setQueueLimits: (q: QueueLimitsDTO) => SetQueueLimits(q),
setQueuePosition: (infohash: string, pos: number) => SetQueuePosition(infohash, pos),
setForceStart: (infohash: string, force: boolean) => SetForceStart(infohash, force),
```

- [ ] **Step 2: Extend store**

```ts
export type AppState = {
  // ... existing ...
  limits: LimitsDTO;
  queueLimits: QueueLimitsDTO;
};

const emptyLimits: LimitsDTO = {down_kbps: 0, up_kbps: 0, alt_down_kbps: 0, alt_up_kbps: 0, alt_active: false};
const emptyQueueLimits: QueueLimitsDTO = {max_active_downloads: 0, max_active_seeds: 0};

// Initial fetch on startup:
api.getLimits().then((l) => setState(produce((s) => { s.limits = l; })));
api.getQueueLimits().then((q) => setState(produce((s) => { s.queueLimits = q; })));

// Methods:
return {
  // ... existing ...
  setLimits: async (l: LimitsDTO) => {
    await api.setLimits(l);
    setState(produce((s) => { s.limits = l; }));
  },
  toggleAltSpeed: async () => {
    const next = await api.toggleAltSpeed();
    setState(produce((s) => { s.limits = {...s.limits, alt_active: next}; }));
  },
  setQueueLimits: async (q: QueueLimitsDTO) => {
    await api.setQueueLimits(q);
    setState(produce((s) => { s.queueLimits = q; }));
  },
  setQueuePosition: (infohash: string, pos: number) => api.setQueuePosition(infohash, pos),
  setForceStart: (infohash: string, force: boolean) => api.setForceStart(infohash, force),
};
```

- [ ] **Step 3: Verify + commit**

```bash
cd frontend && npm run build && npm test
git add frontend/src/lib/
git commit -m "feat(frontend): store + bindings for limits, queue, force-start"
```

---

### Task 8: ConnectionPane

**Files:** Create `frontend/src/components/settings/ConnectionPane.tsx`. Modify `SettingsSidebar.tsx` to add Connection.

- [ ] **Step 1: Add Connection to SettingsSidebar**

```ts
export type SettingsPane = 'general' | 'connection' | 'categories' | 'tags' | 'about';

const items = [
  {value: 'general',    label: 'General',    icon: Sliders},
  {value: 'connection', label: 'Connection', icon: Wifi},
  {value: 'categories', label: 'Categories', icon: Folder},
  {value: 'tags',       label: 'Tags',       icon: Tag},
  {value: 'about',      label: 'About',      icon: Info},
];
```

Add `Wifi` to lucide imports.

- [ ] **Step 2: ConnectionPane component**

```tsx
import {createSignal, createEffect} from 'solid-js';
import {toast} from 'solid-sonner';
import type {LimitsDTO, QueueLimitsDTO} from '../../lib/bindings';
import {Button} from '../ui/Button';

type Props = {
  limits: LimitsDTO;
  queueLimits: QueueLimitsDTO;
  onSetLimits: (l: LimitsDTO) => Promise<void>;
  onSetQueueLimits: (q: QueueLimitsDTO) => Promise<void>;
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

function NumberInput(props: {value: number; onInput: (n: number) => void; suffix?: string; placeholder?: string}) {
  return (
    <div class="inline-flex items-center gap-1.5">
      <input
        type="number"
        min={0}
        class="w-28 rounded border border-white/[.06] bg-black/30 px-2 py-1 text-right font-mono text-sm tabular-nums text-zinc-100 focus:border-accent-500/50 focus:outline-none"
        value={props.value || ''}
        placeholder={props.placeholder ?? '0'}
        onInput={(e) => props.onInput(parseInt(e.currentTarget.value || '0', 10))}
      />
      {props.suffix && <span class="text-xs text-zinc-500">{props.suffix}</span>}
    </div>
  );
}

export function ConnectionPane(props: Props) {
  const [down, setDown] = createSignal(props.limits.down_kbps);
  const [up, setUp] = createSignal(props.limits.up_kbps);
  const [altDown, setAltDown] = createSignal(props.limits.alt_down_kbps);
  const [altUp, setAltUp] = createSignal(props.limits.alt_up_kbps);
  const [maxDL, setMaxDL] = createSignal(props.queueLimits.max_active_downloads);
  const [maxSeeds, setMaxSeeds] = createSignal(props.queueLimits.max_active_seeds);

  // Re-sync when prop changes (initial fetch races)
  createEffect(() => { setDown(props.limits.down_kbps); });
  createEffect(() => { setUp(props.limits.up_kbps); });
  createEffect(() => { setAltDown(props.limits.alt_down_kbps); });
  createEffect(() => { setAltUp(props.limits.alt_up_kbps); });
  createEffect(() => { setMaxDL(props.queueLimits.max_active_downloads); });
  createEffect(() => { setMaxSeeds(props.queueLimits.max_active_seeds); });

  const saveLimits = async () => {
    try {
      await props.onSetLimits({
        down_kbps: down(),
        up_kbps: up(),
        alt_down_kbps: altDown(),
        alt_up_kbps: altUp(),
        alt_active: props.limits.alt_active,
      });
      toast.success('Bandwidth limits saved');
    } catch (e) { toast.error(String(e)); }
  };

  const saveQueue = async () => {
    try {
      await props.onSetQueueLimits({max_active_downloads: maxDL(), max_active_seeds: maxSeeds()});
      toast.success('Queue limits saved');
    } catch (e) { toast.error(String(e)); }
  };

  return (
    <div class="mx-auto max-w-2xl px-6 py-6">
      <PaneHeader title="Connection" subtitle="Bandwidth limits, alt-speed, and queue slots." />

      <div class="text-xs uppercase tracking-wider text-zinc-500 mt-2 mb-1 px-1">Bandwidth</div>
      <Field label="Download limit" help="0 = unlimited">
        <NumberInput value={down()} onInput={setDown} suffix="KB/s" />
      </Field>
      <Field label="Upload limit" help="0 = unlimited">
        <NumberInput value={up()} onInput={setUp} suffix="KB/s" />
      </Field>
      <Field label="Alt download" help="When alt-speed is on (toolbar Zap button)">
        <NumberInput value={altDown()} onInput={setAltDown} suffix="KB/s" />
      </Field>
      <Field label="Alt upload">
        <NumberInput value={altUp()} onInput={setAltUp} suffix="KB/s" />
      </Field>
      <div class="flex justify-end mt-3">
        <Button variant="primary" onClick={saveLimits}>Save bandwidth</Button>
      </div>

      <div class="text-xs uppercase tracking-wider text-zinc-500 mt-6 mb-1 px-1">Queue</div>
      <Field label="Max active downloads" help="0 = unlimited">
        <NumberInput value={maxDL()} onInput={setMaxDL} suffix="torrents" />
      </Field>
      <Field label="Max active seeds" help="0 = unlimited">
        <NumberInput value={maxSeeds()} onInput={setMaxSeeds} suffix="torrents" />
      </Field>
      <div class="flex justify-end mt-3">
        <Button variant="primary" onClick={saveQueue}>Save queue</Button>
      </div>
    </div>
  );
}
```

- [ ] **Step 3: Wire in SettingsRoute**

Add Match for `'connection'` rendering ConnectionPane.

Update SettingsRoute Props to take `limits`, `queueLimits`, `onSetLimits`, `onSetQueueLimits`.

- [ ] **Step 4: Wire in App.tsx**

Pass `store.state.limits`, `store.state.queueLimits`, `store.setLimits`, `store.setQueueLimits` through.

- [ ] **Step 5: Verify build + commit**

```bash
cd frontend && npm run build && npm test
git add frontend/src/components/settings/
git commit -m "feat(frontend): Connection settings pane (bandwidth + alt-speed + queue limits)"
```

---

### Task 9: Toolbar Zap button is real

**Files:** `frontend/src/components/shell/TopToolbar.tsx`, `frontend/src/App.tsx`

- [ ] **Step 1: Wire Zap button**

Update `TopToolbar` Props:
```ts
type Props = {
  // ... existing ...
  altSpeedActive: boolean;
  onToggleAltSpeed: () => void;
};
```

The Zap button:
```tsx
<Tooltip label={`Alt-speed limits ${props.altSpeedActive ? 'on' : 'off'}`}>
  <button
    class="grid h-7 w-7 place-items-center rounded-md transition-colors duration-150"
    classList={{
      'bg-accent-500/[.15] text-accent-300': props.altSpeedActive,
      'text-zinc-400 hover:bg-white/[.04] hover:text-zinc-100': !props.altSpeedActive,
    }}
    onClick={props.onToggleAltSpeed}
  >
    <Zap class="h-3.5 w-3.5" />
  </button>
</Tooltip>
```

(Remove `disabled` attribute.)

- [ ] **Step 2: Thread through WindowShell + App.tsx**

WindowShell Props gain `altSpeedActive` + `onToggleAltSpeed`. App.tsx passes `store.state.limits.alt_active` and `store.toggleAltSpeed`.

- [ ] **Step 3: Verify + commit**

```bash
cd frontend && npm run build
git add frontend/src/components/shell/TopToolbar.tsx frontend/src/components/shell/WindowShell.tsx frontend/src/App.tsx
git commit -m "feat(frontend): toolbar Zap button toggles alt-speed limits"
```

---

### Task 10: Queue badges + force-start star on cards/table

**Files:** `frontend/src/components/list/TorrentCard.tsx`, `frontend/src/components/list/TorrentTable.tsx`

- [ ] **Step 1: TorrentCard — add queue badge + star**

In the right side of the card header (next to size), add:
```tsx
<Show when={t().queued && !t().completed}>
  <span class="inline-flex items-center gap-1 rounded bg-zinc-800/60 px-1.5 py-0.5 font-mono text-[10px] tabular-nums text-zinc-400">
    Q{t().queue_position + 1}
  </span>
</Show>
<Show when={t().force_start}>
  <Star class="h-3 w-3 text-amber-400" fill="currentColor" />
</Show>
```

Import `Star` from lucide-solid.

- [ ] **Step 2: TorrentTable — same on a hover-revealed column** (or fold into the Name cell)

In the Name column cell, append the queue badge + star after the truncated name span.

- [ ] **Step 3: Verify + commit**

```bash
cd frontend && npm run build
git add frontend/src/components/list/TorrentCard.tsx frontend/src/components/list/TorrentTable.tsx
git commit -m "feat(frontend): queue position badge + force-start star on torrents"
```

---

### Task 11: Queue submenu in TorrentRowMenu

**Files:** `frontend/src/components/list/TorrentRowMenu.tsx`

- [ ] **Step 1: Add Queue submenu**

Add a Sub between Pause/Recheck and the existing items:

```tsx
<ContextMenu.Sub>
  <ContextMenu.SubTrigger>
    <ListOrdered class="h-3.5 w-3.5" />
    Queue
    <ChevronRight class="ml-auto h-3 w-3" />
  </ContextMenu.SubTrigger>
  <ContextMenu.SubContent>
    <ContextMenu.Item onSelect={() => props.onMoveQueue('top')}>
      Move to top
    </ContextMenu.Item>
    <ContextMenu.Item onSelect={() => props.onMoveQueue('up')}>
      Move up
    </ContextMenu.Item>
    <ContextMenu.Item onSelect={() => props.onMoveQueue('down')}>
      Move down
    </ContextMenu.Item>
    <ContextMenu.Item onSelect={() => props.onMoveQueue('bottom')}>
      Move to bottom
    </ContextMenu.Item>
    <ContextMenu.Separator />
    <ContextMenu.Item onSelect={() => props.onToggleForceStart()}>
      <Show when={props.forceStart} fallback={<>Force-start</>}>
        <Check class="h-3.5 w-3.5" />
        Force-start (active)
      </Show>
    </ContextMenu.Item>
  </ContextMenu.SubContent>
</ContextMenu.Sub>
```

Add `ListOrdered` to lucide-solid imports.

- [ ] **Step 2: Update TorrentList orchestrator + App.tsx to provide handlers**

In App.tsx, the queue actions need to know about the *other* torrents to compute new positions. Build a small helper:

```ts
const onMoveQueue = async (id: string, direction: 'top' | 'up' | 'down' | 'bottom') => {
  const sorted = [...store.state.torrents].sort((a, b) => a.queue_position - b.queue_position);
  const currentIdx = sorted.findIndex((t) => t.id === id);
  if (currentIdx < 0) return;
  let targetIdx: number;
  switch (direction) {
    case 'top':    targetIdx = 0; break;
    case 'bottom': targetIdx = sorted.length - 1; break;
    case 'up':     targetIdx = Math.max(0, currentIdx - 1); break;
    case 'down':   targetIdx = Math.min(sorted.length - 1, currentIdx + 1); break;
  }
  if (targetIdx === currentIdx) return;
  const moved = sorted.splice(currentIdx, 1)[0];
  sorted.splice(targetIdx, 0, moved);
  // Renumber 0..N
  await Promise.all(sorted.map((t, i) => store.setQueuePosition(t.id, i)));
};

const onToggleForceStart = async (id: string, current: boolean) => {
  await store.setForceStart(id, !current);
};
```

Wire these into TorrentList → TorrentRowMenu.

- [ ] **Step 3: Verify + commit**

```bash
cd frontend && npm run build
git add frontend/src/components/list/TorrentRowMenu.tsx frontend/src/components/list/TorrentList.tsx frontend/src/App.tsx
git commit -m "feat(frontend): row context-menu Queue submenu (move/force-start)"
```

---

### Task 12: StatusBar adds queued count

**Files:** `frontend/src/components/shell/StatusBar.tsx`

- [ ] **Step 1: Add queued count via a new GlobalStats field, OR compute client-side**

Simplest: derive client-side from the torrents list. Pass `queuedCount` into StatusBar from App.tsx.

In WindowShell pass-through, App.tsx computes:
```tsx
const queuedCount = createMemo(() => store.state.torrents.filter((t) => t.queued).length);
```

Pass to WindowShell → StatusBar. Add a span:
```tsx
<span class="font-mono tabular-nums">{props.queuedCount} queued</span>
```

Place it between "active" and "seeding".

- [ ] **Step 2: Verify + commit**

```bash
cd frontend && npm run build
git add frontend/src/components/shell/StatusBar.tsx frontend/src/components/shell/WindowShell.tsx frontend/src/App.tsx
git commit -m "feat(frontend): StatusBar shows queued count"
```

---

## Section E — Smoke test

### Task 13: User-driven smoke

- [ ] **Step 1: Run `~/go/bin/wails dev -skipembedcreate`**

- [ ] **Step 2: Visual + functional check**

- Settings → Connection: pane visible. Set Download to 1000 KB/s, Upload to 100. Save → toast confirms.
- Toggle alt-speed via toolbar Zap button → pill turns accent-tinted. Set alt rates → click Zap → engine throttles to alt rates.
- Set Max active downloads = 2. Add 5 magnets in quick succession. Three should show "Q" badge with their queue position; two should be active. Pause one of the active → the next queued takes its slot.
- Right-click a queued row → Queue submenu → "Move to top" → it becomes the next to start when a slot frees up.
- Right-click a queued row → Queue submenu → "Force-start" → its star indicator appears, and it runs even past the limit.
- Status bar shows "X queued" between active and seeding counts.

- [ ] **Step 3: Tag**

```bash
git tag plan-4c-bandwidth-and-queue-complete
git push origin main
git push origin plan-4c-bandwidth-and-queue-complete
```

---

**End of Plan 4c.**
