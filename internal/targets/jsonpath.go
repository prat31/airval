package targets

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// Path is a compiled JSONPath expression supporting the subset used by
// response mappings:
//
//	$            root
//	.name  ['name']  ["name"]   child
//	.*  [*]                     all children
//	[0]  [-1]                   array index (negative counts from the end)
//	[1:3]  [:2]  [-2:]          array slice
//	..name  ..*                 recursive descent
//	[?(@.k == 'v')]             filter (==, !=, <, <=, >, >=, or bare existence @.k)
//
// Paths are compiled once and evaluated against documents decoded with
// encoding/json into any.
type Path struct {
	src    string
	steps  []pstep
	single bool // no wildcard/filter/slice/descent: at most one result
}

type pstepKind uint8

const (
	pChild pstepKind = iota
	pIndex
	pWild
	pSlice
	pFilter
)

type pstep struct {
	kind      pstepKind
	recursive bool
	name      string
	idx       int
	lo, hi    *int
	filter    *pfilter
}

type pfilter struct {
	path *Path // relative path rooted at @
	op   string
	val  any
}

// CompilePath compiles a JSONPath expression.
func CompilePath(src string) (*Path, error) {
	p := &Path{src: src, single: true}
	s := strings.TrimSpace(src)
	if s == "" {
		return nil, fmt.Errorf("empty JSONPath")
	}
	i := 0
	if s[0] == '$' || s[0] == '@' {
		i = 1
	} else if s[0] != '.' && s[0] != '[' {
		s = "." + s // allow "a.b"
	}
	for i < len(s) {
		rec := false
		switch s[i] {
		case '.':
			if i+1 < len(s) && s[i+1] == '.' {
				rec = true
				i += 2
			} else {
				i++
			}
			if i >= len(s) {
				return nil, fmt.Errorf("jsonpath %q: dangling '.'", src)
			}
			if s[i] == '[' { // "..[0]" style
				st, n, err := parseBracket(s[i:])
				if err != nil {
					return nil, fmt.Errorf("jsonpath %q: %w", src, err)
				}
				st.recursive = rec
				p.add(st)
				i += n
				continue
			}
			if s[i] == '*' {
				p.add(pstep{kind: pWild, recursive: rec})
				i++
				continue
			}
			j := i
			for j < len(s) && s[j] != '.' && s[j] != '[' {
				j++
			}
			if j == i {
				return nil, fmt.Errorf("jsonpath %q: empty name at %d", src, i)
			}
			p.add(pstep{kind: pChild, name: s[i:j], recursive: rec})
			i = j
		case '[':
			st, n, err := parseBracket(s[i:])
			if err != nil {
				return nil, fmt.Errorf("jsonpath %q: %w", src, err)
			}
			p.add(st)
			i += n
		default:
			return nil, fmt.Errorf("jsonpath %q: unexpected %q at %d", src, s[i], i)
		}
	}
	return p, nil
}

// MustCompilePath panics on error (for tests and constants).
func MustCompilePath(src string) *Path {
	p, err := CompilePath(src)
	if err != nil {
		panic(err)
	}
	return p
}

func (p *Path) add(st pstep) {
	if st.recursive || st.kind == pWild || st.kind == pSlice || st.kind == pFilter {
		p.single = false
	}
	p.steps = append(p.steps, st)
}

func (p *Path) String() string { return p.src }

func parseBracket(s string) (pstep, int, error) {
	// s starts with '['; find matching ']' respecting quotes and parens.
	depth, quote := 0, byte(0)
	end := -1
loop:
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case quote != 0:
			if c == '\\' {
				i++
			} else if c == quote {
				quote = 0
			}
		case c == '\'' || c == '"':
			quote = c
		case c == '[' || c == '(':
			depth++
		case c == ']' || c == ')':
			depth--
			if depth == 0 {
				end = i
				break loop
			}
		}
	}
	if end < 0 {
		return pstep{}, 0, fmt.Errorf("unterminated '['")
	}
	inner := strings.TrimSpace(s[1:end])
	n := end + 1
	switch {
	case inner == "*":
		return pstep{kind: pWild}, n, nil
	case strings.HasPrefix(inner, "?"):
		f, err := parseFilter(strings.TrimSpace(inner[1:]))
		if err != nil {
			return pstep{}, 0, err
		}
		return pstep{kind: pFilter, filter: f}, n, nil
	case len(inner) >= 2 && (inner[0] == '\'' || inner[0] == '"') && inner[len(inner)-1] == inner[0]:
		return pstep{kind: pChild, name: unquote(inner)}, n, nil
	case strings.Contains(inner, ":"):
		parts := strings.SplitN(inner, ":", 2)
		st := pstep{kind: pSlice}
		if a := strings.TrimSpace(parts[0]); a != "" {
			v, err := strconv.Atoi(a)
			if err != nil {
				return pstep{}, 0, fmt.Errorf("bad slice %q", inner)
			}
			st.lo = &v
		}
		if b := strings.TrimSpace(parts[1]); b != "" {
			v, err := strconv.Atoi(b)
			if err != nil {
				return pstep{}, 0, fmt.Errorf("bad slice %q", inner)
			}
			st.hi = &v
		}
		return st, n, nil
	default:
		v, err := strconv.Atoi(inner)
		if err != nil {
			return pstep{}, 0, fmt.Errorf("bad index %q", inner)
		}
		return pstep{kind: pIndex, idx: v}, n, nil
	}
}

func unquote(s string) string {
	q := s[0]
	body := s[1 : len(s)-1]
	if !strings.Contains(body, "\\") {
		return body
	}
	var b strings.Builder
	for i := 0; i < len(body); i++ {
		if body[i] == '\\' && i+1 < len(body) {
			i++
		}
		b.WriteByte(body[i])
	}
	_ = q
	return b.String()
}

func parseFilter(s string) (*pfilter, error) {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "(") && strings.HasSuffix(s, ")") {
		s = strings.TrimSpace(s[1 : len(s)-1])
	}
	for _, op := range []string{"==", "!=", "<=", ">=", "<", ">"} {
		if idx := indexOutsideQuotes(s, op); idx > 0 {
			lhs := strings.TrimSpace(s[:idx])
			rhs := strings.TrimSpace(s[idx+len(op):])
			path, err := CompilePath(lhs)
			if err != nil {
				return nil, err
			}
			var val any
			if len(rhs) >= 2 && (rhs[0] == '\'' || rhs[0] == '"') && rhs[len(rhs)-1] == rhs[0] {
				val = unquote(rhs)
			} else if err := json.Unmarshal([]byte(rhs), &val); err != nil {
				return nil, fmt.Errorf("bad filter literal %q", rhs)
			}
			return &pfilter{path: path, op: op, val: val}, nil
		}
	}
	path, err := CompilePath(s)
	if err != nil {
		return nil, err
	}
	return &pfilter{path: path}, nil
}

func indexOutsideQuotes(s, op string) int {
	quote := byte(0)
	for i := 0; i+len(op) <= len(s); i++ {
		c := s[i]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '\'' || c == '"':
			quote = c
		case s[i:i+len(op)] == op:
			return i
		}
	}
	return -1
}

func (f *pfilter) match(node any) bool {
	v, ok := f.path.First(node)
	if f.op == "" {
		return ok
	}
	if !ok {
		return f.op == "!="
	}
	switch f.op {
	case "==":
		return looseEqual(v, f.val)
	case "!=":
		return !looseEqual(v, f.val)
	}
	if a, ok := v.(float64); ok {
		if b, ok := f.val.(float64); ok {
			return cmpOp(f.op, a, b)
		}
	}
	if a, ok := v.(string); ok {
		if b, ok := f.val.(string); ok {
			return cmpOp(f.op, a, b)
		}
	}
	return false
}

func cmpOp[T float64 | string](op string, a, b T) bool {
	switch op {
	case "<":
		return a < b
	case "<=":
		return a <= b
	case ">":
		return a > b
	case ">=":
		return a >= b
	}
	return false
}

func looseEqual(a, b any) bool {
	switch x := a.(type) {
	case string:
		y, ok := b.(string)
		return ok && x == y
	case float64:
		y, ok := b.(float64)
		return ok && x == y
	case bool:
		y, ok := b.(bool)
		return ok && x == y
	case nil:
		return b == nil
	}
	return false
}

func index(arr []any, i int) (any, bool) {
	if i < 0 {
		i += len(arr)
	}
	if i < 0 || i >= len(arr) {
		return nil, false
	}
	return arr[i], true
}

// First returns the first match without allocating for single-valued paths.
func (p *Path) First(doc any) (any, bool) {
	if p.single {
		cur := doc
		for i := range p.steps {
			st := &p.steps[i]
			switch st.kind {
			case pChild:
				m, ok := cur.(map[string]any)
				if !ok {
					return nil, false
				}
				if cur, ok = m[st.name]; !ok {
					return nil, false
				}
			case pIndex:
				a, ok := cur.([]any)
				if !ok {
					return nil, false
				}
				if cur, ok = index(a, st.idx); !ok {
					return nil, false
				}
			}
		}
		return cur, true
	}
	r := p.Eval(doc)
	if len(r) == 0 {
		return nil, false
	}
	return r[0], true
}

// Eval returns all matches in document order.
func (p *Path) Eval(doc any) []any {
	cur := []any{doc}
	for i := range p.steps {
		st := &p.steps[i]
		next := make([]any, 0, len(cur))
		for _, n := range cur {
			if st.recursive {
				walkDescend(n, func(d any) { next = applyStep(st, d, next) })
			} else {
				next = applyStep(st, n, next)
			}
		}
		cur = next
		if len(cur) == 0 {
			return nil
		}
	}
	return cur
}

// walkDescend visits n and all of its descendants.
func walkDescend(n any, fn func(any)) {
	fn(n)
	switch v := n.(type) {
	case map[string]any:
		// Sorted, like the wildcard and filter steps, so `$..x` results don't depend on map order.
		for _, k := range sortedKeys(v) {
			walkDescend(v[k], fn)
		}
	case []any:
		for _, c := range v {
			walkDescend(c, fn)
		}
	}
}

func applyStep(st *pstep, n any, out []any) []any {
	switch st.kind {
	case pChild:
		if m, ok := n.(map[string]any); ok {
			if v, ok := m[st.name]; ok {
				out = append(out, v)
			}
		}
	case pIndex:
		if a, ok := n.([]any); ok {
			if v, ok := index(a, st.idx); ok {
				out = append(out, v)
			}
		}
	case pWild:
		switch v := n.(type) {
		case []any:
			out = append(out, v...)
		case map[string]any:
			// Deterministic order is not guaranteed by Go maps; sort keys for stable output.
			for _, k := range sortedKeys(v) {
				out = append(out, v[k])
			}
		}
	case pSlice:
		if a, ok := n.([]any); ok {
			lo, hi := 0, len(a)
			if st.lo != nil {
				lo = *st.lo
				if lo < 0 {
					lo += len(a)
				}
			}
			if st.hi != nil {
				hi = *st.hi
				if hi < 0 {
					hi += len(a)
				}
			}
			lo, hi = max(lo, 0), min(hi, len(a))
			if lo < hi {
				out = append(out, a[lo:hi]...)
			}
		}
	case pFilter:
		switch v := n.(type) {
		case []any:
			for _, e := range v {
				if st.filter.match(e) {
					out = append(out, e)
				}
			}
		case map[string]any:
			for _, k := range sortedKeys(v) {
				if st.filter.match(v[k]) {
					out = append(out, v[k])
				}
			}
		}
	}
	return out
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	// insertion sort: maps here are tiny
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j] < keys[j-1]; j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	return keys
}

// ---- value helpers shared by mappings ----

// asString renders a JSON value as text: strings verbatim, scalars formatted,
// structures re-marshaled.
func asString(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(x)
	}
	b, _ := json.Marshal(v)
	return string(b)
}

func asInt(v any) int {
	switch x := v.(type) {
	case float64:
		return int(x)
	case string:
		f, _ := strconv.ParseFloat(x, 64)
		return int(f)
	}
	return 0
}

func asFloat(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case string:
		f, _ := strconv.ParseFloat(x, 64)
		return f
	}
	return 0
}

func toRaw(v any) json.RawMessage {
	if v == nil {
		return nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return b
}
