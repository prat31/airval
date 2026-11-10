package secrets

import (
	"errors"
	"testing"

	"github.com/airval/airval/internal/store"
)

func TestKeyringRoundTripAndRotation(t *testing.T) {
	k1key, k2key := GenerateKey(), GenerateKey()
	k1, err := NewKeyring(k1key, 1, "")
	if err != nil {
		t.Fatal(err)
	}
	s := store.Secret{ID: store.NewID()}
	if err := k1.EncryptSecret(&s, "sk-live-123"); err != nil {
		t.Fatal(err)
	}
	if s.KeyVersion != 1 || string(s.Ciphertext) == "sk-live-123" {
		t.Fatal("not encrypted")
	}
	// New keyring with v2 current and v1 previous can read old secrets.
	k2, err := NewKeyring(k2key, 2, "1:"+k1key)
	if err != nil {
		t.Fatal(err)
	}
	pt, err := k2.DecryptSecret(s)
	if err != nil || pt != "sk-live-123" {
		t.Fatalf("decrypt old: %q %v", pt, err)
	}
	// AAD binds to the id.
	other := s
	other.ID = store.NewID()
	if _, err := k2.DecryptSecret(other); err == nil {
		t.Fatal("expected AAD mismatch")
	}
	// Without the old key: unknown version.
	k3, _ := NewKeyring(k2key, 2, "")
	if _, err := k3.DecryptSecret(s); !errors.Is(err, ErrUnknownKeyVersion) {
		t.Fatalf("want unknown version, got %v", err)
	}
	blob, v, err := k2.Seal([]byte("whsec"), []byte("trigger"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := k2.Unseal(blob, v, []byte("trigger"))
	if err != nil || string(got) != "whsec" {
		t.Fatalf("unseal: %q %v", got, err)
	}
}

func TestParseKey(t *testing.T) {
	tests := []struct {
		in      string
		wantErr bool
	}{
		{GenerateKey(), false},
		{"not base64!!", true},
		{"c2hvcnQ=", true},
	}
	for _, tc := range tests {
		if _, err := ParseKey(tc.in); (err != nil) != tc.wantErr {
			t.Errorf("ParseKey(%q) err=%v wantErr=%v", tc.in, err, tc.wantErr)
		}
	}
}
