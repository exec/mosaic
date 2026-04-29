package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestService_Blocklist_DefaultEmpty(t *testing.T) {
	svc, _ := newTestService(t)
	dto := svc.GetBlocklist(context.Background())
	require.Empty(t, dto.URL)
	require.False(t, dto.Enabled)
	require.Equal(t, int64(0), dto.LastLoadedAt)
	require.Equal(t, 0, dto.Entries)
	require.Empty(t, dto.Error)
}

func TestService_SetBlocklistURL_Disabled_ClearsState(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	require.NoError(t, svc.SetBlocklistURL(ctx, "https://example.com/list.p2p", false))
	dto := svc.GetBlocklist(ctx)
	require.Equal(t, "https://example.com/list.p2p", dto.URL)
	require.False(t, dto.Enabled)
	require.Equal(t, 0, dto.Entries)
}

func TestService_SetBlocklistURL_Enabled_LoadsAndPopulates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("evil:10.0.0.0-10.0.0.255\n# comment\nspy:192.168.1.0-192.168.1.255\n"))
	}))
	t.Cleanup(srv.Close)

	svc, _ := newTestService(t)
	ctx := context.Background()
	require.NoError(t, svc.SetBlocklistURL(ctx, srv.URL, true))

	dto := svc.GetBlocklist(ctx)
	require.True(t, dto.Enabled)
	require.Equal(t, srv.URL, dto.URL)
	require.Greater(t, dto.LastLoadedAt, int64(0))
	require.Equal(t, 3, dto.Entries) // countLines counts '\n' bytes
	require.Empty(t, dto.Error)
}

func TestService_RefreshBlocklist_HTTPFailure_RecordsError(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	// Use a clearly invalid URL to force a transport-level failure quickly.
	err := svc.SetBlocklistURL(ctx, "http://127.0.0.1:1/missing", true)
	require.Error(t, err)

	dto := svc.GetBlocklist(ctx)
	require.True(t, dto.Enabled)
	require.NotEmpty(t, dto.Error)
}

func TestService_RefreshBlocklist_NoURL_ReturnsError(t *testing.T) {
	svc, _ := newTestService(t)
	require.Error(t, svc.RefreshBlocklist(context.Background()))
}
