package aggregate

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/prat31/airval/internal/evalcore"
)

func TestParsePrintRoundTrip(t *testing.T) {
	cases := map[string]string{
		"mean(faithfulness) >= 0.85 and count(critical_fail) == 0": "mean(faithfulness) >= 0.85 and count(critical_fail) == 0",
		"pass_rate(all)>=95%":                                "pass_rate(all) >= 0.95",
		"p95(latency_ms) < 2000":                             "p95(latency_ms) < 2000",
		"regression_vs_baseline(correctness) > -0.02":        "regression_vs_baseline(correctness) > -0.02",
		"MEAN(a) > 0.5 AND (mean(b) > 0.5 OR mean(c) > 0.5)": "mean(a) > 0.5 and (mean(b) > 0.5 or mean(c) > 0.5)",
		"mean(a) > 0.5 && mean(b) > 0.5 || !(mean(c) = 1)":   "mean(a) > 0.5 and mean(b) > 0.5 or not mean(c) == 1",
		"not (mean(a) > 1 or mean(b) > 1)":                   "not (mean(a) > 1 or mean(b) > 1)",
		"(mean(a) > 1 and mean(b) > 1) and mean(c) > 1":      "(mean(a) > 1 and mean(b) > 1) and mean(c) > 1",
		"count(fail, faithfulness) <= 3":                     "count(fail, faithfulness) <= 3",
		"0.9 <= median(rag-eval.v2)":                         "0.9 <= median(rag-eval.v2)",
		"sum(cost_usd) < 1_000":                              "sum(cost_usd) < 1000",
		"min(x) != 1e-3":                                     "min(x) != 0.001",
	}
	for in, want := range cases {
		n, err := Parse(in)
		if err != nil {
			t.Errorf("Parse(%q): %v", in, err)
			continue
		}
		got := n.String()
		if got != want {
			t.Errorf("print(%q) = %q, want %q", in, got, want)
		}
		n2, err := Parse(got)
		if err != nil || !reflect.DeepEqual(n, n2) {
			t.Errorf("round trip of %q changed the AST: %v", got, err)
		}
		// JSON round trip.
		b, _ := json.Marshal(n)
		var n3 Node
		if err := json.Unmarshal(b, &n3); err != nil || n3.String() != got {
			t.Errorf("JSON round trip of %q: %s → %q (%v)", got, b, n3.String(), err)
		}
	}
	// Built AST prints and zero numbers survive JSON.
	n := And(Cmp(Call("count", "error"), "==", Num(0)), Cmp(Call("mean", "x"), ">", Num(0.5)))
	b, _ := json.Marshal(n)
	if !strings.Contains(string(b), `"value":0`) {
		t.Fatalf("zero value lost in JSON: %s", b)
	}
	if err := Validate(n, nil); err != nil || n.String() != "count(error) == 0 and mean(x) > 0.5" {
		t.Fatalf("%v %s", err, n)
	}
}

func TestParseErrors(t *testing.T) {
	cases := map[string]string{
		"":                          "empty",
		"meen(a) > 1":               `did you mean "mean"`,
		"mean(a)":                   "expected a comparison",
		"mean(a) > 1 mean(b) > 1":   "join conditions",
		"mean(a > 1":                "expected ',' or ')'",
		"(mean(a) > 1":              "missing ')'",
		"faithfulness >= 0.8":       "must be called as a function",
		"count(bad) == 0":           "unknown count category",
		"mean(a, b) > 1":            "exactly one argument",
		"mean(a) > 1 & mean(b) > 1": "use &&",
		"mean(a) > 1 and":           "unexpected end",
		"mean(a) ≥ 1":               "unexpected character",
		"mean(1) > 0":               "expected an evaluator or metric name",
		"mean(a) > 1.2.3":           "invalid number",
	}
	for in, want := range cases {
		_, err := Parse(in)
		var pe *ParseError
		if err == nil || !errors.As(err, &pe) || !strings.Contains(err.Error(), want) {
			t.Errorf("Parse(%q) error = %v, want containing %q", in, err, want)
		}
	}
}

func TestValidate(t *testing.T) {
	n := MustParse("mean(faithfulnes) > 0.8 and p95(latency_ms) < 2000 and pass_rate(all) > 0.9")
	err := Validate(n, []string{"faithfulness", "correctness"})
	var ue *UnknownEvaluatorError
	if !errors.As(err, &ue) || ue.Suggestion != "faithfulness" || !strings.Contains(err.Error(), `did you mean "faithfulness"`) {
		t.Fatalf("got %v", err)
	}
	err = Validate(MustParse("mean(zzz) > 1"), []string{"faithfulness"})
	if err == nil || !strings.Contains(err.Error(), "suite evaluators: faithfulness") {
		t.Fatalf("got %v", err)
	}
	if err := Validate(MustParse("p95(latency_ms) < 2000 and pass_rate(all) > 0"), []string{}); err != nil {
		t.Fatalf("built-ins must not need evaluators: %v", err)
	}
	if refs := References(MustParse("mean(a) > 1 or count(fail, b) == 0 or p99(cost_usd) < 1")); !reflect.DeepEqual(refs, []string{"a", "b"}) {
		t.Fatalf("refs %v", refs)
	}
	bad := []*Node{
		nil,
		Num(1),
		Call("mean", "x"),
		And(Cmp(Call("mean", "x"), ">", Num(1))),
		Cmp(Call("mean", "x"), "~", Num(1)),
		Cmp(Cmp(Call("mean", "x"), ">", Num(1)), ">", Num(1)),
		{Op: "xor"},
		Cmp(Call("nope", "x"), ">", Num(1)),
	}
	for _, b := range bad {
		if err := Validate(b, nil); err == nil {
			t.Errorf("Validate(%v): want error", b)
		}
	}
}

func gateSummary() *Summary {
	a := New([]EvaluatorInfo{
		{ID: "faithfulness", Severity: evalcore.SeverityMajor, Threshold: 0.75},
		{ID: "json", Severity: evalcore.SeverityCritical, Threshold: 1},
	}, Options{})
	for i, v := range []float64{0.9, 0.8, 1, 0.6} {
		jsonOK := i != 3
		jv := 1.0
		if !jsonOK {
			jv = 0
		}
		a.Add(Record{ExampleID: string(rune('a' + i)), Latency: time.Duration(100*(i+1)) * time.Millisecond, CostUSD: 0.01,
			Scores: []evalcore.Score{sc("faithfulness", v, v >= 0.75), sc("json", jv, jsonOK)}})
	}
	return a.Summary()
}

func TestEvalGate(t *testing.T) {
	s := gateSummary()
	cases := []struct {
		expr string
		pass bool
	}{
		{"mean(faithfulness) >= 0.825", true},
		{"mean(faithfulness) > 0.825", false},
		{"count(critical_fail) == 0", false},
		{"count(critical_fail) <= 1", true},
		{"count(fail) == 1", true}, // one result (d) failed both
		{"count(fail, faithfulness) == 1 and count(critical_fail, json) == 1 and count(critical_fail, faithfulness) == 0", true},
		{"pass_rate(all) >= 0.75", true},
		{"pass_rate(json) == 0.75", true},
		{"p95(latency_ms) < 380", false}, // type-7 p95 of 100..400 = 385
		{"max(latency_ms) == 400 and min(latency_ms) == 100 and median(latency_ms) == 250", true},
		{"sum(cost_usd) <= 0.04", true},
		{"p50(faithfulness) == 0.85", true},
		{"stddev(faithfulness) > 0", true},
		{"count(results) == 4 and count(error) == 0 and count(pass) == 3", true},
		{"not mean(faithfulness) > 0.9", true},
		{"mean(faithfulness) > 0.99 or pass_rate(all) > 0.5", true},
	}
	for _, c := range cases {
		r, err := EvalText(c.expr, s, nil)
		if err != nil {
			t.Errorf("%s: %v", c.expr, err)
			continue
		}
		if r.Passed != c.pass {
			t.Errorf("%s: passed = %v, want %v (clauses %+v)", c.expr, r.Passed, c.pass, r.Clauses)
		}
	}
	r, _ := EvalText("mean(faithfulness) >= 0.8 and count(critical_fail) == 0", s, nil)
	if len(r.Clauses) != 2 || !r.Clauses[0].Passed || r.Clauses[1].Passed || r.Clauses[1].Left != 1 {
		t.Fatalf("clauses: %+v", r.Clauses)
	}
	if _, err := EvalText("mean(faithfulnes) > 0", s, nil); err == nil || !strings.Contains(err.Error(), "did you mean") {
		t.Fatalf("unknown evaluator: %v", err)
	}
	// Eval on an unvalidated env also reports unknown evaluators.
	if _, err := Eval(MustParse("mean(nope) > 0"), SummaryEnv{Summary: s}); err == nil {
		t.Fatal("want unknown evaluator error")
	}
}

func TestEvalBaselineAndNoData(t *testing.T) {
	s := gateSummary()
	// No baseline → regression clause skipped (passes).
	r, err := EvalText("regression_vs_baseline(faithfulness) > -0.02", s, nil)
	if err != nil || !r.Passed || !r.Clauses[0].Skipped {
		t.Fatalf("no baseline: %+v %v", r, err)
	}
	cmp := &Comparison{Evaluators: []EvaluatorComparison{{ID: "faithfulness", Paired: 4, MeanDelta: -0.05}}}
	r, _ = EvalText("regression_vs_baseline(faithfulness) > -0.02", s, cmp)
	if r.Passed || r.Clauses[0].Left != -0.05 {
		t.Fatalf("regression: %+v", r)
	}
	// No data → clause fails with an error message, not a Go error.
	empty := New([]EvaluatorInfo{{ID: "x"}}, Options{}).Summary()
	r, err = EvalText("mean(x) >= 0", empty, nil)
	if err != nil || r.Passed || r.Clauses[0].Error == "" {
		t.Fatalf("no data: %+v %v", r, err)
	}
}

func TestFailureBudget(t *testing.T) {
	evs := []EvaluatorInfo{{ID: "json", Severity: evalcore.SeverityCritical}, {ID: "q", Severity: evalcore.SeverityMinor}}
	two := 2
	tr := NewBudgetTracker(FailureBudget{MaxCriticalFails: &two}, evs)
	fail := Record{Scores: []evalcore.Score{sc("json", 0, false)}}
	minor := Record{Scores: []evalcore.Score{sc("q", 0, false)}}
	for i := 0; i < 2; i++ {
		if ex, _ := tr.Observe(fail); ex {
			t.Fatal("within budget")
		}
	}
	if ex, _ := tr.Observe(minor); ex {
		t.Fatal("minor failures don't count against critical budget")
	}
	ex, why := tr.Observe(fail)
	if !ex || !strings.Contains(why, "3 critical failures exceed the budget of 2") {
		t.Fatalf("exceeded = %v %q", ex, why)
	}
	if ex, _ := tr.Exceeded(); !ex {
		t.Fatal("sticky")
	}

	rate := 0.5
	tr = NewBudgetTracker(FailureBudget{MaxFailRate: &rate, MinResults: 4}, evs)
	ok := Record{Scores: []evalcore.Score{sc("json", 1, true)}}
	for _, r := range []Record{fail, fail, fail} {
		if ex, _ := tr.Observe(r); ex {
			t.Fatal("rate not checked before MinResults")
		}
	}
	if ex, _ := tr.Observe(ok); !ex {
		t.Fatal("3/4 > 50% should exceed")
	}

	zero := 0
	tr = NewBudgetTracker(FailureBudget{MaxErrors: &zero}, evs)
	if ex, _ := tr.Observe(Record{Error: "boom"}); !ex {
		t.Fatal("target error counts")
	}
}

func TestBudgetFromGate(t *testing.T) {
	b := BudgetFromGate(MustParse("mean(f) > 0.8 and count(critical_fail) == 0 and count(fail) < 10 and 5 >= count(error)"))
	if b.MaxCriticalFails == nil || *b.MaxCriticalFails != 0 || *b.MaxFails != 9 || *b.MaxErrors != 5 || b.MaxFailRate != nil {
		t.Fatalf("%+v", b)
	}
	// Under "or" nothing is a hard limit.
	if b := BudgetFromGate(MustParse("count(fail) == 0 or mean(f) > 0.9")); !b.IsZero() {
		t.Fatalf("or: %+v", b)
	}
	// Lower bounds are not budgets.
	if b := BudgetFromGate(MustParse("count(pass) >= 10 and count(fail) > 1")); !b.IsZero() {
		t.Fatalf("lower bounds: %+v", b)
	}
	explicit := 3
	m := FailureBudget{MaxFails: &explicit}.Merge(BudgetFromGate(MustParse("count(fail) == 0 and count(error) == 0")))
	if *m.MaxFails != 3 || *m.MaxErrors != 0 {
		t.Fatalf("merge: %+v", m)
	}
}
