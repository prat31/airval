package importers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// DefaultHFBaseURL is the public Hugging Face datasets-server.
const DefaultHFBaseURL = "https://datasets-server.huggingface.co"

// hfPageSize is the maximum page length the datasets-server allows.
const hfPageSize = 100

// HFClient talks to the HF datasets-server. BaseURL and HTTP are injectable
// for tests; zero values use the public server and http.DefaultClient.
type HFClient struct {
	BaseURL string
	HTTP    *http.Client
	Token   string // optional bearer token (only needed for gated datasets)
	// RetryDelay is the base backoff for 429/5xx responses (default 500ms).
	RetryDelay time.Duration
}

// HFRef identifies a dataset slice on the Hub.
type HFRef struct {
	Dataset string `json:"dataset"`
	Config  string `json:"config,omitempty"`
	Split   string `json:"split,omitempty"`
}

// HFSplit is one (config, split) pair of a dataset.
type HFSplit struct {
	Dataset string `json:"dataset"`
	Config  string `json:"config"`
	Split   string `json:"split"`
}

type hfSource struct {
	c   *HFClient
	ref HFRef
}

// FromHF creates a source reading public rows of a HF dataset. If Config or
// Split are empty they are discovered via /splits (first pair).
func FromHF(c *HFClient, ref HFRef) Source {
	if c == nil {
		c = &HFClient{}
	}
	return Source{Format: FormatHF, Name: ref.Dataset, Size: -1, hf: &hfSource{c: c, ref: ref}}
}

func (c *HFClient) base() string {
	if c.BaseURL != "" {
		return c.BaseURL
	}
	return DefaultHFBaseURL
}

func (c *HFClient) get(ctx context.Context, path string, q url.Values, out any) error {
	cl := c.HTTP
	if cl == nil {
		cl = http.DefaultClient
	}
	delay := c.RetryDelay
	if delay == 0 {
		delay = 500 * time.Millisecond
	}
	u := c.base() + path + "?" + q.Encode()
	var lastErr error
	for attempt := 0; attempt < 4; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay * time.Duration(1<<(attempt-1))):
			}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return err
		}
		req.Header.Set("Accept", "application/json")
		if c.Token != "" {
			req.Header.Set("Authorization", "Bearer "+c.Token)
		}
		resp, err := cl.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		body, rerr := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
		resp.Body.Close()
		if rerr != nil {
			lastErr = rerr
			continue
		}
		if resp.StatusCode == 429 || resp.StatusCode >= 500 {
			lastErr = fmt.Errorf("huggingface: HTTP %d", resp.StatusCode)
			continue
		}
		if resp.StatusCode != 200 {
			var e struct {
				Error string `json:"error"`
			}
			json.Unmarshal(body, &e)
			if e.Error == "" {
				e.Error = http.StatusText(resp.StatusCode)
			}
			return fmt.Errorf("huggingface: %s (HTTP %d)", e.Error, resp.StatusCode)
		}
		dec := json.NewDecoder(bytes.NewReader(body))
		dec.UseNumber()
		return dec.Decode(out)
	}
	return lastErr
}

// Splits lists the configs and splits of a dataset.
func (c *HFClient) Splits(ctx context.Context, dataset string) ([]HFSplit, error) {
	var out struct {
		Splits []HFSplit `json:"splits"`
	}
	if err := c.get(ctx, "/splits", url.Values{"dataset": {dataset}}, &out); err != nil {
		return nil, err
	}
	return out.Splits, nil
}

type hfRowsResp struct {
	Features []struct {
		Name string `json:"name"`
	} `json:"features"`
	Rows []struct {
		RowIdx int64                      `json:"row_idx"`
		Row    map[string]json.RawMessage `json:"row"`
	} `json:"rows"`
	NumRowsTotal int64 `json:"num_rows_total"`
}

type hfReader struct {
	ctx    context.Context
	src    *hfSource
	cols   []string
	buf    []Row
	offset int64
	total  int64
	done   bool
	row    int
}

func newHFReader(ctx context.Context, s *hfSource) (Reader, error) {
	ref := s.ref
	if ref.Dataset == "" {
		return nil, fmt.Errorf("huggingface: dataset is required")
	}
	if ref.Config == "" || ref.Split == "" {
		splits, err := s.c.Splits(ctx, ref.Dataset)
		if err != nil {
			return nil, err
		}
		found := false
		for _, sp := range splits {
			if (ref.Config == "" || sp.Config == ref.Config) && (ref.Split == "" || sp.Split == ref.Split) {
				ref.Config, ref.Split, found = sp.Config, sp.Split, true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("huggingface: no matching split for %s", ref.Dataset)
		}
	}
	s2 := &hfSource{c: s.c, ref: ref}
	h := &hfReader{ctx: ctx, src: s2, total: -1}
	if err := h.fetch(); err != nil { // first page gives columns + total
		return nil, err
	}
	return h, nil
}

func (h *hfReader) fetch() error {
	q := url.Values{
		"dataset": {h.src.ref.Dataset},
		"config":  {h.src.ref.Config},
		"split":   {h.src.ref.Split},
		"offset":  {strconv.FormatInt(h.offset, 10)},
		"length":  {strconv.Itoa(hfPageSize)},
	}
	var resp hfRowsResp
	if err := h.src.c.get(h.ctx, "/rows", q, &resp); err != nil {
		return err
	}
	if h.cols == nil {
		for _, f := range resp.Features {
			h.cols = append(h.cols, f.Name)
		}
	}
	h.total = resp.NumRowsTotal
	h.buf = h.buf[:0]
	for _, r := range resp.Rows {
		vals := make([]any, len(h.cols))
		for i, c := range h.cols {
			if raw, ok := r.Row[c]; ok {
				dec := json.NewDecoder(bytes.NewReader(raw))
				dec.UseNumber()
				var v any
				if err := dec.Decode(&v); err == nil {
					vals[i] = normalize(v)
				}
			}
		}
		h.buf = append(h.buf, Row{Keys: h.cols, Vals: vals})
	}
	h.offset += int64(len(resp.Rows))
	if len(resp.Rows) == 0 || h.offset >= h.total {
		h.done = true
	}
	return nil
}

func (h *hfReader) Columns() []string { return h.cols }
func (h *hfReader) Total() int64      { return h.total }
func (h *hfReader) Close() error      { return nil }

func (h *hfReader) Next() (Row, error) {
	if len(h.buf) == 0 {
		if h.done {
			return Row{}, io.EOF
		}
		if err := h.fetch(); err != nil {
			return Row{}, err
		}
		if len(h.buf) == 0 {
			return Row{}, io.EOF
		}
	}
	r := h.buf[0]
	h.buf = h.buf[1:]
	h.row++
	return r, nil
}
