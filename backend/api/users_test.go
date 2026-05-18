package api

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"mosaic/backend/persistence"
)

// asUser returns a context whose caller is the given user id, so a test can
// drive Service methods as that user instead of the implicit system caller.
func asUser(t *testing.T, svc *Service, id int) context.Context {
	t.Helper()
	c, err := svc.CallerForUserID(context.Background(), id)
	require.NoError(t, err)
	return WithCaller(context.Background(), c)
}

func mkUser(t *testing.T, svc *Service, in UserInput) UserDTO {
	t.Helper()
	u, err := svc.CreateUser(context.Background(), in)
	require.NoError(t, err)
	return u
}

func TestUsers_CreateAndAuthenticate(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	u := mkUser(t, svc, UserInput{Username: "bob", Password: "password123", Role: persistence.RoleUser})
	require.Equal(t, "bob", u.Username)

	caller, err := svc.Authenticate(ctx, "bob", "password123")
	require.NoError(t, err)
	require.Equal(t, u.ID, caller.UserID)

	_, err = svc.Authenticate(ctx, "bob", "wrong")
	require.ErrorIs(t, err, ErrUnauthorized)

	_, err = svc.Authenticate(ctx, "ghost", "password123")
	require.ErrorIs(t, err, ErrUnauthorized)
}

func TestUsers_DisabledAccountCannotAuthenticate(t *testing.T) {
	svc, _ := newTestService(t)
	u := mkUser(t, svc, UserInput{Username: "bob", Password: "password123", Role: persistence.RoleUser})
	_, err := svc.UpdateUser(context.Background(), u.ID, UserInput{Username: "bob", Role: persistence.RoleUser, Disabled: true})
	require.NoError(t, err)
	_, err = svc.Authenticate(context.Background(), "bob", "password123")
	require.ErrorIs(t, err, ErrUnauthorized)
}

func TestUsers_TorrentVisibilityIsolatedThenShared(t *testing.T) {
	svc, _ := newTestService(t)
	bob := mkUser(t, svc, UserInput{Username: "bob", Password: "password123", Role: persistence.RoleUser, PermAddTorrents: true, PermShare: true})
	carol := mkUser(t, svc, UserInput{Username: "carol", Password: "password123", Role: persistence.RoleUser, PermAddTorrents: true})
	bobCtx := asUser(t, svc, bob.ID)
	carolCtx := asUser(t, svc, carol.ID)

	id, err := svc.AddMagnet(bobCtx, "magnet:?xt=urn:btih:shared", "")
	require.NoError(t, err)

	// bob owns it; carol cannot see it at all.
	bobRows, err := svc.ListTorrents(bobCtx)
	require.NoError(t, err)
	require.Len(t, bobRows, 1)
	require.Equal(t, persistence.AccessOwner, bobRows[0].Access)

	carolRows, err := svc.ListTorrents(carolCtx)
	require.NoError(t, err)
	require.Empty(t, carolRows)

	// carol cannot control a torrent she has no access to.
	require.ErrorIs(t, svc.Pause(carolCtx, id), ErrForbidden)

	// Share as viewer: carol sees it read-only, still cannot control it.
	require.NoError(t, svc.ShareTorrent(bobCtx, string(id), carol.ID, persistence.AccessViewer))
	carolRows, _ = svc.ListTorrents(carolCtx)
	require.Len(t, carolRows, 1)
	require.Equal(t, persistence.AccessViewer, carolRows[0].Access)
	require.ErrorIs(t, svc.Pause(carolCtx, id), ErrForbidden)

	// Upgrade to editor: carol can now control it.
	require.NoError(t, svc.ShareTorrent(bobCtx, string(id), carol.ID, persistence.AccessEditor))
	require.NoError(t, svc.Pause(carolCtx, id))

	// A non-owner "remove" just detaches it from their own view.
	require.NoError(t, svc.Remove(carolCtx, id, false))
	carolRows, _ = svc.ListTorrents(carolCtx)
	require.Empty(t, carolRows)
	bobRows, _ = svc.ListTorrents(bobCtx)
	require.Len(t, bobRows, 1, "owner still has the torrent after a sharee detaches")
}

func TestUsers_AdminSeesAllTorrents(t *testing.T) {
	svc, _ := newTestService(t)
	bob := mkUser(t, svc, UserInput{Username: "bob", Password: "password123", Role: persistence.RoleUser, PermAddTorrents: true})
	bobCtx := asUser(t, svc, bob.ID)
	_, err := svc.AddMagnet(bobCtx, "magnet:?xt=urn:btih:bobs", "")
	require.NoError(t, err)

	// The implicit system/admin caller sees every torrent regardless of grants.
	rows, err := svc.ListTorrents(context.Background())
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, persistence.AccessOwner, rows[0].Access)
}

func TestUsers_PermissionGating(t *testing.T) {
	svc, _ := newTestService(t)
	noAdd := mkUser(t, svc, UserInput{Username: "noadd", Password: "password123", Role: persistence.RoleUser, PermAddTorrents: false})
	noAddCtx := asUser(t, svc, noAdd.ID)

	_, err := svc.AddMagnet(noAddCtx, "magnet:?xt=urn:btih:x", "")
	require.ErrorIs(t, err, ErrForbidden)

	// Global settings need the change-settings permission.
	require.ErrorIs(t, svc.SetLimits(noAddCtx, LimitsDTO{}), ErrForbidden)
	_, err = svc.CreateCategory(noAddCtx, "x", "", "#fff")
	require.ErrorIs(t, err, ErrForbidden)

	// A non-admin cannot manage users.
	_, err = svc.ListUsers(noAddCtx)
	require.ErrorIs(t, err, ErrForbidden)
	_, err = svc.CreateUser(noAddCtx, UserInput{Username: "x", Password: "password123", Role: persistence.RoleUser})
	require.ErrorIs(t, err, ErrForbidden)
}

func TestUsers_AdminGuards(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	// The seeded primary admin (id 1) cannot be deleted.
	require.Error(t, svc.DeleteUser(ctx, adminUserID))

	// The last admin cannot be demoted to a regular user.
	_, err := svc.UpdateUser(ctx, adminUserID, UserInput{Username: "admin", Role: persistence.RoleUser})
	require.Error(t, err)

	// A second admin makes demotion of the first allowed again.
	mkUser(t, svc, UserInput{Username: "admin2", Password: "password123", Role: persistence.RoleAdmin})
	_, err = svc.UpdateUser(ctx, adminUserID, UserInput{Username: "admin", Role: persistence.RoleUser})
	require.NoError(t, err)
}

func TestUsers_ShareRequiresOwnership(t *testing.T) {
	svc, _ := newTestService(t)
	bob := mkUser(t, svc, UserInput{Username: "bob", Password: "password123", Role: persistence.RoleUser, PermAddTorrents: true, PermShare: true})
	carol := mkUser(t, svc, UserInput{Username: "carol", Password: "password123", Role: persistence.RoleUser, PermAddTorrents: true, PermShare: true})
	bobCtx := asUser(t, svc, bob.ID)
	carolCtx := asUser(t, svc, carol.ID)

	id, err := svc.AddMagnet(bobCtx, "magnet:?xt=urn:btih:owned", "")
	require.NoError(t, err)

	// carol does not own the torrent, so she cannot share it.
	require.ErrorIs(t, svc.ShareTorrent(carolCtx, string(id), bob.ID, persistence.AccessViewer), ErrForbidden)

	// Owner access cannot be granted via sharing.
	require.Error(t, svc.ShareTorrent(bobCtx, string(id), carol.ID, persistence.AccessOwner))
}
