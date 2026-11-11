// Package aggregate computes run statistics from streamed results: moments,
// percentiles (exact up to a limit, t-digest beyond), histograms, pass rates,
// per-slice breakdowns, trials (pass@k), run comparisons with paired
// bootstrap confidence intervals, and gates (expression AST, parser, pretty
// printer, evaluator) plus incremental failure budgets for early stop.
//
// It depends only on internal/evalcore.
package aggregate

import (
	"math"
	"slices"
)

// DefaultExactLimit is how many values a Stream keeps for exact quantiles
// before switching to a t-digest.
const DefaultExactLimit = 100_000

// Stream accumulates a numeric distribution in one pass. Not safe for
// concurrent use; merge per-goroutine streams with Merge.
type Stream struct {
	n             int
	mean, m2      float64 // Welford
	min, max, sum float64
	vals          []float64
	sorted        bool
	td            *TDigest
	limit         int
}

// NewStream returns a Stream that is exact for up to limit values
// (DefaultExactLimit when limit <= 0).
func NewStream(limit int) *Stream {
	if limit <= 0 {
		limit = DefaultExactLimit
	}
	return &Stream{limit: limit, min: math.Inf(1), max: math.Inf(-1)}
}

// Add records x. NaN and ±Inf are ignored.
func (s *Stream) Add(x float64) {
	if math.IsNaN(x) || math.IsInf(x, 0) {
		return
	}
	s.n++
	d := x - s.mean
	s.mean += d / float64(s.n)
	s.m2 += d * (x - s.mean)
	s.sum += x
	if x < s.min {
		s.min = x
	}
	if x > s.max {
		s.max = x
	}
	if s.td != nil {
		s.td.Add(x)
		return
	}
	s.vals = append(s.vals, x)
	s.sorted = false
	if len(s.vals) > s.limit {
		s.spill()
	}
}

func (s *Stream) spill() {
	s.td = NewTDigest(200)
	for _, v := range s.vals {
		s.td.Add(v)
	}
	s.vals = nil
}

// Merge folds o into s (Chan et al. parallel moments).
func (s *Stream) Merge(o *Stream) {
	if o == nil || o.n == 0 {
		return
	}
	if s.n == 0 {
		lim := s.limit
		*s = *o
		s.vals = slices.Clone(o.vals)
		if o.td != nil {
			s.td = NewTDigest(200)
			s.td.Merge(o.td)
		}
		s.limit = lim
		if s.td == nil && len(s.vals) > s.limit {
			s.spill()
		}
		return
	}
	n := float64(s.n + o.n)
	d := o.mean - s.mean
	s.m2 += o.m2 + d*d*float64(s.n)*float64(o.n)/n
	s.mean += d * float64(o.n) / n
	s.n += o.n
	s.sum += o.sum
	s.min = math.Min(s.min, o.min)
	s.max = math.Max(s.max, o.max)
	switch {
	case s.td == nil && o.td == nil && len(s.vals)+len(o.vals) <= s.limit:
		s.vals = append(s.vals, o.vals...)
		s.sorted = false
	default:
		if s.td == nil {
			s.spill()
		}
		if o.td != nil {
			s.td.Merge(o.td)
		} else {
			for _, v := range o.vals {
				s.td.Add(v)
			}
		}
	}
}

// N is the number of values.
func (s *Stream) N() int { return s.n }

// Mean is the arithmetic mean (0 when empty).
func (s *Stream) Mean() float64 {
	if s.n == 0 {
		return 0
	}
	return s.mean
}

// Stddev is the sample standard deviation (0 for fewer than 2 values).
func (s *Stream) Stddev() float64 {
	if s.n < 2 {
		return 0
	}
	return math.Sqrt(s.m2 / float64(s.n-1))
}

// Sum of all values.
func (s *Stream) Sum() float64 { return s.sum }

// Min value (0 when empty).
func (s *Stream) Min() float64 {
	if s.n == 0 {
		return 0
	}
	return s.min
}

// Max value (0 when empty).
func (s *Stream) Max() float64 {
	if s.n == 0 {
		return 0
	}
	return s.max
}

// Exact reports whether quantiles are exact.
func (s *Stream) Exact() bool { return s.td == nil }

// Quantile returns the q-quantile with linear interpolation between order
// statistics (R type 7) when exact; a t-digest estimate otherwise. 0 when
// empty.
func (s *Stream) Quantile(q float64) float64 {
	if s.n == 0 {
		return 0
	}
	q = math.Max(0, math.Min(1, q))
	if s.td != nil {
		return s.td.Quantile(q)
	}
	if !s.sorted {
		slices.Sort(s.vals)
		s.sorted = true
	}
	h := float64(len(s.vals)-1) * q
	lo := int(math.Floor(h))
	if lo >= len(s.vals)-1 {
		return s.vals[len(s.vals)-1]
	}
	return s.vals[lo] + (h-float64(lo))*(s.vals[lo+1]-s.vals[lo])
}

// Dist is a JSON-friendly summary of a distribution. All fields are finite.
type Dist struct {
	N         int        `json:"n"`
	Mean      float64    `json:"mean"`
	Stddev    float64    `json:"stddev"`
	Min       float64    `json:"min"`
	Max       float64    `json:"max"`
	Sum       float64    `json:"sum"`
	P50       float64    `json:"p50"`
	P90       float64    `json:"p90"`
	P95       float64    `json:"p95"`
	P99       float64    `json:"p99"`
	Exact     bool       `json:"exact"`
	Histogram *Histogram `json:"histogram,omitempty"`
}

// Histogram has len(Counts) bins with boundaries Edges (len(Counts)+1).
// Bin i covers [Edges[i], Edges[i+1]); the last bin includes its upper edge.
type Histogram struct {
	Edges  []float64 `json:"edges"`
	Counts []int     `json:"counts"`
}

// HistKind selects the histogram layout of a Dist.
type HistKind int

const (
	HistNone HistKind = iota
	HistUnit          // 20 fixed bins on [0, 1] (scores)
	HistAuto          // "nice" bins over the observed range (latency, cost, tokens)
)

// Dist summarizes the stream.
func (s *Stream) Dist(h HistKind) Dist {
	d := Dist{N: s.n, Exact: s.Exact()}
	if s.n == 0 {
		return d
	}
	d.Mean, d.Stddev, d.Min, d.Max, d.Sum = s.Mean(), s.Stddev(), s.Min(), s.Max(), s.Sum()
	d.P50, d.P90, d.P95, d.P99 = s.Quantile(0.5), s.Quantile(0.9), s.Quantile(0.95), s.Quantile(0.99)
	switch h {
	case HistUnit:
		d.Histogram = s.histogram(unitEdges())
	case HistAuto:
		d.Histogram = s.histogram(autoEdges(s.min, s.max, s.n))
	}
	return d
}

func unitEdges() []float64 {
	e := make([]float64, 21)
	for i := range e {
		e[i] = float64(i) / 20
	}
	return e
}

// autoEdges picks 1/2/5×10^k bin widths giving about sqrt(n) bins (5–40).
func autoEdges(lo, hi float64, n int) []float64 {
	bins := int(math.Ceil(math.Sqrt(float64(n))))
	bins = max(5, min(40, bins))
	if hi <= lo {
		w := math.Max(math.Abs(lo)*0.1, 1)
		return []float64{lo - w/2, lo + w/2}
	}
	w := niceStep((hi - lo) / float64(bins))
	start := math.Floor(lo/w) * w
	var e []float64
	for x := start; ; x += w {
		e = append(e, round12(x))
		if x > hi || len(e) > 200 {
			break
		}
	}
	if len(e) < 2 {
		e = append(e, round12(start+w))
	}
	return e
}

func round12(x float64) float64 { return math.Round(x*1e12) / 1e12 }

func niceStep(raw float64) float64 {
	exp := math.Floor(math.Log10(raw))
	base := math.Pow(10, exp)
	f := raw / base
	switch {
	case f <= 1:
		return base
	case f <= 2:
		return 2 * base
	case f <= 5:
		return 5 * base
	}
	return 10 * base
}

func (s *Stream) histogram(edges []float64) *Histogram {
	h := &Histogram{Edges: edges, Counts: make([]int, len(edges)-1)}
	if s.td == nil {
		for _, v := range s.vals {
			h.Counts[bin(edges, v)]++
		}
		return h
	}
	// Approximate from centroids, distributing rounding so totals match.
	acc := make([]float64, len(h.Counts))
	for _, c := range s.td.points() {
		acc[bin(edges, c.mean)] += c.w
	}
	carry := 0.0
	for i, a := range acc {
		v := a + carry
		h.Counts[i] = int(math.Round(v))
		carry = v - float64(h.Counts[i])
	}
	return h
}

func bin(edges []float64, v float64) int {
	n := len(edges) - 1
	if v <= edges[0] {
		return 0
	}
	if v >= edges[n] {
		return n - 1
	}
	// edges are sorted: binary search for the last edge <= v.
	i, _ := slices.BinarySearch(edges, v)
	if i < len(edges) && edges[i] == v {
		return min(i, n-1)
	}
	return min(i-1, n-1)
}
