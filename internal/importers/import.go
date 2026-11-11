package importers

import (
	"context"
	"fmt"
	"io"

	"github.com/airval/airval/internal/evalcore"
)

const (
	// DefaultBatchSize is the number of examples handed to the sink at once.
	DefaultBatchSize = 500
	// MaxRowErrors caps the number of collected per-row errors.
	MaxRowErrors = 100
)

// ImportResult summarizes an import.
type ImportResult struct {
	Total           int        `json:"total"`    // rows read (valid + invalid)
	Imported        int        `json:"imported"` // examples passed to the sink
	Failed          int        `json:"failed"`   // rows skipped
	Errors          []RowError `json:"errors,omitempty"`
	ErrorsTruncated bool       `json:"errors_truncated,omitempty"` // more than MaxRowErrors failures
}

func (r *ImportResult) fail(row int, reason string) {
	r.Failed++
	if len(r.Errors) < MaxRowErrors {
		r.Errors = append(r.Errors, RowError{Row: row, Reason: reason})
	} else {
		r.ErrorsTruncated = true
	}
}

// Import streams src, converts each row with the mapping and hands batches of
// examples to sink. Bad rows are skipped and reported in the result; a sink
// error or a fatal read error aborts the import (the partial result is still
// returned). Each batch slice is freshly allocated, so the sink may retain it.
func Import(ctx context.Context, src Source, m Mapping, sink func(batch []evalcore.Example) error, batchSize int) (ImportResult, error) {
	var res ImportResult
	if err := m.Validate(); err != nil {
		return res, err
	}
	if batchSize <= 0 {
		batchSize = DefaultBatchSize
	}
	r, err := src.Open(ctx)
	if err != nil {
		return res, err
	}
	defer r.Close()

	if cols := r.Columns(); len(cols) > 0 {
		have := make(map[string]bool, len(cols))
		for _, c := range cols {
			have[c] = true
		}
		for _, c := range m.referencedColumns() {
			if !have[c] {
				return res, fmt.Errorf("mapping: column %q not found in source", c)
			}
		}
	}

	conv := newConverter(m)
	batch := make([]evalcore.Example, 0, batchSize)
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		if err := sink(batch); err != nil {
			return err
		}
		res.Imported += len(batch)
		batch = make([]evalcore.Example, 0, batchSize)
		return nil
	}

	n := 0
	for {
		if n&255 == 0 {
			if err := ctx.Err(); err != nil {
				return res, err
			}
		}
		row, err := r.Next()
		if err == io.EOF {
			break
		}
		n++
		if re, ok := isRowError(err); ok {
			res.Total++
			res.fail(re.Row, re.Reason)
			continue
		}
		if err != nil {
			return res, err
		}
		res.Total++
		ex, cerr := conv.convert(row)
		if cerr != nil {
			res.fail(n, cerr.Error())
			continue
		}
		batch = append(batch, ex)
		if len(batch) >= batchSize {
			if err := flush(); err != nil {
				return res, err
			}
		}
	}
	if err := flush(); err != nil {
		return res, err
	}
	return res, nil
}
