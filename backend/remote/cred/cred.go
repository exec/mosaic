// Package cred holds password-hashing + random-token primitives used by both
// api.Service (which imports this leaf) and the remote HTTP layer (which
// re-exports). Splitting them out avoids an import cycle between api and
// remote.
package cred

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
)

// randReader is the source of cryptographic randomness for HashPassword and
// RandomToken. It is a package var so tests can swap in a deterministic /
// failure-injecting reader. Production code uses crypto/rand.Reader.
var randReader io.Reader = rand.Reader

// SetRandReader swaps the package-level rand source. Returns a restore func.
// Tests use this to exercise the rand-failure path on RandomToken/HashPassword.
func SetRandReader(r io.Reader) (restore func()) {
	prev := randReader
	randReader = r
	return func() { randReader = prev }
}

// HashPassword returns a phc-string of form
// $argon2id$v=19$m=65536,t=3,p=2$<salt>$<hash>
// using OWASP-recommended parameters.
func HashPassword(plain string) (string, error) {
	if plain == "" {
		return "", errors.New("empty password")
	}
	salt := make([]byte, 16)
	if _, err := io.ReadFull(randReader, salt); err != nil {
		return "", err
	}
	hash := argon2.IDKey([]byte(plain), salt, 3, 64*1024, 2, 32)
	return fmt.Sprintf("$argon2id$v=19$m=65536,t=3,p=2$%s$%s",
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(hash)), nil
}

// HashPasswordWithParams is like HashPassword but lets the caller pin the
// argon2id cost parameters (used by tests that need to encode hashes with
// non-default m/t/p so VerifyPassword's decoder is exercised).
func HashPasswordWithParams(plain string, time, memory uint32, threads uint8) (string, error) {
	if plain == "" {
		return "", errors.New("empty password")
	}
	salt := make([]byte, 16)
	if _, err := io.ReadFull(randReader, salt); err != nil {
		return "", err
	}
	hash := argon2.IDKey([]byte(plain), salt, time, memory, threads, 32)
	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s",
		memory, time, threads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(hash)), nil
}

// VerifyPassword returns true iff plain matches the PHC-encoded hash. The
// argon2id cost parameters (m, t, p) are parsed from the encoded string so a
// hash produced with non-default costs still verifies correctly.
func VerifyPassword(plain, encoded string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false
	}
	memory, time, threads, ok := parseArgon2Params(parts[3])
	if !ok {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false
	}
	got := argon2.IDKey([]byte(plain), salt, time, memory, threads, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}

// parseArgon2Params decodes the "m=<memory>,t=<time>,p=<threads>" segment of a
// PHC-encoded argon2id hash. Returns ok=false if any field is missing /
// malformed.
func parseArgon2Params(seg string) (memory, time uint32, threads uint8, ok bool) {
	var haveM, haveT, haveP bool
	for _, kv := range strings.Split(seg, ",") {
		eq := strings.IndexByte(kv, '=')
		if eq <= 0 {
			return 0, 0, 0, false
		}
		key, val := kv[:eq], kv[eq+1:]
		n, err := strconv.ParseUint(val, 10, 32)
		if err != nil {
			return 0, 0, 0, false
		}
		switch key {
		case "m":
			memory = uint32(n)
			haveM = true
		case "t":
			time = uint32(n)
			haveT = true
		case "p":
			if n > 255 {
				return 0, 0, 0, false
			}
			threads = uint8(n)
			haveP = true
		}
	}
	if !haveM || !haveT || !haveP {
		return 0, 0, 0, false
	}
	return memory, time, threads, true
}

// RandomToken returns a 32-byte URL-safe random token. Returns an error if the
// underlying rand source fails (callers must propagate or surface a 500).
func RandomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := io.ReadFull(randReader, b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
