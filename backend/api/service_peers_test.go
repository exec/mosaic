package api

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPeerBlocksPersistRestoreAndUnblock(t *testing.T) {
	svc, fb := newTestService(t)
	ctx := sysCtx()
	require.NoError(t, svc.SetPeerBlocked(ctx, "::ffff:192.0.2.1", true))
	require.NoError(t, svc.SetPeerBlocked(ctx, "192.0.2.1", true))
	require.NoError(t, svc.SetPeerBlocked(ctx, "2001:db8::1", true))
	ips, err := svc.ListBlockedPeers(ctx)
	require.NoError(t, err)
	require.Equal(t, []string{"192.0.2.1", "2001:db8::1"}, ips)
	require.Equal(t, ips, fb.BlockedPeers())
	fb.SetBlockedPeers(nil)
	require.NoError(t, svc.RestoreOnStartup(ctx))
	require.Equal(t, ips, fb.BlockedPeers())
	require.NoError(t, svc.SetPeerBlocked(ctx, "192.0.2.1", false))
	require.Equal(t, []string{"2001:db8::1"}, fb.BlockedPeers())
	require.NoError(t, svc.SetPeerBlocked(ctx, "2001:db8::1", false))
	ips, err = svc.ListBlockedPeers(ctx)
	require.NoError(t, err)
	require.Empty(t, ips)
}

func TestPeerBlocksValidateAndAuthorize(t *testing.T) {
	svc, fb := newTestService(t)
	for _, ip := range []string{"", "example.com", "192.0.2.1:6881", "192.0.2.0/24"} {
		require.Error(t, svc.SetPeerBlocked(sysCtx(), ip, true))
	}
	require.ErrorIs(t, svc.SetPeerBlocked(context.Background(), "192.0.2.1", true), ErrUnauthorized)
	ctx := WithCaller(context.Background(), Caller{UserID: 2, Role: "user"})
	require.ErrorIs(t, svc.SetPeerBlocked(ctx, "192.0.2.1", true), ErrForbidden)
	_, err := svc.ListBlockedPeers(ctx)
	require.ErrorIs(t, err, ErrForbidden)
	require.Empty(t, fb.BlockedPeers())
	ctx = WithCaller(context.Background(), Caller{UserID: 2, Role: "user", PermChangeSettings: true})
	require.NoError(t, svc.SetPeerBlocked(ctx, "192.0.2.1", true))
}
