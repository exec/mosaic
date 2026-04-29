# Mosaic — Plan 3: Inspector + Snapshot Enrichment + Watch IPC

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Take the polished Plan 2 shell and add the right-side inspector with all five tabs (Overview / Files / Peers / Trackers / Speed). This is where the app stops being a list-of-torrents and becomes a power-user tool. Includes the backend work to surface file lists, peer details, tracker status, and bandwidth history, plus the focus-driven IPC so high-frequency data only flows when the inspector is open.

**Architecture:** Backend grows new domain types (FileEntry, PeerEntry, TrackerEntry), an enriched `DetailedSnapshot(id, scope)` on the engine, and an `InspectorFocus` state on the api Service that gates a 1-second `inspector:tick` event. Frontend adds an Inspector slide-in panel that drives focus state and renders five tabs; uPlot powers the Speed tab's bandwidth chart with a small ring buffer in the store. The current `torrents:tick` and `stats:tick` events stay unchanged — Plan 3 adds the inspector channel alongside.

**Tech additions:**
- `uplot` — fast time-series chart for the Speed tab. ~30 KB gzipped, framework-agnostic, no Solid wrapper needed (mount on a `<div ref>`).

**Reference spec:** `docs/superpowers/specs/2026-04-28-bittorrent-client-design.md` §4.4 (inspector tabs), §5.4 (subscription-driven IPC).

**Aesthetic continuity:** Same tokens from Plan 2. Inspector panel matches FilterRail's glass treatment (`bg-white/[.01]` with backdrop-blur). Tab segmented control reuses the Kobalte ToggleGroup styling. Tables (peers, trackers) use the same TanStack stack as the main list — sticky headers, tabular numerals, hover rows. Bandwidth chart uses accent-violet for download, zinc-500 for upload, no chart border.

---

## Out of Scope (deferred to Plan 4)

- **Polished add-modal** with file tree + category/tags fields — Plan 4
- **`.torrent` file drop** wiring on DropZone (currently shows toast pointing to button) — Plan 4
- **Real categories/tags/trackers** data in FilterRail (still "Coming in Plan 4" stubs) — Plan 4
- **Diff-based `torrents:tick`** (currently full snapshot every 500ms; fine until 200+ torrents) — Plan 4 or 5
- **Per-torrent rate-limit overrides** in Speed tab UI (backend supports it; frontend wires in Plan 4 alongside other limits) — Plan 4
- **Queueing, scheduling, IP filtering** — Plan 4

---

## File Structure (final state)

```
backend/
├── engine/
│   ├── types.go                # + FileEntry, PeerEntry, TrackerEntry, DetailScope, Detail
│   ├── engine.go               # + DetailedSnapshot(id, scope)
│   ├── engine_test.go          # + tests for DetailedSnapshot via FakeBackend
│   ├── anacrolix.go            # + detail extraction from anacrolix Torrent.Files()/PeerConns()/Trackers
│   └── fake.go                 # + Detail support for tests
└── api/
    ├── service.go              # + InspectorFocus state + SetInspectorFocus/ClearInspectorFocus + DetailDTO
    └── service_test.go         # + tests for focus state + detail DTOs
app.go                          # + SetInspectorFocus/ClearInspectorFocus bindings + inspector:tick emission

frontend/src/
├── lib/
│   ├── bindings.ts             # + Detail types + SetInspectorFocus/ClearInspectorFocus + onInspectorTick
│   ├── store.ts                # + inspector state (torrentId, tab, detail snapshot, bandwidth ring)
│   └── format.ts               # + fmtDuration, fmtTimestamp helpers
└── components/
    └── inspector/
        ├── Inspector.tsx           # slide-in panel, header + tabs + active body
        ├── InspectorHeader.tsx     # name + size + progress + ETA + close
        ├── InspectorTabs.tsx       # segmented control via Kobalte ToggleGroup
        ├── OverviewTab.tsx         # static info: hash, save path, magnet, pieces, ratio, totals, timestamps
        ├── FilesTab.tsx            # flat file list with priority dropdown + per-file progress
        ├── PeersTab.tsx            # TanStack table: IP / Client / Flags / Progress / ↓ / ↑
        ├── TrackersTab.tsx         # list of trackers with status, seeds, peers, last/next announce
        ├── SpeedTab.tsx            # uPlot bandwidth chart with 5min/1hr/24hr range toggle
        └── BandwidthChart.tsx      # uPlot wrapper component
```

WindowShell will gain an Inspector slot to the right of `main`.

---

## Task 1: Install uPlot

**Files:** `frontend/package.json`

- [ ] **Step 1: Install**

```bash
cd frontend && npm install uplot
```

uplot ships its own minimal CSS — we'll import it from BandwidthChart.tsx.

- [ ] **Step 2: Verify build**

```bash
npm run build
```

Expected: exit 0.

- [ ] **Step 3: Commit**

```bash
git add frontend/package.json frontend/package-lock.json
git commit -m "chore(frontend): add uPlot for bandwidth charts"
```

---

## Task 2: Engine domain types — FileEntry, PeerEntry, TrackerEntry, Detail

**Files:** `backend/engine/types.go` (modify)

- [ ] **Step 1: Append types**

Append to `backend/engine/types.go`:

```go
// FileEntry is one file inside a torrent's content tree.
type FileEntry struct {
	Index     int        // anacrolix file index, stable across the torrent's life
	Path      string     // forward-slash relative path within the torrent
	Size      int64
	BytesDone int64
	Priority  Priority   // Skip/Normal/High/Max
}

// Priority maps to anacrolix's piece-priority levels.
type Priority int

const (
	PrioritySkip   Priority = 0
	PriorityNormal Priority = 1
	PriorityHigh   Priority = 2
	PriorityMax    Priority = 3
)

// PeerEntry is one connected (or recently connected) peer.
type PeerEntry struct {
	IP            string
	Port          int
	ClientName    string  // e.g. "qBittorrent 4.6.2"
	Flags         string  // BitTorrent client flags string ("D K E I O X cd" etc.)
	Progress      float64 // 0..1
	DownloadRate  int64   // bytes/sec from this peer
	UploadRate    int64   // bytes/sec to this peer
	CountryCode   string  // ISO-3166 alpha-2; empty if unknown
}

// TrackerEntry is one tracker URL announced by the torrent.
type TrackerEntry struct {
	URL           string
	Status        string  // "OK", "Updating", "Not contacted", "Error: ..."
	Seeds         int     // last reported
	Peers         int
	Downloaded    int     // last reported total downloads
	LastAnnounce  time.Time
	NextAnnounce  time.Time
}

// DetailScope controls how much detail the engine packs into a Detail.
type DetailScope struct {
	Files    bool
	Peers    bool
	Trackers bool
}

// Detail is a Snapshot plus optional per-tab heavy data. Empty slices when
// the corresponding scope flag is false.
type Detail struct {
	Snapshot Snapshot
	Files    []FileEntry
	Peers    []PeerEntry
	Trackers []TrackerEntry
}
```

- [ ] **Step 2: Verify build**

```bash
go build ./backend/engine/
```

Expected: exit 0.

- [ ] **Step 3: Commit**

```bash
git add backend/engine/types.go
git commit -m "feat(engine): FileEntry/PeerEntry/TrackerEntry/Detail domain types"
```

---

## Task 3: Backend interface gains DetailedSnapshot — failing test

**Files:** `backend/engine/engine_test.go` (modify), `backend/engine/types.go` (already modified)

- [ ] **Step 1: Add Backend.DetailedSnapshot to the interface**

Modify `backend/engine/types.go`'s `Backend` interface, add the method:

```go
type Backend interface {
	AddMagnet(ctx context.Context, magnet, savePath string) (TorrentID, error)
	AddFile(ctx context.Context, blob []byte, savePath string) (TorrentID, error)
	Pause(id TorrentID) error
	Resume(id TorrentID) error
	Remove(id TorrentID, deleteFiles bool) error
	List() []Snapshot
	Snapshot(id TorrentID) (Snapshot, error)
	DetailedSnapshot(id TorrentID, scope DetailScope) (Detail, error)
	Close() error
}
```

- [ ] **Step 2: Add failing test in engine_test.go**

Append:

```go
func TestEngine_DetailedSnapshot_RoutesThroughBackend(t *testing.T) {
	fb := NewFakeBackend()
	eng := NewEngine(fb, 50*time.Millisecond)
	t.Cleanup(func() { _ = eng.Close() })

	id, err := eng.AddMagnet(context.Background(), "magnet:?xt=urn:btih:detail", "/tmp")
	require.NoError(t, err)

	d, err := eng.DetailedSnapshot(id, DetailScope{Files: true, Peers: true, Trackers: true})
	require.NoError(t, err)
	require.Equal(t, id, d.Snapshot.ID)
	// FakeBackend's seeded fixture returns 2 files / 1 peer / 1 tracker
	require.Len(t, d.Files, 2)
	require.Len(t, d.Peers, 1)
	require.Len(t, d.Trackers, 1)
}

func TestEngine_DetailedSnapshot_ScopeFlagsExcludeData(t *testing.T) {
	fb := NewFakeBackend()
	eng := NewEngine(fb, 50*time.Millisecond)
	t.Cleanup(func() { _ = eng.Close() })

	id, _ := eng.AddMagnet(context.Background(), "magnet:?xt=urn:btih:scope", "/tmp")

	// All three scopes off
	d, err := eng.DetailedSnapshot(id, DetailScope{})
	require.NoError(t, err)
	require.Empty(t, d.Files)
	require.Empty(t, d.Peers)
	require.Empty(t, d.Trackers)
}
```

- [ ] **Step 3: Run tests, confirm fails**

```bash
go test ./backend/engine/ -run TestEngine_DetailedSnapshot -v
```

Expected: FAIL with "Backend.DetailedSnapshot undefined" or "FakeBackend does not implement Backend".

---

## Task 4: FakeBackend.DetailedSnapshot

**Files:** `backend/engine/fake.go` (modify)

- [ ] **Step 1: Add a fixture-emitting DetailedSnapshot to FakeBackend**

Append to `backend/engine/fake.go`:

```go
// DetailedSnapshot returns a deterministic fixture for the requested scope.
// Files: two entries (one half-done, one done). Peers: one entry. Trackers: one entry.
func (f *FakeBackend) DetailedSnapshot(id TorrentID, scope DetailScope) (Detail, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.torrents[id]
	if !ok {
		return Detail{}, errors.New("not found")
	}
	d := Detail{Snapshot: *t}
	if scope.Files {
		d.Files = []FileEntry{
			{Index: 0, Path: "fake/disk1.iso", Size: 1 << 29, BytesDone: 1 << 28, Priority: PriorityNormal},
			{Index: 1, Path: "fake/README", Size: 4096, BytesDone: 4096, Priority: PriorityNormal},
		}
	}
	if scope.Peers {
		d.Peers = []PeerEntry{
			{IP: "10.0.0.1", Port: 6881, ClientName: "FakeClient 1.0", Flags: "D K E", Progress: 0.5, DownloadRate: 1024, UploadRate: 256, CountryCode: "US"},
		}
	}
	if scope.Trackers {
		d.Trackers = []TrackerEntry{
			{URL: "https://tracker.example/announce", Status: "OK", Seeds: 10, Peers: 5, Downloaded: 100, LastAnnounce: time.Unix(1700000000, 0), NextAnnounce: time.Unix(1700001800, 0)},
		}
	}
	return d, nil
}
```

- [ ] **Step 2: Verify build**

```bash
go build ./backend/engine/
```

Expected: exit 0.

- [ ] **Step 3: Run tests**

```bash
go test ./backend/engine/ -v -race
```

The interface assertion + the new tests should now compile, but **the engine wrapper still doesn't have DetailedSnapshot**, so tests fail with "eng.DetailedSnapshot undefined".

---

## Task 5: Engine wrapper exposes DetailedSnapshot

**Files:** `backend/engine/engine.go` (modify)

- [ ] **Step 1: Add the method**

Append to `backend/engine/engine.go`:

```go
// DetailedSnapshot delegates to the backend, packaging the file/peer/tracker
// data per scope alongside the standard Snapshot.
func (e *Engine) DetailedSnapshot(id TorrentID, scope DetailScope) (Detail, error) {
	return e.backend.DetailedSnapshot(id, scope)
}
```

- [ ] **Step 2: Run tests, confirm pass**

```bash
go test ./backend/engine/ -v -race
```

Expected: PASS (5 engine tests now: 3 existing + 2 new).

- [ ] **Step 3: Commit**

```bash
git add backend/engine/types.go backend/engine/engine.go backend/engine/engine_test.go backend/engine/fake.go
git commit -m "feat(engine): DetailedSnapshot(id, scope) for per-tab inspector data"
```

---

## Task 6: AnacrolixBackend.DetailedSnapshot

**Files:** `backend/engine/anacrolix.go` (modify)

- [ ] **Step 1: Implement detail extraction**

Append to `backend/engine/anacrolix.go`:

```go
// DetailedSnapshot pulls files/peers/trackers from the underlying anacrolix
// Torrent based on scope. Anacrolix's APIs:
//
//   t.Files()       — []*File with Path, Length, BytesCompleted, Priority
//   t.PeerConns()   — []PeerConn with RemoteAddr, ClientName, Stats, etc.
//   t.Metainfo()    — has AnnounceList
//   t.Stats().AnnounceList — last announce results
//
// We translate to our FileEntry/PeerEntry/TrackerEntry domain types.
func (a *AnacrolixBackend) DetailedSnapshot(id TorrentID, scope DetailScope) (Detail, error) {
	t, ok := a.find(id)
	if !ok {
		return Detail{}, errors.New("not found")
	}
	d := Detail{Snapshot: snapshotFor(t)}

	if scope.Files {
		for i, f := range t.Files() {
			d.Files = append(d.Files, FileEntry{
				Index:     i,
				Path:      f.DisplayPath(),
				Size:      f.Length(),
				BytesDone: f.BytesCompleted(),
				Priority:  prioFromAnacrolix(f.Priority()),
			})
		}
	}

	if scope.Peers {
		for _, pc := range t.PeerConns() {
			addr := pc.RemoteAddr.String()
			ip := addr
			port := 0
			if h, p, err := splitHostPort(addr); err == nil {
				ip = h
				port = p
			}
			name, _ := pc.PeerClientName.Load().(string)
			d.Peers = append(d.Peers, PeerEntry{
				IP:           ip,
				Port:         port,
				ClientName:   name,
				Flags:        peerFlagsFor(pc),
				Progress:     pieceProgressOf(t, pc),
				DownloadRate: int64(pc.DownloadRate()),
				UploadRate:   int64(pc.Stats().LastWriteUploadRate),
				CountryCode:  "", // anacrolix doesn't ship GeoIP — Plan 4+ may add a local DB
			})
		}
	}

	if scope.Trackers {
		mi := t.Metainfo()
		for _, tier := range mi.AnnounceList {
			for _, url := range tier {
				d.Trackers = append(d.Trackers, TrackerEntry{
					URL:    url,
					Status: "OK", // anacrolix doesn't expose per-tracker status cleanly; refined in Plan 4
				})
			}
		}
		// Fall back to the single Announce field if AnnounceList is empty
		if len(d.Trackers) == 0 && mi.Announce != "" {
			d.Trackers = append(d.Trackers, TrackerEntry{URL: mi.Announce, Status: "OK"})
		}
	}

	return d, nil
}

func prioFromAnacrolix(p anacrolix_types.PiecePriority) Priority {
	switch p {
	case anacrolix_types.PiecePriorityNone:
		return PrioritySkip
	case anacrolix_types.PiecePriorityNormal:
		return PriorityNormal
	case anacrolix_types.PiecePriorityHigh:
		return PriorityHigh
	case anacrolix_types.PiecePriorityNow:
		return PriorityMax
	}
	return PriorityNormal
}

// splitHostPort wraps net.SplitHostPort to also parse the port.
func splitHostPort(addr string) (string, int, error) {
	h, p, err := net.SplitHostPort(addr)
	if err != nil {
		return "", 0, err
	}
	port, err := strconv.Atoi(p)
	if err != nil {
		return h, 0, err
	}
	return h, port, nil
}

// peerFlagsFor returns the BitTorrent-style peer flag string. Plan 3 emits an
// empty string: anacrolix v1.61 does not expose `peerInterested`, `peerChoking`,
// or any header-obfuscation accessor outside the package, so we cannot read the
// bits. Plan 4 (or a small upstream PR) should add them back when accessors land.
func peerFlagsFor(pc *torrent.PeerConn) string {
	return ""
}

// pieceProgressOf returns 0..1 of the parent torrent's pieces this peer has.
// Denominator is t.NumPieces() because *roaring.Bitmap has no Len() method —
// it's a sparse set, not a fixed-length bitmap.
func pieceProgressOf(t *torrent.Torrent, pc *torrent.PeerConn) float64 {
	pp := pc.PeerPieces()
	if pp.IsEmpty() {
		return 0
	}
	n := t.NumPieces()
	if n == 0 {
		return 0
	}
	return float64(pp.GetCardinality()) / float64(n)
}
```

> **Anacrolix v1.61 alignment notes:** Plan was originally written assuming method names that don't exist on this version. Confirmed substitutions:
> - `pc.UploadRate()` → `int64(pc.Stats().LastWriteUploadRate)` (no public `UploadRate()` method)
> - `pc.PeerInterested.Load()` / `pc.PeerChoking.Load()` / `pc.HeaderObfuscation` → unreadable (unexported `bool` fields and missing field). `Flags` ships empty for now.
> - `pp.Len()` on `*roaring.Bitmap` → `t.NumPieces()` (Bitmap has no `Len()`; we want the parent torrent's piece count).
> - `pc.PeerClientName.Load()` returns `interface{}` (atomic.Value) — type-assert to string.
> Confirmed working on v1.61.0: `t.Files()`, `f.DisplayPath/Length/BytesCompleted/Priority`, `t.PeerConns()`, `pc.RemoteAddr`, `pc.PeerClientName`, `pc.PeerPieces()`, `pc.DownloadRate()`, `t.Metainfo().AnnounceList/Announce`, `anacrolix_types.PiecePriorityNone/Normal/High/Now`.

- [ ] **Step 2: Add imports**

In `backend/engine/anacrolix.go` near the top, ensure imports include:

```go
import (
	"net"
	"strconv"
	anacrolix_types "github.com/anacrolix/torrent/types"
)
```

- [ ] **Step 3: Verify build**

```bash
go build ./...
```

Expected: exit 0. If anacrolix internals have changed, fix imports/method names accordingly.

- [ ] **Step 4: Commit**

```bash
git add backend/engine/anacrolix.go
git commit -m "feat(engine): anacrolix DetailedSnapshot — files/peers/trackers extraction"
```

---

## Task 7: API service — InspectorFocus state + DTOs — failing test

**Files:** `backend/api/service_test.go` (modify)

- [ ] **Step 1: Add failing tests**

Append to `backend/api/service_test.go`:

```go
func TestService_InspectorFocus_StoresAndReturnsDetail(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	id, _ := svc.AddMagnet(ctx, "magnet:?xt=urn:btih:focus", "")

	// No focus set — DetailForFocus returns nil, nil
	got, err := svc.DetailForFocus(ctx)
	require.NoError(t, err)
	require.Nil(t, got)

	// Set focus to this torrent with all tabs
	require.NoError(t, svc.SetInspectorFocus(string(id), []string{"overview", "files", "peers", "trackers"}))

	got, err = svc.DetailForFocus(ctx)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, string(id), got.ID)
	require.Len(t, got.Files, 2)
	require.Len(t, got.Peers, 1)
	require.Len(t, got.Trackers, 1)
}

func TestService_ClearInspectorFocus(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	id, _ := svc.AddMagnet(ctx, "magnet:?xt=urn:btih:cf", "")
	require.NoError(t, svc.SetInspectorFocus(string(id), []string{"overview"}))
	svc.ClearInspectorFocus()

	got, err := svc.DetailForFocus(ctx)
	require.NoError(t, err)
	require.Nil(t, got)
}

func TestService_InspectorFocus_ScopesByVisibleTabs(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	id, _ := svc.AddMagnet(ctx, "magnet:?xt=urn:btih:scope2", "")

	// Only Overview tab visible — files/peers/trackers should be empty
	require.NoError(t, svc.SetInspectorFocus(string(id), []string{"overview"}))
	got, _ := svc.DetailForFocus(ctx)
	require.NotNil(t, got)
	require.Empty(t, got.Files)
	require.Empty(t, got.Peers)
	require.Empty(t, got.Trackers)

	// Switch to peers tab
	require.NoError(t, svc.SetInspectorFocus(string(id), []string{"overview", "peers"}))
	got, _ = svc.DetailForFocus(ctx)
	require.Empty(t, got.Files)
	require.Len(t, got.Peers, 1)
	require.Empty(t, got.Trackers)
}
```

- [ ] **Step 2: Run tests, confirm fails**

```bash
go test ./backend/api/ -run TestService_InspectorFocus -v
```

Expected: FAIL with "svc.SetInspectorFocus undefined" / "svc.DetailForFocus undefined".

---

## Task 8: API service — InspectorFocus implementation

**Files:** `backend/api/service.go` (modify)

- [ ] **Step 1: Add focus state + types + methods**

Append to `backend/api/service.go`:

```go
// DetailDTO is the inspector tick payload, returned from DetailForFocus or
// emitted via the inspector:tick event.
type DetailDTO struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	// Overview-tab fields — always present
	Magnet      string  `json:"magnet"`
	SavePath    string  `json:"save_path"`
	TotalBytes  int64   `json:"total_bytes"`
	BytesDone   int64   `json:"bytes_done"`
	Progress    float64 `json:"progress"`
	Ratio       float64 `json:"ratio"`
	TotalDown   int64   `json:"total_down"`
	TotalUp     int64   `json:"total_up"`
	Peers       int     `json:"peers"`
	Seeds       int     `json:"seeds"`
	AddedAt     int64   `json:"added_at"`
	CompletedAt int64   `json:"completed_at,omitempty"`

	Files    []FileDTO    `json:"files,omitempty"`
	PeersList []PeerDTO   `json:"peers_list,omitempty"`
	Trackers []TrackerDTO `json:"trackers,omitempty"`
}

type FileDTO struct {
	Index     int     `json:"index"`
	Path      string  `json:"path"`
	Size      int64   `json:"size"`
	BytesDone int64   `json:"bytes_done"`
	Progress  float64 `json:"progress"`
	Priority  string  `json:"priority"` // "skip" | "normal" | "high" | "max"
}

type PeerDTO struct {
	IP           string  `json:"ip"`
	Port         int     `json:"port"`
	Client       string  `json:"client"`
	Flags        string  `json:"flags"`
	Progress     float64 `json:"progress"`
	DownloadRate int64   `json:"download_rate"`
	UploadRate   int64   `json:"upload_rate"`
	Country      string  `json:"country"`
}

type TrackerDTO struct {
	URL          string `json:"url"`
	Status       string `json:"status"`
	Seeds        int    `json:"seeds"`
	Peers        int    `json:"peers"`
	Downloaded   int    `json:"downloaded"`
	LastAnnounce int64  `json:"last_announce"`
	NextAnnounce int64  `json:"next_announce"`
}

// SetInspectorFocus tells the service which torrent + tabs the UI is looking
// at. Subsequent DetailForFocus calls (and the inspector:tick event in app.go)
// will return the appropriately-scoped Detail. tabs is a subset of:
// "overview", "files", "peers", "trackers", "speed".
func (s *Service) SetInspectorFocus(id string, tabs []string) error {
	if id == "" {
		s.ClearInspectorFocus()
		return nil
	}
	scope := scopeForTabs(tabs)
	s.focusMu.Lock()
	s.focusID = engine.TorrentID(id)
	s.focusScope = scope
	s.focusMu.Unlock()
	return nil
}

func (s *Service) ClearInspectorFocus() {
	s.focusMu.Lock()
	s.focusID = ""
	s.focusScope = engine.DetailScope{}
	s.focusMu.Unlock()
}

// DetailForFocus returns the current focused torrent's detail, or nil if no
// inspector focus is set.
func (s *Service) DetailForFocus(ctx context.Context) (*DetailDTO, error) {
	s.focusMu.RLock()
	id := s.focusID
	scope := s.focusScope
	s.focusMu.RUnlock()
	if id == "" {
		return nil, nil
	}
	d, err := s.engine.DetailedSnapshot(id, scope)
	if err != nil {
		return nil, err
	}
	dto := detailToDTO(d, s.lookupAddedAt(ctx, id))
	return &dto, nil
}

func scopeForTabs(tabs []string) engine.DetailScope {
	scope := engine.DetailScope{}
	for _, t := range tabs {
		switch t {
		case "files":
			scope.Files = true
		case "peers":
			scope.Peers = true
		case "trackers":
			scope.Trackers = true
		}
	}
	return scope
}

func (s *Service) lookupAddedAt(ctx context.Context, id engine.TorrentID) time.Time {
	rec, err := s.torrents.Get(ctx, string(id))
	if err != nil {
		return time.Time{}
	}
	return rec.AddedAt
}

func detailToDTO(d engine.Detail, addedAt time.Time) DetailDTO {
	snap := d.Snapshot
	prog := 0.0
	if snap.TotalBytes > 0 {
		prog = float64(snap.BytesDone) / float64(snap.TotalBytes)
	}
	// Ratio left at 0.0 in Plan 3 — engine.Snapshot doesn't yet expose
	// cumulative-uploaded vs cumulative-downloaded as separate fields, and the
	// UploadRate/DownloadRate names are misleading carry-overs from Plan 1
	// (they're actually cumulative byte counts). Plan 4 will rename to
	// BytesUp/BytesDown and compute ratio = BytesUp / BytesDown.
	dto := DetailDTO{
		ID:         string(snap.ID),
		Name:       snap.Name,
		Magnet:     snap.Magnet,
		SavePath:   snap.SavePath,
		TotalBytes: snap.TotalBytes,
		BytesDone:  snap.BytesDone,
		Progress:   prog,
		Ratio:      0.0,
		TotalDown:  snap.DownloadRate, // misnamed — actually cumulative bytes (Plan 1 leftover); rename in Plan 4
		TotalUp:    snap.UploadRate,
		Peers:      snap.Peers,
		Seeds:      snap.Seeds,
		AddedAt:    addedAt.Unix(),
	}
	for _, f := range d.Files {
		fp := 0.0
		if f.Size > 0 {
			fp = float64(f.BytesDone) / float64(f.Size)
		}
		dto.Files = append(dto.Files, FileDTO{
			Index: f.Index, Path: f.Path, Size: f.Size, BytesDone: f.BytesDone, Progress: fp,
			Priority: priorityToString(f.Priority),
		})
	}
	for _, p := range d.Peers {
		dto.PeersList = append(dto.PeersList, PeerDTO{
			IP: p.IP, Port: p.Port, Client: p.ClientName, Flags: p.Flags,
			Progress: p.Progress, DownloadRate: p.DownloadRate, UploadRate: p.UploadRate, Country: p.CountryCode,
		})
	}
	for _, t := range d.Trackers {
		dto.Trackers = append(dto.Trackers, TrackerDTO{
			URL: t.URL, Status: t.Status, Seeds: t.Seeds, Peers: t.Peers, Downloaded: t.Downloaded,
			LastAnnounce: t.LastAnnounce.Unix(), NextAnnounce: t.NextAnnounce.Unix(),
		})
	}
	return dto
}

func priorityToString(p engine.Priority) string {
	switch p {
	case engine.PrioritySkip:
		return "skip"
	case engine.PriorityHigh:
		return "high"
	case engine.PriorityMax:
		return "max"
	}
	return "normal"
}
```

- [ ] **Step 2: Add focus state fields to Service struct**

Modify the `Service` struct definition in `backend/api/service.go`:

```go
type Service struct {
	engine          *engine.Engine
	torrents        *persistence.Torrents
	defaultSavePath string

	focusMu    sync.RWMutex
	focusID    engine.TorrentID
	focusScope engine.DetailScope
}
```

Add `"sync"` to the imports.

- [ ] **Step 3: Run tests**

```bash
go test ./backend/api/ -v -race
```

Expected: PASS (5 api tests + 3 new = 8 total).

- [ ] **Step 4: Commit**

```bash
git add backend/api/service.go backend/api/service_test.go
git commit -m "feat(api): InspectorFocus state + DetailDTO scoped by visible tabs"
```

---

## Task 9: app.go — bind SetInspectorFocus + emit inspector:tick

**Files:** `app.go` (modify)

- [ ] **Step 1: Add Wails-bound methods**

Add to `App` in `app.go`:

```go
// SetInspectorFocus tells the backend the inspector is open on torrent `id`
// with `tabs` visible. The next inspector:tick (and subsequent ticks at 1Hz)
// will include data scoped to those tabs.
func (a *App) SetInspectorFocus(id string, tabs []string) error {
	return a.svc.SetInspectorFocus(id, tabs)
}

// ClearInspectorFocus stops inspector:tick emission until SetInspectorFocus is called again.
func (a *App) ClearInspectorFocus() {
	a.svc.ClearInspectorFocus()
}
```

- [ ] **Step 2: Add a third ticker in streamTicks for inspector**

Modify `streamTicks` in `app.go`:

```go
func (a *App) streamTicks(ctx context.Context) {
	torrents := time.NewTicker(500 * time.Millisecond)
	stats := time.NewTicker(1 * time.Second)
	inspector := time.NewTicker(1 * time.Second)
	defer torrents.Stop()
	defer stats.Stop()
	defer inspector.Stop()
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
		case <-inspector.C:
			detail, err := a.svc.DetailForFocus(ctx)
			if err != nil {
				log.Error().Err(err).Msg("detail for focus during tick")
				continue
			}
			if detail == nil {
				continue // no focus, no emit
			}
			wailsruntime.EventsEmit(ctx, "inspector:tick", detail)
		}
	}
}
```

- [ ] **Step 3: Regenerate Wails bindings**

```bash
$(go env GOPATH)/bin/wails generate module
```

Verify `frontend/wailsjs/go/main/App.d.ts` exports `SetInspectorFocus(id, tabs)` and `ClearInspectorFocus()`.

- [ ] **Step 4: Verify build**

```bash
go build ./...
```

Expected: exit 0. Backend tests still 8/8 pass.

- [ ] **Step 5: Commit**

```bash
git add app.go
git commit -m "feat: SetInspectorFocus binding + 1Hz inspector:tick event"
```

---

## Task 10: Frontend bindings + inspector store

**Files:** `frontend/src/lib/bindings.ts`, `frontend/src/lib/store.ts`, `frontend/src/lib/format.ts`

- [ ] **Step 1: Extend bindings.ts**

Add to `frontend/src/lib/bindings.ts`:

```ts
import {
  AddMagnet, GlobalStats, ListTorrents, Pause, PickAndAddTorrent, Remove, Resume,
  SetInspectorFocus, ClearInspectorFocus,
} from '../../wailsjs/go/main/App';

// ... existing types ...

export type FileDTO = {
  index: number;
  path: string;
  size: number;
  bytes_done: number;
  progress: number;
  priority: 'skip' | 'normal' | 'high' | 'max';
};

export type PeerDTO = {
  ip: string;
  port: number;
  client: string;
  flags: string;
  progress: number;
  download_rate: number;
  upload_rate: number;
  country: string;
};

export type TrackerDTO = {
  url: string;
  status: string;
  seeds: number;
  peers: number;
  downloaded: number;
  last_announce: number; // unix seconds
  next_announce: number;
};

export type DetailDTO = {
  id: string;
  name: string;
  magnet: string;
  save_path: string;
  total_bytes: number;
  bytes_done: number;
  progress: number;
  ratio: number;
  total_down: number;
  total_up: number;
  peers: number;
  seeds: number;
  added_at: number;
  completed_at?: number;
  files?: FileDTO[];
  peers_list?: PeerDTO[];
  trackers?: TrackerDTO[];
};

// extend api object:
export const api = {
  addMagnet: (magnet: string) => AddMagnet(magnet),
  pickAndAddTorrent: () => PickAndAddTorrent(),
  listTorrents: () => ListTorrents() as Promise<Torrent[]>,
  globalStats: () => GlobalStats() as Promise<GlobalStatsT>,
  pause: (id: string) => Pause(id),
  resume: (id: string) => Resume(id),
  remove: (id: string, deleteFiles: boolean) => Remove(id, deleteFiles),
  setInspectorFocus: (id: string, tabs: InspectorTab[]) => SetInspectorFocus(id, tabs),
  clearInspectorFocus: () => ClearInspectorFocus(),
};

export type InspectorTab = 'overview' | 'files' | 'peers' | 'trackers' | 'speed';

export function onInspectorTick(handler: (detail: DetailDTO) => void): () => void {
  return EventsOn('inspector:tick', handler);
}
```

- [ ] **Step 2: Extend store.ts**

Add to the `AppState` type in `frontend/src/lib/store.ts`:

```ts
export type AppState = {
  torrents: Torrent[];
  stats: GlobalStatsT;
  selection: Set<string>;
  density: Density;
  statusFilter: StatusFilter;
  searchQuery: string;
  loading: boolean;

  // Inspector
  inspectorOpenId: string | null;       // null = closed
  inspectorTab: InspectorTab;
  inspectorDetail: DetailDTO | null;    // latest tick payload
  bandwidthRing: BandwidthSample[];     // ring buffer for Speed-tab chart, ~1Hz, capped at 24h
};

export type BandwidthSample = {t: number; down: number; up: number};

const BANDWIDTH_RING_MAX = 60 * 60 * 24; // 24 hours at 1 Hz

// In createTorrentsStore, add inspector subscription:

const offI = onInspectorTick((detail) => {
  setState(produce((s) => {
    s.inspectorDetail = detail;
    s.bandwidthRing.push({
      t: Date.now() / 1000,
      down: stateRef.state.stats.total_download_rate,  // see note below
      up:   stateRef.state.stats.total_upload_rate,
    });
    if (s.bandwidthRing.length > BANDWIDTH_RING_MAX) s.bandwidthRing.shift();
  }));
});

// Methods:
return {
  // ... existing ...
  openInspector: async (id: string, tab: InspectorTab = 'overview') => {
    setState(produce((s) => {
      s.inspectorOpenId = id;
      s.inspectorTab = tab;
      s.inspectorDetail = null; // wait for next tick
      s.bandwidthRing = [];     // reset ring per torrent
    }));
    await api.setInspectorFocus(id, tabsForActive(tab));
  },
  closeInspector: async () => {
    setState(produce((s) => { s.inspectorOpenId = null; s.inspectorDetail = null; }));
    await api.clearInspectorFocus();
  },
  setInspectorTab: async (tab: InspectorTab) => {
    setState(produce((s) => { s.inspectorTab = tab; }));
    if (state.inspectorOpenId) {
      await api.setInspectorFocus(state.inspectorOpenId, tabsForActive(tab));
    }
  },
  // ... existing ...
  dispose: () => { offT(); offS(); offI(); },
};
```

> Helper at top of file: `function tabsForActive(tab: InspectorTab): InspectorTab[] { return tab === 'overview' ? ['overview'] : ['overview', tab]; }` — Overview is always live alongside whatever's selected, so the panel header stays accurate.

> The bandwidth ring uses **global** rates from `stats`, not per-torrent. Per-torrent rates would require a `Watch` model with per-torrent ring; deferred to Plan 4. Plan 3 ships with global rates rendered when the Speed tab is open — that's still useful.

- [ ] **Step 3: Add format helpers**

Append to `frontend/src/lib/format.ts`:

```ts
export function fmtDuration(seconds: number): string {
  if (seconds < 60) return `${Math.round(seconds)}s`;
  if (seconds < 3600) return `${Math.round(seconds / 60)}m`;
  if (seconds < 86_400) {
    const h = Math.floor(seconds / 3600);
    const m = Math.round((seconds - h * 3600) / 60);
    return `${h}h ${m}m`;
  }
  const d = Math.floor(seconds / 86_400);
  const h = Math.round((seconds - d * 86_400) / 3600);
  return `${d}d ${h}h`;
}

export function fmtTimestamp(unixSeconds: number): string {
  if (!unixSeconds) return '—';
  const d = new Date(unixSeconds * 1000);
  return d.toLocaleString();
}
```

Add tests in `frontend/src/lib/format.test.ts`:

```ts
describe('fmtDuration', () => {
  test('seconds', () => expect(fmtDuration(45)).toBe('45s'));
  test('minutes', () => expect(fmtDuration(125)).toBe('2m'));
  test('hours+minutes', () => expect(fmtDuration(3725)).toBe('1h 2m'));
  test('days+hours', () => expect(fmtDuration(90_061)).toBe('1d 1h'));
});

describe('fmtTimestamp', () => {
  test('zero is em-dash', () => expect(fmtTimestamp(0)).toBe('—'));
  test('renders a date string', () => {
    const out = fmtTimestamp(1700000000);
    expect(out).toMatch(/2023/); // sanity check the year
  });
});
```

- [ ] **Step 4: Verify build + tests**

```bash
cd frontend && npm run build && npm test
```

Expected: build clean, tests 21 → 27.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/lib/bindings.ts frontend/src/lib/store.ts frontend/src/lib/format.ts frontend/src/lib/format.test.ts
git commit -m "feat(frontend): inspector store + bindings + duration/timestamp helpers"
```

---

## Task 11: Inspector header + tabs scaffold

**Files:** `frontend/src/components/inspector/InspectorHeader.tsx`, `frontend/src/components/inspector/InspectorTabs.tsx`, `frontend/src/components/inspector/Inspector.tsx`

- [ ] **Step 1: InspectorHeader**

`frontend/src/components/inspector/InspectorHeader.tsx`:

```tsx
import {X} from 'lucide-solid';
import {fmtBytes, fmtETA, fmtPercent, fmtRate} from '../../lib/format';
import {ProgressBar} from '../ui/ProgressBar';
import type {DetailDTO} from '../../lib/bindings';

type Props = {
  detail: DetailDTO | null;
  onClose: () => void;
};

export function InspectorHeader(props: Props) {
  return (
    <header class="border-b border-white/[.04] px-4 py-3">
      <div class="flex items-start justify-between gap-2">
        <div class="min-w-0">
          <div class="truncate text-sm font-semibold text-zinc-100">
            {props.detail?.name ?? '—'}
          </div>
          <div class="mt-0.5 font-mono text-xs tabular-nums text-zinc-500">
            {props.detail ? fmtBytes(props.detail.total_bytes) : '—'}
          </div>
        </div>
        <button
          type="button"
          onClick={props.onClose}
          class="grid h-7 w-7 shrink-0 place-items-center rounded-md text-zinc-500 hover:bg-white/[.04] hover:text-zinc-100"
          aria-label="Close inspector"
        >
          <X class="h-4 w-4" />
        </button>
      </div>
      <div class="mt-3">
        <ProgressBar value={props.detail?.progress ?? 0} active={!!props.detail && props.detail.progress < 1} />
      </div>
      <div class="mt-2 flex items-center justify-between font-mono text-xs tabular-nums text-zinc-500">
        <span>{fmtPercent(props.detail?.progress ?? 0)}</span>
        <span>
          {props.detail
            ? `${fmtRate(0)} · ETA ${fmtETA(props.detail.total_bytes - props.detail.bytes_done, 0)}`
            : '—'}
        </span>
      </div>
    </header>
  );
}
```

> The rate-rendering in the header passes 0 — the actual rate lives on the `Torrent` row, not the `DetailDTO`. We can plumb it later; for v1 the inspector doesn't double-show rates that the row already shows.

- [ ] **Step 2: InspectorTabs**

`frontend/src/components/inspector/InspectorTabs.tsx`:

```tsx
import {ToggleGroup} from '@kobalte/core/toggle-group';
import type {InspectorTab} from '../../lib/bindings';

const labels: {value: InspectorTab; label: string}[] = [
  {value: 'overview', label: 'Overview'},
  {value: 'files',    label: 'Files'},
  {value: 'peers',    label: 'Peers'},
  {value: 'trackers', label: 'Trackers'},
  {value: 'speed',    label: 'Speed'},
];

type Props = {
  active: InspectorTab;
  onChange: (t: InspectorTab) => void;
};

export function InspectorTabs(props: Props) {
  return (
    <ToggleGroup
      class="flex w-full items-center gap-px rounded-md border border-white/[.06] bg-white/[.02] p-0.5"
      value={props.active}
      onChange={(v) => v && props.onChange(v as InspectorTab)}
    >
      {labels.map((it) => (
        <ToggleGroup.Item
          value={it.value}
          class="flex-1 rounded px-2 py-1 text-xs text-zinc-400 transition-colors duration-100 hover:text-zinc-100 data-[pressed]:bg-white/10 data-[pressed]:text-zinc-100"
        >
          {it.label}
        </ToggleGroup.Item>
      ))}
    </ToggleGroup>
  );
}
```

- [ ] **Step 3: Inspector slide-in**

`frontend/src/components/inspector/Inspector.tsx`:

```tsx
import {Match, Show, Switch} from 'solid-js';
import type {DetailDTO, InspectorTab} from '../../lib/bindings';
import type {BandwidthSample} from '../../lib/store';
import {InspectorHeader} from './InspectorHeader';
import {InspectorTabs} from './InspectorTabs';
import {OverviewTab} from './OverviewTab';
import {FilesTab} from './FilesTab';
import {PeersTab} from './PeersTab';
import {TrackersTab} from './TrackersTab';
import {SpeedTab} from './SpeedTab';

type Props = {
  open: boolean;
  detail: DetailDTO | null;
  tab: InspectorTab;
  bandwidth: BandwidthSample[];
  onTabChange: (t: InspectorTab) => void;
  onClose: () => void;
};

export function Inspector(props: Props) {
  return (
    <Show when={props.open}>
      <aside class="flex h-full w-[420px] shrink-0 flex-col border-l border-white/[.04] bg-white/[.01] backdrop-blur-sm animate-in fade-in">
        <InspectorHeader detail={props.detail} onClose={props.onClose} />
        <div class="border-b border-white/[.04] px-3 py-2">
          <InspectorTabs active={props.tab} onChange={props.onTabChange} />
        </div>
        <div class="flex-1 overflow-auto">
          <Switch>
            <Match when={props.tab === 'overview'}>
              <OverviewTab detail={props.detail} />
            </Match>
            <Match when={props.tab === 'files'}>
              <FilesTab detail={props.detail} />
            </Match>
            <Match when={props.tab === 'peers'}>
              <PeersTab detail={props.detail} />
            </Match>
            <Match when={props.tab === 'trackers'}>
              <TrackersTab detail={props.detail} />
            </Match>
            <Match when={props.tab === 'speed'}>
              <SpeedTab samples={props.bandwidth} />
            </Match>
          </Switch>
        </div>
      </aside>
    </Show>
  );
}
```

- [ ] **Step 4: Build (will fail until tab files exist; that's Tasks 12-16)**

Skip verification for now — we'll build at the end of Task 16.

- [ ] **Step 5: Commit (Inspector + Header + Tabs)**

```bash
git add frontend/src/components/inspector/Inspector.tsx \
        frontend/src/components/inspector/InspectorHeader.tsx \
        frontend/src/components/inspector/InspectorTabs.tsx
git commit -m "feat(frontend): Inspector slide-in scaffold + header + tabs"
```

---

## Task 12: OverviewTab

**Files:** `frontend/src/components/inspector/OverviewTab.tsx`

- [ ] **Step 1: Write the component**

```tsx
import {Show} from 'solid-js';
import {Copy} from 'lucide-solid';
import {toast} from 'solid-sonner';
import type {DetailDTO} from '../../lib/bindings';
import {fmtBytes, fmtPercent, fmtTimestamp} from '../../lib/format';

type Props = {detail: DetailDTO | null};

function Row(props: {label: string; children: any}) {
  return (
    <div class="flex justify-between gap-3 border-b border-white/[.03] py-2 text-xs">
      <span class="text-zinc-500">{props.label}</span>
      <span class="text-right font-mono tabular-nums text-zinc-200 break-all">{props.children}</span>
    </div>
  );
}

export function OverviewTab(props: Props) {
  return (
    <Show
      when={props.detail}
      fallback={<div class="p-4 text-xs text-zinc-500">Loading…</div>}
    >
      {(d) => (
        <div class="px-4 py-2">
          <Row label="Save path">{d().save_path}</Row>
          <Row label="Size">{fmtBytes(d().total_bytes)}</Row>
          <Row label="Done">
            {fmtBytes(d().bytes_done)} ({fmtPercent(d().progress)})
          </Row>
          <Row label="Ratio">{d().ratio.toFixed(2)}</Row>
          <Row label="Total ↓ / ↑">
            {fmtBytes(d().total_down)} / {fmtBytes(d().total_up)}
          </Row>
          <Row label="Peers / Seeds">
            {d().peers} / {d().seeds}
          </Row>
          <Row label="Added">{fmtTimestamp(d().added_at)}</Row>
          <Show when={d().completed_at}>
            <Row label="Completed">{fmtTimestamp(d().completed_at!)}</Row>
          </Show>
          <Row label="Magnet">
            <span class="inline-flex items-center gap-1.5">
              <button
                type="button"
                class="grid h-5 w-5 place-items-center rounded text-zinc-500 hover:bg-white/[.06] hover:text-zinc-200"
                onClick={() => {
                  navigator.clipboard.writeText(d().magnet);
                  toast.success('Magnet copied');
                }}
                title="Copy magnet"
              >
                <Copy class="h-3 w-3" />
              </button>
              <span class="max-w-[180px] truncate text-zinc-400">{d().magnet || '—'}</span>
            </span>
          </Row>
        </div>
      )}
    </Show>
  );
}
```

- [ ] **Step 2: Commit**

```bash
git add frontend/src/components/inspector/OverviewTab.tsx
git commit -m "feat(frontend): OverviewTab with row label/value layout + copy magnet"
```

---

## Task 13: FilesTab

**Files:** `frontend/src/components/inspector/FilesTab.tsx`

- [ ] **Step 1: Write the component**

```tsx
import {For, Show} from 'solid-js';
import type {DetailDTO} from '../../lib/bindings';
import {fmtBytes, fmtPercent} from '../../lib/format';

type Props = {detail: DetailDTO | null};

export function FilesTab(props: Props) {
  return (
    <Show
      when={props.detail?.files?.length}
      fallback={<div class="p-4 text-xs text-zinc-500">No files yet — waiting for metadata.</div>}
    >
      <div class="flex flex-col">
        <For each={props.detail!.files!}>
          {(f) => (
            <div class="border-b border-white/[.03] px-4 py-2 text-xs">
              <div class="flex items-baseline justify-between gap-2">
                <span class="truncate text-zinc-200" title={f.path}>{f.path}</span>
                <span class="shrink-0 font-mono tabular-nums text-zinc-500">{fmtBytes(f.size)}</span>
              </div>
              <div class="mt-1 flex items-center gap-2">
                <div class="relative h-1 flex-1 overflow-hidden rounded-full bg-white/[.04]">
                  <div
                    class="absolute inset-y-0 left-0 rounded-full bg-gradient-to-r from-accent-600 to-accent-400"
                    style={{width: `${(f.progress * 100).toFixed(2)}%`}}
                  />
                </div>
                <span class="font-mono tabular-nums text-zinc-500">{fmtPercent(f.progress)}</span>
              </div>
            </div>
          )}
        </For>
      </div>
    </Show>
  );
}
```

> Per-file priority dropdowns are deferred to Plan 4 (they need a backend `SetFilePriorities` IPC, which itself depends on snapshot wiring landed here).

- [ ] **Step 2: Commit**

```bash
git add frontend/src/components/inspector/FilesTab.tsx
git commit -m "feat(frontend): FilesTab with per-file mini progress bars"
```

---

## Task 14: PeersTab

**Files:** `frontend/src/components/inspector/PeersTab.tsx`

- [ ] **Step 1: Write the component**

```tsx
import {For, Show} from 'solid-js';
import type {DetailDTO} from '../../lib/bindings';
import {fmtPercent, fmtRate} from '../../lib/format';

type Props = {detail: DetailDTO | null};

export function PeersTab(props: Props) {
  return (
    <Show
      when={props.detail?.peers_list?.length}
      fallback={<div class="p-4 text-xs text-zinc-500">No connected peers.</div>}
    >
      <table class="w-full text-xs">
        <thead class="sticky top-0 bg-zinc-950/80 backdrop-blur-md text-[10px] uppercase tracking-wider text-zinc-500">
          <tr class="border-b border-white/[.04]">
            <th class="px-3 py-1.5 text-left font-medium">IP</th>
            <th class="px-2 py-1.5 text-left font-medium">Client</th>
            <th class="px-2 py-1.5 text-left font-medium">Flags</th>
            <th class="px-2 py-1.5 text-right font-medium">%</th>
            <th class="px-2 py-1.5 text-right font-medium">↓</th>
            <th class="px-3 py-1.5 text-right font-medium">↑</th>
          </tr>
        </thead>
        <tbody>
          <For each={props.detail!.peers_list!}>
            {(p) => (
              <tr class="border-b border-white/[.03] hover:bg-white/[.02]">
                <td class="px-3 py-1.5 font-mono tabular-nums text-zinc-300">{p.ip}</td>
                <td class="truncate px-2 py-1.5 text-zinc-400" style={{'max-width': '120px'}} title={p.client}>{p.client || '—'}</td>
                <td class="px-2 py-1.5 font-mono text-zinc-500">{p.flags || '—'}</td>
                <td class="px-2 py-1.5 text-right font-mono tabular-nums text-zinc-400">{fmtPercent(p.progress)}</td>
                <td class="px-2 py-1.5 text-right font-mono tabular-nums text-zinc-400">{fmtRate(p.download_rate)}</td>
                <td class="px-3 py-1.5 text-right font-mono tabular-nums text-zinc-400">{fmtRate(p.upload_rate)}</td>
              </tr>
            )}
          </For>
        </tbody>
      </table>
    </Show>
  );
}
```

- [ ] **Step 2: Commit**

```bash
git add frontend/src/components/inspector/PeersTab.tsx
git commit -m "feat(frontend): PeersTab with sticky-header live peer table"
```

---

## Task 15: TrackersTab

**Files:** `frontend/src/components/inspector/TrackersTab.tsx`

- [ ] **Step 1: Write the component**

```tsx
import {For, Show} from 'solid-js';
import type {DetailDTO} from '../../lib/bindings';
import {fmtTimestamp} from '../../lib/format';

type Props = {detail: DetailDTO | null};

export function TrackersTab(props: Props) {
  return (
    <Show
      when={props.detail?.trackers?.length}
      fallback={<div class="p-4 text-xs text-zinc-500">No trackers known.</div>}
    >
      <div class="flex flex-col">
        <For each={props.detail!.trackers!}>
          {(t) => (
            <div class="border-b border-white/[.03] px-4 py-2 text-xs">
              <div class="flex items-baseline justify-between gap-2">
                <span class="truncate font-mono text-zinc-300" title={t.url}>{t.url}</span>
                <span
                  class="shrink-0 rounded px-1.5 py-0.5 text-[10px] uppercase tracking-wider"
                  classList={{
                    'bg-seed/[.10] text-seed': t.status === 'OK',
                    'bg-zinc-700/30 text-zinc-400': t.status !== 'OK',
                  }}
                >
                  {t.status}
                </span>
              </div>
              <div class="mt-1 flex justify-between font-mono tabular-nums text-zinc-500">
                <span>Seeds {t.seeds} · Peers {t.peers}</span>
                <span>Last {fmtTimestamp(t.last_announce)}</span>
              </div>
            </div>
          )}
        </For>
      </div>
    </Show>
  );
}
```

- [ ] **Step 2: Commit**

```bash
git add frontend/src/components/inspector/TrackersTab.tsx
git commit -m "feat(frontend): TrackersTab with status badges"
```

---

## Task 16: SpeedTab + BandwidthChart (uPlot)

**Files:** `frontend/src/components/inspector/SpeedTab.tsx`, `frontend/src/components/inspector/BandwidthChart.tsx`

- [ ] **Step 1: BandwidthChart wrapper**

`frontend/src/components/inspector/BandwidthChart.tsx`:

```tsx
import {createEffect, onCleanup, onMount} from 'solid-js';
import uPlot from 'uplot';
import 'uplot/dist/uPlot.min.css';
import type {BandwidthSample} from '../../lib/store';

type Props = {samples: BandwidthSample[]; rangeSeconds: number};

export function BandwidthChart(props: Props) {
  let container: HTMLDivElement | undefined;
  let chart: uPlot | undefined;

  const buildOptions = (width: number, height: number): uPlot.Options => ({
    width,
    height,
    cursor: {show: false},
    legend: {show: false},
    axes: [
      {stroke: '#52525b', grid: {show: false}, ticks: {show: false}},
      {stroke: '#52525b', grid: {stroke: 'rgba(255,255,255,0.04)', width: 1}, ticks: {show: false}, size: 50,
       values: (_u, splits) => splits.map((v) => `${(v / 1024).toFixed(0)} KB/s`)},
    ],
    series: [
      {},
      {label: 'Down', stroke: 'oklch(0.65 0.25 290)', width: 1.5, fill: 'oklch(0.65 0.25 290 / 0.15)'},
      {label: 'Up',   stroke: '#71717a',              width: 1, fill: 'rgba(113,113,122,0.10)'},
    ],
    scales: {x: {time: true}},
  });

  const sliceForRange = () => {
    const cutoff = Date.now() / 1000 - props.rangeSeconds;
    const filtered = props.samples.filter((s) => s.t >= cutoff);
    return [
      filtered.map((s) => s.t),
      filtered.map((s) => s.down),
      filtered.map((s) => s.up),
    ] as uPlot.AlignedData;
  };

  onMount(() => {
    if (!container) return;
    const rect = container.getBoundingClientRect();
    chart = new uPlot(buildOptions(rect.width, rect.height), sliceForRange(), container);
  });

  createEffect(() => {
    if (!chart) return;
    chart.setData(sliceForRange());
  });

  onCleanup(() => chart?.destroy());

  return <div ref={container} class="h-full w-full" />;
}
```

- [ ] **Step 2: SpeedTab**

`frontend/src/components/inspector/SpeedTab.tsx`:

```tsx
import {createSignal} from 'solid-js';
import {ToggleGroup} from '@kobalte/core/toggle-group';
import type {BandwidthSample} from '../../lib/store';
import {BandwidthChart} from './BandwidthChart';

const ranges: {value: number; label: string}[] = [
  {value: 5 * 60,        label: '5m'},
  {value: 60 * 60,       label: '1h'},
  {value: 24 * 60 * 60,  label: '24h'},
];

type Props = {samples: BandwidthSample[]};

export function SpeedTab(props: Props) {
  const [range, setRange] = createSignal(5 * 60);

  return (
    <div class="flex h-full flex-col gap-3 p-4">
      <ToggleGroup
        class="inline-flex w-fit items-center gap-px rounded-md border border-white/[.06] bg-white/[.02] p-0.5"
        value={String(range())}
        onChange={(v) => v && setRange(parseInt(v, 10))}
      >
        {ranges.map((r) => (
          <ToggleGroup.Item
            value={String(r.value)}
            class="rounded px-2 py-1 text-xs text-zinc-400 transition-colors duration-100 hover:text-zinc-100 data-[pressed]:bg-white/10 data-[pressed]:text-zinc-100"
          >
            {r.label}
          </ToggleGroup.Item>
        ))}
      </ToggleGroup>
      <div class="flex-1 min-h-0">
        <BandwidthChart samples={props.samples} rangeSeconds={range()} />
      </div>
      <div class="flex items-center justify-between text-[10px] text-zinc-500">
        <span class="inline-flex items-center gap-1.5">
          <span class="h-2 w-2 rounded-full bg-down" /> Download
        </span>
        <span class="inline-flex items-center gap-1.5">
          <span class="h-2 w-2 rounded-full bg-zinc-500" /> Upload
        </span>
      </div>
    </div>
  );
}
```

- [ ] **Step 3: Verify build**

```bash
cd frontend && npm run build
```

Expected: exit 0. If uPlot's CSS import path differs from `'uplot/dist/uPlot.min.css'` in your node_modules version, fix the path; otherwise build is clean.

- [ ] **Step 4: Commit**

```bash
git add frontend/src/components/inspector/SpeedTab.tsx frontend/src/components/inspector/BandwidthChart.tsx
git commit -m "feat(frontend): SpeedTab with uPlot bandwidth chart + range toggle"
```

---

## Task 17: Wire Inspector into WindowShell + App.tsx

**Files:** `frontend/src/components/shell/WindowShell.tsx`, `frontend/src/App.tsx`

- [ ] **Step 1: Add Inspector slot to WindowShell**

Modify `WindowShell.tsx` Props to include the inspector subtree as a separate prop, then render it as a sibling of `<main>`:

```tsx
type Props = {
  // ... existing ...
  inspector?: JSX.Element;
};

export function WindowShell(props: Props) {
  return (
    <div class="flex h-full">
      <IconRail />
      <div class="flex flex-1 min-w-0 flex-col">
        <div class="flex flex-1 min-h-0">
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
          {props.inspector}
        </div>
        <StatusBar stats={props.stats} />
      </div>
    </div>
  );
}
```

- [ ] **Step 2: Add Inspector to App.tsx**

Inside `App()`, after the existing handlers, add:

```tsx
import {Inspector} from './components/inspector/Inspector';
// ...

const handleSelect = (id: string, e: MouseEvent) => {
  if (e.metaKey || e.ctrlKey) store.toggleSelect(id);
  else if (e.shiftKey) store.extendSelectTo(id);
  else {
    store.select(id);
    store.openInspector(id);
  }
};

const inspector = (
  <Inspector
    open={store.state.inspectorOpenId !== null}
    detail={store.state.inspectorDetail}
    tab={store.state.inspectorTab}
    bandwidth={store.state.bandwidthRing}
    onTabChange={(t) => store.setInspectorTab(t)}
    onClose={() => store.closeInspector()}
  />
);
```

Then pass it as `<WindowShell inspector={inspector}>` in the JSX.

Also add an `Esc` to the existing keyboard handler so it closes the inspector if open:

```tsx
} else if (e.key === 'Escape') {
  if (store.state.inspectorOpenId) store.closeInspector();
  else store.clearSelection();
}
```

- [ ] **Step 3: Verify build**

```bash
cd frontend && npm run build
```

Expected: exit 0. Bundle grows ~30 KB (uPlot).

- [ ] **Step 4: Commit**

```bash
git add frontend/src/components/shell/WindowShell.tsx frontend/src/App.tsx
git commit -m "feat(frontend): wire Inspector into WindowShell + click-row-opens behavior"
```

---

## Task 18: End-to-end smoke test (user-driven)

- [ ] **Step 1: Re-launch**

```bash
~/go/bin/wails dev -skipembedcreate
```

- [ ] **Step 2: Visual + functional check**

- Click a torrent row → inspector slides in from right with header (name/size/progress/ETA), tabs (Overview/Files/Peers/Trackers/Speed), Overview content visible
- Switch to Files → file list with progress bars (live updates if you let it run a bit)
- Switch to Peers → peer table populates as connections come in
- Switch to Trackers → tracker URL + status badge
- Switch to Speed → bandwidth chart renders with violet (down) and zinc (up); range toggle 5m/1h/24h works
- Click X (or Esc) → inspector closes; backend `inspector:tick` emission stops (you can verify by checking the dev log briefly — should drop quiet after close)
- ⌘-click multi-select stays compatible: multi-select doesn't open inspector (only single selection does)

- [ ] **Step 3: Tag**

```bash
git tag plan-3-inspector-complete
git push origin main
git push origin plan-3-inspector-complete
```

---

**End of Plan 3.**
