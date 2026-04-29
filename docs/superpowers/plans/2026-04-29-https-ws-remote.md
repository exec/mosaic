# Mosaic — Plan 6: HTTPS+WS Remote Interface

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make Mosaic accessible from any browser on the LAN (or just localhost) by embedding an HTTPS+WebSocket server in the same process. The user enables "Web interface" in Settings, picks a port, sets a username/password, and either sticks to localhost or opens it to all interfaces (with auto-generated self-signed cert). The server serves the same SolidJS SPA bundled into the binary, plus a JSON+WS API that mirrors the Wails IPC layer 1:1 — so the *exact same UI* runs over HTTP+WS in a browser.

**Architecture:** A new `backend/remote` package wires `chi` for routing, `nhooyr.io/websocket` for the live event stream, Argon2id-hashed passwords, session cookies for browser auth, and a bearer API-key escape hatch for programmatic clients. The HTTP handlers are thin adapters that call the existing `api.Service` methods (the cardinal Plan-1 architectural rule pays off here — zero business logic moves). The frontend gains a small transport abstraction (`lib/transport.ts`) that picks Wails IPC when running inside Wails, HTTP+WS when running in a browser; everything above the transport is identical. Self-signed cert auto-generated on first non-loopback enable; loopback-only allows plain HTTP.

**Tech additions:**
- `github.com/go-chi/chi/v5` — HTTP router
- `nhooyr.io/websocket` — WS server (cleaner than gorilla; same Wails-friendly model)
- `golang.org/x/crypto/argon2` — Argon2id password hashing (already a transitive dep)

**Reference spec:** `docs/superpowers/specs/2026-04-28-bittorrent-client-design.md` §7 (Optional HTTP Remote Access).

**Aesthetic continuity:** Settings → Web Interface pane uses the same Field/PaneHeader pattern. Connection state badge in the StatusBar (existing DHT indicator becomes a row of indicators including "Web ON / OFF").

---

## Out of Scope (deferred to Plan 7+)

- **TLS with custom (user-supplied) cert files** — Plan 6 ships self-signed only; user-cert path deferred
- **Auto-update** — Plan 7
- **Packaging / signing / CI** — Plan 8
- **Multi-user accounts** — single user only; v1 has one username/password

---

## File Structure (final state)

```
backend/
├── remote/
│   ├── server.go                              # NEW: chi router, mount points, cert handling
│   ├── handlers.go                            # NEW: REST handlers calling api.Service
│   ├── ws.go                                  # NEW: WebSocket hub + tick fanout
│   ├── auth.go                                # NEW: session cookies + api-key + Argon2id
│   ├── certs.go                               # NEW: self-signed cert generation
│   ├── server_test.go
│   ├── handlers_test.go
│   └── auth_test.go
├── api/
│   └── service.go                             # MODIFIED: GetWebConfig, SetWebConfig, ChangePassword
└── persistence/
    └── settings.go                            # already covers KV — store new keys

app.go                                         # NEW bindings: GetWebConfig, SetWebConfig, ChangePassword

frontend/src/
├── lib/
│   ├── transport.ts                           # NEW: chooses Wails vs HTTP+WS
│   ├── bindings.ts                            # MODIFIED: routes through transport
│   ├── store.ts                               # MODIFIED: webConfig state
│   └── http_transport.ts                      # NEW: REST + WS implementation
└── components/
    ├── settings/
    │   ├── WebInterfacePane.tsx               # NEW
    │   ├── SettingsSidebar.tsx                # MODIFIED: add Web
    │   └── SettingsRoute.tsx                  # MODIFIED
    └── shell/
        └── StatusBar.tsx                      # MODIFIED: web-server indicator
```

---

## Section A — Backend: cert + auth primitives

### Task 1: Self-signed cert generator

`backend/remote/certs.go`:

```go
package remote

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"
)

// EnsureSelfSignedCert returns a tls.Certificate, generating a fresh ECDSA P-256
// self-signed certificate (10-year validity) under dir/cert.pem + key.pem if not
// already present. The cert covers localhost + 127.0.0.1 + ::1.
func EnsureSelfSignedCert(dir string) (tls.Certificate, error) {
	certPath := filepath.Join(dir, "cert.pem")
	keyPath := filepath.Join(dir, "key.pem")

	if _, err := os.Stat(certPath); err == nil {
		if _, err := os.Stat(keyPath); err == nil {
			return tls.LoadX509KeyPair(certPath, keyPath)
		}
	}

	if err := os.MkdirAll(dir, 0o700); err != nil {
		return tls.Certificate{}, err
	}

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}
	serial, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	template := x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "Mosaic local"},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(10 * 365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{"localhost"},
		IPAddresses:           []net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback},
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		return tls.Certificate{}, err
	}
	if err := os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		return tls.Certificate{}, err
	}
	keyDER, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		return tls.Certificate{}, err
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		return tls.Certificate{}, err
	}
	return tls.LoadX509KeyPair(certPath, keyPath)
}
```

Tests: tmp dir, call twice, assert the SAME serial number both times (proves caching). Commit: `feat(remote): self-signed ECDSA cert generator with cache`.

---

### Task 2: Argon2id password hashing + auth helpers

`backend/remote/auth.go`:

```go
package remote

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// HashPassword returns a phc-string of form
// $argon2id$v=19$m=65536,t=3,p=2$<salt>$<hash>
// using OWASP-recommended parameters.
func HashPassword(plain string) (string, error) {
	if plain == "" { return "", errors.New("empty password") }
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil { return "", err }
	hash := argon2.IDKey([]byte(plain), salt, 3, 64*1024, 2, 32)
	return fmt.Sprintf("$argon2id$v=19$m=65536,t=3,p=2$%s$%s",
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(hash)), nil
}

func VerifyPassword(plain, encoded string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" { return false }
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil { return false }
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil { return false }
	got := argon2.IDKey([]byte(plain), salt, 3, 64*1024, 2, 32)
	return subtle.ConstantTimeCompare(got, want) == 1
}

// RandomToken returns a 32-byte URL-safe random token (used for session
// cookies and API keys).
func RandomToken() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}
```

Tests: Hash/Verify round-trip, wrong-password returns false, malformed encoded returns false, two hashes of same password differ (salt). Commit: `feat(remote): Argon2id password hashing + RandomToken`.

---

### Task 3: api.Service WebConfig methods

In `backend/api/service.go` add:

```go
const (
	settingWebEnabled    = "web_enabled"
	settingWebPort       = "web_port"
	settingWebBindAll    = "web_bind_all"
	settingWebUsername   = "web_username"
	settingWebPassHash   = "web_password_hash"
	settingWebAPIKey     = "web_api_key"
)

type WebConfigDTO struct {
	Enabled  bool   `json:"enabled"`
	Port     int    `json:"port"`
	BindAll  bool   `json:"bind_all"`
	Username string `json:"username"`
	APIKey   string `json:"api_key"` // shown once; rotated on demand
}

func (s *Service) GetWebConfig(ctx context.Context) WebConfigDTO {
	port := s.intSetting(ctx, settingWebPort)
	if port == 0 { port = 8080 }
	user, _ := s.settings.Get(ctx, settingWebUsername)
	if user == "" { user = "admin" }
	key, _ := s.settings.Get(ctx, settingWebAPIKey)
	return WebConfigDTO{
		Enabled:  s.boolSetting(ctx, settingWebEnabled),
		Port:     port,
		BindAll:  s.boolSetting(ctx, settingWebBindAll),
		Username: user,
		APIKey:   key,
	}
}

func (s *Service) SetWebConfig(ctx context.Context, c WebConfigDTO) error {
	if err := s.setBoolSetting(ctx, settingWebEnabled, c.Enabled); err != nil { return err }
	if err := s.setIntSetting(ctx, settingWebPort, c.Port); err != nil { return err }
	if err := s.setBoolSetting(ctx, settingWebBindAll, c.BindAll); err != nil { return err }
	if err := s.settings.Set(ctx, settingWebUsername, c.Username); err != nil { return err }
	return nil
}

func (s *Service) SetWebPassword(ctx context.Context, plain string) error {
	hash, err := remote.HashPassword(plain) // import the remote package
	if err != nil { return err }
	return s.settings.Set(ctx, settingWebPassHash, hash)
}

func (s *Service) RotateAPIKey(ctx context.Context) (string, error) {
	key := remote.RandomToken()
	if err := s.settings.Set(ctx, settingWebAPIKey, key); err != nil { return "", err }
	return key, nil
}

// VerifyWebCredentials returns true if username + password match the stored
// hash; used by the auth middleware on login.
func (s *Service) VerifyWebCredentials(ctx context.Context, username, plain string) bool {
	user, _ := s.settings.Get(ctx, settingWebUsername)
	hash, _ := s.settings.Get(ctx, settingWebPassHash)
	if user == "" || hash == "" { return false }
	if subtle.ConstantTimeCompare([]byte(user), []byte(username)) != 1 { return false }
	return remote.VerifyPassword(plain, hash)
}

func (s *Service) VerifyAPIKey(ctx context.Context, key string) bool {
	stored, _ := s.settings.Get(ctx, settingWebAPIKey)
	if stored == "" || key == "" { return false }
	return subtle.ConstantTimeCompare([]byte(key), []byte(stored)) == 1
}
```

Add `"crypto/subtle"` import + `mosaic/backend/remote` import. Tests cover round-trip + verify. Commit: `feat(api): WebConfig + credential methods`.

---

## Section B — Backend: HTTP server

### Task 4: chi router + handlers

`backend/remote/server.go` constructs a chi router, mounts:
- `POST /api/login` → set session cookie on success
- `POST /api/logout` → clear cookie
- `GET /api/torrents` → svc.ListTorrents
- `POST /api/torrents/magnet` → svc.AddMagnet (JSON body: `{magnet, save_path}`)
- `POST /api/torrents/file` → multipart upload → svc.AddTorrentBytes
- `POST /api/torrents/{id}/pause` / `/resume` / `/remove`
- `GET /api/categories` / `/api/tags` / `/api/feeds` ...
- `GET /api/ws` → WebSocket upgrade
- Static SPA at `/` from `embed.FS` of `frontend/dist`

Use `tower-style` chained middleware: requestLogger, panicRecover, authGate (skips /api/login + ws-with-bearer-key + static).

`handlers.go` wraps each `api.Service` method 1:1 with JSON marshaling. Errors return `{"error": "..."}` with appropriate status code.

Commit: `feat(remote): chi router + handlers mirroring api.Service`.

---

### Task 5: WebSocket hub

`backend/remote/ws.go`:

WebSocket upgrade reads from a hub that subscribes to:
- `torrents:tick` (500ms) — same payload as Wails event
- `stats:tick` (1s)
- `inspector:tick` (1s when focused)

The hub has a small fan-out: each connected client gets its own goroutine that reads from a buffered channel and writes JSON-encoded `{type, payload}` messages.

The Wails event emit and the WS hub both publish into a shared `events.Bus` (already exists from Plan 1).

Commit: `feat(remote): WebSocket hub with tick fan-out`.

---

### Task 6: Server lifecycle wired into main.go

In `main.go`, after construction of svc, build a `remote.Server` that takes the chi handler + cert dir + tls config. The server spawns a goroutine when GetWebConfig().Enabled is true. Listens on `:port` (loopback only) or `0.0.0.0:port` based on BindAll. Plain HTTP only when `!BindAll`; HTTPS otherwise (auto-cert via `EnsureSelfSignedCert(paths.DataDir + "/web-tls")`).

Add `Service.OnWebConfigChange(callback)` so frontend SetWebConfig flips the running state — restart the server with new config.

Commit: `feat(remote): server lifecycle wired in main.go`.

---

### Task 7: Wails bindings

App.go gains `GetWebConfig`, `SetWebConfig`, `SetWebPassword`, `RotateAPIKey`. Regenerate. Commit: `feat: Wails bindings for web interface config`.

---

## Section C — Frontend transport abstraction

### Task 8: lib/transport.ts

```ts
export interface Transport {
  invoke<T>(method: string, ...args: any[]): Promise<T>;
  on(event: string, handler: (data: any) => void): () => void;
}

export const transport: Transport = (typeof window !== 'undefined' && (window as any).runtime)
  ? makeWailsTransport()
  : makeHTTPTransport(window.location.origin);
```

`http_transport.ts` implements REST + WS via fetch + WebSocket. Same shape as the Wails transport.

Refactor `bindings.ts` to call `transport.invoke('AddMagnet', magnet, savePath)` instead of importing from `wailsjs/go/main/App`. The api object stays the same — only its inner calls change.

Commit: `feat(frontend): transport abstraction for Wails IPC vs HTTP+WS`.

---

### Task 9: WebInterfacePane in Settings

`frontend/src/components/settings/WebInterfacePane.tsx`. Toggle Enabled. Port input. Bind-to radio (Localhost only / All interfaces). Username + Password inputs. "Generate API key" button shows the new key once with a copy button. Save calls `store.setWebConfig` + (if password changed) `store.setWebPassword`.

Add 'web' to SettingsSidebar after Connection. SettingsRoute Match. App.tsx threads.

Commit: `feat(frontend): WebInterfacePane + sidebar/route wiring`.

---

### Task 10: StatusBar web indicator

When webConfig.enabled, show `Web ON :port` to the right of DHT online. Click → opens settings web pane.

Commit: `feat(frontend): StatusBar shows web-interface state`.

---

## Section D — Smoke

### Task 11: User-driven smoke

- [ ] Run `wails dev -skipembedcreate`
- [ ] Settings → Web Interface → set username/password, port 8080, Enable. Save.
- [ ] Open `http://localhost:8080` in browser. Login screen appears. Use credentials → SPA loads, identical UI.
- [ ] Add a magnet via the browser UI. Verify it appears in the desktop app too (same backend).
- [ ] Toggle to "All interfaces" + restart-via-toggle. Confirm `https://localhost:8080` (cert warning expected). Click through.
- [ ] Tag `plan-6-remote-complete`, push.

---

**End of Plan 6.**
