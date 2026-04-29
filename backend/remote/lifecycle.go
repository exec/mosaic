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

	mu      sync.Mutex
	srv     *http.Server
	cancel  context.CancelFunc
	current api.WebConfigDTO
}

// NewServer constructs a Server bound to the given Service + Hub. dataDir is
// the directory under which the self-signed TLS material is cached
// (dataDir/web-tls/cert.pem + key.pem).
func NewServer(svc *api.Service, hub *Hub, sessions *SessionStore, staticFS fs.FS, dataDir string) *Server {
	return &Server{svc: svc, hub: hub, sessions: sessions, staticFS: staticFS, dataDir: dataDir}
}

// Apply starts, stops, or restarts the server to match cfg.
func (s *Server) Apply(cfg api.WebConfigDTO) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.srv != nil && s.current == cfg {
		return // unchanged
	}

	if s.srv != nil {
		s.shutdownLocked()
	}
	s.current = cfg

	if !cfg.Enabled {
		return
	}

	if err := s.startLocked(cfg); err != nil {
		log.Error().Err(err).Msg("remote: start web interface")
	}
}

// Stop tears down the running server (no-op if not running).
func (s *Server) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.shutdownLocked()
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
	router := Mount(s.svc, s.sessions, s.hub, s.staticFS, useTLS)

	srv := &http.Server{
		Addr:              addr,
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
	}

	if useTLS {
		cert, err := EnsureSelfSignedCert(filepath.Join(s.dataDir, "web-tls"))
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
	s.srv = nil
	s.cancel = nil
}
