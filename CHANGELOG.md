# Changelog

## 1.9.0 — 2026-10-25

A feature release focused on a more capable inspector, safer torrent and file
management, useful diagnostics, and a compact interface. Desktop and browser
clients share the new backend capabilities.

### Inspector, peers, and trackers

- Prevent long addresses, paths, tracker URLs, and inspector controls from
  overflowing; make useful overview values actionable, with copy, navigation,
  open-folder, and explanatory tooltips where appropriate.
- Show each peer's downloaded and uploaded bytes for the current connection,
  alongside rates, client, address, flags, and completion. Add searchable,
  sortable peer views and expandable connection details.
- Disconnect and manually block peer IPs across all torrents; persist blocks
  across restarts and allow unblocking from the inspector. Add central manual
  block search, add, refresh, and unblock controls in Settings → Blocklist.
- Display a compact torrent health explanation for metadata loading, queued or
  paused transfers, verification, missing files, empty/completed selections,
  stalled downloads, and active downloads/seeding. Retain the latest session
  error with its timestamp and offer relevant recovery actions.
- Record the selected torrent's rates in the speed chart instead of global
  traffic; keep paused torrent samples at zero and reset history on selection.
- Replace misleading tracker “OK” labels with “Status unavailable” when actual
  announce results cannot be verified. Improve tracker copy/edit interactions
  and expose load and action errors.
- Allow per-torrent seeding policies before completion. Show the effective
  inherited/default or custom policy; validate ratio and time limits and
  explain when the first reached limit pauses seeding.
- Respect owner/editor/viewer permissions throughout inspector editing,
  including file priorities, sequential mode, limits, trackers, and policies.
- Add a remembered inspector width with pointer and keyboard resize controls.
  Use a dismissible drawer with focus handling when window space is limited.

### Adding torrents, files, and storage

- Apply category destinations before adding torrents while preserving a save
  path the user explicitly edits.
- Add a prepare/review step for torrent files and magnets, with metadata
  loading, file choices, selected-byte totals, organization options, and
  start-paused support. Unresolved magnets can be kept paused for later review.
- Apply file priorities before starting data transfer and persist selections
  across restarts. Keep preparation paused until the user confirms.
- Replace the flat file list with collapsible folders, filename search,
  selected-size summaries, bulk/folder priorities, and select-all/none controls.
  Add native file open/reveal actions where supported.
- Add storage free-space information and access to the existing preallocation
  option. Add paused “Move data” and “Locate existing files” workflows with
  verification, collision checks, safe copy behavior, and retained originals
  when locating existing data.
- Add explicit removal confirmation with keep-files and delete-files choices.
  Preserve unrelated files, including neighboring `.part` files for directory
  torrents, during relocation and deletion.
- Process multiple dropped torrent files one review at a time, show the current
  filename, continue after cancellation or failure, and report each file's
  outcome. Accept case-insensitive `.torrent` extensions.
- Drain/cancel verification safely around pause, removal, relocation, and
  priority changes; avoid starting transfers before verification state settles
  and preserve useful transfer/error state through torrent operations.

### Library and interface

- Restrict Select All and subsequent bulk actions to visible, filtered results.
  Keep global shortcuts away from focused controls, dialogs, and resize handles.
- Add a visible selection toolbar for pause/resume, category, tags, bandwidth
  limits, and removal, with partial-failure feedback.
- Persist table sorting and column visibility. Add a column picker and ratio,
  cumulative uploaded bytes, seed-count, and queue-position columns, with reset
  controls and keyboard-accessible sorting.
- Add arrow-key/Home/End table selection, Shift range selection, and Enter to
  inspect in the current sorted order, while preserving virtualized rendering.
- Make Status, Categories, and Tags sidebar section chevrons functional and
  remember collapsed sections.
- Show browser connection/reconnection status, the last update time, and stale
  data styling. Hide stale aggregate rate readings; refresh library, inspector,
  settings, and account state after recovery, with retry on refresh failure.
- Detect silent WebSocket connections, reconnect with backoff, cancel obsolete
  timers, ignore old socket responses, and prevent delayed fetches from
  replacing newer live torrent/statistics updates.

### Statistics, RSS, and portable tools

- Add an on-demand transfer statistics view with session, today (UTC), and
  current-library lifetime uploaded/downloaded totals and ratios. Include
  selectable 7/30/90-day daily history with per-day details.
- Persist daily transfer deltas atomically with cumulative totals, handle
  engine counter resets without double-counting, honor each user's torrent
  visibility, and checkpoint final observed counters during clean shutdown.
- Keep inspector and table transfer totals consistent across app restarts and
  across concurrent statistics checkpoints.
- Add RSS filter testing through the backend's Go matching engine. Preview
  title matches, already-handled/in-library items, missing torrent sources,
  disabled-rule state, and the resulting destination without adding torrents
  or marking feed items as handled. Invalidate previews when the draft changes.
- Add versioned JSON export/import for bandwidth, queue, peer connection, and
  seeding defaults. Review imports before applying, validate them before
  writing, and save them atomically. Exclude credentials, web exposure, paths,
  RSS automation, categories, and browser appearance from this backup format.
- Export available torrent metadata as a `.torrent` file from the inspector,
  including metadata resolved from magnets, while enforcing torrent access.

### Connection diagnostics and validation

- Show actual listening endpoints, connected inbound/outbound peers, observed
  DHT servers/nodes, and successful DHT announce replies in Connection settings.
  Refresh readings periodically and identify stale/error states.
- Label the footer indicator “DHT configured” and link it to live diagnostics
  instead of presenting enabled configuration as proof of network health.
- Add regression coverage for selection/permissions, inspector controls,
  preparation and priorities, relocation/removal, peer blocks, connection
  recovery, transfer accounting, RSS previews, portable tools, responsive
  layouts, keyboard controls, and database upgrades.
- Include this complete changelog entry in the GitHub release notes rather
  than relying only on generated commit summaries.

### Upgrade notes and current limits

- The database automatically adds persistent file priorities and daily transfer
  history (migrations 17 and 18). Back up the database before downgrading; older
  versions do not understand these additions. The JSON settings export is not
  a complete library/database backup.
- Daily history starts with this version, is retained for 90 days, and follows
  the user's current accessible library. Removing a torrent removes its
  retained statistics. Earlier daily totals are not reconstructed; sudden
  power loss can still lose transfers since the last periodic checkpoint.
- Peer counters cover the current connection; peer IP history and automatic
  abuse scoring are not retained. Manual blocks have no reason/expiry fields
  and do not override downloaded blocklist ranges when removed.
- Torrent error history is session-scoped. Tracker announce status remains
  unavailable where the engine does not expose verified results.
- Imported listening/DHT/encryption/NAT settings require restarting Mosaic.
  Local listening and DHT activity do not constitute an external port test.
- Native file operations are desktop-only; remote path actions remain subject
  to the existing permissions and save-path boundaries.

## 1.8.1 — 2026-10-21

- Declare the imported BitTorrent file type in macOS production and development
  bundles so Finder recognizes `.torrent` files and offers Mosaic as a handler.
- Add regression tests covering the file-type declarations in both bundles.
- Size the macOS Dock/Finder icon on an inset tile with transparent margins,
  sRGB color, and all standard/Retina representations instead of full-bleed artwork.

On macOS, install this release from the DMG to apply the bundle metadata fix.
The in-app updater replaces only the executable and cannot update `Info.plist`.

## 1.8.0 — 2026-10-21

Compatible reliability and OS integration release. No database migration or API
removal. Source builds now require Go 1.26.10 or newer.

### Downloads and persistence

- Keep magnet metadata retrieval alive after the request that added it ends.
- Prevent crashes when pausing or shutting down with unresolved magnets.
- Preserve the original storage path and selected priorities on duplicate adds.
- Preserve existing torrents when a duplicate add fails to save or grant access;
  serialize add/remove operations so ownership rollback cannot race another add.
- Route file-path adds through the same byte-upload implementation.
- Reject deletion targets that resolve to the download root, and fail closed
  on symlink loops and unresolved save-path ancestors.
- Support literal database filenames containing URL characters.
- Persist automatic seeding-limit pauses and apply overnight schedule rules to
  their starting day.
- Return usable per-user download defaults in the browser and confine new or
  edited non-admin RSS filter destinations to the same user directory.

### Operating systems and interface

- Decode forwarded Windows launch requests without waiting for EOF.
- Resolve forwarded relative torrent paths against the sender's directory and
  accept local `file:` URLs, including escaped filenames and Windows UNC paths.
- Forward launches without arguments and raise the existing Linux/Windows window.
- Claim launch forwarding before opening the database or binding torrent ports,
  so a second launch during startup can queue its arguments safely.
- Close the Linux forwarding listener during cleanup.
- Start the native tray after Wails startup, honor macOS start-minimized, and
  unhide the macOS application when showing it from the tray.
- Remove the redundant custom macOS Apple Event bridge; use Wails's native hooks.
- Align the macOS bundle minimum and compiler/linker deployment target to macOS 12,
  matching the existing Go runtime requirement.
- Register Windows file handlers without replacing another client's defaults;
  uninstall only Mosaic's registrations or legacy defaults still owned by it.
- Preserve Linux MIME defaults during package installation.
- Label sidebar icons and add-torrent controls for assistive technology.
- Ignore stale WebSocket callbacks and inspector frames after switching views.

### Updates and builds

- Use the existing semantic-version library for update comparisons.
- Update vulnerable npm tooling and transitive packages; `npm audit` reports zero
  affected packages for the resulting lockfile.
- Update Go networking, DTLS and STUN dependencies and require a patched Go
  toolchain. One upstream OpenPGP advisory remains in the updater dependency;
  see the audit for its actual use and limits.
- Add macOS/Windows platform tests to CI and derive CI's Go version from `go.mod`.
- Default packaging helpers to `VERSION`, report 1.8.0 in bundle metadata, pin the
  DMG tool, preserve local Go caches, and correct daemon tarball login instructions.

See [the audit](docs/audit-1.8.0.md) for remaining issues and validation scope.
