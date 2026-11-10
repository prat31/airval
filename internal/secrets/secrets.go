// Package secrets encrypts credentials at rest with AES-256-GCM. Keys are versioned so the
// master key can be rotated (`airval secrets rotate`) while old ciphertexts stay readable.
package secrets

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/airval/airval/internal/store"
)

// ErrUnknownKeyVersion is returned when a ciphertext was sealed with a key we don't have.
var ErrUnknownKeyVersion = errors.New("secrets: unknown key version")

// Keyring holds the current key and any previous ones.
type Keyring struct {
	current int
	aeads   map[int]cipher.AEAD
}

// ParseKey decodes a base64 (std or url, padded or not) 32-byte key.
func ParseKey(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		if b, err := enc.DecodeString(s); err == nil {
			if len(b) != 32 {
				return nil, fmt.Errorf("secrets: master key must be 32 bytes, got %d", len(b))
			}
			return b, nil
		}
	}
	return nil, errors.New("secrets: master key is not valid base64")
}

// NewKeyring builds a keyring. previous is "version:base64key,..." (may be empty).
func NewKeyring(masterKey string, version int, previous string) (*Keyring, error) {
	if version <= 0 {
		version = 1
	}
	k := &Keyring{current: version, aeads: map[int]cipher.AEAD{}}
	if err := k.add(version, masterKey); err != nil {
		return nil, err
	}
	for _, part := range strings.Split(previous, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		v, key, ok := strings.Cut(part, ":")
		n, err := strconv.Atoi(v)
		if !ok || err != nil {
			return nil, fmt.Errorf("secrets: previous_keys entry must be version:key")
		}
		if n == version {
			continue
		}
		if err := k.add(n, key); err != nil {
			return nil, err
		}
	}
	return k, nil
}

func (k *Keyring) add(version int, key string) error {
	raw, err := ParseKey(key)
	if err != nil {
		return err
	}
	block, err := aes.NewCipher(raw)
	if err != nil {
		return err
	}
	g, err := cipher.NewGCM(block)
	if err != nil {
		return err
	}
	k.aeads[version] = g
	return nil
}

// Version returns the current key version.
func (k *Keyring) Version() int { return k.current }

// Encrypt seals plaintext with the current key. aad binds the ciphertext to its owner (e.g. secret id).
func (k *Keyring) Encrypt(plaintext, aad []byte) (ciphertext, nonce []byte, version int, err error) {
	g := k.aeads[k.current]
	nonce = make([]byte, g.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, nil, 0, err
	}
	return g.Seal(nil, nonce, plaintext, aad), nonce, k.current, nil
}

// Decrypt opens a ciphertext sealed with the given key version.
func (k *Keyring) Decrypt(ciphertext, nonce []byte, version int, aad []byte) ([]byte, error) {
	g, ok := k.aeads[version]
	if !ok {
		return nil, fmt.Errorf("%w: %d", ErrUnknownKeyVersion, version)
	}
	pt, err := g.Open(nil, nonce, ciphertext, aad)
	if err != nil {
		return nil, fmt.Errorf("secrets: decrypt: %w", err)
	}
	return pt, nil
}

// Seal returns nonce||ciphertext (single-column storage, e.g. webhook trigger secrets).
func (k *Keyring) Seal(plaintext, aad []byte) ([]byte, int, error) {
	ct, nonce, v, err := k.Encrypt(plaintext, aad)
	if err != nil {
		return nil, 0, err
	}
	return append(nonce, ct...), v, nil
}

// Unseal reverses Seal.
func (k *Keyring) Unseal(blob []byte, version int, aad []byte) ([]byte, error) {
	g, ok := k.aeads[version]
	if !ok {
		return nil, fmt.Errorf("%w: %d", ErrUnknownKeyVersion, version)
	}
	if len(blob) < g.NonceSize() {
		return nil, errors.New("secrets: sealed blob too short")
	}
	return k.Decrypt(blob[g.NonceSize():], blob[:g.NonceSize()], version, aad)
}

// EncryptSecret fills s.Ciphertext/Nonce/KeyVersion for value. s.ID must be set (used as AAD).
func (k *Keyring) EncryptSecret(s *store.Secret, value string) error {
	ct, nonce, v, err := k.Encrypt([]byte(value), s.ID[:])
	if err != nil {
		return err
	}
	s.Ciphertext, s.Nonce, s.KeyVersion = ct, nonce, v
	return nil
}

// DecryptSecret returns the plaintext of s. Only workers (and target tests) should call this.
func (k *Keyring) DecryptSecret(s store.Secret) (string, error) {
	pt, err := k.Decrypt(s.Ciphertext, s.Nonce, s.KeyVersion, s.ID[:])
	return string(pt), err
}

// GenerateKey returns a new random base64 master key.
func GenerateKey() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.StdEncoding.EncodeToString(b)
}

// LoadOrCreateKeyFile reads a base64 key from path or creates it (0600) — used by `airval dev`.
func LoadOrCreateKeyFile(path string) (string, error) {
	if b, err := os.ReadFile(path); err == nil {
		return strings.TrimSpace(string(b)), nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	key := GenerateKey()
	if err := os.WriteFile(path, []byte(key+"\n"), 0o600); err != nil {
		return "", err
	}
	return key, nil
}

// Rotate re-encrypts every secret not sealed with the current key version. Webhook trigger
// secrets are re-sealed too. It returns the number of rows rewritten.
func Rotate(ctx context.Context, st store.Store, k *Keyring) (int, error) {
	n := 0
	for {
		batch, err := st.ListSecretsNotAtKeyVersion(ctx, k.Version(), 200)
		if err != nil {
			return n, err
		}
		if len(batch) == 0 {
			return n, nil
		}
		for _, s := range batch {
			pt, err := k.DecryptSecret(s)
			if err != nil {
				return n, fmt.Errorf("secrets: rotate %s (%s): %w", s.ID, s.Name, err)
			}
			if err := k.EncryptSecret(&s, pt); err != nil {
				return n, err
			}
			if err := st.UpdateSecret(ctx, &s); err != nil {
				return n, err
			}
			n++
		}
	}
}

// Resolver resolves secret names for one project at execution time (implements evalcore.SecretResolver).
type Resolver struct {
	Ctx       context.Context
	Store     store.SecretStore
	Keyring   *Keyring
	OrgID     uuid.UUID
	ProjectID *uuid.UUID
	cache     map[string]string
}

// Resolve returns the plaintext of a secret by name (project-scoped first, then org-level).
func (r *Resolver) Resolve(name string) (string, error) {
	if v, ok := r.cache[name]; ok {
		return v, nil
	}
	s, err := r.Store.ResolveSecret(r.Ctx, r.OrgID, r.ProjectID, name)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return "", fmt.Errorf("secret %q not found", name)
		}
		return "", err
	}
	v, err := r.Keyring.DecryptSecret(s)
	if err != nil {
		return "", err
	}
	if r.cache == nil {
		r.cache = map[string]string{}
	}
	r.cache[name] = v
	return v, nil
}
