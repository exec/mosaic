package cred

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// failingReader is an io.Reader that always returns the wrapped error so we
// can exercise the rand-failure path on RandomToken / HashPassword.
type failingReader struct{ err error }

func (f failingReader) Read(_ []byte) (int, error) { return 0, f.err }

func TestRandomToken_PropagatesRandError(t *testing.T) {
	want := errors.New("rand source unavailable")
	restore := SetRandReader(failingReader{err: want})
	defer restore()

	tok, err := RandomToken()
	require.Error(t, err)
	require.ErrorIs(t, err, want)
	require.Empty(t, tok)
}

func TestHashPassword_PropagatesRandError(t *testing.T) {
	want := errors.New("rand source unavailable")
	restore := SetRandReader(failingReader{err: want})
	defer restore()

	hash, err := HashPassword("hunter2")
	require.Error(t, err)
	require.ErrorIs(t, err, want)
	require.Empty(t, hash)
}

func TestVerifyPassword_NonDefaultArgon2Params(t *testing.T) {
	// Encode a hash with cost params different from the HashPassword defaults
	// (m=65536,t=3,p=2). Verify must parse those params back out and re-derive
	// with them — the previous implementation hardcoded m/t/p so this would
	// have produced a wrong digest and returned false.
	hash, err := HashPasswordWithParams("hunter2", 2, 32*1024, 1)
	require.NoError(t, err)
	require.True(t, strings.Contains(hash, "m=32768,t=2,p=1"))

	require.True(t, VerifyPassword("hunter2", hash))
	require.False(t, VerifyPassword("hunter1", hash))
}

func TestVerifyPassword_DefaultParamsStillWork(t *testing.T) {
	hash, err := HashPassword("hunter2")
	require.NoError(t, err)
	require.True(t, VerifyPassword("hunter2", hash))
}

func TestVerifyPassword_RejectsMalformedParamSegment(t *testing.T) {
	// missing t=
	require.False(t, VerifyPassword("anything", "$argon2id$v=19$m=65536,p=2$abc$def"))
	// non-numeric m=
	require.False(t, VerifyPassword("anything", "$argon2id$v=19$m=foo,t=3,p=2$abc$def"))
	// p out of uint8 range
	require.False(t, VerifyPassword("anything", "$argon2id$v=19$m=65536,t=3,p=999$abc$def"))
}
