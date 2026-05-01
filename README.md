# Mosaic

A polished cross-platform BitTorrent client. Go + Wails + the [anacrolix](https://github.com/anacrolix/torrent) engine on the backend; SolidJS + Tailwind on the frontend; SQLite for persistence; an HTTPS+WebSocket remote interface that serves the same SPA to a browser. Ships for macOS (universal), Linux, and Windows.

> Documentation lives at **[mosaic.byexec.com](https://mosaic.byexec.com)**.

## Install

Grab the asset for your platform from [the latest release](https://github.com/exec/mosaic/releases/latest). Filenames are versioned (e.g. `Mosaic-v0.2.11-...`), so the per-release page is the canonical pointer.

| Platform | Asset |
|----------|-------|
| macOS (universal) | `Mosaic-vX.Y.Z-darwin-universal.dmg` — drag `mosaic.app` to `/Applications/` |
| Linux             | `Mosaic-vX.Y.Z-linux-amd64.{deb,rpm,AppImage}` — pick what your distro takes |
| Linux (headless)  | `Mosaic-vX.Y.Z-linux-amd64-mosaicd.{deb,rpm}` or `mosaicd-vX.Y.Z-linux-amd64.tar.gz` — daemon-only build for servers / NAS, controlled over the HTTPS+WS interface (same concept as `qbittorrent-nox`) |
| Windows           | `Mosaic-vX.Y.Z-windows-amd64-installer.exe` (NSIS, per-user) or `…-portable.exe` |

Each release also publishes a `SHA256SUMS` manifest; verifying against it before running is recommended. Full per-platform install notes (incl. file association registration, dock + magnet handler quirks, signing status): [docs/installation/](https://mosaic.byexec.com/docs/installation/).

## Documentation

### Overview

- [Introduction](https://mosaic.byexec.com/) — hero, stat grid, feature index
- [Installation](https://mosaic.byexec.com/docs/installation/) — per-platform install, first-run defaults, config locations
- [Getting Started](https://mosaic.byexec.com/docs/getting-started/) — adding torrents, queue, inspector, settings tour
- [Architecture](https://mosaic.byexec.com/docs/architecture/) — process layout, package boundaries, persistence, frontend transport

### Features

- [Categories &amp; Tags](https://mosaic.byexec.com/docs/features/categories-tags/) — 1:N grouping vs M:N labels, save-path inheritance
- [RSS Feeds](https://mosaic.byexec.com/docs/features/rss/) — feeds + per-feed regex filters with auto-add
- [Scheduling](https://mosaic.byexec.com/docs/features/scheduling/) — time-of-day bandwidth rules, alt-speed mode
- [IP Blocklist](https://mosaic.byexec.com/docs/features/blocklist/) — fetch model, refresh, format
- [Desktop Integration](https://mosaic.byexec.com/docs/features/desktop-integration/) — tray, notifications, file associations, magnet handler, single-instance launch, dock click

### Web Interface &amp; API

- [Web Interface](https://mosaic.byexec.com/docs/web-interface/) — enabling, binding, password, API key
- [Daemon (mosaicd)](https://mosaic.byexec.com/docs/daemon/) — headless Linux variant, systemd unit, reverse-proxy deployment
- [API Overview](https://mosaic.byexec.com/docs/api/) — base URL, auth model, error envelope
- [Authentication](https://mosaic.byexec.com/docs/api/authentication/) — login, sessions, bearer keys, CSRF
- [REST Endpoints](https://mosaic.byexec.com/docs/api/rest/) — every route, request/response schema
- [WebSocket](https://mosaic.byexec.com/docs/api/websocket/) — `/api/ws`, frame catalog, reconnect semantics
- [Worked Examples](https://mosaic.byexec.com/docs/api/examples/) — curl, Python, JavaScript end-to-end flows

### Operations

- [Security](https://mosaic.byexec.com/docs/security/) — auth model, audit history, two follow-ups (cert SANs, signed manifest) before non-LAN exposure
- [Auto-Update](https://mosaic.byexec.com/docs/auto-update/) — channel, manifest verification, per-platform asset mapping

## Development

```sh
# Live dev (frontend hot reload + native shell)
wails dev

# Production build
wails build
```

Backend is a Go module at the repo root. Frontend is under `frontend/` (Vite + SolidJS). The remote HTTP layer is `backend/remote/`; the engine wrapper is `backend/engine/`; the typed transport surface is `frontend/src/lib/{transport,bindings}.ts`. See [Architecture](https://mosaic.byexec.com/docs/architecture/) for the full map.

## Reporting issues

Bugs and feature requests: [exec/mosaic/issues](https://github.com/exec/mosaic/issues).

Security: open a private advisory at [exec/mosaic/security/advisories/new](https://github.com/exec/mosaic/security/advisories/new) rather than a public issue. See the [Security](https://mosaic.byexec.com/docs/security/) page for the audit history and current TODO list.

## License

© 2026 Dylan Hart. See [LICENSE](./LICENSE) once added; otherwise all rights reserved pending an explicit license declaration.
