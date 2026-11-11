package aggregate

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/airval/airval/internal/evalcore"
)

// Record is one result (example × trial) as seen by aggregation.
type Record struct {
	ExampleID string           `json:"example_id"`
	Trial     int              `json:"trial"`
	Tags      []string         `json:"tags,omitempty"`
	Metadata  map[string]any   `json:"metadata,omitempty"`
	Latency   time.Duration    `json:"latency"`
	CostUSD   float64          `json:"cost_usd"` // target cost (judge cost is in Scores)
	Usage     evalcore.Usage   `json:"usage"`
	Error     string           `json:"error,omitempty"` // target error: no usable response
	Scores    []evalcore.Score `json:"scores"`
}

// EvaluatorInfo is the suite-level configuration aggregation needs.
type EvaluatorInfo struct {
	ID        string            `json:"id"`
	Kind      string            `json:"kind,omitempty"`
	Severity  evalcore.Severity `json:"severity,omitempty"` // default major
	Threshold float64           `json:"threshold"`
}

// Score labels with special meaning (mirrors internal/evaluators).
const (
	labelError   = "error"
	labelPending = "pending"
	labelSkipped = "skipped"
)

// ScoreState classifies one score.
type ScoreState int

const (
	StatePassed ScoreState = iota
	StateFailed
	StateErrored
	StatePending // waiting for human review
	StateSkipped // short-circuited, sampled out, or no baseline
	StateScored  // has a value but no pass/fail verdict
)

// Classify returns the state of a score.
func Classify(s evalcore.Score) ScoreState {
	switch s.Label {
	case labelError:
		return StateErrored
	case labelPending:
		return StatePending
	case labelSkipped:
		return StateSkipped
	}
	if s.Passed != nil {
		if *s.Passed {
			return StatePassed
		}
		return StateFailed
	}
	if _, ok := s.Meta["error"]; ok {
		return StateErrored
	}
	if s.Value == nil {
		return StateSkipped
	}
	return StateScored
}

// Outcome classifies a whole result.
type Outcome struct {
	Failed   bool // a score failed (or the target errored)
	Critical bool // a critical-severity score failed
	Major    bool
	Minor    bool
	Errored  bool // the target or an evaluator errored
	Pending  bool // a human review is outstanding
}

// Passed reports whether the result counts as a pass: nothing failed or
// errored. Pending reviews do not block a pass.
func (o Outcome) Passed() bool { return !o.Failed && !o.Errored }

// outcome classifies r given evaluator severities.
func outcome(r *Record, sev map[string]evalcore.Severity) Outcome {
	var o Outcome
	if r.Error != "" {
		o.Errored, o.Failed = true, true
	}
	for i := range r.Scores {
		s := &r.Scores[i]
		switch Classify(*s) {
		case StateFailed:
			o.Failed = true
			switch sev[s.EvaluatorID] {
			case evalcore.SeverityCritical:
				o.Critical = true
			case evalcore.SeverityMinor:
				o.Minor = true
			default:
				o.Major = true
			}
		case StateErrored:
			o.Errored = true
		case StatePending:
			o.Pending = true
		}
	}
	return o
}

// Options tune an Aggregator.
type Options struct {
	// MetadataKeys lists metadata keys to slice results by (e.g. "language").
	MetadataKeys []string
	// NoTagSlices disables per-tag slices.
	NoTagSlices bool
	// MaxSliceValues caps distinct values per slice key (default 500);
	// further values are folded into "(other)".
	MaxSliceValues int
	// ExactLimit is the exact-quantile limit per stream (default 100k).
	ExactLimit int
	// NoPerExample disables per-example rollups (trials, comparisons) to
	// save memory on huge runs.
	NoPerExample bool
}

// Aggregator accumulates Records. Add is safe for concurrent use.
type Aggregator struct {
	mu    sync.Mutex
	opts  Options
	order []string
	evals map[string]*evalAcc
	sev   map[string]evalcore.Severity

	results, passed, failed, critical, major, minor, errored, pending int
	latency, cost, tokIn, tokOut, tokTotal                            *Stream
	judgeCost                                                         float64

	slices   map[sliceKey]*sliceAcc
	sliceCnt map[string]int
	examples map[string]*exampleAcc
	exOrder  []string
}

type evalAcc struct {
	info                                              EvaluatorInfo
	values                                            *Stream
	passed, failed, errored, pending, skipped, scored int
	labels                                            map[string]int
	cost                                              float64
}

type sliceKey struct{ key, value string }

type sliceAcc struct {
	results, passed int
	evals           map[string]*moments
}

type moments struct {
	n              int
	mean, m2       float64
	passed, judged int
}

func (m *moments) add(x float64) {
	m.n++
	d := x - m.mean
	m.mean += d / float64(m.n)
	m.m2 += d * (x - m.mean)
}

func (m *moments) stddev() float64 {
	if m.n < 2 {
		return 0
	}
	return math.Sqrt(m.m2 / float64(m.n-1))
}

type exampleAcc struct {
	trials, passed int
	evals          map[string]*exRollup
}

type exRollup struct {
	moments
	failed int
}

// New returns an Aggregator for the given evaluators. Scores whose
// evaluator id is not listed are still aggregated (severity major).
func New(evals []EvaluatorInfo, opts Options) *Aggregator {
	if opts.MaxSliceValues <= 0 {
		opts.MaxSliceValues = 500
	}
	a := &Aggregator{
		opts:     opts,
		evals:    map[string]*evalAcc{},
		sev:      map[string]evalcore.Severity{},
		latency:  NewStream(opts.ExactLimit),
		cost:     NewStream(opts.ExactLimit),
		tokIn:    NewStream(opts.ExactLimit),
		tokOut:   NewStream(opts.ExactLimit),
		tokTotal: NewStream(opts.ExactLimit),
		slices:   map[sliceKey]*sliceAcc{},
		sliceCnt: map[string]int{},
		examples: map[string]*exampleAcc{},
	}
	for _, e := range evals {
		a.evalAcc(e)
	}
	return a
}

func (a *Aggregator) evalAcc(e EvaluatorInfo) *evalAcc {
	if acc, ok := a.evals[e.ID]; ok {
		return acc
	}
	if e.Severity == "" {
		e.Severity = evalcore.SeverityMajor
	}
	acc := &evalAcc{info: e, values: NewStream(a.opts.ExactLimit), labels: map[string]int{}}
	a.evals[e.ID] = acc
	a.sev[e.ID] = e.Severity
	a.order = append(a.order, e.ID)
	return acc
}

// Add aggregates one result.
func (a *Aggregator) Add(r Record) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.results++
	o := outcome(&r, a.sev)
	if o.Passed() {
		a.passed++
	}
	if o.Failed {
		a.failed++
	}
	if o.Critical {
		a.critical++
	}
	if o.Major {
		a.major++
	}
	if o.Minor {
		a.minor++
	}
	if o.Errored {
		a.errored++
	}
	if o.Pending {
		a.pending++
	}
	if r.Error == "" {
		a.latency.Add(float64(r.Latency) / float64(time.Millisecond))
		a.cost.Add(r.CostUSD)
		a.tokIn.Add(float64(r.Usage.InputTokens))
		a.tokOut.Add(float64(r.Usage.OutputTokens))
		a.tokTotal.Add(float64(r.Usage.InputTokens + r.Usage.OutputTokens))
	}
	var ex *exampleAcc
	if !a.opts.NoPerExample && r.ExampleID != "" {
		ex = a.examples[r.ExampleID]
		if ex == nil {
			ex = &exampleAcc{evals: map[string]*exRollup{}}
			a.examples[r.ExampleID] = ex
			a.exOrder = append(a.exOrder, r.ExampleID)
		}
		ex.trials++
		if o.Passed() {
			ex.passed++
		}
	}
	for i := range r.Scores {
		s := &r.Scores[i]
		acc := a.evals[s.EvaluatorID]
		if acc == nil {
			acc = a.evalAcc(EvaluatorInfo{ID: s.EvaluatorID})
		}
		acc.cost += s.CostUSD
		a.judgeCost += s.CostUSD
		if s.Label != "" {
			acc.labels[s.Label]++
		}
		st := Classify(*s)
		switch st {
		case StatePassed:
			acc.passed++
		case StateFailed:
			acc.failed++
		case StateErrored:
			acc.errored++
		case StatePending:
			acc.pending++
		case StateSkipped:
			acc.skipped++
		case StateScored:
			acc.scored++
		}
		if s.Value != nil && st != StateErrored && st != StatePending && st != StateSkipped {
			acc.values.Add(*s.Value)
			if ex != nil {
				rr := ex.evals[s.EvaluatorID]
				if rr == nil {
					rr = &exRollup{}
					ex.evals[s.EvaluatorID] = rr
				}
				rr.add(*s.Value)
				switch st {
				case StatePassed:
					rr.passed++
					rr.judged++
				case StateFailed:
					rr.failed++
					rr.judged++
				}
			}
		}
	}
	a.addSlices(&r, o)
}

func (a *Aggregator) slice(key, value string) *sliceAcc {
	k := sliceKey{key, value}
	if s, ok := a.slices[k]; ok {
		return s
	}
	if a.sliceCnt[key] >= a.opts.MaxSliceValues {
		k.value = "(other)"
		if s, ok := a.slices[k]; ok {
			return s
		}
	} else {
		a.sliceCnt[key]++
	}
	s := &sliceAcc{evals: map[string]*moments{}}
	a.slices[k] = s
	return s
}

func (a *Aggregator) addSlices(r *Record, o Outcome) {
	var targets []*sliceAcc
	if !a.opts.NoTagSlices {
		for _, t := range r.Tags {
			targets = append(targets, a.slice("tag", t))
		}
	}
	for _, k := range a.opts.MetadataKeys {
		v, ok := r.Metadata[k]
		if !ok || v == nil {
			continue
		}
		targets = append(targets, a.slice("metadata."+k, fmt.Sprint(v)))
	}
	for _, sl := range targets {
		sl.results++
		if o.Passed() {
			sl.passed++
		}
		for i := range r.Scores {
			s := &r.Scores[i]
			st := Classify(*s)
			if s.Value == nil || st == StateErrored || st == StatePending || st == StateSkipped {
				continue
			}
			m := sl.evals[s.EvaluatorID]
			if m == nil {
				m = &moments{}
				sl.evals[s.EvaluatorID] = m
			}
			m.add(*s.Value)
			if st == StatePassed || st == StateFailed {
				m.judged++
				if st == StatePassed {
					m.passed++
				}
			}
		}
	}
}

// ---------------------------------------------------------------- summary

// Summary is the aggregate view of a run (stored as run_summary).
type Summary struct {
	Results        int     `json:"results"`  // example × trial results
	Examples       int     `json:"examples"` // distinct examples (0 when per-example tracking is off)
	Passed         int     `json:"passed"`
	PassRate       float64 `json:"pass_rate"`
	Failed         int     `json:"failed"`
	CriticalFailed int     `json:"critical_failed"`
	MajorFailed    int     `json:"major_failed"`
	MinorFailed    int     `json:"minor_failed"`
	Errored        int     `json:"errored"`
	Pending        int     `json:"pending"`

	Evaluators []EvaluatorSummary `json:"evaluators"`

	LatencyMs    Dist    `json:"latency_ms"`
	CostUSD      Dist    `json:"cost_usd"` // target cost per result
	InputTokens  Dist    `json:"input_tokens"`
	OutputTokens Dist    `json:"output_tokens"`
	Tokens       Dist    `json:"tokens"`
	JudgeCostUSD float64 `json:"judge_cost_usd"`
	TotalCostUSD float64 `json:"total_cost_usd"`

	Slices []SliceSummary `json:"slices,omitempty"`
	Trials *TrialsSummary `json:"trials,omitempty"`
}

// EvaluatorSummary aggregates one evaluator's scores.
type EvaluatorSummary struct {
	ID        string            `json:"id"`
	Kind      string            `json:"kind,omitempty"`
	Severity  evalcore.Severity `json:"severity"`
	Threshold float64           `json:"threshold"`
	Values    Dist              `json:"values"` // over scored (non-error, non-pending) values
	Passed    int               `json:"passed"`
	Failed    int               `json:"failed"`
	Errored   int               `json:"errored"`
	Pending   int               `json:"pending"`
	Skipped   int               `json:"skipped"`
	// PassRate = passed / (passed + failed + errored); errors count against.
	PassRate float64        `json:"pass_rate"`
	Labels   map[string]int `json:"labels,omitempty"`
	CostUSD  float64        `json:"cost_usd"`
}

// SliceSummary aggregates results sharing a tag or metadata value.
type SliceSummary struct {
	Key        string                  `json:"key"` // "tag" or "metadata.<key>"
	Value      string                  `json:"value"`
	Results    int                     `json:"results"`
	Passed     int                     `json:"passed"`
	PassRate   float64                 `json:"pass_rate"`
	Evaluators map[string]SliceMetrics `json:"evaluators"`
}

// SliceMetrics are an evaluator's stats within a slice.
type SliceMetrics struct {
	N        int     `json:"n"`
	Mean     float64 `json:"mean"`
	Stddev   float64 `json:"stddev"`
	PassRate float64 `json:"pass_rate"`
}

func ratio(a, b int) float64 {
	if b == 0 {
		return 0
	}
	return float64(a) / float64(b)
}

// Summary computes the current aggregate. It can be called repeatedly while
// results stream in.
func (a *Aggregator) Summary() *Summary {
	a.mu.Lock()
	defer a.mu.Unlock()
	s := &Summary{
		Results: a.results, Examples: len(a.examples), Passed: a.passed,
		PassRate: ratio(a.passed, a.results), Failed: a.failed,
		CriticalFailed: a.critical, MajorFailed: a.major, MinorFailed: a.minor,
		Errored: a.errored, Pending: a.pending,
		LatencyMs:    a.latency.Dist(HistAuto),
		CostUSD:      a.cost.Dist(HistAuto),
		InputTokens:  a.tokIn.Dist(HistNone),
		OutputTokens: a.tokOut.Dist(HistNone),
		Tokens:       a.tokTotal.Dist(HistAuto),
		JudgeCostUSD: a.judgeCost,
	}
	s.TotalCostUSD = a.cost.Sum() + a.judgeCost
	for _, id := range a.order {
		e := a.evals[id]
		s.Evaluators = append(s.Evaluators, EvaluatorSummary{
			ID: id, Kind: e.info.Kind, Severity: e.info.Severity, Threshold: e.info.Threshold,
			Values: e.values.Dist(HistUnit),
			Passed: e.passed, Failed: e.failed, Errored: e.errored, Pending: e.pending, Skipped: e.skipped,
			PassRate: ratio(e.passed, e.passed+e.failed+e.errored),
			Labels:   cloneCounts(e.labels),
			CostUSD:  e.cost,
		})
	}
	for k, sl := range a.slices {
		ss := SliceSummary{Key: k.key, Value: k.value, Results: sl.results, Passed: sl.passed,
			PassRate: ratio(sl.passed, sl.results), Evaluators: map[string]SliceMetrics{}}
		for id, m := range sl.evals {
			ss.Evaluators[id] = SliceMetrics{N: m.n, Mean: m.mean, Stddev: m.stddev(), PassRate: ratio(m.passed, m.judged)}
		}
		s.Slices = append(s.Slices, ss)
	}
	sort.Slice(s.Slices, func(i, j int) bool {
		x, y := s.Slices[i], s.Slices[j]
		if x.Key != y.Key {
			return x.Key < y.Key
		}
		if x.Results != y.Results {
			return x.Results > y.Results
		}
		return x.Value < y.Value
	})
	s.Trials = a.trialsLocked()
	return s
}

func cloneCounts(m map[string]int) map[string]int {
	if len(m) == 0 {
		return nil
	}
	out := make(map[string]int, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// Evaluator returns the summary of one evaluator, or false.
func (s *Summary) Evaluator(id string) (EvaluatorSummary, bool) {
	for _, e := range s.Evaluators {
		if e.ID == id {
			return e, true
		}
	}
	return EvaluatorSummary{}, false
}

// EvaluatorIDs lists the evaluators in the summary.
func (s *Summary) EvaluatorIDs() []string {
	out := make([]string, len(s.Evaluators))
	for i, e := range s.Evaluators {
		out[i] = e.ID
	}
	return out
}

// ---------------------------------------------------------------- trials

// TrialsSummary describes repeated trials (nondeterminism).
type TrialsSummary struct {
	MaxTrials int `json:"max_trials"`
	// PassAtK[k-1] is the unbiased pass@k (Chen et al. 2021): the chance
	// that at least one of k sampled trials passes, averaged over examples
	// with at least k trials.
	PassAtK []float64 `json:"pass_at_k"`
	// PassHatK[k-1] is pass^k: the chance that all k sampled trials pass
	// (consistency), averaged likewise.
	PassHatK []float64 `json:"pass_hat_k"`
	// Flaky counts examples whose trials both passed and failed.
	Flaky int `json:"flaky"`
	// MeanVariance is the average within-example variance per evaluator.
	MeanVariance map[string]float64 `json:"mean_variance"`
	// MostVariable lists up to 20 examples with the largest within-example
	// standard deviation of any evaluator.
	MostVariable []ExampleTrials `json:"most_variable,omitempty"`
}

// ExampleTrials are one example's trial statistics.
type ExampleTrials struct {
	ExampleID string             `json:"example_id"`
	Trials    int                `json:"trials"`
	Passed    int                `json:"passed"`
	Mean      map[string]float64 `json:"mean"`
	Stddev    map[string]float64 `json:"stddev"`
}

// PassAtK is the unbiased estimator 1 - C(n-c, k)/C(n, k) for n trials
// with c passes.
func PassAtK(n, c, k int) float64 {
	if k <= 0 || n <= 0 || k > n {
		return 0
	}
	if n-c < k {
		return 1
	}
	// Π_{i=n-c+1}^{n} (1 - k/i), numerically stable.
	p := 1.0
	for i := n - c + 1; i <= n; i++ {
		p *= 1 - float64(k)/float64(i)
	}
	return 1 - p
}

// PassHatK is C(c, k)/C(n, k): the chance that k trials drawn without
// replacement all pass.
func PassHatK(n, c, k int) float64 {
	if k <= 0 || n <= 0 || k > n || c < k {
		return 0
	}
	p := 1.0
	for i := 0; i < k; i++ {
		p *= float64(c-i) / float64(n-i)
	}
	return p
}

func (a *Aggregator) trialsLocked() *TrialsSummary {
	maxT := 0
	for _, ex := range a.examples {
		maxT = max(maxT, ex.trials)
	}
	if maxT < 2 {
		return nil
	}
	t := &TrialsSummary{MaxTrials: maxT, PassAtK: make([]float64, maxT), PassHatK: make([]float64, maxT), MeanVariance: map[string]float64{}}
	counts := make([]int, maxT)
	varSum := map[string]float64{}
	varN := map[string]int{}
	type scored struct {
		id string
		sd float64
	}
	var variable []scored
	for _, id := range a.exOrder {
		ex := a.examples[id]
		for k := 1; k <= ex.trials; k++ {
			t.PassAtK[k-1] += PassAtK(ex.trials, ex.passed, k)
			t.PassHatK[k-1] += PassHatK(ex.trials, ex.passed, k)
			counts[k-1]++
		}
		if ex.passed > 0 && ex.passed < ex.trials {
			t.Flaky++
		}
		top := 0.0
		for eid, r := range ex.evals {
			if r.n < 2 {
				continue
			}
			v := r.m2 / float64(r.n-1)
			varSum[eid] += v
			varN[eid]++
			top = math.Max(top, math.Sqrt(v))
		}
		if top > 0 {
			variable = append(variable, scored{id, top})
		}
	}
	for k := range counts {
		if counts[k] > 0 {
			t.PassAtK[k] /= float64(counts[k])
			t.PassHatK[k] /= float64(counts[k])
		}
	}
	for eid, s := range varSum {
		t.MeanVariance[eid] = s / float64(varN[eid])
	}
	sort.SliceStable(variable, func(i, j int) bool { return variable[i].sd > variable[j].sd })
	for i := 0; i < len(variable) && i < 20; i++ {
		t.MostVariable = append(t.MostVariable, a.exampleTrialsLocked(variable[i].id))
	}
	return t
}

func (a *Aggregator) exampleTrialsLocked(id string) ExampleTrials {
	ex := a.examples[id]
	et := ExampleTrials{ExampleID: id, Trials: ex.trials, Passed: ex.passed, Mean: map[string]float64{}, Stddev: map[string]float64{}}
	for eid, r := range ex.evals {
		et.Mean[eid] = r.mean
		et.Stddev[eid] = r.stddev()
	}
	return et
}

// ExampleTrials returns per-example trial statistics in first-seen order.
func (a *Aggregator) ExampleTrials() []ExampleTrials {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]ExampleTrials, 0, len(a.exOrder))
	for _, id := range a.exOrder {
		out = append(out, a.exampleTrialsLocked(id))
	}
	return out
}

// ---------------------------------------------------------------- rollups

// ExampleRollup is one example's aggregate for one evaluator (mean over
// trials), the unit of run comparison.
type ExampleRollup struct {
	Mean   float64 `json:"mean"`
	N      int     `json:"n"`
	Passed int     `json:"passed"`
	Failed int     `json:"failed"`
}

// Rollup maps evaluator id → example id → rollup.
type Rollup map[string]map[string]ExampleRollup

// Rollup returns per-example rollups for comparisons (empty when
// Options.NoPerExample is set).
func (a *Aggregator) Rollup() Rollup {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := Rollup{}
	for exID, ex := range a.examples {
		for eid, r := range ex.evals {
			m := out[eid]
			if m == nil {
				m = map[string]ExampleRollup{}
				out[eid] = m
			}
			m[exID] = ExampleRollup{Mean: r.mean, N: r.n, Passed: r.passed, Failed: r.failed}
		}
	}
	return out
}

// Builtin metric names usable in gates in place of an evaluator id.
const (
	MetricLatencyMs    = "latency_ms"
	MetricCostUSD      = "cost_usd"
	MetricTokens       = "tokens"
	MetricInputTokens  = "input_tokens"
	MetricOutputTokens = "output_tokens"
	MetricAll          = "all"
)

func isBuiltinMetric(s string) bool {
	switch strings.ToLower(s) {
	case MetricLatencyMs, MetricCostUSD, MetricTokens, MetricInputTokens, MetricOutputTokens:
		return true
	}
	return false
}
