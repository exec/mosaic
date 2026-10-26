package remote

import (
	"encoding/json"
	"github.com/stretchr/testify/require"
	"mosaic/backend/api"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPortableToolsRoutesAndValidation(t *testing.T) {
	f := newFixture(t)
	key, err := f.svc.RotateAPIKey(sysCtx())
	require.NoError(t, err)
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, authedReq(t, key, http.MethodGet, "/api/settings/backup", nil))
	require.Equal(t, 200, rec.Code, rec.Body.String())
	var b api.SettingsBackupDTO
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &b))
	b.Limits.DownKbps = 50
	rec = httptest.NewRecorder()
	f.router.ServeHTTP(rec, authedReq(t, key, http.MethodPut, "/api/settings/backup", b))
	require.Equal(t, 200, rec.Code, rec.Body.String())
	for _, bad := range []any{map[string]any{"version": 1}, map[string]any{"version": 1, "secret": "ignored"}} {
		rec = httptest.NewRecorder()
		f.router.ServeHTTP(rec, authedReq(t, key, http.MethodPut, "/api/settings/backup", bad))
		require.Equal(t, 400, rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	f.router.ServeHTTP(rec, authedReq(t, key, http.MethodPost, "/api/rss/preview", map[string]any{"regex": "["}))
	require.Equal(t, 400, rec.Code, rec.Body.String())
	rec = httptest.NewRecorder()
	f.router.ServeHTTP(rec, authedReq(t, key, http.MethodGet, "/api/connection/diagnostics", nil))
	require.Equal(t, 200, rec.Code, rec.Body.String())
	rec = httptest.NewRecorder()
	f.router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/settings/backup", nil))
	require.Equal(t, 401, rec.Code)
}
