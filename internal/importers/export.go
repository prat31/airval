package importers

import (
	"bufio"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"iter"
	"sort"
	"strconv"

	"github.com/airval/airval/internal/evalcore"
)

// ExportOptions control CSV export.
type ExportOptions struct {
	// InputKeys / MetadataKeys fix the flattened columns. When empty they are
	// discovered from the first DiscoverRows examples (later unseen keys are
	// dropped), so memory stays bounded.
	InputKeys    []string
	MetadataKeys []string
	// DiscoverRows is how many examples are buffered for key discovery
	// (default 1000).
	DiscoverRows int
}

// ExportJSONL writes one JSON-encoded evalcore.Example per line and returns
// the number written.
func ExportJSONL(ctx context.Context, w io.Writer, seq iter.Seq2[evalcore.Example, error]) (int, error) {
	bw := bufio.NewWriterSize(w, 64<<10)
	enc := json.NewEncoder(bw)
	enc.SetEscapeHTML(false)
	n := 0
	for ex, err := range seq {
		if err != nil {
			return n, err
		}
		if n&255 == 0 {
			if err := ctx.Err(); err != nil {
				return n, err
			}
		}
		if err := enc.Encode(ex); err != nil {
			return n, err
		}
		n++
	}
	return n, bw.Flush()
}

// ExportCSV writes examples as CSV. Columns: id, input.<key>..., expected,
// context, tags, metadata.<key>..., conversation. Scalars are written as-is;
// maps, arrays (context, tags) and conversations are JSON-encoded. The output
// is re-importable via SuggestMapping.
func ExportCSV(ctx context.Context, w io.Writer, seq iter.Seq2[evalcore.Example, error], opts ExportOptions) (int, error) {
	discover := opts.DiscoverRows
	if discover <= 0 {
		discover = 1000
	}
	needDiscovery := len(opts.InputKeys) == 0 || len(opts.MetadataKeys) == 0
	inKeys, metaKeys := opts.InputKeys, opts.MetadataKeys

	cw := csv.NewWriter(w)
	var header []string
	written := 0
	var buf []evalcore.Example

	writeHeader := func() error {
		header = []string{"id"}
		for _, k := range inKeys {
			header = append(header, "input."+k)
		}
		header = append(header, "expected", "context", "tags")
		for _, k := range metaKeys {
			header = append(header, "metadata."+k)
		}
		header = append(header, "conversation")
		return cw.Write(header)
	}
	writeRow := func(ex evalcore.Example) error {
		rec := make([]string, 0, len(header))
		rec = append(rec, ex.ID)
		for _, k := range inKeys {
			rec = append(rec, cellString(ex.Input[k]))
		}
		rec = append(rec, cellString(ex.Expected))
		if len(ex.Context) > 0 {
			rec = append(rec, jsonString(ex.Context))
		} else {
			rec = append(rec, "")
		}
		if len(ex.Tags) > 0 {
			rec = append(rec, jsonString(ex.Tags))
		} else {
			rec = append(rec, "")
		}
		for _, k := range metaKeys {
			rec = append(rec, cellString(ex.Metadata[k]))
		}
		if ex.Conversation != nil {
			rec = append(rec, jsonString(ex.Conversation))
		} else {
			rec = append(rec, "")
		}
		written++
		return cw.Write(rec)
	}
	startStreaming := func() error {
		if needDiscovery {
			ins, metas := map[string]bool{}, map[string]bool{}
			for _, ex := range buf {
				for k := range ex.Input {
					ins[k] = true
				}
				for k := range ex.Metadata {
					metas[k] = true
				}
			}
			if len(inKeys) == 0 {
				inKeys = sortedKeys(ins)
			}
			if len(metaKeys) == 0 {
				metaKeys = sortedKeys(metas)
			}
		}
		if err := writeHeader(); err != nil {
			return err
		}
		for _, ex := range buf {
			if err := writeRow(ex); err != nil {
				return err
			}
		}
		buf = nil
		return nil
	}

	started := !needDiscovery
	if started {
		if err := writeHeader(); err != nil {
			return 0, err
		}
	}
	i := 0
	for ex, err := range seq {
		if err != nil {
			return written, err
		}
		if i&255 == 0 {
			if err := ctx.Err(); err != nil {
				return written, err
			}
		}
		i++
		if !started {
			buf = append(buf, ex)
			if len(buf) >= discover {
				if err := startStreaming(); err != nil {
					return written, err
				}
				started = true
			}
			continue
		}
		if err := writeRow(ex); err != nil {
			return written, err
		}
	}
	if !started {
		if err := startStreaming(); err != nil {
			return written, err
		}
	}
	cw.Flush()
	return written, cw.Error()
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func jsonString(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}

// cellString renders a value for a CSV cell: scalars plain, others as JSON.
func cellString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case bool:
		return strconv.FormatBool(t)
	case int:
		return strconv.Itoa(t)
	case int64:
		return strconv.FormatInt(t, 10)
	case float64:
		return strconv.FormatFloat(t, 'g', -1, 64)
	case json.Number:
		return t.String()
	case fmt.Stringer:
		return t.String()
	}
	return jsonString(v)
}
