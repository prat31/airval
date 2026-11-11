package aggregate

import (
	"fmt"
	"math"
	"strings"
	"sync"

	"github.com/prat31/airval/internal/evalcore"
)

// FailureBudget stops a doomed run early (plan §3.3 "Early stop"). A nil
// limit is unlimited.
type FailureBudget struct {
	MaxCriticalFails *int     `json:"max_critical_fails,omitempty"`
	MaxFails         *int     `json:"max_fails,omitempty"`
	MaxErrors        *int     `json:"max_errors,omitempty"`
	MaxFailRate      *float64 `json:"max_fail_rate,omitempty"` // checked once MinResults results are in
	MinResults       int      `json:"min_results,omitempty"`   // default 50 for MaxFailRate
}

// IsZero reports whether the budget has no limits.
func (b FailureBudget) IsZero() bool {
	return b.MaxCriticalFails == nil && b.MaxFails == nil && b.MaxErrors == nil && b.MaxFailRate == nil
}

// BudgetTracker checks a FailureBudget incrementally while results stream
// in. Safe for concurrent use.
type BudgetTracker struct {
	mu                         sync.Mutex
	b                          FailureBudget
	sev                        map[string]evalcore.Severity
	results, fails, crit, errs int
	exceeded                   string
}

// NewBudgetTracker returns a tracker for b; evals supply severities.
func NewBudgetTracker(b FailureBudget, evals []EvaluatorInfo) *BudgetTracker {
	if b.MinResults <= 0 {
		b.MinResults = 50
	}
	sev := map[string]evalcore.Severity{}
	for _, e := range evals {
		sev[e.ID] = e.Severity
	}
	return &BudgetTracker{b: b, sev: sev}
}

// Observe records one result and reports whether the budget is now
// exceeded, with a human-readable reason. Once exceeded it stays exceeded.
func (t *BudgetTracker) Observe(r Record) (bool, string) {
	o := outcome(&r, t.sev)
	t.mu.Lock()
	defer t.mu.Unlock()
	t.results++
	if o.Failed {
		t.fails++
	}
	if o.Critical {
		t.crit++
	}
	if o.Errored {
		t.errs++
	}
	if t.exceeded != "" {
		return true, t.exceeded
	}
	b := t.b
	switch {
	case b.MaxCriticalFails != nil && t.crit > *b.MaxCriticalFails:
		t.exceeded = fmt.Sprintf("%d critical failures exceed the budget of %d", t.crit, *b.MaxCriticalFails)
	case b.MaxFails != nil && t.fails > *b.MaxFails:
		t.exceeded = fmt.Sprintf("%d failed results exceed the budget of %d", t.fails, *b.MaxFails)
	case b.MaxErrors != nil && t.errs > *b.MaxErrors:
		t.exceeded = fmt.Sprintf("%d errors exceed the budget of %d", t.errs, *b.MaxErrors)
	case b.MaxFailRate != nil && t.results >= b.MinResults && float64(t.fails)/float64(t.results) > *b.MaxFailRate:
		t.exceeded = fmt.Sprintf("failure rate %.1f%% after %d results exceeds the budget of %.1f%%",
			100*float64(t.fails)/float64(t.results), t.results, 100**b.MaxFailRate)
	}
	return t.exceeded != "", t.exceeded
}

// Exceeded reports the current state.
func (t *BudgetTracker) Exceeded() (bool, string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.exceeded != "", t.exceeded
}

// BudgetFromGate derives a failure budget from a gate: clauses like
// count(critical_fail) == 0 or count(fail) <= 10 that must hold for the gate
// to pass (reachable through "and" only) become hard limits, because counts
// only grow as results stream in. Explicit budget fields win over derived
// ones when merged with Merge.
func BudgetFromGate(n *Node) FailureBudget {
	var b FailureBudget
	var walk func(*Node)
	walk = func(n *Node) {
		if n == nil {
			return
		}
		switch n.Op {
		case OpAnd:
			for _, a := range n.Args {
				walk(a)
			}
		case OpCmp:
			limit, cat, ok := countLimit(n)
			if !ok {
				return
			}
			set := func(p **int) {
				if *p == nil || **p > limit {
					v := limit
					*p = &v
				}
			}
			switch cat {
			case CountCriticalFail:
				set(&b.MaxCriticalFails)
			case CountFail:
				set(&b.MaxFails)
			case CountError:
				set(&b.MaxErrors)
			}
		}
	}
	walk(n)
	return b
}

// countLimit recognizes count(cat) OP k and k OP count(cat) upper bounds.
func countLimit(n *Node) (int, string, bool) {
	l, r := n.Args[0], n.Args[1]
	op := n.Cmp
	if l.Op == OpNum && r.Op == OpCall { // flip "k >= count(x)"
		l, r = r, l
		op = map[string]string{">=": "<=", ">": "<", "<=": ">=", "<": ">", "==": "==", "!=": "!="}[op]
	}
	if l.Op != OpCall || l.Func != "count" || len(l.Params) != 1 || r.Op != OpNum || r.Value == nil {
		return 0, "", false
	}
	k := *r.Value
	var limit float64
	switch op {
	case "==", "<=":
		limit = math.Floor(k)
	case "<":
		limit = math.Ceil(k) - 1
	default:
		return 0, "", false
	}
	if limit < 0 {
		limit = 0
	}
	return int(limit), strings.ToLower(l.Params[0]), true
}

// Merge returns b with unset limits filled from o.
func (b FailureBudget) Merge(o FailureBudget) FailureBudget {
	if b.MaxCriticalFails == nil {
		b.MaxCriticalFails = o.MaxCriticalFails
	}
	if b.MaxFails == nil {
		b.MaxFails = o.MaxFails
	}
	if b.MaxErrors == nil {
		b.MaxErrors = o.MaxErrors
	}
	if b.MaxFailRate == nil {
		b.MaxFailRate = o.MaxFailRate
	}
	if b.MinResults == 0 {
		b.MinResults = o.MinResults
	}
	return b
}
