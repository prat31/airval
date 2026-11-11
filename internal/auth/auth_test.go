package auth

import (
	"testing"
)

func TestPasswordHashing(t *testing.T) {
	h, err := HashPassword("correct horse")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		pw   string
		want bool
	}{{"correct horse", true}, {"wrong", false}, {"", false}}
	for _, tc := range tests {
		got, err := VerifyPassword(h, tc.pw)
		if err != nil || got != tc.want {
			t.Errorf("VerifyPassword(%q) = %v, %v; want %v", tc.pw, got, err, tc.want)
		}
	}
	if _, err := VerifyPassword("$bcrypt$x", "a"); err == nil {
		t.Error("expected unsupported hash error")
	}
}

func TestAPIKeys(t *testing.T) {
	k, prefix, hash := NewAPIKey()
	if !IsAPIKey(k) || len(prefix) != 13 || hash != HashToken(k) {
		t.Fatalf("bad key %q %q", k, prefix)
	}
	if IsAPIKey("nope") || IsAPIKey("airv_") {
		t.Fatal("IsAPIKey false positive")
	}
}

func TestSlugify(t *testing.T) {
	tests := map[string]string{"Acme Inc.": "acme-inc", "  ": "default", "Hello__World!!": "hello-world"}
	for in, want := range tests {
		if got := Slugify(in); got != want {
			t.Errorf("Slugify(%q)=%q want %q", in, got, want)
		}
	}
}
