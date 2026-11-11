package runner

import (
	"context"
	"math"
	"sync"
	"time"

	"github.com/prat31/airval/internal/queue"
)

// outcome of one call, fed back into the limiter.
type outcome int

const (
	outcomeOK        outcome = iota
	outcomeThrottled         // 429 / rate limited
	outcomeTransient         // 5xx, timeout, connection error
	outcomeOther             // permanent errors, cancellations: no signal for AIMD
)

// limiter bounds concurrent calls to one target (or judge provider) with an AIMD window:
// additive increase (+1 per window of successes) and multiplicative decrease (×0.5, at most once
// per observed round trip) on throttling or transient errors. A Retry-After hint pauses every
// caller until it expires. Optional rpm/tpm limits come from distributed token buckets in NATS KV
// (leased locally in ~1 s slices) so several workers share the target's real limit.
type limiter struct {
	mu          sync.Mutex
	limit       float64 // current window (≥ 1)
	max         float64
	adaptive    bool
	inflight    int
	waiters     []chan struct{}
	pausedUntil time.Time
	lastCut     time.Time
	rtt         time.Duration // EWMA of call latency (decrease cooldown)

	rpm *queue.LeasedLimiter // nil = unlimited
	tpm *queue.TokenBucket   // nil = unlimited

	// counters for summaries
	throttled, transient int64
}

func newLimiter(max int, adaptive bool) *limiter {
	if max < 1 {
		max = 1
	}
	return &limiter{limit: float64(max), max: float64(max), adaptive: adaptive}
}

// configure updates the maximum window (e.g. a newer run of the same target with other limits).
func (l *limiter) configure(max int, adaptive bool) {
	if max < 1 {
		max = 1
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.adaptive = adaptive
	if float64(max) != l.max {
		l.max = float64(max)
		l.limit = math.Min(l.limit, l.max)
		if !adaptive {
			l.limit = l.max
		}
		l.dispatchLocked()
	}
}

// Acquire waits for a slot (and rate budget). Every successful Acquire must be paired with Release.
func (l *limiter) Acquire(ctx context.Context) error {
	for {
		l.mu.Lock()
		wait := time.Until(l.pausedUntil)
		l.mu.Unlock()
		if wait <= 0 {
			break
		}
		if err := sleepCtx(ctx, wait); err != nil {
			return err
		}
	}
	if l.tpm != nil {
		if err := l.waitTPM(ctx); err != nil {
			return err
		}
	}
	if l.rpm != nil {
		if err := l.rpm.Wait(ctx); err != nil {
			return err
		}
	}
	l.mu.Lock()
	if len(l.waiters) == 0 && l.inflight < l.window() {
		l.inflight++
		l.mu.Unlock()
		return nil
	}
	ch := make(chan struct{})
	l.waiters = append(l.waiters, ch)
	l.mu.Unlock()
	select {
	case <-ch:
		return nil
	case <-ctx.Done():
		l.mu.Lock()
		defer l.mu.Unlock()
		for i, w := range l.waiters {
			if w == ch {
				l.waiters = append(l.waiters[:i], l.waiters[i+1:]...)
				return ctx.Err()
			}
		}
		// The slot was granted concurrently: give it back.
		l.inflight--
		l.dispatchLocked()
		return ctx.Err()
	}
}

func (l *limiter) window() int { return max(1, int(l.limit)) }

// Release returns a slot and adapts the window.
func (l *limiter) Release(o outcome, latency, retryAfter time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.inflight--
	if latency > 0 {
		if l.rtt == 0 {
			l.rtt = latency
		} else {
			l.rtt = (l.rtt*7 + latency) / 8
		}
	}
	now := time.Now()
	switch o {
	case outcomeOK:
		if l.adaptive && l.limit < l.max {
			l.limit = math.Min(l.max, l.limit+1/l.limit)
		}
	case outcomeThrottled, outcomeTransient:
		if o == outcomeThrottled {
			l.throttled++
		} else {
			l.transient++
		}
		cool := max(l.rtt, 100*time.Millisecond)
		if l.adaptive && now.Sub(l.lastCut) >= cool {
			l.limit = math.Max(1, l.limit*0.5)
			l.lastCut = now
		}
		if retryAfter > 0 {
			if until := now.Add(retryAfter); until.After(l.pausedUntil) {
				l.pausedUntil = until
			}
		}
	}
	l.dispatchLocked()
}

func (l *limiter) dispatchLocked() {
	for len(l.waiters) > 0 && l.inflight < l.window() {
		ch := l.waiters[0]
		l.waiters = l.waiters[1:]
		l.inflight++
		close(ch)
	}
}

// consumeTokens charges used tokens against the tpm budget; a deficit pauses callers.
func (l *limiter) consumeTokens(ctx context.Context, n int) {
	if l.tpm == nil || n <= 0 {
		return
	}
	got, wait, err := l.tpm.Take(ctx, n)
	if err != nil || got >= n || wait <= 0 {
		return
	}
	l.mu.Lock()
	if until := time.Now().Add(wait); until.After(l.pausedUntil) {
		l.pausedUntil = until
	}
	l.mu.Unlock()
}

// waitTPM blocks while the token budget is exhausted (probing with a zero-cost take).
func (l *limiter) waitTPM(ctx context.Context) error {
	for {
		got, wait, err := l.tpm.Take(ctx, 1)
		if err != nil || got == 1 {
			return err
		}
		if wait <= 0 {
			wait = 50 * time.Millisecond
		}
		if err := sleepCtx(ctx, wait); err != nil {
			return err
		}
	}
}

// snapshot returns the current window and counters.
func (l *limiter) snapshot() (window float64, inflight int, throttled, transient int64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.limit, l.inflight, l.throttled, l.transient
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
