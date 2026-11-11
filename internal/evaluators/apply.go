package evaluators

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"sync"
	"time"

	"github.com/airval/airval/internal/evalcore"
)

// Turn aggregation modes for per-turn evaluation.
const (
	TurnMean = "mean"
	TurnMin  = "min"
)

// Spec is how a suite attaches an evaluator (plan §6): which kind, its
// config, and the suite-level threshold, severity, apply-to and fail-fast.
type Spec struct {
	ID            string            `json:"id"` // unique within the suite; referenced by gates. Defaults to Kind.
	Kind          string            `json:"kind"`
	Config        json.RawMessage   `json:"config,omitempty"`
	Threshold     *float64          `json:"threshold,omitempty"`      // pass if value >= threshold; default from Catalog
	Severity      evalcore.Severity `json:"severity,omitempty"`       // default major
	ApplyTo       evalcore.ApplyTo  `json:"apply_to,omitempty"`       // default final
	TurnAggregate string            `json:"turn_aggregate,omitempty"` // mean | min, for apply_to=per_turn
	FailFast      bool              `json:"fail_fast,omitempty"`      // a failure skips model-based evaluators for the example
	Weight        float64           `json:"weight,omitempty"`         // default 1
}

// Bound is an Evaluator with its suite-level settings applied.
type Bound struct {
	Spec
	Evaluator Evaluator
	threshold float64
}

// Build validates spec and creates its evaluator.
func Build(spec Spec, deps Deps) (*Bound, error) {
	if spec.ID == "" {
		spec.ID = spec.Kind
	}
	info, err := Info(spec.Kind)
	if err != nil {
		return nil, err
	}
	if spec.Severity == "" {
		spec.Severity = evalcore.SeverityMajor
	}
	switch spec.Severity {
	case evalcore.SeverityCritical, evalcore.SeverityMajor, evalcore.SeverityMinor:
	default:
		return nil, fmt.Errorf("evaluator %s: severity must be critical, major or minor (got %q)", spec.ID, spec.Severity)
	}
	if spec.ApplyTo == "" {
		spec.ApplyTo = evalcore.ApplyFinal
	}
	if !slices.Contains(info.ApplyTo, spec.ApplyTo) {
		return nil, fmt.Errorf("evaluator %s: %s cannot be applied to %q (supported: %v)", spec.ID, spec.Kind, spec.ApplyTo, info.ApplyTo)
	}
	switch spec.TurnAggregate {
	case "":
		spec.TurnAggregate = TurnMean
	case TurnMean, TurnMin:
	default:
		return nil, fmt.Errorf("evaluator %s: turn_aggregate must be mean or min", spec.ID)
	}
	if spec.Weight == 0 {
		spec.Weight = 1
	}
	t := info.DefaultThreshold
	if spec.Threshold != nil {
		t = *spec.Threshold
		if t < 0 || t > 1 || math.IsNaN(t) {
			return nil, fmt.Errorf("evaluator %s: threshold must be between 0 and 1", spec.ID)
		}
	}
	ev, err := New(spec.Kind, spec.Config, deps)
	if err != nil {
		return nil, fmt.Errorf("evaluator %s: %w", spec.ID, err)
	}
	return &Bound{Spec: spec, Evaluator: ev, threshold: t}, nil
}

// Threshold is the effective pass threshold.
func (b *Bound) Threshold() float64 { return b.threshold }

// Cost is the evaluator's cost class.
func (b *Bound) Cost() CostClass { return b.Evaluator.Cost() }

// ErrorScore turns an evaluator error into a Score (no value, label "error").
func ErrorScore(id string, err error) evalcore.Score {
	return evalcore.Score{
		EvaluatorID: id,
		Label:       LabelError,
		Reason:      err.Error(),
		Meta:        map[string]any{MetaError: err.Error()},
	}
}

// SkippedScore is recorded for evaluators short-circuited by fail_fast.
func SkippedScore(id, reason string) evalcore.Score {
	return evalcore.Score{
		EvaluatorID: id,
		Label:       LabelSkipped,
		Reason:      reason,
		Meta:        map[string]any{MetaSkipped: true},
	}
}

// Evaluate runs the evaluator according to apply_to, sets EvaluatorID and
// Latency, and applies the threshold. It never returns an error: failures
// become a Score with Label "error".
func (b *Bound) Evaluate(ctx context.Context, in Input) evalcore.Score {
	start := time.Now()
	var (
		s   evalcore.Score
		err error
	)
	switch b.ApplyTo {
	case evalcore.ApplyPerTurn:
		s, err = EvaluatePerTurn(ctx, b.Evaluator, in, b.TurnAggregate, b.threshold)
	case evalcore.ApplyConversation:
		s, err = b.Evaluator.Evaluate(ctx, ConversationInput(in))
	case evalcore.ApplyTrace:
		s, err = b.Evaluator.Evaluate(ctx, TraceInput(in))
	default:
		s, err = b.Evaluator.Evaluate(ctx, in)
	}
	if err != nil {
		s = ErrorScore(b.ID, err)
	}
	s.EvaluatorID = b.ID
	s.Latency = time.Since(start)
	applyThreshold(&s, b.threshold)
	return s
}

// applyThreshold sets Passed = value >= t for scored results.
func applyThreshold(s *evalcore.Score, t float64) {
	if s.Value == nil {
		return
	}
	switch s.Label {
	case LabelError, LabelPending, LabelSkipped:
		return
	}
	ok := *s.Value >= t
	s.Passed = &ok
	switch s.Label {
	case "", LabelPass, LabelFail:
		if ok {
			s.Label = LabelPass
		} else {
			s.Label = LabelFail
		}
	}
}

// ---------------------------------------------------------------- apply_to

// TurnScore is one turn's result inside a per-turn score (Meta["turn_scores"]).
type TurnScore struct {
	Turn   int      `json:"turn"` // index into Response.Transcript
	Value  *float64 `json:"value,omitempty"`
	Passed *bool    `json:"passed,omitempty"`
	Label  string   `json:"label,omitempty"`
	Reason string   `json:"reason,omitempty"`
	Error  string   `json:"error,omitempty"`
}

// turnExpectations returns the per-turn expectations of a scripted
// conversation: the k-th entry belongs to the k-th assistant reply.
func turnExpectations(ex *evalcore.Example) ([]string, bool) {
	if ex.Conversation == nil {
		return nil, false
	}
	var out []string
	found := false
	for _, t := range ex.Conversation.Turns {
		if t.Role == "user" {
			out = append(out, t.Expected)
			found = found || t.Expected != ""
		}
	}
	return out, found
}

// TurnInputs splits in into one Input per assistant message of the
// transcript. Each turn's Response has Output/ToolCalls of that message and
// the transcript up to and including it; its Example.Expected is the
// scripted per-turn expectation when the conversation defines any.
func TurnInputs(in Input) []Input {
	r, ex := in.response(), in.example()
	exps, hasExps := turnExpectations(ex)
	var out []Input
	k := 0
	for i, m := range r.Transcript {
		if m.Role != "assistant" {
			continue
		}
		sub := *r
		sub.Output = m.Content
		sub.OutputJSON = nil
		sub.ToolCalls = m.ToolCalls
		sub.Transcript = r.Transcript[:i+1]
		e := *ex
		if hasExps {
			e.Expected = nil
			if k < len(exps) && exps[k] != "" {
				e.Expected = exps[k]
			}
		}
		turn := i
		ti := in
		ti.Response, ti.Example, ti.Turn = &sub, &e, &turn
		out = append(out, ti)
		k++
	}
	return out
}

// EvaluatePerTurn runs ev on every assistant turn of the transcript and
// aggregates the values with mean or min. Turns that error are reported in
// Meta["turn_scores"] and excluded; if every turn errors, the first error is
// returned. Without a transcript it evaluates the final output.
func EvaluatePerTurn(ctx context.Context, ev Evaluator, in Input, agg string, threshold float64) (evalcore.Score, error) {
	turns := TurnInputs(in)
	if len(turns) == 0 {
		return ev.Evaluate(ctx, in)
	}
	scores := make([]evalcore.Score, len(turns))
	errs := make([]error, len(turns))
	if ev.Cost() == CostFree {
		for i, ti := range turns {
			scores[i], errs[i] = ev.Evaluate(ctx, ti)
		}
	} else {
		var wg sync.WaitGroup
		for i, ti := range turns {
			wg.Add(1)
			go func() {
				defer wg.Done()
				scores[i], errs[i] = ev.Evaluate(ctx, ti)
			}()
		}
		wg.Wait()
	}
	ts := make([]TurnScore, len(turns))
	var vals []float64
	cost := 0.0
	var firstErr error
	worst := -1
	for i := range turns {
		ts[i].Turn = *turns[i].Turn
		if errs[i] != nil {
			ts[i].Error = errs[i].Error()
			if firstErr == nil {
				firstErr = errs[i]
			}
			continue
		}
		s := scores[i]
		applyThreshold(&s, threshold)
		ts[i].Value, ts[i].Passed, ts[i].Label, ts[i].Reason = s.Value, s.Passed, s.Label, s.Reason
		cost += s.CostUSD
		if s.Value != nil {
			vals = append(vals, *s.Value)
			if worst < 0 || *s.Value < *scores[worst].Value {
				worst = i
			}
		}
	}
	if len(vals) == 0 {
		if firstErr != nil {
			return evalcore.Score{}, firstErr
		}
		return evalcore.Score{Label: LabelPending, Meta: map[string]any{MetaTurnScores: ts}}, nil
	}
	v := 0.0
	if agg == TurnMin {
		v = math.Inf(1)
		for _, x := range vals {
			v = math.Min(v, x)
		}
	} else {
		for _, x := range vals {
			v += x
		}
		v /= float64(len(vals))
	}
	failed := 0
	for _, t := range ts {
		if t.Passed != nil && !*t.Passed {
			failed++
		}
	}
	reason := fmt.Sprintf("%s over %d turns: %.3f; %d turn(s) below threshold", agg, len(vals), v, failed)
	if worst >= 0 && failed > 0 {
		reason += fmt.Sprintf(". Worst turn %d: %s", ts[worst].Turn, scores[worst].Reason)
	}
	s := valueScore(v, threshold, reason)
	s.CostUSD = cost
	s.Meta = map[string]any{MetaTurnScores: ts, "turn_aggregate": agg}
	return s, nil
}

// ConversationInput prepares in for apply_to = conversation: Output becomes
// the rendered transcript and ToolCalls collects every call in the
// conversation. Judges additionally see the transcript itself.
func ConversationInput(in Input) Input {
	r := in.response()
	if len(r.Transcript) == 0 {
		return in
	}
	sub := *r
	sub.Output = renderTranscript(r)
	sub.OutputJSON = nil
	var calls []evalcore.ToolCall
	for _, m := range r.Transcript {
		calls = append(calls, m.ToolCalls...)
	}
	if len(calls) == 0 {
		calls = r.ToolCalls
	}
	sub.ToolCalls = calls
	out := in
	out.Response = &sub
	return out
}

// TraceInput prepares in for apply_to = trace: ToolCalls come from tool
// steps and, when the response has no context, Context comes from retrieval
// steps of the trace.
func TraceInput(in Input) Input {
	r := in.response()
	if len(r.Trace) == 0 {
		return in
	}
	sub := *r
	var calls []evalcore.ToolCall
	var ctxs []string
	for _, st := range r.Trace {
		switch st.Kind {
		case "tool":
			calls = append(calls, evalcore.ToolCall{Name: st.Name, Arguments: st.Input, Result: st.Output})
		case "retrieval":
			ctxs = append(ctxs, retrievalTexts(st.Output)...)
		}
	}
	if len(calls) > 0 {
		sub.ToolCalls = calls
	}
	if len(sub.Context) == 0 && len(ctxs) > 0 {
		sub.Context = ctxs
	}
	out := in
	out.Response = &sub
	return out
}

// retrievalTexts reads a retrieval step output: a string, a list of strings,
// or a list of objects with a text-like field.
func retrievalTexts(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return []string{string(raw)}
	}
	switch t := v.(type) {
	case string:
		return []string{t}
	case []any:
		out := make([]string, 0, len(t))
		for _, x := range t {
			out = append(out, passageText(x))
		}
		return out
	}
	return []string{toText(v)}
}

// ---------------------------------------------------------------- pipeline

// Pipeline runs a suite's evaluators for one example, cheap → expensive:
// free evaluators sequentially, then model-based ones concurrently, then
// human ones (which see all prior scores). A failing fail_fast evaluator
// skips the model-based stage for that example.
type Pipeline struct {
	bounds []*Bound
	order  []int // indices into bounds, sorted by cost class (stable)
	// MaxParallel bounds concurrent model-based evaluators per example
	// (0 = unbounded). The runner's judge pool should still rate-limit LLMs.
	MaxParallel int
}

// NewPipeline creates a pipeline. Evaluator ids must be unique.
func NewPipeline(bounds []*Bound) (*Pipeline, error) {
	seen := map[string]bool{}
	for _, b := range bounds {
		if seen[b.ID] {
			return nil, fmt.Errorf("%w %q: give each evaluator a unique id", ErrDuplicateID, b.ID)
		}
		seen[b.ID] = true
	}
	order := make([]int, len(bounds))
	for i := range order {
		order[i] = i
	}
	slices.SortStableFunc(order, func(a, b int) int { return int(bounds[a].Cost()) - int(bounds[b].Cost()) })
	return &Pipeline{bounds: bounds, order: order}, nil
}

// Bounds returns the evaluators sorted cheap → expensive.
func (p *Pipeline) Bounds() []*Bound {
	out := make([]*Bound, len(p.order))
	for i, j := range p.order {
		out[i] = p.bounds[j]
	}
	return out
}

// Evaluate scores one example. Scores are returned in the order the
// evaluators were given to NewPipeline.
func (p *Pipeline) Evaluate(ctx context.Context, in Input) []evalcore.Score {
	out := make([]evalcore.Score, len(p.bounds))
	failFast := ""
	var model, human []int
	for _, i := range p.order {
		b := p.bounds[i]
		switch b.Cost() {
		case CostFree:
			out[i] = b.Evaluate(ctx, in)
			if b.FailFast && failFast == "" && !scorePassed(out[i]) {
				failFast = b.ID
			}
		case CostModel:
			model = append(model, i)
		default:
			human = append(human, i)
		}
	}
	if failFast != "" {
		for _, i := range model {
			out[i] = SkippedScore(p.bounds[i].ID, fmt.Sprintf("skipped because fail-fast evaluator %q failed", failFast))
		}
	} else if len(model) > 0 {
		var sem chan struct{}
		if p.MaxParallel > 0 {
			sem = make(chan struct{}, p.MaxParallel)
		}
		var wg sync.WaitGroup
		for _, i := range model {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if sem != nil {
					select {
					case sem <- struct{}{}:
						defer func() { <-sem }()
					case <-ctx.Done():
						out[i] = ErrorScore(p.bounds[i].ID, ctx.Err())
						return
					}
				}
				out[i] = p.bounds[i].Evaluate(ctx, in)
			}()
		}
		wg.Wait()
	}
	if len(human) > 0 {
		prior := make([]evalcore.Score, 0, len(out))
		for _, i := range p.order {
			if p.bounds[i].Cost() != CostHuman {
				prior = append(prior, out[i])
			}
		}
		hin := in
		hin.Prior = prior
		for _, i := range human {
			out[i] = p.bounds[i].Evaluate(ctx, hin)
		}
	}
	return out
}

func scorePassed(s evalcore.Score) bool {
	if s.Label == LabelError {
		return false
	}
	return s.Passed == nil || *s.Passed
}

// ErrDuplicateID is returned by NewPipeline for repeated evaluator ids.
var ErrDuplicateID = errors.New("duplicate evaluator id")

func passageText(x any) string {
	if m, ok := x.(map[string]any); ok {
		for _, k := range []string{"text", "content", "page_content", "chunk", "document"} {
			if s, ok := m[k].(string); ok {
				return s
			}
		}
	}
	return toText(x)
}
