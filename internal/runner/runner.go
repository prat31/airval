// Package runner is the worker: it consumes run shard jobs from JetStream, calls targets, runs
// evaluators, batches results into the store and finalizes runs (plan §3).
//
// Pipeline (per process):
//
//	JetStream pull consumer (bounded in-flight shards, heartbeats, graceful drain)
//	  → target stage: one goroutine per item, bounded by workers.concurrency, gated by the target's
//	    AIMD limiter (+ distributed rpm/tpm buckets), retries with jittered backoff, response cache
//	  → evaluator stage (buffered channel + pool): deterministic evaluators inline, LLM judges in a
//	    separate per-provider pool, so slow judges never hold target slots
//	  → ingest stage (buffered channel): batched writes (250 ms / 200 rows), progress events,
//	    review items
//	  → shard done → last shard publishes the finalize job → lease-guarded finalizer computes
//	    aggregates, evaluates gates, writes the summary and notifies.
package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"runtime"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/airval/airval/internal/aggregate"
	"github.com/airval/airval/internal/cache"
	"github.com/airval/airval/internal/config"
	"github.com/airval/airval/internal/notify"
	"github.com/airval/airval/internal/queue"
	"github.com/airval/airval/internal/secrets"
	"github.com/airval/airval/internal/store"
	"github.com/airval/airval/internal/targets"
	"github.com/airval/airval/internal/telemetry"
)

// Deps are the worker's collaborators.
type Deps struct {
	Store   store.Store
	Queue   *queue.Queue
	Keyring *secrets.Keyring
	Config  config.Workers
	Logger  *slog.Logger
	Metrics *telemetry.Metrics
	// WorkerID identifies this process (leases, logs).
	WorkerID string

	// BaseURL is the public server URL (links in notifications).
	BaseURL string
	// Clients is the shared HTTP client factory (SSRF guard). Nil uses targets.DefaultClients().
	Clients *targets.ClientFactory
	// Cache is the response cache. Nil creates one backed by Store.
	Cache *cache.Cache
	// Notifier delivers run notifications (nil = none).
	Notifier *notify.Notifier
	// MaxAttempts per target/judge call (default 4).
	MaxAttempts int
	// FlushInterval / FlushRows tune ingest batching (defaults 250 ms / 200 rows).
	FlushInterval time.Duration
	FlushRows     int
}

// Worker runs until ctx is cancelled, then drains in-flight work within Config.ShutdownTimeout.
type Worker interface {
	Run(ctx context.Context) error
}

// New returns the worker implementation.
func New(d Deps) Worker { return NewEngine(d) }

// NewEngine returns the worker with its concrete type (exposes Stats for benchmarks).
func NewEngine(d Deps) *Engine {
	if d.Logger == nil {
		d.Logger = slog.Default()
	}
	if d.Metrics == nil {
		d.Metrics = telemetry.NewMetrics()
	}
	if d.Config.Concurrency <= 0 {
		d.Config.Concurrency = 256
	}
	if d.Config.JudgeConcurrency <= 0 {
		d.Config.JudgeConcurrency = 32
	}
	if d.Config.ShutdownTimeout <= 0 {
		d.Config.ShutdownTimeout = 30 * time.Second
	}
	if d.MaxAttempts <= 0 {
		d.MaxAttempts = 4
	}
	if d.FlushInterval <= 0 {
		d.FlushInterval = 250 * time.Millisecond
	}
	if d.FlushRows <= 0 {
		d.FlushRows = 200
	}
	if d.Clients == nil {
		d.Clients = targets.DefaultClients()
	}
	if d.WorkerID == "" {
		d.WorkerID = "worker-" + uuid.NewString()[:8]
	}
	w := &Engine{d: d, clients: d.Clients, cache: d.Cache,
		runs: map[uuid.UUID]*runState{}, buildLocks: map[uuid.UUID]*sync.Mutex{},
		limiters: map[uuid.UUID]*limiter{}, reviewQueues: map[string]uuid.UUID{},
		itemSem: make(chan struct{}, d.Config.Concurrency), overhead: aggregate.NewStream(0)}
	if w.cache == nil && d.Store != nil {
		w.cache = cache.New(d.Store, cache.Options{Logger: d.Logger})
		w.ownCache = true
	}
	w.judges = newJudgePool(d.Config.JudgeConcurrency, d.MaxAttempts, d.Clients, d.Metrics)
	return w
}

// Engine is the execution engine of one process.
type Engine struct {
	d        Deps
	bg       context.Context
	clients  *targets.ClientFactory
	cache    *cache.Cache
	ownCache bool
	judges   *judgePool
	cancels  *queue.CancelWatcher
	ing      *ingester
	evalCh   chan *evalJob
	itemSem  chan struct{}

	runsMu     sync.Mutex
	runs       map[uuid.UUID]*runState
	buildLocks map[uuid.UUID]*sync.Mutex

	limMu    sync.Mutex
	limiters map[uuid.UUID]*limiter

	rqMu         sync.Mutex
	reviewQueues map[string]uuid.UUID

	statMu   sync.Mutex
	overhead *aggregate.Stream // per-example engine overhead (ms), excluding target/judge time
	flushes  int
	flushMs  float64
	rows     int
}

// maxShards is how many shards a worker holds at once: enough items to keep every target slot
// busy (assuming the smallest shard size) with headroom for shards finishing their tail.
func (w *Engine) maxShards() int {
	return max(4, min(64, 2*((w.d.Config.Concurrency+24)/25)))
}

// Run consumes jobs until ctx is cancelled, then stops pulling, drains in-flight shards (naking
// what is left after the shutdown timeout) and flushes buffered results.
func (w *Engine) Run(ctx context.Context) error {
	if w.d.Queue == nil || w.d.Store == nil {
		return errors.New("runner: store and queue are required")
	}
	bg, stopBG := context.WithCancel(context.WithoutCancel(ctx))
	defer stopBG()
	w.bg = bg
	cw, err := w.d.Queue.WatchCancels(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return fmt.Errorf("runner: watch cancels: %w", err)
	}
	w.cancels = cw
	defer cw.Stop()

	w.ing = newIngester(w)
	w.ing.start()
	w.evalCh = make(chan *evalJob, 4*w.d.Config.Concurrency)
	var evalWG sync.WaitGroup
	nEval := max(16, 4*runtime.GOMAXPROCS(0), w.d.Config.JudgeConcurrency*2)
	for i := 0; i < nEval; i++ {
		evalWG.Add(1)
		go func() { defer evalWG.Done(); w.evalLoop() }()
	}
	var bgWG sync.WaitGroup
	bgWG.Add(1)
	go func() { defer bgWG.Done(); w.housekeeping(ctx) }()

	w.d.Logger.Info("worker started", "worker_id", w.d.WorkerID, "concurrency", w.d.Config.Concurrency,
		"judge_concurrency", w.d.Config.JudgeConcurrency, "max_shards", w.maxShards())
	err = w.d.Queue.ConsumeJobs(ctx, w.maxShards(), w.d.Config.ShutdownTimeout, w.handle)

	// Every handler has returned, so no item is in flight: stop the stages in order.
	close(w.evalCh)
	evalWG.Wait()
	w.ing.stop()
	bgWG.Wait()
	if w.ownCache {
		w.cache.Close()
	}
	w.d.Logger.Info("worker stopped", "worker_id", w.d.WorkerID)
	return err
}

func (w *Engine) handle(ctx context.Context, d queue.Delivery) error {
	runID, err := uuid.Parse(d.Job.RunID)
	if err != nil {
		return fmt.Errorf("%w: bad run id %q", queue.ErrNoRetry, d.Job.RunID)
	}
	switch d.Job.Kind {
	case queue.JobExecute, queue.JobRescore:
		return w.runShard(ctx, runID, d)
	case queue.JobFinalize:
		return w.finalize(ctx, runID)
	}
	return fmt.Errorf("%w: unknown job kind %q", queue.ErrNoRetry, d.Job.Kind)
}

// housekeeping reports queue lag and evicts idle run states.
func (w *Engine) housekeeping(ctx context.Context) {
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		if n, err := w.d.Queue.PendingJobs(ctx); err == nil {
			w.d.Metrics.QueueLag.WithLabelValues(queue.ConsumerJobs).Set(float64(n))
		}
		cutoff := time.Now().Add(-10 * time.Minute).UnixNano()
		w.runsMu.Lock()
		for id, rs := range w.runs {
			if rs.lastUsed.Load() < cutoff {
				delete(w.runs, id)
				delete(w.buildLocks, id)
			}
		}
		w.runsMu.Unlock()
	}
}

func (w *Engine) forget(runID uuid.UUID) {
	w.runsMu.Lock()
	delete(w.runs, runID)
	delete(w.buildLocks, runID)
	w.runsMu.Unlock()
}

func (w *Engine) cancelled(rs *runState) bool {
	return rs.stopped.Load() || (w.cancels != nil && w.cancels.Cancelled(rs.id.String()))
}

func (w *Engine) publish(runID uuid.UUID, typ string, data any) {
	b, err := json.Marshal(data)
	if err != nil {
		return
	}
	if err := w.d.Queue.PublishEvent(w.bg, queue.Event{Type: typ, RunID: runID.String(), Data: b}); err != nil {
		w.d.Logger.Debug("publish event", "run_id", runID, "type", typ, "err", err)
	}
}

func newLeased(w *Engine, name string, ratePerSec float64) *queue.LeasedLimiter {
	return queue.NewLeasedLimiter(w.d.Queue.TokenBucket(name, ratePerSec, max(1, ratePerSec)), 4)
}

// Stats are engine counters for benchmarks and diagnostics.
type Stats struct {
	Examples       int
	OverheadP50Ms  float64
	OverheadP95Ms  float64
	OverheadP99Ms  float64
	Flushes        int
	RowsFlushed    int
	FlushMsPerRow  float64
	MeanFlushMs    float64
	CacheHits      int64
	CacheMisses    int64
	TargetWindow   map[string]float64
	TargetThrottle map[string]int64
}

// Stats returns a snapshot of engine counters.
func (w *Engine) Stats() Stats {
	w.statMu.Lock()
	s := Stats{Examples: w.overhead.N(), OverheadP50Ms: w.overhead.Quantile(0.5), OverheadP95Ms: w.overhead.Quantile(0.95),
		OverheadP99Ms: w.overhead.Quantile(0.99), Flushes: w.flushes, RowsFlushed: w.rows}
	if w.rows > 0 {
		s.FlushMsPerRow = w.flushMs / float64(w.rows)
	}
	if w.flushes > 0 {
		s.MeanFlushMs = w.flushMs / float64(w.flushes)
	}
	w.statMu.Unlock()
	if w.cache != nil {
		s.CacheHits, s.CacheMisses, _ = w.cache.Stats()
	}
	s.TargetWindow, s.TargetThrottle = map[string]float64{}, map[string]int64{}
	w.limMu.Lock()
	for id, l := range w.limiters {
		win, _, thr, _ := l.snapshot()
		s.TargetWindow[id.String()], s.TargetThrottle[id.String()] = win, thr
	}
	w.limMu.Unlock()
	return s
}

// ResetStats clears overhead/flush counters (benchmarks call it between phases).
func (w *Engine) ResetStats() {
	w.statMu.Lock()
	w.overhead = aggregate.NewStream(0)
	w.flushes, w.flushMs, w.rows = 0, 0, 0
	w.statMu.Unlock()
}

func (w *Engine) recordOverhead(d time.Duration) {
	w.statMu.Lock()
	w.overhead.Add(float64(d) / float64(time.Millisecond))
	w.statMu.Unlock()
}
