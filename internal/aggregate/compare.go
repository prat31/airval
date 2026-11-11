package aggregate

import (
	"math"
	"math/rand/v2"
	"sort"
)

// CompareOptions tune Compare.
type CompareOptions struct {
	// Evaluators to compare (default: every evaluator present in both).
	Evaluators []string
	// Epsilon is the minimum |delta| that counts as a change (default 1e-9).
	Epsilon float64
	// Resamples for the paired bootstrap (default 2000).
	Resamples int
	// Confidence level of the interval (default 0.95).
	Confidence float64
	// Seed makes the bootstrap deterministic (default 1).
	Seed uint64
	// MaxListed caps the regressions/improvements lists (default 100).
	MaxListed int
	// BootstrapLimit: above this many paired examples the CI uses the
	// normal approximation instead of resampling (default 20,000).
	BootstrapLimit int
}

func (o *CompareOptions) defaults() {
	if o.Epsilon <= 0 {
		o.Epsilon = 1e-9
	}
	if o.Resamples <= 0 {
		o.Resamples = 2000
	}
	if o.Confidence <= 0 || o.Confidence >= 1 {
		o.Confidence = 0.95
	}
	if o.Seed == 0 {
		o.Seed = 1
	}
	if o.MaxListed <= 0 {
		o.MaxListed = 100
	}
	if o.BootstrapLimit <= 0 {
		o.BootstrapLimit = 20_000
	}
}

// Comparison is a baseline vs candidate diff of two runs.
type Comparison struct {
	Evaluators []EvaluatorComparison `json:"evaluators"`
}

// Evaluator returns the comparison for one evaluator, or false.
func (c *Comparison) Evaluator(id string) (EvaluatorComparison, bool) {
	if c == nil {
		return EvaluatorComparison{}, false
	}
	for _, e := range c.Evaluators {
		if e.ID == id {
			return e, true
		}
	}
	return EvaluatorComparison{}, false
}

// EvaluatorComparison compares one evaluator over paired examples.
type EvaluatorComparison struct {
	ID            string  `json:"id"`
	Paired        int     `json:"paired"` // examples scored in both runs
	BaselineOnly  int     `json:"baseline_only"`
	CandidateOnly int     `json:"candidate_only"`
	BaselineMean  float64 `json:"baseline_mean"`
	CandidateMean float64 `json:"candidate_mean"`
	// MeanDelta = candidate - baseline over paired examples (positive is better).
	MeanDelta float64 `json:"mean_delta"`
	CILow     float64 `json:"ci_low"`
	CIHigh    float64 `json:"ci_high"`
	// PValue is a two-sided bootstrap p-value for "mean delta = 0".
	PValue      float64 `json:"p_value"`
	Significant bool    `json:"significant"` // CI excludes 0
	Method      string  `json:"method"`      // bootstrap | normal | none

	Regressed int `json:"regressed"`
	Improved  int `json:"improved"`
	Unchanged int `json:"unchanged"`
	NewlyFail int `json:"newly_failing"` // passed in baseline, fails in candidate
	NewlyPass int `json:"newly_passing"`

	// Regressions are sorted worst first; Improvements best first.
	Regressions  []ExampleDelta `json:"regressions"`
	Improvements []ExampleDelta `json:"improvements"`
}

// ExampleDelta is one example's change.
type ExampleDelta struct {
	ExampleID string  `json:"example_id"`
	Baseline  float64 `json:"baseline"`
	Candidate float64 `json:"candidate"`
	Delta     float64 `json:"delta"`
	// Pass status (majority over trials) when the evaluator has a threshold.
	BaselinePassed  *bool `json:"baseline_passed,omitempty"`
	CandidatePassed *bool `json:"candidate_passed,omitempty"`
}

func passedOf(r ExampleRollup) *bool {
	if r.Passed+r.Failed == 0 {
		return nil
	}
	p := r.Passed >= r.Failed
	return &p
}

// Compare diffs candidate against baseline. Examples are paired by id;
// per-example values are means over trials.
func Compare(baseline, candidate Rollup, opts CompareOptions) *Comparison {
	opts.defaults()
	ids := opts.Evaluators
	if len(ids) == 0 {
		for id := range candidate {
			if _, ok := baseline[id]; ok {
				ids = append(ids, id)
			}
		}
		sort.Strings(ids)
	}
	c := &Comparison{}
	for _, id := range ids {
		c.Evaluators = append(c.Evaluators, compareOne(id, baseline[id], candidate[id], opts))
	}
	return c
}

// CompareRecords builds rollups from two record sets and compares them.
func CompareRecords(baseline, candidate []Record, opts CompareOptions) *Comparison {
	return Compare(rollupOf(baseline), rollupOf(candidate), opts)
}

func rollupOf(rs []Record) Rollup {
	a := New(nil, Options{NoTagSlices: true})
	for _, r := range rs {
		a.Add(r)
	}
	return a.Rollup()
}

func compareOne(id string, base, cand map[string]ExampleRollup, opts CompareOptions) EvaluatorComparison {
	ec := EvaluatorComparison{ID: id, Method: "none", PValue: 1}
	exIDs := make([]string, 0, len(cand))
	for ex := range cand {
		if _, ok := base[ex]; ok {
			exIDs = append(exIDs, ex)
		} else {
			ec.CandidateOnly++
		}
	}
	for ex := range base {
		if _, ok := cand[ex]; !ok {
			ec.BaselineOnly++
		}
	}
	sort.Strings(exIDs) // deterministic bootstrap input order
	deltas := make([]float64, len(exIDs))
	var sb, sc float64
	for i, ex := range exIDs {
		b, cnd := base[ex], cand[ex]
		d := cnd.Mean - b.Mean
		deltas[i] = d
		sb += b.Mean
		sc += cnd.Mean
		ed := ExampleDelta{ExampleID: ex, Baseline: b.Mean, Candidate: cnd.Mean, Delta: d,
			BaselinePassed: passedOf(b), CandidatePassed: passedOf(cnd)}
		switch {
		case d < -opts.Epsilon:
			ec.Regressed++
			ec.Regressions = append(ec.Regressions, ed)
		case d > opts.Epsilon:
			ec.Improved++
			ec.Improvements = append(ec.Improvements, ed)
		default:
			ec.Unchanged++
		}
		if ed.BaselinePassed != nil && ed.CandidatePassed != nil {
			if *ed.BaselinePassed && !*ed.CandidatePassed {
				ec.NewlyFail++
			} else if !*ed.BaselinePassed && *ed.CandidatePassed {
				ec.NewlyPass++
			}
		}
	}
	n := len(exIDs)
	ec.Paired = n
	if n == 0 {
		return ec
	}
	ec.BaselineMean, ec.CandidateMean = sb/float64(n), sc/float64(n)
	ec.MeanDelta = ec.CandidateMean - ec.BaselineMean
	sort.SliceStable(ec.Regressions, func(i, j int) bool { return ec.Regressions[i].Delta < ec.Regressions[j].Delta })
	sort.SliceStable(ec.Improvements, func(i, j int) bool { return ec.Improvements[i].Delta > ec.Improvements[j].Delta })
	if len(ec.Regressions) > opts.MaxListed {
		ec.Regressions = ec.Regressions[:opts.MaxListed]
	}
	if len(ec.Improvements) > opts.MaxListed {
		ec.Improvements = ec.Improvements[:opts.MaxListed]
	}
	ec.CILow, ec.CIHigh, ec.PValue, ec.Method = pairedCI(deltas, opts)
	ec.Significant = n > 1 && (ec.CILow > 0 || ec.CIHigh < 0)
	return ec
}

// pairedCI returns a confidence interval and two-sided p-value for the
// mean of deltas.
func pairedCI(deltas []float64, opts CompareOptions) (lo, hi, p float64, method string) {
	n := len(deltas)
	mean := 0.0
	for _, d := range deltas {
		mean += d
	}
	mean /= float64(n)
	if n < 2 {
		return mean, mean, 1, "none"
	}
	alpha := 1 - opts.Confidence
	if n > opts.BootstrapLimit {
		ss := 0.0
		for _, d := range deltas {
			ss += (d - mean) * (d - mean)
		}
		se := math.Sqrt(ss/float64(n-1)) / math.Sqrt(float64(n))
		if se == 0 {
			return mean, mean, pZero(mean), "normal"
		}
		z := normQuantile(1 - alpha/2)
		zp := math.Abs(mean) / se
		return mean - z*se, mean + z*se, math.Erfc(zp / math.Sqrt2), "normal"
	}
	rng := rand.New(rand.NewPCG(opts.Seed, opts.Seed^0x9e3779b97f4a7c15))
	means := make([]float64, opts.Resamples)
	le, ge := 0, 0
	for b := range means {
		s := 0.0
		for i := 0; i < n; i++ {
			s += deltas[rng.IntN(n)]
		}
		m := s / float64(n)
		means[b] = m
		if m <= 0 {
			le++
		}
		if m >= 0 {
			ge++
		}
	}
	sort.Float64s(means)
	lo = quantileSorted(means, alpha/2)
	hi = quantileSorted(means, 1-alpha/2)
	p = math.Min(1, 2*math.Min(float64(le), float64(ge))/float64(opts.Resamples))
	return lo, hi, p, "bootstrap"
}

func pZero(mean float64) float64 {
	if mean == 0 {
		return 1
	}
	return 0
}

func quantileSorted(s []float64, q float64) float64 {
	h := float64(len(s)-1) * q
	l := int(math.Floor(h))
	if l >= len(s)-1 {
		return s[len(s)-1]
	}
	return s[l] + (h-float64(l))*(s[l+1]-s[l])
}

// normQuantile is the standard normal inverse CDF (Acklam's algorithm,
// relative error < 1.2e-9).
func normQuantile(p float64) float64 {
	a := [6]float64{-3.969683028665376e+01, 2.209460984245205e+02, -2.759285104469687e+02, 1.383577518672690e+02, -3.066479806614716e+01, 2.506628277459239e+00}
	b := [5]float64{-5.447609879822406e+01, 1.615858368580409e+02, -1.556989798598866e+02, 6.680131188771972e+01, -1.328068155288572e+01}
	c := [6]float64{-7.784894002430293e-03, -3.223964580411365e-01, -2.400758277161838e+00, -2.549732539343734e+00, 4.374664141464968e+00, 2.938163982698783e+00}
	d := [4]float64{7.784695709041462e-03, 3.224671290700398e-01, 2.445134137142996e+00, 3.754408661907416e+00}
	const plow = 0.02425
	switch {
	case p < plow:
		q := math.Sqrt(-2 * math.Log(p))
		return (((((c[0]*q+c[1])*q+c[2])*q+c[3])*q+c[4])*q + c[5]) / ((((d[0]*q+d[1])*q+d[2])*q+d[3])*q + 1)
	case p <= 1-plow:
		q := p - 0.5
		r := q * q
		return (((((a[0]*r+a[1])*r+a[2])*r+a[3])*r+a[4])*r + a[5]) * q / (((((b[0]*r+b[1])*r+b[2])*r+b[3])*r+b[4])*r + 1)
	default:
		q := math.Sqrt(-2 * math.Log(1-p))
		return -(((((c[0]*q+c[1])*q+c[2])*q+c[3])*q+c[4])*q + c[5]) / ((((d[0]*q+d[1])*q+d[2])*q+d[3])*q + 1)
	}
}
