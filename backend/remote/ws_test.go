package remote

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"nhooyr.io/websocket"

	"mosaic/backend/api"
	"mosaic/backend/engine"
	"mosaic/backend/persistence"
)

func newWSFixture(t *testing.T) (*api.Service, *SessionStore, *Hub, *httptest.Server) {
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
		persistence.NewUsers(db),
		persistence.NewTorrentAccess(db),
		nil, "/tmp/dl",
	)

	sessions := NewSessionStore()
	hub := NewHub()
	t.Cleanup(hub.Close)

	router := Mount(svc, sessions, hub, nil, false, FlavorDaemon)
	srv := httptest.NewServer(router)
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go hub.Run(ctx)

	return svc, sessions, hub, srv
}

func wsURL(httpURL, path string) string {
	if strings.HasPrefix(httpURL, "https") {
		return "wss" + strings.TrimPrefix(httpURL, "https") + path
	}
	return "ws" + strings.TrimPrefix(httpURL, "http") + path
}

// bearerDialOpts builds DialOptions carrying an Authorization: Bearer header.
// The legacy `?key=` URL-param auth path was removed for security, so WS
// tests must send the key in the header.
func bearerDialOpts(key string, extra ...string) *websocket.DialOptions {
	hdr := map[string][]string{"Authorization": {"Bearer " + key}}
	for i := 0; i+1 < len(extra); i += 2 {
		hdr[extra[i]] = []string{extra[i+1]}
	}
	return &websocket.DialOptions{HTTPHeader: hdr}
}

func TestWS_RejectsUnauthenticated(t *testing.T) {
	_, _, _, srv := newWSFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, _, err := websocket.Dial(ctx, wsURL(srv.URL, "/api/ws"), nil)
	require.Error(t, err)
}

func TestWS_AcceptsBearerKeyAndDeliversTorrentTick(t *testing.T) {
	svc, _, hub, srv := newWSFixture(t)
	key, err := svc.RotateAPIKey(sysCtx())
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, wsURL(srv.URL, "/api/ws"), bearerDialOpts(key))
	require.NoError(t, err)
	defer conn.Close(websocket.StatusNormalClosure, "")

	// Wait until the hub has registered the client before publishing —
	// otherwise the broadcast can fire before the connection is in the map.
	require.Eventually(t, func() bool { return hub.ClientCount() == 1 },
		2*time.Second, 10*time.Millisecond)

	hub.PublishTorrents([]api.TorrentDTO{{ID: "abc", Name: "demo"}})

	_, raw, err := conn.Read(ctx)
	require.NoError(t, err)

	var env Envelope
	require.NoError(t, json.Unmarshal(raw, &env))
	require.Equal(t, "torrents:tick", env.Type)
	require.NotNil(t, env.Payload)
}

func TestWS_AcceptsCookieAuth(t *testing.T) {
	svc, sessions, hub, srv := newWSFixture(t)
	require.NoError(t, svc.SetWebConfig(sysCtx(), api.WebConfigDTO{Username: "alice"}))
	require.NoError(t, svc.SetWebPassword(sysCtx(), "p4ss"))
	// The session must reference a real user — SetWebConfig renamed the
	// seeded admin (id 1) to "alice".
	tok, err := sessions.Create(1)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	hdr := map[string][]string{"Cookie": {"mosaic_session=" + tok}}
	conn, _, err := websocket.Dial(ctx, wsURL(srv.URL, "/api/ws"), &websocket.DialOptions{HTTPHeader: hdr})
	require.NoError(t, err)
	defer conn.Close(websocket.StatusNormalClosure, "")

	require.Eventually(t, func() bool { return hub.ClientCount() == 1 },
		2*time.Second, 10*time.Millisecond)
}

func TestWS_FanOutsToMultipleClients(t *testing.T) {
	svc, _, hub, srv := newWSFixture(t)
	key, _ := svc.RotateAPIKey(sysCtx())

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	dial := func() *websocket.Conn {
		c, _, err := websocket.Dial(ctx, wsURL(srv.URL, "/api/ws"), bearerDialOpts(key))
		require.NoError(t, err)
		return c
	}
	a := dial()
	defer a.Close(websocket.StatusNormalClosure, "")
	b := dial()
	defer b.Close(websocket.StatusNormalClosure, "")

	require.Eventually(t, func() bool { return hub.ClientCount() == 2 },
		2*time.Second, 10*time.Millisecond)

	hub.PublishStats(api.GlobalStats{TotalTorrents: 7})

	for _, c := range []*websocket.Conn{a, b} {
		_, raw, err := c.Read(ctx)
		require.NoError(t, err)
		var env Envelope
		require.NoError(t, json.Unmarshal(raw, &env))
		require.Equal(t, "stats:tick", env.Type)
	}
}

func TestWS_RejectsMismatchedOrigin(t *testing.T) {
	svc, _, _, srv := newWSFixture(t)
	key, err := svc.RotateAPIKey(sysCtx())
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Browser-style upgrade: legitimate auth (bearer key in header) but a
	// mismatched Origin header — exactly what a CSWH attempt looks like. The
	// upgrade must fail because OriginPatterns is pinned to r.Host.
	_, resp, err := websocket.Dial(ctx,
		wsURL(srv.URL, "/api/ws"),
		bearerDialOpts(key, "Origin", "https://evil.example.com"),
	)
	require.Error(t, err, "expected upgrade to fail on mismatched Origin")
	if resp != nil {
		_ = resp.Body.Close()
		require.Equal(t, 403, resp.StatusCode, "expected 403 from upgrade")
	}
}

// TestWS_RevokeUserClosesSocket confirms Hub.RevokeUser hangs up the
// targeted user's live WebSocket. This is the new hook that
// api.invalidateCaller is expected to call when a user is deleted, disabled,
// or has their password changed.
func TestWS_RevokeUserClosesSocket(t *testing.T) {
	svc, _, hub, srv := newWSFixture(t)
	key, _ := svc.RotateAPIKey(sysCtx())

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, wsURL(srv.URL, "/api/ws"), bearerDialOpts(key))
	require.NoError(t, err)
	defer conn.Close(websocket.StatusNormalClosure, "")
	require.Eventually(t, func() bool { return hub.ClientCount() == 1 },
		2*time.Second, 10*time.Millisecond)

	hub.RevokeUser(1)

	// Read should now return with a non-nil error within a short window —
	// the server-side close races the client's read loop.
	readErrCh := make(chan error, 1)
	go func() {
		_, _, err := conn.Read(ctx)
		readErrCh <- err
	}()
	select {
	case err := <-readErrCh:
		require.Error(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("expected revoked WS to be closed")
	}
	require.Eventually(t, func() bool { return hub.ClientCount() == 0 },
		2*time.Second, 10*time.Millisecond)

	// RevokeUser on a userID with no live socket is a no-op.
	hub.RevokeUser(999)
}

func TestWS_RemoveClientOnDisconnect(t *testing.T) {
	svc, _, hub, srv := newWSFixture(t)
	key, _ := svc.RotateAPIKey(sysCtx())

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, wsURL(srv.URL, "/api/ws"), bearerDialOpts(key))
	require.NoError(t, err)
	require.Eventually(t, func() bool { return hub.ClientCount() == 1 },
		2*time.Second, 10*time.Millisecond)

	conn.Close(websocket.StatusNormalClosure, "bye")
	require.Eventually(t, func() bool { return hub.ClientCount() == 0 },
		2*time.Second, 10*time.Millisecond)
}
