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
	if isASCII(s) {
		return n.applyASCII(s, trim)
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

func (n *NormalizeOptions) applyASCII(s string, trim bool) string {
	buf := make([]byte, 0, len(s))
	lastSpace := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if n.IgnorePunctuation && (c >= '!' && c <= '/' || c >= ':' && c <= '@' || c >= '[' && c <= '`' || c >= '{' && c <= '~') {
			continue
		}
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\v' || c == '\f' {
			if n.CollapseWhitespace {
				if lastSpace {
					continue
				}
				c = ' '
			}
			lastSpace = true
		} else {
			lastSpace = false
		}
		if n.IgnoreCase && c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		buf = append(buf, c)
	}
	out := string(buf)
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

// levenshteinDistance is the rune-level edit distance between a and b. It
// strips the common prefix/suffix and uses two pooled rows (O(min(len))
// memory).
func levenshteinDistance(a, b string) int {
	if a == b {
		return 0
	}
	if isASCII(a) && isASCII(b) {
		return levenshteinSeq([]byte(a), []byte(b))
	}
	return levenshteinSeq([]rune(a), []rune(b))
}

func levenshteinSeq[E comparable](a, b []E) int {
	for len(a) > 0 && len(b) > 0 && a[0] == b[0] {
		a, b = a[1:], b[1:]
	}
	for len(a) > 0 && len(b) > 0 && a[len(a)-1] == b[len(b)-1] {
		a, b = a[:len(a)-1], b[:len(b)-1]
	}
	if len(a) == 0 {
		return len(b)
	}
	if len(b) == 0 {
		return len(a)
	}
	if len(b) > len(a) { // keep rows short
		a, b = b, a
	}
	lb := len(b)
	p, buf := getInts(2 * (lb + 1))
	defer intBufPool.Put(p)
	prev, cur := buf[:lb+1], buf[lb+1:2*(lb+1)]
	for j := range prev {
		prev[j] = j
	}
	for i, ca := range a {
		cur[0] = i + 1
		for j, cb := range b {
			v := prev[j]
			if ca != cb {
				v++
			}
			if d := prev[j+1] + 1; d < v {
				v = d
			}
			if ins := cur[j] + 1; ins < v {
				v = ins
			}
			cur[j+1] = v
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
	vocab := make(map[string]uint64, len(cand)*2)
	candIDs, ok := tokenIDs(vocab, cand)
	refIDs := make([][]uint64, len(refs))
	for i, r := range refs {
		var ok2 bool
		refIDs[i], ok2 = tokenIDs(vocab, r)
		ok = ok && ok2
	}
	if !ok {
		// Pathologically large vocabulary: truncate so the total token count
		// (an upper bound on the vocabulary) fits in 16-bit ids.
		limit := 60000 / (len(refs) + 1)
		trimmed := make([][]string, len(refs))
		for i, r := range refs {
			trimmed[i] = r[:min(len(r), limit)]
		}
		return sentenceBLEU(cand[:min(len(cand), limit)], trimmed, maxN, epsilon)
	}
	logSum := 0.0
	for n := 1; n <= maxN; n++ {
		total := len(cand) - n + 1
		if total <= 0 {
			// Candidate shorter than n: treat as zero matches, smoothed.
			logSum += math.Log(epsilon / 1)
			continue
		}
		candCounts := ngramCounts(candIDs, n)
		maxRef := map[uint64]int{}
		for _, ref := range refIDs {
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

// ngramCounts counts n-grams (n <= 4) of token ids, packed 16 bits per id.
func ngramCounts(ids []uint64, n int) map[uint64]int {
	m := make(map[uint64]int, len(ids))
	for i := 0; i+n <= len(ids); i++ {
		var k uint64
		for j := 0; j < n; j++ {
			k = k<<16 | ids[i+j]
		}
		m[k]++
	}
	return m
}

// tokenIDs interns tokens; ids start at 1 so packed n-grams never collide
// with shorter ones. ok is false when the vocabulary exceeds 16 bits.
func tokenIDs(vocab map[string]uint64, toks []string) ([]uint64, bool) {
	out := make([]uint64, len(toks))
	for i, t := range toks {
		id, ok := vocab[t]
		if !ok {
			id = uint64(len(vocab) + 1)
			if id >= 1<<16 {
				return nil, false
			}
			vocab[t] = id
		}
		out[i] = id
	}
	return out, true
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
