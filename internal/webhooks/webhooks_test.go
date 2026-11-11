package webhooks

import (
	"errors"
	"strconv"
	"testing"
	"time"
)

func TestVerify(t *testing.T) {
	secret := []byte("whsec_x")
	body := []byte(`{"a":1}`)
	now := time.Now()
	ts := strconv.FormatInt(now.Unix(), 10)
	sig := Sign(secret, ts, body)
	if err := Verify(secret, ts, sig, body, now); err != nil {
		t.Fatal(err)
	}
	if err := Verify(secret, ts, sig, []byte(`{"a":2}`), now); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("tampered body: %v", err)
	}
	if err := Verify([]byte("other"), ts, sig, body, now); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("wrong secret: %v", err)
	}
	if err := Verify(secret, ts, sig, body, now.Add(6*time.Minute)); !errors.Is(err, ErrExpired) {
		t.Fatalf("replay: %v", err)
	}
	if err := Verify(secret, "", sig, body, now); !errors.Is(err, ErrMissingSignature) {
		t.Fatalf("missing: %v", err)
	}
	if err := Verify(secret, "abc", sig, body, now); !errors.Is(err, ErrBadTimestamp) {
		t.Fatalf("bad ts: %v", err)
	}
}

func TestMap(t *testing.T) {
	body := []byte(`{"ref":"refs/heads/feature/x","after":"abc123","deployment_status":{"target_url":"https://pr-1.example.app"},
		"pull_request":{"number":42}}`)
	vars, labels, err := Map(body, map[string]string{"base_url": "$.deployment_status.target_url"},
		map[string]string{"pr": "pull_request.number"})
	if err != nil {
		t.Fatal(err)
	}
	if vars["base_url"] != "https://pr-1.example.app" {
		t.Fatalf("vars: %v", vars)
	}
	if labels["git_sha"] != "abc123" || labels["branch"] != "feature/x" || labels["pr"] != "42" ||
		labels["url"] != "https://pr-1.example.app" {
		t.Fatalf("labels: %v", labels)
	}
	if _, _, err := Map([]byte("nope"), nil, nil); err == nil {
		t.Fatal("want error for non-JSON")
	}
}
