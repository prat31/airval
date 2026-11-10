package importers

import (
	"fmt"
	"io"
	"os"
	"time"

	"github.com/parquet-go/parquet-go"
)

type parquetReader struct {
	rc      io.Closer
	cleanup func()
	r       *parquet.Reader
	cols    []string
	total   int64
	row     int
}

func newParquetReader(rc io.ReadCloser) (Reader, error) {
	var (
		ra      io.ReaderAt
		size    int64
		cleanup = func() {}
	)
	switch t := rc.(type) {
	case *os.File:
		st, err := t.Stat()
		if err != nil {
			return nil, err
		}
		ra, size = t, st.Size()
	case readerAtCloser:
		ra, size = t.Reader, t.Reader.Size()
	default:
		// Parquet needs random access: spool to a temp file.
		tmp, err := os.CreateTemp("", "airval-parquet-*")
		if err != nil {
			return nil, err
		}
		cleanup = func() { tmp.Close(); os.Remove(tmp.Name()) }
		n, err := io.Copy(tmp, rc)
		if err != nil {
			cleanup()
			return nil, err
		}
		ra, size = tmp, n
	}
	f, err := parquet.OpenFile(ra, size)
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("invalid parquet file: %w", err)
	}
	var cols []string
	for _, fld := range f.Schema().Fields() {
		cols = append(cols, fld.Name())
	}
	return &parquetReader{rc: rc, cleanup: cleanup, r: parquet.NewReader(f), cols: cols, total: f.NumRows()}, nil
}

func (p *parquetReader) Columns() []string { return p.cols }
func (p *parquetReader) Total() int64      { return p.total }

func (p *parquetReader) Close() error {
	p.r.Close()
	p.cleanup()
	return p.rc.Close()
}

func (p *parquetReader) Next() (Row, error) {
	m := make(map[string]any, len(p.cols))
	if err := p.r.Read(&m); err != nil {
		if err == io.EOF {
			return Row{}, io.EOF
		}
		return Row{}, err
	}
	p.row++
	vals := make([]any, len(p.cols))
	for i, c := range p.cols {
		vals[i] = normalizeParquet(m[c])
	}
	return Row{Keys: p.cols, Vals: vals}, nil
}

func normalizeParquet(v any) any {
	switch t := v.(type) {
	case time.Time:
		return t.UTC().Format(time.RFC3339Nano)
	case time.Duration:
		return t.String()
	case []any:
		for i := range t {
			t[i] = normalizeParquet(t[i])
		}
		return t
	case map[string]any:
		for k, x := range t {
			t[k] = normalizeParquet(x)
		}
		return t
	}
	return normalize(v)
}
