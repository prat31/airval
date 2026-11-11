package importers

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// decodeObject decodes one JSON object preserving key order.
func decodeObject(dec *json.Decoder) (Row, error) {
	tok, err := dec.Token()
	if err != nil {
		return Row{}, err
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		if ok && d == '[' {
			skipRest(dec)
		}
		return Row{}, fmt.Errorf("expected object, got %v", tok)
	}
	var row Row
	for dec.More() {
		kt, err := dec.Token()
		if err != nil {
			return Row{}, err
		}
		key, _ := kt.(string)
		var v any
		if err := dec.Decode(&v); err != nil {
			return Row{}, err
		}
		row.Keys = append(row.Keys, key)
		row.Vals = append(row.Vals, normalize(v))
	}
	if _, err := dec.Token(); err != nil { // closing }
		return Row{}, err
	}
	return row, nil
}

// skipRest consumes tokens until the already-opened container is closed.
func skipRest(dec *json.Decoder) {
	depth := 1
	for depth > 0 {
		t, err := dec.Token()
		if err != nil {
			return
		}
		if d, ok := t.(json.Delim); ok {
			if d == '[' || d == '{' {
				depth++
			} else {
				depth--
			}
		}
	}
}

// ---- JSONL ----

type jsonlReader struct {
	rc  io.Closer
	br  *bufio.Reader
	row int
	eof bool
}

func newJSONLReader(rc io.ReadCloser) Reader {
	return &jsonlReader{rc: rc, br: bufio.NewReaderSize(rc, 64<<10)}
}

func (j *jsonlReader) Columns() []string { return nil }
func (j *jsonlReader) Close() error      { return j.rc.Close() }

func (j *jsonlReader) Next() (Row, error) {
	for !j.eof {
		line, err := j.br.ReadBytes('\n')
		if err != nil {
			if err != io.EOF {
				return Row{}, err
			}
			j.eof = true
		}
		line = bytes.TrimPrefix(line, []byte("\xef\xbb\xbf"))
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		j.row++
		dec := json.NewDecoder(bytes.NewReader(line))
		dec.UseNumber()
		row, derr := decodeObject(dec)
		if derr != nil {
			return Row{}, &RowError{Row: j.row, Reason: "invalid JSON: " + derr.Error()}
		}
		return row, nil
	}
	return Row{}, io.EOF
}

// ---- JSON (array of objects, or object with a top-level array field) ----

type jsonReader struct {
	rc    io.Closer
	dec   *json.Decoder
	field string
	state int // 0 start, 1 in array, 2 done
	row   int
}

func newJSONReader(rc io.ReadCloser, field string) Reader {
	br := bufio.NewReaderSize(rc, 64<<10)
	if b, err := br.Peek(3); err == nil && bytes.Equal(b, []byte("\xef\xbb\xbf")) {
		br.Discard(3)
	}
	dec := json.NewDecoder(br)
	dec.UseNumber()
	return &jsonReader{rc: rc, dec: dec, field: field}
}

func (j *jsonReader) Columns() []string { return nil }
func (j *jsonReader) Close() error      { return j.rc.Close() }

func (j *jsonReader) start() error {
	tok, err := j.dec.Token()
	if err != nil {
		if err == io.EOF {
			j.state = 2
			return nil
		}
		return err
	}
	d, _ := tok.(json.Delim)
	switch d {
	case '[':
		j.state = 1
		return nil
	case '{':
		for j.dec.More() {
			kt, err := j.dec.Token()
			if err != nil {
				return err
			}
			key, _ := kt.(string)
			vt, err := j.dec.Token()
			if err != nil {
				return err
			}
			if vd, ok := vt.(json.Delim); ok {
				if vd == '[' && (j.field == "" || j.field == key) {
					j.state = 1
					return nil
				}
				skipRest(j.dec)
			}
		}
		if j.field != "" {
			return fmt.Errorf("array field %q not found in JSON object", j.field)
		}
		return errors.New("JSON object has no top-level array field")
	}
	return errors.New("JSON must be an array of objects or an object with an array field")
}

func (j *jsonReader) Next() (Row, error) {
	if j.state == 0 {
		if err := j.start(); err != nil {
			return Row{}, err
		}
	}
	if j.state == 2 || !j.dec.More() {
		j.state = 2
		return Row{}, io.EOF
	}
	j.row++
	row, err := decodeObject(j.dec)
	if err != nil {
		var se *json.SyntaxError
		if errors.As(err, &se) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) {
			return Row{}, err
		}
		return Row{}, &RowError{Row: j.row, Reason: err.Error()}
	}
	return row, nil
}
