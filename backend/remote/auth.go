package remote

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"mosaic/backend/api"
	"mosaic/backend/remote/cred"
)

// ErrTooManySessions is returned by SessionStore.Create when the in-memory
// store is at capacity. Login translates it to 503 so the operator gets a
// distinct signal from a generic 500 (and an attacker pumping sessions
// doesn't quietly evict a real user's token).
var ErrTooManySessions = errors.New("session store at capacity")

// HashPassword/VerifyPassword/RandomToken are re-exported from the cred leaf
// subpackage. The split exists to avoid an import cycle: api.Service uses
// these primitives, while the remote HTTP layer in turn imports api for DTOs
// and Service.
var (
	HashPassword   = cred.HashPassword
	VerifyPassword = cred.VerifyPassword
	RandomToken    = cred.RandomToken
)

const (
	sessionCookieName = "mosaic_session"
	sessionTTL        = 12 * time.Hour
	// maxSessions caps the SessionStore so a flood of logins (or stale tokens
	// piling up) can't grow memory without bound. When full, the oldest
	// (earliest-expiring) entry is evicted. 100 is plenty for an interactive
	// single-user web UI.
	maxSessions = 100
)

// sessionEntry binds a session token to the user it authenticates and its
// expiry. Sessions are user-scoped so a password/role change for one user can
// revoke just their sessions (RevokeUser) without logging everyone out.
type sessionEntry struct {
	userID  int
	expires time.Time
}

// SessionStore holds active session tokens in memory. Tokens reset on process
// restart; there is no persistence requirement.
type SessionStore struct {
	mu       sync.RWMutex
	sessions map[string]sessionEntry // token → entry
}

func NewSessionStore() *SessionStore {
	return &SessionStore{sessions: make(map[string]sessionEntry)}
}

// Create issues a new session token bound to userID. If the store is full it
// returns ErrTooManySessions — the previous behavior (silently evicting the
// oldest entry) let a flood of logins quietly knock out a legitimate user's
// session, which is itself an availability attack. Returns ("", err) if the
// rand source fails.
//
// First it opportunistically reaps expired entries, since expiry pruning is
// otherwise lazy (see Valid) and a store can sit "full" of dead tokens for
// hours under the sliding-TTL model.
func (s *SessionStore) Create(userID int) (string, error) {
	tok, err := RandomToken()
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.sessions) >= maxSessions {
		s.reapExpiredLocked()
	}
	if len(s.sessions) >= maxSessions {
		return "", ErrTooManySessions
	}
	s.sessions[tok] = sessionEntry{userID: userID, expires: time.Now().Add(sessionTTL)}
	return tok, nil
}

// reapExpiredLocked drops every entry past its expiry. Caller must hold
// s.mu (write).
func (s *SessionStore) reapExpiredLocked() {
	now := time.Now()
	for tok, e := range s.sessions {
		if now.After(e.expires) {
			delete(s.sessions, tok)
		}
	}
}

// Valid returns the user id the token authenticates, and ok=false if the
// token is unknown or expired. On a successful lookup the entry's expiry is
// slid forward by sessionTTL (rolling window) so an active user is never
// abruptly logged out mid-session at the 12h mark.
func (s *SessionStore) Valid(token string) (int, bool) {
	if token == "" {
		return 0, false
	}
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.sessions[token]
	if !ok {
		return 0, false
	}
	if now.After(e.expires) {
		delete(s.sessions, token)
		return 0, false
	}
	e.expires = now.Add(sessionTTL)
	s.sessions[token] = e
	return e.userID, true
}

func (s *SessionStore) Delete(token string) {
	s.mu.Lock()
	delete(s.sessions, token)
	s.mu.Unlock()
}

// RevokeAll drops every session.
func (s *SessionStore) RevokeAll() {
	s.mu.Lock()
	s.sessions = make(map[string]sessionEntry)
	s.mu.Unlock()
}

// RevokeUser drops every session belonging to one user. Called from
// api.Service when that user's password, username, role or permissions change.
func (s *SessionStore) RevokeUser(userID int) {
	s.mu.Lock()
	for tok, e := range s.sessions {
		if e.userID == userID {
			delete(s.sessions, tok)
		}
	}
	s.mu.Unlock()
}

// Count returns the number of active sessions. Exposed for tests + status.
func (s *SessionStore) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.sessions)
}

// CallerResolver is the subset of *api.Service the auth layer needs to turn a
// session's user id or a bearer API key into an authenticated caller identity.
// Defining it as an interface keeps the seam testable.
type CallerResolver interface {
	CallerForUserID(ctx context.Context, id int) (api.Caller, error)
	AuthenticateAPIKey(ctx context.Context, key string) (api.Caller, error)
}

func SetSessionCookie(w http.ResponseWriter, token string, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		// Strict — the SPA is same-origin so we never need the cookie sent on
		// cross-site navigations, and Strict is the strongest CSRF defense for
		// this cookie.
		SameSite: http.SameSiteStrictMode,
		MaxAge:   int(sessionTTL.Seconds()),
	})
}

func ClearSessionCookie(w http.ResponseWriter, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
	})
}

func SessionTokenFromRequest(r *http.Request) string {
	c, err := r.Cookie(sessionCookieName)
	if err != nil {
		return ""
	}
	return c.Value
}

// BearerTokenFromRequest extracts a bearer token from the Authorization
// header. The legacy `?key=<token>` query-param form was removed: URL params
// leak into Referer headers, reverse-proxy access logs, and browser history,
// so passing a credential there is unsafe. The SPA's WebSocket uses the
// session cookie; scripted clients use the Authorization header.
func BearerTokenFromRequest(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(auth, "Bearer "))
	}
	return ""
}

// resolveCaller authenticates a request via its session cookie or bearer API
// key and returns the caller identity. ok=false means unauthenticated.
func resolveCaller(r *http.Request, sessions *SessionStore, res CallerResolver) (api.Caller, bool) {
	if uid, valid := sessions.Valid(SessionTokenFromRequest(r)); valid {
		if c, err := res.CallerForUserID(r.Context(), uid); err == nil {
			return c, true
		}
	}
	if key := BearerTokenFromRequest(r); key != "" {
		if c, err := res.AuthenticateAPIKey(r.Context(), key); err == nil {
			return c, true
		}
	}
	return api.Caller{}, false
}

// AuthGate is the auth middleware. It authenticates the request (session
// cookie OR bearer API key), installs the resolved caller on the request
// context for downstream handlers + the Service, and returns 401 JSON if the
// request is unauthenticated.
func AuthGate(sessions *SessionStore, res CallerResolver) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			caller, ok := resolveCaller(r, sessions, res)
			if !ok {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
				return
			}
			next.ServeHTTP(w, r.WithContext(api.WithCaller(r.Context(), caller)))
		})
	}
}
