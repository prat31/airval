package queue

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

// kvKey makes arbitrary names safe for KV keys.
func kvKey(s string) string {
	return strings.NewReplacer(" ", "_", "*", "_", ">", "_", ":", "_", "/", ".").Replace(s)
}

// ---- Leases ----

type leaseVal struct {
	Holder  string    `json:"holder"`
	Expires time.Time `json:"expires"`
}

// AcquireLease takes or renews lease `name` for `holder` for ttl. It returns true when holder owns
// the lease afterwards. Safe across processes (compare-and-swap on the KV revision).
func (q *Queue) AcquireLease(ctx context.Context, name, holder string, ttl time.Duration) (bool, error) {
	key := kvKey(name)
	val, _ := json.Marshal(leaseVal{Holder: holder, Expires: time.Now().Add(ttl)})
	for attempt := 0; attempt < 3; attempt++ {
		e, err := q.leases.Get(ctx, key)
		switch {
		case errors.Is(err, jetstream.ErrKeyNotFound):
			if _, err := q.leases.Create(ctx, key, val); err == nil {
				return true, nil
			} else if !errors.Is(err, jetstream.ErrKeyExists) {
				return false, err
			}
			continue // raced with another creator
		case err != nil:
			return false, err
		}
		var cur leaseVal
		_ = json.Unmarshal(e.Value(), &cur)
		if cur.Holder != holder && time.Now().Before(cur.Expires) {
			return false, nil
		}
		if _, err := q.leases.Update(ctx, key, val, e.Revision()); err == nil {
			return true, nil
		} else if !isWrongSeq(err) {
			return false, err
		}
	}
	return false, nil
}

// ReleaseLease gives up the lease if holder owns it.
func (q *Queue) ReleaseLease(ctx context.Context, name, holder string) error {
	key := kvKey(name)
	e, err := q.leases.Get(ctx, key)
	if errors.Is(err, jetstream.ErrKeyNotFound) {
		return nil
	} else if err != nil {
		return err
	}
	var cur leaseVal
	_ = json.Unmarshal(e.Value(), &cur)
	if cur.Holder != holder {
		return nil
	}
	err = q.leases.Delete(ctx, key, jetstream.LastRevision(e.Revision()))
	if isWrongSeq(err) {
		return nil
	}
	return err
}

// RunAsLeader blocks until ctx is done, running fn whenever this holder owns lease `name`.
// fn's context is cancelled as soon as the lease is lost (or ctx ends). Use for the scheduler.
func (q *Queue) RunAsLeader(ctx context.Context, name, holder string, ttl time.Duration, fn func(ctx context.Context)) {
	renew := ttl / 3
	for ctx.Err() == nil {
		ok, err := q.AcquireLease(ctx, name, holder, ttl)
		if err != nil && ctx.Err() == nil {
			q.log.Warn("queue: lease acquire", "lease", name, "err", err)
		}
		if !ok {
			sleepCtx(ctx, renew)
			continue
		}
		lctx, cancel := context.WithCancel(ctx)
		done := make(chan struct{})
		go func() { defer close(done); fn(lctx) }()
	hold:
		for {
			select {
			case <-done:
				break hold
			case <-time.After(renew):
				if ok, err := q.AcquireLease(ctx, name, holder, ttl); !ok || err != nil {
					break hold
				}
			case <-ctx.Done():
				break hold
			}
		}
		cancel()
		<-done
		_ = q.ReleaseLease(context.WithoutCancel(ctx), name, holder)
	}
}

func sleepCtx(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}

func isWrongSeq(err error) bool {
	if err == nil {
		return false
	}
	var apiErr *jetstream.APIError
	if errors.As(err, &apiErr) && apiErr.ErrorCode == jetstream.JSErrCodeStreamWrongLastSequence {
		return true
	}
	return strings.Contains(err.Error(), "wrong last sequence")
}

// ---- Cancel flags ----

// SetCancel marks a run as cancelled. Workers observe it via IsCancelled or a CancelWatcher.
func (q *Queue) SetCancel(ctx context.Context, runID string) error {
	_, err := q.cancels.Put(ctx, kvKey(runID), []byte(time.Now().UTC().Format(time.RFC3339Nano)))
	return err
}

// IsCancelled reads the cancel flag of one run (one KV round trip).
func (q *Queue) IsCancelled(ctx context.Context, runID string) (bool, error) {
	_, err := q.cancels.Get(ctx, kvKey(runID))
	if errors.Is(err, jetstream.ErrKeyNotFound) {
		return false, nil
	}
	return err == nil, err
}

// CancelWatcher keeps a local, push-updated set of cancelled runs so hot loops can check
// cancellation without network calls.
type CancelWatcher struct {
	set  sync.Map
	stop func() error
}

// WatchCancels starts a CancelWatcher; it stops when ctx is done.
func (q *Queue) WatchCancels(ctx context.Context) (*CancelWatcher, error) {
	w, err := q.cancels.WatchAll(ctx)
	if err != nil {
		return nil, err
	}
	cw := &CancelWatcher{stop: w.Stop}
	ready := make(chan struct{})
	go func() {
		readyOnce := sync.OnceFunc(func() { close(ready) })
		defer readyOnce()
		for e := range w.Updates() {
			if e == nil { // initial values delivered
				readyOnce()
				continue
			}
			if e.Operation() == jetstream.KeyValuePut {
				cw.set.Store(e.Key(), struct{}{})
			} else {
				cw.set.Delete(e.Key())
			}
		}
	}()
	select {
	case <-ready:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return cw, nil
}

// Cancelled reports whether runID has been cancelled.
func (c *CancelWatcher) Cancelled(runID string) bool {
	_, ok := c.set.Load(kvKey(runID))
	return ok
}

// Stop ends the watch.
func (c *CancelWatcher) Stop() { _ = c.stop() }

// ---- Distributed token bucket ----

type bucketState struct {
	Tokens float64 `json:"t"`
	At     int64   `json:"at"` // unix nanos of last refill
}

// TokenBucket is a token bucket whose state lives in NATS KV, shared by all workers.
// Rate is tokens per second; Burst is the bucket capacity.
type TokenBucket struct {
	q     *Queue
	key   string
	mu    sync.Mutex
	rate  float64
	burst float64
}

// TokenBucket returns a handle on the shared bucket `name`.
func (q *Queue) TokenBucket(name string, ratePerSec, burst float64) *TokenBucket {
	if burst < 1 {
		burst = math.Max(1, ratePerSec)
	}
	return &TokenBucket{q: q, key: kvKey(name), rate: ratePerSec, burst: burst}
}

// SetRate changes the refill rate (AIMD adjusts this).
func (b *TokenBucket) SetRate(ratePerSec float64) {
	b.mu.Lock()
	b.rate = ratePerSec
	b.mu.Unlock()
}

// Rate returns the current refill rate.
func (b *TokenBucket) Rate() float64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.rate
}

// Take tries to take up to n tokens. It returns how many were granted (0..n) and, when fewer than
// n were granted, how long until the remainder would be available.
func (b *TokenBucket) Take(ctx context.Context, n int) (int, time.Duration, error) {
	b.mu.Lock()
	rate, burst := b.rate, b.burst
	b.mu.Unlock()
	kv := b.q.limits
	for attempt := 0; attempt < 10; attempt++ {
		now := time.Now()
		var st bucketState
		var rev uint64
		e, err := kv.Get(ctx, b.key)
		switch {
		case errors.Is(err, jetstream.ErrKeyNotFound):
			st = bucketState{Tokens: burst, At: now.UnixNano()}
		case err != nil:
			return 0, 0, err
		default:
			rev = e.Revision()
			_ = json.Unmarshal(e.Value(), &st)
			elapsed := float64(now.UnixNano()-st.At) / 1e9
			if elapsed > 0 {
				st.Tokens = math.Min(burst, st.Tokens+elapsed*rate)
			}
			st.At = now.UnixNano()
		}
		granted := int(math.Min(float64(n), math.Floor(st.Tokens)))
		if granted < 0 {
			granted = 0
		}
		st.Tokens -= float64(granted)
		val, _ := json.Marshal(st)
		if rev == 0 {
			_, err = kv.Create(ctx, b.key, val)
			if errors.Is(err, jetstream.ErrKeyExists) {
				continue
			}
		} else {
			_, err = kv.Update(ctx, b.key, val, rev)
			if isWrongSeq(err) {
				continue
			}
		}
		if err != nil {
			return 0, 0, err
		}
		var wait time.Duration
		if granted < n && rate > 0 {
			missing := float64(n-granted) - st.Tokens
			wait = time.Duration(math.Max(0, missing) / rate * float64(time.Second))
		}
		return granted, wait, nil
	}
	return 0, 10 * time.Millisecond, nil // heavy contention: back off briefly
}

// LeasedLimiter rate-limits locally by leasing slices of the shared bucket, so workers hit KV
// about once per slice instead of once per request. Slice defaults to ~1 second of budget split
// across `shares` workers.
type LeasedLimiter struct {
	b      *TokenBucket
	shares int
	mu     sync.Mutex
	local  int
}

// NewLeasedLimiter wraps a bucket. shares ≈ number of workers sharing it (≥1).
func NewLeasedLimiter(b *TokenBucket, shares int) *LeasedLimiter {
	if shares < 1 {
		shares = 1
	}
	return &LeasedLimiter{b: b, shares: shares}
}

func (l *LeasedLimiter) slice() int {
	s := int(math.Ceil(l.b.Rate() / float64(l.shares)))
	if s < 1 {
		s = 1
	}
	return s
}

// Wait blocks until one token is available or ctx is done.
func (l *LeasedLimiter) Wait(ctx context.Context) error {
	for {
		l.mu.Lock()
		if l.local > 0 {
			l.local--
			l.mu.Unlock()
			return nil
		}
		got, wait, err := l.b.Take(ctx, l.slice())
		if err != nil {
			l.mu.Unlock()
			return err
		}
		if got > 0 {
			l.local += got - 1
			l.mu.Unlock()
			return nil
		}
		l.mu.Unlock()
		if wait <= 0 {
			wait = 5 * time.Millisecond
		}
		t := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			t.Stop()
			return ctx.Err()
		case <-t.C:
		}
	}
}
