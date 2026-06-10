package remote

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"mosaic/backend/api"
)

// Server owns the optional HTTP+WS interface lifecycle. It can be started,
// stopped, and reconfigured at runtime as the user toggles the Web Interface
// settings.
type Server struct {
	svc      *api.Service
	hub      *Hub
	sessions *SessionStore
	staticFS fs.FS
	dataDir  string
	flavor   string

	// trustedProxies / trustForwardedProto are the deployment-time knobs for
	// running mosaicd behind a reverse proxy. Set via SetTrustedProxies and
	// SetTrustForwardedProto before Apply; both default to the safe-when-
	// directly-exposed values (empty / false).
	trustedProxies      []*net.IPNet
	trustForwardedProto bool

	mu      sync.Mutex
	srv     *http.Server
	cancel  context.CancelFunc
	current api.WebConfigDTO
}

// NewServer constructs a Server bound to the given Service + Hub. dataDir is
// the directory under which the self-signed TLS material is cached
// (dataDir/web-tls/cert.pem + key.pem). flavor is FlavorDaemon (mosaicd) or
// FlavorDesktop (the Wails app's optional web server).
func NewServer(svc *api.Service, hub *Hub, sessions *SessionStore, staticFS fs.FS, dataDir, flavor string) *Server {
	return &Server{svc: svc, hub: hub, sessions: sessions, staticFS: staticFS, dataDir: dataDir, flavor: flavor}
}

// SetTrustedProxies configures the reverse-proxy CIDR allowlist consulted by
// the per-IP rate limiters. Must be called before Apply to take effect on the
// first router build; subsequent calls only affect the next restart.
func (s *Server) SetTrustedProxies(nets []*net.IPNet) {
	s.mu.Lock()
	s.trustedProxies = nets
	s.mu.Unlock()
}

// SetTrustForwardedProto enables honouring `X-Forwarded-Proto: https` for
// cookie-Secure decisions. Off by default; turn on only when behind a TLS-
// terminating reverse proxy that sets the header itself (i.e. strips any
// client-supplied value).
func (s *Server) SetTrustForwardedProto(v bool) {
	s.mu.Lock()
	s.trustForwardedProto = v
	s.mu.Unlock()
}

// ParseTrustedProxiesCIDRs converts a list of CIDR strings ("10.0.0.0/8",
// "fe80::/10", ...) to *net.IPNet for SetTrustedProxies. Bare-IP entries
// without a mask are accepted and treated as /32 (or /128 for IPv6) so an
// operator can list individual proxy hosts without arithmetic. A bad entry
// returns an error rather than being silently dropped — a misconfigured
// allowlist would otherwise re-enable XFF spoofing.
func ParseTrustedProxiesCIDRs(cidrs []string) ([]*net.IPNet, error) {
	if len(cidrs) == 0 {
		return nil, nil
	}
	out := make([]*net.IPNet, 0, len(cidrs))
	for _, s := range cidrs {
		if !strings.Contains(s, "/") {
			ip := net.ParseIP(s)
			if ip == nil {
				return nil, fmt.Errorf("trusted proxy %q: not an IP or CIDR", s)
			}
			if ip.To4() != nil {
				s = s + "/32"
			} else {
				s = s + "/128"
			}
		}
		_, n, err := net.ParseCIDR(s)
		if err != nil {
			return nil, fmt.Errorf("trusted proxy %q: %w", s, err)
		}
		out = append(out, n)
	}
	return out, nil
}

// Apply starts, stops, or restarts the server to match cfg. Returns the
// error from net.Listen / startLocked so callers can decide whether the
// failure is fatal (mosaicd's bootstrap: yes, the daemon is useless
// without its web surface) or recoverable (GUI Mosaic during runtime
// re-config: log + surface in UI, don't kill the desktop session).
func (s *Server) Apply(cfg api.WebConfigDTO) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.srv != nil && s.current == cfg {
		return nil // unchanged
	}

	if s.srv != nil {
		s.shutdownLocked()
	}
	s.current = cfg

	if !cfg.Enabled {
		// Full disable: revoke every session so remote credentials stop
		// working the moment the interface is turned off (and don't silently
		// come back to life on a later re-enable). A reconfigure restart
		// (Enabled stays true, port/bind changed) deliberately keeps sessions:
		// the browser reconnects to the new listener with its still-valid
		// cookie instead of forcing everyone through the login screen.
		s.sessions.RevokeAll()
		return nil
	}

	if err := s.startLocked(cfg); err != nil {
		// Log here so we always have a server-side trail even if the
		// caller swallows the returned error.
		log.Error().Err(err).Msg("remote: start web interface")
		return err
	}
	return nil
}

// Stop tears down the running server (no-op if not running) and revokes all
// sessions — it is a full stop, not a restart, so no remote credential should
// outlive it.
func (s *Server) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.shutdownLocked()
	s.sessions.RevokeAll()
}

// CurrentAddr returns the addr the server is bound to ("" if not running).
func (s *Server) CurrentAddr() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.srv == nil {
		return ""
	}
	return s.srv.Addr
}

func (s *Server) startLocked(cfg api.WebConfigDTO) error {
	host := "127.0.0.1"
	if cfg.BindAll {
		host = "0.0.0.0"
	}
	addr := net.JoinHostPort(host, fmt.Sprintf("%d", cfg.Port))

	useTLS := cfg.BindAll
	router := MountWithOptions(s.svc, s.sessions, s.hub, s.staticFS, useTLS, s.flavor, MountOptions{
		TrustedProxies:      s.trustedProxies,
		TrustForwardedProto: s.trustForwardedProto,
	})

	srv := &http.Server{
		Addr:              addr,
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
	}

	if useTLS {
		// BindAll is the only path here, so the cert must cover not just
		// loopback but every LAN interface IP a browser on the network
		// might reach us at — otherwise users see a TLS-name-mismatch
		// warning on first connect from anything that isn't localhost.
		cert, err := EnsureSelfSignedCert(filepath.Join(s.dataDir, "web-tls"), LocalInterfaceIPs())
		if err != nil {
			return fmt.Errorf("ensure cert: %w", err)
		}
		srv.TLSConfig = &tls.Config{
			Certificates: []tls.Certificate{cert},
			MinVersion:   tls.VersionTLS12,
		}
	}

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", addr, err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	s.srv = srv
	s.cancel = cancel
	go s.hub.Run(ctx)

	go func() {
		var err error
		if useTLS {
			err = srv.ServeTLS(ln, "", "")
		} else {
			err = srv.Serve(ln)
		}
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error().Err(err).Str("addr", addr).Msg("remote: serve")
		}
	}()

	log.Info().Bool("tls", useTLS).Str("addr", addr).Msg("remote: web interface listening")
	return nil
}

func (s *Server) shutdownLocked() {
	if s.srv == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.srv.Shutdown(ctx); err != nil {
		log.Warn().Err(err).Msg("remote: shutdown")
	}
	if s.cancel != nil {
		s.cancel()
	}
	// Shutdown does not close hijacked connections, so established WebSocket
	// clients would otherwise survive the listener and keep receiving the
	// per-user tick frames pushed directly via sendFrameToUser.
	s.hub.DisconnectAll()
	s.srv = nil
	s.cancel = nil
}
