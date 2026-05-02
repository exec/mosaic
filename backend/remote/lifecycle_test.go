package remote

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/stretchr/testify/require"

	"mosaic/backend/api"
	"mosaic/backend/engine"
	"mosaic/backend/persistence"
)

func newServerFixture(t *testing.T) (*api.Service, *Server) {
	t.Helper()
	db, err := persistence.Open(context.Background(), filepath.Join(t.TempDir(), "t.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	fb := engine.NewFakeBackend()
	eng := engine.NewEngine(fb, 50*time.Millisecond)
	t.Cleanup(func() { _ = eng.Close() })

	svc := api.NewService(eng,
		persistence.NewTorrents(db),
		persistence.NewCategories(db),
		persistence.NewTags(db),
		persistence.NewSettings(db),
		persistence.NewScheduleRules(db),
		persistence.NewFeeds(db),
		persistence.NewFilters(db),
		nil, "/tmp/dl",
	)

	hub := NewHub()
	t.Cleanup(hub.Close)

	dataDir := t.TempDir()
	srv := NewServer(svc, hub, NewSessionStore(), nil, dataDir)
	t.Cleanup(srv.Stop)
	return svc, srv
}

// freePort returns an available TCP port on localhost. Brief race window
// between unbind and re-bind is acceptable for these tests.
func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := ln.Addr().(*net.TCPAddr).Port
	require.NoError(t, ln.Close())
	return port
}

func waitListening(t *testing.T, addr string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		c, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if err == nil {
			_ = c.Close()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("server did not start listening on %s in time", addr)
}

func TestServer_DisabledIsNoOp(t *testing.T) {
	_, srv := newServerFixture(t)
	srv.Apply(api.WebConfigDTO{Enabled: false, Port: 9000})
	require.Empty(t, srv.CurrentAddr())
}

func TestServer_StartsHTTPOnLoopbackWhenBindAllFalse(t *testing.T) {
	svc, srv := newServerFixture(t)
	port := freePort(t)
	key, err := svc.RotateAPIKey(context.Background())
	require.NoError(t, err)

	srv.Apply(api.WebConfigDTO{Enabled: true, Port: port, BindAll: false})
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	waitListening(t, addr)

	require.Equal(t, addr, srv.CurrentAddr())

	// Plain HTTP — should respond on /api/torrents with bearer.
	req, err := http.NewRequest(http.MethodGet, "http://"+addr+"/api/torrents", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+key)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	body, _ := io.ReadAll(resp.Body)
	var rows []api.TorrentDTO
	require.NoError(t, json.Unmarshal(body, &rows))
}

func TestServer_StartsHTTPSWhenBindAll(t *testing.T) {
	svc, srv := newServerFixture(t)
	port := freePort(t)
	key, err := svc.RotateAPIKey(context.Background())
	require.NoError(t, err)

	srv.Apply(api.WebConfigDTO{Enabled: true, Port: port, BindAll: true})
	addr := fmt.Sprintf("0.0.0.0:%d", port)
	waitListening(t, fmt.Sprintf("127.0.0.1:%d", port))
	require.Equal(t, addr, srv.CurrentAddr())

	// HTTPS with self-signed cert — skip verification.
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}
	req, _ := http.NewRequest(http.MethodGet, fmt.Sprintf("https://127.0.0.1:%d/api/torrents", port), nil)
	req.Header.Set("Authorization", "Bearer "+key)
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestServer_ApplyRestartsOnPortChange(t *testing.T) {
	_, srv := newServerFixture(t)
	p1 := freePort(t)
	srv.Apply(api.WebConfigDTO{Enabled: true, Port: p1, BindAll: false})
	waitListening(t, fmt.Sprintf("127.0.0.1:%d", p1))

	p2 := freePort(t)
	srv.Apply(api.WebConfigDTO{Enabled: true, Port: p2, BindAll: false})
	waitListening(t, fmt.Sprintf("127.0.0.1:%d", p2))
	require.Contains(t, srv.CurrentAddr(), fmt.Sprintf(":%d", p2))

	// Old port should be free again — we should be able to dial within a
	// short window of the shutdown returning.
	require.Eventually(t, func() bool {
		c, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", p1), 50*time.Millisecond)
		if err != nil {
			return true
		}
		_ = c.Close()
		return false
	}, 2*time.Second, 50*time.Millisecond)
}

func TestServer_ApplyDisablesRunning(t *testing.T) {
	_, srv := newServerFixture(t)
	port := freePort(t)
	srv.Apply(api.WebConfigDTO{Enabled: true, Port: port})
	waitListening(t, fmt.Sprintf("127.0.0.1:%d", port))

	srv.Apply(api.WebConfigDTO{Enabled: false, Port: port})
	require.Empty(t, srv.CurrentAddr())
}

func TestService_OnWebConfigChange_FiresFromSetWebConfig(t *testing.T) {
	svc, srv := newServerFixture(t)
	// Apply now returns an error so callers can decide whether to
	// fatal (mosaicd bootstrap) or log (GUI / runtime re-config).
	// The OnWebConfigChange hook signature is fixed at func(WebConfigDTO),
	// so wrap to discard.
	svc.OnWebConfigChange(func(c api.WebConfigDTO) { _ = srv.Apply(c) })

	// SetWebConfig should now restart srv.
	port := freePort(t)
	require.NoError(t, svc.SetWebConfig(context.Background(), api.WebConfigDTO{
		Enabled: true, Port: port, Username: "alice",
	}))
	waitListening(t, fmt.Sprintf("127.0.0.1:%d", port))
	require.Contains(t, srv.CurrentAddr(), fmt.Sprintf(":%d", port))

	// Disable → server stops.
	require.NoError(t, svc.SetWebConfig(context.Background(), api.WebConfigDTO{
		Enabled: false, Port: port, Username: "alice",
	}))
	require.Eventually(t, func() bool { return srv.CurrentAddr() == "" },
		2*time.Second, 20*time.Millisecond)
}

func TestServer_StaticFSServedAtRoot(t *testing.T) {
	svc, _ := newServerFixture(t)
	hub := NewHub()
	t.Cleanup(hub.Close)
	staticFS := fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("hello world")}}
	srv := NewServer(svc, hub, NewSessionStore(), staticFS, t.TempDir())
	t.Cleanup(srv.Stop)

	port := freePort(t)
	srv.Apply(api.WebConfigDTO{Enabled: true, Port: port, BindAll: false})
	waitListening(t, fmt.Sprintf("127.0.0.1:%d", port))

	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/index.html", port))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	body, _ := io.ReadAll(resp.Body)
	require.Equal(t, "hello world", strings.TrimSpace(string(body)))
}
