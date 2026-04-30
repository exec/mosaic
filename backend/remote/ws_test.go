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
		nil, "/tmp/dl",
	)

	sessions := NewSessionStore()
	hub := NewHub()
	t.Cleanup(hub.Close)

	router := Mount(svc, sessions, hub, nil, false)
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

func TestWS_RejectsUnauthenticated(t *testing.T) {
	_, _, _, srv := newWSFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, _, err := websocket.Dial(ctx, wsURL(srv.URL, "/api/ws"), nil)
	require.Error(t, err)
}

func TestWS_AcceptsBearerKeyAndDeliversTorrentTick(t *testing.T) {
	svc, _, hub, srv := newWSFixture(t)
	key, err := svc.RotateAPIKey(context.Background())
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, wsURL(srv.URL, "/api/ws?key="+key), nil)
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
	require.NoError(t, svc.SetWebConfig(context.Background(), api.WebConfigDTO{Username: "alice"}))
	require.NoError(t, svc.SetWebPassword(context.Background(), "p4ss"))
	tok, err := sessions.Create()
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
	key, _ := svc.RotateAPIKey(context.Background())

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	dial := func() *websocket.Conn {
		c, _, err := websocket.Dial(ctx, wsURL(srv.URL, "/api/ws?key="+key), nil)
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
	key, err := svc.RotateAPIKey(context.Background())
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Browser-style upgrade: legitimate auth (bearer key in query) but a
	// mismatched Origin header — exactly what a CSWH attempt looks like. The
	// upgrade must fail because OriginPatterns is pinned to r.Host.
	hdr := map[string][]string{"Origin": {"https://evil.example.com"}}
	_, resp, err := websocket.Dial(ctx,
		wsURL(srv.URL, "/api/ws?key="+key),
		&websocket.DialOptions{HTTPHeader: hdr},
	)
	require.Error(t, err, "expected upgrade to fail on mismatched Origin")
	if resp != nil {
		_ = resp.Body.Close()
		require.Equal(t, 403, resp.StatusCode, "expected 403 from upgrade")
	}
}

func TestWS_RemoveClientOnDisconnect(t *testing.T) {
	svc, _, hub, srv := newWSFixture(t)
	key, _ := svc.RotateAPIKey(context.Background())

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, wsURL(srv.URL, "/api/ws?key="+key), nil)
	require.NoError(t, err)
	require.Eventually(t, func() bool { return hub.ClientCount() == 1 },
		2*time.Second, 10*time.Millisecond)

	conn.Close(websocket.StatusNormalClosure, "bye")
	require.Eventually(t, func() bool { return hub.ClientCount() == 0 },
		2*time.Second, 10*time.Millisecond)
}
