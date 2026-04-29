package remote

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"mosaic/backend/api"
	"mosaic/backend/engine"
	"mosaic/backend/persistence"
)

// fixture spins up a real api.Service backed by a fake engine + tmp sqlite.
type fixture struct {
	svc      *api.Service
	fb       *engine.FakeBackend
	sessions *SessionStore
	router   http.Handler
}

func newFixture(t *testing.T) *fixture {
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
	router := Mount(svc, sessions, nil, false)
	return &fixture{svc: svc, fb: fb, sessions: sessions, router: router}
}

func (f *fixture) seedCreds(t *testing.T, user, pass string) {
	t.Helper()
	require.NoError(t, f.svc.SetWebConfig(context.Background(), api.WebConfigDTO{Username: user}))
	require.NoError(t, f.svc.SetWebPassword(context.Background(), pass))
}

func (f *fixture) loginCookie(t *testing.T, user, pass string) *http.Cookie {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"username": user, "password": pass})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/login", bytes.NewReader(body))
	f.router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	for _, c := range rec.Result().Cookies() {
		if c.Name == "mosaic_session" {
			return c
		}
	}
	t.Fatal("login did not set a session cookie")
	return nil
}

func TestServer_LoginRejectsWrongCreds(t *testing.T) {
	f := newFixture(t)
	f.seedCreds(t, "alice", "s3cret")

	body, _ := json.Marshal(map[string]string{"username": "alice", "password": "wrong"})
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/login", bytes.NewReader(body)))
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestServer_LoginAcceptsCorrectCredsAndIssuesCookie(t *testing.T) {
	f := newFixture(t)
	f.seedCreds(t, "alice", "s3cret")

	cookie := f.loginCookie(t, "alice", "s3cret")
	require.NotEmpty(t, cookie.Value)
	require.True(t, f.sessions.Valid(cookie.Value))
}

func TestServer_GatedRouteRejectsAnonymous(t *testing.T) {
	f := newFixture(t)
	f.seedCreds(t, "alice", "s3cret")

	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/torrents", nil))
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestServer_GatedRouteAcceptsSessionCookie(t *testing.T) {
	f := newFixture(t)
	f.seedCreds(t, "alice", "s3cret")
	cookie := f.loginCookie(t, "alice", "s3cret")

	req := httptest.NewRequest(http.MethodGet, "/api/torrents", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var got []api.TorrentDTO
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Empty(t, got)
}

func TestServer_GatedRouteAcceptsBearerAPIKey(t *testing.T) {
	f := newFixture(t)
	key, err := f.svc.RotateAPIKey(context.Background())
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/api/torrents", nil)
	req.Header.Set("Authorization", "Bearer "+key)
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
}

func TestServer_LogoutClearsCookieAndInvalidatesSession(t *testing.T) {
	f := newFixture(t)
	f.seedCreds(t, "alice", "s3cret")
	cookie := f.loginCookie(t, "alice", "s3cret")
	require.True(t, f.sessions.Valid(cookie.Value))

	req := httptest.NewRequest(http.MethodPost, "/api/logout", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	require.False(t, f.sessions.Valid(cookie.Value))
}

func TestServer_AddMagnetThenList(t *testing.T) {
	f := newFixture(t)
	key, _ := f.svc.RotateAPIKey(context.Background())

	body, _ := json.Marshal(map[string]string{"magnet": "magnet:?xt=urn:btih:abc", "save_path": "/tmp"})
	req := httptest.NewRequest(http.MethodPost, "/api/torrents/magnet", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+key)
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	req2 := httptest.NewRequest(http.MethodGet, "/api/torrents", nil)
	req2.Header.Set("Authorization", "Bearer "+key)
	rec2 := httptest.NewRecorder()
	f.router.ServeHTTP(rec2, req2)
	require.Equal(t, http.StatusOK, rec2.Code)

	var rows []api.TorrentDTO
	require.NoError(t, json.Unmarshal(rec2.Body.Bytes(), &rows))
	require.Len(t, rows, 1)
}

func TestServer_PauseResumeRemove(t *testing.T) {
	f := newFixture(t)
	key, _ := f.svc.RotateAPIKey(context.Background())
	id, err := f.svc.AddMagnet(context.Background(), "magnet:?xt=urn:btih:zzz", "/tmp")
	require.NoError(t, err)

	cases := []struct {
		method, path string
	}{
		{http.MethodPost, "/api/torrents/" + string(id) + "/pause"},
		{http.MethodPost, "/api/torrents/" + string(id) + "/resume"},
		{http.MethodDelete, "/api/torrents/" + string(id)},
	}
	for _, c := range cases {
		req := httptest.NewRequest(c.method, c.path, nil)
		req.Header.Set("Authorization", "Bearer "+key)
		rec := httptest.NewRecorder()
		f.router.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, c.path+" body="+rec.Body.String())
	}
	require.Empty(t, f.fb.List())
}

func TestServer_AddTorrentFileMultipart(t *testing.T) {
	f := newFixture(t)
	key, _ := f.svc.RotateAPIKey(context.Background())

	var buf bytes.Buffer
	mw := newMultipart(&buf)
	mw.writeField("save_path", "/tmp")
	mw.writeFile("file", "x.torrent", []byte("d4:infod6:lengthi42e4:name3:abcee"))
	mw.close()

	req := httptest.NewRequest(http.MethodPost, "/api/torrents/file", &buf)
	req.Header.Set("Content-Type", mw.contentType())
	req.Header.Set("Authorization", "Bearer "+key)
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	require.Len(t, f.fb.List(), 1)
}

// ---- minimal multipart helper (avoid pulling mime/multipart into call sites) ----

type multipartW struct {
	w *bytes.Buffer
	c io.Writer
	b string
}

func newMultipart(w *bytes.Buffer) *multipartW {
	m := &multipartW{w: w, b: "BOUND" + RandomToken()[:12]}
	m.c = w
	return m
}

func (m *multipartW) contentType() string { return "multipart/form-data; boundary=" + m.b }

func (m *multipartW) writeField(name, value string) {
	_, _ = m.c.Write([]byte("--" + m.b + "\r\n"))
	_, _ = m.c.Write([]byte("Content-Disposition: form-data; name=\"" + name + "\"\r\n\r\n"))
	_, _ = m.c.Write([]byte(value + "\r\n"))
}

func (m *multipartW) writeFile(name, filename string, body []byte) {
	_, _ = m.c.Write([]byte("--" + m.b + "\r\n"))
	_, _ = m.c.Write([]byte("Content-Disposition: form-data; name=\"" + name + "\"; filename=\"" + filename + "\"\r\nContent-Type: application/octet-stream\r\n\r\n"))
	_, _ = m.c.Write(body)
	_, _ = m.c.Write([]byte("\r\n"))
}

func (m *multipartW) close() { _, _ = m.c.Write([]byte("--" + m.b + "--\r\n")) }
