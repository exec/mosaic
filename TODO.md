# Mosaic — long-term TODO

Findings from the v0.4.2 brittleness audit + debt accumulated through the
v0.5.x cycle. Prioritized by *long-term risk × effort to fix*.

## High-priority

### Auto-updater asset matcher is brittle on three axes simultaneously
`backend/updater/source.go:75-96`

Filename regex (e.g. `darwin-universal\.tar\.gz$`) + inner-tarball entry name (must be lowercase `mosaic` per `tarball_contract_test.go`) + go-selfupdate's case-sensitive `matchExecutableName` all have to align. Renaming a release artifact in CI, switching builders, or upstream go-selfupdate tweaking its matcher silently breaks updates with no error path the user sees. The v0.1.13–v0.1.22 macOS silent-failure was exactly this — the inner tarball name capitalization regressed and nobody noticed for ten releases.

**Direction:** keep the existing `tarball_contract_test`, but add an end-to-end "test-mode" install path that runs against a fixture release tarball in CI, so the full filter → download → swap pipeline is exercised every PR. Catches the next regression at PR time instead of in a user's tray.

## Medium-priority

### Anacrolix internals workarounds need contract tests
`backend/engine/anacrolix.go:31-50, 100-105`

Two load-bearing workarounds for v1.61 internals:
1. `ipBlocklistProxy` exists because `Client.config.IPBlocklist` is copied into an unexported field at construction; we mutate the proxy's inner ranger via atomic.Pointer.
2. `dlLim`/`ulLim` pointers are stashed because the Client has no runtime setter for global rate limits; we mutate them via `SetLimit`/`SetBurst`.

Both depend on anacrolix continuing to (a) hold the `IPBlocklist` interface verbatim and (b) consult the `*rate.Limiter` pointers through their setters rather than copying limit values. A library refactor to copy-by-value or a typed-config struct would silently null these out — blocklist updates and bandwidth caps become no-ops with no panic.

**Direction:** `engine_anacrolix_contract_test.go` that asserts both behaviors against a real `torrent.Client` on every CI run (live blocklist swap, live limit change, observe effect on a synthetic peer connection). Pairs with the existing pin in `go.mod`.

### Tray availability decided exactly once at startup
`main.go:230-241`, `app.go:447-466`

If the user installs the AppIndicator extension or logs into a different desktop session mid-run, `trayHandle` stays disabled and close-to-tray stays force-off until a relaunch. The Gnome enable flow tells the user to log out and back in, at which point the *next* Mosaic launch picks it up — but `energye/systray.Run`'s `LockOSThread`-bound runloop can't be re-spawned without a process restart anyway (already commented in `main.go`).

**Direction:** lower-priority because the workaround is documented and the lib limitation is real. Worth surfacing "tray will activate after restart" copy in the Gnome enable flow so users aren't left wondering. (GnomeTrayPrompt's `needs_restart` branch already does this; double-check messaging covers the install-mid-session case.)

### `PreallocateFullFiles` setting needs a UI surface
`backend/engine/anacrolix.go` (PreallocateFullFiles), `main.go` reads `storage.preallocate_full_files`

v0.5.10 wired the engine + reads from settings DAO, but no Settings pane exposes it. Users who'd benefit from the fast-resume path that PreallocateFull provides (no per-restart `setCompletionFromPartFiles` wipe = instant resume) currently have to set the bit by editing the DB. Add a checkbox in Settings → General or a new Storage pane.

## Low-priority / FYI

### `energye/systray` is unmaintained
`backend/tray/tray_other.go`

v1.0.3, no commits in roughly two years. We've already wrapped PNG-as-ICO ourselves, pulled macOS off it entirely (Cgo NSStatusItem), and `godbus` is already a dep for notifications + tray availability. A direct `org.kde.StatusNotifierItem` implementation on Linux would let us drop the lib.

**Direction:** track this for when the next `energye/systray` bug bites. Not urgent.

### Settings table mixes naming conventions
`backend/api/service.go:148-161`

`peer_listen_port` (snake_case) vs `desktop.tray_enabled` (dotted) vs `storage.preallocate_full_files` (dotted). Fine today; if we ever bulk-export settings or pattern-match for migration, the inconsistency is annoying. Cosmetic.

## Done

- ✅ **`EventError` wired up** (v0.5.1) — `installWriteErrorHook` + `SetErrorHandler` emit real `EventError` on chunk-write failures (disk-full + storage errors).
- ✅ **DTO drift snapshot test** (v0.5.0) — `backend/api/dto_snapshot_test.go` golden-files every DTO; CI fails on diff.
- ✅ **APT-managed sentinel** (v0.5.1) — `/usr/share/mosaic/installed-by-apt` written by postinst; dpkg-glob fallback retained for backward compat.
- ✅ **`beeep.AppName` race** (v0.5.9) — set under a mutex immediately before each `beeep.Notify` call so a transitive importer can't stomp it.
- ✅ **Cert SANs for LAN bind** (v0.5.9) — `EnsureSelfSignedCert` accepts `extraIPs`; `LocalInterfaceIPs()` enumerates non-loopback unicast addresses on BindAll, cache invalidates when a new IP shows up.
- ✅ **Per-peer windowed download rate** (v0.5.9) — backend tracks `prevPeerRates[(TorrentID, "ip:port")]` and computes `(current - prev) / dt` per tick; replaces anacrolix's cumulative-lifetime average.
- ✅ **Tracker HTTP transport timeouts** (v0.5.10) — `tcfg.WebTransport` set to a custom `http.Transport` with 10s dial / 15s response-header / 90s idle conn caps; stuck HTTP-only trackers no longer pin `t.Drop` past those bounds.
- ✅ **SQLite `busy_timeout` stress test** (v0.5.10) — `TestOpen_BusyTimeoutSurvivesConcurrentWriters` confirms 5 s is enough for 8×50 concurrent inserts without surfacing `database is locked`.
- ✅ **`UsePartFiles=false` opt-in** (v0.5.10) — `AnacrolixConfig.PreallocateFullFiles` toggles anacrolix's storage layer to preallocate the full file at the final path, dodging the `setCompletionFromPartFiles` bolt-wipe entirely. Reads `storage.preallocate_full_files` from settings DAO; UI surface still pending (see medium-priority entry above).
