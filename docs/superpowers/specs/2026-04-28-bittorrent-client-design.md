# Mosaic — BitTorrent Client Design

**Date:** 2026-04-28
**Status:** Design — awaiting implementation plan
**Working name:** Mosaic *(matches working-directory name; not yet final)*

---

## 1. Goals

A polished, full-featured, cross-platform BitTorrent client for macOS, Linux, and Windows, written from a single codebase.

**Feature scope:** qBittorrent / Deluge territory — DHT, PEX, magnet links, MSE/PE encryption, web seeds, sequential download, RSS auto-add, bandwidth scheduling, queueing, IP filtering, per-torrent rate limits, optional remote web access.

**Design priorities (in order):**
1. Single binary per OS; no daemon-process complexity for the common case.
2. Modern, polished UI with power-user depth available.
3. Optional remote/LAN access exposed as a single setting, not a separate mode.
4. Clean engine boundary so the BitTorrent backend can be swapped if needed.
5. Zero telemetry by default; privacy-respecting.

## 2. Tech Stack

| Layer | Choice | Rationale |
|---|---|---|
| Language (backend) | **Go** | Goroutine concurrency fits BitTorrent's many-long-lived-connections workload; mature pure-Go BT library. |
| Desktop framework | **Wails v2** | Go backend + system webview frontend; produces small native binaries. v3 is still alpha as of 2026-04. |
| BitTorrent engine | **`github.com/anacrolix/torrent`** | 11+ years in production; supports DHT, PEX, magnet, **MSE/PE encryption**, **web seeds**, IP filtering, per-torrent + global rate limits, BT v2. |
| Frontend framework | **SolidJS + TypeScript** | Fine-grained reactivity beats VDOM diffing for high-frequency table updates (peers/torrents ticking every 500ms). |
| Styling | **Tailwind CSS v4** | |
| Components | **Kobalte** (headless) + **solid-ui** (shadcn-style) | Polished, accessible primitives for Solid. |
| Tables | **`@tanstack/solid-table`** + **`@tanstack/solid-virtual`** | Virtualization mandatory for peer/file lists. |
| Charts | **uPlot** | Tiny, fast, ideal for streaming bandwidth graphs. |
| Icons | **`lucide-solid`** | |
| DB | **`modernc.org/sqlite`** (pure Go, no CGO) | Keeps cross-compilation clean. |
| Migrations | **`pressly/goose`** | |
| HTTP server (optional remote) | **`go-chi/chi`** + **`nhooyr.io/websocket`** | |
| Config | **viper-style layered** (defaults → file → env) | |
| Logging | **`zerolog`** + rolling files via lumberjack | |
| Auto-update | **`creativeprojects/go-selfupdate`** | Wails has no built-in updater. |
| Build/scaffold | `wails init -n mosaic -t solid-ts` | |

### Acknowledged gaps vs. libtorrent-rasterbar

These are accepted v1 trade-offs in exchange for skipping the C++ FFI tax:

- **LSD (Local Service Discovery, BEP-14)** — not in anacrolix; minor impact (LAN-only peer discovery).
- **Super-seeding (BEP-16)** — not in anacrolix; only matters when you are the very first seeder.
- **NAT-PMP** — anacrolix supports UPnP; NAT-PMP not first-class. UPnP covers the majority case.
- **Webseed throughput** — known to be slower / sometimes flaky in anacrolix vs libtorrent.

If feature parity demands it later, the engine wrapper (Section 5.2) makes a swap to a `libtorrent-rasterbar` CGO binding bounded.

## 3. Architecture Overview

```
┌──────────────────────────────────────────────────────────────┐
│                       Wails App Process                       │
│                                                                │
│   ┌─────────────────┐         ┌──────────────────────────┐   │
│   │  SolidJS UI     │◀──IPC──▶│  Go: Wails bindings      │   │
│   │  (system        │  events │  + optional HTTP server  │   │
│   │   webview)      │         │  ┌────────────────────┐  │   │
│   └─────────────────┘         │  │  api (services)    │  │   │
│                                │  └─────────┬──────────┘  │   │
│                                │  ┌─────────▼──────────┐  │   │
│                                │  │ engine wrapper     │  │   │
│                                │  │  └─ anacrolix/torrent│  │
│                                │  │ persistence        │  │   │
│                                │  │  └─ SQLite          │  │   │
│                                │  │ scheduler / RSS    │  │   │
│                                │  │ updater / events   │  │   │
│                                │  └────────────────────┘  │   │
│                                └──────────────────────────┘   │
└──────────────────────────────────────────────────────────────┘
```

**By default:** UI ↔ backend over Wails in-process IPC. **Zero ports bound.**
**With "Enable web interface" on:** the same `api` service layer is also exposed via HTTPS+WS at user-chosen bind address.

## 4. UI / Panel Design

### 4.1 Direction

**Modern web-app polish** — middle ground between qBittorrent (dense, power-user) and Transmission (minimal). Toggleable density. Dark mode default.

### 4.2 Window Shape

Four vertical regions, left to right:

```
┌──┬─────────────┬──────────────────────────────────┬─────────────┐
│  │ STATUS      │  Search...    [+ Add ▾]  ⚡  ⊞ ≡│  Inspector  │
│⌂ │ ▸ All       │ ┌────────────────────────────┐   │ ─────────── │
│⊕ │ ▸ Active    │ │  Torrent name              │   │  Overview   │
│🔍│ ▸ Done      │ │  Progress ▓▓▓▓░░  72%      │   │  Files      │
│⏰│ CATEGORIES  │ │  ↓2.1MB/s ↑0.3MB/s         │   │  Peers      │
│📡│   Movies    │ ├────────────────────────────┤   │  Trackers   │
│  │   Software  │ │  Another torrent           │   │  Speed      │
│⚙ │ TAGS        │ │  ▓▓▓▓▓▓▓▓▓▓ 100%           │   │             │
│ⓘ │   #archive  │ │                            │   │  [details]  │
│  │ TRACKERS    │ │                            │   │             │
└──┴─────────────┴──────────────────────────────────┴─────────────┘
            ↓ 4.2 MB/s   ↑ 1.1 MB/s   42 peers   ⚡ Alt
```

- **Icon rail (40px):** Torrents · Add · Search · Schedule · RSS · Settings · About. Active state: filled icon + accent edge.
- **Filter rail (~220px, collapsible):** sections Status / Categories / Tags / Trackers, each collapsible and drag-reorderable. Click filter to apply; ⌘/Ctrl-click multi-selects (OR within section, AND across). Active filters appear as removable chips above the list. Counts update live. Right-click → manage.
- **Main pane:** torrent list (Section 4.3).
- **Inspector (~420px, slide-in, resizable, pinnable, dismissible with `Esc`):** Section 4.4.
- **Status bar (24px, always visible):** `↓ rate · ↑ rate   ●  N torrents · K active · L seeding · M peers   |   DHT N nodes · Port P ✓`. Click ↓/↑ → speed inspector. Click DHT/Port → connection diagnostics.

### 4.3 Main Torrent List

**Density toggle** (toolbar `⊞ ≡`): Cards (default) vs. Table. Choice persists per-window.

**Card mode:**
```
┌────────────────────────────────────────────────────────────────┐
│ ●  ubuntu-24.04.iso                                4.2 GB     │
│    ▓▓▓▓▓▓▓▓▓▓▓░░░░ 72%  ↓ 2.1 MB/s  ↑ 0.3 MB/s  ETA 18m       │
└────────────────────────────────────────────────────────────────┘
```

**Table mode default columns:** Status · Name · Size · ↓ · ↑ · Progress · ETA · Ratio · Seeds · Peers · Added on. Right-click header → toggle: Save path · Category · Tags · Tracker · Availability · Up/Down limit · Last activity · Time active · Hash.

**Selection / interaction:**
- Click select; ⌘/Ctrl-click multi; Shift-click range.
- **Right-click menu:** Pause / Resume / Force Resume / Recheck / Move / Set Location / Set Category / Tags ▸ / Trackers ▸ / Limits ▸ / Open Folder / Copy Magnet / Properties / Remove (with confirm: delete files? checkbox).
- **Keyboard:** `Space` pause/resume · `Enter` open inspector · `⌘/Ctrl-A` select all · `Delete` remove (with confirm).
- **Drag-and-drop:** drag torrent onto a category/tag to assign. Drag within list to set queue priority (only when sorted by Queue Position).

### 4.4 Right Inspector

Header shows torrent name, size, progress, ETA. Tabs (segmented control):

| Tab | Contents |
|---|---|
| **Overview** | Save path, infohash, magnet link (copy button), pieces (total / done), availability, share ratio, total down/up, time active, added/completed timestamps, comment. |
| **Files** | Tree view; per-file priority (skip / normal / high / max) via dropdown; per-file progress bar; sort by name/size/progress; "Open file" / "Reveal in folder". |
| **Peers** | Live table: IP · Client · Flags (DEHIPSXcd…) · Progress · ↓/s · ↑/s · Country flag. Sort/filter inline. |
| **Trackers** | URL · Status · Seeds · Peers · Downloaded · Last announce · Next announce. Buttons: + Add tracker · ↻ Force re-announce. |
| **Speed** | uPlot bandwidth chart with 5-min / 1-hr / 24-hr toggle; per-torrent rate-limit overrides. |

Active tab persists per torrent. With multi-select, tabs show aggregate info (badge count instead of detail).

### 4.5 Top Toolbar

```
┌──────────────────────────────────────────────────────────────────────┐
│ 🔍 Search torrents...        [+ Add ▾]    🌙   ⚡ Alt   [⊞ ≡ ▢] [⚙] │
└──────────────────────────────────────────────────────────────────────┘
```

- **Search** (debounced 200ms) filters main list across name/category/tag/tracker.
- **+ Add ▾:** From file · From magnet link · From URL. Drag-drop a `.torrent` or magnet anywhere in the window also opens this flow.
- **🌙:** theme toggle (Dark / Light / System).
- **⚡ Alt:** toggle alt-speed-limits; tinted accent when active; tooltip shows current limits.
- **⊞ ≡ ▢:** density toggle (cards / table) and inspector toggle.
- **⚙:** settings.

### 4.6 Add-Torrent Modal

Centered modal, ~560px, single flow for file / magnet / URL. Source field accepts paste of magnet links and URLs, or drag-drop / browse for `.torrent`.

Fields: Save to (with "remember last folder"), Category, Tags, Files (tree with checkboxes), and toggles: Start immediately · Skip hash check · Sequential download. Magnet links resolve metadata first (spinner), then populate the file tree. Drag-drop into the window opens this modal pre-populated.

### 4.7 Theme & Visual Style

- **Three themes:** Dark (default) / Light / System.
- **Single accent color** (default `#3b82f6`); user-pickable from a small palette.
- **Native window chrome on macOS** (traffic lights, unified toolbar style); custom on Windows/Linux for tighter integration.
- **Density:** comfortable default; compact reduces row padding ~30%.
- **Status colors:** green (seeding), blue (downloading), amber (queued), red (error), gray (paused).
- **Motion:** 150ms ease for panel slide; 100ms for selection feedback. No bouncy/playful animations.

## 5. Backend Architecture

### 5.1 Module Layout

```
/backend
  /engine        — wraps anacrolix/torrent.Client; owns the stable engine API
  /api           — service layer (the only place business logic lives)
  /persistence   — SQLite-backed torrent metadata, settings, history, RSS
  /events        — typed in-process event bus (channels)
  /scheduler     — bandwidth-schedule and queue-priority rules
  /rss           — feed polling + filter rules → auto-add
  /remote        — optional embedded HTTP+WS server
  /updater       — go-selfupdate against GitHub Releases
  /config        — layered config loader
  /logging       — zerolog setup + log file rotation
  /platform      — OS-specific glue (magnet handler, notifications, single-instance)
  main.go        — wires it all up; starts Wails app
```

**Cardinal rule:** Wails handlers and HTTP handlers are thin adapters. They translate transport shapes into `api` calls and back. All business logic — every state mutation, every validation — lives in `api`. This is what makes the "Enable web interface" toggle a one-line wiring change.

### 5.2 Engine Wrapper

```go
type Engine interface {
    AddTorrent(ctx context.Context, req AddRequest) (TorrentID, error)
    AddMagnet(ctx context.Context, magnet string, opts AddOptions) (TorrentID, error)
    Pause(id TorrentID) error
    Resume(id TorrentID) error
    Remove(id TorrentID, deleteFiles bool) error
    SetFilePriorities(id TorrentID, prios FilePriorities) error
    SetRateLimits(id TorrentID, limits RateLimits) error
    Recheck(id TorrentID) error
    Snapshot(id TorrentID) (TorrentSnapshot, error)
    List() []TorrentSnapshot
    Subscribe() <-chan EngineEvent
}
```

`TorrentSnapshot`, `EngineEvent`, etc. are domain types we own — not anacrolix types — so consumers never see vendor-specific shapes. The wrapper is the only file that imports `anacrolix/torrent` directly.

### 5.3 Event Bus

`engine.Subscribe()` returns a single channel of `EngineEvent` (progress, peer connect/disconnect, completion, error, tracker announce). The `events` package fans these out to subscribers (UI streamer, persistence, scheduler, RSS) without each consumer racing on the engine channel.

### 5.4 Subscription-Driven IPC

The frontend tells the backend what it's currently looking at:

```
engine.Watch(torrentID, [tabs: "peers", "files"])
```

The backend only emits high-frequency updates (peer table ticks, file progress diffs) for what's visible. With 200 torrents but one inspector open, this avoids ~99% of the IPC traffic a naive design would produce.

### 5.5 Frontend ↔ Backend Channels

| Channel | Direction | Use |
|---|---|---|
| Wails bindings (request/response) | UI → Go | All user-initiated actions: AddTorrent, Pause, GetSettings, etc. |
| `EventsEmit` / `EventsOn` | Go → UI | Streamed updates: `torrents:tick`, `torrents:added`, `torrents:removed`, `peers:tick`, `bandwidth:tick`, `notification`, `settings:changed`. |

`torrents:tick` fires every 500ms and carries only changed fields (diff, not full snapshot). `bandwidth:tick` fires every 1s.

## 6. Persistence

**SQLite via `modernc.org/sqlite`** — pure Go, no CGO, single DB file at the platform-appropriate config dir (e.g., `~/Library/Application Support/Mosaic/mosaic.db` on macOS).

Schema sketch (final shape during implementation):

```sql
CREATE TABLE torrents (
  infohash TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  magnet TEXT,
  save_path TEXT NOT NULL,
  category_id INTEGER REFERENCES categories(id),
  added_at INTEGER NOT NULL,
  completed_at INTEGER,
  ratio_limit REAL,
  seed_time_limit INTEGER,
  sequential INTEGER NOT NULL DEFAULT 0,
  paused INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE categories (
  id INTEGER PRIMARY KEY,
  name TEXT UNIQUE NOT NULL,
  default_save_path TEXT,
  color TEXT
);
CREATE TABLE tags (
  id INTEGER PRIMARY KEY,
  name TEXT UNIQUE NOT NULL,
  color TEXT
);
CREATE TABLE torrent_tags (
  infohash TEXT REFERENCES torrents(infohash) ON DELETE CASCADE,
  tag_id INTEGER REFERENCES tags(id) ON DELETE CASCADE,
  PRIMARY KEY (infohash, tag_id)
);
CREATE TABLE trackers_seen (
  infohash TEXT REFERENCES torrents(infohash) ON DELETE CASCADE,
  url TEXT NOT NULL,
  last_status TEXT,
  last_announce INTEGER,
  PRIMARY KEY (infohash, url)
);
CREATE TABLE settings (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL
);
CREATE TABLE schedule_rules (
  id INTEGER PRIMARY KEY,
  days_mask INTEGER NOT NULL,
  start_min INTEGER NOT NULL,
  end_min INTEGER NOT NULL,
  down_kbps INTEGER,
  up_kbps INTEGER,
  alt_only INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE rss_feeds (
  id INTEGER PRIMARY KEY,
  url TEXT NOT NULL,
  name TEXT,
  interval_min INTEGER NOT NULL DEFAULT 30,
  last_polled INTEGER,
  etag TEXT,
  enabled INTEGER NOT NULL DEFAULT 1
);
CREATE TABLE rss_filters (
  id INTEGER PRIMARY KEY,
  feed_id INTEGER REFERENCES rss_feeds(id) ON DELETE CASCADE,
  regex TEXT NOT NULL,
  category_id INTEGER REFERENCES categories(id),
  save_path TEXT,
  enabled INTEGER NOT NULL DEFAULT 1
);
CREATE TABLE download_history (
  id INTEGER PRIMARY KEY,
  infohash TEXT,
  action TEXT NOT NULL,
  ts INTEGER NOT NULL,
  details TEXT
);
```

anacrolix/torrent owns its own resume data on disk (piece bitmaps, peer stores). We do **not** duplicate that — we persist metadata only.

## 7. Optional HTTP Remote Access

Disabled by default. UI to enable lives in Settings → Web Interface:

```
[✓] Enable web interface
     Port:    8080
     Bind to: ⦿ Localhost only   ○ All interfaces
     Auth:    Username [admin]   Password [••••••••]
              [Generate API key]
     TLS:     ⦿ Off (loopback only)
              ○ Self-signed
              ○ Custom cert (path…)
```

- **Server:** `chi` router on `net/http`; WebSocket via `nhooyr.io/websocket`.
- **Routes:** mirror the `api` service layer 1:1 (`GET /torrents`, `POST /torrents`, `WS /events`, etc.).
- **Auth:** session cookie (browser) + bearer API key (programmatic). Passwords stored Argon2id-hashed.
- **TLS:** when binding non-loopback, plain HTTP is rejected; TLS is on. First non-loopback enable generates a self-signed cert (with a clear cert-pinning warning), or accepts a user-supplied cert. Loopback-only bind allows plain HTTP.
- **Static SPA:** the same built frontend assets are embedded via `embed.FS` and mounted at `/`. The frontend has a small transport abstraction so the same SolidJS app runs over Wails IPC or HTTP+WS.

## 8. Auto-Update

- **Channel:** GitHub Releases.
- **Library:** `creativeprojects/go-selfupdate`.
- **Schedule:** check on startup, then every 24h.
- **UX:** non-modal toast "Update available — [Install] [Later]"; user-initiated install downloads the signed bundle, verifies signature/checksum, swaps, prompts to relaunch.
- **macOS gotcha:** replacing a running `.app` requires a post-quit helper; library handles it. Each release must be re-notarized.
- **Disable switch:** Settings → Updates. Enterprise/portable builds ship with updater off.

## 9. Packaging, Signing, CI

| Platform | Output | Signing | Notes |
|---|---|---|---|
| macOS | `.app` in `.dmg` | Apple Developer ID + `notarytool` | Universal binary (amd64+arm64); hardened runtime + entitlements file. |
| Windows | NSIS installer + portable `.exe` | Azure Key Vault + AzureSignTool (OV) **or** EV cert | OV certs are HSM-only; SmartScreen warm-up takes weeks for OV. |
| Linux | `.deb`, `.rpm`, AppImage | unsigned acceptable | AppImage is the universal artifact; Flatpak deferred. |

- **CI:** GitHub Actions matrix `[macos-14, ubuntu-22.04, windows-latest]`.
- **Build command:** `wails build -platform <os/arch>` per matrix entry.
- **Backend tests:** `go test` + `go test -race` for engine and event paths.
- **Frontend tests:** Vitest unit, Playwright E2E against `wails dev`.
- **Release flow:** semver tag → CI builds → drafts GitHub release → uploads signed artifacts → updater picks them up.

## 10. Cross-Cutting Concerns

- **Single instance:** Wails single-instance lock. Second-launch with a magnet/file argument forwards via local IPC to the first instance.
- **`magnet:` protocol handler:** registered on first launch with explicit consent ("Make Mosaic your default for magnet links?"). Per-OS via Wails docs.
- **Logging:** `zerolog` structured logs → rolling file at platform log dir (lumberjack). In-app log viewer shows last N lines.
- **Crash handling:** `recover()` at every goroutine boundary. Crashes log a structured event; crash reports are *opt-in only*, never automatic.
- **Privacy:** zero telemetry by default. No analytics, no ping-home. Update checks talk only to GitHub Releases.
- **Notifications:** OS notification on torrent completion / error (toggleable per category and globally).
- **Watch folder:** optional directory watched for new `.torrent` files; auto-add with default category. Implemented above the engine, persisted in settings.

## 11. Open Questions

1. **Final product name** — "Mosaic" is the working name based on the project directory; not yet locked. Trademark/availability check needed before public release.
2. **Search engine plugins** — the icon rail reserves a "Search" slot for future BT-search-engine plugins (qBittorrent has a similar feature). Out of scope for v1; design the icon and route now, leave the page as "coming soon."
3. **Mobile / native UIs** — explicitly out of scope. The remote HTTP interface gives a mobile-browser path.
4. **Telemetry model for opt-in crash reports** — endpoint, schema, retention policy. Defer until v1.1.

## 12. Out of Scope (v1)

- Local Service Discovery (LSD) — anacrolix gap.
- Super-seeding — anacrolix gap.
- BT search-engine plugins — design slot reserved; implementation deferred.
- Mobile or native (non-web) UIs.
- Flatpak packaging.
- I2P / Tor transports.
- Plugin/scripting system.

## 13. Success Criteria

- A user on macOS, Linux, and Windows can install Mosaic, add a magnet link, and complete a download with no terminal commands.
- All v1 features in Section 1 work on all three platforms.
- Cold UI tick under 500ms (perceived) for actions; sustained download list updates at 500ms cadence with 200+ active torrents without dropping frames.
- Single-binary install size under 30 MB compressed.
- The "Enable web interface" toggle exposes the full UI over HTTPS+WS without code duplication.
- Zero outbound network traffic on first launch beyond the explicit update check (which is itself disable-able).
