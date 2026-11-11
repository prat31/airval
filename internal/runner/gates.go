package runner

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/airval/airval/internal/aggregate"
	"github.com/airval/airval/internal/notify"
)

// apiGate mirrors the API's GateExpr schema (stored in gates.expr).
type apiGate struct {
	Op          string    `json:"op"`
	Args        []apiGate `json:"args,omitempty"`
	Fn          string    `json:"fn,omitempty"`
	Metric      string    `json:"metric,omitempty"`
	EvaluatorID string    `json:"evaluator_id,omitempty"`
	Selector    string    `json:"selector,omitempty"`
	Tag         string    `json:"tag,omitempty"`
	Comparator  string    `json:"comparator,omitempty"`
	Value       *float64  `json:"value,omitempty"`
}

var selectorCategory = map[string]string{
	"": aggregate.CountFail, "all": aggregate.CountResults, "passed": aggregate.CountPass, "failed": aggregate.CountFail,
	"errored": aggregate.CountError, "critical_fail": aggregate.CountCriticalFail,
	"major_fail": aggregate.CountMajorFail, "minor_fail": aggregate.CountMinorFail,
}

// clause converts one compare node into an aggregate comparison.
func (g apiGate) clause() (*aggregate.Node, error) {
	if g.Value == nil {
		return nil, errors.New("gate: comparison without a value")
	}
	var call *aggregate.Node
	metric := g.Metric
	if metric == "" {
		metric = "evaluator"
	}
	switch metric {
	case "evaluator":
		switch g.Fn {
		case "count":
			cat, ok := selectorCategory[g.Selector]
			if !ok {
				return nil, fmt.Errorf("gate: unknown selector %q", g.Selector)
			}
			if g.EvaluatorID != "" {
				call = aggregate.Call("count", cat, g.EvaluatorID)
			} else {
				call = aggregate.Call("count", cat)
			}
		default:
			if g.EvaluatorID == "" {
				return nil, errors.New("gate: evaluator_id is required")
			}
			call = aggregate.Call(g.Fn, g.EvaluatorID)
		}
	case "results":
		switch g.Fn {
		case "count":
			sel := g.Selector
			if sel == "" {
				sel = "all"
			}
			cat, ok := selectorCategory[sel]
			if !ok {
				return nil, fmt.Errorf("gate: unknown selector %q", g.Selector)
			}
			call = aggregate.Call("count", cat)
		case "pass_rate", "mean":
			call = aggregate.Call("pass_rate", aggregate.MetricAll)
		default:
			return nil, fmt.Errorf("gate: %s is not available for results (use count or pass_rate)", g.Fn)
		}
	case "latency_ms", "ttft_ms", "cost_usd", "tokens":
		if g.Fn == "count" || g.Fn == "pass_rate" || g.Fn == "regression_vs_baseline" {
			return nil, fmt.Errorf("gate: %s is not available for %s", g.Fn, metric)
		}
		call = aggregate.Call(g.Fn, metric)
	default:
		return nil, fmt.Errorf("gate: unknown metric %q", metric)
	}
	return aggregate.Cmp(call, g.Comparator, aggregate.Num(*g.Value)), nil
}

// GateNode converts a stored gate expression into an aggregate AST (tag-scoped clauses are
// omitted; used to derive failure budgets).
func GateNode(raw json.RawMessage) (*aggregate.Node, error) {
	var g apiGate
	if err := json.Unmarshal(raw, &g); err != nil {
		return nil, err
	}
	n, err := g.node()
	if err == nil && n == nil {
		return nil, errors.New("gate: empty")
	}
	return n, err
}

func (g apiGate) node() (*aggregate.Node, error) {
	switch g.Op {
	case "and", "or":
		var args []*aggregate.Node
		for _, a := range g.Args {
			n, err := a.node()
			if err != nil {
				return nil, err
			}
			if n != nil {
				args = append(args, n)
			}
		}
		switch {
		case len(args) == 0:
			return nil, nil
		case len(args) == 1:
			return args[0], nil
		case g.Op == "and":
			return aggregate.And(args...), nil
		}
		return aggregate.Or(args...), nil
	case "not":
		if len(g.Args) != 1 {
			return nil, errors.New("gate: not takes one argument")
		}
		n, err := g.Args[0].node()
		if err != nil || n == nil {
			return nil, err
		}
		return aggregate.Not(n), nil
	case "compare":
		if g.Tag != "" {
			return nil, nil
		}
		return g.clause()
	}
	return nil, fmt.Errorf("gate: unknown op %q", g.Op)
}

// text renders the expression for humans (same rendering as the API).
func (g apiGate) text() string {
	switch g.Op {
	case "and", "or":
		parts := make([]string, len(g.Args))
		for i, a := range g.Args {
			parts[i] = a.text()
			if a.Op == "and" || a.Op == "or" {
				parts[i] = "(" + parts[i] + ")"
			}
		}
		return strings.Join(parts, " "+g.Op+" ")
	case "not":
		if len(g.Args) == 1 {
			return "not (" + g.Args[0].text() + ")"
		}
	case "compare":
		var arg string
		switch {
		case g.Metric == "evaluator" || g.Metric == "":
			arg = g.EvaluatorID
			if g.Selector != "" {
				arg = g.Selector
				if g.EvaluatorID != "" {
					arg += ", " + g.EvaluatorID
				}
			}
		case g.Metric == "results":
			arg = g.Selector
			if arg == "" {
				arg = "all"
			}
		default:
			arg = g.Metric
		}
		if g.Tag != "" {
			arg += ", tag=" + g.Tag
		}
		v := ""
		if g.Value != nil {
			v = strconv.FormatFloat(*g.Value, 'g', -1, 64)
		}
		return fmt.Sprintf("%s(%s) %s %s", g.Fn, arg, g.Comparator, v)
	}
	return "?"
}

// gateEnv supplies metric values for one slice of results (all results or one tag).
type gateEnv struct {
	base aggregate.SummaryEnv
	ttft aggregate.Dist
}

func (e gateEnv) Value(fn string, params []string) (float64, error) {
	if len(params) == 1 && params[0] == "ttft_ms" {
		return distStat(fn, e.ttft)
	}
	return e.base.Value(fn, params)
}

func distStat(fn string, d aggregate.Dist) (float64, error) {
	if d.N == 0 {
		return 0, aggregate.ErrNoData
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

// gateEval evaluates gate trees; envFor returns the environment for a tag ("" = all results).
type gateEval struct {
	envFor  func(tag string) gateEnv
	clauses []aggregate.ClauseResult
	names   map[string]string // evaluator id → name, for readable details
}

func (ge *gateEval) eval(g apiGate) (bool, error) {
	switch g.Op {
	case "and":
		all := true
		for _, a := range g.Args {
			ok, err := ge.eval(a)
			if err != nil {
				return false, err
			}
			all = all && ok
		}
		return all, nil
	case "or":
		anyOK := len(g.Args) == 0
		for _, a := range g.Args {
			ok, err := ge.eval(a)
			if err != nil {
				return false, err
			}
			anyOK = anyOK || ok
		}
		return anyOK, nil
	case "not":
		if len(g.Args) != 1 {
			return false, errors.New("gate: not takes one argument")
		}
		ok, err := ge.eval(g.Args[0])
		return !ok, err
	case "compare":
		n, err := g.clause()
		if err != nil {
			return false, err
		}
		res, err := aggregate.Eval(n, ge.envFor(g.Tag))
		if err != nil {
			var ue *aggregate.UnknownEvaluatorError
			if errors.As(err, &ue) {
				cr := aggregate.ClauseResult{Text: ge.readable(g.text()), Error: err.Error()}
				ge.clauses = append(ge.clauses, cr)
				return false, nil
			}
			return false, err
		}
		for _, c := range res.Clauses {
			c.Text = ge.readable(g.text())
			ge.clauses = append(ge.clauses, c)
		}
		return res.Passed, nil
	}
	return false, fmt.Errorf("gate: unknown op %q", g.Op)
}

func (ge *gateEval) readable(s string) string {
	for id, name := range ge.names {
		s = strings.ReplaceAll(s, id, name)
	}
	return s
}

// evalGate evaluates one stored gate.
func evalGate(id, name string, raw json.RawMessage, envFor func(string) gateEnv, names map[string]string) notify.GateResult {
	var g apiGate
	res := notify.GateResult{GateID: id, Name: name}
	if err := json.Unmarshal(raw, &g); err != nil {
		res.Detail = "invalid gate expression: " + err.Error()
		return res
	}
	ge := &gateEval{envFor: envFor, names: names}
	res.Text = ge.readable(g.text())
	ok, err := ge.eval(g)
	if err != nil {
		res.Detail = err.Error()
		return res
	}
	res.Passed = ok
	if len(ge.clauses) > 0 {
		v := ge.clauses[0].Left
		res.Value = &v
	}
	var details []string
	for _, c := range ge.clauses {
		switch {
		case c.Skipped:
			details = append(details, c.Text+": skipped ("+c.Error+")")
		case c.Error != "":
			details = append(details, c.Text+": "+c.Error)
		case !c.Passed:
			details = append(details, fmt.Sprintf("%s: got %s", c.Text, strconv.FormatFloat(c.Left, 'g', 4, 64)))
		}
	}
	res.Detail = strings.Join(details, "; ")
	return res
}
