package api

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"mosaic/backend/persistence"
	"mosaic/backend/remote/cred"
)

// sentinelPasswordHash is a fixed argon2id PHC string generated once at init
// time. Authenticate / AuthenticateAPIKey verify against it on a user-lookup
// miss so the verify cost is paid on every login attempt — without this an
// attacker can distinguish "user does not exist" from "user exists, wrong
// password" by timing the response.
var sentinelPasswordHash string

func init() {
	h, err := cred.HashPassword("invalid-sentinel-password-do-not-use")
	if err != nil {
		panic("api: sentinel password hash init failed: " + err.Error())
	}
	sentinelPasswordHash = h
}

// UserDTO is the transport shape for an account. It never carries the password
// hash or the API key — only whether each is set, plus a non-secret key hint.
type UserDTO struct {
	ID                 int    `json:"id"`
	Username           string `json:"username"`
	Role               string `json:"role"`
	PermAddTorrents    bool   `json:"perm_add_torrents"`
	PermManageRSS      bool   `json:"perm_manage_rss"`
	PermManageCatTags  bool   `json:"perm_manage_cat_tags"`
	PermChangeSettings bool   `json:"perm_change_settings"`
	PermShare          bool   `json:"perm_share"`
	HasAPIKey          bool   `json:"has_api_key"`
	APIKeyHint         string `json:"api_key_hint"`
	PasswordSet        bool   `json:"password_set"`
	Disabled           bool   `json:"disabled"`
	CreatedAt          int64  `json:"created_at"`
}

// ShareDTO is one access grant on a torrent, with the grantee's username
// resolved for display.
type ShareDTO struct {
	UserID   int    `json:"user_id"`
	Username string `json:"username"`
	Access   string `json:"access"`
}

// BootstrapDTO is the unauthenticated payload the SPA fetches before login to
// learn which build it is talking to. Flavor drives whether the "Web
// Interface" or the "Users" settings pane is shown.
type BootstrapDTO struct {
	Flavor    string `json:"flavor"`     // "daemon" | "desktop"
	Version   string `json:"version"`
	MultiUser bool   `json:"multi_user"` // true for the daemon
}

// UserInput is the create/update payload for an account. For CreateUser,
// Password is required; for UpdateUser it is ignored (use ResetUserPassword).
type UserInput struct {
	Username           string
	Password           string
	Role               string
	PermAddTorrents    bool
	PermManageRSS      bool
	PermManageCatTags  bool
	PermChangeSettings bool
	PermShare          bool
	Disabled           bool
}

func userToDTO(u persistence.User) UserDTO {
	return UserDTO{
		ID:                 u.ID,
		Username:           u.Username,
		Role:               u.Role,
		PermAddTorrents:    u.PermAddTorrents,
		PermManageRSS:      u.PermManageRSS,
		PermManageCatTags:  u.PermManageCatTags,
		PermChangeSettings: u.PermChangeSettings,
		PermShare:          u.PermShare,
		HasAPIKey:          u.APIKeyHash != "",
		APIKeyHint:         u.APIKeyHint,
		PasswordSet:        u.PasswordSet,
		Disabled:           u.Disabled,
		CreatedAt:          u.CreatedAt.Unix(),
	}
}

// applyRoleDefaults fills permission flags from the role preset. An admin
// implicitly holds every permission; a regular user gets the caller-supplied
// flags verbatim.
func applyRoleDefaults(u *persistence.User) {
	if u.Role == persistence.RoleAdmin {
		u.PermAddTorrents = true
		u.PermManageRSS = true
		u.PermManageCatTags = true
		u.PermChangeSettings = true
		u.PermShare = true
	}
}

// Authenticate verifies a username + password and returns the caller identity.
// Returns ErrUnauthorized for unknown users, wrong passwords, accounts with no
// password yet, and disabled accounts.
func (s *Service) Authenticate(ctx context.Context, username, plain string) (Caller, error) {
	u, err := s.users.GetByUsername(ctx, username)
	if err != nil {
		// Constant-time defense against username enumeration — always run a
		// real argon2id verify on a miss so the response timing matches a
		// real user with a wrong password.
		_ = cred.VerifyPassword(plain, sentinelPasswordHash)
		return Caller{}, ErrUnauthorized
	}
	if u.Disabled || u.PasswordHash == "" {
		_ = cred.VerifyPassword(plain, sentinelPasswordHash)
		return Caller{}, ErrUnauthorized
	}
	if !cred.VerifyPassword(plain, u.PasswordHash) {
		return Caller{}, ErrUnauthorized
	}
	return CallerFromUser(u), nil
}

// AuthenticateAPIKey resolves a bearer API key to its owning caller. Returns
// ErrUnauthorized for unknown keys and disabled accounts.
func (s *Service) AuthenticateAPIKey(ctx context.Context, key string) (Caller, error) {
	if key == "" {
		// Pay the same verify cost on an empty key as on a real lookup so
		// callers can't distinguish "no key supplied" from "key not found"
		// by timing.
		_ = cred.VerifyPassword(key, sentinelPasswordHash)
		return Caller{}, ErrUnauthorized
	}
	u, err := s.users.GetByAPIKeyHash(ctx, cred.HashAPIKey(key))
	if err != nil || u.Disabled {
		_ = cred.VerifyPassword(key, sentinelPasswordHash)
		return Caller{}, ErrUnauthorized
	}
	return CallerFromUser(u), nil
}

// CallerForUserID resolves a user id (from a session) to a Caller identity.
// Returns ErrUnauthorized for disabled accounts so a user disabled mid-session
// is locked out on their next request.
//
// The resolved Caller is memoized in s.callers: it is the same for every
// request a user makes until that user is mutated, and rebuilding it from a
// users-table SELECT on every authenticated HTTP request and per-user WS tick
// is pure overhead. Cache entries are evicted by invalidateCaller on any
// authority change, so a disabled/demoted user cannot keep a stale caller —
// only enabled users are ever cached here.
func (s *Service) CallerForUserID(ctx context.Context, id int) (Caller, error) {
	if s.callers != nil {
		if c, ok := s.callers.get(id); ok {
			return c, nil
		}
	}
	u, err := s.users.Get(ctx, id)
	if err != nil {
		return Caller{}, err
	}
	if u.Disabled {
		return Caller{}, ErrUnauthorized
	}
	caller := CallerFromUser(u)
	if s.callers != nil {
		s.callers.put(id, caller)
	}
	return caller, nil
}

// Me returns the calling user's own profile.
func (s *Service) Me(ctx context.Context) (UserDTO, error) {
	caller := CallerFrom(ctx)
	if err := requireAuth(caller); err != nil {
		return UserDTO{}, err
	}
	u, err := s.users.Get(ctx, caller.UserID)
	if err != nil {
		return UserDTO{}, err
	}
	return userToDTO(u), nil
}

// ListUsers returns every account. Admin only.
func (s *Service) ListUsers(ctx context.Context) ([]UserDTO, error) {
	if !CallerFrom(ctx).CanManageUsers() {
		return nil, ErrForbidden
	}
	users, err := s.users.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]UserDTO, 0, len(users))
	for _, u := range users {
		out = append(out, userToDTO(u))
	}
	return out, nil
}

func validateUserInput(in UserInput, requirePassword bool) error {
	if strings.TrimSpace(in.Username) == "" {
		return errors.New("username is required")
	}
	if in.Role != persistence.RoleAdmin && in.Role != persistence.RoleUser {
		return fmt.Errorf("role must be %q or %q", persistence.RoleAdmin, persistence.RoleUser)
	}
	if requirePassword && len(in.Password) < 8 {
		return errors.New("password must be at least 8 characters")
	}
	return nil
}

// CreateUser adds a new account. Admin only.
func (s *Service) CreateUser(ctx context.Context, in UserInput) (UserDTO, error) {
	if !CallerFrom(ctx).CanManageUsers() {
		return UserDTO{}, ErrForbidden
	}
	if err := validateUserInput(in, true); err != nil {
		return UserDTO{}, err
	}
	hash, err := cred.HashPassword(in.Password)
	if err != nil {
		return UserDTO{}, err
	}
	u := persistence.User{
		Username:           strings.TrimSpace(in.Username),
		PasswordHash:       hash,
		PasswordSet:        true,
		Role:               in.Role,
		PermAddTorrents:    in.PermAddTorrents,
		PermManageRSS:      in.PermManageRSS,
		PermManageCatTags:  in.PermManageCatTags,
		PermChangeSettings: in.PermChangeSettings,
		PermShare:          in.PermShare,
		Disabled:           in.Disabled,
	}
	applyRoleDefaults(&u)
	id, err := s.users.Create(ctx, u)
	if err != nil {
		return UserDTO{}, err
	}
	u.ID = id
	created, err := s.users.Get(ctx, id)
	if err != nil {
		return UserDTO{}, err
	}
	return userToDTO(created), nil
}

// UpdateUser changes an account's username, role, permission flags and
// disabled state. Admin only. It refuses any change that would leave the
// system with zero enabled admins.
func (s *Service) UpdateUser(ctx context.Context, id int, in UserInput) (UserDTO, error) {
	if !CallerFrom(ctx).CanManageUsers() {
		return UserDTO{}, ErrForbidden
	}
	if err := validateUserInput(in, false); err != nil {
		return UserDTO{}, err
	}
	u, err := s.users.Get(ctx, id)
	if err != nil {
		return UserDTO{}, err
	}
	// Guard the last admin: block a demotion / disable that locks everyone out.
	losingAdmin := (u.Role == persistence.RoleAdmin && !u.Disabled) &&
		(in.Role != persistence.RoleAdmin || in.Disabled)
	if losingAdmin {
		admins, err := s.users.CountAdmins(ctx)
		if err != nil {
			return UserDTO{}, err
		}
		if admins <= 1 {
			return UserDTO{}, errors.New("cannot demote or disable the last admin")
		}
	}
	u.Username = strings.TrimSpace(in.Username)
	u.Role = in.Role
	u.PermAddTorrents = in.PermAddTorrents
	u.PermManageRSS = in.PermManageRSS
	u.PermManageCatTags = in.PermManageCatTags
	u.PermChangeSettings = in.PermChangeSettings
	u.PermShare = in.PermShare
	u.Disabled = in.Disabled
	applyRoleDefaults(&u)
	if err := s.users.Update(ctx, u); err != nil {
		return UserDTO{}, err
	}
	// A role/permission/disable change must drop that user's live sessions and
	// cached caller so the new (possibly reduced) authority takes effect
	// immediately.
	s.invalidateCaller(id)
	updated, err := s.users.Get(ctx, id)
	if err != nil {
		return UserDTO{}, err
	}
	return userToDTO(updated), nil
}

// DeleteUser removes an account. Admin only. The seeded admin (id 1) cannot be
// deleted, nor can the caller delete themselves. Torrent access grants held by
// the deleted user are cascade-removed; torrents only they owned remain in the
// engine and stay visible to admins.
func (s *Service) DeleteUser(ctx context.Context, id int) error {
	caller := CallerFrom(ctx)
	if !caller.CanManageUsers() {
		return ErrForbidden
	}
	if id == adminUserID {
		return errors.New("the primary admin account cannot be deleted")
	}
	if id == caller.UserID {
		return errors.New("you cannot delete your own account")
	}
	if _, err := s.users.Get(ctx, id); err != nil {
		return err
	}
	if err := s.users.Delete(ctx, id); err != nil {
		return err
	}
	s.invalidateCaller(id)
	return nil
}

// ResetUserPassword sets another user's password (admin "reset password"
// action) and revokes their sessions.
func (s *Service) ResetUserPassword(ctx context.Context, id int, newPassword string) error {
	if !CallerFrom(ctx).CanManageUsers() {
		return ErrForbidden
	}
	if len(newPassword) < 8 {
		return errors.New("password must be at least 8 characters")
	}
	if _, err := s.users.Get(ctx, id); err != nil {
		return err
	}
	if err := s.setUserPasswordHash(ctx, id, newPassword, true); err != nil {
		return err
	}
	s.invalidateCaller(id)
	return nil
}

// ChangeMyPassword lets the calling user change their own password after
// confirming the current one. Their other sessions are revoked.
func (s *Service) ChangeMyPassword(ctx context.Context, oldPassword, newPassword string) error {
	caller := CallerFrom(ctx)
	if err := requireAuth(caller); err != nil {
		return err
	}
	u, err := s.users.Get(ctx, caller.UserID)
	if err != nil {
		return err
	}
	if u.PasswordHash != "" && !cred.VerifyPassword(oldPassword, u.PasswordHash) {
		return errors.New("current password is incorrect")
	}
	if len(newPassword) < 8 {
		return errors.New("password must be at least 8 characters")
	}
	if err := s.setUserPasswordHash(ctx, caller.UserID, newPassword, true); err != nil {
		return err
	}
	s.invalidateCaller(caller.UserID)
	return nil
}

// RotateMyAPIKey mints a fresh API key for the calling user.
func (s *Service) RotateMyAPIKey(ctx context.Context) (string, error) {
	caller := CallerFrom(ctx)
	if err := requireAuth(caller); err != nil {
		return "", err
	}
	return s.RotateUserAPIKey(ctx, caller.UserID)
}

// RotateUserAPIKey mints a fresh API key for a user and returns the cleartext
// (shown to the operator once). Only the hash + a display hint are persisted.
// A user may rotate their own key; only admins may rotate another user's.
func (s *Service) RotateUserAPIKey(ctx context.Context, userID int) (string, error) {
	caller := CallerFrom(ctx)
	if err := requireAuth(caller); err != nil {
		return "", err
	}
	if userID != caller.UserID && !caller.CanManageUsers() {
		return "", ErrForbidden
	}
	key, err := cred.RandomToken()
	if err != nil {
		return "", err
	}
	if err := s.users.SetAPIKey(ctx, userID, cred.HashAPIKey(key), cred.APIKeyHint(key)); err != nil {
		return "", err
	}
	return key, nil
}

// ShareTorrent grants another user viewer or editor access to a torrent. The
// caller must own the torrent and hold the share permission. Owner access
// cannot be handed out via sharing — only Remove transfers/ends ownership.
func (s *Service) ShareTorrent(ctx context.Context, infohash string, targetUserID int, access string) error {
	caller := CallerFrom(ctx)
	if !caller.CanShare() {
		return ErrForbidden
	}
	if err := s.requireTorrentAccess(ctx, infohash, persistence.AccessOwner); err != nil {
		return err
	}
	if access != persistence.AccessViewer && access != persistence.AccessEditor {
		return fmt.Errorf("share access must be %q or %q", persistence.AccessViewer, persistence.AccessEditor)
	}
	if targetUserID == caller.UserID {
		return errors.New("cannot share a torrent with yourself")
	}
	target, err := s.users.Get(ctx, targetUserID)
	if err != nil {
		return errors.New("target user does not exist")
	}
	if target.Disabled {
		return errors.New("cannot share with a disabled account")
	}
	by := caller.UserID
	return s.access.Grant(ctx, infohash, targetUserID, access, &by)
}

// UnshareTorrent revokes another user's access to a torrent. The caller must
// own the torrent. An owner grant cannot be revoked this way.
func (s *Service) UnshareTorrent(ctx context.Context, infohash string, targetUserID int) error {
	if !CallerFrom(ctx).CanShare() {
		return ErrForbidden
	}
	if err := s.requireTorrentAccess(ctx, infohash, persistence.AccessOwner); err != nil {
		return err
	}
	lvl, err := s.access.AccessFor(ctx, infohash, targetUserID)
	if err != nil {
		return err
	}
	if lvl == persistence.AccessOwner {
		return errors.New("cannot unshare an owner; the owner must remove the torrent themselves")
	}
	return s.access.Revoke(ctx, infohash, targetUserID)
}

// ListTorrentShares returns every access grant on a torrent (usernames
// resolved). The caller must own the torrent.
func (s *Service) ListTorrentShares(ctx context.Context, infohash string) ([]ShareDTO, error) {
	if err := s.requireTorrentAccess(ctx, infohash, persistence.AccessOwner); err != nil {
		return nil, err
	}
	rows, err := s.access.ListSharesForTorrent(ctx, infohash)
	if err != nil {
		return nil, err
	}
	out := make([]ShareDTO, 0, len(rows))
	for _, r := range rows {
		out = append(out, ShareDTO{UserID: r.UserID, Username: r.Username, Access: r.Access})
	}
	return out, nil
}

// EnsureAdminUser guarantees the seeded admin account exists. Migration 0009
// always inserts it, but this is a cheap idempotent safety net for the Service
// constructor / startup path.
func (s *Service) EnsureAdminUser(ctx context.Context) error {
	if _, err := s.users.Get(ctx, adminUserID); err == nil {
		return nil
	}
	_, err := s.users.Create(ctx, persistence.User{
		Username:           "admin",
		Role:               persistence.RoleAdmin,
		PermAddTorrents:    true,
		PermManageRSS:      true,
		PermManageCatTags:  true,
		PermChangeSettings: true,
		PermShare:          true,
	})
	return err
}

// recoverInvalidAdminPasswordHash self-heals an admin row whose password_hash
// was copied verbatim from a pre-0009 legacy web_password_hash setting in a
// non-argon2id format (e.g. bcrypt or sha256). Without this the admin would
// be permanently locked out: VerifyPassword only accepts argon2id PHC, and
// users.password_set=1 keeps mosaicd from minting a fresh ephemeral password.
//
// We clear the hash + password_set on detection so the daemon's
// ephemeral-password path runs on the next boot and prints a fresh credential
// banner. Idempotent: a valid argon2id PHC, an empty hash, or a missing admin
// row all no-op.
func (s *Service) recoverInvalidAdminPasswordHash(ctx context.Context) error {
	u, err := s.users.Get(ctx, adminUserID)
	if err != nil {
		return nil
	}
	if u.PasswordHash == "" {
		return nil
	}
	if strings.HasPrefix(u.PasswordHash, "$argon2id$") {
		return nil
	}
	return s.users.SetPasswordHash(ctx, adminUserID, "", false)
}
