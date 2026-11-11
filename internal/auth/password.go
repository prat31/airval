// Package auth implements local accounts (argon2id), cookie sessions with CSRF tokens and API keys.
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// argon2id parameters (RFC 9106 second recommended option, memory-trimmed for small servers).
const (
	argonTime    = 2
	argonMemory  = 64 * 1024 // KiB
	argonThreads = 2
	argonKeyLen  = 32
	saltLen      = 16
)

// MinPasswordLen is enforced on every password set through the API.
const MinPasswordLen = 8

// HashPassword returns an encoded argon2id hash ($argon2id$v=19$m=..,t=..,p=..$salt$hash).
func HashPassword(pw string) (string, error) {
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(pw), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	b64 := base64.RawStdEncoding
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version, argonMemory, argonTime, argonThreads,
		b64.EncodeToString(salt), b64.EncodeToString(key)), nil
}

// VerifyPassword checks pw against an encoded hash in constant time.
func VerifyPassword(encoded, pw string) (bool, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false, errors.New("auth: unsupported password hash")
	}
	var v int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &v); err != nil || v != argon2.Version {
		return false, errors.New("auth: unsupported argon2 version")
	}
	var m, t uint32
	var p uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &m, &t, &p); err != nil {
		return false, errors.New("auth: bad argon2 params")
	}
	b64 := base64.RawStdEncoding
	salt, err := b64.DecodeString(parts[4])
	if err != nil {
		return false, err
	}
	want, err := b64.DecodeString(parts[5])
	if err != nil {
		return false, err
	}
	got := argon2.IDKey([]byte(pw), salt, t, m, p, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}

// dummyHash is verified against when a login email is unknown, so timing doesn't reveal accounts.
var dummyHash, _ = HashPassword("airval-timing-equalizer")

// NewToken returns a random URL-safe token (256 bits).
func NewToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// HashToken returns the hex SHA-256 of a session token or API key (what we store).
func HashToken(t string) string {
	h := sha256.Sum256([]byte(t))
	return hex.EncodeToString(h[:])
}

// API keys look like airv_<43 url-safe chars>.
const (
	APIKeyPrefix    = "airv_"
	apiKeyPrefixLen = len(APIKeyPrefix) + 8
)

// NewAPIKey returns a new key, its display prefix and its storage hash.
func NewAPIKey() (key, prefix, hash string) {
	key = APIKeyPrefix + NewToken()
	return key, key[:apiKeyPrefixLen], HashToken(key)
}

// IsAPIKey reports whether s looks like an airval API key.
func IsAPIKey(s string) bool { return strings.HasPrefix(s, APIKeyPrefix) && len(s) > apiKeyPrefixLen }
