# Mosaic — Plan 4a: Organization & Per-Torrent Control

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Take Plan 3's powerful inspector and add the surface for *organizing* and *controlling* individual torrents — the polished add-modal you've been seeing in mockups, real categories and tags (replacing the "Coming in Plan 4" stubs), per-file priority dropdowns, `.torrent` file drag-drop, and a long-overdue cleanup of the misnamed `Snapshot` rate fields. After this plan, the app is feature-complete for power users running dozens of torrents organized into categories, with priority control over what downloads first.

**Architecture:** Backend grows two new persistence tables (`categories`, `tags`) plus `torrent_tags`, a small migration to add `category_id` and a few control columns to the existing `torrents` table, DAOs over each, and a CRUD surface on the `api` Service. The engine snapshot grows separate `BytesDown`/`BytesUp` (cumulative) and `RateDown`/`RateUp` (instantaneous) fields — replacing the misleading carry-over `DownloadRate`/`UploadRate` names from Plan 1. Frontend gets the polished add-modal (Kobalte Dialog with three sections — source / save target / file tree), wires the FilterRail's category/tag/tracker placeholder sections to live data, surfaces a per-file priority dropdown in `FilesTab`, and accepts `.torrent` files dropped onto the window.

**Tech additions:** none — everything builds on Plans 1–3 stack.

**Reference spec:** `docs/superpowers/specs/2026-04-28-bittorrent-client-design.md` §4.6 (add-modal), §4.4 (file priority dropdown), §6 (full schema).

**Aesthetic continuity:** Same tokens. Categories use the per-category `color` field as a small dot in the FilterRail and on torrent cards. Tags use a similar treatment. Add-modal keeps the existing glass surface from Plan 2's `AddMagnetModal` but grows three collapsible sections: Source / Save target / Files & options.

---

## Out of Scope (deferred to Plan 4b)

- **Queueing** — global active-torrent limit + queue priority — Plan 4b
- **Bandwidth scheduling** — time-of-day rate-limit profiles — Plan 4b
- **IP filtering / blocklists** — Plan 4b
- **Real alt-speed limits** (toolbar Zap button stays inert) — Plan 4b
- **RSS auto-add** — Plan 5
- **Settings page chrome** — multiple later plans

---

## File Structure (final state at end of plan)

```
backend/
├── persistence/
│   ├── migrations/
│   │   ├── 0001_initial.sql               # unchanged
│   │   └── 0002_categories_tags.sql       # NEW: categories, tags, torrent_tags + ALTER torrents
│   ├── categories.go                       # NEW: Category DAO
│   ├── categories_test.go                  # NEW
│   ├── tags.go                             # NEW: Tag DAO + torrent_tags assoc
│   ├── tags_test.go                        # NEW
│   └── torrents.go                         # MODIFIED: TorrentRecord adds CategoryID, ratio fields
├── engine/
│   ├── types.go                            # MODIFIED: Snapshot rename DownloadRate/UploadRate → BytesDown/BytesUp + add RateDown/RateUp; Backend gets SetFilePriorities
│   ├── anacrolix.go                        # MODIFIED: rate computation from previous-stats delta + SetFilePriorities
│   ├── engine.go                           # MODIFIED: SetFilePriorities passthrough
│   ├── engine_test.go                      # MODIFIED: rename in fixtures
│   └── fake.go                             # MODIFIED: rename in fixtures + SetFilePriorities
└── api/
    ├── service.go                          # MODIFIED: TorrentDTO field rename; new CategoryDTO/TagDTO; CRUD methods + SetFilePriorities + SetTorrentCategory/Tags
    └── service_test.go                     # MODIFIED + NEW tests for the above
app.go                                      # MODIFIED: new bindings

frontend/src/
├── lib/
│   ├── bindings.ts                         # MODIFIED: TorrentDTO rate field rename; CategoryDTO/TagDTO; new api methods
│   └── store.ts                            # MODIFIED: live categories/tags lists; selectedCategory/Tag filters
└── components/
    ├── shell/
    │   ├── AddTorrentModal.tsx             # NEW (replaces AddMagnetModal): three-section polished add flow
    │   ├── FilterRail.tsx                  # MODIFIED: real Categories/Tags/Trackers sections with live counts
    │   └── DropZone.tsx                    # MODIFIED: handles .torrent file drops
    ├── inspector/
    │   └── FilesTab.tsx                    # MODIFIED: per-file priority dropdown
    └── list/
        └── TorrentRowMenu.tsx              # MODIFIED: Set category / Set tags submenus

docs/superpowers/plans/2026-04-29-organization-and-per-torrent-control.md   # this file
```

---

## Section A — Snapshot Field Rename (foundational)

The `Snapshot.DownloadRate` and `Snapshot.UploadRate` fields are mis-named carry-overs from Plan 1. They actually hold **cumulative** byte counts, not bytes/sec. Plan 3's inspector left ratio at 0.0 specifically because of this. Plan 4a fixes it before adding new features that build on the snapshot.

### Task 1: Snapshot rename — failing tests

**Files:** `backend/engine/engine_test.go` (modify)

- [ ] **Step 1: Update existing test to expect new field names**

The `TestEngine_Tick_EmitsForActiveTorrents` test currently doesn't check rate fields, but Task 2's struct rename will break callers that DO reference `.DownloadRate`. Search first:

```bash
grep -rn "DownloadRate\|UploadRate" backend/ app.go
```

You'll find references in `backend/api/service.go` (`toDTO` function and `GlobalStats` accumulator), `backend/engine/anacrolix.go` (the `snapshotFor` helper), and `backend/engine/fake.go` (none — Fake doesn't set these). All will need updating in Task 2.

- [ ] **Step 2: Add a new failing test that exercises rate vs. cumulative**

Append to `backend/engine/engine_test.go`:

```go
func TestEngine_Snapshot_HasSeparateBytesAndRateFields(t *testing.T) {
	fb := NewFakeBackend()
	eng := NewEngine(fb, 50*time.Millisecond)
	t.Cleanup(func() { _ = eng.Close() })

	id, err := eng.AddMagnet(context.Background(), "magnet:?xt=urn:btih:fields", "/tmp")
	require.NoError(t, err)
	fb.AdvanceProgress(id, 1024)

	snap, err := eng.Snapshot(id)
	require.NoError(t, err)
	require.Equal(t, int64(1024), snap.BytesDone)
	// BytesDown/BytesUp default to 0 in the fake; RateDown/RateUp default to 0
	require.Equal(t, int64(0), snap.BytesDown)
	require.Equal(t, int64(0), snap.BytesUp)
	require.Equal(t, int64(0), snap.RateDown)
	require.Equal(t, int64(0), snap.RateUp)
}
```

- [ ] **Step 3: Run, confirm fails**

```bash
go test ./backend/engine/ -run TestEngine_Snapshot_HasSeparateBytesAndRateFields -v
```

Expected: FAIL with `snap.BytesDown undefined` (etc.).

---

### Task 2: Snapshot rename — implementation

**Files:** `backend/engine/types.go`, `backend/engine/anacrolix.go`, `backend/engine/fake.go`

- [ ] **Step 1: Update `Snapshot` struct**

In `backend/engine/types.go`, replace the existing `Snapshot` struct:

```go
type Snapshot struct {
	ID         TorrentID
	Name       string
	Magnet     string
	SavePath   string
	TotalBytes int64
	BytesDone  int64
	BytesDown  int64 // cumulative bytes downloaded this session
	BytesUp    int64 // cumulative bytes uploaded this session
	RateDown   int64 // instantaneous bytes/sec
	RateUp     int64
	Peers      int
	Seeds      int
	Paused     bool
	Completed  bool
	AddedAt    time.Time
}
```

- [ ] **Step 2: Update anacrolix.go's `snapshotFor`**

Replace the `snapshotFor` helper. Compute rates by tracking previous samples per-torrent:

```go
type rateSample struct {
	at   time.Time
	down int64
	up   int64
}

// AnacrolixBackend struct gains a rate-sample map.
// Add to AnacrolixBackend struct:
//   rateMu      sync.Mutex
//   prevRates   map[TorrentID]rateSample

func snapshotFor(t *torrent.Torrent, prev rateSample) (Snapshot, rateSample) {
	stats := t.Stats()
	name := t.Name()
	if name == "" {
		name = t.InfoHash().HexString()
	}
	total := int64(0)
	if info := t.Info(); info != nil {
		total = info.TotalLength()
	}
	bytesDown := stats.BytesReadData.Int64()
	bytesUp := stats.BytesWrittenData.Int64()
	now := time.Now()
	var rateDown, rateUp int64
	if !prev.at.IsZero() {
		dt := now.Sub(prev.at).Seconds()
		if dt > 0 {
			rateDown = int64(float64(bytesDown-prev.down) / dt)
			rateUp = int64(float64(bytesUp-prev.up) / dt)
		}
	}
	snap := Snapshot{
		ID:         TorrentID(t.InfoHash().HexString()),
		Name:       name,
		TotalBytes: total,
		BytesDone:  t.BytesCompleted(),
		BytesDown:  bytesDown,
		BytesUp:    bytesUp,
		RateDown:   rateDown,
		RateUp:     rateUp,
		Peers:      stats.ActivePeers,
		Seeds:      stats.ConnectedSeeders,
		Completed:  total > 0 && t.BytesCompleted() == total,
		AddedAt:    time.Now(),
	}
	return snap, rateSample{at: now, down: bytesDown, up: bytesUp}
}
```

Update both `AnacrolixBackend.Snapshot(id)` and `AnacrolixBackend.List()` to thread the rate-sample map through. The simplest pattern:

```go
func (a *AnacrolixBackend) Snapshot(id TorrentID) (Snapshot, error) {
	t, ok := a.find(id)
	if !ok { return Snapshot{}, errors.New("not found") }
	a.rateMu.Lock()
	prev := a.prevRates[id]
	snap, next := snapshotFor(t, prev)
	a.prevRates[id] = next
	a.rateMu.Unlock()
	return snap, nil
}

func (a *AnacrolixBackend) List() []Snapshot {
	ts := a.client.Torrents()
	out := make([]Snapshot, 0, len(ts))
	a.rateMu.Lock()
	defer a.rateMu.Unlock()
	for _, t := range ts {
		id := TorrentID(t.InfoHash().HexString())
		snap, next := snapshotFor(t, a.prevRates[id])
		a.prevRates[id] = next
		out = append(out, snap)
	}
	return out
}
```

Initialize `prevRates: make(map[TorrentID]rateSample)` in `NewAnacrolixBackend`. Add `"sync"` import if not already present.

- [ ] **Step 3: Verify tests pass**

```bash
go test ./backend/engine/ -v -race
```

Expected: PASS (6 engine tests now: 5 existing + 1 new).

- [ ] **Step 4: Update api/service.go** — fix `toDTO` and `GlobalStats` to use the new field names

In `backend/api/service.go`, find `toDTO`:

```go
return TorrentDTO{
    // ...
    DownloadRate: s.RateDown,  // was: s.DownloadRate
    UploadRate:   s.RateUp,    // was: s.UploadRate
    // ...
}
```

In `GlobalStats`:
```go
st.TotalDownloadRate += snap.RateDown  // was: snap.DownloadRate
st.TotalUploadRate   += snap.RateUp    // was: snap.UploadRate
```

In `detailToDTO` (the inspector data builder):
```go
TotalDown: snap.BytesDown,  // was: snap.DownloadRate
TotalUp:   snap.BytesUp,    // was: snap.UploadRate
Ratio:     ratioOf(snap.BytesDown, snap.BytesUp),
```

Add helper:
```go
func ratioOf(down, up int64) float64 {
	if down == 0 { return 0 }
	return float64(up) / float64(down)
}
```

- [ ] **Step 5: Verify everything builds + tests pass**

```bash
go build ./...
go test ./... -race -count=1
```

Expected: clean. 22+ tests passing.

- [ ] **Step 6: Commit**

```bash
git add backend/engine/ backend/api/
git commit -m "refactor(engine): split Snapshot rate vs cumulative bytes; compute real ratio"
```

---

## Section B — Categories & Tags

### Task 3: Migration 0002

**Files:** Create `backend/persistence/migrations/0002_categories_tags.sql`

- [ ] **Step 1: Write the migration**

```sql
-- +goose Up
CREATE TABLE categories (
  id                INTEGER PRIMARY KEY,
  name              TEXT UNIQUE NOT NULL,
  default_save_path TEXT,
  color             TEXT NOT NULL DEFAULT '#71717a'
);

CREATE TABLE tags (
  id    INTEGER PRIMARY KEY,
  name  TEXT UNIQUE NOT NULL,
  color TEXT NOT NULL DEFAULT '#71717a'
);

CREATE TABLE torrent_tags (
  infohash TEXT NOT NULL REFERENCES torrents(infohash) ON DELETE CASCADE,
  tag_id   INTEGER NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
  PRIMARY KEY (infohash, tag_id)
);

ALTER TABLE torrents ADD COLUMN category_id INTEGER REFERENCES categories(id);

-- +goose Down
ALTER TABLE torrents DROP COLUMN category_id;
DROP TABLE torrent_tags;
DROP TABLE tags;
DROP TABLE categories;
```

- [ ] **Step 2: Verify migration runs cleanly**

```bash
go test ./backend/persistence/ -run TestOpen_RunsMigrations -v
```

Update the test to also assert the new tables exist:

```go
require.Contains(t, names, "categories")
require.Contains(t, names, "tags")
require.Contains(t, names, "torrent_tags")
```

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add backend/persistence/migrations/0002_categories_tags.sql backend/persistence/db_test.go
git commit -m "feat(persistence): migration 0002 — categories, tags, torrent_tags"
```

---

### Task 4: Category DAO — failing tests

**Files:** `backend/persistence/categories_test.go`

- [ ] **Step 1: Write failing tests**

```go
package persistence

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCategories_CreateGet(t *testing.T) {
	db := newTestDB(t)
	c := NewCategories(db)
	ctx := context.Background()

	id, err := c.Create(ctx, Category{Name: "Movies", DefaultSavePath: "/Volumes/media", Color: "#ef4444"})
	require.NoError(t, err)
	require.Greater(t, id, 0)

	got, err := c.Get(ctx, id)
	require.NoError(t, err)
	require.Equal(t, "Movies", got.Name)
	require.Equal(t, "/Volumes/media", got.DefaultSavePath)
	require.Equal(t, "#ef4444", got.Color)
}

func TestCategories_List(t *testing.T) {
	db := newTestDB(t)
	c := NewCategories(db)
	ctx := context.Background()

	_, _ = c.Create(ctx, Category{Name: "Movies"})
	_, _ = c.Create(ctx, Category{Name: "Software"})

	rows, err := c.List(ctx)
	require.NoError(t, err)
	require.Len(t, rows, 2)
}

func TestCategories_Update(t *testing.T) {
	db := newTestDB(t)
	c := NewCategories(db)
	ctx := context.Background()

	id, _ := c.Create(ctx, Category{Name: "Movies"})
	require.NoError(t, c.Update(ctx, Category{ID: id, Name: "Cinema", Color: "#000000"}))

	got, _ := c.Get(ctx, id)
	require.Equal(t, "Cinema", got.Name)
	require.Equal(t, "#000000", got.Color)
}

func TestCategories_Delete(t *testing.T) {
	db := newTestDB(t)
	c := NewCategories(db)
	ctx := context.Background()

	id, _ := c.Create(ctx, Category{Name: "Tmp"})
	require.NoError(t, c.Delete(ctx, id))
	_, err := c.Get(ctx, id)
	require.ErrorIs(t, err, ErrNotFound)
}

func TestCategories_NameUnique(t *testing.T) {
	db := newTestDB(t)
	c := NewCategories(db)
	ctx := context.Background()
	_, err := c.Create(ctx, Category{Name: "Movies"})
	require.NoError(t, err)
	_, err = c.Create(ctx, Category{Name: "Movies"})
	require.Error(t, err)
}
```

- [ ] **Step 2: Run, confirm fails**

```bash
go test ./backend/persistence/ -run TestCategories -v
```

Expected: FAIL with "undefined: NewCategories" / "undefined: Category".

---

### Task 5: Category DAO — implementation

**Files:** `backend/persistence/categories.go`

```go
package persistence

import (
	"context"
	"database/sql"
	"errors"
)

type Category struct {
	ID              int
	Name            string
	DefaultSavePath string
	Color           string
}

type Categories struct{ db *DB }

func NewCategories(db *DB) *Categories { return &Categories{db: db} }

func (c *Categories) Create(ctx context.Context, cat Category) (int, error) {
	color := cat.Color
	if color == "" {
		color = "#71717a"
	}
	res, err := c.db.SQL().ExecContext(ctx,
		`INSERT INTO categories (name, default_save_path, color) VALUES (?, ?, ?)`,
		cat.Name, cat.DefaultSavePath, color)
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	return int(id), err
}

func (c *Categories) Get(ctx context.Context, id int) (Category, error) {
	var cat Category
	err := c.db.SQL().QueryRowContext(ctx,
		`SELECT id, name, COALESCE(default_save_path, ''), color FROM categories WHERE id = ?`, id,
	).Scan(&cat.ID, &cat.Name, &cat.DefaultSavePath, &cat.Color)
	if errors.Is(err, sql.ErrNoRows) {
		return cat, ErrNotFound
	}
	return cat, err
}

func (c *Categories) List(ctx context.Context) ([]Category, error) {
	rows, err := c.db.SQL().QueryContext(ctx,
		`SELECT id, name, COALESCE(default_save_path, ''), color FROM categories ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Category
	for rows.Next() {
		var cat Category
		if err := rows.Scan(&cat.ID, &cat.Name, &cat.DefaultSavePath, &cat.Color); err != nil {
			return nil, err
		}
		out = append(out, cat)
	}
	return out, rows.Err()
}

func (c *Categories) Update(ctx context.Context, cat Category) error {
	_, err := c.db.SQL().ExecContext(ctx,
		`UPDATE categories SET name = ?, default_save_path = ?, color = ? WHERE id = ?`,
		cat.Name, cat.DefaultSavePath, cat.Color, cat.ID)
	return err
}

func (c *Categories) Delete(ctx context.Context, id int) error {
	_, err := c.db.SQL().ExecContext(ctx, `DELETE FROM categories WHERE id = ?`, id)
	return err
}
```

- [ ] **Step 1: Write the file (above)**
- [ ] **Step 2: Run tests, confirm pass**

```bash
go test ./backend/persistence/ -v -race
```

Expected: PASS (5 new + existing tests).

- [ ] **Step 3: Commit**

```bash
git add backend/persistence/categories.go backend/persistence/categories_test.go
git commit -m "feat(persistence): Categories DAO (Create/Get/List/Update/Delete)"
```

---

### Task 6: Tag DAO + torrent_tags assoc — failing tests

**Files:** `backend/persistence/tags_test.go`

- [ ] **Step 1: Write failing tests**

```go
package persistence

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestTags_CreateListUpdateDelete(t *testing.T) {
	db := newTestDB(t)
	tg := NewTags(db)
	ctx := context.Background()

	id, err := tg.Create(ctx, Tag{Name: "#archive", Color: "#3b82f6"})
	require.NoError(t, err)
	require.Greater(t, id, 0)

	got, err := tg.Get(ctx, id)
	require.NoError(t, err)
	require.Equal(t, "#archive", got.Name)

	require.NoError(t, tg.Update(ctx, Tag{ID: id, Name: "#archived", Color: "#000000"}))
	got, _ = tg.Get(ctx, id)
	require.Equal(t, "#archived", got.Name)

	require.NoError(t, tg.Delete(ctx, id))
	_, err = tg.Get(ctx, id)
	require.ErrorIs(t, err, ErrNotFound)
}

func TestTags_AssocWithTorrent(t *testing.T) {
	db := newTestDB(t)
	tor := NewTorrents(db)
	tg := NewTags(db)
	ctx := context.Background()

	require.NoError(t, tor.Save(ctx, TorrentRecord{
		InfoHash: "abc123", Name: "x", SavePath: "/tmp", AddedAt: time.Now(),
	}))
	tagID, _ := tg.Create(ctx, Tag{Name: "#priority"})

	require.NoError(t, tg.Assign(ctx, "abc123", tagID))

	tags, err := tg.ForTorrent(ctx, "abc123")
	require.NoError(t, err)
	require.Len(t, tags, 1)
	require.Equal(t, "#priority", tags[0].Name)

	require.NoError(t, tg.Unassign(ctx, "abc123", tagID))
	tags, _ = tg.ForTorrent(ctx, "abc123")
	require.Empty(t, tags)
}
```

- [ ] **Step 2: Run, confirm fails**

```bash
go test ./backend/persistence/ -run TestTags -v
```

Expected: FAIL with "undefined: NewTags" / "undefined: Tag".

---

### Task 7: Tag DAO — implementation

**Files:** `backend/persistence/tags.go`

```go
package persistence

import (
	"context"
	"database/sql"
	"errors"
)

type Tag struct {
	ID    int
	Name  string
	Color string
}

type Tags struct{ db *DB }

func NewTags(db *DB) *Tags { return &Tags{db: db} }

func (t *Tags) Create(ctx context.Context, tag Tag) (int, error) {
	color := tag.Color
	if color == "" {
		color = "#71717a"
	}
	res, err := t.db.SQL().ExecContext(ctx,
		`INSERT INTO tags (name, color) VALUES (?, ?)`, tag.Name, color)
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	return int(id), err
}

func (t *Tags) Get(ctx context.Context, id int) (Tag, error) {
	var tag Tag
	err := t.db.SQL().QueryRowContext(ctx,
		`SELECT id, name, color FROM tags WHERE id = ?`, id,
	).Scan(&tag.ID, &tag.Name, &tag.Color)
	if errors.Is(err, sql.ErrNoRows) {
		return tag, ErrNotFound
	}
	return tag, err
}

func (t *Tags) List(ctx context.Context) ([]Tag, error) {
	rows, err := t.db.SQL().QueryContext(ctx, `SELECT id, name, color FROM tags ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Tag
	for rows.Next() {
		var tag Tag
		if err := rows.Scan(&tag.ID, &tag.Name, &tag.Color); err != nil {
			return nil, err
		}
		out = append(out, tag)
	}
	return out, rows.Err()
}

func (t *Tags) Update(ctx context.Context, tag Tag) error {
	_, err := t.db.SQL().ExecContext(ctx,
		`UPDATE tags SET name = ?, color = ? WHERE id = ?`, tag.Name, tag.Color, tag.ID)
	return err
}

func (t *Tags) Delete(ctx context.Context, id int) error {
	_, err := t.db.SQL().ExecContext(ctx, `DELETE FROM tags WHERE id = ?`, id)
	return err
}

func (t *Tags) Assign(ctx context.Context, infohash string, tagID int) error {
	_, err := t.db.SQL().ExecContext(ctx,
		`INSERT INTO torrent_tags (infohash, tag_id) VALUES (?, ?) ON CONFLICT DO NOTHING`,
		infohash, tagID)
	return err
}

func (t *Tags) Unassign(ctx context.Context, infohash string, tagID int) error {
	_, err := t.db.SQL().ExecContext(ctx,
		`DELETE FROM torrent_tags WHERE infohash = ? AND tag_id = ?`, infohash, tagID)
	return err
}

func (t *Tags) ForTorrent(ctx context.Context, infohash string) ([]Tag, error) {
	rows, err := t.db.SQL().QueryContext(ctx, `
		SELECT t.id, t.name, t.color FROM tags t
		JOIN torrent_tags tt ON tt.tag_id = t.id
		WHERE tt.infohash = ? ORDER BY t.name`, infohash)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Tag
	for rows.Next() {
		var tag Tag
		if err := rows.Scan(&tag.ID, &tag.Name, &tag.Color); err != nil {
			return nil, err
		}
		out = append(out, tag)
	}
	return out, rows.Err()
}
```

- [ ] **Step 1: Write the file (above)**
- [ ] **Step 2: Run tests, confirm pass**
- [ ] **Step 3: Commit**

```bash
git add backend/persistence/tags.go backend/persistence/tags_test.go
git commit -m "feat(persistence): Tags DAO with torrent_tags assoc"
```

---

### Task 8: Extend `TorrentRecord` and Torrents DAO with category_id

**Files:** `backend/persistence/torrents.go` (modify), `backend/persistence/torrents_test.go` (modify)

- [ ] **Step 1: Update `TorrentRecord` struct**

```go
type TorrentRecord struct {
	InfoHash    string
	Name        string
	Magnet      string
	SavePath    string
	CategoryID  *int       // NEW: nullable foreign key
	AddedAt     time.Time
	CompletedAt *time.Time
	Paused      bool
}
```

- [ ] **Step 2: Update `Save` and scan helper to include category_id**

```go
func (t *Torrents) Save(ctx context.Context, r TorrentRecord) error {
	var completed sql.NullInt64
	if r.CompletedAt != nil {
		completed = sql.NullInt64{Int64: r.CompletedAt.Unix(), Valid: true}
	}
	var catID sql.NullInt64
	if r.CategoryID != nil {
		catID = sql.NullInt64{Int64: int64(*r.CategoryID), Valid: true}
	}
	paused := 0
	if r.Paused {
		paused = 1
	}
	_, err := t.db.SQL().ExecContext(ctx, `
INSERT INTO torrents (infohash, name, magnet, save_path, category_id, added_at, completed_at, paused)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(infohash) DO UPDATE SET
  name = excluded.name,
  magnet = excluded.magnet,
  save_path = excluded.save_path,
  category_id = excluded.category_id,
  added_at = excluded.added_at,
  completed_at = excluded.completed_at,
  paused = excluded.paused
`, r.InfoHash, r.Name, r.Magnet, r.SavePath, catID, r.AddedAt.Unix(), completed, paused)
	return err
}
```

Update `scanTorrent` to scan category_id into a `sql.NullInt64`, then convert to `*int`. Update both SELECT statements (`Get` and `List`) to include `category_id`.

- [ ] **Step 3: Add `SetCategory(infohash, categoryID *int)` method**

```go
func (t *Torrents) SetCategory(ctx context.Context, infohash string, categoryID *int) error {
	var v sql.NullInt64
	if categoryID != nil {
		v = sql.NullInt64{Int64: int64(*categoryID), Valid: true}
	}
	_, err := t.db.SQL().ExecContext(ctx,
		`UPDATE torrents SET category_id = ? WHERE infohash = ?`, v, infohash)
	return err
}
```

- [ ] **Step 4: Add a test that round-trips category_id**

In `backend/persistence/torrents_test.go`:

```go
func TestTorrents_CategoryAssignment(t *testing.T) {
	db := newTestDB(t)
	tor := NewTorrents(db)
	cats := NewCategories(db)
	ctx := context.Background()

	catID, _ := cats.Create(ctx, Category{Name: "Movies"})

	require.NoError(t, tor.Save(ctx, TorrentRecord{
		InfoHash: "h1", Name: "n", SavePath: "/p", AddedAt: time.Now(),
	}))
	require.NoError(t, tor.SetCategory(ctx, "h1", &catID))

	got, _ := tor.Get(ctx, "h1")
	require.NotNil(t, got.CategoryID)
	require.Equal(t, catID, *got.CategoryID)

	require.NoError(t, tor.SetCategory(ctx, "h1", nil))
	got, _ = tor.Get(ctx, "h1")
	require.Nil(t, got.CategoryID)
}
```

- [ ] **Step 5: Run, confirm pass**
- [ ] **Step 6: Commit**

```bash
git add backend/persistence/torrents.go backend/persistence/torrents_test.go
git commit -m "feat(persistence): Torrents DAO grows category_id (nullable FK + SetCategory)"
```

---

## Section C — API Service: Category/Tag CRUD + Assignment

### Task 9: Service layer — Category/Tag DTOs and CRUD methods (TDD)

**Files:** `backend/api/service.go`, `backend/api/service_test.go`

- [ ] **Step 1: Add failing tests**

Append to `backend/api/service_test.go`:

```go
func TestService_CategoryCRUD(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	id, err := svc.CreateCategory(ctx, "Movies", "/Volumes/media", "#ef4444")
	require.NoError(t, err)
	require.Greater(t, id, 0)

	cats, err := svc.ListCategories(ctx)
	require.NoError(t, err)
	require.Len(t, cats, 1)
	require.Equal(t, "Movies", cats[0].Name)

	require.NoError(t, svc.UpdateCategory(ctx, id, "Cinema", "/v/m", "#000"))
	cats, _ = svc.ListCategories(ctx)
	require.Equal(t, "Cinema", cats[0].Name)

	require.NoError(t, svc.DeleteCategory(ctx, id))
	cats, _ = svc.ListCategories(ctx)
	require.Empty(t, cats)
}

func TestService_TagAssignment(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	id, _ := svc.AddMagnet(ctx, "magnet:?xt=urn:btih:tag", "")
	tagID, err := svc.CreateTag(ctx, "#priority", "#3b82f6")
	require.NoError(t, err)

	require.NoError(t, svc.AssignTag(ctx, string(id), tagID))

	tags, err := svc.ListTagsFor(ctx, string(id))
	require.NoError(t, err)
	require.Len(t, tags, 1)
}

func TestService_SetTorrentCategory(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	id, _ := svc.AddMagnet(ctx, "magnet:?xt=urn:btih:cat", "")
	catID, _ := svc.CreateCategory(ctx, "Linux ISOs", "", "#22c55e")

	require.NoError(t, svc.SetTorrentCategory(ctx, string(id), &catID))

	rows, _ := svc.ListTorrents(ctx)
	require.Len(t, rows, 1)
	require.NotNil(t, rows[0].CategoryID)
	require.Equal(t, catID, *rows[0].CategoryID)
}
```

- [ ] **Step 2: Run, confirm fails**

```bash
go test ./backend/api/ -run TestService_(CategoryCRUD|TagAssignment|SetTorrentCategory) -v
```

Expected: FAIL on undefined methods.

- [ ] **Step 3: Implement on `Service`**

Add to `backend/api/service.go`:

```go
type CategoryDTO struct {
	ID              int    `json:"id"`
	Name            string `json:"name"`
	DefaultSavePath string `json:"default_save_path"`
	Color           string `json:"color"`
}

type TagDTO struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color"`
}

// Service struct gains:
//   categories *persistence.Categories
//   tags       *persistence.Tags
// And NewService takes those as params.
```

Update `NewService` signature:
```go
func NewService(
	eng *engine.Engine,
	torrents *persistence.Torrents,
	categories *persistence.Categories,
	tags *persistence.Tags,
	defaultSavePath string,
) *Service {
	return &Service{
		engine:          eng,
		torrents:        torrents,
		categories:      categories,
		tags:            tags,
		defaultSavePath: defaultSavePath,
	}
}
```

Add methods:
```go
func (s *Service) CreateCategory(ctx context.Context, name, defaultPath, color string) (int, error) {
	return s.categories.Create(ctx, persistence.Category{Name: name, DefaultSavePath: defaultPath, Color: color})
}

func (s *Service) ListCategories(ctx context.Context) ([]CategoryDTO, error) {
	cats, err := s.categories.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]CategoryDTO, 0, len(cats))
	for _, c := range cats {
		out = append(out, CategoryDTO{ID: c.ID, Name: c.Name, DefaultSavePath: c.DefaultSavePath, Color: c.Color})
	}
	return out, nil
}

func (s *Service) UpdateCategory(ctx context.Context, id int, name, defaultPath, color string) error {
	return s.categories.Update(ctx, persistence.Category{ID: id, Name: name, DefaultSavePath: defaultPath, Color: color})
}

func (s *Service) DeleteCategory(ctx context.Context, id int) error {
	return s.categories.Delete(ctx, id)
}

func (s *Service) CreateTag(ctx context.Context, name, color string) (int, error) {
	return s.tags.Create(ctx, persistence.Tag{Name: name, Color: color})
}

func (s *Service) ListTags(ctx context.Context) ([]TagDTO, error) {
	tags, err := s.tags.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]TagDTO, 0, len(tags))
	for _, t := range tags {
		out = append(out, TagDTO{ID: t.ID, Name: t.Name, Color: t.Color})
	}
	return out, nil
}

func (s *Service) DeleteTag(ctx context.Context, id int) error {
	return s.tags.Delete(ctx, id)
}

func (s *Service) AssignTag(ctx context.Context, infohash string, tagID int) error {
	return s.tags.Assign(ctx, infohash, tagID)
}

func (s *Service) UnassignTag(ctx context.Context, infohash string, tagID int) error {
	return s.tags.Unassign(ctx, infohash, tagID)
}

func (s *Service) ListTagsFor(ctx context.Context, infohash string) ([]TagDTO, error) {
	tags, err := s.tags.ForTorrent(ctx, infohash)
	if err != nil {
		return nil, err
	}
	out := make([]TagDTO, 0, len(tags))
	for _, t := range tags {
		out = append(out, TagDTO{ID: t.ID, Name: t.Name, Color: t.Color})
	}
	return out, nil
}

func (s *Service) SetTorrentCategory(ctx context.Context, infohash string, categoryID *int) error {
	return s.torrents.SetCategory(ctx, infohash, categoryID)
}
```

Also extend `TorrentDTO` to include category and tags:
```go
type TorrentDTO struct {
    // ... existing fields ...
    CategoryID *int     `json:"category_id"`
    Tags       []TagDTO `json:"tags"`
}
```

Update `ListTorrents` to populate these from the persistence layer.

- [ ] **Step 4: Update `newTestService` helper**

In `backend/api/service_test.go`, add Categories and Tags to the test setup:

```go
func newTestService(t *testing.T) (*Service, *engine.FakeBackend) {
	t.Helper()
	db, err := persistence.Open(context.Background(), filepath.Join(t.TempDir(), "t.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	fb := engine.NewFakeBackend()
	eng := engine.NewEngine(fb, 50*time.Millisecond)
	t.Cleanup(func() { _ = eng.Close() })

	svc := NewService(eng,
		persistence.NewTorrents(db),
		persistence.NewCategories(db),
		persistence.NewTags(db),
		"/tmp/dl")
	return svc, fb
}
```

- [ ] **Step 5: Update main.go to pass the new DAOs**

In `main.go`:
```go
svc := api.NewService(eng,
	persistence.NewTorrents(db),
	persistence.NewCategories(db),
	persistence.NewTags(db),
	cfg.DefaultSavePath)
```

- [ ] **Step 6: Run all tests, confirm pass**

```bash
go test ./... -race -count=1
```

Expected: 9 api + 7 persistence + 6 engine + others = 25+ tests, all green.

- [ ] **Step 7: Commit**

```bash
git add backend/api/ main.go
git commit -m "feat(api): Category/Tag CRUD + tag assignment + torrent category setter"
```

---

### Task 10: Bind on App.go

**Files:** `app.go` (modify)

- [ ] **Step 1: Add Wails-bound methods**

Append to `app.go`:

```go
func (a *App) ListCategories() ([]api.CategoryDTO, error) {
	return a.svc.ListCategories(a.ctx)
}

func (a *App) CreateCategory(name, defaultPath, color string) (int, error) {
	return a.svc.CreateCategory(a.ctx, name, defaultPath, color)
}

func (a *App) UpdateCategory(id int, name, defaultPath, color string) error {
	return a.svc.UpdateCategory(a.ctx, id, name, defaultPath, color)
}

func (a *App) DeleteCategory(id int) error {
	return a.svc.DeleteCategory(a.ctx, id)
}

func (a *App) ListTags() ([]api.TagDTO, error) {
	return a.svc.ListTags(a.ctx)
}

func (a *App) CreateTag(name, color string) (int, error) {
	return a.svc.CreateTag(a.ctx, name, color)
}

func (a *App) DeleteTag(id int) error {
	return a.svc.DeleteTag(a.ctx, id)
}

func (a *App) AssignTag(infohash string, tagID int) error {
	return a.svc.AssignTag(a.ctx, infohash, tagID)
}

func (a *App) UnassignTag(infohash string, tagID int) error {
	return a.svc.UnassignTag(a.ctx, infohash, tagID)
}

func (a *App) SetTorrentCategory(infohash string, categoryID *int) error {
	return a.svc.SetTorrentCategory(a.ctx, infohash, categoryID)
}
```

- [ ] **Step 2: Regenerate bindings**

```bash
$(go env GOPATH)/bin/wails generate module
```

Verify `frontend/wailsjs/go/main/App.d.ts` exports all 10 new methods.

- [ ] **Step 3: Verify build**

```bash
go build ./...
```

Expected: clean.

- [ ] **Step 4: Commit**

```bash
git add app.go
git commit -m "feat: Wails bindings for category/tag CRUD + assignment"
```

---

## Section D — Per-File Priority

### Task 11: Engine — `SetFilePriorities` (TDD)

**Files:** `backend/engine/types.go` (Backend interface), `backend/engine/fake.go`, `backend/engine/anacrolix.go`, `backend/engine/engine.go`, `backend/engine/engine_test.go`

- [ ] **Step 1: Add to Backend interface + failing test**

In `backend/engine/types.go` `Backend`:

```go
SetFilePriorities(id TorrentID, prios map[int]Priority) error
```

In `backend/engine/engine_test.go`, append:

```go
func TestEngine_SetFilePriorities(t *testing.T) {
	fb := NewFakeBackend()
	eng := NewEngine(fb, 50*time.Millisecond)
	t.Cleanup(func() { _ = eng.Close() })

	id, _ := eng.AddMagnet(context.Background(), "magnet:?xt=urn:btih:prio", "/tmp")

	require.NoError(t, eng.SetFilePriorities(id, map[int]Priority{
		0: PriorityHigh,
		1: PrioritySkip,
	}))

	d, _ := eng.DetailedSnapshot(id, DetailScope{Files: true})
	require.Len(t, d.Files, 2)
	for _, f := range d.Files {
		switch f.Index {
		case 0:
			require.Equal(t, PriorityHigh, f.Priority)
		case 1:
			require.Equal(t, PrioritySkip, f.Priority)
		}
	}
}
```

Run, confirm fails.

- [ ] **Step 2: Implement on FakeBackend**

```go
func (f *FakeBackend) SetFilePriorities(id TorrentID, prios map[int]Priority) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.torrents[id]; !ok {
		return errors.New("not found")
	}
	if f.filePrios == nil {
		f.filePrios = make(map[TorrentID]map[int]Priority)
	}
	if f.filePrios[id] == nil {
		f.filePrios[id] = make(map[int]Priority)
	}
	for idx, p := range prios {
		f.filePrios[id][idx] = p
	}
	return nil
}
```

Add `filePrios map[TorrentID]map[int]Priority` to `FakeBackend` struct. Update `DetailedSnapshot`'s Files branch to use `f.filePrios[id][i]` if present (default `PriorityNormal`).

- [ ] **Step 3: Implement on AnacrolixBackend**

In `backend/engine/anacrolix.go`:

```go
func (a *AnacrolixBackend) SetFilePriorities(id TorrentID, prios map[int]Priority) error {
	t, ok := a.find(id)
	if !ok {
		return errors.New("not found")
	}
	files := t.Files()
	for idx, p := range prios {
		if idx < 0 || idx >= len(files) {
			continue
		}
		files[idx].SetPriority(prioToAnacrolix(p))
	}
	return nil
}

func prioToAnacrolix(p Priority) anacrolix_types.PiecePriority {
	switch p {
	case PrioritySkip:
		return anacrolix_types.PiecePriorityNone
	case PriorityHigh:
		return anacrolix_types.PiecePriorityHigh
	case PriorityMax:
		return anacrolix_types.PiecePriorityNow
	}
	return anacrolix_types.PiecePriorityNormal
}
```

- [ ] **Step 4: Engine wrapper passthrough**

In `backend/engine/engine.go`:

```go
func (e *Engine) SetFilePriorities(id TorrentID, prios map[int]Priority) error {
	return e.backend.SetFilePriorities(id, prios)
}
```

- [ ] **Step 5: Run tests, confirm pass**
- [ ] **Step 6: Commit**

```bash
git add backend/engine/
git commit -m "feat(engine): SetFilePriorities(id, map[int]Priority)"
```

---

### Task 12: API service + Wails binding for SetFilePriorities

**Files:** `backend/api/service.go`, `app.go`

- [ ] **Step 1: Add service method + test**

Test in `service_test.go`:
```go
func TestService_SetFilePriorities(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	id, _ := svc.AddMagnet(ctx, "magnet:?xt=urn:btih:fp", "")

	require.NoError(t, svc.SetFilePriorities(ctx, string(id), map[int]string{
		0: "high",
		1: "skip",
	}))
}
```

Method on Service:
```go
func (s *Service) SetFilePriorities(ctx context.Context, infohash string, prios map[int]string) error {
	mapped := make(map[int]engine.Priority, len(prios))
	for idx, p := range prios {
		switch p {
		case "skip":
			mapped[idx] = engine.PrioritySkip
		case "high":
			mapped[idx] = engine.PriorityHigh
		case "max":
			mapped[idx] = engine.PriorityMax
		default:
			mapped[idx] = engine.PriorityNormal
		}
	}
	return s.engine.SetFilePriorities(engine.TorrentID(infohash), mapped)
}
```

- [ ] **Step 2: Bind on App**

```go
func (a *App) SetFilePriorities(infohash string, prios map[int]string) error {
	return a.svc.SetFilePriorities(a.ctx, infohash, prios)
}
```

- [ ] **Step 3: Regenerate bindings**

```bash
$(go env GOPATH)/bin/wails generate module
```

- [ ] **Step 4: Verify + commit**

```bash
go test ./... -race -count=1
go build ./...
git add backend/api/ app.go
git commit -m "feat(api): SetFilePriorities Wails binding"
```

---

## Section E — Frontend wiring

### Task 13: Frontend bindings + store extensions

**Files:** `frontend/src/lib/bindings.ts`, `frontend/src/lib/store.ts`

- [ ] **Step 1: Update `Torrent` type and add new types**

In `frontend/src/lib/bindings.ts`:

```ts
export type Torrent = {
  // ... existing fields, but rename:
  // download_rate is now "instantaneous bytes/sec rate" — same name
  // upload_rate same
  // ADD:
  category_id: number | null;
  tags: TagDTO[];
};

export type CategoryDTO = {
  id: number;
  name: string;
  default_save_path: string;
  color: string;
};

export type TagDTO = {
  id: number;
  name: string;
  color: string;
};
```

(Note: `download_rate`/`upload_rate` JSON tags on the Go side now map to `RateDown`/`RateUp` instead of the old cumulative-named fields, but the TS-side names stay the same — the Go DTO does the renaming.)

Add api methods:

```ts
import {
  // existing imports
  ListCategories, CreateCategory, UpdateCategory, DeleteCategory,
  ListTags, CreateTag, DeleteTag, AssignTag, UnassignTag,
  SetTorrentCategory, SetFilePriorities,
} from '../../wailsjs/go/main/App';

export const api = {
  // ... existing ...
  listCategories: () => ListCategories() as Promise<CategoryDTO[]>,
  createCategory: (name: string, savePath: string, color: string) => CreateCategory(name, savePath, color),
  updateCategory: (id: number, name: string, savePath: string, color: string) => UpdateCategory(id, name, savePath, color),
  deleteCategory: (id: number) => DeleteCategory(id),
  listTags: () => ListTags() as Promise<TagDTO[]>,
  createTag: (name: string, color: string) => CreateTag(name, color),
  deleteTag: (id: number) => DeleteTag(id),
  assignTag: (infohash: string, tagID: number) => AssignTag(infohash, tagID),
  unassignTag: (infohash: string, tagID: number) => UnassignTag(infohash, tagID),
  setTorrentCategory: (infohash: string, categoryID: number | null) => SetTorrentCategory(infohash, categoryID),
  setFilePriorities: (infohash: string, prios: Record<number, 'skip' | 'normal' | 'high' | 'max'>) => SetFilePriorities(infohash, prios),
};
```

- [ ] **Step 2: Extend store**

In `frontend/src/lib/store.ts`, extend `AppState`:

```ts
export type AppState = {
  // ... existing ...
  categories: CategoryDTO[];
  tags: TagDTO[];
  selectedCategoryID: number | null;  // FilterRail filter
  selectedTagID: number | null;       // FilterRail filter
};
```

In `createTorrentsStore`, after the initial `api.listTorrents()`:
```ts
api.listCategories().then((cs) => setState(produce((s) => { s.categories = cs; })));
api.listTags().then((ts) => setState(produce((s) => { s.tags = ts; })));
```

Add methods:
```ts
return {
  // ... existing ...
  refreshCategories: async () => {
    const cs = await api.listCategories();
    setState(produce((s) => { s.categories = cs; }));
  },
  refreshTags: async () => {
    const ts = await api.listTags();
    setState(produce((s) => { s.tags = ts; }));
  },
  createCategory: async (name: string, savePath: string, color: string) => {
    await api.createCategory(name, savePath, color);
    await store.refreshCategories();
  },
  // similarly for create/delete tag, etc.
  setSelectedCategory: (id: number | null) => setState(produce((s) => { s.selectedCategoryID = id; })),
  setSelectedTag: (id: number | null) => setState(produce((s) => { s.selectedTagID = id; })),
};
```

Extend `filterTorrents` to honor selectedCategory/Tag:

```ts
export function filterTorrents(
  rows: Torrent[],
  status: StatusFilter,
  query: string,
  categoryID: number | null,
  tagID: number | null,
): Torrent[] {
  let out = rows;
  if (status !== 'all') { /* existing */ }
  if (categoryID !== null) {
    out = out.filter((t) => t.category_id === categoryID);
  }
  if (tagID !== null) {
    out = out.filter((t) => t.tags.some((tg) => tg.id === tagID));
  }
  if (query.trim()) { /* existing */ }
  return out;
}
```

- [ ] **Step 3: Update App.tsx's `filterTorrents` call site to pass the new params**

In `frontend/src/App.tsx`:
```tsx
const filtered = createMemo(() =>
  filterTorrents(
    store.state.torrents,
    store.state.statusFilter,
    store.state.searchQuery,
    store.state.selectedCategoryID,
    store.state.selectedTagID,
  ),
);
```

- [ ] **Step 4: Verify build + tests**

```bash
cd frontend && npm run build && npm test
```

Expected: build clean, 27 tests still pass.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/lib/ frontend/src/App.tsx
git commit -m "feat(frontend): bindings + store for categories/tags + per-file priorities"
```

---

### Task 14: FilterRail — wire Categories, Tags

**Files:** `frontend/src/components/shell/FilterRail.tsx`

- [ ] **Step 1: Replace the Plan-2 Categories/Tags placeholder sections with live data**

The plan-2 FilterRail showed "Coming in Plan 4" stubs for Categories/Tags. Replace those with real lists driven by `props.categories` and `props.tags`. Each item shows the colored dot + name + count of torrents in that category/tag.

```tsx
type Props = {
  torrents: Torrent[];
  active: StatusFilter;
  categories: CategoryDTO[];
  tags: TagDTO[];
  selectedCategoryID: number | null;
  selectedTagID: number | null;
  onSelect: (s: StatusFilter) => void;
  onSelectCategory: (id: number | null) => void;
  onSelectTag: (id: number | null) => void;
};

// Inside the FilterRail component, replace the Categories Section body:
<Section icon={Folder} title="Categories">
  <Show when={props.categories.length > 0} fallback={<p class="px-2 text-xs text-zinc-600">No categories yet</p>}>
    <ul class="flex flex-col gap-px">
      <For each={props.categories}>
        {(cat) => {
          const count = () => props.torrents.filter((t) => t.category_id === cat.id).length;
          return (
            <li>
              <button
                onClick={() => props.onSelectCategory(props.selectedCategoryID === cat.id ? null : cat.id)}
                class="flex w-full items-center justify-between gap-2 rounded-md px-2 py-1.5 text-sm transition-colors duration-100 hover:bg-white/[.04]"
                classList={{'bg-accent-500/[.10] text-accent-200': props.selectedCategoryID === cat.id, 'text-zinc-300': props.selectedCategoryID !== cat.id}}
              >
                <span class="inline-flex items-center gap-2 min-w-0">
                  <span class="h-2 w-2 shrink-0 rounded-full" style={{background: cat.color}} />
                  <span class="truncate">{cat.name}</span>
                </span>
                <Show when={count() > 0}>
                  <span class="font-mono text-xs tabular-nums text-zinc-500">{count()}</span>
                </Show>
              </button>
            </li>
          );
        }}
      </For>
    </ul>
  </Show>
</Section>
```

Mirror the pattern for Tags. Trackers stays as a "Coming in Plan 4b" stub (real tracker data wiring is small and needed for queueing/scheduling features).

- [ ] **Step 2: Update WindowShell + App.tsx to thread the new props**

```tsx
// WindowShell.tsx Props gain: categories, tags, selectedCategoryID, selectedTagID, onSelectCategory, onSelectTag
// App.tsx passes them from store.state
```

- [ ] **Step 3: Verify build**
- [ ] **Step 4: Commit**

```bash
git add frontend/src/components/shell/FilterRail.tsx frontend/src/components/shell/WindowShell.tsx frontend/src/App.tsx
git commit -m "feat(frontend): FilterRail wires live Categories + Tags with color dots and counts"
```

---

### Task 15: TorrentRowMenu — Set Category / Set Tags submenus

**Files:** `frontend/src/components/list/TorrentRowMenu.tsx`

- [ ] **Step 1: Add submenu support**

Kobalte ContextMenu supports `Sub` / `SubTrigger` / `SubContent`. Add Category and Tags submenus to the existing menu:

```tsx
<ContextMenu.Sub>
  <ContextMenu.SubTrigger>
    <Folder class="h-3.5 w-3.5" />
    Category
    <ChevronRight class="ml-auto h-3 w-3" />
  </ContextMenu.SubTrigger>
  <ContextMenu.SubContent>
    <ContextMenu.Item onSelect={() => props.onSetCategory(null)}>
      <span class="text-zinc-500">None</span>
    </ContextMenu.Item>
    <ContextMenu.Separator />
    <For each={props.categories}>
      {(cat) => (
        <ContextMenu.Item onSelect={() => props.onSetCategory(cat.id)}>
          <span class="h-2 w-2 rounded-full" style={{background: cat.color}} />
          {cat.name}
        </ContextMenu.Item>
      )}
    </For>
  </ContextMenu.SubContent>
</ContextMenu.Sub>
```

> The existing `ContextMenu` wrapper in `frontend/src/components/ui/ContextMenu.tsx` wraps just `Trigger`/`Item`/`Separator` — we'll need to add `Sub`/`SubTrigger`/`SubContent` to it. Update that file to expose them with consistent styling.

- [ ] **Step 2: Update the Sub primitive in `ui/ContextMenu.tsx`**

```tsx
// In ui/ContextMenu.tsx, add to the Object.assign:
Sub: KContextMenu.Sub,
SubTrigger: (props: {children: JSX.Element}) => (
  <KContextMenu.SubTrigger class={itemClass}>{props.children}</KContextMenu.SubTrigger>
),
SubContent: (props: {children: JSX.Element}) => (
  <KContextMenu.Portal>
    <KContextMenu.SubContent class={contentClass}>{props.children}</KContextMenu.SubContent>
  </KContextMenu.Portal>
),
```

- [ ] **Step 3: Wire `onSetCategory` + `onAssignTag` props through TorrentRowMenu**

Update `TorrentRowMenu`'s Props to accept `categories`, `tags`, `currentCategoryID`, `currentTagIDs`, `onSetCategory(id|null)`, `onToggleTag(id)`. The orchestrator (TorrentList) passes them.

- [ ] **Step 4: Update `TorrentList` orchestrator to pass through**

Pass `store.state.categories`, `store.state.tags`, plus handlers that call `store.setTorrentCategory(...)` and toggle assigning tags.

- [ ] **Step 5: Verify build**
- [ ] **Step 6: Commit**

```bash
git add frontend/src/components/ui/ContextMenu.tsx frontend/src/components/list/TorrentRowMenu.tsx frontend/src/components/list/TorrentList.tsx frontend/src/App.tsx
git commit -m "feat(frontend): row context menu — Set Category + Toggle Tag submenus"
```

---

### Task 16: FilesTab — per-file priority dropdown

**Files:** `frontend/src/components/inspector/FilesTab.tsx`

- [ ] **Step 1: Add a priority dropdown to each file row**

Use the existing `DropdownMenu` from `ui/DropdownMenu.tsx`. The dropdown has four items (Skip / Normal / High / Max) — selecting one calls `props.onSetPriority(file.index, value)`.

```tsx
<DropdownMenu trigger={
  <button class="text-xs text-zinc-500 hover:text-zinc-200">
    {f.priority}
  </button>
}>
  <DropdownMenu.Item onSelect={() => props.onSetPriority(f.index, 'skip')}>Skip</DropdownMenu.Item>
  <DropdownMenu.Item onSelect={() => props.onSetPriority(f.index, 'normal')}>Normal</DropdownMenu.Item>
  <DropdownMenu.Item onSelect={() => props.onSetPriority(f.index, 'high')}>High</DropdownMenu.Item>
  <DropdownMenu.Item onSelect={() => props.onSetPriority(f.index, 'max')}>Max</DropdownMenu.Item>
</DropdownMenu>
```

- [ ] **Step 2: Wire `onSetPriority` from Inspector → App.tsx → store.setFilePriorities**

The Inspector receives `onSetFilePriority(index, value)` in props; FilesTab gets it from there. App.tsx supplies the handler that calls `store.setFilePriorities(inspectorOpenId, {[index]: value})`.

- [ ] **Step 3: Verify build**
- [ ] **Step 4: Commit**

```bash
git add frontend/src/components/inspector/FilesTab.tsx frontend/src/components/inspector/Inspector.tsx frontend/src/App.tsx
git commit -m "feat(frontend): FilesTab per-file priority dropdown wired to backend"
```

---

## Section F — Add-Modal & .torrent Drop

### Task 17: Polished AddTorrentModal (replaces AddMagnetModal)

**Files:** Create `frontend/src/components/shell/AddTorrentModal.tsx`; delete `frontend/src/components/shell/AddMagnetModal.tsx`

- [ ] **Step 1: Write `AddTorrentModal`**

Three sections in a Kobalte Dialog:

1. **Source** — radio between "Magnet link" (textarea) and "Torrent file" (button → `pickAndAddTorrent`). When a torrent file is selected, parse its name from the path; when a magnet is pasted, parse the `dn=` parameter.
2. **Save target** — text input pre-filled with the configured default path. Browse button (later).
3. **Files & options** — placeholder for v1 (file tree from torrent metadata after add). Show category dropdown + tag chips picker. Toggle: "Start torrent immediately" / "Skip hash check" / "Sequential download" (latter two deferred to Plan 4b — show but disabled).

Submit button calls either `store.addMagnet(magnet)` or — if a `.torrent` file was picked — the existing `store.pickAndAddTorrent()` flow. After a successful add, also call `store.setTorrentCategory(id, selectedCategoryID)` and `store.assignTag(id, ...)` for selected tags.

Full code is too long to inline here; follow the structure of the existing AddMagnetModal but expand with the three Sections. Use `@kobalte/core/radio-group` for the source toggle.

- [ ] **Step 2: Update App.tsx**

Replace the `<AddMagnetModal>` import/usage with `<AddTorrentModal>`. Remove the import of the deleted file.

- [ ] **Step 3: Update TopToolbar**

The "+ .torrent" button now opens the same modal pre-set to "Torrent file" mode. The "+ Magnet" button opens it pre-set to "Magnet link" mode. Pass an `initialSource: 'magnet' | 'file'` prop to the modal.

- [ ] **Step 4: Delete the old AddMagnetModal**

```bash
git rm frontend/src/components/shell/AddMagnetModal.tsx
```

- [ ] **Step 5: Verify build + commit**

```bash
git add frontend/src/components/shell/AddTorrentModal.tsx frontend/src/App.tsx frontend/src/components/shell/TopToolbar.tsx
git commit -m "feat(frontend): polished AddTorrentModal (3 sections: source, save target, options)"
```

---

### Task 18: DropZone — accept .torrent files

**Files:** `frontend/src/components/shell/DropZone.tsx`

- [ ] **Step 1: Wire .torrent file drops**

Currently the `onDrop` handler shows a toast for files; replace with real handling:

```ts
if (e.dataTransfer?.files.length) {
  const file = e.dataTransfer.files[0];
  if (!file.name.endsWith('.torrent')) {
    toast.error('Only .torrent files are supported');
    return;
  }
  // We could read and base64 the file, but the cleanest path is to expose a
  // backend method that takes a file path. Browser File objects don't expose
  // a real path in a webview, so use the FileReader → bytes → backend route.
  const bytes = new Uint8Array(await file.arrayBuffer());
  // Convert to a number[] for Wails JSON, or call a new method that takes []byte.
  await props.onTorrentBytes(bytes);
}
```

Wails JSON marshaling supports `[]byte` natively (encoded as base64 over the wire). Add an `AddTorrentBytes(blob []byte, savePath string)` method to the App + Service that wraps `engine.AddFile`. This is the minimal IPC surface for the drop.

- [ ] **Step 2: Backend AddTorrentBytes method**

In `backend/api/service.go`, add:
```go
func (s *Service) AddTorrentBytes(ctx context.Context, blob []byte, savePath string) (engine.TorrentID, error) {
	if savePath == "" {
		savePath = s.defaultSavePath
	}
	id, err := s.engine.AddFile(ctx, blob, savePath)
	if err != nil {
		return "", err
	}
	snap, err := s.engine.Snapshot(id)
	if err != nil {
		return "", err
	}
	if err := s.torrents.Save(ctx, persistence.TorrentRecord{
		InfoHash: string(id),
		Name:     snap.Name,
		SavePath: savePath,
		AddedAt:  time.Now(),
	}); err != nil {
		return "", err
	}
	return id, nil
}
```

In `app.go`:
```go
func (a *App) AddTorrentBytes(blob []byte, savePath string) (string, error) {
	id, err := a.svc.AddTorrentBytes(a.ctx, blob, savePath)
	if err != nil {
		return "", err
	}
	return string(id), nil
}
```

Regenerate Wails bindings.

- [ ] **Step 3: Wire frontend**

Update `DropZone`'s `onTorrentBytes` callback signature — it takes a `Uint8Array`, calls `api.addTorrentBytes(Array.from(bytes), '')`. (`Array.from` converts to a number array which JSON-serializes correctly.) Or use Wails' built-in []byte support which accepts ArrayBuffer / typed array directly — verify in your version.

- [ ] **Step 4: Update bindings.ts**

Add `addTorrentBytes` to the api object. Add `onTorrentBytes` to DropZone's Props.

- [ ] **Step 5: Update WindowShell + App.tsx to thread the new handler**

- [ ] **Step 6: Verify build + commit**

```bash
git add frontend/src/components/shell/DropZone.tsx backend/api/ app.go frontend/src/lib/bindings.ts frontend/src/components/shell/WindowShell.tsx frontend/src/App.tsx
git commit -m "feat: drag-drop .torrent files onto window — AddTorrentBytes IPC"
```

---

## Section G — End-to-end smoke test

### Task 19: User-driven smoke test

- [ ] **Step 1: Run `~/go/bin/wails dev -skipembedcreate`**

- [ ] **Step 2: Visual + functional check**

- Open the app. FilterRail Categories/Tags sections now show "No categories yet" / "No tags yet" instead of "Coming in Plan 4".
- Open the AddTorrentModal via `+ Magnet` — see three sections (Source / Save target / Files & options).
- Click the Source toggle to "Torrent file" — sees the file picker button.
- Add a torrent. Open inspector, switch to Files tab. Click the priority dropdown next to a file → choose "Skip" → file's progress freezes.
- Right-click a torrent row → submenu shows Category + Tags options. Create a category if there isn't one (use the modal). Assign it. Verify the row carries a colored dot for the category.
- Click the category in FilterRail → list filters. Click again → unfilters.
- Drag a `.torrent` file from Finder onto the window → DropZone activates → file drops → torrent added. Toast confirms.
- Inspector Overview tab now shows real Ratio (BytesUp / BytesDown) instead of 0.00.

- [ ] **Step 3: Tag**

```bash
git tag plan-4a-organization-complete
git push origin main
git push origin plan-4a-organization-complete
```

---

**End of Plan 4a.**
