package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// EngineStore holds the queries the execution engine (planner, runner, finalizer) needs beyond the
// CRUD areas: shard bookkeeping, atomic run counters, run completion and lightweight result lookups.
type EngineStore interface {
	// CreateRunShards records the shards of a run; items[i] is the item count of shard i.
	CreateRunShards(ctx context.Context, runID uuid.UUID, items []int) error
	// CompleteRunShard marks a shard done (idempotent: only the first completion stores stats)
	// and returns how many shards of the run are still pending.
	CompleteRunShard(ctx context.Context, runID uuid.UUID, shard int, stats json.RawMessage) (remaining int, err error)
	// ListRunShards returns a run's shards in order.
	ListRunShards(ctx context.Context, runID uuid.UUID) ([]RunShard, error)

	// IncrRunProgress atomically increments run counters and returns the new totals.
	IncrRunProgress(ctx context.Context, id uuid.UUID, d RunProgress) (RunCounters, error)
	// SetRunError records a message on a run (e.g. why it stopped early) without touching other fields.
	SetRunError(ctx context.Context, id uuid.UUID, msg string) error
	// FinishRun writes the final counters, summary and gate verdict. The status is only changed
	// while the run is queued or running (a canceled run stays canceled); the resulting status is returned.
	FinishRun(ctx context.Context, id uuid.UUID, f RunFinish) (string, error)

	// ListRunResultRefs returns lightweight references to a run's results (optionally filtered by
	// status), in id order, without loading outputs.
	ListRunResultRefs(ctx context.Context, runID uuid.UUID, statuses []string) ([]ResultRef, error)
	// ListResultsForExamples returns a run's results (without scores) for the given examples.
	ListResultsForExamples(ctx context.Context, runID uuid.UUID, exampleIDs []uuid.UUID) ([]Result, error)
}

// RunShard is one shard of a run.
type RunShard struct {
	RunID  uuid.UUID
	Shard  int
	Items  int
	Stats  json.RawMessage
	DoneAt *time.Time
}

// RunCounters are a run's progress counters.
type RunCounters struct {
	Total     int
	Completed int
	Failed    int
	Errored   int
	CostUSD   float64
	TokensIn  int64
	TokensOut int64
}

// RunFinish is the final state written by the finalizer.
type RunFinish struct {
	Status     string // applied only while the run is queued/running
	Completed  int
	Failed     int
	Errored    int
	CostUSD    float64
	TokensIn   int64
	TokensOut  int64
	Summary    json.RawMessage
	GatePassed *bool
	Error      string // replaces the run error when non-empty
	FinishedAt time.Time
}

// ResultRef identifies one result of a run.
type ResultRef struct {
	ID                uuid.UUID
	ExampleID         uuid.UUID
	ExampleRevisionID uuid.UUID
	Trial             int
	Status            string
}
