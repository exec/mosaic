package remote

import (
	"errors"
	"strings"
	"testing"

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
	a, err := s.Create()
	require.NoError(t, err)
	b, err := s.Create()
	require.NoError(t, err)
	require.True(t, s.Valid(a))
	require.True(t, s.Valid(b))
	require.Equal(t, 2, s.Count())

	s.RevokeAll()

	require.False(t, s.Valid(a))
	require.False(t, s.Valid(b))
	require.Equal(t, 0, s.Count())
}
