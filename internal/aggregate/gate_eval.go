package aggregate

import (
	"errors"
	"fmt"
	"math"
	"strings"
)

// ErrNoBaseline is returned by an Env for regression_vs_baseline when the
// run has no baseline. Clauses that need it are skipped (treated as passing).
var ErrNoBaseline = errors.New("no baseline run to compare against")

// ErrNoData is returned when a metric has no observations (e.g. every score
// errored). Such comparisons fail.
var ErrNoData = errors.New("no data")

// Env supplies metric values to gate evaluation.
type Env interface {
	Value(fn string, params []string) (float64, error)
}

// SummaryEnv evaluates gates against a run Summary and an optional
// Comparison with the baseline.
type SummaryEnv struct {
	Summary    *Summary
	Comparison *Comparison // nil when there is no baseline
}

func distStat(fn string, d Dist) (float64, error) {
	if d.N == 0 {
		return 0, ErrNoData
	}
	switch fn {
	case "mean":
		return d.Mean, nil
	case "median", "p50":
		return d.P50, nil
	case "p90":
		return d.P90, nil
	case "p95":
		return d.P95, nil
	case "p99":
		return d.P99, nil
	case "min":
		return d.Min, nil
	case "max":
		return d.Max, nil
	case "stddev":
		return d.Stddev, nil
	case "sum":
		return d.Sum, nil
	}
	return 0, fmt.Errorf("gate: %s is not a statistic", fn)
}

func (e SummaryEnv) metricDist(name string) (Dist, bool) {
	s := e.Summary
	switch strings.ToLower(name) {
	case MetricLatencyMs:
		return s.LatencyMs, true
	case MetricCostUSD:
		return s.CostUSD, true
	case MetricTokens:
		return s.Tokens, true
	case MetricInputTokens:
		return s.InputTokens, true
	case MetricOutputTokens:
		return s.OutputTokens, true
	}
	return Dist{}, false
}

func (e SummaryEnv) evaluator(id string) (EvaluatorSummary, error) {
	es, ok := e.Summary.Evaluator(id)
	if !ok {
		return es, unknownEvaluator(id, e.Summary.EvaluatorIDs())
	}
	return es, nil
}

// Value implements Env.
func (e SummaryEnv) Value(fn string, params []string) (float64, error) {
	s := e.Summary
	if s == nil {
		return 0, errors.New("gate: no summary")
	}
	arg := params[0]
	switch fn {
	case "count":
		cat := strings.ToLower(arg)
		if len(params) == 2 {
			es, err := e.evaluator(params[1])
			if err != nil {
				return 0, err
			}
			switch cat {
			case CountFail:
				return float64(es.Failed), nil
			case CountCriticalFail, CountMajorFail, CountMinorFail:
				if string(es.Severity)+"_fail" == cat {
					return float64(es.Failed), nil
				}
				return 0, nil
			case CountError:
				return float64(es.Errored), nil
			case CountPending:
				return float64(es.Pending), nil
			case CountPass:
				return float64(es.Passed), nil
			case CountResults:
				return float64(es.Passed + es.Failed + es.Errored + es.Pending + es.Skipped), nil
			}
		}
		switch cat {
		case CountFail:
			return float64(s.Failed), nil
		case CountCriticalFail:
			return float64(s.CriticalFailed), nil
		case CountMajorFail:
			return float64(s.MajorFailed), nil
		case CountMinorFail:
			return float64(s.MinorFailed), nil
		case CountError:
			return float64(s.Errored), nil
		case CountPending:
			return float64(s.Pending), nil
		case CountPass:
			return float64(s.Passed), nil
		case CountResults:
			return float64(s.Results), nil
		}
		return 0, fmt.Errorf("gate: unknown count category %q", arg)
	case "pass_rate":
		if strings.EqualFold(arg, MetricAll) {
			if s.Results == 0 {
				return 0, ErrNoData
			}
			return s.PassRate, nil
		}
		es, err := e.evaluator(arg)
		if err != nil {
			return 0, err
		}
		if es.Passed+es.Failed+es.Errored == 0 {
			return 0, ErrNoData
		}
		return es.PassRate, nil
	case "regression_vs_baseline":
		if _, err := e.evaluator(arg); err != nil {
			return 0, err
		}
		if e.Comparison == nil {
			return 0, ErrNoBaseline
		}
		ec, ok := e.Comparison.Evaluator(arg)
		if !ok || ec.Paired == 0 {
			return 0, ErrNoBaseline
		}
		return ec.MeanDelta, nil
	}
	// Statistics: built-in metrics take precedence over evaluator ids.
	if d, ok := e.metricDist(arg); ok {
		return distStat(fn, d)
	}
	es, err := e.evaluator(arg)
	if err != nil {
		return 0, err
	}
	return distStat(fn, es.Values)
}

// ClauseResult is the outcome of one comparison, for display.
type ClauseResult struct {
	Text    string  `json:"text"`
	Passed  bool    `json:"passed"`
	Skipped bool    `json:"skipped,omitempty"` // e.g. no baseline
	Left    float64 `json:"left"`
	Right   float64 `json:"right"`
	Error   string  `json:"error,omitempty"`
}

// Result is the outcome of evaluating a gate.
type Result struct {
	Passed  bool           `json:"passed"`
	Text    string         `json:"text"`
	Clauses []ClauseResult `json:"clauses"`
}

// Eval evaluates the expression. Errors are returned only for invalid
// expressions or unknown evaluators; missing data fails the clause and a
// missing baseline skips it.
func Eval(n *Node, env Env) (Result, error) {
	if err := Validate(n, nil); err != nil {
		return Result{}, err
	}
	r := Result{Text: n.String()}
	ok, err := evalBool(n, env, &r)
	if err != nil {
		return Result{}, err
	}
	r.Passed = ok
	return r, nil
}

func evalBool(n *Node, env Env, r *Result) (bool, error) {
	switch n.Op {
	case OpAnd:
		all := true
		for _, a := range n.Args { // no short-circuit: report every clause
			ok, err := evalBool(a, env, r)
			if err != nil {
				return false, err
			}
			all = all && ok
		}
		return all, nil
	case OpOr:
		anyOK := false
		for _, a := range n.Args {
			ok, err := evalBool(a, env, r)
			if err != nil {
				return false, err
			}
			anyOK = anyOK || ok
		}
		return anyOK, nil
	case OpNot:
		ok, err := evalBool(n.Args[0], env, r)
		return !ok, err
	case OpCmp:
		cr := ClauseResult{Text: n.String()}
		l, lerr := evalNum(n.Args[0], env)
		rv, rerr := evalNum(n.Args[1], env)
		cr.Left, cr.Right = l, rv
		for _, err := range []error{lerr, rerr} {
			if err == nil {
				continue
			}
			var ue *UnknownEvaluatorError
			if errors.As(err, &ue) {
				return false, err
			}
			if errors.Is(err, ErrNoBaseline) {
				cr.Skipped, cr.Passed = true, true
				cr.Error = err.Error()
				r.Clauses = append(r.Clauses, cr)
				return true, nil
			}
			cr.Error = err.Error()
			r.Clauses = append(r.Clauses, cr)
			return false, nil
		}
		cr.Passed = compare(l, n.Cmp, rv)
		r.Clauses = append(r.Clauses, cr)
		return cr.Passed, nil
	}
	return false, fmt.Errorf("gate: %s is not a condition", n)
}

func evalNum(n *Node, env Env) (float64, error) {
	switch n.Op {
	case OpNum:
		return *n.Value, nil
	case OpCall:
		return env.Value(n.Func, n.Params)
	}
	return 0, fmt.Errorf("gate: %s is not a number", n)
}

const cmpEps = 1e-12

func compare(l float64, op string, r float64) bool {
	if math.IsNaN(l) || math.IsNaN(r) {
		return false
	}
	switch op {
	case ">=":
		return l >= r-cmpEps
	case ">":
		return l > r
	case "<=":
		return l <= r+cmpEps
	case "<":
		return l < r
	case "==":
		return math.Abs(l-r) <= cmpEps
	case "!=":
		return math.Abs(l-r) > cmpEps
	}
	return false
}

// EvalText parses and evaluates a gate expression against a summary.
func EvalText(expr string, s *Summary, c *Comparison) (Result, error) {
	n, err := Parse(expr)
	if err != nil {
		return Result{}, err
	}
	if err := Validate(n, s.EvaluatorIDs()); err != nil {
		return Result{}, err
	}
	return Eval(n, SummaryEnv{Summary: s, Comparison: c})
}
