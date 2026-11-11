package importers

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// ReadOptions tweak parsing. All fields are optional.
type ReadOptions struct {
	Delimiter rune   // CSV delimiter; 0 = sniff
	JSONField string // top-level array field for JSON objects; "" = first array field
}

// Source describes where a dataset comes from. Build one with FromReader,
// FromBytes, FromFile or FromHF.
type Source struct {
	Format  Format
	Name    string
	Size    int64 // bytes if known, else -1
	Options ReadOptions

	open func(ctx context.Context) (io.ReadCloser, error)
	hf   *hfSource
}

// Open returns a streaming Reader over the source.
func (s Source) Open(ctx context.Context) (Reader, error) {
	return s.openReader(ctx, nil)
}

func (s Source) openReader(ctx context.Context, wrap func(io.ReadCloser) io.ReadCloser) (Reader, error) {
	if s.Format == FormatHF {
		if s.hf == nil {
			return nil, errors.New("importers: hf source not configured")
		}
		return newHFReader(ctx, s.hf)
	}
	if s.open == nil {
		return nil, errors.New("importers: source has no data")
	}
	rc, err := s.open(ctx)
	if err != nil {
		return nil, err
	}
	if wrap != nil {
		rc = wrap(rc)
	}
	var r Reader
	switch s.Format {
	case FormatCSV:
		r, err = newCSVReader(rc, s.Options.Delimiter)
	case FormatJSONL:
		r = newJSONLReader(rc)
	case FormatJSON:
		r = newJSONReader(rc, s.Options.JSONField)
	case FormatParquet:
		r, err = newParquetReader(rc)
	default:
		err = fmt.Errorf("importers: unsupported format %q", s.Format)
	}
	if err != nil {
		rc.Close()
		return nil, err
	}
	return r, nil
}

// FromReader wraps a one-shot reader. If format is empty it is sniffed from the
// first bytes. The source can be opened only once.
func FromReader(r io.Reader, format Format) Source {
	br := bufio.NewReaderSize(r, 64<<10)
	if format == "" {
		head, _ := br.Peek(8 << 10)
		format = Sniff("", head)
	}
	var once sync.Once
	return Source{Format: format, Size: -1, open: func(context.Context) (io.ReadCloser, error) {
		var rc io.ReadCloser
		once.Do(func() { rc = io.NopCloser(br) })
		if rc == nil {
			return nil, errors.New("importers: reader source already consumed")
		}
		return rc, nil
	}}
}

// FromBytes creates a re-openable source from memory.
func FromBytes(b []byte, format Format) Source {
	if format == "" {
		format = Sniff("", b)
	}
	return Source{Format: format, Size: int64(len(b)), open: func(context.Context) (io.ReadCloser, error) {
		return readerAtCloser{bytes.NewReader(b)}, nil
	}}
}

type readerAtCloser struct{ *bytes.Reader }

func (readerAtCloser) Close() error { return nil }

// FromFile creates a re-openable source from a file path (e.g. an upload). The
// format is detected from the extension, falling back to content sniffing; a
// non-empty format overrides detection.
func FromFile(path string, format Format) (Source, error) {
	f, err := os.Open(path)
	if err != nil {
		return Source{}, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return Source{}, err
	}
	if format == "" {
		head := make([]byte, 8<<10)
		n, _ := io.ReadFull(f, head)
		format = Sniff(path, head[:n])
	}
	return Source{Format: format, Name: filepath.Base(path), Size: st.Size(), open: func(context.Context) (io.ReadCloser, error) {
		return os.Open(path)
	}}, nil
}

// FormatFromName maps a file name's extension to a Format ("" if unknown).
func FormatFromName(name string) Format {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".csv", ".tsv", ".txt":
		return FormatCSV
	case ".jsonl", ".ndjson":
		return FormatJSONL
	case ".json":
		return FormatJSON
	case ".parquet", ".pq":
		return FormatParquet
	}
	return ""
}

// Sniff detects the format from a file name and the first bytes of content.
func Sniff(name string, head []byte) Format {
	if bytes.HasPrefix(head, []byte("PAR1")) {
		return FormatParquet
	}
	byExt := FormatFromName(name)
	if byExt == FormatCSV || byExt == FormatParquet || byExt == FormatJSONL {
		return byExt
	}
	t := bytes.TrimPrefix(head, []byte("\xef\xbb\xbf"))
	t = bytes.TrimLeft(t, " \t\r\n")
	switch {
	case len(t) > 0 && t[0] == '[':
		return FormatJSON
	case len(t) > 0 && t[0] == '{':
		line := t
		if i := bytes.IndexByte(t, '\n'); i >= 0 {
			line = t[:i]
		}
		var m map[string]any
		if json.Unmarshal(bytes.TrimSpace(line), &m) != nil {
			return FormatJSON
		}
		// One-line object with no further lines: a wrapper object around an
		// array of objects is JSON, anything else is a single JSONL record.
		if len(bytes.TrimSpace(t[len(line):])) == 0 {
			for _, v := range m {
				if a, ok := v.([]any); ok && len(a) > 0 {
					allObj := true
					for _, e := range a {
						if _, ok := e.(map[string]any); !ok {
							allObj = false
							break
						}
					}
					if allObj {
						return FormatJSON
					}
				}
			}
		}
		return FormatJSONL
	}
	if byExt != "" {
		return byExt
	}
	return FormatCSV
}
