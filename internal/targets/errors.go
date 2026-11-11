package targets

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// The runner uses these error types for AIMD / backoff decisions. Adapters never
// retry; they only classify.

// ErrRateLimited means the target asked us to slow down (HTTP 429). RetryAfter
// is zero when the target gave no hint.
type ErrRateLimited struct {
	RetryAfter time.Duration
	Err        error
}

func (e ErrRateLimited) Error() string {
	if e.RetryAfter > 0 {
		return fmt.Sprintf("rate limited (retry after %s): %v", e.RetryAfter, e.Err)
	}
	return fmt.Sprintf("rate limited: %v", e.Err)
}
func (e ErrRateLimited) Unwrap() error { return e.Err }

// ErrTransient is a retryable failure: 5xx, timeouts, connection resets.
type ErrTransient struct {
	Err    error
	Status int // HTTP status, 0 for network errors
}

func (e ErrTransient) Error() string { return fmt.Sprintf("transient error: %v", e.Err) }
func (e ErrTransient) Unwrap() error { return e.Err }

// ErrPermanent is a failure that retrying will not fix: 4xx, bad config,
// unparsable responses, SSRF denials, oversized responses.
type ErrPermanent struct {
	Err    error
	Status int
}

func (e ErrPermanent) Error() string { return fmt.Sprintf("permanent error: %v", e.Err) }
func (e ErrPermanent) Unwrap() error { return e.Err }

// Class is the coarse classification of an error returned by an adapter.
type Class int

const (
	ClassNone Class = iota
	ClassRateLimited
	ClassTransient
	ClassPermanent
	ClassCanceled // caller's context was canceled / its deadline passed
	ClassUnknown
)

func (c Class) String() string {
	return [...]string{"none", "rate_limited", "transient", "permanent", "canceled", "unknown"}[c]
}

// Classify maps an error returned by Adapter.Invoke to a Class.
func Classify(err error) Class {
	if err == nil {
		return ClassNone
	}
	var rl ErrRateLimited
	var tr ErrTransient
	var pe ErrPermanent
	switch {
	case errors.As(err, &rl):
		return ClassRateLimited
	case errors.As(err, &tr):
		return ClassTransient
	case errors.As(err, &pe):
		return ClassPermanent
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return ClassCanceled
	}
	return ClassUnknown
}

// RetryAfter extracts the retry hint of a rate-limit error.
func RetryAfter(err error) (time.Duration, bool) {
	var rl ErrRateLimited
	if errors.As(err, &rl) {
		return rl.RetryAfter, true
	}
	return 0, false
}

func permanent(format string, a ...any) error {
	return ErrPermanent{Err: fmt.Errorf(format, a...)}
}

// classifyStatus converts a non-success HTTP status into a typed error.
func classifyStatus(status int, h http.Header, body []byte, redact func(string) string) error {
	snippet := string(body)
	if len(snippet) > 300 {
		snippet = snippet[:300] + "…"
	}
	if redact != nil {
		snippet = redact(snippet)
	}
	err := fmt.Errorf("http %d: %s", status, strings.TrimSpace(snippet))
	switch {
	case status == http.StatusTooManyRequests:
		// Quota exhaustion is not fixed by waiting.
		if strings.Contains(snippet, "insufficient_quota") {
			return ErrPermanent{Err: err, Status: status}
		}
		return ErrRateLimited{RetryAfter: parseRetryAfter(h), Err: err}
	case status == http.StatusRequestTimeout, status == http.StatusTooEarly, status >= 500:
		return ErrTransient{Err: err, Status: status}
	default:
		return ErrPermanent{Err: err, Status: status}
	}
}

func parseRetryAfter(h http.Header) time.Duration {
	if v := h.Get("Retry-After-Ms"); v != "" {
		if ms, err := strconv.ParseFloat(v, 64); err == nil && ms >= 0 {
			return time.Duration(ms * float64(time.Millisecond))
		}
	}
	v := strings.TrimSpace(h.Get("Retry-After"))
	if v == "" {
		return 0
	}
	if s, err := strconv.ParseFloat(v, 64); err == nil && s >= 0 {
		return time.Duration(s * float64(time.Second))
	}
	if t, err := http.ParseTime(v); err == nil {
		if d := time.Until(t); d > 0 {
			return d
		}
	}
	return 0
}

// classifyNetErr classifies an error from http.Client.Do or a body read.
// parent is the caller's context: if it is done, the caller canceled and the
// error is returned unclassified.
func classifyNetErr(parent context.Context, err error) error {
	if err == nil {
		return nil
	}
	if perr := parent.Err(); perr != nil {
		return perr
	}
	var ssrf *SSRFError
	if errors.As(err, &ssrf) {
		return ErrPermanent{Err: err}
	}
	var (
		unknownAuth x509.UnknownAuthorityError
		certInvalid x509.CertificateInvalidError
		hostErr     x509.HostnameError
		dnsErr      *net.DNSError
		urlErr      *url.Error
	)
	switch {
	case errors.As(err, &unknownAuth), errors.As(err, &certInvalid), errors.As(err, &hostErr):
		return ErrPermanent{Err: err}
	case errors.Is(err, context.DeadlineExceeded):
		return ErrTransient{Err: fmt.Errorf("timeout: %w", err)}
	case errors.As(err, &dnsErr):
		if dnsErr.IsNotFound {
			return ErrPermanent{Err: err}
		}
		return ErrTransient{Err: err}
	case errors.As(err, &urlErr) && strings.Contains(urlErr.Error(), "unsupported protocol scheme"):
		return ErrPermanent{Err: err}
	}
	return ErrTransient{Err: err}
}
