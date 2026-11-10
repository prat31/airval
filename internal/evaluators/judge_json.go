package evaluators

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var (
	fenceRe         = regexp.MustCompile("(?s)```[a-zA-Z0-9_-]*\\s*\\n?(.*?)```")
	trailingCommaRe = regexp.MustCompile(`,\s*([}\]])`)
	smartQuotes     = strings.NewReplacer("“", `"`, "”", `"`)
)

var errNoJSON = errors.New("no JSON object found")

// extractJSON finds the JSON object in a judge reply that contains key. It
// tolerates Markdown fences, prose before/after the object, trailing commas
// and typographic quotes. When several objects qualify, the last one wins
// (models put the final answer last).
func extractJSON(s, key string) (map[string]any, error) {
	s = strings.TrimSpace(s)
	if obj, ok := tryObject(s, key); ok {
		return obj, nil
	}
	if ms := fenceRe.FindAllStringSubmatch(s, -1); len(ms) > 0 {
		for i := len(ms) - 1; i >= 0; i-- {
			if obj, ok := tryObject(strings.TrimSpace(ms[i][1]), key); ok {
				return obj, nil
			}
		}
	}
	cands := braceCandidates(s)
	for i := len(cands) - 1; i >= 0; i-- {
		if obj, ok := tryObject(cands[i], key); ok {
			return obj, nil
		}
	}
	if len(cands) > 0 {
		return nil, fmt.Errorf("JSON object without %q field", key)
	}
	return nil, errNoJSON
}

func tryObject(s, key string) (map[string]any, bool) {
	if len(s) < 2 || s[0] != '{' {
		return nil, false
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(s), &obj); err != nil {
		fixed := trailingCommaRe.ReplaceAllString(smartQuotes.Replace(s), "$1")
		if fixed == s || json.Unmarshal([]byte(fixed), &obj) != nil {
			return nil, false
		}
	}
	if _, ok := obj[key]; !ok {
		return nil, false
	}
	return obj, true
}

// braceCandidates returns every balanced {...} substring (string-aware).
func braceCandidates(s string) []string {
	var out []string
	for i := 0; i < len(s); i++ {
		if s[i] != '{' {
			continue
		}
		depth, inStr, esc := 0, false, false
		for j := i; j < len(s); j++ {
			c := s[j]
			if inStr {
				switch {
				case esc:
					esc = false
				case c == '\\':
					esc = true
				case c == '"':
					inStr = false
				}
				continue
			}
			switch c {
			case '"':
				inStr = true
			case '{':
				depth++
			case '}':
				depth--
				if depth == 0 {
					out = append(out, s[i:j+1])
					j = len(s)
				}
			}
		}
	}
	return out
}

// parseScore reads a score field: a number, "4", "4/5" or "4 out of 5".
func parseScore(v any, lo, hi int) (float64, error) {
	var f float64
	switch t := v.(type) {
	case float64:
		f = t
	case string:
		s := strings.TrimSpace(t)
		if i := strings.IndexAny(s, "/ "); i > 0 {
			s = s[:i]
		}
		n, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return 0, fmt.Errorf("score %q is not a number", t)
		}
		f = n
	default:
		return 0, fmt.Errorf("score has unexpected type %T", v)
	}
	if math.IsNaN(f) || f < float64(lo) || f > float64(hi) {
		return 0, fmt.Errorf("score %v is outside the scale %d-%d", f, lo, hi)
	}
	return f, nil
}

func reasonOf(obj map[string]any) string {
	for _, k := range []string{"reason", "reasoning", "explanation", "rationale"} {
		if s, ok := obj[k].(string); ok {
			return strings.TrimSpace(s)
		}
	}
	return ""
}

// weightedScore computes the probability-weighted score from first-token
// logprobs over the integer score tokens of the scale (G-Eval). ok is false
// when no alternative is a score token.
func weightedScore(top map[string]float64, lo, hi int) (float64, bool) {
	if len(top) == 0 {
		return 0, false
	}
	keys := make([]string, 0, len(top))
	for k := range top {
		keys = append(keys, k)
	}
	sort.Strings(keys) // deterministic summation order
	var num, den float64
	for _, k := range keys {
		n, err := strconv.Atoi(strings.TrimSpace(k))
		if err != nil || n < lo || n > hi {
			continue
		}
		p := math.Exp(top[k])
		num += p * float64(n)
		den += p
	}
	if den <= 0 {
		return 0, false
	}
	return num / den, true
}

// normalizeVerdict maps a pairwise verdict to "A", "B" or "tie".
func normalizeVerdict(v any) (string, error) {
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("verdict has unexpected type %T", v)
	}
	switch strings.ToLower(strings.Trim(strings.TrimSpace(s), `"'[]().`)) {
	case "a", "response a", "response_a":
		return "A", nil
	case "b", "response b", "response_b":
		return "B", nil
	case "tie", "draw", "equal", "c":
		return "tie", nil
	}
	return "", fmt.Errorf("verdict %q is not A, B or tie", s)
}
