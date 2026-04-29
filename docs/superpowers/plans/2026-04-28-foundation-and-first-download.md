# Mosaic — Plan 1: Foundation & First Download

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Bootstrap the Mosaic project from an empty directory to a runnable Wails app where pasting a magnet link starts a download and progress updates live in the UI.

**Architecture:** Go backend wraps `anacrolix/torrent` behind a `Backend` interface. A thin `api` service layer mediates between Wails IPC handlers and the engine + SQLite persistence. SolidJS frontend subscribes to a 500ms tick event and renders a list. Everything else from the spec (full inspector, filter rail, RSS, remote HTTP, signing) is deferred to later plans.

**Tech Stack:** Go 1.22+, Wails v2, anacrolix/torrent v1.61+, modernc.org/sqlite (pure Go), pressly/goose, zerolog, adrg/xdg, SolidJS + TypeScript + Vite, Tailwind v4.

**Reference spec:** `docs/superpowers/specs/2026-04-28-bittorrent-client-design.md`

---

## File Structure (created across this plan)

```
mosaic/
├── .gitignore
├── go.mod
├── go.sum
├── main.go                          # Wails app entry, wires everything together
├── wails.json                       # Wails config (generated, edited)
├── backend/
│   ├── platform/
│   │   ├── paths.go                 # config / data / log dir resolution per OS
│   │   └── paths_test.go
│   ├── config/
│   │   ├── config.go                # Config struct + loader (defaults → file → env)
│   │   └── config_test.go
│   ├── logging/
│   │   └── logging.go               # zerolog setup, lumberjack rotation
│   ├── persistence/
│   │   ├── db.go                    # open + migrate
│   │   ├── db_test.go
│   │   ├── migrations/
│   │   │   └── 0001_initial.sql     # torrents + settings tables
│   │   ├── torrents.go              # TorrentRecord DAO
│   │   ├── torrents_test.go
│   │   ├── settings.go              # KV settings DAO
│   │   └── settings_test.go
│   ├── events/
│   │   ├── bus.go                   # typed pub/sub
│   │   └── bus_test.go
│   ├── engine/
│   │   ├── types.go                 # TorrentID, Snapshot, Event, AddRequest, Backend interface
│   │   ├── engine.go                # Engine = wrapper around a Backend; translation + restore
│   │   ├── engine_test.go           # tests using fake Backend
│   │   ├── anacrolix.go             # anacrolixBackend (production impl)
│   │   └── fake.go                  # fakeBackend (test-only, build tag `test`)
│   └── api/
│       ├── service.go               # Service: AddMagnet, Pause, Resume, Remove, List
│       └── service_test.go
└── frontend/
    ├── package.json
    ├── tailwind.config.* / index.css
    ├── src/
    │   ├── App.tsx
    │   ├── lib/
    │   │   ├── bindings.ts          # typed wrapper around Wails-generated bindings
    │   │   └── store.ts             # createTorrentStore (SolidJS reactive store)
    │   └── components/
    │       ├── TorrentList.tsx
    │       └── AddMagnetModal.tsx
    └── ...                          # Wails-generated boilerplate
```

---

## Task 1: Scaffold Wails + SolidJS project

**Files:**
- Create: project root via `wails init`

- [ ] **Step 1: Verify Wails CLI is installed**

Run: `wails version`
Expected: prints `v2.10.0` or higher. If not installed: `go install github.com/wailsapp/wails/v2/cmd/wails@latest`.

- [ ] **Step 2: Generate the Wails project in place**

Run from the project root (`/Users/dylan/Developer/mosaic`):

```bash
wails init -n mosaic -t solid-ts -d .
```

Expected: prompts may appear; accept defaults. Creates `main.go`, `app.go`, `wails.json`, and a `frontend/` directory with a SolidJS + TypeScript template.

- [ ] **Step 3: Verify `wails dev` starts**

Run: `wails dev`
Expected: a small window opens with the default SolidJS template. Close it with `Ctrl+C` after confirming.

- [ ] **Step 4: Stage and commit**

```bash
git add -A
git commit -m "chore: scaffold Wails + SolidJS project"
```

---

## Task 2: Add `.gitignore` and project-wide ignores

**Files:**
- Create: `.gitignore`

- [ ] **Step 1: Write `.gitignore`**

```gitignore
# Wails build artifacts
build/bin/
build/darwin/
build/windows/
build/linux/

# Frontend
frontend/node_modules/
frontend/dist/
frontend/wailsjs/

# Go
*.exe
*.test
*.out
vendor/

# Editor
.vscode/
.idea/
*.swp

# OS
.DS_Store
Thumbs.db

# Mosaic runtime
*.log
mosaic.db
mosaic.db-journal
mosaic.db-wal
mosaic.db-shm
```

> Note: `frontend/wailsjs/` is regenerated on every build; we never commit it.

- [ ] **Step 2: Commit**

```bash
git add .gitignore
git commit -m "chore: add .gitignore"
```

---

## Task 3: Create backend directory layout

**Files:**
- Create empty package files so the layout exists and Go is happy.

- [ ] **Step 1: Make directories**

```bash
mkdir -p backend/platform backend/config backend/logging \
         backend/persistence/migrations backend/events \
         backend/engine backend/api
```

- [ ] **Step 2: Create `doc.go` stubs for each package**

Create `backend/platform/doc.go`:
```go
// Package platform resolves OS-appropriate config, data, and log directories.
package platform
```

Create `backend/config/doc.go`:
```go
// Package config loads layered application configuration (defaults → file → env).
package config
```

Create `backend/logging/doc.go`:
```go
// Package logging configures the global zerolog logger with file rotation.
package logging
```

Create `backend/persistence/doc.go`:
```go
// Package persistence is the SQLite-backed metadata store.
package persistence
```

Create `backend/events/doc.go`:
```go
// Package events is an in-process typed pub/sub bus.
package events
```

Create `backend/engine/doc.go`:
```go
// Package engine wraps a BitTorrent backend behind a stable interface and
// translates backend events into domain events.
package engine
```

Create `backend/api/doc.go`:
```go
// Package api is the service layer that Wails handlers and (later) HTTP
// handlers both call into. All business logic lives here.
package api
```

- [ ] **Step 3: Verify `go build ./...` succeeds**

Run: `go build ./...`
Expected: exits 0 with no output.

- [ ] **Step 4: Commit**

```bash
git add backend/
git commit -m "chore: scaffold backend package layout"
```

---

## Task 4: Add Go dependencies

**Files:**
- Modify: `go.mod`, `go.sum`

- [ ] **Step 1: Add deps**

```bash
go get github.com/anacrolix/torrent@latest
go get modernc.org/sqlite@latest
go get github.com/pressly/goose/v3@latest
go get github.com/rs/zerolog@latest
go get gopkg.in/natefinch/lumberjack.v2@latest
go get github.com/adrg/xdg@latest
go get github.com/stretchr/testify@latest
go mod tidy
```

Expected: `go.mod` lists each dep. No errors.

- [ ] **Step 2: Verify build still works**

Run: `go build ./...`
Expected: exits 0.

- [ ] **Step 3: Commit**

```bash
git add go.mod go.sum
git commit -m "chore: add core Go dependencies"
```

---

## Task 5: Platform paths — failing test

**Files:**
- Test: `backend/platform/paths_test.go`

- [ ] **Step 1: Write the failing test**

Create `backend/platform/paths_test.go`:
```go
package platform

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPaths_ReturnsAppQualifiedDirs(t *testing.T) {
	p, err := Paths("Mosaic")
	require.NoError(t, err)

	require.True(t, filepath.IsAbs(p.ConfigDir), "ConfigDir should be absolute")
	require.True(t, filepath.IsAbs(p.DataDir), "DataDir should be absolute")
	require.True(t, filepath.IsAbs(p.LogDir), "LogDir should be absolute")

	// All three should include the app name as a path segment so we don't
	// pollute the user's directories.
	require.True(t, strings.Contains(p.ConfigDir, "Mosaic"))
	require.True(t, strings.Contains(p.DataDir, "Mosaic"))
	require.True(t, strings.Contains(p.LogDir, "Mosaic"))
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./backend/platform/ -run TestPaths_ReturnsAppQualifiedDirs -v`
Expected: FAIL with "undefined: Paths".

---

## Task 6: Platform paths — implementation

**Files:**
- Create: `backend/platform/paths.go`

- [ ] **Step 1: Write the implementation**

Create `backend/platform/paths.go`:
```go
package platform

import (
	"path/filepath"

	"github.com/adrg/xdg"
)

// Paths holds OS-appropriate directories for an app.
type AppPaths struct {
	ConfigDir string // user config (e.g. ~/Library/Application Support/Mosaic on macOS)
	DataDir   string // user data (db file lives here)
	LogDir    string // log files
}

// Paths returns app-qualified directories. Directories are not created; callers
// must mkdir as needed.
func Paths(app string) (AppPaths, error) {
	cfg := filepath.Join(xdg.ConfigHome, app)
	data := filepath.Join(xdg.DataHome, app)
	logs := filepath.Join(xdg.StateHome, app, "logs")
	return AppPaths{ConfigDir: cfg, DataDir: data, LogDir: logs}, nil
}
```

- [ ] **Step 2: Run test to verify it passes**

Run: `go test ./backend/platform/ -v`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add backend/platform/
git commit -m "feat(platform): app-qualified config/data/log paths"
```

---

## Task 7: Config — failing tests

**Files:**
- Test: `backend/config/config_test.go`

- [ ] **Step 1: Write failing tests**

Create `backend/config/config_test.go`:
```go
package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoad_ReturnsDefaultsWhenNoFile(t *testing.T) {
	dir := t.TempDir()
	cfg, err := Load(filepath.Join(dir, "missing.yaml"))
	require.NoError(t, err)

	require.Equal(t, 6881, cfg.ListenPort)
	require.NotEmpty(t, cfg.DefaultSavePath)
	require.True(t, cfg.EnableDHT)
	require.True(t, cfg.EnableEncryption)
}

func TestLoad_OverridesFromYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mosaic.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`
listen_port: 51413
default_save_path: /tmp/dl
enable_dht: false
enable_encryption: false
`), 0o644))

	cfg, err := Load(path)
	require.NoError(t, err)

	require.Equal(t, 51413, cfg.ListenPort)
	require.Equal(t, "/tmp/dl", cfg.DefaultSavePath)
	require.False(t, cfg.EnableDHT)
	require.False(t, cfg.EnableEncryption)
}

func TestLoad_EnvOverridesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mosaic.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`listen_port: 51413`), 0o644))

	t.Setenv("MOSAIC_LISTEN_PORT", "9999")
	cfg, err := Load(path)
	require.NoError(t, err)

	require.Equal(t, 9999, cfg.ListenPort)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./backend/config/ -v`
Expected: FAIL with "undefined: Load".

---

## Task 8: Config — implementation

**Files:**
- Create: `backend/config/config.go`

- [ ] **Step 1: Write the implementation**

Create `backend/config/config.go`:
```go
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"

	"gopkg.in/yaml.v3"
)

// Config is the merged application configuration.
type Config struct {
	ListenPort       int    `yaml:"listen_port"`
	DefaultSavePath  string `yaml:"default_save_path"`
	EnableDHT        bool   `yaml:"enable_dht"`
	EnableEncryption bool   `yaml:"enable_encryption"`
}

func defaults() Config {
	home, _ := os.UserHomeDir()
	return Config{
		ListenPort:       6881,
		DefaultSavePath:  filepath.Join(home, "Downloads"),
		EnableDHT:        true,
		EnableEncryption: true,
	}
}

// Load returns config built from defaults, then overlaid with the YAML file at
// `path` (if it exists), then overlaid with env vars (prefix MOSAIC_).
// Missing files are not an error.
func Load(path string) (Config, error) {
	cfg := defaults()

	if data, err := os.ReadFile(path); err == nil {
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			return cfg, fmt.Errorf("parse config %s: %w", path, err)
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return cfg, fmt.Errorf("read config %s: %w", path, err)
	}

	if v := os.Getenv("MOSAIC_LISTEN_PORT"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return cfg, fmt.Errorf("MOSAIC_LISTEN_PORT: %w", err)
		}
		cfg.ListenPort = n
	}
	if v := os.Getenv("MOSAIC_DEFAULT_SAVE_PATH"); v != "" {
		cfg.DefaultSavePath = v
	}
	if v := os.Getenv("MOSAIC_ENABLE_DHT"); v != "" {
		cfg.EnableDHT = v == "true" || v == "1"
	}
	if v := os.Getenv("MOSAIC_ENABLE_ENCRYPTION"); v != "" {
		cfg.EnableEncryption = v == "true" || v == "1"
	}

	return cfg, nil
}
```

- [ ] **Step 2: Add yaml dep**

```bash
go get gopkg.in/yaml.v3@latest && go mod tidy
```

- [ ] **Step 3: Run tests to verify they pass**

Run: `go test ./backend/config/ -v`
Expected: PASS (3 tests).

- [ ] **Step 4: Commit**

```bash
git add backend/config/ go.mod go.sum
git commit -m "feat(config): layered config loader (defaults → yaml → env)"
```

---

## Task 9: Logging setup

**Files:**
- Create: `backend/logging/logging.go`

> No tests for this — it's pure setup that wires zerolog to stdout + a rolling file. We verify it works by observing log output during development.

- [ ] **Step 1: Write the implementation**

Create `backend/logging/logging.go`:
```go
package logging

import (
	"io"
	"os"
	"path/filepath"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"gopkg.in/natefinch/lumberjack.v2"
)

// Init configures the global logger to write JSON to a rotating file in
// logDir/mosaic.log and pretty console output to stderr in debug mode.
// Returns a closer that flushes the file writer.
func Init(logDir string, debug bool) (io.Closer, error) {
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return nil, err
	}

	rot := &lumberjack.Logger{
		Filename:   filepath.Join(logDir, "mosaic.log"),
		MaxSize:    10, // MB
		MaxBackups: 5,
		MaxAge:     14, // days
		Compress:   true,
	}

	var writers []io.Writer = []io.Writer{rot}
	if debug {
		writers = append(writers, zerolog.ConsoleWriter{Out: os.Stderr})
	}

	zerolog.TimeFieldFormat = zerolog.TimeFormatUnixMs
	level := zerolog.InfoLevel
	if debug {
		level = zerolog.DebugLevel
	}
	log.Logger = zerolog.New(io.MultiWriter(writers...)).
		Level(level).
		With().
		Timestamp().
		Logger()

	log.Info().Str("log_dir", logDir).Msg("logging initialized")
	return rot, nil
}
```

- [ ] **Step 2: Verify build**

Run: `go build ./...`
Expected: exits 0.

- [ ] **Step 3: Commit**

```bash
git add backend/logging/
git commit -m "feat(logging): zerolog with lumberjack rotation"
```

---

## Task 10: Persistence — DB open + migrations failing test

**Files:**
- Test: `backend/persistence/db_test.go`
- Create: `backend/persistence/migrations/0001_initial.sql`

- [ ] **Step 1: Write the migration**

Create `backend/persistence/migrations/0001_initial.sql`:
```sql
-- +goose Up
CREATE TABLE torrents (
  infohash       TEXT PRIMARY KEY,
  name           TEXT NOT NULL,
  magnet         TEXT,
  save_path      TEXT NOT NULL,
  added_at       INTEGER NOT NULL,
  completed_at   INTEGER,
  paused         INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE settings (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
);

-- +goose Down
DROP TABLE settings;
DROP TABLE torrents;
```

- [ ] **Step 2: Write the failing test**

Create `backend/persistence/db_test.go`:
```go
package persistence

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOpen_RunsMigrations(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(context.Background(), filepath.Join(dir, "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	rows, err := db.SQL().Query(`SELECT name FROM sqlite_master WHERE type='table' ORDER BY name`)
	require.NoError(t, err)
	defer rows.Close()

	var names []string
	for rows.Next() {
		var n string
		require.NoError(t, rows.Scan(&n))
		names = append(names, n)
	}
	require.Contains(t, names, "torrents")
	require.Contains(t, names, "settings")
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./backend/persistence/ -run TestOpen_RunsMigrations -v`
Expected: FAIL with "undefined: Open".

---

## Task 11: Persistence — DB open + migrations implementation

**Files:**
- Create: `backend/persistence/db.go`

- [ ] **Step 1: Write the implementation**

Create `backend/persistence/db.go`:
```go
package persistence

import (
	"context"
	"database/sql"
	"embed"
	"fmt"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// DB is a thin wrapper exposing a *sql.DB to consumers and owning teardown.
type DB struct {
	db *sql.DB
}

// Open opens (or creates) the SQLite database at path and runs all pending
// goose migrations from the embedded migrations dir. WAL mode is enabled so
// concurrent reads don't block writes.
func Open(ctx context.Context, path string) (*DB, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=foreign_keys(on)&_pragma=busy_timeout(5000)", path)
	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	if err := sqlDB.PingContext(ctx); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}

	goose.SetBaseFS(migrationsFS)
	if err := goose.SetDialect("sqlite3"); err != nil {
		_ = sqlDB.Close()
		return nil, err
	}
	if err := goose.UpContext(ctx, sqlDB, "migrations"); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("run migrations: %w", err)
	}

	return &DB{db: sqlDB}, nil
}

// SQL returns the underlying *sql.DB.
func (d *DB) SQL() *sql.DB { return d.db }

// Close closes the underlying connection.
func (d *DB) Close() error { return d.db.Close() }
```

- [ ] **Step 2: Run test to verify it passes**

Run: `go test ./backend/persistence/ -v`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add backend/persistence/
git commit -m "feat(persistence): SQLite open + goose migrations"
```

---

## Task 12: Torrent DAO — failing tests

**Files:**
- Test: `backend/persistence/torrents_test.go`

- [ ] **Step 1: Write failing tests**

Create `backend/persistence/torrents_test.go`:
```go
package persistence

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func newTestDB(t *testing.T) *DB {
	t.Helper()
	db, err := Open(context.Background(), filepath.Join(t.TempDir(), "t.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestTorrents_SaveAndGet(t *testing.T) {
	db := newTestDB(t)
	tr := NewTorrents(db)
	ctx := context.Background()

	rec := TorrentRecord{
		InfoHash:  "abc123",
		Name:      "ubuntu-24.04.iso",
		Magnet:    "magnet:?xt=urn:btih:abc123",
		SavePath:  "/tmp/dl",
		AddedAt:   time.Unix(1700000000, 0),
	}
	require.NoError(t, tr.Save(ctx, rec))

	got, err := tr.Get(ctx, "abc123")
	require.NoError(t, err)
	require.Equal(t, rec.Name, got.Name)
	require.Equal(t, rec.SavePath, got.SavePath)
	require.Equal(t, rec.AddedAt.Unix(), got.AddedAt.Unix())
}

func TestTorrents_List_ReturnsAll(t *testing.T) {
	db := newTestDB(t)
	tr := NewTorrents(db)
	ctx := context.Background()

	require.NoError(t, tr.Save(ctx, TorrentRecord{InfoHash: "h1", Name: "a", SavePath: "/p", AddedAt: time.Now()}))
	require.NoError(t, tr.Save(ctx, TorrentRecord{InfoHash: "h2", Name: "b", SavePath: "/p", AddedAt: time.Now()}))

	all, err := tr.List(ctx)
	require.NoError(t, err)
	require.Len(t, all, 2)
}

func TestTorrents_Remove(t *testing.T) {
	db := newTestDB(t)
	tr := NewTorrents(db)
	ctx := context.Background()

	require.NoError(t, tr.Save(ctx, TorrentRecord{InfoHash: "h1", Name: "a", SavePath: "/p", AddedAt: time.Now()}))
	require.NoError(t, tr.Remove(ctx, "h1"))

	all, err := tr.List(ctx)
	require.NoError(t, err)
	require.Empty(t, all)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./backend/persistence/ -run TestTorrents -v`
Expected: FAIL with "undefined: NewTorrents" / "undefined: TorrentRecord".

---

## Task 13: Torrent DAO — implementation

**Files:**
- Create: `backend/persistence/torrents.go`

- [ ] **Step 1: Write the implementation**

Create `backend/persistence/torrents.go`:
```go
package persistence

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// TorrentRecord is the persisted metadata for a single torrent.
type TorrentRecord struct {
	InfoHash    string
	Name        string
	Magnet      string
	SavePath    string
	AddedAt     time.Time
	CompletedAt *time.Time
	Paused      bool
}

// Torrents is the DAO for the torrents table.
type Torrents struct{ db *DB }

func NewTorrents(db *DB) *Torrents { return &Torrents{db: db} }

// ErrNotFound is returned when a record does not exist.
var ErrNotFound = errors.New("not found")

// Save inserts or updates a torrent record.
func (t *Torrents) Save(ctx context.Context, r TorrentRecord) error {
	var completed sql.NullInt64
	if r.CompletedAt != nil {
		completed = sql.NullInt64{Int64: r.CompletedAt.Unix(), Valid: true}
	}
	paused := 0
	if r.Paused {
		paused = 1
	}
	_, err := t.db.SQL().ExecContext(ctx, `
INSERT INTO torrents (infohash, name, magnet, save_path, added_at, completed_at, paused)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(infohash) DO UPDATE SET
  name = excluded.name,
  magnet = excluded.magnet,
  save_path = excluded.save_path,
  added_at = excluded.added_at,
  completed_at = excluded.completed_at,
  paused = excluded.paused
`, r.InfoHash, r.Name, r.Magnet, r.SavePath, r.AddedAt.Unix(), completed, paused)
	return err
}

// Get returns a single record by infohash.
func (t *Torrents) Get(ctx context.Context, infohash string) (TorrentRecord, error) {
	row := t.db.SQL().QueryRowContext(ctx, `
SELECT infohash, name, COALESCE(magnet, ''), save_path, added_at, completed_at, paused
FROM torrents WHERE infohash = ?`, infohash)
	return scanTorrent(row)
}

// List returns all records ordered by added_at descending.
func (t *Torrents) List(ctx context.Context) ([]TorrentRecord, error) {
	rows, err := t.db.SQL().QueryContext(ctx, `
SELECT infohash, name, COALESCE(magnet, ''), save_path, added_at, completed_at, paused
FROM torrents ORDER BY added_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TorrentRecord
	for rows.Next() {
		r, err := scanTorrent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// Remove deletes by infohash. Missing rows are not an error.
func (t *Torrents) Remove(ctx context.Context, infohash string) error {
	_, err := t.db.SQL().ExecContext(ctx, `DELETE FROM torrents WHERE infohash = ?`, infohash)
	return err
}

type scanner interface {
	Scan(dest ...any) error
}

func scanTorrent(s scanner) (TorrentRecord, error) {
	var r TorrentRecord
	var addedAt int64
	var completedAt sql.NullInt64
	var paused int
	if err := s.Scan(&r.InfoHash, &r.Name, &r.Magnet, &r.SavePath, &addedAt, &completedAt, &paused); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return r, ErrNotFound
		}
		return r, err
	}
	r.AddedAt = time.Unix(addedAt, 0)
	if completedAt.Valid {
		t := time.Unix(completedAt.Int64, 0)
		r.CompletedAt = &t
	}
	r.Paused = paused == 1
	return r, nil
}
```

- [ ] **Step 2: Run tests to verify they pass**

Run: `go test ./backend/persistence/ -v`
Expected: PASS (4 tests).

- [ ] **Step 3: Commit**

```bash
git add backend/persistence/
git commit -m "feat(persistence): Torrents DAO (Save/Get/List/Remove)"
```

---

## Task 14: Settings DAO

**Files:**
- Test: `backend/persistence/settings_test.go`
- Create: `backend/persistence/settings.go`

- [ ] **Step 1: Write the failing test**

Create `backend/persistence/settings_test.go`:
```go
package persistence

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSettings_GetSet(t *testing.T) {
	db := newTestDB(t)
	s := NewSettings(db)
	ctx := context.Background()

	_, err := s.Get(ctx, "missing")
	require.ErrorIs(t, err, ErrNotFound)

	require.NoError(t, s.Set(ctx, "alt_speed", "true"))
	v, err := s.Get(ctx, "alt_speed")
	require.NoError(t, err)
	require.Equal(t, "true", v)

	require.NoError(t, s.Set(ctx, "alt_speed", "false"))
	v, err = s.Get(ctx, "alt_speed")
	require.NoError(t, err)
	require.Equal(t, "false", v)
}
```

- [ ] **Step 2: Verify it fails**

Run: `go test ./backend/persistence/ -run TestSettings -v`
Expected: FAIL with "undefined: NewSettings".

- [ ] **Step 3: Write implementation**

Create `backend/persistence/settings.go`:
```go
package persistence

import (
	"context"
	"database/sql"
	"errors"
)

// Settings is a key/value DAO over the settings table.
type Settings struct{ db *DB }

func NewSettings(db *DB) *Settings { return &Settings{db: db} }

func (s *Settings) Get(ctx context.Context, key string) (string, error) {
	var v string
	err := s.db.SQL().QueryRowContext(ctx,
		`SELECT value FROM settings WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return v, err
}

func (s *Settings) Set(ctx context.Context, key, value string) error {
	_, err := s.db.SQL().ExecContext(ctx, `
INSERT INTO settings (key, value) VALUES (?, ?)
ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}
```

- [ ] **Step 4: Verify it passes**

Run: `go test ./backend/persistence/ -v`
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/persistence/
git commit -m "feat(persistence): Settings KV DAO"
```

---

## Task 15: Event bus — failing test

**Files:**
- Test: `backend/events/bus_test.go`

- [ ] **Step 1: Write the failing test**

Create `backend/events/bus_test.go`:
```go
package events

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type sample struct{ N int }

func TestBus_FanOutsToAllSubscribers(t *testing.T) {
	bus := NewBus[sample](16)
	t.Cleanup(bus.Close)

	a := bus.Subscribe()
	b := bus.Subscribe()

	var wg sync.WaitGroup
	wg.Add(2)

	got := func(ch <-chan sample, dst *[]int) {
		defer wg.Done()
		timeout := time.After(time.Second)
		for {
			select {
			case ev := <-ch:
				*dst = append(*dst, ev.N)
				if len(*dst) == 3 {
					return
				}
			case <-timeout:
				return
			}
		}
	}

	var av, bv []int
	go got(a, &av)
	go got(b, &bv)

	bus.Publish(sample{N: 1})
	bus.Publish(sample{N: 2})
	bus.Publish(sample{N: 3})
	wg.Wait()

	require.Equal(t, []int{1, 2, 3}, av)
	require.Equal(t, []int{1, 2, 3}, bv)
}

func TestBus_DropsWhenSubscriberSlow(t *testing.T) {
	bus := NewBus[sample](2)
	t.Cleanup(bus.Close)

	_ = bus.Subscribe() // never reads

	for i := 0; i < 100; i++ {
		bus.Publish(sample{N: i})
	}
	// no panic, no deadlock — bus drops on full buffer
}
```

- [ ] **Step 2: Verify it fails**

Run: `go test ./backend/events/ -v`
Expected: FAIL with "undefined: NewBus".

---

## Task 16: Event bus — implementation

**Files:**
- Create: `backend/events/bus.go`

- [ ] **Step 1: Write the implementation**

Create `backend/events/bus.go`:
```go
package events

import "sync"

// Bus is a typed in-process pub/sub bus. Subscribers each get their own
// buffered channel of size `buf`; if a subscriber's buffer is full, the
// publish is dropped *for that subscriber* (others are not affected).
type Bus[T any] struct {
	mu   sync.RWMutex
	buf  int
	subs []chan T
	done chan struct{}
}

func NewBus[T any](buf int) *Bus[T] {
	return &Bus[T]{buf: buf, done: make(chan struct{})}
}

func (b *Bus[T]) Subscribe() <-chan T {
	ch := make(chan T, b.buf)
	b.mu.Lock()
	b.subs = append(b.subs, ch)
	b.mu.Unlock()
	return ch
}

func (b *Bus[T]) Publish(v T) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for _, ch := range b.subs {
		select {
		case ch <- v:
		default: // drop on full
		}
	}
}

func (b *Bus[T]) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	select {
	case <-b.done:
		return
	default:
	}
	close(b.done)
	for _, ch := range b.subs {
		close(ch)
	}
	b.subs = nil
}
```

- [ ] **Step 2: Run tests to verify they pass**

Run: `go test ./backend/events/ -v`
Expected: PASS (2 tests).

- [ ] **Step 3: Commit**

```bash
git add backend/events/
git commit -m "feat(events): typed in-process pub/sub bus"
```

---

## Task 17: Engine types and Backend interface

**Files:**
- Create: `backend/engine/types.go`

> No tests yet — these are pure type definitions consumed by later tasks.

- [ ] **Step 1: Write the file**

Create `backend/engine/types.go`:
```go
package engine

import (
	"context"
	"time"
)

// TorrentID identifies a torrent in the engine. It's the hex-encoded infohash.
type TorrentID string

// AddRequest holds the inputs needed to add a torrent (file path or magnet).
type AddRequest struct {
	Magnet     string // one of Magnet or TorrentFile
	TorrentFile []byte
	SavePath   string
	Paused     bool
}

// Snapshot is a point-in-time view of a torrent's state, suitable for the UI.
type Snapshot struct {
	ID           TorrentID
	Name         string
	Magnet       string
	SavePath     string
	TotalBytes   int64
	BytesDone    int64
	DownloadRate int64 // bytes/sec
	UploadRate   int64
	Peers        int
	Seeds        int
	Paused       bool
	Completed    bool
	AddedAt      time.Time
}

// EventKind enumerates the kinds of EngineEvent.
type EventKind int

const (
	EventAdded EventKind = iota + 1
	EventRemoved
	EventTick    // periodic state update
	EventComplete
	EventError
)

type EngineEvent struct {
	Kind     EventKind
	ID       TorrentID
	Snapshot Snapshot // populated for Added/Tick/Complete
	Err      error    // populated for Error
}

// Backend is the minimal interface the engine wrapper needs from a torrent
// library. The production implementation wraps anacrolix/torrent; tests use a
// fake.
type Backend interface {
	AddMagnet(ctx context.Context, magnet, savePath string) (TorrentID, error)
	AddFile(ctx context.Context, blob []byte, savePath string) (TorrentID, error)
	Pause(id TorrentID) error
	Resume(id TorrentID) error
	Remove(id TorrentID, deleteFiles bool) error
	List() []Snapshot
	Snapshot(id TorrentID) (Snapshot, error)
	Close() error
}
```

- [ ] **Step 2: Verify build**

Run: `go build ./backend/engine/`
Expected: exits 0.

- [ ] **Step 3: Commit**

```bash
git add backend/engine/
git commit -m "feat(engine): domain types and Backend interface"
```

---

## Task 18: Fake Backend (for tests)

**Files:**
- Create: `backend/engine/fake.go`

> The fake is a normal package file (not test-only) because the api/service tests need it too.

- [ ] **Step 1: Write the fake**

Create `backend/engine/fake.go`:
```go
package engine

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"sync"
	"time"
)

// FakeBackend is an in-memory Backend for tests.
type FakeBackend struct {
	mu        sync.Mutex
	torrents  map[TorrentID]*Snapshot
}

func NewFakeBackend() *FakeBackend {
	return &FakeBackend{torrents: make(map[TorrentID]*Snapshot)}
}

func (f *FakeBackend) AddMagnet(_ context.Context, magnet, savePath string) (TorrentID, error) {
	id := hashOf(magnet)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.torrents[id] = &Snapshot{
		ID:         id,
		Name:       "fake-" + string(id[:8]),
		Magnet:     magnet,
		SavePath:   savePath,
		TotalBytes: 1 << 30, // 1 GB placeholder
		AddedAt:    time.Now(),
	}
	return id, nil
}

func (f *FakeBackend) AddFile(_ context.Context, blob []byte, savePath string) (TorrentID, error) {
	id := hashOf(string(blob))
	f.mu.Lock()
	defer f.mu.Unlock()
	f.torrents[id] = &Snapshot{
		ID: id, Name: "fake-file", SavePath: savePath, AddedAt: time.Now(),
	}
	return id, nil
}

func (f *FakeBackend) Pause(id TorrentID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.torrents[id]
	if !ok {
		return errors.New("not found")
	}
	t.Paused = true
	return nil
}

func (f *FakeBackend) Resume(id TorrentID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.torrents[id]
	if !ok {
		return errors.New("not found")
	}
	t.Paused = false
	return nil
}

func (f *FakeBackend) Remove(id TorrentID, _ bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.torrents[id]; !ok {
		return errors.New("not found")
	}
	delete(f.torrents, id)
	return nil
}

func (f *FakeBackend) List() []Snapshot {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]Snapshot, 0, len(f.torrents))
	for _, t := range f.torrents {
		out = append(out, *t)
	}
	return out
}

func (f *FakeBackend) Snapshot(id TorrentID) (Snapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.torrents[id]
	if !ok {
		return Snapshot{}, errors.New("not found")
	}
	return *t, nil
}

func (f *FakeBackend) Close() error { return nil }

// AdvanceProgress is a test helper: bumps BytesDone for a torrent.
func (f *FakeBackend) AdvanceProgress(id TorrentID, by int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if t, ok := f.torrents[id]; ok {
		t.BytesDone += by
		if t.BytesDone >= t.TotalBytes {
			t.Completed = true
		}
	}
}

func hashOf(s string) TorrentID {
	sum := sha1.Sum([]byte(s))
	return TorrentID(hex.EncodeToString(sum[:]))
}
```

- [ ] **Step 2: Verify build**

Run: `go build ./backend/engine/`
Expected: exits 0.

- [ ] **Step 3: Commit**

```bash
git add backend/engine/fake.go
git commit -m "feat(engine): in-memory FakeBackend for tests"
```

---

## Task 19: Engine wrapper — failing test

**Files:**
- Test: `backend/engine/engine_test.go`

- [ ] **Step 1: Write failing tests**

Create `backend/engine/engine_test.go`:
```go
package engine

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestEngine_AddMagnet_EmitsAddedEvent(t *testing.T) {
	fb := NewFakeBackend()
	eng := NewEngine(fb, 50*time.Millisecond)
	t.Cleanup(func() { _ = eng.Close() })

	sub := eng.Subscribe()

	id, err := eng.AddMagnet(context.Background(), "magnet:?xt=urn:btih:abc", "/tmp")
	require.NoError(t, err)
	require.NotEmpty(t, id)

	select {
	case ev := <-sub:
		require.Equal(t, EventAdded, ev.Kind)
		require.Equal(t, id, ev.ID)
	case <-time.After(time.Second):
		t.Fatal("expected EventAdded within 1s")
	}
}

func TestEngine_Tick_EmitsForActiveTorrents(t *testing.T) {
	fb := NewFakeBackend()
	eng := NewEngine(fb, 30*time.Millisecond)
	t.Cleanup(func() { _ = eng.Close() })

	sub := eng.Subscribe()
	id, err := eng.AddMagnet(context.Background(), "magnet:?xt=urn:btih:xyz", "/tmp")
	require.NoError(t, err)

	// drain Added
	<-sub

	fb.AdvanceProgress(id, 1024)

	deadline := time.After(time.Second)
	for {
		select {
		case ev := <-sub:
			if ev.Kind == EventTick && ev.ID == id {
				return // success
			}
		case <-deadline:
			t.Fatal("expected EventTick within 1s")
		}
	}
}

func TestEngine_Remove_EmitsRemovedEvent(t *testing.T) {
	fb := NewFakeBackend()
	eng := NewEngine(fb, 50*time.Millisecond)
	t.Cleanup(func() { _ = eng.Close() })

	id, err := eng.AddMagnet(context.Background(), "magnet:?xt=urn:btih:rm", "/tmp")
	require.NoError(t, err)

	sub := eng.Subscribe()
	require.NoError(t, eng.Remove(id, false))

	deadline := time.After(time.Second)
	for {
		select {
		case ev := <-sub:
			if ev.Kind == EventRemoved && ev.ID == id {
				return
			}
		case <-deadline:
			t.Fatal("expected EventRemoved within 1s")
		}
	}
}
```

- [ ] **Step 2: Verify they fail**

Run: `go test ./backend/engine/ -v`
Expected: FAIL with "undefined: NewEngine".

---

## Task 20: Engine wrapper — implementation

**Files:**
- Create: `backend/engine/engine.go`

- [ ] **Step 1: Write the implementation**

Create `backend/engine/engine.go`:
```go
package engine

import (
	"context"
	"sync"
	"time"
)

// Engine wraps a Backend and emits a stream of EngineEvent via Subscribe.
type Engine struct {
	backend  Backend
	tickRate time.Duration

	mu     sync.RWMutex
	subs   []chan EngineEvent
	stop   chan struct{}
	closed bool
}

// NewEngine returns an Engine that polls Backend.List() every tickRate and
// emits EventTick for each torrent. The caller must call Close().
func NewEngine(b Backend, tickRate time.Duration) *Engine {
	e := &Engine{
		backend:  b,
		tickRate: tickRate,
		stop:     make(chan struct{}),
	}
	go e.run()
	return e
}

func (e *Engine) Subscribe() <-chan EngineEvent {
	ch := make(chan EngineEvent, 64)
	e.mu.Lock()
	e.subs = append(e.subs, ch)
	e.mu.Unlock()
	return ch
}

func (e *Engine) emit(ev EngineEvent) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	for _, ch := range e.subs {
		select {
		case ch <- ev:
		default:
		}
	}
}

func (e *Engine) AddMagnet(ctx context.Context, magnet, savePath string) (TorrentID, error) {
	id, err := e.backend.AddMagnet(ctx, magnet, savePath)
	if err != nil {
		return "", err
	}
	snap, err := e.backend.Snapshot(id)
	if err != nil {
		return "", err
	}
	e.emit(EngineEvent{Kind: EventAdded, ID: id, Snapshot: snap})
	return id, nil
}

func (e *Engine) AddFile(ctx context.Context, blob []byte, savePath string) (TorrentID, error) {
	id, err := e.backend.AddFile(ctx, blob, savePath)
	if err != nil {
		return "", err
	}
	snap, err := e.backend.Snapshot(id)
	if err != nil {
		return "", err
	}
	e.emit(EngineEvent{Kind: EventAdded, ID: id, Snapshot: snap})
	return id, nil
}

func (e *Engine) Pause(id TorrentID) error  { return e.backend.Pause(id) }
func (e *Engine) Resume(id TorrentID) error { return e.backend.Resume(id) }

func (e *Engine) Remove(id TorrentID, deleteFiles bool) error {
	if err := e.backend.Remove(id, deleteFiles); err != nil {
		return err
	}
	e.emit(EngineEvent{Kind: EventRemoved, ID: id})
	return nil
}

func (e *Engine) List() []Snapshot { return e.backend.List() }

func (e *Engine) Snapshot(id TorrentID) (Snapshot, error) { return e.backend.Snapshot(id) }

func (e *Engine) Close() error {
	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		return nil
	}
	e.closed = true
	close(e.stop)
	for _, ch := range e.subs {
		close(ch)
	}
	e.subs = nil
	e.mu.Unlock()
	return e.backend.Close()
}

func (e *Engine) run() {
	t := time.NewTicker(e.tickRate)
	defer t.Stop()
	for {
		select {
		case <-e.stop:
			return
		case <-t.C:
			for _, snap := range e.backend.List() {
				kind := EventTick
				if snap.Completed {
					kind = EventComplete
				}
				e.emit(EngineEvent{Kind: kind, ID: snap.ID, Snapshot: snap})
			}
		}
	}
}
```

- [ ] **Step 2: Run tests**

Run: `go test ./backend/engine/ -v -race`
Expected: PASS (3 tests).

- [ ] **Step 3: Commit**

```bash
git add backend/engine/engine.go backend/engine/engine_test.go
git commit -m "feat(engine): wrapper with subscribe + periodic tick"
```

---

## Task 21: anacrolix Backend implementation

**Files:**
- Create: `backend/engine/anacrolix.go`

> This wraps anacrolix/torrent and is hard to unit-test in isolation; we rely on the manual smoke test at the end of the plan and on integration tests added in later plans.

- [ ] **Step 1: Write the implementation**

Create `backend/engine/anacrolix.go`:
```go
package engine

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"
)

// AnacrolixConfig configures the production Backend.
type AnacrolixConfig struct {
	DataDir          string // engine state dir (peer cache, etc.)
	ListenPort       int
	EnableDHT        bool
	EnableEncryption bool
}

// AnacrolixBackend implements Backend on top of anacrolix/torrent.
type AnacrolixBackend struct {
	client *torrent.Client

	mu       sync.Mutex
	bySaveTo map[TorrentID]string // id → save path (we set it per-torrent)
}

// NewAnacrolixBackend opens a torrent.Client with our config.
func NewAnacrolixBackend(cfg AnacrolixConfig) (*AnacrolixBackend, error) {
	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		return nil, err
	}
	tcfg := torrent.NewDefaultClientConfig()
	tcfg.DataDir = cfg.DataDir
	tcfg.ListenPort = cfg.ListenPort
	tcfg.NoDHT = !cfg.EnableDHT
	if cfg.EnableEncryption {
		tcfg.HeaderObfuscationPolicy.Preferred = true
		tcfg.HeaderObfuscationPolicy.RequirePreferred = false
	} else {
		tcfg.HeaderObfuscationPolicy.Preferred = false
	}
	tcfg.DefaultStorage = storage.NewFile(cfg.DataDir)

	c, err := torrent.NewClient(tcfg)
	if err != nil {
		return nil, fmt.Errorf("anacrolix client: %w", err)
	}
	return &AnacrolixBackend{client: c, bySaveTo: make(map[TorrentID]string)}, nil
}

func idFor(t *torrent.Torrent) TorrentID {
	return TorrentID(t.InfoHash().HexString())
}

func (a *AnacrolixBackend) AddMagnet(ctx context.Context, magnet, savePath string) (TorrentID, error) {
	if err := os.MkdirAll(savePath, 0o755); err != nil {
		return "", err
	}
	t, err := a.client.AddMagnet(magnet)
	if err != nil {
		return "", err
	}
	id := idFor(t)
	a.mu.Lock()
	a.bySaveTo[id] = savePath
	a.mu.Unlock()
	go func() {
		select {
		case <-t.GotInfo():
			t.DownloadAll()
		case <-ctx.Done():
		}
	}()
	return id, nil
}

func (a *AnacrolixBackend) AddFile(ctx context.Context, blob []byte, savePath string) (TorrentID, error) {
	if err := os.MkdirAll(savePath, 0o755); err != nil {
		return "", err
	}
	mi, err := metainfo.Load(bytes.NewReader(blob))
	if err != nil {
		return "", err
	}
	t, err := a.client.AddTorrent(mi)
	if err != nil {
		return "", err
	}
	id := idFor(t)
	a.mu.Lock()
	a.bySaveTo[id] = savePath
	a.mu.Unlock()
	t.DownloadAll()
	return id, nil
}

func (a *AnacrolixBackend) Pause(id TorrentID) error {
	t, ok := a.find(id)
	if !ok {
		return errors.New("not found")
	}
	t.SetMaxEstablishedConns(0)
	return nil
}

func (a *AnacrolixBackend) Resume(id TorrentID) error {
	t, ok := a.find(id)
	if !ok {
		return errors.New("not found")
	}
	t.SetMaxEstablishedConns(80)
	return nil
}

func (a *AnacrolixBackend) Remove(id TorrentID, deleteFiles bool) error {
	t, ok := a.find(id)
	if !ok {
		return errors.New("not found")
	}
	a.mu.Lock()
	saveTo := a.bySaveTo[id]
	delete(a.bySaveTo, id)
	a.mu.Unlock()
	t.Drop()
	if deleteFiles && saveTo != "" {
		if info := t.Info(); info != nil {
			_ = os.RemoveAll(filepath.Join(saveTo, info.Name))
		}
	}
	return nil
}

func (a *AnacrolixBackend) List() []Snapshot {
	ts := a.client.Torrents()
	out := make([]Snapshot, 0, len(ts))
	for _, t := range ts {
		out = append(out, snapshotFor(t))
	}
	return out
}

func (a *AnacrolixBackend) Snapshot(id TorrentID) (Snapshot, error) {
	t, ok := a.find(id)
	if !ok {
		return Snapshot{}, errors.New("not found")
	}
	return snapshotFor(t), nil
}

func (a *AnacrolixBackend) Close() error {
	errs := a.client.Close()
	if len(errs) > 0 {
		return errs[0]
	}
	return nil
}

func (a *AnacrolixBackend) find(id TorrentID) (*torrent.Torrent, bool) {
	for _, t := range a.client.Torrents() {
		if idFor(t) == id {
			return t, true
		}
	}
	return nil, false
}

func snapshotFor(t *torrent.Torrent) Snapshot {
	stats := t.Stats()
	name := t.Name()
	if name == "" {
		name = t.InfoHash().HexString()
	}
	total := int64(0)
	if info := t.Info(); info != nil {
		total = info.TotalLength()
	}
	return Snapshot{
		ID:           TorrentID(t.InfoHash().HexString()),
		Name:         name,
		TotalBytes:   total,
		BytesDone:    t.BytesCompleted(),
		DownloadRate: int64(stats.BytesReadData.Int64()), // cumulative; UI computes rate
		UploadRate:   int64(stats.BytesWrittenData.Int64()),
		Peers:        stats.ActivePeers,
		Seeds:        stats.ConnectedSeeders,
		Paused:       false, // TODO in plan 2: distinguish via SetMaxEstablishedConns(0)
		Completed:    total > 0 && t.BytesCompleted() == total,
		AddedAt:      time.Now(), // engine wrapper does not track AddedAt; persistence does
	}
}

```

- [ ] **Step 2: Verify build**

Run: `go build ./backend/engine/`
Expected: exits 0.

- [ ] **Step 3: Run all engine tests**

Run: `go test ./backend/engine/ -v -race`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add backend/engine/anacrolix.go
git commit -m "feat(engine): anacrolix/torrent backend"
```

---

## Task 22: API service — failing tests

**Files:**
- Test: `backend/api/service_test.go`

- [ ] **Step 1: Write failing tests**

Create `backend/api/service_test.go`:
```go
package api

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"mosaic/backend/engine"
	"mosaic/backend/persistence"
)

func newTestService(t *testing.T) (*Service, *engine.FakeBackend) {
	t.Helper()
	db, err := persistence.Open(context.Background(), filepath.Join(t.TempDir(), "t.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	fb := engine.NewFakeBackend()
	eng := engine.NewEngine(fb, 50*time.Millisecond)
	t.Cleanup(func() { _ = eng.Close() })

	svc := NewService(eng, persistence.NewTorrents(db), "/tmp/dl")
	return svc, fb
}

func TestService_AddMagnet_PersistsAndAddsToEngine(t *testing.T) {
	svc, fb := newTestService(t)

	id, err := svc.AddMagnet(context.Background(), "magnet:?xt=urn:btih:abc", "")
	require.NoError(t, err)
	require.NotEmpty(t, id)

	// engine sees it
	require.Len(t, fb.List(), 1)

	// persistence sees it
	rows, err := svc.ListTorrents(context.Background())
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, string(id), rows[0].ID)
}

func TestService_AddMagnet_UsesDefaultSavePathWhenEmpty(t *testing.T) {
	svc, _ := newTestService(t)
	_, err := svc.AddMagnet(context.Background(), "magnet:?xt=urn:btih:def", "")
	require.NoError(t, err)

	rows, err := svc.ListTorrents(context.Background())
	require.NoError(t, err)
	require.Equal(t, "/tmp/dl", rows[0].SavePath)
}

func TestService_Remove_RemovesFromEngineAndPersistence(t *testing.T) {
	svc, fb := newTestService(t)
	id, _ := svc.AddMagnet(context.Background(), "magnet:?xt=urn:btih:rm", "/tmp")
	require.NoError(t, svc.Remove(context.Background(), id, false))

	require.Len(t, fb.List(), 0)
	rows, _ := svc.ListTorrents(context.Background())
	require.Empty(t, rows)
}
```

- [ ] **Step 2: Verify they fail**

Run: `go test ./backend/api/ -v`
Expected: FAIL with "undefined: NewService" / "undefined: Service".

> Note: `module mosaic` should already be set in `go.mod` from `wails init`. If imports don't resolve, check `head go.mod` and confirm the module path is `mosaic`. If it's something else (like `changeme`), update it: `go mod edit -module mosaic && go mod tidy`.

---

## Task 23: API service — implementation

**Files:**
- Create: `backend/api/service.go`

- [ ] **Step 1: Write the implementation**

Create `backend/api/service.go`:
```go
package api

import (
	"context"
	"fmt"
	"time"

	"mosaic/backend/engine"
	"mosaic/backend/persistence"
)

// Service is the only place business logic lives. Wails handlers and (later)
// HTTP handlers are thin adapters that translate transport shapes into Service
// calls.
type Service struct {
	engine          *engine.Engine
	torrents        *persistence.Torrents
	defaultSavePath string
}

func NewService(eng *engine.Engine, torrents *persistence.Torrents, defaultSavePath string) *Service {
	return &Service{engine: eng, torrents: torrents, defaultSavePath: defaultSavePath}
}

// TorrentDTO is the shape returned to UI/transport callers.
type TorrentDTO struct {
	ID           string  `json:"id"`
	Name         string  `json:"name"`
	Magnet       string  `json:"magnet"`
	SavePath     string  `json:"save_path"`
	TotalBytes   int64   `json:"total_bytes"`
	BytesDone    int64   `json:"bytes_done"`
	Progress     float64 `json:"progress"` // 0..1
	DownloadRate int64   `json:"download_rate"`
	UploadRate   int64   `json:"upload_rate"`
	Peers        int     `json:"peers"`
	Seeds        int     `json:"seeds"`
	Paused       bool    `json:"paused"`
	Completed    bool    `json:"completed"`
	AddedAt      int64   `json:"added_at"` // unix seconds
}

func toDTO(s engine.Snapshot, addedAt time.Time) TorrentDTO {
	prog := 0.0
	if s.TotalBytes > 0 {
		prog = float64(s.BytesDone) / float64(s.TotalBytes)
	}
	return TorrentDTO{
		ID:           string(s.ID),
		Name:         s.Name,
		Magnet:       s.Magnet,
		SavePath:     s.SavePath,
		TotalBytes:   s.TotalBytes,
		BytesDone:    s.BytesDone,
		Progress:     prog,
		DownloadRate: s.DownloadRate,
		UploadRate:   s.UploadRate,
		Peers:        s.Peers,
		Seeds:        s.Seeds,
		Paused:       s.Paused,
		Completed:    s.Completed,
		AddedAt:      addedAt.Unix(),
	}
}

func (s *Service) AddMagnet(ctx context.Context, magnet, savePath string) (engine.TorrentID, error) {
	if savePath == "" {
		savePath = s.defaultSavePath
	}
	id, err := s.engine.AddMagnet(ctx, magnet, savePath)
	if err != nil {
		return "", fmt.Errorf("add magnet: %w", err)
	}
	snap, err := s.engine.Snapshot(id)
	if err != nil {
		return "", err
	}
	if err := s.torrents.Save(ctx, persistence.TorrentRecord{
		InfoHash: string(id),
		Name:     snap.Name,
		Magnet:   magnet,
		SavePath: savePath,
		AddedAt:  time.Now(),
	}); err != nil {
		return "", fmt.Errorf("persist: %w", err)
	}
	return id, nil
}

func (s *Service) Pause(id engine.TorrentID) error  { return s.engine.Pause(id) }
func (s *Service) Resume(id engine.TorrentID) error { return s.engine.Resume(id) }

func (s *Service) Remove(ctx context.Context, id engine.TorrentID, deleteFiles bool) error {
	if err := s.engine.Remove(id, deleteFiles); err != nil {
		return err
	}
	return s.torrents.Remove(ctx, string(id))
}

func (s *Service) ListTorrents(ctx context.Context) ([]TorrentDTO, error) {
	records, err := s.torrents.List(ctx)
	if err != nil {
		return nil, err
	}
	byHash := make(map[string]persistence.TorrentRecord, len(records))
	for _, r := range records {
		byHash[r.InfoHash] = r
	}
	snaps := s.engine.List()
	out := make([]TorrentDTO, 0, len(snaps))
	for _, snap := range snaps {
		rec, ok := byHash[string(snap.ID)]
		addedAt := time.Now()
		if ok {
			snap.SavePath = rec.SavePath
			if snap.Magnet == "" {
				snap.Magnet = rec.Magnet
			}
			addedAt = rec.AddedAt
		}
		out = append(out, toDTO(snap, addedAt))
	}
	return out, nil
}
```

- [ ] **Step 2: Run tests**

Run: `go test ./backend/api/ -v -race`
Expected: PASS (3 tests).

- [ ] **Step 3: Commit**

```bash
git add backend/api/
git commit -m "feat(api): service layer with AddMagnet/Pause/Resume/Remove/List"
```

---

## Task 24: Wire everything in `main.go` and `app.go`

**Files:**
- Modify: `main.go` (replace generated content)
- Modify: `app.go` (replace generated content)

- [ ] **Step 1: Replace `main.go`**

Overwrite `main.go`:
```go
package main

import (
	"context"
	"embed"
	"os"
	"path/filepath"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"

	"mosaic/backend/api"
	"mosaic/backend/config"
	"mosaic/backend/engine"
	"mosaic/backend/logging"
	"mosaic/backend/persistence"
	"mosaic/backend/platform"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	paths, err := platform.Paths("Mosaic")
	if err != nil {
		panic(err)
	}
	for _, d := range []string{paths.ConfigDir, paths.DataDir, paths.LogDir} {
		_ = os.MkdirAll(d, 0o755)
	}

	debug := os.Getenv("MOSAIC_DEBUG") == "1"
	closer, err := logging.Init(paths.LogDir, debug)
	if err != nil {
		panic(err)
	}
	defer closer.Close()

	cfg, err := config.Load(filepath.Join(paths.ConfigDir, "mosaic.yaml"))
	if err != nil {
		log.Fatal().Err(err).Msg("load config")
	}

	ctx := context.Background()
	db, err := persistence.Open(ctx, filepath.Join(paths.DataDir, "mosaic.db"))
	if err != nil {
		log.Fatal().Err(err).Msg("open db")
	}
	defer db.Close()

	backend, err := engine.NewAnacrolixBackend(engine.AnacrolixConfig{
		DataDir:          filepath.Join(paths.DataDir, "engine"),
		ListenPort:       cfg.ListenPort,
		EnableDHT:        cfg.EnableDHT,
		EnableEncryption: cfg.EnableEncryption,
	})
	if err != nil {
		log.Fatal().Err(err).Msg("open engine backend")
	}
	defer backend.Close()

	eng := engine.NewEngine(backend, 500*time.Millisecond)
	defer eng.Close()

	svc := api.NewService(eng, persistence.NewTorrents(db), cfg.DefaultSavePath)
	app := NewApp(svc)

	err = wails.Run(&options.App{
		Title:  "Mosaic",
		Width:  1200,
		Height: 800,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		OnStartup: app.startup,
		Bind: []any{
			app,
		},
	})
	if err != nil {
		log.Fatal().Err(err).Msg("wails run")
	}
}
```

- [ ] **Step 2: Replace `app.go`**

Overwrite `app.go`:
```go
package main

import (
	"context"
	"time"

	"github.com/rs/zerolog/log"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"mosaic/backend/api"
	"mosaic/backend/engine"
)

// App is the Wails-bound type. Methods on App become callable from the
// frontend via the auto-generated bindings in frontend/wailsjs/.
type App struct {
	svc *api.Service
	ctx context.Context
}

func NewApp(svc *api.Service) *App {
	return &App{svc: svc}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	go a.streamTicks(ctx)
}

// AddMagnet adds a magnet link. Returns the torrent ID.
func (a *App) AddMagnet(magnet string) (string, error) {
	id, err := a.svc.AddMagnet(a.ctx, magnet, "")
	if err != nil {
		return "", err
	}
	return string(id), nil
}

// ListTorrents returns the current list as DTOs.
func (a *App) ListTorrents() ([]api.TorrentDTO, error) {
	return a.svc.ListTorrents(a.ctx)
}

// Pause/Resume/Remove operate by id.
func (a *App) Pause(id string) error  { return a.svc.Pause(engine.TorrentID(id)) }
func (a *App) Resume(id string) error { return a.svc.Resume(engine.TorrentID(id)) }
func (a *App) Remove(id string, deleteFiles bool) error {
	return a.svc.Remove(a.ctx, engine.TorrentID(id), deleteFiles)
}

// streamTicks emits a "torrents:tick" event every 500ms with the current list.
// Plan 2 will replace this with a diff-based emitter; full snapshot is fine
// for now while the list is small.
func (a *App) streamTicks(ctx context.Context) {
	t := time.NewTicker(500 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			rows, err := a.svc.ListTorrents(ctx)
			if err != nil {
				log.Error().Err(err).Msg("list torrents during tick")
				continue
			}
			wailsruntime.EventsEmit(ctx, "torrents:tick", rows)
		}
	}
}
```

- [ ] **Step 3: Verify build**

Run: `go build ./...`
Expected: exits 0.

- [ ] **Step 4: Commit**

```bash
git add main.go app.go
git commit -m "feat: wire backend into Wails app (bindings + 500ms tick)"
```

---

## Task 25: Frontend — install Tailwind v4

**Files:**
- Modify: `frontend/package.json`, create `frontend/src/index.css`, modify `frontend/vite.config.ts` (or `.js`).

- [ ] **Step 1: Install Tailwind**

```bash
cd frontend
pnpm add -D tailwindcss @tailwindcss/vite
cd ..
```

- [ ] **Step 2: Configure Vite plugin**

Modify `frontend/vite.config.ts`. Find the `plugins` array and add `tailwindcss()`:

```ts
import {defineConfig} from 'vite';
import solidPlugin from 'vite-plugin-solid';
import tailwindcss from '@tailwindcss/vite';

export default defineConfig({
  plugins: [solidPlugin(), tailwindcss()],
  // ...rest unchanged
});
```

- [ ] **Step 3: Add Tailwind to global CSS**

Replace the contents of `frontend/src/index.css`:
```css
@import "tailwindcss";

:root {
  color-scheme: dark;
}

html, body, #root {
  height: 100%;
  margin: 0;
  background: #0b0c0e;
  color: #e7e7e9;
  font-family: ui-sans-serif, system-ui, -apple-system, "Segoe UI", Roboto, sans-serif;
}
```

- [ ] **Step 4: Verify dev server runs**

Run: `wails dev` — let it open the window, confirm no console errors. Close.

- [ ] **Step 5: Commit**

```bash
git add frontend/
git commit -m "chore(frontend): add Tailwind v4"
```

---

## Task 26: Frontend — typed bindings wrapper

**Files:**
- Create: `frontend/src/lib/bindings.ts`

> Wails generates `frontend/wailsjs/go/main/App.js` + `.d.ts` automatically when bindings change. We add a small typed wrapper so the rest of the app doesn't import generated code directly.

- [ ] **Step 1: Force a Wails binding regeneration**

Run: `wails generate module`
Expected: writes/updates `frontend/wailsjs/go/main/App.{js,d.ts}` and `frontend/wailsjs/runtime/`. (If `wails generate module` is unavailable on your version, run `wails dev` once and Ctrl+C — it generates on dev startup.)

- [ ] **Step 2: Write the wrapper**

Create `frontend/src/lib/bindings.ts`:
```ts
import {AddMagnet, ListTorrents, Pause, Remove, Resume} from '../../wailsjs/go/main/App';
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

export const api = {
  addMagnet: (magnet: string) => AddMagnet(magnet),
  listTorrents: () => ListTorrents() as Promise<Torrent[]>,
  pause: (id: string) => Pause(id),
  resume: (id: string) => Resume(id),
  remove: (id: string, deleteFiles: boolean) => Remove(id, deleteFiles),
};

export function onTorrentsTick(handler: (rows: Torrent[]) => void): () => void {
  return EventsOn('torrents:tick', handler);
}
```

- [ ] **Step 3: Commit**

```bash
git add frontend/src/lib/bindings.ts
git commit -m "feat(frontend): typed wrapper around Wails bindings"
```

---

## Task 27: Frontend — torrent store

**Files:**
- Create: `frontend/src/lib/store.ts`

- [ ] **Step 1: Write the store**

Create `frontend/src/lib/store.ts`:
```ts
import {createStore, produce} from 'solid-js/store';
import {api, onTorrentsTick, Torrent} from './bindings';

export type TorrentsStore = {
  torrents: Torrent[];
  loading: boolean;
  error: string | null;
};

export function createTorrentsStore() {
  const [state, setState] = createStore<TorrentsStore>({
    torrents: [],
    loading: true,
    error: null,
  });

  // initial load
  api.listTorrents()
    .then((rows) => setState({torrents: rows, loading: false}))
    .catch((e) => setState({error: String(e), loading: false}));

  // live updates
  const off = onTorrentsTick((rows) => {
    setState(produce((s) => { s.torrents = rows; }));
  });

  return {
    state,
    addMagnet: (m: string) => api.addMagnet(m),
    pause: (id: string) => api.pause(id),
    resume: (id: string) => api.resume(id),
    remove: (id: string, deleteFiles: boolean) => api.remove(id, deleteFiles),
    dispose: () => off(),
  };
}
```

- [ ] **Step 2: Commit**

```bash
git add frontend/src/lib/store.ts
git commit -m "feat(frontend): SolidJS torrents store with live tick subscription"
```

---

## Task 28: Frontend — TorrentList component

**Files:**
- Create: `frontend/src/components/TorrentList.tsx`

- [ ] **Step 1: Write the component**

Create `frontend/src/components/TorrentList.tsx`:
```tsx
import {For, Show} from 'solid-js';
import type {Torrent} from '../lib/bindings';

function fmtBytes(n: number): string {
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
  if (n < 1024 * 1024 * 1024) return `${(n / 1024 / 1024).toFixed(1)} MB`;
  return `${(n / 1024 / 1024 / 1024).toFixed(2)} GB`;
}

function fmtRate(n: number): string {
  return `${fmtBytes(n)}/s`;
}

type Props = {
  torrents: Torrent[];
  onPause: (id: string) => void;
  onResume: (id: string) => void;
  onRemove: (id: string) => void;
};

export function TorrentList(props: Props) {
  return (
    <div class="flex flex-col gap-2 p-3">
      <Show when={props.torrents.length === 0}>
        <div class="text-sm text-zinc-500 p-6 text-center">
          No torrents yet. Use <kbd>Add Magnet</kbd> to start one.
        </div>
      </Show>
      <For each={props.torrents}>
        {(t) => (
          <div class="rounded-lg border border-zinc-800 bg-zinc-900/50 p-3">
            <div class="flex items-baseline justify-between gap-3">
              <div class="font-medium truncate">{t.name}</div>
              <div class="text-xs text-zinc-500">{fmtBytes(t.total_bytes)}</div>
            </div>
            <div class="mt-2 h-1.5 rounded bg-zinc-800 overflow-hidden">
              <div
                class="h-full bg-blue-500 transition-[width] duration-300"
                style={{width: `${(t.progress * 100).toFixed(1)}%`}}
              />
            </div>
            <div class="mt-2 flex items-center justify-between text-xs text-zinc-400">
              <span>
                {(t.progress * 100).toFixed(1)}% · ↓ {fmtRate(t.download_rate)} · ↑ {fmtRate(t.upload_rate)} · peers {t.peers}
              </span>
              <span class="flex gap-2">
                <Show
                  when={!t.paused}
                  fallback={
                    <button class="text-blue-400 hover:underline" onClick={() => props.onResume(t.id)}>
                      Resume
                    </button>
                  }
                >
                  <button class="text-amber-400 hover:underline" onClick={() => props.onPause(t.id)}>
                    Pause
                  </button>
                </Show>
                <button class="text-red-400 hover:underline" onClick={() => props.onRemove(t.id)}>
                  Remove
                </button>
              </span>
            </div>
          </div>
        )}
      </For>
    </div>
  );
}
```

- [ ] **Step 2: Commit**

```bash
git add frontend/src/components/TorrentList.tsx
git commit -m "feat(frontend): TorrentList component"
```

---

## Task 29: Frontend — AddMagnetModal

**Files:**
- Create: `frontend/src/components/AddMagnetModal.tsx`

- [ ] **Step 1: Write the component**

Create `frontend/src/components/AddMagnetModal.tsx`:
```tsx
import {createSignal, Show} from 'solid-js';

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
    <Show when={props.open}>
      <div class="fixed inset-0 bg-black/60 grid place-items-center z-50" onClick={props.onClose}>
        <form
          class="w-[560px] rounded-lg bg-zinc-900 border border-zinc-800 p-4 flex flex-col gap-3"
          onClick={(e) => e.stopPropagation()}
          onSubmit={submit}
        >
          <h2 class="text-lg font-semibold">Add Magnet</h2>
          <textarea
            class="bg-zinc-950 border border-zinc-800 rounded p-2 font-mono text-sm h-24"
            placeholder="magnet:?xt=urn:btih:..."
            value={value()}
            onInput={(e) => setValue(e.currentTarget.value)}
            autofocus
            disabled={busy()}
          />
          <Show when={error()}>
            <div class="text-sm text-red-400">{error()}</div>
          </Show>
          <div class="flex justify-end gap-2">
            <button type="button" class="px-3 py-1.5 rounded border border-zinc-700" onClick={props.onClose}>
              Cancel
            </button>
            <button
              type="submit"
              class="px-3 py-1.5 rounded bg-blue-600 disabled:opacity-50"
              disabled={busy() || !value().trim()}
            >
              {busy() ? 'Adding...' : 'Add'}
            </button>
          </div>
        </form>
      </div>
    </Show>
  );
}
```

- [ ] **Step 2: Commit**

```bash
git add frontend/src/components/AddMagnetModal.tsx
git commit -m "feat(frontend): AddMagnetModal component"
```

---

## Task 30: Frontend — wire `App.tsx`

**Files:**
- Replace: `frontend/src/App.tsx`

- [ ] **Step 1: Replace App.tsx**

Overwrite `frontend/src/App.tsx`:
```tsx
import {createSignal, onCleanup} from 'solid-js';
import {createTorrentsStore} from './lib/store';
import {TorrentList} from './components/TorrentList';
import {AddMagnetModal} from './components/AddMagnetModal';
import './index.css';

export default function App() {
  const store = createTorrentsStore();
  const [modalOpen, setModalOpen] = createSignal(false);
  onCleanup(() => store.dispose());

  return (
    <div class="h-full flex flex-col">
      <header class="flex items-center justify-between px-4 py-2 border-b border-zinc-800">
        <div class="font-semibold">Mosaic</div>
        <button
          class="px-3 py-1.5 rounded bg-blue-600 text-sm"
          onClick={() => setModalOpen(true)}
        >
          + Add Magnet
        </button>
      </header>
      <main class="flex-1 overflow-auto">
        <TorrentList
          torrents={store.state.torrents}
          onPause={(id) => store.pause(id)}
          onResume={(id) => store.resume(id)}
          onRemove={(id) => store.remove(id, false)}
        />
      </main>
      <AddMagnetModal
        open={modalOpen()}
        onClose={() => setModalOpen(false)}
        onSubmit={async (m) => { await store.addMagnet(m); }}
      />
    </div>
  );
}
```

- [ ] **Step 2: Commit**

```bash
git add frontend/src/App.tsx
git commit -m "feat(frontend): wire App with torrents list + add-magnet modal"
```

---

## Task 31: End-to-end smoke test

**Files:** none.

- [ ] **Step 1: Run the dev build**

Run: `wails dev`
Expected: Mosaic window opens, dark theme, "+ Add Magnet" button visible, "No torrents yet" message in the body.

- [ ] **Step 2: Add the Ubuntu 24.04 release magnet**

Click **+ Add Magnet**. Paste the canonical Ubuntu Server 24.04 magnet (or any large public test magnet you trust):

```
magnet:?xt=urn:btih:9F9165D9A281A9B8E782CD5176BBCC8256FD1B44&dn=ubuntu-24.04-live-server-amd64.iso&tr=https%3A%2F%2Ftorrent.ubuntu.com%2Fannounce&tr=https%3A%2F%2Fipv6.torrent.ubuntu.com%2Fannounce
```

Click **Add**.

- [ ] **Step 3: Verify download starts**

Within ~30 seconds the row should show:
- A torrent name (probably "ubuntu-24.04-live-server-amd64.iso" once metadata loads)
- A growing percentage on the progress bar
- A non-zero `↓` rate (assuming peers are reachable)
- A non-zero peer count

If the row stays stuck at "0.0% · ↓ 0 B/s · peers 0" for more than 60 seconds, check:
- `~/Downloads` (or whatever `default_save_path` resolves to) has write permissions
- No firewall blocking port 6881
- Logs in `~/Library/Application Support/Mosaic/logs/mosaic.log` (macOS) for errors

- [ ] **Step 4: Verify Pause / Resume / Remove**

- Click **Pause** — `↓` should fall to 0 within a tick.
- Click **Resume** — `↓` should pick back up.
- Click **Remove** — row disappears. Restart the app (`Ctrl+C`, `wails dev` again) and verify the row does NOT come back (it was removed from persistence too).

- [ ] **Step 5: Stop the dev server**

`Ctrl+C` in the terminal running `wails dev`.

- [ ] **Step 6: Tag plan completion**

```bash
git tag plan-1-foundation-complete
```

- [ ] **Step 7: Final summary commit (no-op or docs only)**

If smoke test surfaced any issue, fix it now (a small commit per fix). Otherwise skip.

---

## Self-Review Notes (for the executing engineer)

- **Spec coverage:** This plan covers spec sections 2 (tech stack), 3 (architecture skeleton), 5.1 (module layout — partial), 5.2 (engine wrapper), 5.3 (event bus — partial; only engine→ui tick), 5.5 (Wails IPC — only `torrents:tick` for now), and 6 (persistence — only torrents + settings tables). Sections 4 (full UI panels), 7 (HTTP remote), 8 (auto-update), 9 (packaging/CI), 10 (cross-cutting), and the rest of the persistence schema are deferred to later plans by design.
- **Type consistency check:** `TorrentDTO.id` is `string`; `engine.TorrentID` is `string` underlying — bindings convert at the boundary. `Torrent` (TS) shape mirrors `TorrentDTO` field-for-field with json tags as keys.
- **Known shortcuts** (tracked for Plan 2):
  - `app.go:streamTicks` sends the full list every tick instead of a diff. Fine for ≤ ~50 torrents; replace with diff emission in Plan 2.
  - `engine.Snapshot.AddedAt` is set to `time.Now()` in `anacrolix.go`; the persistence layer is the source of truth, and the api service overlays the real value.
  - `Pause` is implemented as `SetMaxEstablishedConns(0)`. The `Snapshot.Paused` field is not yet wired through; UI's pause/resume buttons work but the Pause status indicator does not update until Plan 2 wires it through `bySaveTo` or a parallel `paused` map.
  - No file picker yet — save path always uses the configured default. The Add modal in Plan 2 will offer Browse and per-torrent save paths.

---

**End of Plan 1.**
