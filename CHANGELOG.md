# Changelog

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
