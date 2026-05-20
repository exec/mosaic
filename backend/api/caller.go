package api

import (
	"context"
	"errors"

	"mosaic/backend/persistence"
)

// ErrForbidden / ErrUnauthorized are sentinel errors returned by Service
// methods when the caller lacks permission. The remote HTTP layer maps them to
// 403 / 401 respectively (see backend/remote/handlers.go).
var (
	ErrForbidden    = errors.New("forbidden")
	ErrUnauthorized = errors.New("unauthorized")
)

// Caller is the identity performing a Service operation. It is carried in the
// request context (WithCaller / CallerFrom): the remote HTTP layer derives it
// from the session cookie or API key in AuthGate; the Wails desktop app and
// internal background workers (RSS poller, scheduler, startup restore) run as
// SystemCaller.
//
// All users share one OS process — this is an application-level pseudo-
// permissions model, not OS isolation.
type Caller struct {
	UserID   int    // users.id; SystemCaller uses the admin row (id 1)
	Username string
	Role     string // persistence.RoleAdmin | persistence.RoleUser
	// System marks the implicit desktop / internal caller. It bypasses every
	// permission check and sees every torrent, regardless of Role.
	System bool

	PermAddTorrents    bool
	PermManageRSS      bool
	PermManageCatTags  bool
	PermChangeSettings bool
	PermShare          bool
}

// SystemCaller is the implicit identity for the Wails desktop app (which has no
// login) and for internal background workers. It maps to the admin user row
// (id 1, always created by migration 0009) so torrents it adds have a valid
// owner foreign key.
var SystemCaller = Caller{
	UserID:             1,
	Username:           "admin",
	Role:               persistence.RoleAdmin,
	System:             true,
	PermAddTorrents:    true,
	PermManageRSS:      true,
	PermManageCatTags:  true,
	PermChangeSettings: true,
	PermShare:          true,
}

// CallerFromUser builds a Caller from a persisted user record.
func CallerFromUser(u persistence.User) Caller {
	return Caller{
		UserID:             u.ID,
		Username:           u.Username,
		Role:               u.Role,
		PermAddTorrents:    u.PermAddTorrents,
		PermManageRSS:      u.PermManageRSS,
		PermManageCatTags:  u.PermManageCatTags,
		PermChangeSettings: u.PermChangeSettings,
		PermShare:          u.PermShare,
	}
}

// IsAdmin reports whether the caller has the admin role (or is the system).
func (c Caller) IsAdmin() bool { return c.System || c.Role == persistence.RoleAdmin }

// SeesAllTorrents reports whether the caller's torrent list is unfiltered.
func (c Caller) SeesAllTorrents() bool { return c.IsAdmin() }

// Permission predicates. Admins (and the system caller) implicitly hold every
// permission regardless of their individual flags.
func (c Caller) CanAddTorrents() bool    { return c.IsAdmin() || c.PermAddTorrents }
func (c Caller) CanManageRSS() bool      { return c.IsAdmin() || c.PermManageRSS }
func (c Caller) CanManageCatTags() bool  { return c.IsAdmin() || c.PermManageCatTags }
func (c Caller) CanChangeSettings() bool { return c.IsAdmin() || c.PermChangeSettings }
func (c Caller) CanShare() bool          { return c.IsAdmin() || c.PermShare }
func (c Caller) CanManageUsers() bool    { return c.IsAdmin() }

type callerCtxKey struct{}

// WithCaller returns a context carrying the given caller identity.
func WithCaller(ctx context.Context, c Caller) context.Context {
	return context.WithValue(ctx, callerCtxKey{}, c)
}

// CallerFrom extracts the caller from a context. Default-deny: a context with
// no caller installed yields a zero-value Caller (no System, no role, no
// perms). Every internal call site that legitimately needs system privileges
// must call WithCaller(ctx, SystemCaller) explicitly — see the RSS poller,
// schedule engine, RestoreOnStartup, and the Wails desktop adapter.
func CallerFrom(ctx context.Context) Caller {
	if c, ok := ctx.Value(callerCtxKey{}).(Caller); ok {
		return c
	}
	return Caller{}
}

// requireAuth is the defense-in-depth check at the top of every Service method
// that requires an authenticated identity. Even with the default-deny
// CallerFrom above, a Service method reached via a context that somehow
// bypassed AuthGate (or an internal call site that forgot WithCaller) is
// rejected here rather than running as the zero-value Caller.
func requireAuth(c Caller) error {
	if !c.System && c.UserID == 0 {
		return ErrUnauthorized
	}
	return nil
}
