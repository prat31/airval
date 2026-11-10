package queue

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func openTest(t *testing.T) *Queue {
	t.Helper()
	q, err := Open(context.Background(), Options{StoreDir: t.TempDir(), Port: -1, JobAckWait: 2 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(q.Close)
	return q
}

func TestJobsWorkQueue(t *testing.T) {
	q := openTest(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := q.Ready(ctx); err != nil {
		t.Fatal(err)
	}
	var jobs []Job
	for i := 0; i < 20; i++ {
		jobs = append(jobs, Job{Kind: JobExecute, RunID: "r1", Shard: i, ShardCount: 20})
	}
	if err := q.PublishJobs(ctx, jobs); err != nil {
		t.Fatal(err)
	}
	// Duplicate publish is de-duplicated.
	if err := q.PublishJob(ctx, jobs[0]); err != nil {
		t.Fatal(err)
	}
	var (
		mu      sync.Mutex
		seen    = map[int]int{}
		failed  atomic.Bool
		handled atomic.Int32
	)
	cctx, ccancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() {
		done <- q.ConsumeJobs(cctx, 4, time.Second, func(ctx context.Context, d Delivery) error {
			// Fail shard 3 once to exercise redelivery.
			if d.Job.Shard == 3 && !failed.Swap(true) {
				return context.DeadlineExceeded
			}
			mu.Lock()
			seen[d.Job.Shard]++
			mu.Unlock()
			if handled.Add(1) == 20 {
				ccancel()
			}
			return nil
		})
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatalf("timeout; handled %d", handled.Load())
	}
	if len(seen) != 20 {
		t.Fatalf("saw %d shards", len(seen))
	}
	for s, n := range seen {
		if n != 1 {
			t.Fatalf("shard %d handled %d times", s, n)
		}
	}
	pending, err := q.PendingJobs(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if pending != 0 {
		t.Fatalf("pending %d", pending)
	}
}

func TestEvents(t *testing.T) {
	q := openTest(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for i := 0; i < 3; i++ {
		data, _ := json.Marshal(map[string]int{"completed": i})
		if err := q.PublishEvent(ctx, Event{Type: EventProgress, RunID: "run-a", Data: data}); err != nil {
			t.Fatal(err)
		}
	}
	_ = q.PublishEvent(ctx, Event{Type: EventProgress, RunID: "run-b"})
	ch, err := q.SubscribeEvents(ctx, "run-a", 0)
	if err != nil {
		t.Fatal(err)
	}
	var got []Event
	for len(got) < 3 {
		select {
		case e := <-ch:
			got = append(got, e)
		case <-ctx.Done():
			t.Fatalf("got %d events", len(got))
		}
	}
	if got[2].Seq <= got[0].Seq || got[0].RunID != "run-a" {
		t.Fatalf("bad events %+v", got)
	}
	// Resume after the second event.
	sctx, scancel := context.WithCancel(ctx)
	ch2, err := q.SubscribeEvents(sctx, "run-a", got[1].Seq)
	if err != nil {
		t.Fatal(err)
	}
	e := <-ch2
	if e.Seq != got[2].Seq {
		t.Fatalf("resume: got seq %d want %d", e.Seq, got[2].Seq)
	}
	scancel()
}

func TestLeases(t *testing.T) {
	q := openTest(t)
	ctx := context.Background()
	tests := []struct {
		holder string
		want   bool
	}{
		{"a", true},  // free → a
		{"b", false}, // held by a
		{"a", true},  // renew
	}
	for _, tc := range tests {
		got, err := q.AcquireLease(ctx, "scheduler", tc.holder, 300*time.Millisecond)
		if err != nil || got != tc.want {
			t.Fatalf("acquire %s: got %v err %v want %v", tc.holder, got, err, tc.want)
		}
	}
	time.Sleep(350 * time.Millisecond)
	if ok, _ := q.AcquireLease(ctx, "scheduler", "b", time.Second); !ok {
		t.Fatal("b should take expired lease")
	}
	if err := q.ReleaseLease(ctx, "scheduler", "b"); err != nil {
		t.Fatal(err)
	}
	if ok, _ := q.AcquireLease(ctx, "scheduler", "a", time.Second); !ok {
		t.Fatal("a should take released lease")
	}

	// RunAsLeader: only one of two contenders runs at a time.
	lctx, cancel := context.WithTimeout(ctx, 700*time.Millisecond)
	defer cancel()
	var running, maxRunning atomic.Int32
	fn := func(ctx context.Context) {
		n := running.Add(1)
		if n > maxRunning.Load() {
			maxRunning.Store(n)
		}
		<-ctx.Done()
		running.Add(-1)
	}
	var wg sync.WaitGroup
	for _, h := range []string{"x", "y"} {
		wg.Add(1)
		go func(h string) { defer wg.Done(); q.RunAsLeader(lctx, "leader-test", h, 300*time.Millisecond, fn) }(h)
	}
	wg.Wait()
	if maxRunning.Load() != 1 {
		t.Fatalf("max concurrent leaders %d", maxRunning.Load())
	}
}

func TestCancelFlags(t *testing.T) {
	q := openTest(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := q.SetCancel(ctx, "early"); err != nil {
		t.Fatal(err)
	}
	w, err := q.WatchCancels(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Stop()
	if !w.Cancelled("early") {
		t.Fatal("initial value not loaded")
	}
	if err := q.SetCancel(ctx, "r2"); err != nil {
		t.Fatal(err)
	}
	ok, err := q.IsCancelled(ctx, "r2")
	if err != nil || !ok {
		t.Fatal("IsCancelled")
	}
	deadline := time.Now().Add(2 * time.Second)
	for !w.Cancelled("r2") {
		if time.Now().After(deadline) {
			t.Fatal("watcher did not see cancel")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if w.Cancelled("nope") {
		t.Fatal("false positive")
	}
}

func TestTokenBucket(t *testing.T) {
	q := openTest(t)
	ctx := context.Background()
	b := q.TokenBucket("target:t1", 100, 10)
	got, _, err := b.Take(ctx, 8)
	if err != nil || got != 8 {
		t.Fatalf("take 8: %d %v", got, err)
	}
	got, wait, _ := b.Take(ctx, 5)
	if got != 2 || wait <= 0 {
		t.Fatalf("take 5: got %d wait %s", got, wait)
	}
	// Two leased limiters sharing the bucket stay near the rate.
	b2 := q.TokenBucket("target:t2", 200, 20)
	l1, l2 := NewLeasedLimiter(b2, 2), NewLeasedLimiter(b2, 2)
	start := time.Now()
	var n atomic.Int32
	var wg sync.WaitGroup
	for _, l := range []*LeasedLimiter{l1, l2} {
		wg.Add(1)
		go func(l *LeasedLimiter) {
			defer wg.Done()
			for i := 0; i < 60; i++ {
				if err := l.Wait(ctx); err != nil {
					t.Error(err)
					return
				}
				n.Add(1)
			}
		}(l)
	}
	wg.Wait()
	el := time.Since(start)
	// 120 tokens at 200/s with burst 20 → ≥ ~0.5s.
	if el < 400*time.Millisecond {
		t.Fatalf("limiter too permissive: %d tokens in %s", n.Load(), el)
	}
}
