package importers

import (
	"bufio"
	"bytes"
	"encoding/csv"
	"fmt"
	"io"
	"strconv"
)

type csvReader struct {
	rc   io.Closer
	r    *csv.Reader
	cols []string
	row  int
}

const sniffWindow = 64 << 10

func newCSVReader(rc io.ReadCloser, delim rune) (Reader, error) {
	br := bufio.NewReaderSize(rc, sniffWindow)
	if b, err := br.Peek(3); err == nil && bytes.Equal(b, []byte("\xef\xbb\xbf")) {
		br.Discard(3)
	}
	if delim == 0 {
		head, _ := br.Peek(sniffWindow)
		delim = sniffDelimiter(head)
	}
	r := csv.NewReader(br)
	r.Comma = delim
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	hdr, err := r.Read()
	for err != nil {
		if err == io.EOF {
			return &csvReader{rc: rc, r: r}, nil // empty file
		}
		if _, ok := err.(*csv.ParseError); !ok {
			return nil, err
		}
		hdr, err = r.Read()
	}
	return &csvReader{rc: rc, r: r, cols: dedupeHeader(hdr)}, nil
}

func dedupeHeader(h []string) []string {
	out := make([]string, len(h))
	seen := make(map[string]int, len(h))
	for i, n := range h {
		if n == "" {
			n = "column_" + strconv.Itoa(i+1)
		}
		if c := seen[n]; c > 0 {
			seen[n] = c + 1
			n = n + "_" + strconv.Itoa(c+1)
		} else {
			seen[n] = 1
		}
		out[i] = n
	}
	return out
}

func sniffDelimiter(head []byte) rune {
	if len(head) >= sniffWindow {
		if i := bytes.LastIndexByte(head, '\n'); i > 0 {
			head = head[:i]
		}
	}
	best, bestScore := ',', 0
	for _, d := range []rune{',', '\t', ';', '|'} {
		r := csv.NewReader(bytes.NewReader(head))
		r.Comma = d
		r.FieldsPerRecord = -1
		r.LazyQuotes = true
		first, matches := -1, 0
		for n := 0; n < 50; n++ {
			rec, err := r.Read()
			if err == io.EOF {
				break
			}
			if err != nil {
				continue
			}
			if first < 0 {
				first = len(rec)
				if first < 2 {
					break
				}
			}
			if len(rec) == first {
				matches++
			}
		}
		// Prefer the delimiter that yields the most rows matching the header
		// width; ties go to the wider header.
		if first > 1 {
			if score := matches*1000 + first; score > bestScore {
				best, bestScore = d, score
			}
		}
	}
	return best
}

func (c *csvReader) Columns() []string { return c.cols }
func (c *csvReader) Close() error      { return c.rc.Close() }

func (c *csvReader) Next() (Row, error) {
	if c.cols == nil {
		return Row{}, io.EOF
	}
	rec, err := c.r.Read()
	if err == io.EOF {
		return Row{}, io.EOF
	}
	c.row++
	if err != nil {
		if pe, ok := err.(*csv.ParseError); ok {
			return Row{}, &RowError{Row: c.row, Reason: pe.Err.Error()}
		}
		return Row{}, err
	}
	if len(rec) > len(c.cols) {
		return Row{}, &RowError{Row: c.row, Reason: fmt.Sprintf("too many fields: got %d, header has %d", len(rec), len(c.cols))}
	}
	vals := make([]any, len(c.cols))
	for i, s := range rec {
		vals[i] = s
	}
	for i := len(rec); i < len(vals); i++ {
		vals[i] = ""
	}
	return Row{Keys: c.cols, Vals: vals}, nil
}
