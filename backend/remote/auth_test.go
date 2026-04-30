package remote

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
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
	a := RandomToken()
	b := RandomToken()
	require.NotEmpty(t, a)
	require.NotEqual(t, a, b)
}
