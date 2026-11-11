package runner

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/goleak"

	"github.com/airval/airval/internal/aggregate"
	"github.com/airval/airval/internal/evalcore"
	"github.com/airval/airval/internal/targets"
)

func TestMain(m *testing.M) { goleak.VerifyTestMain(m) }

func TestLimiterAIMD(t *testing.T) {
	l := newLimiter(64, true)
	ctx := context.Background()
	// Throttling halves the window (at most once per RTT).
	if err := l.Acquire(ctx); err != nil {
		t.Fatal(err)
	}
	l.Release(outcomeThrottled, time.Millisecond, 0)
	if w, _, thr, _ := l.snapshot(); w != 32 || thr != 1 {
		t.Fatalf("after 429: window=%v throttled=%d", w, thr)
	}
	_ = l.Acquire(ctx)
	l.Release(outcomeThrottled, time.Millisecond, 0) // within cooldown: no second cut
	if w, _, _, _ := l.snapshot(); w != 32 {
		t.Fatalf("cooldown ignored: %v", w)
	}
	// Additive increase: about +1 per window of successes.
	for i := 0; i < 32; i++ {
		_ = l.Acquire(ctx)
		l.Release(outcomeOK, time.Millisecond, 0)
	}
	if w, _, _, _ := l.snapshot(); w < 32.9 || w > 33.1 {
		t.Fatalf("additive increase: %v", w)
	}
}

func TestLimiterBoundsConcurrency(t *testing.T) {
	l := newLimiter(4, false)
	var cur, peak atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := l.Acquire(context.Background()); err != nil {
				t.Error(err)
				return
			}
			n := cur.Add(1)
			for {
				p := peak.Load()
				if n <= p || peak.CompareAndSwap(p, n) {
					break
				}
			}
			time.Sleep(time.Millisecond)
			cur.Add(-1)
			l.Release(outcomeOK, time.Millisecond, 0)
		}()
	}
	wg.Wait()
	if peak.Load() > 4 {
		t.Fatalf("peak %d > 4", peak.Load())
	}
}

func TestLimiterRetryAfterPausesAndCancel(t *testing.T) {
	l := newLimiter(2, true)
	_ = l.Acquire(context.Background())
	l.Release(outcomeThrottled, time.Millisecond, 150*time.Millisecond)
	start := time.Now()
	if err := l.Acquire(context.Background()); err != nil {
		t.Fatal(err)
	}
	if el := time.Since(start); el < 120*time.Millisecond {
		t.Fatalf("Retry-After not honored: %s", el)
	}
	l.Release(outcomeOK, 0, 0)
	// A waiter whose context ends leaves the queue cleanly.
	win, _, _, _ := l.snapshot()
	for i := 0; i < int(win); i++ { // fill the window
		_ = l.Acquire(context.Background())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := l.Acquire(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("want deadline, got %v", err)
	}
	for i := 0; i < int(win); i++ {
		l.Release(outcomeOK, 0, 0)
	}
	if _, inflight, _, _ := l.snapshot(); inflight != 0 {
		t.Fatalf("inflight leak: %d", inflight)
	}
}

func TestBackoffAndClassification(t *testing.T) {
	for a := 1; a <= 12; a++ {
		d := backoff(a, 0)
		if d <= 0 || d > backoffMax {
			t.Fatalf("attempt %d: %s", a, d)
		}
	}
	if d := backoff(1, 2*time.Second); d < 2*time.Second {
		t.Fatalf("Retry-After ignored: %s", d)
	}
	rl := targets.ErrRateLimited{RetryAfter: time.Second, Err: errors.New("429")}
	if o, ra := classifyOutcome(rl); o != outcomeThrottled || ra != time.Second || !retryable(rl) {
		t.Fatal("rate limit classification")
	}
	if o, _ := classifyOutcome(targets.ErrTransient{Err: errors.New("503")}); o != outcomeTransient {
		t.Fatal("transient classification")
	}
	if retryable(targets.ErrPermanent{Err: errors.New("400")}) || retryable(context.Canceled) {
		t.Fatal("permanent/canceled must not retry")
	}
}

func TestApplyVariables(t *testing.T) {
	raw := json.RawMessage(`{"url":"{{ .vars.base_url }}/chat","body":"{\"q\": {{ json .input.q }}, \"env\": \"{{vars.env}}\"}"}`)
	out, err := applyVariables(raw, map[string]string{"base_url": "https://pr-1.example.app", "env": `st"g`})
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]string
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatalf("invalid JSON after substitution: %v: %s", err, out)
	}
	if m["url"] != "https://pr-1.example.app/chat" || m["body"] != `{"q": {{ json .input.q }}, "env": "st"g"}` {
		t.Fatalf("substitution: %v", m)
	}
	if _, err := applyVariables(raw, nil); err == nil {
		t.Fatal("missing variables must error")
	}
}

func TestEvalGate(t *testing.T) {
	agg := aggregate.New([]aggregate.EvaluatorInfo{{ID: "e1", Severity: evalcore.SeverityCritical, Threshold: 0.5}}, aggregate.Options{})
	for i := 0; i < 10; i++ {
		v := 1.0
		passed := true
		if i < 2 {
			v, passed = 0, false
		}
		agg.Add(aggregate.Record{ExampleID: "x" + string(rune('a'+i)), Latency: 100 * time.Millisecond,
			Scores: []evalcore.Score{{EvaluatorID: "e1", Value: &v, Passed: &passed}}})
	}
	env := gateEnv{base: aggregate.SummaryEnv{Summary: agg.Summary()}}
	expr := func(s string) json.RawMessage { b, _ := json.Marshal(aggregate.MustParse(s)); return b }
	names := map[string]string{"e1": "Faithfulness"}
	if g := evalGate("g", "q", expr("mean(e1) >= 0.75 and p95(latency_ms) < 2000"), env, names); !g.Passed ||
		g.Text != "mean(faithfulness) >= 0.75 and p95(latency_ms) < 2000" {
		t.Fatalf("passing gate: %+v", g)
	}
	g := evalGate("g", "q", expr("count(critical_fail) == 0"), env, names)
	if g.Passed || g.Detail == "" {
		t.Fatalf("failing gate: %+v", g)
	}
	if b := aggregate.BudgetFromGate(aggregate.MustParse("count(critical_fail) <= 1")); b.MaxCriticalFails == nil || *b.MaxCriticalFails != 1 {
		t.Fatal("budget derivation")
	}
	if g := evalGate("g", "q", expr("mean(missing) >= 1"), env, names); g.Passed {
		t.Fatal("unknown evaluator must fail")
	}
}

func TestResultStatus(t *testing.T) {
	f, tr := false, true
	if s := resultStatus("boom", nil); s != "errored" {
		t.Fatal(s)
	}
	if s := resultStatus("", []evalcore.Score{{Passed: &tr}, {Passed: &f}}); s != "failed" {
		t.Fatal(s)
	}
	if s := resultStatus("", []evalcore.Score{{Passed: &tr}, {Label: "pending"}}); s != "passed" {
		t.Fatal(s)
	}
}
