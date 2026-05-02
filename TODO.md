# Mosaic — long-term TODO

Findings from the v0.4.2 brittleness audit. Prioritized by *long-term risk × effort to fix*. The two highest-value items (streamTicks N+1, O(N) `find()`) are being handled next; everything below is queued.

## High-priority

### Auto-updater asset matcher is brittle on three axes simultaneously
`backend/updater/source.go:75-96`

Filename regex (e.g. `darwin-universal\.tar\.gz$`) + inner-tarball entry name (must be lowercase `mosaic` per `tarball_contract_test.go`) + go-selfupdate's case-sensitive `matchExecutableName` all have to align. Renaming a release artifact in CI, switching builders, or upstream go-selfupdate tweaking its matcher silently breaks updates with no error path the user sees. The v0.1.13–v0.1.22 macOS silent-failure was exactly this — the inner tarball name capitalization regressed and nobody noticed for ten releases.

**Direction:** keep the existing `tarball_contract_test`, but add an end-to-end "test-mode" install path that runs against a fixture release tarball in CI, so the full filter → download → swap pipeline is exercised every PR. Catches the next regression at PR time instead of in a user's tray.

### `EventError` is half-implemented
`backend/engine/types.go:52`, `backend/notifications/subscriber.go:133`

The enum value exists, the subscriber has a full handler with title "Torrent error" + deduping + settings gate + tests. **Nothing in production ever emits it.** `notify_on_error` is on by default and silently does nothing — the toggle is lying to the user.

**Direction:** either wire `engine.run` to surface anacrolix tracker/peer errors as `EventError` (anacrolix exposes per-torrent stats and tracker state), or pull the dead handler so the toggle stops claiming a feature we don't ship. ~1 evening either way.

### DTO drift between Go ↔ TS is unprotected
`backend/api/service.go` ↔ `frontend/src/lib/bindings.ts`

Frontend hand-mirrors every DTO. Wails's generated `App.d.ts` types methods opaquely (`api.TorrentDTO` is just a name). Field renames on the Go side compile cleanly and ship silently-broken JSON to a frontend that still expects the old field name. `omitempty` is also inconsistent — `CategoryID *int` ships as `"category_id": null`, while `Files []FileDTO` with `,omitempty` ships as missing-key.

**Direction:** snapshot test that JSON-marshals every DTO into a golden file. CI fails on diff. ~50 LOC, catches every drift case at PR time. (A `tygo`-style codegen step is the bigger version of this if we ever want it.)

## Medium-priority

### Anacrolix internals workarounds need contract tests
`backend/engine/anacrolix.go:31-50, 100-105`

Two load-bearing workarounds for v1.61 internals:
1. `ipBlocklistProxy` exists because `Client.config.IPBlocklist` is copied into an unexported field at construction; we mutate the proxy's inner ranger via atomic.Pointer.
2. `dlLim`/`ulLim` pointers are stashed because the Client has no runtime setter for global rate limits; we mutate them via `SetLimit`/`SetBurst`.

Both depend on anacrolix continuing to (a) hold the `IPBlocklist` interface verbatim and (b) consult the `*rate.Limiter` pointers through their setters rather than copying limit values. A library refactor to copy-by-value or a typed-config struct would silently null these out — blocklist updates and bandwidth caps become no-ops with no panic.

**Direction:** `engine_anacrolix_contract_test.go` that asserts both behaviors against a real `torrent.Client` on every CI run (live blocklist swap, live limit change, observe effect on a synthetic peer connection). Pairs with the existing pin in `go.mod`.

### APT detection via dpkg `.list` glob is fragile
`backend/updater/install_source.go:69-87`

Detection is `filepath.Glob("/var/lib/dpkg/info/mosaic*.list")` + string-match the resolved exe path. False negatives if the path appears with different casing, a different multiarch suffix we didn't anticipate, dpkg-divert-installed paths, or any non-Debian package manager (rpm, pacman, nix, snap, flatpak — all of which "manage" the binary similarly). False negatives let the in-app updater fight the package manager.

**Direction:** explicit sentinel file written by the .deb postinst (e.g. `/usr/share/mosaic/installed-by-apt`) and check that. Fully under our control. The same pattern can extend to `installed-by-rpm` etc. when we wire those.

### `beeep.AppName` package-global race
`backend/notifications/beeep_notifier.go:7-13`

`beeep.AppName` is a package-global with a one-shot `init()` setter — anyone else importing beeep in this process (now or via a transitive dep) wins the race depending on import order. macOS uses it as the `terminal-notifier -group` flag and Windows as the toast AppID; a stomp would ungroup notifications and break Action Center attribution. Currently safe, but a single new dep could silently break it.

**Direction:** write `AppName` immediately before each `beeep.Notify` call (cheap), or vendor the two surfaces (NSUserNotification + Win toast) directly — beeep is ~150 LOC of glue we're already bypassing on Linux.

### Tray availability decided exactly once at startup
`main.go:230-241`, `app.go:447-466`

If the user installs the AppIndicator extension or logs into a different desktop session mid-run, `trayHandle` stays disabled and close-to-tray stays force-off until a relaunch. The Gnome enable flow tells the user to log out and back in, at which point the *next* Mosaic launch picks it up — but `energye/systray.Run`'s `LockOSThread`-bound runloop can't be re-spawned without a process restart anyway (already commented in `main.go`).

**Direction:** lower-priority because the workaround is documented and the lib limitation is real. Worth surfacing "tray will activate after restart" copy in the Gnome enable flow so users aren't left wondering.

## Low-priority / FYI

### `energye/systray` is unmaintained
`backend/tray/tray_other.go`

v1.0.3, no commits in roughly two years. We've already wrapped PNG-as-ICO ourselves, pulled macOS off it entirely (Cgo NSStatusItem), and `godbus` is already a dep for notifications + tray availability. A direct `org.kde.StatusNotifierItem` implementation on Linux would let us drop the lib.

**Direction:** track this for when the next `energye/systray` bug bites. Not urgent.

### Self-signed cert SANs cover only `localhost`/`127.0.0.1`/`::1`
`backend/remote/certs.go:46-56`

When a user enables BindAll without a custom cert, every browser on the LAN hits a TLS warning and the bundled cert is technically invalid for the LAN IP.

**Direction:** detect the bound interface address at `Apply` time, add to the SAN list, regenerate. Already in security TODO memory.

### Settings table mixes naming conventions
`backend/api/service.go:148-161`

`peer_listen_port` (snake_case) vs `desktop.tray_enabled` (dotted). Fine today; if we ever bulk-export settings or pattern-match for migration, the inconsistency is annoying. Cosmetic.

### SQLite `busy_timeout=5000` untested under load
`backend/persistence/db.go:25`

Under heavy concurrent writes (RSS poller adding 50 magnets while user is renaming categories), 5s is the blast-radius cap before `database is locked` surfaces. Untested under that load.

**Direction:** a stress test that hammers the DAO from multiple goroutines just to confirm the timeout is enough. ~30 LOC, cheap to write.
