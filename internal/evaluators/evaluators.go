// Package evaluators turns (Example, Response) pairs into Scores.
//
// It holds three families of evaluators:
//
//   - deterministic: pure Go checks that run in microseconds (equals, regex,
//     json_schema, rouge_l, tool_called, no_pii, ...);
//   - model-based: LLM judges (llm_rubric and versioned presets such as
//     faithfulness), pairwise comparison against a baseline, and
//     embedding_similarity;
//   - human: emits a pending score and asks the runner to enqueue a review.
//
// Every kind is created through the registry ([New] / [Build]) from a raw JSON
// config, which is decoded into an exported, validated config struct. [Catalog]
// describes every kind (description, JSON schema of its config, default
// threshold) so the UI can render pickers and forms without hard-coding them.
//
// The runner usually works with [Bound] evaluators (an evaluator plus its
// suite-level settings: id, threshold, severity, apply_to, fail_fast) and a
// [Pipeline] that orders them cheap → expensive and short-circuits.
package evaluators

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/prat31/airval/internal/evalcore"
)

// CostClass orders evaluators from cheap to expensive.
type CostClass int

const (
	// CostFree evaluators are pure computation (microseconds, no I/O).
	CostFree CostClass = iota
	// CostModel evaluators call an LLM or embedding model.
	CostModel
	// CostHuman evaluators wait for a human reviewer.
	CostHuman
)

func (c CostClass) String() string {
	switch c {
	case CostFree:
		return "free"
	case CostModel:
		return "model"
	case CostHuman:
		return "human"
	}
	return "unknown"
}

// MarshalText makes CostClass render as a string in JSON.
func (c CostClass) MarshalText() ([]byte, error) { return []byte(c.String()), nil }

// Input is what an evaluator scores.
type Input struct {
	Example  *evalcore.Example
	Response *evalcore.Response
	// Baseline is the baseline run's response for the same example (pairwise).
	Baseline *evalcore.Response
	// Turn is the index into the original Response.Transcript when the
	// evaluator is applied per turn; nil otherwise.
	Turn *int
	// Prior holds the scores of evaluators that already ran for this
	// example (set by Pipeline). The human evaluator uses it for
	// "failures only" review queues.
	Prior []evalcore.Score
}

func (in Input) example() *evalcore.Example {
	if in.Example == nil {
		return &evalcore.Example{}
	}
	return in.Example
}

func (in Input) response() *evalcore.Response {
	if in.Response == nil {
		return &evalcore.Response{}
	}
	return in.Response
}

// Evaluator scores one response.
//
// Evaluate returns an error only for operational failures (LLM unreachable,
// missing required data); a bad output is expressed as a low/failed Score.
type Evaluator interface {
	Kind() string
	Cost() CostClass
	Evaluate(ctx context.Context, in Input) (evalcore.Score, error)
}

// LLMFactory returns a chat-completion client for a judge. secret is the name
// of a Secret holding the API key (may be empty for keyless providers).
type LLMFactory func(provider, model, secret string) (evalcore.LLM, error)

// Embedder turns texts into vectors.
type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
}

// EmbedderFactory returns an Embedder for a provider/model/secret triple.
type EmbedderFactory func(provider, model, secret string) (Embedder, error)

// Cache stores judge results and embeddings. Implementations must be safe for
// concurrent use. A miss is (nil, false, nil).
type Cache interface {
	Get(ctx context.Context, key string) ([]byte, bool, error)
	Set(ctx context.Context, key string, value []byte) error
}

// Deps are the runtime services model-based evaluators need. Deterministic
// and human evaluators need none, so a zero Deps is valid for them.
type Deps struct {
	LLMs      LLMFactory
	Embedders EmbedderFactory
	Cache     Cache // optional
}

// Errors returned by the registry.
var (
	ErrUnknownKind = errors.New("unknown evaluator kind")
	ErrNoLLM       = errors.New("no LLM provider configured for judges")
	ErrNoEmbedder  = errors.New("no embedding provider configured")
)

// Score.Meta keys with a meaning shared with the runner and the UI.
const (
	MetaError          = "error"           // string: evaluator error message
	MetaSkipped        = "skipped"         // bool: short-circuited by fail_fast
	MetaReviewRequired = "review_required" // bool: runner must enqueue a review item
	MetaReview         = "review"          // object: review queue settings
	MetaTurnScores     = "turn_scores"     // []TurnScore for per-turn evaluation
	MetaTurn           = "turn"            // int: transcript index for per-turn scores
	MetaCached         = "cached"          // bool: judge result came from cache
	MetaPromptVersion  = "prompt_version"  // string: judge prompt version
	MetaThreshold      = "threshold"       // float64: threshold used for Passed
)

// Labels set by the framework.
const (
	LabelError   = "error"
	LabelSkipped = "skipped"
	LabelPending = "pending"
	LabelPass    = "pass"
	LabelFail    = "fail"
)

// rawOrEmpty returns raw, or an empty object for nil/null raw configs.
func rawOrEmpty(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 || string(raw) == "null" {
		return json.RawMessage("{}")
	}
	return raw
}

func f64(v float64) *float64 { return &v }
func boolp(v bool) *bool     { return &v }

// boolScore builds a 1/0 score with Passed set.
func boolScore(ok bool, reason string) evalcore.Score {
	s := evalcore.Score{Passed: boolp(ok), Reason: reason}
	if ok {
		s.Value = f64(1)
		s.Label = LabelPass
	} else {
		s.Value = f64(0)
		s.Label = LabelFail
	}
	return s
}

// valueScore builds a score with a continuous value; passed is computed
// against threshold.
func valueScore(v, threshold float64, reason string) evalcore.Score {
	v = clamp01(v)
	ok := v >= threshold
	s := evalcore.Score{Value: f64(v), Passed: boolp(ok), Reason: reason}
	if ok {
		s.Label = LabelPass
	} else {
		s.Label = LabelFail
	}
	return s
}

func clamp01(v float64) float64 {
	if v < 0 || v != v {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
