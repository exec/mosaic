package remote

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"mosaic/backend/remote/cred"
)

func TestHashPassword_RoundTrip(t *testing.T) {
	hash, err := HashPassword("hunter2")
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(hash, "$argon2id$v=19$m=65536,t=3,p=2$"))
	require.True(t, VerifyPassword("hunter2", hash))
}

func TestHashPassword_EmptyRejected(t *testing.T) {
	_, err := HashPassword("")
	require.Error(t, err)
}

func TestVerifyPassword_WrongPassword(t *testing.T) {
	hash, err := HashPassword("correct horse")
	require.NoError(t, err)
	require.False(t, VerifyPassword("battery staple", hash))
}

func TestVerifyPassword_MalformedEncoded(t *testing.T) {
	require.False(t, VerifyPassword("anything", ""))
	require.False(t, VerifyPassword("anything", "not-a-phc-string"))
	require.False(t, VerifyPassword("anything", "$bcrypt$v=19$m=65536,t=3,p=2$abc$def"))
	require.False(t, VerifyPassword("anything", "$argon2id$v=19$m=65536,t=3,p=2$!!!$!!!"))
}

func TestHashPassword_DifferentSaltsProduceDifferentHashes(t *testing.T) {
	a, err := HashPassword("same-password")
	require.NoError(t, err)
	b, err := HashPassword("same-password")
	require.NoError(t, err)
	require.NotEqual(t, a, b, "two hashes of the same password must differ (random salt)")
	require.True(t, VerifyPassword("same-password", a))
	require.True(t, VerifyPassword("same-password", b))
}

func TestRandomToken_NonEmptyAndDistinct(t *testing.T) {
	a, err := RandomToken()
	require.NoError(t, err)
	b, err := RandomToken()
	require.NoError(t, err)
	require.NotEmpty(t, a)
	require.NotEqual(t, a, b)
}

// failingReader is an io.Reader that always returns the wrapped error so we
// can exercise the rand-failure path in HashPassword / RandomToken.
type failingReader struct{ err error }

func (f failingReader) Read(_ []byte) (int, error) { return 0, f.err }

func TestRandomToken_PropagatesRandError(t *testing.T) {
	want := errors.New("rand source unavailable")
	restore := cred.SetRandReader(failingReader{err: want})
	defer restore()

	tok, err := RandomToken()
	require.Error(t, err)
	require.ErrorIs(t, err, want)
	require.Empty(t, tok)
}

func TestVerifyPassword_NonDefaultArgon2Params(t *testing.T) {
	// Encode a hash with cost params different from the HashPassword defaults
	// (m=65536,t=3,p=2). Verify must parse those params back out and re-derive
	// with them — using the hardcoded defaults would produce a wrong digest.
	hash, err := cred.HashPasswordWithParams("hunter2", 2, 32*1024, 1)
	require.NoError(t, err)
	require.Contains(t, hash, "m=32768,t=2,p=1")
	require.True(t, VerifyPassword("hunter2", hash))
	require.False(t, VerifyPassword("wrong", hash))
}

func TestSessionStore_RevokeAllClearsAllTokens(t *testing.T) {
	s := NewSessionStore()
	a, err := s.Create(1)
	require.NoError(t, err)
	b, err := s.Create(1)
	require.NoError(t, err)
	requireValid(t, s, a, 1)
	requireValid(t, s, b, 1)
	require.Equal(t, 2, s.Count())

	s.RevokeAll()

	requireInvalid(t, s, a)
	requireInvalid(t, s, b)
	require.Equal(t, 0, s.Count())
}

// TestSessionStore_ValidSlidesExpiry confirms a successful Valid() lookup
// extends the entry's expiry by sessionTTL (rolling-window auth so an active
// user is never abruptly logged out at the 12h mark).
func TestSessionStore_ValidSlidesExpiry(t *testing.T) {
	s := NewSessionStore()
	tok, err := s.Create(1)
	require.NoError(t, err)

	s.mu.RLock()
	originalExp := s.sessions[tok].expires
	s.mu.RUnlock()

	// Re-validate after a short pause — expiry should slide forward.
	time.Sleep(10 * time.Millisecond)
	_, ok := s.Valid(tok)
	require.True(t, ok)

	s.mu.RLock()
	newExp := s.sessions[tok].expires
	s.mu.RUnlock()
	require.True(t, newExp.After(originalExp), "Valid() must slide expiry forward")
}

// TestSessionStore_PeekDoesNotSlideExpiry confirms the read-only validity
// check used by the WS 30s recheck loop: an idle tab's periodic pings must
// not renew the session forever.
func TestSessionStore_PeekDoesNotSlideExpiry(t *testing.T) {
	s := NewSessionStore()
	tok, err := s.Create(1)
	require.NoError(t, err)

	s.mu.RLock()
	originalExp := s.sessions[tok].expires
	s.mu.RUnlock()

	time.Sleep(10 * time.Millisecond)
	uid, ok := s.Peek(tok)
	require.True(t, ok)
	require.Equal(t, 1, uid)

	s.mu.RLock()
	newExp := s.sessions[tok].expires
	s.mu.RUnlock()
	require.Equal(t, originalExp, newExp, "Peek() must not slide expiry")

	// Unknown and expired tokens are rejected.
	_, ok = s.Peek("nope")
	require.False(t, ok)
	s.mu.Lock()
	s.sessions[tok] = sessionEntry{userID: 1, expires: time.Now().Add(-time.Second)}
	s.mu.Unlock()
	_, ok = s.Peek(tok)
	require.False(t, ok)
}

// TestSessionStore_CreateRejectsAtCapacity confirms that an in-memory full
// store refuses new logins instead of silently evicting the oldest live
// session — closing an availability-attack vector where a flood of logins
// could knock real users out of their sessions.
func TestSessionStore_CreateRejectsAtCapacity(t *testing.T) {
	s := NewSessionStore()
	for i := 0; i < maxSessions; i++ {
		_, err := s.Create(i + 1)
		require.NoError(t, err)
	}
	_, err := s.Create(maxSessions + 1)
	require.ErrorIs(t, err, ErrTooManySessions)
}

// TestSessionStore_PerUserCapEvictsOwnSessions confirms a single account cannot
// exhaust the global pool: once it hits maxSessionsPerUser, each new login
// evicts one of that user's own sessions so the count holds steady at the cap.
func TestSessionStore_PerUserCapEvictsOwnSessions(t *testing.T) {
	s := NewSessionStore()
	toks := make([]string, 0, maxSessionsPerUser+5)
	for i := 0; i < maxSessionsPerUser; i++ {
		tok, err := s.Create(1)
		require.NoError(t, err)
		toks = append(toks, tok)
	}
	require.Equal(t, maxSessionsPerUser, s.Count())

	// Five more logins for the same user: count must stay pinned at the cap.
	for i := 0; i < 5; i++ {
		tok, err := s.Create(1)
		require.NoError(t, err)
		require.Equal(t, maxSessionsPerUser, s.Count())
		toks = append(toks, tok)
	}
	// Exactly maxSessionsPerUser of the issued tokens survive — the rest were
	// evicted. (Which specific ones is timing-dependent under the sliding TTL,
	// so we assert the count, not the identity.)
	valid := 0
	for _, tok := range toks {
		if _, ok := s.Peek(tok); ok {
			valid++
		}
	}
	require.Equal(t, maxSessionsPerUser, valid)
}

// TestSessionStore_PerUserCapDoesNotEvictOthers confirms one user's churn never
// touches another user's sessions — the cap is the whole point of stopping a
// single account from locking everyone else out.
func TestSessionStore_PerUserCapDoesNotEvictOthers(t *testing.T) {
	s := NewSessionStore()
	victim, err := s.Create(2)
	require.NoError(t, err)

	// User 1 logs in well past their own cap.
	for i := 0; i < maxSessionsPerUser+5; i++ {
		_, err := s.Create(1)
		require.NoError(t, err)
	}
	// User 2's session is untouched, and the global pool never overflowed.
	requireValid(t, s, victim, 2)
	require.Equal(t, maxSessionsPerUser+1, s.Count())
}

// TestSessionStore_RevokeUser drops only the targeted user's sessions.
func TestSessionStore_RevokeUser(t *testing.T) {
	s := NewSessionStore()
	u1, err := s.Create(1)
	require.NoError(t, err)
	u2, err := s.Create(2)
	require.NoError(t, err)

	s.RevokeUser(1)

	requireInvalid(t, s, u1)
	requireValid(t, s, u2, 2)
}

func requireValid(t *testing.T, s *SessionStore, token string, wantUser int) {
	t.Helper()
	uid, ok := s.Valid(token)
	require.True(t, ok)
	require.Equal(t, wantUser, uid)
}

func requireInvalid(t *testing.T, s *SessionStore, token string) {
	t.Helper()
	_, ok := s.Valid(token)
	require.False(t, ok)
}
