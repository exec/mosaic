package remote

import (
	"bytes"
	"context"
	"encoding/json"
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
	key, _ := f.svc.RotateAPIKey(context.Background())

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
	key, _ := f.svc.RotateAPIKey(context.Background())

	id, err := f.svc.AddMagnet(context.Background(), "magnet:?xt=urn:btih:tag", "/tmp")
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
	key, _ := f.svc.RotateAPIKey(context.Background())

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
	key, _ := f.svc.RotateAPIKey(context.Background())

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
	key, _ := f.svc.RotateAPIKey(context.Background())
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, authedReq(t, key, http.MethodGet, "/api/stats", nil))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var s api.GlobalStats
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &s))
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
