package remote

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"

	"mosaic/backend/remote/cred"
)

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
)

// SessionStore holds active session tokens in memory. Tokens reset on process
// restart; v1 has no persistence requirement.
type SessionStore struct {
	mu       sync.RWMutex
	sessions map[string]time.Time // token → expires-at
}

func NewSessionStore() *SessionStore {
	return &SessionStore{sessions: make(map[string]time.Time)}
}

func (s *SessionStore) Create() string {
	tok := RandomToken()
	s.mu.Lock()
	s.sessions[tok] = time.Now().Add(sessionTTL)
	s.mu.Unlock()
	return tok
}

func (s *SessionStore) Valid(token string) bool {
	if token == "" {
		return false
	}
	s.mu.RLock()
	exp, ok := s.sessions[token]
	s.mu.RUnlock()
	if !ok {
		return false
	}
	if time.Now().After(exp) {
		s.mu.Lock()
		delete(s.sessions, token)
		s.mu.Unlock()
		return false
	}
	return true
}

func (s *SessionStore) Delete(token string) {
	s.mu.Lock()
	delete(s.sessions, token)
	s.mu.Unlock()
}

// CredentialChecker is the subset of api.Service that the auth layer needs.
// Defining it here lets tests inject fakes without importing api.
type CredentialChecker interface {
	VerifyWebCredentials(ctx context.Context, username, plain string) bool
	VerifyAPIKey(ctx context.Context, key string) bool
}

func SetSessionCookie(w http.ResponseWriter, token string, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
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
		SameSite: http.SameSiteLaxMode,
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

// BearerTokenFromRequest extracts a bearer token from the Authorization header
// or from a `?key=<token>` query param (browser WS upgrades cannot set headers).
func BearerTokenFromRequest(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(auth, "Bearer "))
	}
	return r.URL.Query().Get("key")
}

// AuthGate is the auth middleware. Allows the request through if the session
// cookie is valid OR a bearer API key matches; otherwise returns 401 JSON.
func AuthGate(sessions *SessionStore, creds CredentialChecker) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if sessions.Valid(SessionTokenFromRequest(r)) {
				next.ServeHTTP(w, r)
				return
			}
			if key := BearerTokenFromRequest(r); key != "" && creds.VerifyAPIKey(r.Context(), key) {
				next.ServeHTTP(w, r)
				return
			}
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		})
	}
}
