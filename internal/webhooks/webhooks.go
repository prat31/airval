// Package webhooks handles inbound webhook triggers: HMAC-SHA256 signature verification with a
// timestamp replay window, and mapping of the JSON payload (GitHub, GitLab, Vercel deploy events
// or anything custom) to target variable overrides and run labels.
//
// Signature: X-Airval-Signature: sha256=<hex(hmac_sha256(secret, timestamp + "." + body))>,
// X-Airval-Timestamp: <unix seconds>. Requests outside a 5-minute window are rejected.
package webhooks

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/airval/airval/internal/targets"
)

// Headers.
const (
	HeaderSignature = "X-Airval-Signature"
	HeaderTimestamp = "X-Airval-Timestamp"
)

// MaxBody bounds webhook payloads.
const MaxBody = 1 << 20

// Window is the accepted clock skew / replay window.
const Window = 5 * time.Minute

var (
	ErrMissingSignature = errors.New("missing X-Airval-Signature or X-Airval-Timestamp header")
	ErrBadTimestamp     = errors.New("X-Airval-Timestamp is not a unix timestamp")
	ErrExpired          = errors.New("request timestamp is outside the 5-minute window")
	ErrBadSignature     = errors.New("signature does not match")
)

// Sign returns the signature header value for body at ts.
func Sign(secret []byte, ts string, body []byte) string {
	m := hmac.New(sha256.New, secret)
	m.Write([]byte(ts))
	m.Write([]byte("."))
	m.Write(body)
	return "sha256=" + hex.EncodeToString(m.Sum(nil))
}

// Verify checks the signature and timestamp headers of a request body.
func Verify(secret []byte, tsHeader, sigHeader string, body []byte, now time.Time) error {
	if tsHeader == "" || sigHeader == "" {
		return ErrMissingSignature
	}
	sec, err := strconv.ParseInt(strings.TrimSpace(tsHeader), 10, 64)
	if err != nil {
		return ErrBadTimestamp
	}
	if d := now.Sub(time.Unix(sec, 0)); d > Window || d < -Window {
		return ErrExpired
	}
	want := Sign(secret, strings.TrimSpace(tsHeader), body)
	got := strings.TrimSpace(sigHeader)
	if !strings.HasPrefix(got, "sha256=") {
		got = "sha256=" + got
	}
	if !hmac.Equal([]byte(strings.ToLower(got)), []byte(want)) {
		return ErrBadSignature
	}
	return nil
}

// defaultLabels are tried (in order) when a trigger has no payload_labels mapping for a label.
var defaultLabels = map[string][]string{
	"git_sha": {"$.git_sha", "$.sha", "$.commit_sha", "$.pull_request.head.sha", "$.head_commit.id", "$.after",
		"$.checkout_sha", "$.deployment.sha", "$.payload.deployment.meta.githubCommitSha"},
	"branch": {"$.branch", "$.pull_request.head.ref", "$.ref", "$.deployment.ref",
		"$.payload.deployment.meta.githubCommitRef"},
	"url": {"$.url", "$.deployment_url", "$.target_url", "$.deployment_status.target_url",
		"$.deployment_status.environment_url", "$.payload.deployment.url"},
}

// Map extracts variables and labels from a payload. payloadVars / payloadLabels map names to
// JSONPath expressions; well-known labels (git_sha, branch, url) are detected automatically.
func Map(body []byte, payloadVars, payloadLabels map[string]string) (vars, labels map[string]string, err error) {
	vars, labels = map[string]string{}, map[string]string{}
	if len(strings.TrimSpace(string(body))) == 0 {
		return vars, labels, nil
	}
	var doc any
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, nil, fmt.Errorf("payload is not JSON: %w", err)
	}
	for name, path := range payloadVars {
		v, ok, err := lookup(doc, path)
		if err != nil {
			return nil, nil, fmt.Errorf("payload_variables.%s: %w", name, err)
		}
		if ok {
			vars[name] = v
		}
	}
	for name, path := range payloadLabels {
		v, ok, err := lookup(doc, path)
		if err != nil {
			return nil, nil, fmt.Errorf("payload_labels.%s: %w", name, err)
		}
		if ok {
			labels[name] = v
		}
	}
	for name, paths := range defaultLabels {
		if _, set := labels[name]; set {
			continue
		}
		for _, p := range paths {
			if v, ok, _ := lookup(doc, p); ok && v != "" {
				if name == "branch" {
					v = strings.TrimPrefix(v, "refs/heads/")
				}
				labels[name] = v
				break
			}
		}
	}
	return vars, labels, nil
}

func lookup(doc any, path string) (string, bool, error) {
	if !strings.HasPrefix(path, "$") {
		path = "$." + path
	}
	p, err := targets.CompilePath(path)
	if err != nil {
		return "", false, err
	}
	v, ok := p.First(doc)
	if !ok || v == nil {
		return "", false, nil
	}
	switch t := v.(type) {
	case string:
		return t, true, nil
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64), true, nil
	case bool:
		return strconv.FormatBool(t), true, nil
	}
	b, _ := json.Marshal(v)
	return string(b), true, nil
}
