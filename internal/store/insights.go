package store

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// InsightStore holds read-optimized queries behind the dashboards: trends, project overview
// tiles and judge-vs-human agreement data. Each method is a single (or a handful of) indexed
// queries, safe to call on every page view.
type InsightStore interface {
	// TrendRows returns one row per finished run (× evaluator for score_mean) in the project
	// matching q, oldest first, joined with the run's aggregates in a single query.
	TrendRows(ctx context.Context, q TrendQuery) ([]TrendRow, error)
	// ProjectStats computes the counters behind the project overview tiles.
	ProjectStats(ctx context.Context, projectID uuid.UUID, now time.Time) (ProjectStats, error)
	// AgreementPairs returns, for every result scored by the evaluator that also has a human
	// annotation (for that evaluator, or unscoped), the evaluator's score and each annotation.
	AgreementPairs(ctx context.Context, evaluatorID uuid.UUID, limit int) ([]AgreementPair, error)
}

// Trend metrics understood by TrendRows.
const (
	TrendPassRate    = "pass_rate"
	TrendScoreMean   = "score_mean"
	TrendLatencyP50  = "latency_p50"
	TrendLatencyP95  = "latency_p95"
	TrendCostUSD     = "cost_usd"
	TrendTokens      = "tokens"
	DefaultTrendRows = 5000
)

// TrendQuery selects runs for a time series. Only finished runs (succeeded or failed) count.
type TrendQuery struct {
	ProjectID   uuid.UUID
	SuiteID     *uuid.UUID
	TargetID    *uuid.UUID
	EvaluatorID *uuid.UUID // score_mean only
	Metric      string
	From, To    *time.Time // on run creation time; To is exclusive
	Limit       int        // default DefaultTrendRows (the most recent rows are kept)
}

// TrendRow is one point of a series.
type TrendRow struct {
	RunID       uuid.UUID
	SuiteID     *uuid.UUID
	TargetID    *uuid.UUID
	EvaluatorID uuid.UUID // NilID unless the metric is per evaluator
	At          time.Time // finish time (creation time when unknown)
	Value       *float64
	N           int
}

// ProjectStats feeds the overview tiles.
type ProjectStats struct {
	Runs7d          int
	RunsPrev7d      int
	Spend7dUSD      float64
	SpendPrev7dUSD  float64
	FailingSuiteIDs []uuid.UUID // suites whose latest finished run failed its gate
	FailingRunIDs   []uuid.UUID // the corresponding runs (same order)
}

// AgreementPair is one result with the evaluator's verdict and the human annotations on it.
type AgreementPair struct {
	ResultID     uuid.UUID
	JudgeValue   *float64
	JudgePassed  *bool
	HumanValues  []float64 // one per annotation that carries a value
	HumanPassed  []bool    // one per annotation that carries a verdict
	AnnotationCt int
}
