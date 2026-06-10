package remote

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"mosaic/backend/api"
)

func authedReq(t *testing.T, key, method, path string, body any) *http.Request {
	t.Helper()
	var br *bytes.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		require.NoError(t, err)
		br = bytes.NewReader(raw)
	}
	var req *http.Request
	if br != nil {
		req = httptest.NewRequest(method, path, br)
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	return req
}

func TestHandlers_Categories_CRUD(t *testing.T) {
	f := newFixture(t)
	key, _ := f.svc.RotateAPIKey(sysCtx())

	// Create.
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, authedReq(t, key, http.MethodPost, "/api/categories", map[string]string{
		"name": "Movies", "default_save_path": "/m", "color": "#abc",
	}))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var created struct{ ID int }
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &created))
	require.NotZero(t, created.ID)

	// List.
	rec = httptest.NewRecorder()
	f.router.ServeHTTP(rec, authedReq(t, key, http.MethodGet, "/api/categories", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	var cats []api.CategoryDTO
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &cats))
	require.Len(t, cats, 1)
	require.Equal(t, "Movies", cats[0].Name)

	// Update.
	rec = httptest.NewRecorder()
	f.router.ServeHTTP(rec, authedReq(t, key, http.MethodPut, "/api/categories", map[string]any{
		"id": created.ID, "name": "Films", "default_save_path": "/f", "color": "#fff",
	}))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	// Delete.
	rec = httptest.NewRecorder()
	f.router.ServeHTTP(rec, authedReq(t, key, http.MethodDelete, "/api/categories/"+itoa(created.ID), nil))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
}

func TestHandlers_Tags_CRUDAndAssign(t *testing.T) {
	f := newFixture(t)
	key, _ := f.svc.RotateAPIKey(sysCtx())

	id, err := f.svc.AddMagnet(sysCtx(), "magnet:?xt=urn:btih:tag", "/tmp")
	require.NoError(t, err)

	// Create tag.
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, authedReq(t, key, http.MethodPost, "/api/tags", map[string]string{"name": "hd", "color": "#0f0"}))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var got struct{ ID int }
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))

	// Assign.
	rec = httptest.NewRecorder()
	f.router.ServeHTTP(rec, authedReq(t, key, http.MethodPost, "/api/tags/assign", map[string]any{
		"infohash": string(id), "tag_id": got.ID,
	}))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	// Unassign.
	rec = httptest.NewRecorder()
	f.router.ServeHTTP(rec, authedReq(t, key, http.MethodPost, "/api/tags/unassign", map[string]any{
		"infohash": string(id), "tag_id": got.ID,
	}))
	require.Equal(t, http.StatusOK, rec.Code)

	// Delete.
	rec = httptest.NewRecorder()
	f.router.ServeHTTP(rec, authedReq(t, key, http.MethodDelete, "/api/tags/"+itoa(got.ID), nil))
	require.Equal(t, http.StatusOK, rec.Code)
}

func TestHandlers_Limits_GetSetToggle(t *testing.T) {
	f := newFixture(t)
	key, _ := f.svc.RotateAPIKey(sysCtx())

	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, authedReq(t, key, http.MethodPut, "/api/settings/limits", api.LimitsDTO{
		DownKbps: 1000, UpKbps: 100, AltDownKbps: 50, AltUpKbps: 25, AltActive: false,
	}))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	rec = httptest.NewRecorder()
	f.router.ServeHTTP(rec, authedReq(t, key, http.MethodGet, "/api/settings/limits", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	var got api.LimitsDTO
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Equal(t, 1000, got.DownKbps)

	rec = httptest.NewRecorder()
	f.router.ServeHTTP(rec, authedReq(t, key, http.MethodPost, "/api/settings/alt_speed/toggle", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	var toggled struct {
		AltActive bool `json:"alt_active"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &toggled))
	require.True(t, toggled.AltActive)
}

func TestHandlers_WebConfigAndPasswordRotation(t *testing.T) {
	f := newFixture(t)
	key, _ := f.svc.RotateAPIKey(sysCtx())

	// PUT web config.
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, authedReq(t, key, http.MethodPut, "/api/settings/web", api.WebConfigDTO{
		Enabled: true, Port: 9091, BindAll: false, Username: "remote",
	}))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	// GET reflects state.
	rec = httptest.NewRecorder()
	f.router.ServeHTTP(rec, authedReq(t, key, http.MethodGet, "/api/settings/web", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	var cfg api.WebConfigDTO
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &cfg))
	require.Equal(t, "remote", cfg.Username)
	require.Equal(t, 9091, cfg.Port)

	// Set password and verify by logging in.
	rec = httptest.NewRecorder()
	f.router.ServeHTTP(rec, authedReq(t, key, http.MethodPut, "/api/settings/web/password", map[string]string{"password": "p4ssword!"}))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	body, _ := json.Marshal(map[string]string{"username": "remote", "password": "p4ssword!"})
	rec = httptest.NewRecorder()
	f.router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/login", bytes.NewReader(body)))
	require.Equal(t, http.StatusOK, rec.Code)

	// Rotate API key — old key still works for the PUT but a new one comes back.
	rec = httptest.NewRecorder()
	f.router.ServeHTTP(rec, authedReq(t, key, http.MethodPost, "/api/settings/web/api_key/rotate", nil))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var rot struct {
		APIKey string `json:"api_key"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &rot))
	require.NotEmpty(t, rot.APIKey)
	require.NotEqual(t, key, rot.APIKey)
}

func TestHandlers_Stats(t *testing.T) {
	f := newFixture(t)
	key, _ := f.svc.RotateAPIKey(sysCtx())
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, authedReq(t, key, http.MethodGet, "/api/stats", nil))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var s api.GlobalStats
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &s))
}

func TestHandlers_Updater_GetConfig_DefaultsEnabled(t *testing.T) {
	f := newFixture(t)
	key, _ := f.svc.RotateAPIKey(sysCtx())

	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, authedReq(t, key, http.MethodGet, "/api/settings/updater", nil))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var got api.UpdaterConfigDTO
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.True(t, got.Enabled, "default Enabled=true expected")
	require.Equal(t, "stable", got.Channel)
}

func TestHandlers_Updater_SetConfig_RoundTrip(t *testing.T) {
	f := newFixture(t)
	key, _ := f.svc.RotateAPIKey(sysCtx())

	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, authedReq(t, key, http.MethodPut, "/api/settings/updater", api.UpdaterConfigDTO{
		Enabled: false, Channel: "beta",
	}))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	rec = httptest.NewRecorder()
	f.router.ServeHTTP(rec, authedReq(t, key, http.MethodGet, "/api/settings/updater", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	var got api.UpdaterConfigDTO
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.False(t, got.Enabled)
	require.Equal(t, "beta", got.Channel)
}

func TestHandlers_Updater_RejectsUnknownChannel(t *testing.T) {
	f := newFixture(t)
	key, _ := f.svc.RotateAPIKey(sysCtx())

	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, authedReq(t, key, http.MethodPut, "/api/settings/updater", api.UpdaterConfigDTO{
		Enabled: true, Channel: "nightly",
	}))
	require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
}

func TestHandlers_Updater_CheckWithoutUpdater_400s(t *testing.T) {
	// fixture Service has no updater attached → CheckForUpdate returns the
	// "updater disabled" error → writeServiceErr recognizes it as a
	// user-facing validation message and maps it to 400.
	f := newFixture(t)
	key, _ := f.svc.RotateAPIKey(sysCtx())

	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, authedReq(t, key, http.MethodPost, "/api/updater/check", nil))
	require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
}

func TestHandlers_Updater_InstallWithoutUpdater_400s(t *testing.T) {
	f := newFixture(t)
	key, _ := f.svc.RotateAPIKey(sysCtx())

	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, authedReq(t, key, http.MethodPost, "/api/updater/install", nil))
	require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
}

func TestHandlers_Version_OK(t *testing.T) {
	f := newFixture(t)
	key, _ := f.svc.RotateAPIKey(sysCtx())

	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, authedReq(t, key, http.MethodGet, "/api/version", nil))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var got map[string]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	_, ok := got["version"]
	require.True(t, ok, "missing 'version' field")
}

func TestLogin_CookieHasSameSiteStrict(t *testing.T) {
	f := newFixture(t)
	f.seedCreds(t, "alice", "s3cret")

	body, _ := json.Marshal(map[string]string{"username": "alice", "password": "s3cret"})
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/login", bytes.NewReader(body)))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var sessionCookie *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == "mosaic_session" {
			sessionCookie = c
			break
		}
	}
	require.NotNil(t, sessionCookie, "expected session cookie")
	require.Equal(t, http.SameSiteStrictMode, sessionCookie.SameSite)
	require.True(t, sessionCookie.HttpOnly)
}

func TestLogin_RateLimitReturns429AfterFiveFailures(t *testing.T) {
	f := newFixture(t)
	f.seedCreds(t, "alice", "s3cret")

	body, _ := json.Marshal(map[string]string{"username": "alice", "password": "wrong"})

	// Burst is 5: first 5 failed attempts return 401 (invalid creds), then the
	// 6th from the same IP must trip the limiter and return 429.
	for i := 0; i < 5; i++ {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/login", bytes.NewReader(body))
		req.RemoteAddr = "10.0.0.7:54321"
		f.router.ServeHTTP(rec, req)
		require.Equal(t, http.StatusUnauthorized, rec.Code, "attempt %d", i+1)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/login", bytes.NewReader(body))
	req.RemoteAddr = "10.0.0.7:54321"
	f.router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusTooManyRequests, rec.Code, rec.Body.String())
	require.NotEmpty(t, rec.Header().Get("Retry-After"))

	// Different IP gets its own bucket.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/login", bytes.NewReader(body))
	req.RemoteAddr = "10.0.0.8:54321"
	f.router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestOriginGuard_RejectsMismatchedOriginOnPOST(t *testing.T) {
	f := newFixture(t)
	f.seedCreds(t, "alice", "s3cret")
	cookie := f.loginCookie(t, "alice", "s3cret")

	body, _ := json.Marshal(map[string]string{"magnet": "magnet:?xt=urn:btih:bad", "save_path": "/tmp"})
	req := httptest.NewRequest(http.MethodPost, "/api/torrents/magnet", bytes.NewReader(body))
	req.Host = "mosaic.local:8080"
	req.Header.Set("Origin", "https://evil.example.com")
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
}

func TestOriginGuard_AllowsMatchingOriginOnPOST(t *testing.T) {
	f := newFixture(t)
	f.seedCreds(t, "alice", "s3cret")
	cookie := f.loginCookie(t, "alice", "s3cret")

	body, _ := json.Marshal(map[string]string{"magnet": "magnet:?xt=urn:btih:good", "save_path": "/tmp"})
	req := httptest.NewRequest(http.MethodPost, "/api/torrents/magnet", bytes.NewReader(body))
	req.Host = "mosaic.local:8080"
	req.Header.Set("Origin", "https://mosaic.local:8080")
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
}

func TestOriginGuard_BypassedForBearerAuth(t *testing.T) {
	f := newFixture(t)
	key, err := f.svc.RotateAPIKey(sysCtx())
	require.NoError(t, err)

	body, _ := json.Marshal(map[string]string{"magnet": "magnet:?xt=urn:btih:bypass", "save_path": "/tmp"})
	req := httptest.NewRequest(http.MethodPost, "/api/torrents/magnet", bytes.NewReader(body))
	req.Host = "mosaic.local:8080"
	// Bearer-keyed callers are CSRF-immune; the mismatched Origin must NOT
	// cause a rejection.
	req.Header.Set("Origin", "https://evil.example.com")
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
}

func TestOriginGuard_AllowsGETWithMismatchedOrigin(t *testing.T) {
	// GET is not state-changing; the guard must let it through.
	f := newFixture(t)
	key, _ := f.svc.RotateAPIKey(sysCtx())

	req := httptest.NewRequest(http.MethodGet, "/api/torrents", nil)
	req.Host = "mosaic.local:8080"
	req.Header.Set("Origin", "https://evil.example.com")
	req.Header.Set("Authorization", "Bearer "+key)
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
}

// TestCookieSecure_RespectsTrustForwardedProto exercises the
// XFP-aware cookie-Secure decision: when TrustForwardedProto is on, a
// request flagged `X-Forwarded-Proto: https` yields a Secure cookie even
// though the listener itself is plain HTTP.
func TestCookieSecure_RespectsTrustForwardedProto(t *testing.T) {
	h := &Handlers{}
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("X-Forwarded-Proto", "https")
	require.False(t, h.cookieSecure(r), "default off — XFP must be ignored")

	h.SetTrustForwardedProto(true)
	require.True(t, h.cookieSecure(r))

	r2 := httptest.NewRequest(http.MethodGet, "/", nil)
	require.False(t, h.cookieSecure(r2), "no XFP header → still insecure")

	h2 := &Handlers{secure: true}
	require.True(t, h2.cookieSecure(httptest.NewRequest(http.MethodGet, "/", nil)),
		"listener TLS short-circuits regardless of XFP")
}

// TestChangeMyPassword_RateLimitedPerUser confirms a stolen session cookie
// can't be used to grind through the old-password check: after the burst
// budget is exhausted, the endpoint returns 429.
func TestChangeMyPassword_RateLimitedPerUser(t *testing.T) {
	f := newFixture(t)
	f.seedCreds(t, "alice", "s3cret")
	cookie := f.loginCookie(t, "alice", "s3cret")

	body, _ := json.Marshal(map[string]string{
		"old_password": "wrong-guess",
		"new_password": "new-pass-123",
	})
	post := func() *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/me/password", bytes.NewReader(body))
		req.AddCookie(cookie)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", "http://"+req.Host)
		f.router.ServeHTTP(rec, req)
		return rec
	}

	// burst attempts return 400 ("current password is incorrect") because
	// the wrong old password fails validation but the limiter is still
	// charged on each call.
	for i := 0; i < passwordRateBurst; i++ {
		rec := post()
		require.Equal(t, http.StatusBadRequest, rec.Code, "attempt %d body=%s", i+1, rec.Body.String())
	}
	// (burst+1)th attempt must trip the per-user limiter.
	rec := post()
	require.Equal(t, http.StatusTooManyRequests, rec.Code, rec.Body.String())
	require.NotEmpty(t, rec.Header().Get("Retry-After"))
}

// TestClientIP_HonorsXFFFromTrustedProxy walks the rightmost-untrusted-hop
// rule: when the request originates from a trusted proxy CIDR, the rightmost
// X-Forwarded-For entry that isn't itself a trusted proxy is the attributed
// client IP. From an untrusted RemoteAddr the header is ignored.
func TestClientIP_HonorsXFFFromTrustedProxy(t *testing.T) {
	nets, err := ParseTrustedProxiesCIDRs([]string{"10.0.0.0/8"})
	require.NoError(t, err)
	h := &Handlers{trustedProxies: nets}

	// Trusted proxy → walks XFF, skipping trusted hops.
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "10.1.2.3:55555"
	r.Header.Set("X-Forwarded-For", "203.0.113.5, 198.51.100.7, 10.0.0.1")
	require.Equal(t, "198.51.100.7", h.clientIP(r))

	// Untrusted RemoteAddr → XFF ignored.
	r2 := httptest.NewRequest(http.MethodGet, "/", nil)
	r2.RemoteAddr = "203.0.113.99:1234"
	r2.Header.Set("X-Forwarded-For", "1.1.1.1")
	require.Equal(t, "203.0.113.99", h.clientIP(r2))

	// Empty trusted list → XFF always ignored even from loopback.
	h2 := &Handlers{}
	r3 := httptest.NewRequest(http.MethodGet, "/", nil)
	r3.RemoteAddr = "10.1.2.3:55555"
	r3.Header.Set("X-Forwarded-For", "1.1.1.1")
	require.Equal(t, "10.1.2.3", h2.clientIP(r3))
}

// TestParseTrustedProxiesCIDRs covers both the bare-IP shorthand and an
// invalid entry (the safe behavior is to reject the whole list rather than
// silently drop the bad one).
func TestParseTrustedProxiesCIDRs(t *testing.T) {
	nets, err := ParseTrustedProxiesCIDRs([]string{"10.0.0.0/8", "192.0.2.1", "::1"})
	require.NoError(t, err)
	require.Len(t, nets, 3)

	_, err = ParseTrustedProxiesCIDRs([]string{"not-an-ip"})
	require.Error(t, err)
}

// TestDecodeJSON_BodyOver1MiBReturns413 confirms the MaxBytesReader cap
// applied inside decodeJSON: a 2 MiB body is rejected as 413 (Request Entity
// Too Large) rather than silently consuming memory.
func TestDecodeJSON_BodyOver1MiBReturns413(t *testing.T) {
	f := newFixture(t)
	key, _ := f.svc.RotateAPIKey(sysCtx())

	// 2 MiB of payload wrapped as a JSON object — any JSON endpoint will do;
	// AddMagnet is convenient because its struct ignores unknown fields.
	junk := bytes.Repeat([]byte("a"), 2<<20)
	body := append([]byte(`{"junk":"`), junk...)
	body = append(body, []byte(`"}`)...)

	req := httptest.NewRequest(http.MethodPost, "/api/torrents/magnet", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusRequestEntityTooLarge, rec.Code, rec.Body.String())
}

// TestLogin_BodyOver1MiBReturns413BeforeRateLimit confirms the body cap is
// applied to /api/login before the per-IP limiter slot is consumed — i.e. a
// malicious sender of huge bodies can't burn through their own bucket via the
// cheap reject path.
func TestLogin_BodyOver1MiBReturns413BeforeRateLimit(t *testing.T) {
	f := newFixture(t)
	f.seedCreds(t, "alice", "s3cret")

	junk := bytes.Repeat([]byte("a"), 2<<20)
	body := append([]byte(`{"username":"alice","password":"`), junk...)
	body = append(body, []byte(`"}`)...)

	req := httptest.NewRequest(http.MethodPost, "/api/login", bytes.NewReader(body))
	req.RemoteAddr = "10.0.0.99:12345"
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusRequestEntityTooLarge, rec.Code, rec.Body.String())

	// Limiter must still allow a legitimate login from the same IP — the
	// rejected oversized request did not consume a slot.
	good, _ := json.Marshal(map[string]string{"username": "alice", "password": "s3cret"})
	for i := 0; i < 5; i++ {
		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodPost, "/api/login", bytes.NewReader(good))
		req.RemoteAddr = "10.0.0.99:12345"
		f.router.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, "login %d", i+1)
	}
}

// TestOriginGuard_RejectsMissingOriginAndRefererOnPOST is the closure of the
// previously-tolerated "no Origin and no Referer" case for cookie-authed
// state-changing requests. A real browser always sends at least one.
func TestOriginGuard_RejectsMissingOriginAndRefererOnPOST(t *testing.T) {
	f := newFixture(t)
	f.seedCreds(t, "alice", "s3cret")
	cookie := f.loginCookie(t, "alice", "s3cret")

	body, _ := json.Marshal(map[string]string{"magnet": "magnet:?xt=urn:btih:noorigin", "save_path": "/tmp"})
	req := httptest.NewRequest(http.MethodPost, "/api/torrents/magnet", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	// No Origin / Referer headers — this used to slip past the guard.
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
}

// TestOriginGuard_AllowsMissingOriginForBearerAuth confirms the bearer-key
// path still skips the OriginGuard — scripted clients (curl, mosaicd CLI)
// don't set Origin and aren't CSRF-vulnerable.
func TestOriginGuard_AllowsMissingOriginForBearerAuth(t *testing.T) {
	f := newFixture(t)
	key, _ := f.svc.RotateAPIKey(sysCtx())

	body, _ := json.Marshal(map[string]string{"magnet": "magnet:?xt=urn:btih:scripted", "save_path": "/tmp"})
	req := httptest.NewRequest(http.MethodPost, "/api/torrents/magnet", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
}

// TestBearerToken_URLParamNoLongerAuthenticates locks in the removal of the
// `?key=` query-param bearer path. A request that supplies the key only via
// URL must be 401, even if the key itself is valid.
func TestBearerToken_URLParamNoLongerAuthenticates(t *testing.T) {
	f := newFixture(t)
	key, _ := f.svc.RotateAPIKey(sysCtx())

	req := httptest.NewRequest(http.MethodGet, "/api/torrents?key="+key, nil)
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusUnauthorized, rec.Code, rec.Body.String())
}

// TestWriteServiceErr_UnknownErrorMapsTo500 confirms that an unrecognized
// error type drops to 500 with a generic body rather than leaking
// err.Error() into the response.
func TestWriteServiceErr_UnknownErrorMapsTo500(t *testing.T) {
	rec := httptest.NewRecorder()
	// A bespoke error type the allowlist will not recognise. Its message
	// looks like an SQL error string — exactly what we don't want leaking.
	writeServiceErr(rec, errors.New("sql: no rows in result set: /var/lib/mosaic/mosaic.db"))

	require.Equal(t, http.StatusInternalServerError, rec.Code)
	var got map[string]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Equal(t, "internal error", got["error"])
	require.NotContains(t, got["error"], "sql")
	require.NotContains(t, got["error"], "mosaic.db")
}

// TestWriteServiceErr_ValidationErrorKeepsBody confirms a validation-class
// error stays as 400 with its friendly text — the SPA renders this so the
// allowlist must not regress.
func TestWriteServiceErr_ValidationErrorKeepsBody(t *testing.T) {
	rec := httptest.NewRecorder()
	writeServiceErr(rec, errors.New("password must be at least 8 characters"))
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "password must be at least 8 characters")
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	digits := make([]byte, 0, 8)
	for n > 0 {
		digits = append(digits, byte('0'+n%10))
		n /= 10
	}
	for i, j := 0, len(digits)-1; i < j; i, j = i+1, j-1 {
		digits[i], digits[j] = digits[j], digits[i]
	}
	if neg {
		return "-" + string(digits)
	}
	return string(digits)
}
