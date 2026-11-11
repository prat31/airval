package aggregate

import (
	"encoding/json"
	"math"
	"math/rand/v2"
	"slices"
	"testing"
	"time"

	"github.com/airval/airval/internal/evalcore"
)

func near(t *testing.T, name string, got, want, tol float64) {
	t.Helper()
	if math.Abs(got-want) > tol {
		t.Errorf("%s = %v, want %v (±%v)", name, got, want, tol)
	}
}

func sc(id string, v float64, passed bool) evalcore.Score {
	return evalcore.Score{EvaluatorID: id, Value: &v, Passed: &passed}
}

func TestStreamExact(t *testing.T) {
	s := NewStream(0)
	for _, x := range []float64{2, 4, 4, 4, 5, 5, 7, 9} {
		s.Add(x)
	}
	s.Add(math.NaN())
	near(t, "mean", s.Mean(), 5, 1e-12)
	near(t, "stddev", s.Stddev(), math.Sqrt(32.0/7), 1e-12)
	near(t, "p50", s.Quantile(0.5), 4.5, 1e-12)
	// R type 7: h = 7*0.9 = 6.3 → 7 + 0.3*(9-7) = 7.6
	near(t, "p90", s.Quantile(0.9), 7.6, 1e-12)
	near(t, "min", s.Quantile(0), 2, 0)
	near(t, "max", s.Quantile(1), 9, 0)
	if s.N() != 8 || s.Sum() != 40 || !s.Exact() {
		t.Fatalf("n=%d sum=%v", s.N(), s.Sum())
	}
	d := s.Dist(HistNone)
	if d.Histogram != nil || d.P95 == 0 {
		t.Fatal("dist")
	}
	e := NewStream(0).Dist(HistUnit)
	if e.N != 0 || e.Mean != 0 || e.Histogram != nil {
		t.Fatal("empty dist must be zero")
	}
	if _, err := json.Marshal(e); err != nil {
		t.Fatal(err)
	}
}

func TestStreamMerge(t *testing.T) {
	a, b, all := NewStream(0), NewStream(0), NewStream(0)
	r := rand.New(rand.NewPCG(1, 2))
	for i := 0; i < 1000; i++ {
		x := r.NormFloat64()*3 + 10
		all.Add(x)
		if i%3 == 0 {
			a.Add(x)
		} else {
			b.Add(x)
		}
	}
	a.Merge(b)
	near(t, "mean", a.Mean(), all.Mean(), 1e-9)
	near(t, "stddev", a.Stddev(), all.Stddev(), 1e-9)
	near(t, "p95", a.Quantile(0.95), all.Quantile(0.95), 1e-12)
	empty := NewStream(0)
	empty.Merge(all)
	near(t, "merged into empty", empty.Quantile(0.5), all.Quantile(0.5), 0)
}

func TestTDigestAccuracy(t *testing.T) {
	const n = 300_000
	s := NewStream(1000) // force the digest
	exact := make([]float64, 0, n)
	r := rand.New(rand.NewPCG(7, 7))
	for i := 0; i < n; i++ {
		x := r.ExpFloat64() * 100 // skewed like latency
		s.Add(x)
		exact = append(exact, x)
	}
	if s.Exact() {
		t.Fatal("should have spilled to t-digest")
	}
	slices.Sort(exact)
	for _, q := range []float64{0.5, 0.9, 0.95, 0.99, 0.999} {
		want := exact[int(q*float64(n-1))]
		got := s.Quantile(q)
		if rel := math.Abs(got-want) / want; rel > 0.01 {
			t.Errorf("q%.3f: got %.3f want %.3f (rel err %.4f)", q, got, want, rel)
		}
	}
	h := s.Dist(HistAuto).Histogram
	total := 0
	for _, c := range h.Counts {
		total += c
	}
	if total != n {
		t.Fatalf("digest histogram total %d, want %d", total, n)
	}
	// Merging digests.
	a, b := NewTDigest(0), NewTDigest(0)
	for i := 0; i < 50_000; i++ {
		a.Add(float64(i))
		b.Add(float64(i + 50_000))
	}
	a.Merge(b)
	near(t, "merged median", a.Quantile(0.5), 50_000, 500)
	if a.Count() != 100_000 {
		t.Fatal("count")
	}
}

func TestHistograms(t *testing.T) {
	s := NewStream(0)
	for _, v := range []float64{0, 0.04, 0.05, 0.5, 0.99, 1} {
		s.Add(v)
	}
	h := s.Dist(HistUnit).Histogram
	if len(h.Counts) != 20 || len(h.Edges) != 21 {
		t.Fatal("unit histogram must have 20 bins")
	}
	want := map[int]int{0: 2, 1: 1, 10: 1, 19: 2}
	for i, c := range h.Counts {
		if c != want[i] {
			t.Errorf("bin %d = %d, want %d", i, c, want[i])
		}
	}
	lat := NewStream(0)
	for i := 0; i < 100; i++ {
		lat.Add(float64(120 + i*13))
	}
	lh := lat.Dist(HistAuto).Histogram
	total := 0
	for _, c := range lh.Counts {
		total += c
	}
	if total != 100 || lh.Edges[0] > 120 || lh.Edges[len(lh.Edges)-1] < 120+99*13 || len(lh.Counts) < 5 || len(lh.Counts) > 41 {
		t.Fatalf("auto histogram %+v", lh)
	}
	one := NewStream(0)
	one.Add(5)
	if hh := one.Dist(HistAuto).Histogram; hh.Counts[0] != 1 {
		t.Fatal("single value histogram")
	}
}

func evals() []EvaluatorInfo {
	return []EvaluatorInfo{
		{ID: "faithfulness", Kind: "faithfulness", Severity: evalcore.SeverityMajor, Threshold: 0.75},
		{ID: "json", Kind: "is_json", Severity: evalcore.SeverityCritical, Threshold: 1},
	}
}

func TestAggregator(t *testing.T) {
	a := New(evals(), Options{MetadataKeys: []string{"lang"}})
	recs := []Record{
		{ExampleID: "e1", Tags: []string{"billing"}, Metadata: map[string]any{"lang": "en"}, Latency: 100 * time.Millisecond, CostUSD: 0.01,
			Usage:  evalcore.Usage{InputTokens: 10, OutputTokens: 5},
			Scores: []evalcore.Score{sc("faithfulness", 1, true), sc("json", 1, true)}},
		{ExampleID: "e2", Tags: []string{"billing", "hard"}, Metadata: map[string]any{"lang": "de"}, Latency: 300 * time.Millisecond, CostUSD: 0.03,
			Usage:  evalcore.Usage{InputTokens: 20, OutputTokens: 10},
			Scores: []evalcore.Score{sc("faithfulness", 0.5, false), sc("json", 1, true)}},
		{ExampleID: "e3", Latency: 200 * time.Millisecond, CostUSD: 0.02,
			Scores: []evalcore.Score{{EvaluatorID: "faithfulness", Label: "error", Meta: map[string]any{"error": "timeout"}}, sc("json", 0, false)}},
		{ExampleID: "e4", Error: "connection refused"},
		{ExampleID: "e5", Latency: 400 * time.Millisecond, Scores: []evalcore.Score{
			sc("faithfulness", 0.75, true), sc("json", 1, true), {EvaluatorID: "review", Label: "pending"}}},
	}
	recs[1].Scores[0].CostUSD = 0.002
	for _, r := range recs {
		a.Add(r)
	}
	s := a.Summary()
	if s.Results != 5 || s.Examples != 5 || s.Passed != 2 || s.Failed != 3 || s.CriticalFailed != 1 || s.MajorFailed != 1 || s.Errored != 2 || s.Pending != 1 {
		t.Fatalf("counts: %+v", s)
	}
	near(t, "pass rate", s.PassRate, 0.4, 1e-12)
	near(t, "latency mean", s.LatencyMs.Mean, 250, 1e-9)
	near(t, "latency p50", s.LatencyMs.P50, 250, 1e-9)
	near(t, "target cost", s.CostUSD.Sum, 0.06, 1e-12)
	near(t, "total cost", s.TotalCostUSD, 0.062, 1e-12)
	near(t, "tokens", s.Tokens.Sum, 45, 0)

	f, _ := s.Evaluator("faithfulness")
	if f.Values.N != 3 || f.Passed != 2 || f.Failed != 1 || f.Errored != 1 {
		t.Fatalf("faithfulness: %+v", f)
	}
	near(t, "faith mean", f.Values.Mean, 0.75, 1e-12)
	near(t, "faith pass rate (errors count against)", f.PassRate, 2.0/4, 1e-12)
	if f.Values.Histogram == nil || len(f.Values.Histogram.Counts) != 20 {
		t.Fatal("score histogram")
	}
	r, ok := s.Evaluator("review")
	if !ok || r.Pending != 1 || r.Values.N != 0 {
		t.Fatalf("unlisted evaluator: %+v", r)
	}
	var billing, de *SliceSummary
	for i := range s.Slices {
		switch {
		case s.Slices[i].Key == "tag" && s.Slices[i].Value == "billing":
			billing = &s.Slices[i]
		case s.Slices[i].Key == "metadata.lang" && s.Slices[i].Value == "de":
			de = &s.Slices[i]
		}
	}
	if billing == nil || billing.Results != 2 || billing.Passed != 1 || billing.Evaluators["faithfulness"].Mean != 0.75 {
		t.Fatalf("billing slice: %+v", billing)
	}
	if de == nil || de.PassRate != 0 || de.Evaluators["faithfulness"].PassRate != 0 {
		t.Fatalf("de slice: %+v", de)
	}
	if s.Trials != nil {
		t.Fatal("single trial runs have no trials summary")
	}
	if _, err := json.Marshal(s); err != nil {
		t.Fatalf("summary must be JSON-serializable: %v", err)
	}
}

func TestSliceCap(t *testing.T) {
	a := New(nil, Options{MaxSliceValues: 2})
	for _, tag := range []string{"a", "b", "c", "d"} {
		a.Add(Record{ExampleID: tag, Tags: []string{tag}})
	}
	s := a.Summary()
	var other int
	for _, sl := range s.Slices {
		if sl.Value == "(other)" {
			other = sl.Results
		}
	}
	if len(s.Slices) != 3 || other != 2 {
		t.Fatalf("slices: %+v", s.Slices)
	}
}

func TestPassAtK(t *testing.T) {
	// n=5, c=2, k=2: 1 - C(3,2)/C(5,2) = 1 - 3/10 = 0.7
	near(t, "pass@2", PassAtK(5, 2, 2), 0.7, 1e-12)
	near(t, "pass@1", PassAtK(5, 2, 1), 0.4, 1e-12)
	near(t, "pass@k all fail", PassAtK(5, 0, 3), 0, 0)
	near(t, "pass@k forced", PassAtK(5, 3, 3), 1, 0)
	// pass^2: C(2,2)/C(5,2) = 0.1
	near(t, "pass^2", PassHatK(5, 2, 2), 0.1, 1e-12)
	near(t, "pass^k too few", PassHatK(5, 1, 2), 0, 0)

	a := New([]EvaluatorInfo{{ID: "c", Threshold: 0.5}}, Options{})
	// e1: 3 trials, 2 pass. e2: 3 trials, all pass (identical values).
	for i, v := range []float64{1, 0.9, 0} {
		a.Add(Record{ExampleID: "e1", Trial: i, Scores: []evalcore.Score{sc("c", v, v >= 0.5)}})
		a.Add(Record{ExampleID: "e2", Trial: i, Scores: []evalcore.Score{sc("c", 1, true)}})
	}
	tr := a.Summary().Trials
	if tr == nil || tr.MaxTrials != 3 || tr.Flaky != 1 {
		t.Fatalf("trials: %+v", tr)
	}
	near(t, "pass@1", tr.PassAtK[0], (2.0/3+1)/2, 1e-12)
	near(t, "pass@2", tr.PassAtK[1], (1+1)/2.0, 1e-12)
	near(t, "pass^3", tr.PassHatK[2], (0+1)/2.0, 1e-12)
	// var(e1) = var(1,0.9,0) = 0.91/3 ≈ 0.30333, var(e2) = 0 → mean 0.151667
	near(t, "mean variance", tr.MeanVariance["c"], 0.91/6, 1e-12)
	if len(tr.MostVariable) != 1 || tr.MostVariable[0].ExampleID != "e1" {
		t.Fatalf("most variable: %+v", tr.MostVariable)
	}
	if et := a.ExampleTrials(); len(et) != 2 || et[0].Trials != 3 {
		t.Fatalf("example trials %+v", et)
	}
}

func rollup(vals map[string]float64) Rollup {
	m := map[string]ExampleRollup{}
	for k, v := range vals {
		m[k] = ExampleRollup{Mean: v, N: 1, Passed: b2i(v >= 0.5), Failed: b2i(v < 0.5)}
	}
	return Rollup{"q": m}
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

func TestCompare(t *testing.T) {
	base := rollup(map[string]float64{"a": 0.9, "b": 0.8, "c": 0.5, "d": 0.4, "only_base": 1})
	cand := rollup(map[string]float64{"a": 0.9, "b": 0.2, "c": 0.7, "d": 0.6, "only_cand": 1})
	c := Compare(base, cand, CompareOptions{})
	ec, ok := c.Evaluator("q")
	if !ok {
		t.Fatal("missing evaluator")
	}
	if ec.Paired != 4 || ec.BaselineOnly != 1 || ec.CandidateOnly != 1 || ec.Regressed != 1 || ec.Improved != 2 || ec.Unchanged != 1 {
		t.Fatalf("counts: %+v", ec)
	}
	near(t, "mean delta", ec.MeanDelta, (0-0.6+0.2+0.2)/4, 1e-12)
	if ec.Regressions[0].ExampleID != "b" || ec.Improvements[0].Delta < ec.Improvements[1].Delta {
		t.Fatalf("ranking: %+v %+v", ec.Regressions, ec.Improvements)
	}
	if ec.NewlyFail != 1 || ec.NewlyPass != 1 {
		t.Fatalf("flips: fail %d pass %d", ec.NewlyFail, ec.NewlyPass)
	}
	if ec.Method != "bootstrap" || ec.CILow > ec.MeanDelta || ec.CIHigh < ec.MeanDelta || ec.Significant {
		t.Fatalf("CI: %+v", ec)
	}
	// Deterministic with the same seed.
	c2 := Compare(base, cand, CompareOptions{})
	if c2.Evaluators[0].CILow != ec.CILow || c2.Evaluators[0].PValue != ec.PValue {
		t.Fatal("bootstrap must be deterministic")
	}

	// A consistent improvement over many examples is significant.
	bb, cc := map[string]float64{}, map[string]float64{}
	r := rand.New(rand.NewPCG(3, 4))
	for i := 0; i < 200; i++ {
		id := string(rune('A'+i%26)) + string(rune('a'+i/26))
		x := r.Float64() * 0.8
		bb[id] = x
		cc[id] = x + 0.1 + r.NormFloat64()*0.02
	}
	ec = Compare(rollup(bb), rollup(cc), CompareOptions{}).Evaluators[0]
	if !ec.Significant || ec.CILow <= 0 || ec.PValue > 0.01 {
		t.Fatalf("expected significant improvement: %+v", ec)
	}
	near(t, "mean delta", ec.MeanDelta, 0.1, 0.01)
	// Normal approximation path agrees.
	en := Compare(rollup(bb), rollup(cc), CompareOptions{BootstrapLimit: 10}).Evaluators[0]
	if en.Method != "normal" || math.Abs(en.CILow-ec.CILow) > 0.005 || math.Abs(en.CIHigh-ec.CIHigh) > 0.005 {
		t.Fatalf("normal vs bootstrap: %+v vs %+v", en, ec)
	}
	near(t, "normQuantile", normQuantile(0.975), 1.959963984540054, 1e-8)
}

func TestCompareRecords(t *testing.T) {
	mk := func(vals ...float64) []Record {
		var rs []Record
		for i, v := range vals {
			rs = append(rs, Record{ExampleID: string(rune('a' + i)), Scores: []evalcore.Score{sc("x", v, v >= 0.5)}})
		}
		return rs
	}
	c := CompareRecords(mk(1, 1, 0), mk(1, 0, 0), CompareOptions{})
	ec := c.Evaluators[0]
	if ec.Paired != 3 || ec.Regressed != 1 || ec.Regressions[0].ExampleID != "b" {
		t.Fatalf("%+v", ec)
	}
}

func BenchmarkAggregate100k(b *testing.B) {
	r := rand.New(rand.NewPCG(1, 1))
	recs := make([]Record, 100_000)
	for i := range recs {
		v1, v2 := r.Float64(), r.Float64()
		recs[i] = Record{
			ExampleID: "e" + string(rune(i)), Tags: []string{[]string{"a", "b", "c"}[i%3]},
			Latency: time.Duration(r.ExpFloat64()*200) * time.Millisecond, CostUSD: 0.001,
			Usage:  evalcore.Usage{InputTokens: 100, OutputTokens: 50},
			Scores: []evalcore.Score{sc("faithfulness", v1, v1 >= 0.75), sc("json", v2, v2 >= 0.1)},
		}
	}
	b.ReportAllocs()
	for b.Loop() {
		a := New(evals(), Options{})
		for i := range recs {
			a.Add(recs[i])
		}
		_ = a.Summary()
	}
}

func BenchmarkStreamQuantile100k(b *testing.B) {
	r := rand.New(rand.NewPCG(1, 1))
	vals := make([]float64, 100_000)
	for i := range vals {
		vals[i] = r.Float64()
	}
	b.ReportAllocs()
	for b.Loop() {
		s := NewStream(0)
		for _, v := range vals {
			s.Add(v)
		}
		_ = s.Dist(HistUnit)
	}
}

func BenchmarkTDigest1M(b *testing.B) {
	r := rand.New(rand.NewPCG(1, 1))
	vals := make([]float64, 1_000_000)
	for i := range vals {
		vals[i] = r.ExpFloat64()
	}
	b.ReportAllocs()
	for b.Loop() {
		td := NewTDigest(0)
		for _, v := range vals {
			td.Add(v)
		}
		_ = td.Quantile(0.99)
	}
}
