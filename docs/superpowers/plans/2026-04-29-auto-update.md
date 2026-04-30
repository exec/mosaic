# Mosaic — Plan 7: Auto-update

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Keep installed Mosaic builds current. On startup and every 24 h after, the app silently asks GitHub Releases whether a newer signed build exists for the current OS/arch. If yes, a non-modal toast surfaces the new version with **Install** / **Later** actions. Install streams the platform-appropriate asset, verifies its checksum, swaps the running binary in place (`go-selfupdate` handles the macOS post-quit dance), and prompts the user to relaunch. Users can switch the channel (stable / beta), force an immediate check, or disable the updater entirely from a new Settings → Updates pane. The updater also exposes a build-time `disabled` flag so enterprise/portable builds ship with checks turned off.

**Architecture:** New `backend/updater` package wraps `creativeprojects/go-selfupdate` behind a small `Updater` interface (`Check`, `Install`, `Schedule`). `main.go` instantiates it once with the GitHub source (`github.com/exec/mosaic`) plus the build-time version constant (`-ldflags '-X main.version=...'`) and starts a 24 h ticker. When `Check` returns a release newer than `version`, the updater pushes a `update_available` envelope through the existing `events.Bus[remote.Envelope]` so the SPA toast appears in both the Wails desktop window and any open browser sessions. `api.Service` gains `GetUpdaterConfig` / `SetUpdaterConfig` / `CheckForUpdate` / `InstallUpdate` (settings-table-backed). Wails bindings + chi routes mirror those four calls. Frontend gets an `UpdatesPane` (Enable + channel select + "Check now" + last-check timestamp + version display + Install button) and a single `<UpdateToast>` that renders when `store.state.update.available` is truthy. `AboutPane` grows a "Mosaic v0.7.0" header pulled from the same Wails binding.

**Tech additions:**
- `github.com/creativeprojects/go-selfupdate` — release fetcher + binary swap (handles macOS .app post-quit)

**Reference spec:** `docs/superpowers/specs/2026-04-28-bittorrent-client-design.md` §8 (Auto-update).

**Aesthetic continuity:** UpdatesPane reuses `PaneHeader` / `Field` / Save+Discard layout from `WebInterfacePane` and `ConnectionPane`. UpdateToast leverages `solid-sonner` (already in deps) with the existing Mosaic toast styling — no new component library.

---

## Out of Scope (deferred)

- **Cryptographic release signing** — Plan 8 generates the signed `.dmg` / `.exe` / `.AppImage`; this plan verifies the SHA-256 published in the GitHub release notes, which is what `go-selfupdate` does out of the box.
- **Delta updates** — full-asset replacement only. Bundle is ~25 MB, trivial.
- **Pre-release rollback** — failed install just exits with the old binary intact (the swap is atomic in `go-selfupdate`); manual reinstall handles roll-back. v2 problem.
- **In-app changelog** — toast and pane show version + a single-line release note; deep-linking to the GitHub release page is fine for v1.
- **Browser-mode install** — Install action is **disabled in browser** (`!isWailsRuntime()`). Browser users still see the *available* toast with a copy-link to the GitHub release; only the desktop shell can replace the binary.

---

## File Structure (final state)

```
backend/
├── updater/
│   ├── updater.go                              # NEW: Updater struct, Check/Install/Schedule
│   ├── source.go                               # NEW: GitHub source thin wrapper (mockable)
│   └── updater_test.go                         # NEW: table tests + fake source
├── api/
│   └── service.go                              # MODIFIED: 4 updater methods
└── events/
    └── bus.go                                  # unchanged — reuses existing events bus

main.go                                         # MODIFIED: instantiate updater, start ticker
app.go                                          # MODIFIED: 4 Wails bindings; version getter

backend/remote/
├── handlers.go                                 # MODIFIED: 4 REST routes
└── server.go                                   # MODIFIED: ROUTES table additions

frontend/src/
├── lib/
│   ├── bindings.ts                             # MODIFIED: UpdaterConfigDTO + UpdateInfoDTO + 4 calls
│   ├── http_transport.ts                       # MODIFIED: 4 routes
│   └── store.ts                                # MODIFIED: updater + lastCheckedAt + available
└── components/
    ├── settings/
    │   ├── UpdatesPane.tsx                     # NEW
    │   ├── SettingsSidebar.tsx                 # MODIFIED: add Updates
    │   ├── SettingsRoute.tsx                   # MODIFIED
    │   └── AboutPane.tsx                       # MODIFIED: version header + roadmap refresh
    └── shell/
        └── UpdateToast.tsx                     # NEW: sonner-driven toast
```

---

## Tasks

### Section A — Backend

#### Task 1: Add `creativeprojects/go-selfupdate` dependency

**Files:**
- Modify: `go.mod`, `go.sum`

- [ ] **Step 1: Add the dep**

```bash
go get github.com/creativeprojects/go-selfupdate@latest
```

Don't run `go mod tidy` yet — the rest of Task 1 makes the import live.

- [ ] **Step 2: Commit**

```bash
git add go.mod go.sum
git commit -m "chore: add creativeprojects/go-selfupdate"
```

---

#### Task 2: Build-time version constant

**Files:**
- Modify: `main.go`
- Test: `backend/version_test.go`

- [ ] **Step 1: Define the variable in main.go**

Above `func main()` (and below imports), add:

```go
// version is overridden at build time with `-ldflags "-X main.version=v0.7.0"`.
// Defaults to "dev" so `wails dev` runs cleanly.
var version = "dev"
```

- [ ] **Step 2: Add a getter on App for the frontend**

In `app.go`, add the method:

```go
// AppVersion returns the build-time version string (e.g. "v0.7.0" or "dev").
func (a *App) AppVersion() string {
    return version
}
```

This relies on `version` being declared in the same `main` package — confirm the receiver `*App` and the `version` variable resolve in `app.go`. (If `app.go` is in a separate package, expose `version` via an exported `Version` and have `app.go` import it.)

- [ ] **Step 3: Run `go build` to confirm it compiles**

```bash
go build ./...
```

- [ ] **Step 4: Commit**

```bash
git add main.go app.go
git commit -m "feat: build-time version constant + AppVersion binding"
```

---

#### Task 3: Updater package — `Updater` interface + GitHub source

**Files:**
- Create: `backend/updater/source.go`
- Create: `backend/updater/updater.go`
- Test: `backend/updater/updater_test.go`

- [ ] **Step 1: Write the failing test**

`backend/updater/updater_test.go`:

```go
package updater

import (
    "context"
    "errors"
    "testing"
    "time"
)

type fakeSource struct {
    latestTag string
    latestErr error
}

func (f *fakeSource) DetectLatest(ctx context.Context) (string, string, error) {
    if f.latestErr != nil { return "", "", f.latestErr }
    return f.latestTag, "https://example.com/release", nil
}

func TestCheck_NewerVersion(t *testing.T) {
    u := New(Config{
        CurrentVersion: "v0.7.0",
        Source: &fakeSource{latestTag: "v0.8.0"},
    })
    info, err := u.Check(context.Background())
    if err != nil { t.Fatal(err) }
    if !info.Available { t.Fatal("expected available=true") }
    if info.LatestVersion != "v0.8.0" { t.Fatalf("got %q", info.LatestVersion) }
}

func TestCheck_SameVersion(t *testing.T) {
    u := New(Config{
        CurrentVersion: "v0.7.0",
        Source: &fakeSource{latestTag: "v0.7.0"},
    })
    info, err := u.Check(context.Background())
    if err != nil { t.Fatal(err) }
    if info.Available { t.Fatal("expected available=false") }
}

func TestCheck_OlderRemote(t *testing.T) {
    // dev/local builds shouldn't try to "downgrade".
    u := New(Config{
        CurrentVersion: "v0.9.0",
        Source: &fakeSource{latestTag: "v0.7.0"},
    })
    info, err := u.Check(context.Background())
    if err != nil { t.Fatal(err) }
    if info.Available { t.Fatal("expected available=false on older remote") }
}

func TestCheck_DevBuild(t *testing.T) {
    // CurrentVersion=="dev" means a `wails dev` run; never claim an update.
    u := New(Config{
        CurrentVersion: "dev",
        Source: &fakeSource{latestTag: "v0.7.0"},
    })
    info, err := u.Check(context.Background())
    if err != nil { t.Fatal(err) }
    if info.Available { t.Fatal("expected available=false in dev") }
}

func TestCheck_NetworkError(t *testing.T) {
    u := New(Config{
        CurrentVersion: "v0.7.0",
        Source: &fakeSource{latestErr: errors.New("offline")},
    })
    _, err := u.Check(context.Background())
    if err == nil { t.Fatal("expected error") }
}

func TestSchedule_TickRespectsCancel(t *testing.T) {
    u := New(Config{
        CurrentVersion: "v0.7.0",
        Source: &fakeSource{latestTag: "v0.7.0"},
        Interval: 10 * time.Millisecond,
    })
    ctx, cancel := context.WithCancel(context.Background())
    done := make(chan struct{})
    go func() { u.Schedule(ctx); close(done) }()
    time.Sleep(50 * time.Millisecond)
    cancel()
    select {
    case <-done:
    case <-time.After(time.Second):
        t.Fatal("Schedule did not exit on cancel")
    }
}
```

- [ ] **Step 2: Run; expect compile failure**

```bash
go test ./backend/updater -run TestCheck_NewerVersion
```

Expected: package not found / type undefined.

- [ ] **Step 3: Define the `Source` interface**

`backend/updater/source.go`:

```go
package updater

import (
    "context"
    "fmt"

    selfupdate "github.com/creativeprojects/go-selfupdate"
)

// Source abstracts the release fetcher so tests can supply fakes.
type Source interface {
    // DetectLatest returns (versionTag, downloadURL, error). versionTag is the
    // raw tag (e.g. "v0.8.0"); downloadURL points to the platform-appropriate
    // asset the caller will hand to Apply.
    DetectLatest(ctx context.Context) (version string, assetURL string, err error)
}

// GitHubSource wraps go-selfupdate's GitHub source.
type GitHubSource struct {
    Owner string
    Repo  string
    // Channel is "stable" (default) or "beta"; beta accepts pre-release tags.
    Channel string

    cached *selfupdate.Updater
}

func (s *GitHubSource) lazyInit() (*selfupdate.Updater, error) {
    if s.cached != nil { return s.cached, nil }
    cfg := selfupdate.Config{
        Source: selfupdate.NewGitHubSource(selfupdate.GitHubConfig{}),
        Validator: nil, // Plan 8 will plug in checksum/signature validators
    }
    u, err := selfupdate.NewUpdater(cfg)
    if err != nil { return nil, err }
    s.cached = u
    return u, nil
}

func (s *GitHubSource) DetectLatest(ctx context.Context) (string, string, error) {
    u, err := s.lazyInit()
    if err != nil { return "", "", err }
    rel, found, err := u.DetectLatest(ctx, selfupdate.ParseSlug(fmt.Sprintf("%s/%s", s.Owner, s.Repo)))
    if err != nil { return "", "", err }
    if !found { return "", "", nil }
    if s.Channel != "beta" && rel.Prerelease { return "", "", nil }
    return rel.Version(), rel.AssetURL, nil
}
```

If `go-selfupdate`'s exact API differs slightly (e.g., `Release.AssetURL` vs `URL`), adjust to match the version pinned in `go.mod`. Don't fight the library — read its godoc and match its shape.

- [ ] **Step 4: Define the `Updater` struct + `Config` + `Info`**

`backend/updater/updater.go`:

```go
package updater

import (
    "context"
    "fmt"
    "sync"
    "time"

    selfupdate "github.com/creativeprojects/go-selfupdate"
)

const DefaultInterval = 24 * time.Hour

type Info struct {
    Available     bool      `json:"available"`
    LatestVersion string    `json:"latest_version"`
    AssetURL      string    `json:"asset_url"`
    CheckedAt     time.Time `json:"checked_at"`
}

type Config struct {
    CurrentVersion string
    Source         Source
    Interval       time.Duration // 0 → DefaultInterval
    OnAvailable    func(Info)    // optional; called from Check goroutine when Info.Available
}

type Updater struct {
    cfg Config
    mu  sync.Mutex
    last Info
}

func New(c Config) *Updater {
    if c.Interval == 0 { c.Interval = DefaultInterval }
    return &Updater{cfg: c}
}

func (u *Updater) Last() Info {
    u.mu.Lock(); defer u.mu.Unlock()
    return u.last
}

func (u *Updater) Check(ctx context.Context) (Info, error) {
    info := Info{CheckedAt: time.Now()}
    if u.cfg.CurrentVersion == "dev" || u.cfg.CurrentVersion == "" {
        u.set(info); return info, nil
    }
    tag, asset, err := u.cfg.Source.DetectLatest(ctx)
    if err != nil { return Info{}, err }
    if tag == "" { u.set(info); return info, nil }
    if compareVersions(tag, u.cfg.CurrentVersion) > 0 {
        info.Available = true
        info.LatestVersion = tag
        info.AssetURL = asset
        if u.cfg.OnAvailable != nil { u.cfg.OnAvailable(info) }
    }
    u.set(info)
    return info, nil
}

func (u *Updater) set(i Info) {
    u.mu.Lock(); defer u.mu.Unlock()
    u.last = i
}

func (u *Updater) Schedule(ctx context.Context) {
    // First check on startup, ignoring transient errors.
    _, _ = u.Check(ctx)
    t := time.NewTicker(u.cfg.Interval)
    defer t.Stop()
    for {
        select {
        case <-ctx.Done():
            return
        case <-t.C:
            _, _ = u.Check(ctx)
        }
    }
}

// Install downloads + applies the asset at info.AssetURL, replacing the running
// binary. Caller must arrange a relaunch prompt afterward.
func (u *Updater) Install(ctx context.Context, info Info) error {
    if !info.Available { return fmt.Errorf("no update available") }
    return selfupdate.UpdateTo(ctx, info.AssetURL, info.LatestVersion, selfupdate.ExecutablePath())
}
```

- [ ] **Step 5: Add `compareVersions`**

Add to the same file:

```go
// compareVersions returns negative / zero / positive for a < / == / > b
// using simple semver-ish numeric segment compare. Tolerant of "v" prefix.
func compareVersions(a, b string) int {
    pa := parseSegments(a); pb := parseSegments(b)
    n := len(pa); if len(pb) > n { n = len(pb) }
    for i := 0; i < n; i++ {
        var ai, bi int
        if i < len(pa) { ai = pa[i] }
        if i < len(pb) { bi = pb[i] }
        if ai < bi { return -1 }
        if ai > bi { return 1 }
    }
    return 0
}

func parseSegments(v string) []int {
    if len(v) > 0 && (v[0] == 'v' || v[0] == 'V') { v = v[1:] }
    var out []int
    cur := 0; have := false
    for _, c := range v {
        if c >= '0' && c <= '9' {
            cur = cur*10 + int(c-'0'); have = true
            continue
        }
        if have { out = append(out, cur); cur = 0; have = false }
        if c != '.' { break } // stop at first non-dot non-digit (e.g. "-rc1")
    }
    if have { out = append(out, cur) }
    return out
}
```

- [ ] **Step 6: Run tests; expect PASS**

```bash
go test ./backend/updater -count=1
```

All five `TestCheck_*` and `TestSchedule_*` should pass.

- [ ] **Step 7: Commit**

```bash
git add backend/updater go.sum
git commit -m "feat(updater): Updater struct + GitHub source + 24h schedule"
```

---

#### Task 4: Settings keys + Service methods

**Files:**
- Modify: `backend/api/service.go`
- Test: `backend/api/service_test.go` (extend existing)

- [ ] **Step 1: Define the DTOs**

In `backend/api/service.go`, near the other DTOs:

```go
type UpdaterConfigDTO struct {
    Enabled       bool   `json:"enabled"`
    Channel       string `json:"channel"` // "stable" | "beta"
    LastCheckedAt int64  `json:"last_checked_at"` // unix seconds
    LastSeenVersion string `json:"last_seen_version"`
}

type UpdateInfoDTO struct {
    Available     bool   `json:"available"`
    LatestVersion string `json:"latest_version"`
    AssetURL      string `json:"asset_url"`
    CheckedAt     int64  `json:"checked_at"`
    CurrentVersion string `json:"current_version"`
}
```

- [ ] **Step 2: Define settings keys**

Near the other `setting*` constants:

```go
const (
    settingUpdaterEnabled         = "updater.enabled"          // "1" / "0"
    settingUpdaterChannel         = "updater.channel"          // "stable" | "beta"
    settingUpdaterLastChecked     = "updater.last_checked_at"  // unix seconds
    settingUpdaterLastSeenVersion = "updater.last_seen_version"
)
```

- [ ] **Step 3: Add an `Updater` field on Service**

The Service struct needs to hold a reference to the live `*updater.Updater` plus the build-time version string. Add to the struct:

```go
import "mosaic/backend/updater"

// in Service struct:
updater    *updater.Updater
appVersion string
```

Constructor change: `NewService(... , upd *updater.Updater, version string)`. Store both. Pass `nil` updater when not yet wired (Task 5 fixes `main.go`).

- [ ] **Step 4: Methods**

Add to `Service`:

```go
func (s *Service) GetUpdaterConfig() (UpdaterConfigDTO, error) {
    cfg := UpdaterConfigDTO{
        Enabled:         s.settings.GetBool(settingUpdaterEnabled, true),
        Channel:         s.settings.GetString(settingUpdaterChannel, "stable"),
        LastCheckedAt:   s.settings.GetInt(settingUpdaterLastChecked, 0),
        LastSeenVersion: s.settings.GetString(settingUpdaterLastSeenVersion, ""),
    }
    return cfg, nil
}

func (s *Service) SetUpdaterConfig(cfg UpdaterConfigDTO) error {
    if cfg.Channel != "stable" && cfg.Channel != "beta" {
        return fmt.Errorf("channel must be stable or beta")
    }
    if err := s.settings.SetBool(settingUpdaterEnabled, cfg.Enabled); err != nil { return err }
    return s.settings.SetString(settingUpdaterChannel, cfg.Channel)
}

func (s *Service) CheckForUpdate(ctx context.Context) (UpdateInfoDTO, error) {
    if s.updater == nil {
        return UpdateInfoDTO{CurrentVersion: s.appVersion}, fmt.Errorf("updater disabled")
    }
    info, err := s.updater.Check(ctx)
    if err != nil { return UpdateInfoDTO{}, err }
    _ = s.settings.SetInt(settingUpdaterLastChecked, info.CheckedAt.Unix())
    if info.Available {
        _ = s.settings.SetString(settingUpdaterLastSeenVersion, info.LatestVersion)
    }
    return UpdateInfoDTO{
        Available:      info.Available,
        LatestVersion:  info.LatestVersion,
        AssetURL:       info.AssetURL,
        CheckedAt:      info.CheckedAt.Unix(),
        CurrentVersion: s.appVersion,
    }, nil
}

func (s *Service) InstallUpdate(ctx context.Context) error {
    if s.updater == nil { return fmt.Errorf("updater disabled") }
    last := s.updater.Last()
    return s.updater.Install(ctx, last)
}
```

- [ ] **Step 5: Tests**

Extend `service_test.go`:

```go
func TestUpdaterConfig_RoundTrip(t *testing.T) {
    svc := newTestService(t) // existing helper
    cfg, err := svc.GetUpdaterConfig()
    if err != nil { t.Fatal(err) }
    if !cfg.Enabled { t.Fatal("expected default enabled=true") }
    if cfg.Channel != "stable" { t.Fatalf("default channel=%q", cfg.Channel) }

    if err := svc.SetUpdaterConfig(UpdaterConfigDTO{Enabled: false, Channel: "beta"}); err != nil { t.Fatal(err) }
    got, _ := svc.GetUpdaterConfig()
    if got.Enabled || got.Channel != "beta" { t.Fatalf("got %+v", got) }
}

func TestUpdaterConfig_RejectsUnknownChannel(t *testing.T) {
    svc := newTestService(t)
    if err := svc.SetUpdaterConfig(UpdaterConfigDTO{Enabled: true, Channel: "nightly"}); err == nil {
        t.Fatal("expected error for unknown channel")
    }
}

func TestCheckForUpdate_NoUpdater(t *testing.T) {
    svc := newTestService(t) // no updater wired
    info, err := svc.CheckForUpdate(context.Background())
    if err == nil { t.Fatal("expected updater-disabled error") }
    if info.CurrentVersion == "" { t.Fatal("CurrentVersion should still be set") }
}
```

If `newTestService` doesn't yet take an updater pointer, leave it nil — that's the path being tested.

- [ ] **Step 6: Run all backend tests**

```bash
go test ./... -race -count=1
```

Should be green.

- [ ] **Step 7: Commit**

```bash
git add backend/api
git commit -m "feat(api): UpdaterConfig + CheckForUpdate + InstallUpdate"
```

---

#### Task 5: Wire updater in main.go + Wails bindings

**Files:**
- Modify: `main.go`
- Modify: `app.go`

- [ ] **Step 1: Construct updater in main.go**

In `main.go`, after the persistence/api wiring and before constructing the Wails app, add:

```go
updaterSrc := &updater.GitHubSource{
    Owner:   "exec",
    Repo:    "mosaic",
    Channel: svc.UpdaterChannel(), // tiny helper that reads settingUpdaterChannel
}
upd := updater.New(updater.Config{
    CurrentVersion: version,
    Source:         updaterSrc,
    OnAvailable: func(info updater.Info) {
        if hub != nil {
            hub.Publish(remote.Envelope{Type: "update_available", Data: info})
        }
        wailsruntime.EventsEmit(ctx, "update:available", info) // when ctx is in scope
    },
})

// Inject into svc and start the schedule
svc.AttachUpdater(upd, version)
if svc.UpdaterEnabled() {
    go upd.Schedule(appCtx)
}
```

`AttachUpdater` is a tiny method on Service that sets the unexported `updater` and `appVersion` fields after construction (the constructor signature stays the same to keep the diff small). Update `service.go`:

```go
func (s *Service) AttachUpdater(u *updater.Updater, version string) {
    s.updater = u
    s.appVersion = version
}

func (s *Service) UpdaterEnabled() bool {
    return s.settings.GetBool(settingUpdaterEnabled, true)
}

func (s *Service) UpdaterChannel() string {
    return s.settings.GetString(settingUpdaterChannel, "stable")
}
```

The `wailsruntime.EventsEmit` call needs the runtime context; if it isn't available where main.go constructs the updater, defer the wails-side emission to a tiny wrapper inside `App.OnDomReady` (or wherever runtime ctx becomes available). Use the existing `app.go` tick-publish pattern as a guide.

- [ ] **Step 2: Add Wails bindings**

In `app.go`:

```go
func (a *App) GetUpdaterConfig() (api.UpdaterConfigDTO, error) {
    return a.svc.GetUpdaterConfig()
}
func (a *App) SetUpdaterConfig(cfg api.UpdaterConfigDTO) error {
    return a.svc.SetUpdaterConfig(cfg)
}
func (a *App) CheckForUpdate() (api.UpdateInfoDTO, error) {
    return a.svc.CheckForUpdate(a.ctx)
}
func (a *App) InstallUpdate() error {
    return a.svc.InstallUpdate(a.ctx)
}
```

Use whatever the existing pattern is in `app.go` for accessing the Wails runtime context (`a.ctx` if that's how Plan 1 set it up; otherwise wails `runtime.NewContext`).

- [ ] **Step 3: Build the app**

```bash
go build ./...
```

Confirm clean compile. Don't run `go mod tidy` — we'll do that at the end if needed.

- [ ] **Step 4: Commit**

```bash
git add main.go app.go backend/api/service.go
git commit -m "feat: wire updater + Wails bindings + 24h schedule"
```

---

### Section B — HTTP/WS mirror

#### Task 6: REST routes + WS event in `backend/remote`

**Files:**
- Modify: `backend/remote/handlers.go`
- Modify: `backend/remote/server.go`

- [ ] **Step 1: Add four handlers**

`handlers.go` — append:

```go
func (h *Handlers) getUpdaterConfig(w http.ResponseWriter, r *http.Request) {
    cfg, err := h.svc.GetUpdaterConfig()
    if err != nil { writeErr(w, err, 500); return }
    writeJSON(w, cfg)
}

func (h *Handlers) setUpdaterConfig(w http.ResponseWriter, r *http.Request) {
    var cfg api.UpdaterConfigDTO
    if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil { writeErr(w, err, 400); return }
    if err := h.svc.SetUpdaterConfig(cfg); err != nil { writeErr(w, err, 400); return }
    writeOK(w)
}

func (h *Handlers) checkUpdate(w http.ResponseWriter, r *http.Request) {
    info, err := h.svc.CheckForUpdate(r.Context())
    if err != nil { writeErr(w, err, 500); return }
    writeJSON(w, info)
}

func (h *Handlers) installUpdate(w http.ResponseWriter, r *http.Request) {
    if err := h.svc.InstallUpdate(r.Context()); err != nil { writeErr(w, err, 500); return }
    writeOK(w)
}
```

- [ ] **Step 2: Mount the routes**

In `server.go`, in the `/api` subrouter (next to `/api/settings/web`), add:

```go
r.Get("/api/settings/updater", h.getUpdaterConfig)
r.Put("/api/settings/updater", h.setUpdaterConfig)
r.Post("/api/updater/check", h.checkUpdate)
r.Post("/api/updater/install", h.installUpdate)
```

All four behind `AuthGate`. The exact mount style should match what's already in `server.go` for the existing settings routes — copy the idiom.

- [ ] **Step 3: Add a tiny test**

Extend `handlers_test.go`:

```go
func TestUpdater_GetConfig_OK(t *testing.T) {
    srv := newTestServer(t) // existing helper that builds Handlers + auths
    req := authedReq(t, srv, http.MethodGet, "/api/settings/updater", nil)
    resp := srv.serve(req)
    if resp.Code != 200 { t.Fatalf("status=%d body=%s", resp.Code, resp.Body) }
    var got api.UpdaterConfigDTO
    if err := json.Unmarshal(resp.Body.Bytes(), &got); err != nil { t.Fatal(err) }
    if !got.Enabled { t.Fatal("default Enabled=true expected") }
}
```

If `newTestServer` doesn't already exist, follow the pattern used by `TestRSS_*` or `TestWeb_*` in the same file.

- [ ] **Step 4: Run**

```bash
go test ./backend/remote -race -count=1
```

- [ ] **Step 5: Commit**

```bash
git add backend/remote
git commit -m "feat(remote): updater REST endpoints"
```

---

### Section C — Frontend

#### Task 7: Bindings + transport routes + store

**Files:**
- Modify: `frontend/src/lib/bindings.ts`
- Modify: `frontend/src/lib/http_transport.ts`
- Modify: `frontend/src/lib/store.ts`

- [ ] **Step 1: Bindings — types + api methods**

`bindings.ts` — near the other DTOs, add:

```ts
export type UpdaterConfigDTO = {
  enabled: boolean;
  channel: 'stable' | 'beta';
  last_checked_at: number;
  last_seen_version: string;
};

export type UpdateInfoDTO = {
  available: boolean;
  latest_version: string;
  asset_url: string;
  checked_at: number;
  current_version: string;
};
```

In the `api` object:

```ts
appVersion: () => transport.invoke<string>('AppVersion'),
getUpdaterConfig: () => transport.invoke<UpdaterConfigDTO>('GetUpdaterConfig'),
setUpdaterConfig: (c: UpdaterConfigDTO) => transport.invoke<void>('SetUpdaterConfig', c),
checkForUpdate: () => transport.invoke<UpdateInfoDTO>('CheckForUpdate'),
installUpdate: () => transport.invoke<void>('InstallUpdate'),
```

And register the WS event:

```ts
export function onUpdateAvailable(handler: (info: UpdateInfoDTO) => void): () => void {
  return transport.on('update_available', handler);
}
```

(The Wails-side event is named `update:available` while the WS envelope uses `update_available`. The transport abstraction normalizes — confirm the Wails transport's `EventsOn` uses the colon-form and adjust either side so both shells deliver to the same handler. Simplest: emit on both sides under the colon form and update the WS envelope `Type` to `"update:available"` for symmetry.)

- [ ] **Step 2: Add ROUTES entries to http_transport.ts**

```ts
AppVersion:        {method: 'GET',  path: () => '/api/version', unwrap: (r) => r.version},
GetUpdaterConfig:  {method: 'GET',  path: () => '/api/settings/updater'},
SetUpdaterConfig:  {method: 'PUT',  path: () => '/api/settings/updater', body: (c) => c, unwrap: () => undefined},
CheckForUpdate:    {method: 'POST', path: () => '/api/updater/check'},
InstallUpdate:     {method: 'POST', path: () => '/api/updater/install', unwrap: () => undefined},
```

You'll need to add a tiny `GET /api/version` handler in `backend/remote/handlers.go` that returns `{version: app.AppVersion()}`. Wire it in the same commit if it isn't already there.

- [ ] **Step 3: Store**

In `store.ts`, add to `AppState`:

```ts
updaterConfig: UpdaterConfigDTO;
updateInfo: UpdateInfoDTO | null;  // last check result; null until first check
```

Defaults:

```ts
const emptyUpdaterConfig: UpdaterConfigDTO = {
  enabled: true,
  channel: 'stable',
  last_checked_at: 0,
  last_seen_version: '',
};
```

Initial fetch:

```ts
api.getUpdaterConfig().then((c) => setState(produce((s) => { s.updaterConfig = c; }))).catch(console.error);
```

Methods:

```ts
refreshUpdaterConfig: async () => {
  const c = await api.getUpdaterConfig();
  setState(produce((s) => { s.updaterConfig = c; }));
},
setUpdaterConfig: async (c: UpdaterConfigDTO) => {
  await api.setUpdaterConfig(c);
  const fresh = await api.getUpdaterConfig();
  setState(produce((s) => { s.updaterConfig = fresh; }));
},
checkForUpdate: async () => {
  const info = await api.checkForUpdate();
  setState(produce((s) => { s.updateInfo = info; }));
  return info;
},
installUpdate: () => api.installUpdate(),
```

Subscribe to the WS event near the other `onTorrentsTick` etc:

```ts
const offU = onUpdateAvailable((info) => setState(produce((s) => { s.updateInfo = info; })));
// ...add offU() to the dispose() chain
```

- [ ] **Step 4: Run**

```bash
cd frontend && npm test
cd frontend && npm run build
```

- [ ] **Step 5: Commit**

```bash
git add frontend/src/lib backend/remote
git commit -m "feat(frontend): updater bindings + transport routes + store"
```

---

#### Task 8: UpdatesPane in Settings

**Files:**
- Create: `frontend/src/components/settings/UpdatesPane.tsx`
- Test: `frontend/src/components/settings/UpdatesPane.test.tsx`
- Modify: `frontend/src/components/settings/SettingsSidebar.tsx`
- Modify: `frontend/src/components/settings/SettingsRoute.tsx`
- Modify: `frontend/src/App.tsx`

- [ ] **Step 1: Write the pane**

`UpdatesPane.tsx`:

```tsx
import {createSignal, createEffect, Show} from 'solid-js';
import {Download, RefreshCw} from 'lucide-solid';
import {toast} from 'solid-sonner';
import type {UpdaterConfigDTO, UpdateInfoDTO} from '../../lib/bindings';
import {isWailsRuntime} from '../../lib/runtime';

type Props = {
  config: UpdaterConfigDTO;
  info: UpdateInfoDTO | null;
  appVersion: string;
  onSet: (c: UpdaterConfigDTO) => Promise<void>;
  onCheck: () => Promise<UpdateInfoDTO>;
  onInstall: () => Promise<void>;
};

export function UpdatesPane(props: Props) {
  const [enabled, setEnabled] = createSignal(props.config.enabled);
  const [channel, setChannel] = createSignal<'stable' | 'beta'>(props.config.channel);
  const [checking, setChecking] = createSignal(false);
  const [installing, setInstalling] = createSignal(false);

  createEffect(() => {
    setEnabled(props.config.enabled);
    setChannel(props.config.channel);
  });

  const dirty = () => enabled() !== props.config.enabled || channel() !== props.config.channel;

  const save = async () => {
    try {
      await props.onSet({...props.config, enabled: enabled(), channel: channel()});
      toast.success('Update settings saved');
    } catch (e) { toast.error(String(e)); }
  };

  const check = async () => {
    setChecking(true);
    try { await props.onCheck(); toast.success('Check complete'); }
    catch (e) { toast.error(String(e)); }
    finally { setChecking(false); }
  };

  const install = async () => {
    setInstalling(true);
    try { await props.onInstall(); toast.success('Installed — relaunch Mosaic'); }
    catch (e) { toast.error(String(e)); }
    finally { setInstalling(false); }
  };

  const lastCheckedLabel = () => {
    if (!props.config.last_checked_at) return 'never';
    return new Date(props.config.last_checked_at * 1000).toLocaleString();
  };

  return (
    <div class="mx-auto max-w-2xl px-6 py-6">
      <div class="mb-4 border-b border-white/[.04] pb-3">
        <h2 class="text-lg font-semibold text-zinc-100">Updates</h2>
        <p class="mt-0.5 text-sm text-zinc-500">Mosaic checks for new versions on startup and every 24 hours.</p>
      </div>

      <div class="rounded-md border border-white/[.06] bg-white/[.02] p-4 mb-4">
        <div class="flex items-center justify-between">
          <span class="text-sm text-zinc-300">Current version</span>
          <span class="font-mono text-sm tabular-nums text-zinc-100">{props.appVersion}</span>
        </div>
        <div class="mt-2 flex items-center justify-between">
          <span class="text-sm text-zinc-300">Last checked</span>
          <span class="text-sm text-zinc-400">{lastCheckedLabel()}</span>
        </div>
        <Show when={props.info?.available}>
          <div class="mt-3 rounded-md bg-accent-500/10 p-3 text-sm">
            <div class="text-accent-200">Update available: <span class="font-mono">{props.info!.latest_version}</span></div>
            <button
              type="button"
              class="mt-2 inline-flex items-center gap-1.5 rounded-md bg-accent-500 px-3 py-1 text-sm font-medium text-zinc-950 hover:bg-accent-400 disabled:opacity-50"
              disabled={installing() || !isWailsRuntime()}
              onClick={install}
              data-testid="updater-install"
            >
              <Download class="h-3.5 w-3.5" />
              {installing() ? 'Installing…' : 'Install update'}
            </button>
            <Show when={!isWailsRuntime()}>
              <p class="mt-1 text-xs text-zinc-500">Install must run from the desktop app.</p>
            </Show>
          </div>
        </Show>
      </div>

      <div class="rounded-md border border-white/[.06] bg-white/[.02] p-4 mb-4">
        <label class="flex items-center gap-2 text-sm text-zinc-200">
          <input type="checkbox" checked={enabled()} onChange={(e) => setEnabled(e.currentTarget.checked)} class="accent-accent-500" data-testid="updater-enabled" />
          Enable automatic update checks
        </label>

        <fieldset class="mt-4">
          <legend class="text-xs uppercase tracking-wider text-zinc-500 mb-1">Channel</legend>
          <div class="flex gap-4 text-sm text-zinc-200">
            <label class="flex items-center gap-1.5">
              <input type="radio" name="channel" checked={channel() === 'stable'} onChange={() => setChannel('stable')} data-testid="updater-channel-stable" />
              Stable
            </label>
            <label class="flex items-center gap-1.5">
              <input type="radio" name="channel" checked={channel() === 'beta'} onChange={() => setChannel('beta')} data-testid="updater-channel-beta" />
              Beta
            </label>
          </div>
        </fieldset>
      </div>

      <div class="flex items-center gap-2">
        <button
          type="button"
          onClick={check}
          disabled={checking()}
          class="inline-flex items-center gap-1.5 rounded-md border border-white/[.06] bg-white/[.04] px-3 py-1.5 text-sm text-zinc-200 hover:bg-white/[.06] disabled:opacity-50"
          data-testid="updater-check"
        >
          <RefreshCw class={`h-3.5 w-3.5 ${checking() ? 'animate-spin' : ''}`} />
          Check now
        </button>
        <div class="flex-1" />
        <button
          type="button"
          onClick={save}
          disabled={!dirty()}
          class="rounded-md bg-accent-500 px-3 py-1.5 text-sm font-medium text-zinc-950 hover:bg-accent-400 disabled:opacity-50"
          data-testid="updater-save"
        >
          Save
        </button>
      </div>
    </div>
  );
}
```

- [ ] **Step 2: Tests**

`UpdatesPane.test.tsx`:

```tsx
import {describe, it, expect, vi, beforeEach, afterEach} from 'vitest';
import {render} from 'solid-js/web';
import {UpdatesPane} from './UpdatesPane';
import type {UpdaterConfigDTO, UpdateInfoDTO} from '../../lib/bindings';

vi.mock('solid-sonner', () => ({toast: {success: vi.fn(), error: vi.fn()}}));

let host: HTMLDivElement;
let dispose: () => void;

beforeEach(() => { host = document.createElement('div'); document.body.appendChild(host); });
afterEach(() => { dispose?.(); host.remove(); });

const baseCfg: UpdaterConfigDTO = {enabled: true, channel: 'stable', last_checked_at: 0, last_seen_version: ''};

describe('UpdatesPane', () => {
  it('shows current version', () => {
    dispose = render(() => (
      <UpdatesPane config={baseCfg} info={null} appVersion="v0.7.0" onSet={vi.fn()} onCheck={vi.fn()} onInstall={vi.fn()} />
    ), host);
    expect(host.textContent).toContain('v0.7.0');
  });

  it('Save calls onSet with channel change', async () => {
    const onSet = vi.fn().mockResolvedValue(undefined);
    dispose = render(() => (
      <UpdatesPane config={baseCfg} info={null} appVersion="v0.7.0" onSet={onSet} onCheck={vi.fn()} onInstall={vi.fn()} />
    ), host);
    host.querySelector<HTMLInputElement>('[data-testid=updater-channel-beta]')!.click();
    host.querySelector<HTMLButtonElement>('[data-testid=updater-save]')!.click();
    await Promise.resolve();
    expect(onSet).toHaveBeenCalledWith(expect.objectContaining({channel: 'beta'}));
  });

  it('Check now calls onCheck', async () => {
    const onCheck = vi.fn().mockResolvedValue({available: false, latest_version: '', asset_url: '', checked_at: 0, current_version: 'v0.7.0'});
    dispose = render(() => (
      <UpdatesPane config={baseCfg} info={null} appVersion="v0.7.0" onSet={vi.fn()} onCheck={onCheck} onInstall={vi.fn()} />
    ), host);
    host.querySelector<HTMLButtonElement>('[data-testid=updater-check]')!.click();
    await Promise.resolve();
    expect(onCheck).toHaveBeenCalled();
  });

  it('Install button rendered when update available; click invokes onInstall in Wails', async () => {
    (window as any).runtime = {}; // simulate Wails
    const onInstall = vi.fn().mockResolvedValue(undefined);
    const info: UpdateInfoDTO = {available: true, latest_version: 'v0.8.0', asset_url: 'x', checked_at: 1, current_version: 'v0.7.0'};
    dispose = render(() => (
      <UpdatesPane config={baseCfg} info={info} appVersion="v0.7.0" onSet={vi.fn()} onCheck={vi.fn()} onInstall={onInstall} />
    ), host);
    host.querySelector<HTMLButtonElement>('[data-testid=updater-install]')!.click();
    await Promise.resolve();
    expect(onInstall).toHaveBeenCalled();
    delete (window as any).runtime;
  });

  it('Install button disabled outside Wails', () => {
    const info: UpdateInfoDTO = {available: true, latest_version: 'v0.8.0', asset_url: 'x', checked_at: 1, current_version: 'v0.7.0'};
    dispose = render(() => (
      <UpdatesPane config={baseCfg} info={info} appVersion="v0.7.0" onSet={vi.fn()} onCheck={vi.fn()} onInstall={vi.fn()} />
    ), host);
    const btn = host.querySelector<HTMLButtonElement>('[data-testid=updater-install]')!;
    expect(btn.disabled).toBe(true);
  });
});
```

- [ ] **Step 3: Sidebar + route wiring**

`SettingsSidebar.tsx`:

```ts
import {Download as DownloadIcon} from 'lucide-solid';
// add to SettingsPane union: 'updates'
// add { value: 'updates', label: 'Updates', icon: DownloadIcon } before 'about'
```

`SettingsRoute.tsx`:

- Import `UpdatesPane`.
- Add 4 props: `updaterConfig`, `updateInfo`, `appVersion`, `onSetUpdaterConfig`, `onCheckForUpdate`, `onInstallUpdate`.
- Add `<Match when={props.pane === 'updates'}>` before About.

`App.tsx`:

- Pass through. `appVersion` is loaded once via `await api.appVersion()` in an `onMount` then stored in a `createSignal`.

- [ ] **Step 4: Run**

```bash
cd frontend && npm test
cd frontend && npm run build
```

- [ ] **Step 5: Commit**

```bash
git add frontend/src/components/settings frontend/src/App.tsx
git commit -m "feat(frontend): UpdatesPane + sidebar/route wiring"
```

---

#### Task 9: UpdateToast — non-modal sonner toast

**Files:**
- Create: `frontend/src/components/shell/UpdateToast.tsx`
- Modify: `frontend/src/App.tsx`
- Test: extend `frontend/src/lib/store.test.ts` (or add new) — assert that setting `updateInfo.available=true` dispatches the toast call once

- [ ] **Step 1: Component**

```tsx
import {createEffect} from 'solid-js';
import {toast} from 'solid-sonner';
import type {UpdateInfoDTO} from '../../lib/bindings';

type Props = {
  info: UpdateInfoDTO | null;
  onInstall: () => void;
  onDismiss: () => void;
};

let lastShownFor = '';

export function UpdateToast(props: Props) {
  createEffect(() => {
    const info = props.info;
    if (!info?.available) return;
    if (info.latest_version === lastShownFor) return;
    lastShownFor = info.latest_version;
    toast(`Update available — ${info.latest_version}`, {
      duration: 30000,
      action: {label: 'Install', onClick: () => props.onInstall()},
      cancel: {label: 'Later', onClick: () => props.onDismiss()},
    });
  });
  return null;
}
```

- [ ] **Step 2: Mount in App.tsx**

Inside the existing JSX tree, near the `<Toaster>`:

```tsx
<UpdateToast
  info={store.state.updateInfo}
  onInstall={() => { store.setView('settings'); store.setSettingsPane('updates'); }}
  onDismiss={() => {}}
/>
```

Clicking Install routes to the Updates pane (where the user clicks the actual Install button) — keeps the toast logic dumb and ensures we don't auto-install without the user confirming on the pane.

- [ ] **Step 3: Test**

Lightweight test: render `<UpdateToast info={null}>` then change to `info.available=true` via a Solid signal wrapper, assert `toast` was called once. Module-mock `solid-sonner`.

- [ ] **Step 4: Run + commit**

```bash
cd frontend && npm test
cd frontend && npm run build
git add frontend/src
git commit -m "feat(frontend): non-modal update-available toast"
```

---

#### Task 10: AboutPane refresh

**Files:**
- Modify: `frontend/src/components/settings/AboutPane.tsx`

- [ ] **Step 1: Show the version**

Accept a prop `appVersion: string` (threaded through `SettingsRoute` from `App.tsx` — already available from Task 8). Show it under the title:

```tsx
<p class="text-xs font-mono text-zinc-400">Version <span class="text-zinc-200">{props.appVersion}</span></p>
```

- [ ] **Step 2: Refresh PROGRESS list**

Mark Plan 4c, 5, 6 as `done: true`. Add Plan 7 (`done: true`). Plan 8 stays `done: false`.

- [ ] **Step 3: Commit**

```bash
git add frontend/src/components/settings/AboutPane.tsx
git commit -m "feat(frontend): AboutPane shows version + refresh roadmap"
```

---

### Section D — Smoke

#### Task 11: User-driven smoke

This task is gated on Plan 8 producing actual signed releases — without a real release artifact, the Install code path can't be exercised end-to-end. What we *can* smoke now:

- [ ] `wails dev -skipembedcreate`. App boots; Settings → Updates pane renders with current version "dev"; "Check now" button is callable but `Check` returns `available=false` immediately (because CurrentVersion=="dev").
- [ ] Toggle Enable off and on; observe the goroutine ticker stops/starts (use a debug log or trace if necessary). Channel switch persists.
- [ ] Build a release binary with `go build -ldflags "-X main.version=v0.7.0"` and run it. Check now should hit GitHub. (If no v0.7.x release exists yet, this will simply return `available=false`; that's expected.)
- [ ] Manually craft a fake GitHub-shaped JSON via a local HTTP fixture (only if the dev wants to exercise the toast/install paths) — otherwise defer the install validation to Plan 8 once the release pipeline produces real artifacts.
- [ ] Tag `plan-7-update-complete`, push.

---

## Dispatch summary (suggested batches)

- **Batch 1 (Backend):** Tasks 1, 2, 3 — dep + version + Updater package. 4 commits.
- **Batch 2 (API + main):** Tasks 4, 5 — Service methods + main wiring + bindings. 2 commits.
- **Batch 3 (Remote mirror):** Task 6 — REST routes + tests. 1 commit.
- **Batch 4 (Frontend plumbing):** Task 7 — bindings + transport + store. 1 commit.
- **Batch 5 (UI):** Tasks 8, 9, 10 — UpdatesPane + UpdateToast + AboutPane. 3 commits.
- **Batch 6:** Task 11 — user smoke (deferred per Plan 8 dependency).

---

**End of Plan 7.**
