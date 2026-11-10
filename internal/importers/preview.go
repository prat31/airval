package importers

import (
	"context"
	"encoding/json"
	"io"
	"strconv"
	"strings"
)

// Column describes one source column.
type Column struct {
	Name         string `json:"name"`
	InferredType string `json:"inferred_type"` // string|int|float|bool|array|object|messages|null
	SampleValues []any  `json:"sample_values"`
}

// DatasetPreview is the first rows of a source plus column info.
type DatasetPreview struct {
	Format        Format     `json:"format"`
	Columns       []Column   `json:"columns"`
	Rows          [][]any    `json:"rows"` // aligned with Columns
	TotalEstimate int64      `json:"total_estimate"`
	TotalExact    bool       `json:"total_exact"`
	RowErrors     []RowError `json:"row_errors,omitempty"`
}

const (
	maxSamples     = 5
	maxSampleChars = 200
)

// Preview reads up to n rows from src (it does not read the whole source).
// TotalEstimate is -1 when it cannot be estimated.
func Preview(ctx context.Context, src Source, n int) (DatasetPreview, error) {
	if n <= 0 {
		n = 20
	}
	r, err := src.Open(ctx)
	if err != nil {
		return DatasetPreview{}, err
	}
	defer r.Close()

	p := DatasetPreview{Format: src.Format, TotalEstimate: -1}
	var rows []Row
	eof := false
	var sizeSum int64
	for len(rows) < n {
		row, err := r.Next()
		if err == io.EOF {
			eof = true
			break
		}
		if re, ok := isRowError(err); ok {
			if len(p.RowErrors) < 20 {
				p.RowErrors = append(p.RowErrors, *re)
			}
			continue
		}
		if err != nil {
			return DatasetPreview{}, err
		}
		rows = append(rows, row)
		sizeSum += rowSize(row)
		if err := ctx.Err(); err != nil {
			return DatasetPreview{}, err
		}
	}

	names := append([]string(nil), r.Columns()...)
	seen := make(map[string]bool, len(names))
	for _, c := range names {
		seen[c] = true
	}
	for _, row := range rows {
		for _, k := range row.Keys {
			if !seen[k] {
				seen[k] = true
				names = append(names, k)
			}
		}
	}
	idx := make(map[string]int, len(names))
	for i, c := range names {
		idx[c] = i
	}
	accs := make([]typeAcc, len(names))
	p.Columns = make([]Column, len(names))
	for i, c := range names {
		p.Columns[i] = Column{Name: c, SampleValues: []any{}}
	}
	p.Rows = make([][]any, 0, len(rows))
	for _, row := range rows {
		out := make([]any, len(names))
		for i, k := range row.Keys {
			ci := idx[k]
			out[ci] = row.Vals[i]
			accs[ci].add(row.Vals[i])
			c := &p.Columns[ci]
			if len(c.SampleValues) < maxSamples && !isEmptyValue(row.Vals[i]) {
				c.SampleValues = append(c.SampleValues, truncateValue(row.Vals[i]))
			}
		}
		p.Rows = append(p.Rows, out)
	}
	for i := range p.Columns {
		p.Columns[i].InferredType = accs[i].result()
	}

	switch {
	case eof:
		p.TotalEstimate, p.TotalExact = int64(len(rows)), true
	default:
		if t, ok := r.(totaler); ok && t.Total() >= 0 {
			p.TotalEstimate, p.TotalExact = t.Total(), true
		} else if src.Size > 0 && len(rows) > 0 && sizeSum > 0 {
			p.TotalEstimate = src.Size * int64(len(rows)) / sizeSum
		}
	}
	return p, nil
}

func rowSize(r Row) int64 {
	n := int64(len(r.Vals)) // delimiters
	for _, v := range r.Vals {
		switch t := v.(type) {
		case string:
			n += int64(len(t))
		case nil:
		case []any, map[string]any:
			b, _ := json.Marshal(t)
			n += int64(len(b))
		default:
			n += 8
		}
	}
	return n + 1
}

func isEmptyValue(v any) bool {
	if v == nil {
		return true
	}
	s, ok := v.(string)
	return ok && s == ""
}

func truncateValue(v any) any {
	if s, ok := v.(string); ok && len(s) > maxSampleChars {
		return s[:maxSampleChars] + "..."
	}
	if _, ok := v.(string); ok || v == nil {
		return v
	}
	switch v.(type) {
	case []any, map[string]any:
		b, _ := json.Marshal(v)
		if len(b) > maxSampleChars*4 {
			return string(b[:maxSampleChars*4]) + "..."
		}
	}
	return v
}

// ---- type inference ----

type typeAcc struct {
	counts map[string]int
}

func (a *typeAcc) add(v any) {
	t := classify(v)
	if t == "" {
		return
	}
	if a.counts == nil {
		a.counts = map[string]int{}
	}
	a.counts[t]++
}

func (a *typeAcc) result() string {
	switch len(a.counts) {
	case 0:
		return "null"
	case 1:
		for k := range a.counts {
			return k
		}
	}
	if len(a.counts) == 2 && a.counts["int"] > 0 && a.counts["float"] > 0 {
		return "float"
	}
	return "string"
}

// classify returns the type of a value ("" for empty/null). Strings (CSV) are
// parsed for numbers, booleans and JSON containers.
func classify(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case bool:
		return "bool"
	case int64, int:
		return "int"
	case float64:
		return "float"
	case []any:
		if isMessages(t) {
			return "messages"
		}
		return "array"
	case map[string]any:
		return "object"
	case string:
		s := strings.TrimSpace(t)
		if s == "" {
			return ""
		}
		switch strings.ToLower(s) {
		case "true", "false":
			return "bool"
		}
		if _, err := strconv.ParseInt(s, 10, 64); err == nil && !(len(s) > 1 && s[0] == '0') {
			return "int"
		}
		if (s[0] == '-' || s[0] == '.' || (s[0] >= '0' && s[0] <= '9')) && !(len(s) > 1 && s[0] == '0' && s[1] != '.') {
			if _, err := strconv.ParseFloat(s, 64); err == nil {
				return "float"
			}
		}
		if s[0] == '[' || s[0] == '{' {
			var x any
			if json.Unmarshal([]byte(s), &x) == nil {
				return classify(x)
			}
		}
		return "string"
	}
	return "string"
}

func isMessages(a []any) bool {
	if len(a) == 0 {
		return false
	}
	for _, e := range a {
		m, ok := e.(map[string]any)
		if !ok {
			return false
		}
		if _, ok := m["role"]; !ok {
			return false
		}
		if _, ok := m["content"]; !ok {
			return false
		}
	}
	return true
}
