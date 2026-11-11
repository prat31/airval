package planner

import (
	"math"
	"math/rand/v2"
	"slices"
	"sort"
)

// candidate is one example eligible for a run.
type candidate struct {
	item Item
	tag  string // stratum (first tag, alphabetically; "" when untagged)
}

// sampleSize returns how many of total candidates to keep.
func sampleSize(s *Sampling, total int) int {
	n := total
	switch {
	case s.N > 0:
		n = s.N
	case s.Percent > 0:
		n = int(math.Ceil(s.Percent / 100 * float64(total)))
	}
	return max(0, min(n, total))
}

// sample deterministically selects candidates (stable for a given seed and input order).
// Random sampling draws uniformly; stratified sampling allocates the sample to strata in
// proportion to their size (largest remainder) and draws within each stratum. The result keeps
// the original order.
func sample(cands []candidate, s *Sampling, seed int64) []candidate {
	if !s.Active() {
		return cands
	}
	n := sampleSize(s, len(cands))
	if n >= len(cands) {
		return cands
	}
	rng := rand.New(rand.NewPCG(uint64(seed), 0x9e3779b97f4a7c15))
	var keep []int
	if s.Mode == "stratified" {
		keep = stratified(cands, n, rng)
	} else {
		keep = rng.Perm(len(cands))[:n]
	}
	sort.Ints(keep)
	out := make([]candidate, len(keep))
	for i, k := range keep {
		out[i] = cands[k]
	}
	return out
}

func stratified(cands []candidate, n int, rng *rand.Rand) []int {
	groups := map[string][]int{}
	var keys []string
	for i, c := range cands {
		if _, ok := groups[c.tag]; !ok {
			keys = append(keys, c.tag)
		}
		groups[c.tag] = append(groups[c.tag], i)
	}
	slices.Sort(keys)
	total := len(cands)
	type alloc struct {
		key  string
		n    int
		frac float64
	}
	allocs := make([]alloc, len(keys))
	used := 0
	for i, k := range keys {
		exact := float64(n) * float64(len(groups[k])) / float64(total)
		allocs[i] = alloc{key: k, n: int(exact), frac: exact - math.Floor(exact)}
		used += allocs[i].n
	}
	// Largest remainder; ties broken by key for determinism.
	order := make([]int, len(allocs))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool { return allocs[order[a]].frac > allocs[order[b]].frac })
	for i := 0; used < n && i < len(order); i++ {
		a := &allocs[order[i]]
		if a.n < len(groups[a.key]) {
			a.n++
			used++
		}
	}
	var keep []int
	for _, a := range allocs {
		g := groups[a.key]
		perm := rng.Perm(len(g))
		for _, p := range perm[:a.n] {
			keep = append(keep, g[p])
		}
	}
	return keep
}
