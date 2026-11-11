package runner

import (
	"math/rand/v2"
	"time"

	"github.com/airval/airval/internal/targets"
)

// classifyOutcome maps an adapter/LLM error to a limiter outcome and Retry-After hint.
func classifyOutcome(err error) (outcome, time.Duration) {
	switch targets.Classify(err) {
	case targets.ClassNone:
		return outcomeOK, 0
	case targets.ClassRateLimited:
		ra, _ := targets.RetryAfter(err)
		return outcomeThrottled, ra
	case targets.ClassTransient:
		return outcomeTransient, 0
	}
	return outcomeOther, 0
}

// retryable reports whether an error is worth another attempt.
func retryable(err error) bool {
	switch targets.Classify(err) {
	case targets.ClassRateLimited, targets.ClassTransient, targets.ClassUnknown:
		return true
	}
	return false
}

const (
	backoffBase = 200 * time.Millisecond
	backoffMax  = 30 * time.Second
)

// backoff returns a full-jitter exponential delay for attempt (1-based), never shorter than a
// Retry-After hint.
func backoff(attempt int, retryAfter time.Duration) time.Duration {
	d := backoffBase << min(attempt-1, 10)
	if d > backoffMax {
		d = backoffMax
	}
	d = d/2 + time.Duration(rand.Int64N(int64(d/2)+1)) // jitter in [d/2, d]
	if retryAfter > d {
		d = retryAfter + time.Duration(rand.Int64N(int64(50*time.Millisecond)))
	}
	return min(d, max(backoffMax, retryAfter))
}
