package aggregate

import (
	"math"
	"slices"
)

// TDigest is a merging t-digest (Dunning & Ertl) for streaming quantile
// estimates in bounded memory. It is used once a Stream holds more values
// than its exact limit. Not safe for concurrent use.
type TDigest struct {
	compression float64
	cs          []centroid // sorted by mean, merged
	buf         []float64  // unmerged points
	total       float64
	min, max    float64
}

type centroid struct {
	mean, w float64
}

// NewTDigest returns a digest with the given compression (δ, default 200).
// Higher compression is more accurate and uses more memory (≈ δ centroids).
func NewTDigest(compression float64) *TDigest {
	if compression <= 0 {
		compression = 200
	}
	return &TDigest{compression: compression, min: math.Inf(1), max: math.Inf(-1)}
}

// Add records one value.
func (t *TDigest) Add(x float64) {
	if math.IsNaN(x) {
		return
	}
	t.buf = append(t.buf, x)
	t.total++
	t.min = math.Min(t.min, x)
	t.max = math.Max(t.max, x)
	if len(t.buf) >= int(t.compression)*8 {
		t.compress()
	}
}

// Count is the number of values added.
func (t *TDigest) Count() float64 { return t.total }

// Merge adds every centroid of o into t.
func (t *TDigest) Merge(o *TDigest) {
	o.compress()
	t.compress()
	if len(o.cs) == 0 {
		return
	}
	t.cs = append(t.cs, o.cs...)
	t.total += o.total
	t.min = math.Min(t.min, o.min)
	t.max = math.Max(t.max, o.max)
	slices.SortFunc(t.cs, func(a, b centroid) int { return cmpF(a.mean, b.mean) })
	t.mergeSorted(t.cs)
}

func cmpF(a, b float64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// k1 scale function and its inverse.
func (t *TDigest) k(q float64) float64 { return t.compression / (2 * math.Pi) * math.Asin(2*q-1) }
func (t *TDigest) kInv(k float64) float64 {
	return (math.Sin(2*math.Pi*k/t.compression) + 1) / 2
}

func (t *TDigest) qLimit(q0 float64) float64 {
	k := t.k(q0) + 1
	if k >= t.compression/4 {
		return 1
	}
	return t.kInv(k)
}

func (t *TDigest) compress() {
	if len(t.buf) == 0 {
		return
	}
	pts := make([]centroid, 0, len(t.cs)+len(t.buf))
	pts = append(pts, t.cs...)
	for _, x := range t.buf {
		pts = append(pts, centroid{mean: x, w: 1})
	}
	t.buf = t.buf[:0]
	slices.SortFunc(pts, func(a, b centroid) int { return cmpF(a.mean, b.mean) })
	t.mergeSorted(pts)
}

// mergeSorted rebuilds t.cs from sorted points under the size bound.
func (t *TDigest) mergeSorted(pts []centroid) {
	out := make([]centroid, 0, int(t.compression)*2)
	cur := pts[0]
	soFar := 0.0
	limit := t.qLimit(0)
	for _, p := range pts[1:] {
		if (soFar+cur.w+p.w)/t.total <= limit {
			w := cur.w + p.w
			cur.mean += (p.mean - cur.mean) * p.w / w
			cur.w = w
			continue
		}
		out = append(out, cur)
		soFar += cur.w
		limit = t.qLimit(soFar / t.total)
		cur = p
	}
	t.cs = append(out, cur)
}

// Quantile estimates the q-quantile (0 ≤ q ≤ 1).
func (t *TDigest) Quantile(q float64) float64 {
	t.compress()
	n := len(t.cs)
	switch {
	case n == 0:
		return math.NaN()
	case q <= 0:
		return t.min
	case q >= 1:
		return t.max
	case n == 1:
		return t.cs[0].mean
	}
	target := q * t.total
	first := t.cs[0]
	if target < first.w/2 {
		if first.w == 1 {
			return t.min
		}
		return t.min + (first.mean-t.min)*target/(first.w/2)
	}
	cum := 0.0
	for i := 0; i < n-1; i++ {
		a, b := t.cs[i], t.cs[i+1]
		ca := cum + a.w/2
		cb := cum + a.w + b.w/2
		if target <= cb {
			// Singletons are exact points.
			if a.w == 1 && target-ca < 0.5 {
				return a.mean
			}
			if b.w == 1 && cb-target <= 0.5 {
				return b.mean
			}
			return a.mean + (b.mean-a.mean)*(target-ca)/(cb-ca)
		}
		cum += a.w
	}
	last := t.cs[n-1]
	lc := t.total - last.w/2
	if last.w == 1 {
		return t.max
	}
	return last.mean + (t.max-last.mean)*(target-lc)/(last.w/2)
}

// points returns the centroids as weighted points (for histograms).
func (t *TDigest) points() []centroid {
	t.compress()
	return t.cs
}
