package targets

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/prat31/airval/internal/evalcore"
)

// HTTPConfig configures the generic `http` target kind. method, url, headers
// and body are templates; response fields are mapped with JSONPath.
//
// Template variables: .input.x, .example.{id,metadata,expected,tags,context},
// .trial, .turn, .session, .messages (conversation so far, each {role,content}),
// .message (latest user message); functions: secret, json, default, upper,
// lower, trim, join, replace, contains.
type HTTPConfig struct {
	HTTPCommon
	Method       string            `json:"method,omitempty"` // default POST
	URL          string            `json:"url"`
	Headers      map[string]string `json:"headers,omitempty"`
	Body         string            `json:"body,omitempty"`
	ExpectStatus []int             `json:"expect_status,omitempty"` // default: any 2xx
	Stream       *StreamConfig     `json:"stream,omitempty"`
	Response     ResponseMapping   `json:"response"`
	Session      *SessionConfig    `json:"session,omitempty"`
}

// AgentConfig is the `agent` kind: an HTTP target whose response carries a
// trace (steps, tool calls, retrieved docs). It requires a trace or tool_calls mapping.
type AgentConfig struct {
	HTTPConfig
}

// StreamConfig turns on streaming-response handling.
type StreamConfig struct {
	Mode         string `json:"mode"`                    // "sse" | "ndjson"
	DeltaPath    string `json:"delta_path"`              // JSONPath to the text delta inside each event
	DoneSentinel string `json:"done_sentinel,omitempty"` // SSE data that ends the stream (default "[DONE]")
	Event        string `json:"event,omitempty"`         // only accept SSE events with this name
}

// SessionConfig extracts a conversation/session id from each response; it is
// available to the next turn's templates as {{ .session }}.
type SessionConfig struct {
	Path string `json:"path"`
}

// ResponseMapping maps a JSON response to a Response using JSONPath.
// Paths for usage/cost/context/tool_calls/trace/output_json are optional.
// For streams every event is mapped and the last non-empty value wins; the
// output comes from the accumulated stream.delta_path text.
type ResponseMapping struct {
	Output       string           `json:"output,omitempty"` // default: the whole body as text
	OutputJSON   string           `json:"output_json,omitempty"`
	Context      string           `json:"context,omitempty"` // path yielding strings or objects (-> JSON text)
	ToolCalls    *ToolCallMapping `json:"tool_calls,omitempty"`
	Trace        *TraceMapping    `json:"trace,omitempty"`
	InputTokens  string           `json:"input_tokens,omitempty"`
	OutputTokens string           `json:"output_tokens,omitempty"`
	Cost         string           `json:"cost_usd,omitempty"`
}

// ToolCallMapping: Path selects the tool call objects; the other fields are
// paths relative to each (defaults: $.id $.name $.arguments $.result).
type ToolCallMapping struct {
	Path      string `json:"path"`
	ID        string `json:"id,omitempty"`
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
	Result    string `json:"result,omitempty"`
}

// TraceMapping: Path selects the step objects; the other fields are relative
// paths (defaults: $.kind $.name $.input $.output $.duration_ms).
type TraceMapping struct {
	Path       string `json:"path"`
	Kind       string `json:"kind,omitempty"`
	Name       string `json:"name,omitempty"`
	Input      string `json:"input,omitempty"`
	Output     string `json:"output,omitempty"`
	DurationMS string `json:"duration_ms,omitempty"`
}

func (c *HTTPConfig) Validate() error {
	if err := c.HTTPCommon.validate(); err != nil {
		return err
	}
	if strings.TrimSpace(c.URL) == "" {
		return fmt.Errorf("url is required")
	}
	switch strings.ToUpper(c.Method) {
	case "", "GET", "POST", "PUT", "PATCH", "DELETE":
	default:
		return fmt.Errorf("unsupported method %q", c.Method)
	}
	if c.Stream != nil {
		if c.Stream.Mode != "sse" && c.Stream.Mode != "ndjson" {
			return fmt.Errorf("stream.mode must be \"sse\" or \"ndjson\"")
		}
		if c.Stream.DeltaPath == "" {
			return fmt.Errorf("stream.delta_path is required")
		}
	}
	_, err := compileHTTP(c)
	return err
}

func (c *AgentConfig) Validate() error {
	if err := c.HTTPConfig.Validate(); err != nil {
		return err
	}
	if c.Response.Trace == nil && c.Response.ToolCalls == nil {
		return fmt.Errorf("agent targets need response.trace and/or response.tool_calls")
	}
	return nil
}

// ---- compiled form ----

type mapField struct {
	name string
	path *Path
}

type compiledTools struct {
	path                   *Path
	id, name, args, result *Path
}

type compiledTrace struct {
	path                             *Path
	kind, name, input, output, durMS *Path
}

type compiledMapping struct {
	output, outputJSON, context, inTok, outTok, cost, session *Path
	tools                                                     *compiledTools
	trace                                                     *compiledTrace
	fields                                                    []mapField
}

type hdrTpl struct {
	key string
	tpl *Template
}

type compiledHTTP struct {
	method, url, body *Template
	headers           []hdrTpl
	m                 *compiledMapping
	delta             *Path
}

func cp(src, def string) (*Path, error) {
	if src == "" {
		src = def
	}
	if src == "" {
		return nil, nil
	}
	return CompilePath(src)
}

func compileMapping(rm *ResponseMapping, session *SessionConfig) (*compiledMapping, error) {
	m := &compiledMapping{}
	var err error
	add := func(name, src string, dst **Path) {
		if err != nil || src == "" {
			return
		}
		var p *Path
		if p, err = CompilePath(src); err != nil {
			err = fmt.Errorf("response.%s: %w", name, err)
			return
		}
		*dst = p
		m.fields = append(m.fields, mapField{name, p})
	}
	add("output", rm.Output, &m.output)
	add("output_json", rm.OutputJSON, &m.outputJSON)
	add("context", rm.Context, &m.context)
	add("input_tokens", rm.InputTokens, &m.inTok)
	add("output_tokens", rm.OutputTokens, &m.outTok)
	add("cost_usd", rm.Cost, &m.cost)
	if session != nil {
		add("session", session.Path, &m.session)
	}
	if err != nil {
		return nil, err
	}
	if t := rm.ToolCalls; t != nil {
		ct := &compiledTools{}
		for _, x := range []struct {
			dst      **Path
			src, def string
		}{{&ct.path, t.Path, ""}, {&ct.id, t.ID, "$.id"}, {&ct.name, t.Name, "$.name"}, {&ct.args, t.Arguments, "$.arguments"}, {&ct.result, t.Result, "$.result"}} {
			if *x.dst, err = cp(x.src, x.def); err != nil {
				return nil, fmt.Errorf("response.tool_calls: %w", err)
			}
		}
		if ct.path == nil {
			return nil, fmt.Errorf("response.tool_calls.path is required")
		}
		m.tools = ct
		m.fields = append(m.fields, mapField{"tool_calls", ct.path})
	}
	if t := rm.Trace; t != nil {
		ct := &compiledTrace{}
		for _, x := range []struct {
			dst      **Path
			src, def string
		}{{&ct.path, t.Path, ""}, {&ct.kind, t.Kind, "$.kind"}, {&ct.name, t.Name, "$.name"}, {&ct.input, t.Input, "$.input"}, {&ct.output, t.Output, "$.output"}, {&ct.durMS, t.DurationMS, "$.duration_ms"}} {
			if *x.dst, err = cp(x.src, x.def); err != nil {
				return nil, fmt.Errorf("response.trace: %w", err)
			}
		}
		if ct.path == nil {
			return nil, fmt.Errorf("response.trace.path is required")
		}
		m.trace = ct
		m.fields = append(m.fields, mapField{"trace", ct.path})
	}
	return m, nil
}

func compileHTTP(c *HTTPConfig) (*compiledHTTP, error) {
	ch := &compiledHTTP{}
	var err error
	method := c.Method
	if method == "" {
		method = "POST"
	}
	if ch.method, err = CompileTemplate("method", method); err != nil {
		return nil, err
	}
	if ch.url, err = CompileTemplate("url", c.URL); err != nil {
		return nil, err
	}
	if ch.body, err = CompileTemplate("body", c.Body); err != nil {
		return nil, err
	}
	for k, v := range c.Headers {
		t, err := CompileTemplate("header "+k, v)
		if err != nil {
			return nil, err
		}
		ch.headers = append(ch.headers, hdrTpl{http.CanonicalHeaderKey(k), t})
	}
	if ch.m, err = compileMapping(&c.Response, c.Session); err != nil {
		return nil, err
	}
	if c.Stream != nil {
		if ch.delta, err = CompilePath(c.Stream.DeltaPath); err != nil {
			return nil, fmt.Errorf("stream.delta_path: %w", err)
		}
	}
	return ch, nil
}

// ---- extraction ----

func flatten(vals []any) []any {
	if len(vals) == 1 {
		if a, ok := vals[0].([]any); ok {
			return a
		}
	}
	return vals
}

func argsRaw(v any) json.RawMessage {
	if s, ok := v.(string); ok {
		if t := strings.TrimSpace(s); len(t) > 0 && json.Valid([]byte(t)) {
			return json.RawMessage(t)
		}
	}
	return toRaw(v)
}

// extract applies the mapping to a decoded document, overwriting fields of r
// that have a configured, non-empty match. It returns the session id (if any).
func (m *compiledMapping) extract(doc any, r *evalcore.Response) (session string) {
	if m.output != nil {
		if vals := m.output.Eval(doc); len(vals) == 1 {
			r.Output = asString(vals[0])
		} else if len(vals) > 1 {
			parts := make([]string, len(vals))
			for i, v := range vals {
				parts[i] = asString(v)
			}
			r.Output = strings.Join(parts, "\n")
		}
	}
	if m.outputJSON != nil {
		if v, ok := m.outputJSON.First(doc); ok && v != nil {
			if s, isStr := v.(string); isStr {
				if t := strings.TrimSpace(s); len(t) > 0 && (t[0] == '{' || t[0] == '[') {
					var parsed any
					if json.Unmarshal([]byte(t), &parsed) == nil {
						v = parsed
					}
				}
			}
			r.OutputJSON = v
		}
	}
	if m.context != nil {
		if vals := flatten(m.context.Eval(doc)); len(vals) > 0 {
			r.Context = r.Context[:0:0]
			for _, v := range vals {
				r.Context = append(r.Context, asString(v))
			}
		}
	}
	if m.inTok != nil {
		if v, ok := m.inTok.First(doc); ok {
			r.Usage.InputTokens = asInt(v)
		}
	}
	if m.outTok != nil {
		if v, ok := m.outTok.First(doc); ok {
			r.Usage.OutputTokens = asInt(v)
		}
	}
	if m.cost != nil {
		if v, ok := m.cost.First(doc); ok {
			r.CostUSD = asFloat(v)
		}
	}
	if t := m.tools; t != nil {
		if els := flatten(t.path.Eval(doc)); len(els) > 0 {
			r.ToolCalls = make([]evalcore.ToolCall, 0, len(els))
			for _, e := range els {
				tc := evalcore.ToolCall{}
				if v, ok := t.id.First(e); ok {
					tc.ID = asString(v)
				}
				if v, ok := t.name.First(e); ok {
					tc.Name = asString(v)
				}
				if v, ok := t.args.First(e); ok {
					tc.Arguments = argsRaw(v)
				}
				if v, ok := t.result.First(e); ok {
					tc.Result = argsRaw(v)
				}
				r.ToolCalls = append(r.ToolCalls, tc)
			}
		}
	}
	if t := m.trace; t != nil {
		if els := flatten(t.path.Eval(doc)); len(els) > 0 {
			r.Trace = make([]evalcore.TraceStep, 0, len(els))
			for _, e := range els {
				st := evalcore.TraceStep{Kind: "custom"}
				if v, ok := t.kind.First(e); ok && asString(v) != "" {
					st.Kind = asString(v)
				}
				if v, ok := t.name.First(e); ok {
					st.Name = asString(v)
				}
				if v, ok := t.input.First(e); ok {
					st.Input = toRaw(v)
				}
				if v, ok := t.output.First(e); ok {
					st.Output = toRaw(v)
				}
				if v, ok := t.durMS.First(e); ok {
					st.Duration = time.Duration(asFloat(v) * float64(time.Millisecond))
				}
				r.Trace = append(r.Trace, st)
			}
		}
	}
	if m.session != nil {
		if v, ok := m.session.First(doc); ok {
			session = asString(v)
		}
	}
	return session
}

// mergeStreamEvent copies non-empty mapped fields of an event into the final response.
func mergeStreamEvent(dst, ev *evalcore.Response) {
	if ev.OutputJSON != nil {
		dst.OutputJSON = ev.OutputJSON
	}
	if len(ev.Context) > 0 {
		dst.Context = ev.Context
	}
	if ev.Usage.InputTokens != 0 {
		dst.Usage.InputTokens = ev.Usage.InputTokens
	}
	if ev.Usage.OutputTokens != 0 {
		dst.Usage.OutputTokens = ev.Usage.OutputTokens
	}
	if ev.CostUSD != 0 {
		dst.CostUSD = ev.CostUSD
	}
	if len(ev.ToolCalls) > 0 {
		dst.ToolCalls = ev.ToolCalls
	}
	if len(ev.Trace) > 0 {
		dst.Trace = ev.Trace
	}
}

// ---- adapter ----

type httpAdapter struct {
	cfg *HTTPConfig
	c   *compiledHTTP
	tr  *transport
	ok  func(int) bool
}

func newHTTPAdapter(kind string, cfg *HTTPConfig, deps Deps) (Adapter, error) {
	c, err := compileHTTP(cfg)
	if err != nil {
		return nil, err
	}
	tr, err := newTransport(deps, cfg.HTTPCommon)
	if err != nil {
		return nil, err
	}
	h := &httpAdapter{cfg: cfg, c: c, tr: tr}
	if len(cfg.ExpectStatus) > 0 {
		set := map[int]bool{}
		for _, s := range cfg.ExpectStatus {
			set[s] = true
		}
		h.ok = func(s int) bool { return set[s] }
	}
	return &adapter{kind: kind, call: h.call, dbg: h.debugMapping}, nil
}

func (h *httpAdapter) call(ctx context.Context, tc *turnCtx) (evalcore.Response, string, error) {
	var resp evalcore.Response
	env := newEnv(tc.opts.Secrets)
	data := tc.data(env)

	method, err := h.c.method.Render(data)
	if err != nil {
		return resp, "", permanent("%v", err)
	}
	url, err := h.c.url.Render(data)
	if err != nil {
		return resp, "", permanent("%v", err)
	}
	body, err := h.c.body.Render(data)
	if err != nil {
		return resp, "", permanent("%v", err)
	}
	hdr := make(http.Header, len(h.c.headers)+2)
	for _, ht := range h.c.headers {
		v, err := ht.tpl.Render(data)
		if err != nil {
			return resp, "", permanent("%v", err)
		}
		hdr[ht.key] = []string{strings.TrimSpace(v)}
	}
	if body != "" && hdr["Content-Type"] == nil {
		if t := strings.TrimSpace(body); t != "" && (t[0] == '{' || t[0] == '[') {
			hdr["Content-Type"] = []string{"application/json"}
		}
	}
	if st := h.cfg.Stream; st != nil {
		if hdr["Accept"] == nil && st.Mode == "sse" {
			hdr["Accept"] = []string{"text/event-stream"}
		}
		hdr["Accept-Encoding"] = []string{"identity"}
	}

	wr := wireReq{method: strings.ToUpper(strings.TrimSpace(method)), url: strings.TrimSpace(url), header: hdr, body: []byte(body), ok: h.ok}
	resp.Request = recordRequest(wr.method, wr.url, hdr, wr.body, env.redactor())

	if st := h.cfg.Stream; st != nil {
		return h.callStream(ctx, env, wr, resp)
	}
	wresp, err := h.tr.do(ctx, env, wr, nil)
	resp.Latency = wresp.latency
	if err != nil {
		return resp, "", err
	}
	resp.Raw = rawJSON(wresp.body)
	m := h.c.m
	var session string
	if len(m.fields) == 0 {
		resp.Output = string(wresp.body)
		return resp, "", nil
	}
	var doc any
	if err := json.Unmarshal(wresp.body, &doc); err != nil {
		if m.output == nil && len(m.fields) == 0 {
			resp.Output = string(wresp.body)
			return resp, "", nil
		}
		return resp, "", permanent("response is not valid JSON: %v", err)
	}
	if m.output == nil {
		resp.Output = string(wresp.body)
	}
	session = m.extract(doc, &resp)
	return resp, session, nil
}

type cappedBuf struct {
	b []byte
}

func (c *cappedBuf) Write(p []byte) (int, error) {
	if room := rawLimit - len(c.b); room > 0 {
		if len(p) > room {
			c.b = append(c.b, p[:room]...)
		} else {
			c.b = append(c.b, p...)
		}
	}
	return len(p), nil
}

func (h *httpAdapter) callStream(ctx context.Context, env *renderEnv, wr wireReq, resp evalcore.Response) (evalcore.Response, string, error) {
	st := h.cfg.Stream
	sentinel := st.DoneSentinel
	if sentinel == "" {
		sentinel = "[DONE]"
	}
	var out strings.Builder
	var session, fallbackOutput string
	m := h.c.m
	handle := func(data []byte, first func()) (bool, error) {
		var doc any
		if err := json.Unmarshal(data, &doc); err != nil {
			return false, nil // non-JSON event (keep-alive, comment): ignore
		}
		got := false
		for _, v := range h.c.delta.Eval(doc) {
			if s := asString(v); s != "" {
				out.WriteString(s)
				got = true
			}
		}
		if got {
			first()
		}
		var ev evalcore.Response
		if s := m.extract(doc, &ev); s != "" {
			session = s
		}
		if ev.Output != "" {
			fallbackOutput = ev.Output
		}
		mergeStreamEvent(&resp, &ev)
		return false, nil
	}
	var capture cappedBuf
	stream := func(body io.Reader, first func()) error {
		r := io.TeeReader(body, &capture)
		mark := func() { first() }
		if st.Mode == "sse" {
			return readSSE(r, func(event string, data []byte) (bool, error) {
				if st.Event != "" && event != st.Event {
					return false, nil
				}
				if string(bytes.TrimSpace(data)) == sentinel {
					return true, nil
				}
				return handle(data, mark)
			})
		}
		return readNDJSON(r, func(line []byte) (bool, error) { return handle(line, mark) })
	}
	wresp, err := h.tr.do(ctx, env, wr, stream)
	resp.Latency, resp.TTFT = wresp.latency, wresp.ttft
	if err != nil {
		return resp, "", err
	}
	resp.Output = out.String()
	if resp.Output == "" {
		resp.Output = fallbackOutput
	}
	b, _ := json.Marshal(string(capture.b))
	resp.Raw = b
	return resp, session, nil
}

// MappingResult is one row of the mapping debug table shown by TestConnection.
type MappingResult struct {
	Field   string `json:"field"`
	Path    string `json:"path"`
	Matched bool   `json:"matched"`
	Value   any    `json:"value,omitempty"`
}

func (h *httpAdapter) debugMapping(raw []byte) []MappingResult {
	if h.cfg.Stream != nil || len(h.c.m.fields) == 0 {
		return nil
	}
	var doc any
	if json.Unmarshal(raw, &doc) != nil {
		return nil
	}
	var out []MappingResult
	for _, f := range h.c.m.fields {
		vals := f.path.Eval(doc)
		r := MappingResult{Field: f.name, Path: f.path.String(), Matched: len(vals) > 0}
		switch len(vals) {
		case 0:
		case 1:
			r.Value = vals[0]
		default:
			r.Value = vals
		}
		out = append(out, r)
	}
	return out
}
