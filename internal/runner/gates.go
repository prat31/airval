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

// Gates are stored as the aggregate expression AST with evaluators referenced by id.

// GateNode decodes a stored gate expression.
func GateNode(raw json.RawMessage) (*aggregate.Node, error) {
	var n aggregate.Node
	if err := json.Unmarshal(raw, &n); err != nil {
		return nil, err
	}
	if n.Op == "" {
		return nil, errors.New("gate: empty expression")
	}
	return &n, nil
}

// MetricTTFT is accepted in gates in addition to the aggregate built-ins.
const MetricTTFT = "ttft_ms"

// gateEnv supplies metric values (adds ttft_ms to the summary environment).
type gateEnv struct {
	base aggregate.SummaryEnv
	ttft aggregate.Dist
}

func (e gateEnv) Value(fn string, params []string) (float64, error) {
	if len(params) == 1 && strings.EqualFold(params[0], MetricTTFT) {
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

// renameParams returns a copy of n with evaluator ids replaced by display names.
func renameParams(n *aggregate.Node, names map[string]string) *aggregate.Node {
	if n == nil {
		return nil
	}
	c := *n
	if len(n.Params) > 0 {
		c.Params = make([]string, len(n.Params))
		for i, p := range n.Params {
			if name, ok := names[p]; ok {
				p = strings.ReplaceAll(strings.ToLower(name), " ", "_")
			}
			c.Params[i] = p
		}
	}
	if len(n.Args) > 0 {
		c.Args = make([]*aggregate.Node, len(n.Args))
		for i, a := range n.Args {
			c.Args[i] = renameParams(a, names)
		}
	}
	return &c
}

// evalGate evaluates one stored gate against env.
func evalGate(id, name string, raw json.RawMessage, env aggregate.Env, names map[string]string) notify.GateResult {
	res := notify.GateResult{GateID: id, Name: name}
	n, err := GateNode(raw)
	if err != nil {
		res.Detail = "invalid gate expression: " + err.Error()
		return res
	}
	disp := renameParams(n, names)
	res.Text = disp.String()
	r, err := aggregate.Eval(n, env)
	if err != nil {
		res.Detail = err.Error()
		var ue *aggregate.UnknownEvaluatorError
		if errors.As(err, &ue) {
			res.Detail = fmt.Sprintf("the gate references an evaluator that is not part of this run (%s)", ue.ID)
		}
		return res
	}
	res.Passed = r.Passed
	if len(r.Clauses) > 0 {
		v := r.Clauses[0].Left
		res.Value = &v
	}
	var details []string
	for _, c := range r.Clauses {
		text := c.Text
		for id, nm := range names {
			text = strings.ReplaceAll(text, id, strings.ReplaceAll(strings.ToLower(nm), " ", "_"))
		}
		switch {
		case c.Skipped:
			details = append(details, text+": skipped ("+c.Error+")")
		case c.Error != "":
			details = append(details, text+": "+c.Error)
		case !c.Passed:
			details = append(details, fmt.Sprintf("%s: got %s", text, strconv.FormatFloat(c.Left, 'g', 4, 64)))
		}
	}
	res.Detail = strings.Join(details, "; ")
	return res
}
