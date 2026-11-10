package evaluators

import (
	"context"
	"errors"
	"fmt"
	"math"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/airval/airval/internal/evalcore"
)

// funcEval adapts a closure to Evaluator.
type funcEval struct {
	kind string
	cost CostClass
	fn   func(ctx context.Context, in Input) (evalcore.Score, error)
}

func (f *funcEval) Kind() string    { return f.kind }
func (f *funcEval) Cost() CostClass { return f.cost }
func (f *funcEval) Evaluate(ctx context.Context, in Input) (evalcore.Score, error) {
	return f.fn(ctx, in)
}

func free(kind string, fn func(ctx context.Context, in Input) (evalcore.Score, error)) Evaluator {
	return &funcEval{kind: kind, cost: CostFree, fn: fn}
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	// Cut on a rune boundary.
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n] + "…"
}

const (
	groupMatch      = "Text match"
	groupSimilarity = "Similarity"
	groupStructure  = "Structure"
	groupPerf       = "Performance"
	groupTools      = "Tools & agents"
	groupSafety     = "Safety"
	groupQuality    = "Quality"
	groupRAG        = "RAG"
	groupConv       = "Conversation"
	groupCompare    = "Comparison"
	groupHuman      = "Human review"
)

// ---------------------------------------------------------------- equals

// EqualsConfig configures the equals evaluator.
type EqualsConfig struct {
	FieldSelector
	ExpectedSource
	NormalizeOptions
}

func (c *EqualsConfig) Validate() error {
	c.NormalizeOptions.defaults()
	if err := c.FieldSelector.validate(); err != nil {
		return err
	}
	return c.ExpectedSource.validate()
}

func isStructured(v any) bool {
	switch v.(type) {
	case map[string]any, []any:
		return true
	}
	return false
}

// jsonEqual compares generic JSON values; numbers compare by value.
func jsonEqual(a, b any) bool {
	switch x := a.(type) {
	case map[string]any:
		y, ok := b.(map[string]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for k, v := range x {
			w, ok := y[k]
			if !ok || !jsonEqual(v, w) {
				return false
			}
		}
		return true
	case []any:
		y, ok := b.([]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for i := range x {
			if !jsonEqual(x[i], y[i]) {
				return false
			}
		}
		return true
	case float64:
		y, ok := toFloat(b)
		return ok && x == y
	case int, int64, int32:
		xf, _ := toFloat(a)
		y, ok := toFloat(b)
		return ok && xf == y
	}
	return reflect.DeepEqual(a, b)
}

func toFloat(v any) (float64, bool) {
	switch t := v.(type) {
	case float64:
		return t, true
	case float32:
		return float64(t), true
	case int:
		return float64(t), true
	case int64:
		return float64(t), true
	case int32:
		return float64(t), true
	}
	return 0, false
}

func init() {
	register(KindInfo{
		Kind: "equals", Name: "Exact match", Category: CategoryDeterministic, Group: groupMatch, DefaultThreshold: 1,
		Requires:    []string{NeedsExpected},
		Description: "Passes when the output exactly matches the expected value. Optionally ignore case, extra whitespace or punctuation. Structured values are compared as JSON.",
	}, func(c *EqualsConfig, _ Deps) (Evaluator, error) {
		return free("equals", func(_ context.Context, in Input) (evalcore.Score, error) {
			exp, err := c.ExpectedSource.resolve(in)
			if err != nil {
				return evalcore.Score{}, err
			}
			got, err := c.FieldSelector.value(in)
			if err != nil {
				return boolScore(false, err.Error()), nil
			}
			if isStructured(exp) && isStructured(got) {
				if jsonEqual(normalizeMust(exp), got) {
					return boolScore(true, "output matches the expected JSON"), nil
				}
				return boolScore(false, "output JSON differs from the expected value"), nil
			}
			e, g := c.NormalizeOptions.apply(toText(exp)), c.NormalizeOptions.apply(toText(got))
			if e == g {
				return boolScore(true, "output matches the expected value"), nil
			}
			return boolScore(false, fmt.Sprintf("expected %q, got %q", trunc(e, 120), trunc(g, 120))), nil
		}), nil
	})
}

func normalizeMust(v any) any {
	n, err := normalizeJSON(v)
	if err != nil {
		return v
	}
	return n
}

// ---------------------------------------------------------------- contains

// ContainsConfig configures the contains evaluator.
type ContainsConfig struct {
	FieldSelector
	Values       []string `json:"values,omitempty" title:"Values" ui:"list" desc:"Substrings to look for. Leave empty to use the example's expected value (a string or a list of strings)."`
	Mode         string   `json:"mode,omitempty" title:"Mode" enum:"any|all|none" default:"any" desc:"any: pass if at least one value appears. all: every value must appear (score = fraction found). none: pass only if no value appears."`
	IgnoreCase   bool     `json:"ignore_case,omitempty" title:"Ignore case" desc:"Match case-insensitively."`
	ExpectedPath string   `json:"expected_path,omitempty" title:"Expected path" desc:"JSONPath into the example's structured expected value when values is empty."`

	src     ExpectedSource
	lowered []string
}

func (c *ContainsConfig) Validate() error {
	switch c.Mode {
	case "":
		c.Mode = "any"
	case "any", "all", "none":
	default:
		return fmt.Errorf("mode: must be any, all or none (got %q)", c.Mode)
	}
	if err := c.FieldSelector.validate(); err != nil {
		return err
	}
	c.src.ExpectedPath = c.ExpectedPath
	if len(c.Values) > 0 {
		c.lowered = make([]string, len(c.Values))
		for i, v := range c.Values {
			if c.IgnoreCase {
				v = strings.ToLower(v)
			}
			c.lowered[i] = v
		}
	}
	return c.src.validate()
}

func init() {
	register(KindInfo{
		Kind: "contains", Name: "Contains", Category: CategoryDeterministic, Group: groupMatch, DefaultThreshold: 1,
		Description: "Checks that the output includes (or avoids) given words or phrases. Choose whether any, all or none of them must appear.",
	}, func(c *ContainsConfig, _ Deps) (Evaluator, error) {
		return free("contains", func(_ context.Context, in Input) (evalcore.Score, error) {
			needles := c.lowered
			if needles == nil {
				list, err := c.src.resolveList(in)
				if err != nil {
					return evalcore.Score{}, err
				}
				if c.IgnoreCase {
					for i := range list {
						list[i] = strings.ToLower(list[i])
					}
				}
				needles = list
			}
			if len(needles) == 0 {
				return evalcore.Score{}, errors.New("no values to look for")
			}
			text, err := c.FieldSelector.text(in)
			if err != nil {
				return boolScore(false, err.Error()), nil
			}
			if c.IgnoreCase {
				text = strings.ToLower(text)
			}
			found := 0
			var missing, present []string
			for _, n := range needles {
				if strings.Contains(text, n) {
					found++
					present = append(present, n)
				} else {
					missing = append(missing, n)
				}
			}
			switch c.Mode {
			case "all":
				v := float64(found) / float64(len(needles))
				if found == len(needles) {
					return valueScore(1, 1, "all values found"), nil
				}
				return valueScore(v, 1, fmt.Sprintf("missing %d of %d: %s", len(missing), len(needles), quoteList(missing, 5))), nil
			case "none":
				if found == 0 {
					return boolScore(true, "none of the values appear"), nil
				}
				return boolScore(false, "found forbidden: "+quoteList(present, 5)), nil
			default:
				if found > 0 {
					return boolScore(true, "found "+quoteList(present, 5)), nil
				}
				return boolScore(false, "none of the values appear"), nil
			}
		}), nil
	})
}

func quoteList(xs []string, n int) string {
	var b strings.Builder
	for i, x := range xs {
		if i == n {
			fmt.Fprintf(&b, " (+%d more)", len(xs)-n)
			break
		}
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "%q", trunc(x, 60))
	}
	return b.String()
}

// ---------------------------------------------------------------- regex

// RegexConfig configures the regex evaluator.
type RegexConfig struct {
	FieldSelector
	Pattern    string `json:"pattern,omitempty" title:"Pattern" ui:"code" desc:"Regular expression (RE2 syntax). Leave empty to use each example's expected value as the pattern."`
	IgnoreCase bool   `json:"ignore_case,omitempty" title:"Ignore case" desc:"Match case-insensitively."`
	Multiline  bool   `json:"multiline,omitempty" title:"Multiline" desc:"^ and $ match at line boundaries."`
	DotAll     bool   `json:"dot_all,omitempty" title:"Dot matches newline" desc:". also matches newlines."`
	FullMatch  bool   `json:"full_match,omitempty" title:"Full match" desc:"The whole field must match, not just a part of it."`
	Invert     bool   `json:"invert,omitempty" title:"Must not match" desc:"Pass when the pattern does NOT match."`

	re *regexp.Regexp
}

func (c *RegexConfig) flags() string {
	f := ""
	if c.IgnoreCase {
		f += "i"
	}
	if c.Multiline {
		f += "m"
	}
	if c.DotAll {
		f += "s"
	}
	return f
}

func (c *RegexConfig) compile(p string) (*regexp.Regexp, error) {
	if c.FullMatch {
		p = `^(?:` + p + `)$`
	}
	if f := c.flags(); f != "" {
		p = "(?" + f + ")" + p
	}
	return regexp.Compile(p)
}

func (c *RegexConfig) Validate() error {
	if err := c.FieldSelector.validate(); err != nil {
		return err
	}
	if c.Pattern != "" {
		re, err := c.compile(c.Pattern)
		if err != nil {
			return fmt.Errorf("pattern: %w", err)
		}
		c.re = re
	}
	return nil
}

var dynRegexCache sync.Map // pattern+flags → *regexp.Regexp

func init() {
	register(KindInfo{
		Kind: "regex", Name: "Regex match", Category: CategoryDeterministic, Group: groupMatch, DefaultThreshold: 1,
		Description: "Passes when the output matches a regular expression — handy for formats like IDs, dates or required phrasing. Can also require that a pattern does not match.",
	}, func(c *RegexConfig, _ Deps) (Evaluator, error) {
		return free("regex", func(_ context.Context, in Input) (evalcore.Score, error) {
			re := c.re
			if re == nil {
				p, err := (&ExpectedSource{}).resolveText(in)
				if err != nil {
					return evalcore.Score{}, err
				}
				key := c.flags() + fmt.Sprint(c.FullMatch) + "\x00" + p
				if v, ok := dynRegexCache.Load(key); ok {
					re = v.(*regexp.Regexp)
				} else {
					if re, err = c.compile(p); err != nil {
						return evalcore.Score{}, fmt.Errorf("expected value is not a valid pattern: %w", err)
					}
					dynRegexCache.Store(key, re)
				}
			}
			text, err := c.FieldSelector.text(in)
			if err != nil {
				return boolScore(false, err.Error()), nil
			}
			m := re.MatchString(text)
			switch {
			case m && !c.Invert:
				return boolScore(true, "pattern matches"), nil
			case !m && c.Invert:
				return boolScore(true, "pattern does not match, as required"), nil
			case m:
				return boolScore(false, fmt.Sprintf("forbidden pattern matched %q", trunc(re.FindString(text), 80))), nil
			default:
				return boolScore(false, "pattern does not match"), nil
			}
		}), nil
	})
}

// ---------------------------------------------------------------- starts_with / ends_with

// AffixConfig configures starts_with and ends_with.
type AffixConfig struct {
	FieldSelector
	Value string `json:"value,omitempty" title:"Value" desc:"Required prefix/suffix. Leave empty to use the example's expected value."`
	NormalizeOptions
}

func (c *AffixConfig) Validate() error {
	c.NormalizeOptions.defaults()
	return c.FieldSelector.validate()
}

func init() {
	for _, k := range []struct {
		kind, name, verb, desc string
		fn                     func(s, affix string) bool
	}{
		{"starts_with", "Starts with", "start", "Passes when the output begins with the given text.", strings.HasPrefix},
		{"ends_with", "Ends with", "end", "Passes when the output ends with the given text.", strings.HasSuffix},
	} {
		register(KindInfo{
			Kind: k.kind, Name: k.name, Category: CategoryDeterministic, Group: groupMatch, DefaultThreshold: 1,
			Description: k.desc,
		}, func(c *AffixConfig, _ Deps) (Evaluator, error) {
			want := c.NormalizeOptions.apply(c.Value)
			return free(k.kind, func(_ context.Context, in Input) (evalcore.Score, error) {
				w := want
				if c.Value == "" {
					e, err := (&ExpectedSource{}).resolveText(in)
					if err != nil {
						return evalcore.Score{}, err
					}
					w = c.NormalizeOptions.apply(e)
				}
				text, err := c.FieldSelector.text(in)
				if err != nil {
					return boolScore(false, err.Error()), nil
				}
				if k.fn(c.NormalizeOptions.apply(text), w) {
					return boolScore(true, fmt.Sprintf("%ss with %q", k.verb, trunc(w, 80))), nil
				}
				return boolScore(false, fmt.Sprintf("does not %s with %q", k.verb, trunc(w, 80))), nil
			}), nil
		})
	}
}

// ---------------------------------------------------------------- levenshtein

// SimilarityConfig configures levenshtein.
type SimilarityConfig struct {
	FieldSelector
	ExpectedSource
	NormalizeOptions
}

func (c *SimilarityConfig) Validate() error {
	c.NormalizeOptions.defaults()
	if err := c.FieldSelector.validate(); err != nil {
		return err
	}
	return c.ExpectedSource.validate()
}

func init() {
	register(KindInfo{
		Kind: "levenshtein", Name: "Edit similarity", Category: CategoryDeterministic, Group: groupSimilarity, DefaultThreshold: 0.8,
		Requires:    []string{NeedsExpected},
		Description: "Character-level similarity to the expected output (1 = identical, 0 = completely different), based on Levenshtein edit distance. Tolerates typos and small wording changes. With several expected values, the best match counts.",
	}, func(c *SimilarityConfig, _ Deps) (Evaluator, error) {
		return free("levenshtein", func(_ context.Context, in Input) (evalcore.Score, error) {
			refs, err := c.ExpectedSource.resolveList(in)
			if err != nil {
				return evalcore.Score{}, err
			}
			text, err := c.FieldSelector.text(in)
			if err != nil {
				return valueScore(0, 0.8, err.Error()), nil
			}
			text = c.NormalizeOptions.apply(text)
			best := 0.0
			for _, r := range refs {
				if s := levenshteinSimilarity(text, c.NormalizeOptions.apply(r)); s > best {
					best = s
				}
			}
			return valueScore(best, 0.8, fmt.Sprintf("edit similarity %.3f", best)), nil
		}), nil
	})
}

// ---------------------------------------------------------------- rouge_l

// RougeConfig configures rouge_l.
type RougeConfig struct {
	FieldSelector
	ExpectedSource
	Metric string `json:"metric,omitempty" title:"Metric" enum:"f1|precision|recall" default:"f1" desc:"f1 balances both; recall rewards covering the reference; precision penalizes extra content."`
}

func (c *RougeConfig) Validate() error {
	switch c.Metric {
	case "":
		c.Metric = "f1"
	case "f1", "precision", "recall":
	default:
		return fmt.Errorf("metric: must be f1, precision or recall (got %q)", c.Metric)
	}
	if err := c.FieldSelector.validate(); err != nil {
		return err
	}
	return c.ExpectedSource.validate()
}

func init() {
	register(KindInfo{
		Kind: "rouge_l", Name: "ROUGE-L", Category: CategoryDeterministic, Group: groupSimilarity, DefaultThreshold: 0.5,
		Requires:    []string{NeedsExpected},
		Description: "Word-overlap score based on the longest common subsequence with the expected output. A standard metric for summaries and free-form answers.",
	}, func(c *RougeConfig, _ Deps) (Evaluator, error) {
		return free("rouge_l", func(_ context.Context, in Input) (evalcore.Score, error) {
			refs, err := c.ExpectedSource.resolveList(in)
			if err != nil {
				return evalcore.Score{}, err
			}
			text, err := c.FieldSelector.text(in)
			if err != nil {
				return valueScore(0, 0.5, err.Error()), nil
			}
			cand := tokenize(text)
			best, bp, br := 0.0, 0.0, 0.0
			for _, r := range refs {
				p, rc, f := rougeL(cand, tokenize(r))
				v := f
				switch c.Metric {
				case "precision":
					v = p
				case "recall":
					v = rc
				}
				if v >= best {
					best, bp, br = v, p, rc
				}
			}
			return valueScore(best, 0.5, fmt.Sprintf("ROUGE-L %s %.3f (precision %.3f, recall %.3f)", c.Metric, best, bp, br)), nil
		}), nil
	})
}

// ---------------------------------------------------------------- bleu

// BLEUConfig configures bleu.
type BLEUConfig struct {
	FieldSelector
	ExpectedSource
	MaxN    int     `json:"max_n,omitempty" title:"Max n-gram" default:"4" min:"1" max:"4" desc:"Longest n-gram to count (BLEU-4 by default)."`
	Epsilon float64 `json:"epsilon,omitempty" title:"Smoothing epsilon" default:"0.1" min:"0" max:"1" desc:"Smoothing for n-gram orders with no matches, so one missing order doesn't zero the score."`
}

func (c *BLEUConfig) Validate() error {
	if c.MaxN == 0 {
		c.MaxN = 4
	}
	if c.MaxN < 1 || c.MaxN > 4 {
		return fmt.Errorf("max_n: must be between 1 and 4 (got %d)", c.MaxN)
	}
	if c.Epsilon == 0 {
		c.Epsilon = 0.1
	}
	if c.Epsilon < 0 || c.Epsilon > 1 {
		return fmt.Errorf("epsilon: must be between 0 and 1")
	}
	if err := c.FieldSelector.validate(); err != nil {
		return err
	}
	return c.ExpectedSource.validate()
}

func init() {
	register(KindInfo{
		Kind: "bleu", Name: "BLEU", Category: CategoryDeterministic, Group: groupSimilarity, DefaultThreshold: 0.3,
		Requires:    []string{NeedsExpected},
		Description: "Sentence-level BLEU: n-gram precision against one or more expected outputs, with a brevity penalty and smoothing. Common for translation-style tasks.",
	}, func(c *BLEUConfig, _ Deps) (Evaluator, error) {
		return free("bleu", func(_ context.Context, in Input) (evalcore.Score, error) {
			refs, err := c.ExpectedSource.resolveList(in)
			if err != nil {
				return evalcore.Score{}, err
			}
			text, err := c.FieldSelector.text(in)
			if err != nil {
				return valueScore(0, 0.3, err.Error()), nil
			}
			rt := make([][]string, len(refs))
			for i, r := range refs {
				rt[i] = tokenize(r)
			}
			v := sentenceBLEU(tokenize(text), rt, c.MaxN, c.Epsilon)
			return valueScore(v, 0.3, fmt.Sprintf("BLEU-%d %.3f", c.MaxN, v)), nil
		}), nil
	})
}

// ---------------------------------------------------------------- length

// LengthConfig configures length.
type LengthConfig struct {
	FieldSelector
	Unit string `json:"unit,omitempty" title:"Unit" enum:"chars|words|tokens" default:"chars" desc:"Count characters, words, or approximate tokens (about 4 characters per token)."`
	Min  *int   `json:"min,omitempty" title:"Minimum" min:"0" desc:"Fail if shorter than this."`
	Max  *int   `json:"max,omitempty" title:"Maximum" min:"0" desc:"Fail if longer than this."`
}

func (c *LengthConfig) Validate() error {
	switch c.Unit {
	case "":
		c.Unit = "chars"
	case "chars", "words", "tokens":
	default:
		return fmt.Errorf("unit: must be chars, words or tokens (got %q)", c.Unit)
	}
	if c.Min == nil && c.Max == nil {
		return errors.New("set min, max or both")
	}
	if c.Min != nil && c.Max != nil && *c.Min > *c.Max {
		return errors.New("min must not exceed max")
	}
	return c.FieldSelector.validate()
}

func countWords(s string) int {
	n, in := 0, false
	for _, r := range s {
		sp := r == ' ' || r == '\n' || r == '\t' || r == '\r' || r == '\f' || r == '\v' || r == 0x85 || r == 0xA0 || r == 0x3000
		if !sp && !in {
			n++
		}
		in = !sp
	}
	return n
}

// approxTokens estimates BPE tokens: ~4 characters per token, never fewer
// than the number of words.
func approxTokens(s string) int {
	c := utf8.RuneCountInString(s)
	t := int(math.Ceil(float64(c) / 4))
	if w := countWords(s); w > t {
		t = w
	}
	return t
}

func init() {
	register(KindInfo{
		Kind: "length", Name: "Length", Category: CategoryDeterministic, Group: groupStructure, DefaultThreshold: 1,
		Description: "Passes when the output length is within bounds, counted in characters, words or approximate tokens.",
	}, func(c *LengthConfig, _ Deps) (Evaluator, error) {
		return free("length", func(_ context.Context, in Input) (evalcore.Score, error) {
			text, err := c.FieldSelector.text(in)
			if err != nil {
				return boolScore(false, err.Error()), nil
			}
			var n int
			switch c.Unit {
			case "words":
				n = countWords(text)
			case "tokens":
				n = approxTokens(text)
			default:
				n = utf8.RuneCountInString(text)
			}
			s := evalcore.Score{}
			switch {
			case c.Min != nil && n < *c.Min:
				s = boolScore(false, fmt.Sprintf("%d %s, below the minimum of %d", n, c.Unit, *c.Min))
			case c.Max != nil && n > *c.Max:
				s = boolScore(false, fmt.Sprintf("%d %s, above the maximum of %d", n, c.Unit, *c.Max))
			default:
				s = boolScore(true, fmt.Sprintf("%d %s", n, c.Unit))
			}
			s.Meta = map[string]any{"length": n, "unit": c.Unit}
			return s, nil
		}), nil
	})
}
