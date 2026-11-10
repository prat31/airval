// Package importers reads datasets from CSV, JSONL, JSON, Parquet and Hugging
// Face Datasets in a streaming fashion (constant memory), suggests column
// mappings, converts rows into evalcore.Example batches and exports examples
// back to CSV / JSONL.
package importers

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
)

// Format is a supported dataset format.
type Format string

const (
	FormatCSV     Format = "csv"
	FormatJSONL   Format = "jsonl"
	FormatJSON    Format = "json"
	FormatParquet Format = "parquet"
	FormatHF      Format = "hf"
)

// Row is one source record. Keys keeps the original column order; Vals is
// parallel to Keys. Values are normalized: string, bool, int64, float64, nil,
// []any, map[string]any.
type Row struct {
	Keys []string
	Vals []any
}

// Get returns the value of the named column.
func (r Row) Get(name string) (any, bool) {
	for i, k := range r.Keys {
		if k == name {
			if i < len(r.Vals) {
				return r.Vals[i], true
			}
			return nil, false
		}
	}
	return nil, false
}

// Map returns the row as a map.
func (r Row) Map() map[string]any {
	m := make(map[string]any, len(r.Keys))
	for i, k := range r.Keys {
		if i < len(r.Vals) {
			m[k] = r.Vals[i]
		}
	}
	return m
}

// RowError is a recoverable per-row problem. Row is the 1-based index of the
// data row (header excluded).
type RowError struct {
	Row    int    `json:"row"`
	Reason string `json:"reason"`
}

func (e *RowError) Error() string { return fmt.Sprintf("row %d: %s", e.Row, e.Reason) }

// Reader streams rows from a source.
//
// Next returns io.EOF at the end. A *RowError means that row was skipped and
// reading can continue; any other error is fatal.
type Reader interface {
	// Columns returns the column names when known up-front (CSV header,
	// Parquet schema, HF features); nil for JSON/JSONL.
	Columns() []string
	Next() (Row, error)
	Close() error
}

// totaler is implemented by readers that know the exact row count.
type totaler interface{ Total() int64 }

func isRowError(err error) (*RowError, bool) {
	var re *RowError
	if errors.As(err, &re) {
		return re, true
	}
	return nil, false
}

// normalize converts decoder output into the canonical value set.
func normalize(v any) any {
	switch t := v.(type) {
	case json.Number:
		if i, err := t.Int64(); err == nil {
			return i
		}
		f, err := t.Float64()
		if err != nil || math.IsInf(f, 0) {
			return t.String()
		}
		return f
	case []byte:
		return string(t)
	case []any:
		for i := range t {
			t[i] = normalize(t[i])
		}
		return t
	case map[string]any:
		for k, x := range t {
			t[k] = normalize(x)
		}
		return t
	case int:
		return int64(t)
	case int8:
		return int64(t)
	case int16:
		return int64(t)
	case int32:
		return int64(t)
	case uint8:
		return int64(t)
	case uint16:
		return int64(t)
	case uint32:
		return int64(t)
	case uint64:
		if t > math.MaxInt64 {
			return float64(t)
		}
		return int64(t)
	case float32:
		return float64(t)
	}
	return v
}
