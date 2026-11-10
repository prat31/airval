package evaluators

import (
	"math"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"
)

// NormalizeOptions controls text normalization before comparison.
type NormalizeOptions struct {
	Trim               *bool `json:"trim,omitempty" title:"Trim" default:"true" desc:"Ignore leading and trailing whitespace."`
	IgnoreCase         bool  `json:"ignore_case,omitempty" title:"Ignore case" desc:"Compare case-insensitively."`
	CollapseWhitespace bool  `json:"collapse_whitespace,omitempty" title:"Collapse whitespace" desc:"Treat any run of spaces, tabs and newlines as a single space."`
	IgnorePunctuation  bool  `json:"ignore_punctuation,omitempty" title:"Ignore punctuation" desc:"Drop punctuation and symbols before comparing."`
}

func (n *NormalizeOptions) defaults() {
	if n.Trim == nil {
		n.Trim = boolp(true)
	}
}

func (n *NormalizeOptions) apply(s string) string {
	trim := n.Trim == nil || *n.Trim
	if !n.IgnoreCase && !n.CollapseWhitespace && !n.IgnorePunctuation {
		if trim {
			return strings.TrimSpace(s)
		}
		return s
	}
	if n.IgnoreCase && !n.CollapseWhitespace && !n.IgnorePunctuation {
		if trim {
			s = strings.TrimSpace(s)
		}
		return strings.ToLower(s)
	}
	var b strings.Builder
	b.Grow(len(s))
	lastSpace := false
	for _, r := range s {
		if n.IgnorePunctuation && (unicode.IsPunct(r) || unicode.IsSymbol(r)) {
			continue
		}
		if unicode.IsSpace(r) {
			if n.CollapseWhitespace {
				if lastSpace {
					continue
				}
				r = ' '
			}
			lastSpace = true
		} else {
			lastSpace = false
		}
		if n.IgnoreCase {
			r = unicode.ToLower(r)
		}
		b.WriteRune(r)
	}
	out := b.String()
	if trim {
		out = strings.TrimSpace(out)
	}
	return out
}

// tokenize lowercases s and splits it into word tokens (letters, digits and
// intra-word apostrophes).
func tokenize(s string) []string {
	toks := make([]string, 0, len(s)/5+1)
	start := -1
	lower := !isLowerASCII(s)
	if lower {
		s = strings.ToLower(s)
	}
	for i, r := range s {
		word := unicode.IsLetter(r) || unicode.IsDigit(r) || (r == '\'' && start >= 0)
		if word {
			if start < 0 {
				start = i
			}
			continue
		}
		if start >= 0 {
			toks = append(toks, strings.TrimRight(s[start:i], "'"))
			start = -1
		}
	}
	if start >= 0 {
		toks = append(toks, strings.TrimRight(s[start:], "'"))
	}
	return toks
}

func isLowerASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= utf8.RuneSelf || (c >= 'A' && c <= 'Z') {
			return false
		}
	}
	return true
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}

var intBufPool = sync.Pool{New: func() any { b := make([]int, 0, 256); return &b }}

func getInts(n int) (*[]int, []int) {
	p := intBufPool.Get().(*[]int)
	if cap(*p) < n {
		*p = make([]int, n)
	}
	return p, (*p)[:n]
}

// levenshteinDistance is the rune-level edit distance between a and b, using
// two pooled rows (O(min(len)) memory, no allocation in steady state for
// ASCII input).
func levenshteinDistance(a, b string) int {
	if a == b {
		return 0
	}
	if isASCII(a) && isASCII(b) {
		return levenshteinSeq(a, b, func(s string, i int) rune { return rune(s[i]) }, len(a), len(b))
	}
	ra, rb := []rune(a), []rune(b)
	return levenshteinSeq(ra, rb, func(s []rune, i int) rune { return s[i] }, len(ra), len(rb))
}

func levenshteinSeq[S any](a, b S, at func(S, int) rune, la, lb int) int {
	if la == 0 {
		return lb
	}
	if lb == 0 {
		return la
	}
	if lb > la { // keep rows short
		a, b = b, a
		la, lb = lb, la
	}
	p, buf := getInts(2 * (lb + 1))
	defer intBufPool.Put(p)
	prev, cur := buf[:lb+1], buf[lb+1:]
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= la; i++ {
		cur[0] = i
		ca := at(a, i-1)
		for j := 1; j <= lb; j++ {
			cost := 1
			if ca == at(b, j-1) {
				cost = 0
			}
			v := prev[j-1] + cost
			if d := prev[j] + 1; d < v {
				v = d
			}
			if ins := cur[j-1] + 1; ins < v {
				v = ins
			}
			cur[j] = v
		}
		prev, cur = cur, prev
	}
	return prev[lb]
}

// levenshteinSimilarity is 1 - distance/max(len), in [0,1].
func levenshteinSimilarity(a, b string) float64 {
	la, lb := utf8.RuneCountInString(a), utf8.RuneCountInString(b)
	m := max(la, lb)
	if m == 0 {
		return 1
	}
	return 1 - float64(levenshteinDistance(a, b))/float64(m)
}

// lcsLen is the longest-common-subsequence length of two token slices.
func lcsLen(a, b []string) int {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	if len(b) > len(a) {
		a, b = b, a
	}
	p, buf := getInts(2 * (len(b) + 1))
	defer intBufPool.Put(p)
	prev, cur := buf[:len(b)+1], buf[len(b)+1:]
	clear(prev)
	for i := 1; i <= len(a); i++ {
		cur[0] = 0
		for j := 1; j <= len(b); j++ {
			if a[i-1] == b[j-1] {
				cur[j] = prev[j-1] + 1
			} else if prev[j] >= cur[j-1] {
				cur[j] = prev[j]
			} else {
				cur[j] = cur[j-1]
			}
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}

// rougeL returns ROUGE-L precision, recall and F1 of candidate vs reference
// tokens.
func rougeL(cand, ref []string) (p, r, f float64) {
	if len(cand) == 0 || len(ref) == 0 {
		if len(cand) == 0 && len(ref) == 0 {
			return 1, 1, 1
		}
		return 0, 0, 0
	}
	l := float64(lcsLen(cand, ref))
	p = l / float64(len(cand))
	r = l / float64(len(ref))
	if p+r == 0 {
		return p, r, 0
	}
	return p, r, 2 * p * r / (p + r)
}

// sentenceBLEU computes BLEU-N of cand against one or more references with
// uniform weights, the closest-reference brevity penalty, and smoothing
// method 1 of Chen & Cherry (2014): zero n-gram matches count as epsilon.
func sentenceBLEU(cand []string, refs [][]string, maxN int, epsilon float64) float64 {
	if len(cand) == 0 {
		return 0
	}
	if maxN <= 0 {
		maxN = 4
	}
	logSum := 0.0
	for n := 1; n <= maxN; n++ {
		total := len(cand) - n + 1
		if total <= 0 {
			// Candidate shorter than n: treat as zero matches, smoothed.
			logSum += math.Log(epsilon / 1)
			continue
		}
		candCounts := ngramCounts(cand, n)
		maxRef := map[string]int{}
		for _, ref := range refs {
			for g, c := range ngramCounts(ref, n) {
				if c > maxRef[g] {
					maxRef[g] = c
				}
			}
		}
		match := 0
		for g, c := range candCounts {
			match += min(c, maxRef[g])
		}
		num := float64(match)
		if match == 0 {
			num = epsilon
		}
		logSum += math.Log(num / float64(total))
	}
	// Brevity penalty against the reference closest in length (ties → shorter).
	c := len(cand)
	r := -1
	for _, ref := range refs {
		l := len(ref)
		if r < 0 || abs(l-c) < abs(r-c) || (abs(l-c) == abs(r-c) && l < r) {
			r = l
		}
	}
	bp := 1.0
	if c < r {
		bp = math.Exp(1 - float64(r)/float64(c))
	}
	return bp * math.Exp(logSum/float64(maxN))
}

func ngramCounts(toks []string, n int) map[string]int {
	m := make(map[string]int, len(toks))
	if n == 1 {
		for _, t := range toks {
			m[t]++
		}
		return m
	}
	var b strings.Builder
	for i := 0; i+n <= len(toks); i++ {
		b.Reset()
		for k := 0; k < n; k++ {
			if k > 0 {
				b.WriteByte(0)
			}
			b.WriteString(toks[i+k])
		}
		m[b.String()]++
	}
	return m
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
