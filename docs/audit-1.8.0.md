# Mosaic 1.8.0 audit

What this repo does: Mosaic is a desktop BitTorrent client with a shared Go
engine and SQLite database, a SolidJS interface, and an optional authenticated
HTTP/WebSocket interface. The same backend also runs as the `mosaicd` daemon.
This audit assumes one desktop user or a small trusted group, with tens to
hundreds of torrents; it does not assume hostile multi-tenant hosting.

The starting checkout, `00bf349`, matched GitHub `origin/main` after fetching.
The architecture is worth keeping: the engine/service/transport boundaries,
shared desktop/daemon bootstrap, bulk tick snapshots, virtualized lists and
fast-resume support are useful. The main weakness is that many service tests use
a fake engine that cannot reproduce native-library lifecycle failures. A real
daemon smoke test found a pause/shutdown crash after the original suite passed.

The changes warrant **1.8.0**, not a breaking 2.0 rewrite. They retain the public
API and database schema. Go 1.26.10 is the new minimum source-build toolchain.

## Must fix — addressed in 1.8.0

1. **A successful magnet add could never start downloading.**
   `backend/engine/anacrolix_torrents.go`, `spawnVerify` and `verifyAndStart`.
   - **What this is:** Metadata retrieval continues after the add request returns.
   - **Problem:** Returning from an HTTP handler canceled the context used by the
     metadata waiter. Later metadata could arrive with no waiter left to start data.
   - **Fix:** Use the engine lifetime and stop waiting when the torrent is removed.
   - **If skipped:** Browser and daemon users can see connected peers but no download.

2. **Pausing an unresolved magnet could panic; shutdown could exit the process.**
   `backend/engine/anacrolix_torrents.go`, snapshot checkpoint functions.
   - **What this is:** Pauses, periodic checkpoints and shutdown save resume state.
   - **Problem:** `BytesMissing` requires metadata. The checkpoint called it before
     checking whether metadata existed; the daemon smoke test reproduced the crash.
   - **Fix:** Skip both snapshot paths until metadata exists; add a real-engine test.
   - **If skipped:** An ordinary pause or quit can fail while a magnet is pending.

3. **A symlink could turn torrent deletion into deletion of the whole save root.**
   `backend/engine/safepath.go`, `safeRemovePath`.
   - **What this is:** The delete-files action resolves torrent paths before deletion.
   - **Problem:** A child symlink pointing back at the save directory passed the
     resolved containment check, which allowed equality with the root.
   - **Fix:** Reject that equality and fail closed on resolution errors. Restricted
     save paths now return their resolved location and reject dangling ancestors.
   - **If skipped:** Unrelated downloads can be deleted by the same action.

4. **Re-adding a torrent could change its recorded storage location or erase state on failure.**
   `backend/engine/anacrolix_torrents.go` and
   `backend/api/service_torrents.go`, add and rollback paths.
   - **What this is:** Watch folders, RSS and manual adds can encounter an existing hash.
   - **Problem:** The library kept its original storage while Mosaic overwrote its
     path and restarted verification. A failed ownership grant could also delete
     the already-existing record and all its access grants.
   - **Fix:** Honor the library's new/existing result, preserve existing state on
     rollback, and serialize service adds/removes. Reuse the byte-add path for files
     and make the fake engine follow the production duplicate-add contract.
   - **If skipped:** Deletion/checkpointing can target the wrong path, file choices
     can reset, and a failed duplicate add can remove a working torrent from the library.

5. **Regular accounts received a default path they were forbidden to use.**
   `backend/api/service_settings.go`, `GetDefaultSavePath`; `feeds.go`, filter writes.
   - **What this is:** The add dialog pre-fills the default returned by the service.
   - **Problem:** It received the shared root, while adds required `<root>/<username>`.
     RSS filter destinations also bypassed that restriction when later used by the
     privileged background worker.
   - **Fix:** Return and validate the same current per-user default, reject unsafe
     username path components, and resolve non-admin filter destinations at write time.
   - **If skipped:** Normal browser adds fail and RSS managers can store unrestricted
     destinations. Existing RSS rules remain a shared administrative namespace;
     operators should review rules created before this change.

6. **Windows forwarding discarded ordinary launch messages.**
   `backend/platform/launch.go` and `single_instance_windows.go`.
   - **What this is:** A second process sends file or magnet arguments to the running app.
   - **Problem:** The receiver waited for EOF while the sender kept the pipe open
     waiting for the receiver. Its read deadline then discarded the message.
   - **Fix:** Decode one bounded JSON value. Share the receiver across platforms,
     resolve the sender's relative paths, normalize file URLs, and forward empty launches.
     Claim the listener before backend initialization so launches during startup queue safely.
   - **If skipped:** Double-clicking torrents can silently do nothing or start a
     competing process. A stream test now verifies dispatch without closing the sender.

7. **Builds used dependency versions with published advisories.**
   `go.mod`, `frontend/package-lock.json`, `.github/workflows`.
   - **What this is:** These libraries handle network traffic and frontend development.
   - **Problem:** The initial tools reported 13 affected npm packages and 21 Go
     advisories, including the local Go standard library. These are scanner results,
     not 21 independently demonstrated application exploits.
   - **Fix:** Update compatible npm packages, Vite/Vitest, the networking/DTLS/STUN
     chain and the Go toolchain; use `go.mod` in CI. npm reports zero afterward.
   - **If skipped:** Releases continue to embed fixable, publicly known issues.
     One upstream Go advisory remains, described below.

## Should fix — addressed in 1.8.0

8. **OS settings and file handlers did not consistently respect users' choices.**
   `main.go`, `app.go`, `backend/platform/registry_windows.go`, packaging scripts.
   - **What this is:** Startup, tray commands and installers integrate with the desktop.
   - **Problem:** macOS ignored start-minimized; tray construction preceded Wails
     startup; showing a window did not explicitly unhide the application. Windows
     startup overwrote generic handler keys, uninstall deleted shared association
     keys, and Linux installation selected defaults on the user's behalf.
   - **Fix:** Start the tray from Wails startup, honor minimized launch, unhide on
     show, register owned Windows handlers and remove only owned defaults, and stop
     changing Linux defaults. Close the Linux socket listener during cleanup.
     Align macOS metadata and compiler/linker targets to macOS 12, the minimum
     already required by [Go 1.25](https://go.dev/doc/go1.25#darwin) and retained
     by [Go 1.26](https://go.dev/doc/go1.26#darwin).
   - **If skipped:** Settings lie, apps can remain hidden, and installing or removing
     Mosaic can change another torrent client's associations.

9. **Browser events could update a view that no longer owned them.**
   `frontend/src/lib/http_transport.ts` and `store.ts`.
   - **What this is:** One socket feeds live torrent and inspector state.
   - **Problem:** A delayed close from an old socket could clear a newer connection;
     queued inspector frames could replace the newly selected torrent's details.
   - **Fix:** Check socket identity and focused torrent identity; cancel pending
     reconnect timers when starting another connection. Add lifecycle regressions.
   - **If skipped:** Repeated subscriptions can create extra connections or stale details.

10. **Seeding stops and overnight rules lost their intended meaning.**
    `backend/api/service_seeding.go` and `schedule_engine.go`.
    - **What this is:** Background policy stops seeding and changes scheduled bandwidth.
    - **Problem:** Automatic pauses were not persisted. Saturday 22:00–06:00 used
      Sunday's day bit after midnight, so the Saturday rule stopped early.
    - **Fix:** Use the persisted service pause path and attribute overnight mornings
      to the previous day, including the Saturday/Sunday boundary.
    - **If skipped:** Restarting resumes stopped torrents and overnight schedules run
      on the wrong mornings.

11. **A literal database filename could be interpreted as a connection URL.**
    `backend/persistence/db.go`, `Open`.
    - **What this is:** The database path becomes a SQLite URI with connection options.
    - **Problem:** Interpolating an unescaped filename containing `?`, `#` or `%`
      could open a different file or change the connection options.
    - **Fix:** Construct an escaped file URI and verify write/close/reopen at the
      exact requested filename. Also correct macOS test expectations for `/private/var`.
    - **If skipped:** Users with such data-directory names can appear to lose their library.

12. **Release and accessibility details were incomplete.**
    `backend/updater/updater.go`, build helpers and shell components.
    - **What this is:** Version comparisons, package metadata and native accessibility
      names affect updates and everyday operation.
    - **Problem:** Numeric prerelease identifiers compared lexically; bundle metadata
      still said 0.0.0; local packaging deleted shared Go caches; icon buttons had no
      accessible names. Daemon tarball instructions referred to a nonexistent password file.
    - **Fix:** Reuse the existing semver library, align 1.8.0 metadata, make helper
      defaults explicit, limit cache cleanup to CI, pin the DMG tool and correct labels/docs.
    - **If skipped:** Updates can be missed, package versions disagree, local builds
      disrupt other projects and assistive technology cannot identify primary controls.

## Must fix before relying on the affected features — follow-up work

13. **Per-torrent limits do not enforce the configured average on fast connections.**
    `backend/engine/anacrolix_limits.go:95`, `runPerTorrentLimiter`.
    - **What this is:** A 500 ms loop enables or disables transfer for each torrent.
    - **Problem:** It forgets excess bytes each tick. In a steady fast-link model,
      an over-budget interval is followed by a stopped interval, then another full-speed
      interval: roughly half line speed, even for a much smaller requested cap.
    - **Fix:** Carry transfer debt across intervals and compose the limiter with
      pause/error state; clearing a cap must not undo a user's pause. Validate against
      a controlled real-peer transfer, not only a counter simulation.
    - **If skipped:** These caps should be treated as approximate throttles, not
      dependable bandwidth ceilings. Global limits use the library's token bucket.

14. **Skipped files and file priorities are not restored after restart.**
    `backend/api/service_torrents.go`, `SetFilePriorities` and `RestoreOnStartup`;
    `backend/engine/anacrolix_torrents.go`, `applyPostVerifyPriorities`.
    - **What this is:** Users select individual files in the inspector.
    - **Problem:** Changes reach only the live engine. Startup has no persisted file
      choices and verification restores normal priorities.
    - **Fix:** Persist per-file priorities and replay them after verification, with
      restart tests covering skipped files and sequential mode together.
    - **If skipped:** A restart can download files the user explicitly skipped.
      This needs a storage contract and migration, so it is separate from this release.

15. **Deleting files can report success without deleting the payload.**
    `backend/engine/anacrolix_torrents.go`, `Remove`.
    - **What this is:** Removing a torrent optionally deletes its downloaded data.
    - **Problem:** `os.RemoveAll` errors are ignored; a single-file partial stored as
      `<name>.part` is not addressed by deleting only `<name>`. The torrent is already
      dropped before deletion finishes.
    - **Fix:** Delete the actual storage paths, propagate failures and preserve a
      recoverable record when filesystem deletion fails. Test locked files on Windows
      and partial files before changing this destructive path.
    - **If skipped:** Disk space can remain occupied after the UI reports removal.
      The 1.8.0 containment fix prevents the demonstrated root-deletion case; it does
      not replace the whole deletion lifecycle.

## Should fix — trust and maintenance follow-ups

16. **Knowing a torrent hash is enough to become a co-owner.**
    `backend/api/service_torrents.go`, `grantOwner`.
    - **What this is:** Duplicate adds deliberately grant the adding account ownership.
    - **Problem:** Another account can add a known public hash and gain editing and
      sharing rights without the first owner's approval. A viewer can also promote
      their own access by re-adding it.
    - **Fix:** Before supporting untrusted accounts, require an explicit share grant
      or separate each user's torrent identity/storage. Decide the co-ownership policy first.
    - **If skipped:** This remains a trusted-group permission model, not an isolation
      boundary for hostile users. Changing this policy silently would break an intentional feature.

17. **The updater still imports unmaintained OpenPGP and trusts an unsigned checksum manifest.**
    `backend/updater/source.go`; upstream `go-selfupdate/validate.go`.
    - **What this is:** Auto-update verifies the downloaded asset against `SHA256SUMS`.
    - **Problem:** `govulncheck` still reports
      [GO-2026-5932](https://pkg.go.dev/vuln/GO-2026-5932) through the updater's OpenPGP
      import. Mosaic configures SHA-256 validation, not OpenPGP verification; no
      application OpenPGP exploit was demonstrated. Separately, someone able to
      replace both a release asset and its manifest passes the current check.
    - **Fix:** Move to a maintained updater dependency and verify release signatures
      against a separately trusted key before describing updates as publisher-authenticated.
    - **If skipped:** The Go scan remains nonzero, and checksums detect corruption
      without protecting against compromise of the release publishing account.

## Validation and limits

- Full Go tests with `-race -count=1 -timeout=300s`, plus targeted real-engine,
  persistence, schedule, stream-framing and permission regressions.
- Frontend: 106 passing tests, TypeScript compilation and production bundling.
- Live daemon smoke test with isolated temporary data: first run, authentication,
  static assets, version, add/list/pause/resume/remove, logout and clean shutdown.
- Linux and Windows platform tests cross-compiled locally. CI now runs platform,
  persistence and configuration tests on native macOS and Windows runners.
- The Windows desktop executable and Linux amd64 daemon also cross-compiled successfully.
- Universal macOS application, DMG and updater tarball built locally. Both Intel
  and Apple Silicon slices and version metadata are checked. Local artifacts have
  ad-hoc signing, without Developer ID signing or notarization.
- npm audit: 13 affected packages before, zero after. Go vulnerability scan: 21
  reported advisories before, one upstream OpenPGP report after.

Verdict: keep the architecture and ship the compatible fixes. Prioritize reliable
per-torrent caps, persisted file choices and recoverable deletion next; keep daemon
accounts trusted until the ownership policy changes.

Lean: removed the redundant Apple Event bridge, the duplicate file-add flow,
the hand-written version parser and speculative JSON aliases. No new runtime
dependency was introduced; semver was already present transitively.

Not checked: actual peer throughput, long-running swarm load, native Windows/Linux
desktop sessions or installers, and full macOS notification/Finder/Dock behavior
on the newly built app. The installed Mosaic session and its downloads were not
modified. The local packages are not a published GitHub release.
