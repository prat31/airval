package planner

import (
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
)

// Run modes.
const (
	ModeExecute = "execute" // call the target, then evaluate
	ModeRescore = "rescore" // re-evaluate the outputs of a source run (no target calls)
)

// Start kinds (StartRequest.Kind).
const (
	KindSuite       = ""             // a fresh run of the suite
	KindRerunFailed = "rerun_failed" // child run over a run's failed/errored results
	KindRescore     = "rescore"      // child run re-scoring a run's outputs
)

// RunConfig is the resolved, immutable snapshot of everything a run needs. It is stored in
// runs.config so workers never re-read the (mutable) suite, evaluators or gates.
type RunConfig struct {
	Version           int               `json:"v"`
	Mode              string            `json:"mode"`
	SuiteName         string            `json:"suite_name,omitempty"`
	TargetName        string            `json:"target_name,omitempty"`
	TargetKind        string            `json:"target_kind,omitempty"`
	DatasetVersionIDs []uuid.UUID       `json:"dataset_version_ids,omitempty"`
	Evaluators        []EvaluatorRef    `json:"evaluators"`
	Gates             []GateRef         `json:"gates,omitempty"`
	Trials            int               `json:"trials"`
	Sampling          *Sampling         `json:"sampling,omitempty"`
	Limits            Limits            `json:"limits"`
	Cache             CachePolicy       `json:"cache"`
	FailureBudget     *FailureBudget    `json:"failure_budget,omitempty"`
	Simulator         *Simulator        `json:"simulator,omitempty"`
	TimeoutSeconds    int               `json:"timeout_seconds,omitempty"`
	Variables         map[string]string `json:"variables,omitempty"`
	SourceRunID       *uuid.UUID        `json:"source_run_id,omitempty"`
	Baseline          string            `json:"baseline_policy,omitempty"`
	Shards            int               `json:"shards"`
	ShardSize         int               `json:"shard_size"`
}

// EvaluatorRef is an evaluator as attached to the run (definition + suite-level settings).
type EvaluatorRef struct {
	ID            uuid.UUID       `json:"id"`
	Name          string          `json:"name"`
	Kind          string          `json:"kind"`
	Config        json.RawMessage `json:"config,omitempty"`
	Version       int             `json:"version"`
	Weight        float64         `json:"weight,omitempty"`
	Threshold     *float64        `json:"threshold,omitempty"`
	Severity      string          `json:"severity,omitempty"`
	ApplyTo       string          `json:"apply_to,omitempty"`
	FailFast      bool            `json:"fail_fast,omitempty"`
	TurnAggregate string          `json:"turn_aggregate,omitempty"`
}

// GateRef is a gate snapshot (expression AST in the API's GateExpr shape).
type GateRef struct {
	ID   uuid.UUID       `json:"id"`
	Name string          `json:"name"`
	Expr json.RawMessage `json:"expr"`
}

// Sampling selects a subset of examples.
type Sampling struct {
	Mode    string   `json:"mode,omitempty"` // none | random | stratified
	N       int      `json:"n,omitempty"`
	Percent float64  `json:"percent,omitempty"`
	Seed    *int64   `json:"seed,omitempty"`
	Tags    []string `json:"-"`
}

// Active reports whether sampling reduces the example set.
func (s *Sampling) Active() bool {
	return s != nil && (s.Mode == "random" || s.Mode == "stratified") && (s.N > 0 || s.Percent > 0)
}

// Limits are per-target execution limits (suite `limits`).
type Limits struct {
	MaxConcurrency int   `json:"max_concurrency,omitempty"`
	RPM            int   `json:"rpm,omitempty"`
	TPM            int   `json:"tpm,omitempty"`
	Adaptive       *bool `json:"adaptive,omitempty"` // AIMD (default true)
}

// AdaptiveOn reports whether AIMD is enabled.
func (l Limits) AdaptiveOn() bool { return l.Adaptive == nil || *l.Adaptive }

// CachePolicy controls the response cache.
type CachePolicy struct {
	Target bool `json:"target"`
	Judges bool `json:"judges"`
}

// FailureBudget stops a doomed run early.
// Simulator is the LLM playing the user in simulated conversations.
type Simulator struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
	Secret   string `json:"secret,omitempty"`
	BaseURL  string `json:"base_url,omitempty"`
}

// FailureBudget cancels a run early once a limit is exceeded (nil = unlimited).
type FailureBudget struct {
	CriticalFailures *int     `json:"critical_failures,omitempty"`
	Failures         *int     `json:"failures,omitempty"`
	Errors           *int     `json:"errors,omitempty"`
	FailRate         *float64 `json:"fail_rate,omitempty"`   // 0..1, checked once MinResults results are in
	MinResults       int      `json:"min_results,omitempty"` // default 50
}

// ShardPayload is the queue.Job payload of an execute/rescore shard.
type ShardPayload struct {
	Items []Item            `json:"items"`
	Trace map[string]string `json:"trace,omitempty"` // W3C trace context (server → worker spans)
}

// Item is one example × trial of a shard. For rescore shards S is the source result.
type Item struct {
	E uuid.UUID  `json:"e"`
	R uuid.UUID  `json:"r"`
	T int        `json:"t,omitempty"`
	S *uuid.UUID `json:"s,omitempty"`
}

// InvalidError is a user-facing validation error (the API answers 400 validation_failed).
type InvalidError struct{ Msg string }

func (e *InvalidError) Error() string { return e.Msg }

func invalidf(format string, a ...any) error { return &InvalidError{Msg: fmt.Sprintf(format, a...)} }

// ParseRunConfig decodes a run's config snapshot.
func ParseRunConfig(raw json.RawMessage) (RunConfig, error) {
	var c RunConfig
	if len(raw) == 0 {
		return c, fmt.Errorf("planner: run has no config snapshot")
	}
	if err := json.Unmarshal(raw, &c); err != nil {
		return c, fmt.Errorf("planner: decode run config: %w", err)
	}
	if c.Trials < 1 {
		c.Trials = 1
	}
	if c.Mode == "" {
		c.Mode = ModeExecute
	}
	return c, nil
}

// ShardSize picks the shard size for a run of n items: about 64 shards, clamped to 25–100 items,
// so small runs spread across workers and big runs don't flood the queue.
func ShardSize(n int) int {
	s := (n + 63) / 64
	if s < 25 {
		s = 25
	}
	if s > 100 {
		s = 100
	}
	return s
}
