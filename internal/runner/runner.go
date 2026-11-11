// Package runner is the worker: it consumes run shard jobs from JetStream, calls targets, runs
// evaluators, batches results into the store and finalizes runs.
//
// TODO(engine): replace the placeholder returned by New with the real pipeline (plan §3). The
// app wires Worker into `airval worker` and (embedded) `airval dev`; keep this signature.
package runner

import (
	"context"
	"log/slog"
	"time"

	"github.com/airval/airval/internal/config"
	"github.com/airval/airval/internal/queue"
	"github.com/airval/airval/internal/secrets"
	"github.com/airval/airval/internal/store"
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
}

// Worker runs until ctx is cancelled, then drains in-flight work within Config.ShutdownTimeout.
type Worker interface {
	Run(ctx context.Context) error
}

// New returns the worker implementation.
func New(d Deps) Worker { return &placeholder{d: d} }

// placeholder doesn't consume jobs (so nothing is lost before the engine lands); it only
// reports queue lag so the metrics pipeline is exercised.
type placeholder struct{ d Deps }

func (p *placeholder) Run(ctx context.Context) error {
	p.d.Logger.Info("worker started (execution engine not yet installed)", "worker_id", p.d.WorkerID,
		"concurrency", p.d.Config.Concurrency)
	t := time.NewTicker(15 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			p.d.Logger.Info("worker stopped", "worker_id", p.d.WorkerID)
			return nil
		case <-t.C:
			if p.d.Queue == nil || p.d.Metrics == nil {
				continue
			}
			if n, err := p.d.Queue.PendingJobs(ctx); err == nil {
				p.d.Metrics.QueueLag.WithLabelValues(queue.ConsumerJobs).Set(float64(n))
			}
		}
	}
}
