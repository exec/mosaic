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

**Direction:** lower-priority because the workaround is documented and the lib limitation is real. Worth surfacing "tray will activate after restart" copy in the Gnome enable flow so users aren't left wondering.

### `UsePartFiles=false` as opt-in storage strategy
`backend/engine/anacrolix.go` (NewAnacrolixBackend)

Currently we use anacrolix's default `UsePartFiles=true`, which preallocates a `.part` file at full torrent size at first write and keeps the final path empty until completion. That makes `setCompletionFromPartFiles` wipe bolt's piece-completion entries on every storage open (final path missing → all pieces marked notComplete). v0.5.6 worked around it with our own bolt-mirror replay, but the underlying interaction is fragile.

`UsePartFiles=false` writes directly to the final path and preallocates there, which dodges the wipe entirely. Tradeoff: full disk-space commitment up front (vs. virtual file size that grows). Pair with the existing disk-space precheck.

**Direction:** add a setting (`storage.preallocate_full`) defaulting to false (current behavior) but exposing a toggle in Settings → Storage. Users on roomy disks who want fast restart without the bolt-mirror dance can flip it on.

### Stop-announce timeout for stuck trackers
`backend/engine/anacrolix.go` (Remove + Close)

v0.5.7 capped `t.Drop` at 5s in Remove and the checkpoint loop at 5s in Close, but anacrolix has no config knob to bound the underlying tracker stop-announce — we just orphan the goroutine. PR upstream a per-call timeout, or wrap our own HTTP transport with a deadline.

**Direction:** wrap stop-announce in a context with a 2s deadline locally (HTTP transport-level), or land a config option in anacrolix. Either keeps the orphaned goroutine count bounded.

### Per-peer windowed download rate
`backend/engine/anacrolix.go` DetailedSnapshot peer loop

anacrolix's `Peer.DownloadRate()` is `BytesReadUsefulData / totalExpectingTime` — a cumulative average over the connection lifetime, not a windowed instantaneous rate. v0.5.7 zeroes the column when the torrent is complete, but for in-progress downloads the per-peer column still reads the historical average instead of "how fast are bytes coming in *right now*."

**Direction:** track `prevPeerStats[(torrentID, peerKey)] = {at, bytes}` in the backend. On DetailedSnapshot, compute (currentBytes - prevBytes) / (now - prevAt). Same shape as the per-torrent rate calc.

## Low-priority / FYI

### `energye/systray` is unmaintained
`backend/tray/tray_other.go`

v1.0.3, no commits in roughly two years. We've already wrapped PNG-as-ICO ourselves, pulled macOS off it entirely (Cgo NSStatusItem), and `godbus` is already a dep for notifications + tray availability. A direct `org.kde.StatusNotifierItem` implementation on Linux would let us drop the lib.

**Direction:** track this for when the next `energye/systray` bug bites. Not urgent.

### Settings table mixes naming conventions
`backend/api/service.go:148-161`

`peer_listen_port` (snake_case) vs `desktop.tray_enabled` (dotted). Fine today; if we ever bulk-export settings or pattern-match for migration, the inconsistency is annoying. Cosmetic.

### SQLite `busy_timeout=5000` untested under load
`backend/persistence/db.go:25`

Under heavy concurrent writes (RSS poller adding 50 magnets while user is renaming categories), 5s is the blast-radius cap before `database is locked` surfaces. Untested under that load.

**Direction:** a stress test that hammers the DAO from multiple goroutines just to confirm the timeout is enough. ~30 LOC, cheap to write.

## Done

- ✅ **`EventError` wired up** (v0.5.1) — `installWriteErrorHook` + `SetErrorHandler` emit real `EventError` on chunk-write failures (disk-full + storage errors).
- ✅ **DTO drift snapshot test** (v0.5.0) — `backend/api/dto_snapshot_test.go` golden-files every DTO; CI fails on diff.
- ✅ **APT-managed sentinel** (v0.5.1) — `/usr/share/mosaic/installed-by-apt` written by postinst; dpkg-glob fallback retained for backward compat.
- ✅ **`beeep.AppName` race** (v0.5.9) — set immediately before each `beeep.Notify` call.
- ✅ **Cert SANs for LAN bind** (v0.5.9) — detect bound interface address at Apply time, add to the regenerated cert's SAN list.
