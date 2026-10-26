package remote

import (
	"encoding/json"
	"github.com/stretchr/testify/require"
	"mosaic/backend/api"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTransferStatisticsEndpoint(t *testing.T) {
	f := newFixture(t)
	key, err := f.svc.RotateAPIKey(sysCtx())
	require.NoError(t, err)
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, authedReq(t, key, http.MethodGet, "/api/statistics/transfers", nil))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var stats api.TransferStatisticsDTO
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &stats))
	require.Positive(t, stats.SessionStartedAt)
	require.NotNil(t, stats.Days)
	rec = httptest.NewRecorder()
	f.router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/statistics/transfers", nil))
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}
