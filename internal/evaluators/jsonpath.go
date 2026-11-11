package evaluators

import (
	"fmt"
	"strconv"
	"strings"
)

// jsonPath is a compiled subset of JSONPath sufficient for selecting values
// out of structured outputs:
//
//	$            root (optional prefix)
//	.key  ['key'] ["key"]   object member
//	[0]  [-1]    array index (negative counts from the end)
//	[*]  .*      wildcard over array elements / object values
//
// A path containing a wildcard yields a []any of all matches.
type jsonPath struct {
	src  string
	segs []pathSeg
	wild bool
}

type pathSeg struct {
	key   string
	index int
	kind  uint8 // segKey | segIndex | segWild
}

const (
	segKey uint8 = iota
	segIndex
	segWild
)

func compileJSONPath(p string) (*jsonPath, error) {
	jp := &jsonPath{src: p}
	s := strings.TrimSpace(p)
	s = strings.TrimPrefix(s, "$")
	i := 0
	for i < len(s) {
		switch s[i] {
		case '.':
			i++
			if i < len(s) && s[i] == '*' {
				jp.segs = append(jp.segs, pathSeg{kind: segWild})
				jp.wild = true
				i++
				continue
			}
			j := i
			for j < len(s) && s[j] != '.' && s[j] != '[' {
				j++
			}
			if j == i {
				return nil, fmt.Errorf("invalid JSONPath %q: empty member name at offset %d", p, i)
			}
			jp.segs = append(jp.segs, pathSeg{kind: segKey, key: s[i:j]})
			i = j
		case '[':
			j := i + 1
			for j < len(s) && s[j] == ' ' {
				j++
			}
			// Quoted keys may contain ']' - scan to the matching quote.
			if j < len(s) && (s[j] == '\'' || s[j] == '"') {
				q := s[j]
				closeAt := strings.IndexByte(s[j+1:], q)
				if closeAt < 0 {
					return nil, fmt.Errorf("invalid JSONPath %q: unterminated quote", p)
				}
				key := s[j+1 : j+1+closeAt]
				k := j + 1 + closeAt + 1
				for k < len(s) && s[k] == ' ' {
					k++
				}
				if k >= len(s) || s[k] != ']' {
					return nil, fmt.Errorf("invalid JSONPath %q: expected ']' after quoted key", p)
				}
				jp.segs = append(jp.segs, pathSeg{kind: segKey, key: key})
				i = k + 1
				continue
			}
			end := strings.IndexByte(s[i:], ']')
			if end < 0 {
				return nil, fmt.Errorf("invalid JSONPath %q: unclosed '['", p)
			}
			inner := strings.TrimSpace(s[i+1 : i+end])
			switch {
			case inner == "*":
				jp.segs = append(jp.segs, pathSeg{kind: segWild})
				jp.wild = true
			default:
				n, err := strconv.Atoi(inner)
				if err != nil {
					return nil, fmt.Errorf("invalid JSONPath %q: bad index %q", p, inner)
				}
				jp.segs = append(jp.segs, pathSeg{kind: segIndex, index: n})
			}
			i += end + 1
		default:
			// Bare leading member name: "a.b" == "$.a.b".
			if i == 0 {
				s = "." + s
				continue
			}
			return nil, fmt.Errorf("invalid JSONPath %q: unexpected %q at offset %d", p, s[i], i)
		}
	}
	return jp, nil
}

// get evaluates the path. ok is false if a non-wildcard segment is missing.
func (jp *jsonPath) get(v any) (any, bool) {
	if !jp.wild {
		cur := v
		for _, sg := range jp.segs {
			var ok bool
			cur, ok = step(cur, sg)
			if !ok {
				return nil, false
			}
		}
		return cur, true
	}
	cur := []any{v}
	for _, sg := range jp.segs {
		next := cur[:0:0]
		for _, c := range cur {
			if sg.kind == segWild {
				switch t := c.(type) {
				case []any:
					next = append(next, t...)
				case map[string]any:
					for _, k := range sortedKeys(t) {
						next = append(next, t[k])
					}
				}
				continue
			}
			if r, ok := step(c, sg); ok {
				next = append(next, r)
			}
		}
		cur = next
	}
	return cur, true
}

func step(v any, sg pathSeg) (any, bool) {
	switch sg.kind {
	case segKey:
		m, ok := v.(map[string]any)
		if !ok {
			return nil, false
		}
		r, ok := m[sg.key]
		return r, ok
	case segIndex:
		a, ok := v.([]any)
		if !ok {
			return nil, false
		}
		i := sg.index
		if i < 0 {
			i += len(a)
		}
		if i < 0 || i >= len(a) {
			return nil, false
		}
		return a[i], true
	}
	return nil, false
}
