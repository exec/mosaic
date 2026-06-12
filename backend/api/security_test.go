package api

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"mosaic/backend/persistence"
)

// TestCallerFrom_EmptyContextIsDenyAll pins the default-deny contract: a
// context that never went through WithCaller yields the zero-value Caller
// (not System, no role, no perms). Internal call sites that legitimately
// need system privileges must install SystemCaller explicitly.
func TestCallerFrom_EmptyContextIsDenyAll(t *testing.T) {
	c := CallerFrom(context.Background())
	require.False(t, c.System)
	require.Zero(t, c.UserID)
	require.Empty(t, c.Role)
	require.False(t, c.IsAdmin())
	require.False(t, c.CanAddTorrents())
	require.False(t, c.CanManageRSS())
	require.False(t, c.CanManageCatTags())
	require.False(t, c.CanChangeSettings())
	require.False(t, c.CanShare())
	require.False(t, c.CanManageUsers())
}

func TestCallerFrom_SystemCallerRoundTrips(t *testing.T) {
	c := CallerFrom(WithCaller(context.Background(), SystemCaller))
	require.True(t, c.System)
	require.True(t, c.IsAdmin())
	require.True(t, c.CanManageUsers())
}

// nonAdminCtx creates a regular-user account and returns its caller context.
func nonAdminCtx(t *testing.T, svc *Service) (context.Context, UserDTO) {
	t.Helper()
	u := mkUser(t, svc, UserInput{
		Username: "regular", Password: "password123", Role: persistence.RoleUser,
		PermAddTorrents: true,
	})
	return asUser(t, svc, u.ID), u
}

func TestAuthz_SetWebConfig_NonAdminForbidden(t *testing.T) {
	svc, _ := newTestService(t)
	ctx, _ := nonAdminCtx(t, svc)
	err := svc.SetWebConfig(ctx, WebConfigDTO{Enabled: true, Port: 9000, Username: "alice"})
	require.ErrorIs(t, err, ErrForbidden)
}

func TestAuthz_GetWebConfig_NonAdminGetsRedactedDTO(t *testing.T) {
	svc, _ := newTestService(t)
	require.NoError(t, svc.SetWebConfig(sysCtx(), WebConfigDTO{
		Enabled: true, Port: 9090, Username: "admin",
	}))
	ctx, _ := nonAdminCtx(t, svc)
	dto := svc.GetWebConfig(ctx)
	require.Equal(t, WebConfigDTO{}, dto, "non-admin must not see admin web config")
}

func TestAuthz_SetWebPassword_NonAdminForbidden(t *testing.T) {
	svc, _ := newTestService(t)
	ctx, _ := nonAdminCtx(t, svc)
	require.ErrorIs(t, svc.SetWebPassword(ctx, "newpw1234"), ErrForbidden)
}

func TestAuthz_RotateAPIKey_NonAdminForbidden(t *testing.T) {
	svc, _ := newTestService(t)
	ctx, _ := nonAdminCtx(t, svc)
	_, err := svc.RotateAPIKey(ctx)
	require.ErrorIs(t, err, ErrForbidden)
}

func TestAuthz_RotateUserAPIKey_SelfAllowed_OtherForbidden(t *testing.T) {
	svc, _ := newTestService(t)
	ctx, self := nonAdminCtx(t, svc)

	// Rotating own key is allowed even without admin.
	key, err := svc.RotateUserAPIKey(ctx, self.ID)
	require.NoError(t, err)
	require.NotEmpty(t, key)

	// Rotating another user's key requires admin.
	other := mkUser(t, svc, UserInput{Username: "other", Password: "password123", Role: persistence.RoleUser})
	_, err = svc.RotateUserAPIKey(ctx, other.ID)
	require.ErrorIs(t, err, ErrForbidden)
}

func TestAuthz_RotateUserAPIKey_UnauthenticatedRejected(t *testing.T) {
	svc, _ := newTestService(t)
	_, err := svc.RotateUserAPIKey(context.Background(), 1)
	require.ErrorIs(t, err, ErrUnauthorized)
}

func TestAuthz_InstallUpdate_NonAdminForbidden(t *testing.T) {
	svc, _ := newTestService(t)
	ctx, _ := nonAdminCtx(t, svc)
	require.ErrorIs(t, svc.InstallUpdate(ctx), ErrForbidden)
}

func TestAuthz_SetUpdaterConfig_NonAdminForbidden(t *testing.T) {
	svc, _ := newTestService(t)
	ctx, _ := nonAdminCtx(t, svc)
	err := svc.SetUpdaterConfig(ctx, UpdaterConfigDTO{Enabled: true, Channel: "stable"})
	require.ErrorIs(t, err, ErrForbidden)
}

func TestAuthz_GetUpdaterConfig_NonAdminGetsEmptyDTO(t *testing.T) {
	svc, _ := newTestService(t)
	ctx, _ := nonAdminCtx(t, svc)
	dto := svc.GetUpdaterConfig(ctx)
	require.Equal(t, UpdaterConfigDTO{}, dto)
}

// settingsDelegateCtx creates a non-admin who DOES hold PermChangeSettings —
// the delegable "tweak preferences" flag — to prove the host-integrity and
// filesystem-reach operations are lifted above it to admin-only.
func settingsDelegateCtx(t *testing.T, svc *Service) context.Context {
	t.Helper()
	u := mkUser(t, svc, UserInput{
		Username: "settings-delegate", Password: "password123", Role: persistence.RoleUser,
		PermChangeSettings: true,
	})
	return asUser(t, svc, u.ID)
}

// TestAuthz_HostOps_RequireAdminNotChangeSettings pins the finding fix: a
// PermChangeSettings holder can still edit ordinary preferences but cannot
// reach the updater (swaps the running binary) or the watch folder (an
// unconfined server directory the daemon reads from and deletes within).
func TestAuthz_HostOps_RequireAdminNotChangeSettings(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := settingsDelegateCtx(t, svc)

	// Sanity: the delegable flag still grants ordinary settings access, so the
	// test below is proving a real boundary, not a wholesale loss of the perm.
	require.NoError(t, svc.SetLimits(ctx, LimitsDTO{DownKbps: 1000, UpKbps: 500}))

	// Host-integrity / filesystem-reach operations are now admin-only.
	require.ErrorIs(t, svc.InstallUpdate(ctx), ErrForbidden)
	require.ErrorIs(t, svc.SetUpdaterConfig(ctx, UpdaterConfigDTO{Enabled: true, Channel: "stable"}), ErrForbidden)
	_, err := svc.CheckForUpdate(ctx)
	require.ErrorIs(t, err, ErrForbidden)
	require.ErrorIs(t, svc.SetWatchFolder(ctx, WatchFolderDTO{Enabled: true, Path: "/etc"}), ErrForbidden)

	// The same operations clear the auth gate for an admin/system caller —
	// InstallUpdate still fails on the disabled test updater, but with a
	// non-Forbidden error, proving the gate let it through.
	require.NoError(t, svc.SetWatchFolder(sysCtx(), WatchFolderDTO{Enabled: false, Path: ""}))
	require.NoError(t, svc.SetUpdaterConfig(sysCtx(), UpdaterConfigDTO{Enabled: true, Channel: "stable"}))
	require.NotErrorIs(t, svc.InstallUpdate(sysCtx()), ErrForbidden)
}

func TestAuthz_SetDesktopIntegration_NonAdminForbidden(t *testing.T) {
	svc, _ := newTestService(t)
	ctx, _ := nonAdminCtx(t, svc)
	err := svc.SetDesktopIntegration(ctx, DesktopIntegrationDTO{TrayEnabled: true})
	require.ErrorIs(t, err, ErrForbidden)
}

func TestAuthz_GetPeerLimits_NonAdminGetsEmptyDTO(t *testing.T) {
	svc, _ := newTestService(t)
	require.NoError(t, svc.SetPeerLimits(sysCtx(), PeerLimitsDTO{
		ListenPort: 6881, MaxPeersPerTorrent: 50, DHTEnabled: true, EncryptionEnabled: true,
	}))
	ctx, _ := nonAdminCtx(t, svc)
	dto := svc.GetPeerLimits(ctx)
	require.Equal(t, PeerLimitsDTO{}, dto, "non-admin must not see peer limits")
}

func TestAuthz_GetBlocklist_NonAdminGetsEmptyDTO(t *testing.T) {
	svc, _ := newTestService(t)
	require.NoError(t, svc.SetBlocklistURL(sysCtx(), "https://example.test/block.p2p", false))
	ctx, _ := nonAdminCtx(t, svc)
	dto := svc.GetBlocklist(ctx)
	require.Empty(t, dto.URL)
}

func TestAuthz_GetBlocklist_StripsCredentialsFromURL(t *testing.T) {
	svc, _ := newTestService(t)
	// Stash a URL with credentials directly into settings — SetBlocklistURL
	// validateFetchURL would reject the localhost form below, but we want to
	// exercise the read-side scrubber regardless of how the URL got there.
	require.NoError(t, svc.settings.Set(sysCtx(), settingBlocklistURL, "https://user:s3cret@example.test/block.p2p"))
	dto := svc.GetBlocklist(sysCtx())
	require.Equal(t, "https://example.test/block.p2p", dto.URL)
	require.NotContains(t, dto.URL, "s3cret")
}

// TestAuthenticate_ConstantTimeOnMiss asserts both the lookup-miss and the
// lookup-hit-wrong-password paths invoke argon2id verify, so an attacker
// cannot enumerate usernames via timing. We measure relative cost; the
// absolute threshold (50% .. 200%) is generous to tolerate CI noise. The
// alternative wall-clock-free check would be a counter, but we don't want a
// production hook just for tests — argon2id at OWASP cost is ~10ms so even
// noisy CI gives a clear signal between "no verify" (microseconds) and
// "verify" (~milliseconds).
func TestAuthenticate_ConstantTimeOnMiss(t *testing.T) {
	svc, _ := newTestService(t)
	mkUser(t, svc, UserInput{Username: "alice", Password: "correct-horse-battery", Role: persistence.RoleUser})

	measure := func(user, pw string) time.Duration {
		start := time.Now()
		_, _ = svc.Authenticate(context.Background(), user, pw)
		return time.Since(start)
	}

	// Warm up — first call after init can pay JIT / page-fault costs.
	measure("alice", "warmup")
	measure("ghost", "warmup")

	const samples = 5
	var hitTotal, missTotal time.Duration
	for i := 0; i < samples; i++ {
		hitTotal += measure("alice", "wrong-password")
		missTotal += measure("ghost", "wrong-password")
	}
	hit := hitTotal / samples
	miss := missTotal / samples

	ratio := float64(miss) / float64(hit)
	require.Greaterf(t, ratio, 0.5,
		"miss path is suspiciously faster than hit path (miss=%v hit=%v) — likely skipping the sentinel verify",
		miss, hit)
	require.Lessf(t, ratio, 2.0,
		"miss path is suspiciously slower than hit path (miss=%v hit=%v)",
		miss, hit)
}

func TestAuthenticate_RejectsUnknownUserWithoutLeakingHit(t *testing.T) {
	svc, _ := newTestService(t)
	_, err := svc.Authenticate(context.Background(), "nonexistent", "any-password")
	require.ErrorIs(t, err, ErrUnauthorized)
}

func TestAuthenticateAPIKey_RejectsUnknownKey(t *testing.T) {
	svc, _ := newTestService(t)
	_, err := svc.AuthenticateAPIKey(context.Background(), "not-a-real-key")
	require.ErrorIs(t, err, ErrUnauthorized)
}

// TestRecoverInvalidAdminPasswordHash_SelfHealsLegacyHash pins the audit-fix
// contract: a non-argon2id password hash on the admin row (e.g. a bcrypt
// string copied verbatim from a pre-0009 web_password_hash setting) is
// cleared at startup so the daemon's ephemeral-password path can mint a
// fresh credential. Without this the admin is permanently locked out.
func TestRecoverInvalidAdminPasswordHash_SelfHealsLegacyHash(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := sysCtx()

	require.NoError(t, svc.users.SetPasswordHash(ctx, adminUserID,
		"$2a$10$abcdefghijklmnopqrstuv.legacybcryptdoesnotverify",
		true))

	require.True(t, svc.IsWebPasswordUserSet(ctx),
		"precondition: admin row reports operator-set with legacy hash")

	require.NoError(t, svc.recoverInvalidAdminPasswordHash(ctx))

	u, err := svc.users.Get(ctx, adminUserID)
	require.NoError(t, err)
	require.Empty(t, u.PasswordHash, "legacy hash must be cleared")
	require.False(t, u.PasswordSet, "password_set must be cleared so mosaicd mints a fresh ephemeral")
}

func TestRecoverInvalidAdminPasswordHash_LeavesValidArgon2id(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := sysCtx()

	require.NoError(t, svc.SetWebPassword(ctx, "operator-chosen-pw"))
	before, err := svc.users.Get(ctx, adminUserID)
	require.NoError(t, err)

	require.NoError(t, svc.recoverInvalidAdminPasswordHash(ctx))

	after, err := svc.users.Get(ctx, adminUserID)
	require.NoError(t, err)
	require.Equal(t, before.PasswordHash, after.PasswordHash, "valid argon2id hash must be left intact")
	require.True(t, after.PasswordSet)
}

func TestRecoverInvalidAdminPasswordHash_NoOpOnEmptyHash(t *testing.T) {
	svc, _ := newTestService(t)
	require.NoError(t, svc.recoverInvalidAdminPasswordHash(sysCtx()))
	u, err := svc.users.Get(sysCtx(), adminUserID)
	require.NoError(t, err)
	require.Empty(t, u.PasswordHash)
}
