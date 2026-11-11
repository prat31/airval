// Package mock is a fast, configurable fake AI service for tests, benchmarks
// and demos. Answers are deterministic functions of the input, so evaluators
// can be tested against a target whose accuracy is known.
//
// Endpoints:
//
//	POST /v1/chat/completions  OpenAI-compatible (stream and non-stream, tool calls when tools are passed,
//	                           json_object mode returns {answer,score,passed,reason}, logprobs)
//	ANY  /echo                 echoes the input; ?stream=sse|ndjson streams word deltas
//	POST /rag                  {question} -> {answer, context:[{id,text,score}], usage}
//	POST /agent                {question} -> {output, tool_calls, trace, usage}
//	POST /chat                 {session_id?, message} -> {session_id, reply, turn, done}
//	GET  /healthz
//
// Per-request overrides: ?mock_status=503, ?mock_latency_ms=250, ?mock_accuracy=0.9 and
// ?mock_variant=v2 (or the X-Mock-Status / X-Mock-Latency-Ms / X-Mock-Accuracy / X-Mock-Variant
// headers). A variant re-draws which questions are answered correctly, so two variants disagree
// on some questions (regressions and improvements in a comparison).
package mock

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"math"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Config tunes the mock's behaviour.
type Config struct {
	LatencyMean   time.Duration // mean artificial latency per request
	LatencyJitter time.Duration // uniform +/- jitter around the mean
	ErrorRate     float64       // fraction of requests failing with 500/502/503
	RateLimitRate float64       // fraction of requests answered 429 + Retry-After
	RetryAfter    time.Duration // Retry-After advertised on 429 (default 1s)
	// Accuracy is the fraction of questions answered correctly (decided by a
	// hash of the question, so it is stable across runs). 0 means the default
	// 0.8; a negative value means 0%.
	Accuracy   float64
	Seed       uint64        // seeds error/latency randomness
	ChunkDelay time.Duration // delay between streamed chunks (makes TTFT observable)
}

// DefaultConfig: no latency, no errors, 80% accuracy.
func DefaultConfig() Config { return Config{Accuracy: 0.8, RetryAfter: time.Second} }

type server struct {
	cfg      Config
	ctr      atomic.Uint64
	sessMu   sync.Mutex
	sessions map[string]int
}

// NewServer returns the mock as an http.Handler.
func NewServer(cfg Config) http.Handler {
	if cfg.Accuracy == 0 {
		cfg.Accuracy = 0.8
	} else if cfg.Accuracy < 0 {
		cfg.Accuracy = 0
	}
	if cfg.RetryAfter <= 0 {
		cfg.RetryAfter = time.Second
	}
	s := &server{cfg: cfg, sessions: map[string]int{}}
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "ok") })
	mux.HandleFunc("POST /v1/chat/completions", s.chaos(s.chatCompletions))
	mux.HandleFunc("/echo", s.chaos(s.echo))
	mux.HandleFunc("POST /rag", s.chaos(s.rag))
	mux.HandleFunc("POST /agent", s.chaos(s.agent))
	mux.HandleFunc("POST /chat", s.chaos(s.chat))
	return mux
}

// ---- deterministic oracle ----

func hash64(s string) uint64 {
	h := fnv.New64a()
	h.Write([]byte(s))
	return h.Sum64()
}

// Truth is the ground-truth answer for a question; dataset generators can use
// it to build `expected` values.
func Truth(question string) string {
	return fmt.Sprintf("The answer is %08x.", uint32(hash64(question)))
}

// IsCorrect reports whether the mock answers question correctly at the given
// accuracy (same 0/negative semantics as Config.Accuracy).
func IsCorrect(question string, accuracy float64) bool {
	if accuracy == 0 {
		accuracy = 0.8
	}
	if accuracy < 0 {
		accuracy = 0
	}
	return float64(hash64("correct:"+question)%10000)/10000 < accuracy
}

// IsCorrectVariant is IsCorrect for a named variant ("" is the default variant, same as IsCorrect).
func IsCorrectVariant(question string, accuracy float64, variant string) bool {
	if variant == "" {
		return IsCorrect(question, accuracy)
	}
	return IsCorrect(variant+":"+question, accuracy)
}

// answer returns the mock's answer to question, honouring per-request accuracy/variant overrides.
func (s *server) answer(r *http.Request, question, expected string) string {
	acc, variant := s.cfg.Accuracy, firstNonEmpty(r.URL.Query().Get("mock_variant"), r.Header.Get("X-Mock-Variant"))
	if v := firstNonEmpty(r.URL.Query().Get("mock_accuracy"), r.Header.Get("X-Mock-Accuracy")); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f >= 0 && f <= 1 {
			acc = f
			if f == 0 {
				acc = -1
			}
		}
	}
	if IsCorrectVariant(question, acc, variant) {
		if expected != "" {
			return expected
		}
		return Truth(question)
	}
	return fmt.Sprintf("The answer is %08x.", uint32(hash64("wrong:"+question)))
}

func tokens(s string) int { return len(strings.Fields(s)) }

type usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// ---- chaos middleware ----

func (s *server) rng() *rand.Rand {
	return rand.New(rand.NewPCG(s.cfg.Seed, s.ctr.Add(1)))
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return true
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-ctx.Done():
		return false
	}
}

func (s *server) chaos(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rg := s.rng()
		delay := s.cfg.LatencyMean
		if j := s.cfg.LatencyJitter; j > 0 {
			delay += time.Duration((rg.Float64()*2 - 1) * float64(j))
		}
		if v := firstNonEmpty(r.URL.Query().Get("mock_latency_ms"), r.Header.Get("X-Mock-Latency-Ms")); v != "" {
			if ms, err := strconv.Atoi(v); err == nil {
				delay = time.Duration(ms) * time.Millisecond
			}
		}
		if !sleepCtx(r.Context(), delay) {
			return
		}
		if v := firstNonEmpty(r.URL.Query().Get("mock_status"), r.Header.Get("X-Mock-Status")); v != "" {
			if code, err := strconv.Atoi(v); err == nil && code >= 400 {
				s.fail(w, code)
				return
			}
		}
		if s.cfg.RateLimitRate > 0 && rg.Float64() < s.cfg.RateLimitRate {
			s.fail(w, http.StatusTooManyRequests)
			return
		}
		if s.cfg.ErrorRate > 0 && rg.Float64() < s.cfg.ErrorRate {
			s.fail(w, []int{500, 502, 503}[rg.IntN(3)])
			return
		}
		next(w, r)
	}
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if s != "" {
			return s
		}
	}
	return ""
}

func (s *server) fail(w http.ResponseWriter, code int) {
	if code == http.StatusTooManyRequests {
		w.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(s.cfg.RetryAfter.Seconds()))))
	}
	writeJSON(w, code, map[string]any{"error": map[string]any{
		"message": fmt.Sprintf("mock injected error %d", code), "type": "mock_error", "code": code,
	}})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func readJSON(r *http.Request, v any) error {
	b, err := io.ReadAll(io.LimitReader(r.Body, 8<<20))
	if err != nil {
		return err
	}
	if len(b) == 0 {
		return nil
	}
	return json.Unmarshal(b, v)
}

// ---- streaming helpers ----

func words(s string) []string {
	f := strings.Fields(s)
	for i := range f {
		if i < len(f)-1 {
			f[i] += " "
		}
	}
	return f
}

type streamer struct {
	w     http.ResponseWriter
	fl    http.Flusher
	sse   bool
	delay time.Duration
	ctx   context.Context
}

func (s *server) newStreamer(w http.ResponseWriter, r *http.Request, sse bool) *streamer {
	fl, _ := w.(http.Flusher)
	if sse {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
	} else {
		w.Header().Set("Content-Type", "application/x-ndjson")
	}
	w.WriteHeader(http.StatusOK)
	return &streamer{w: w, fl: fl, sse: sse, delay: s.cfg.ChunkDelay, ctx: r.Context()}
}

func (st *streamer) event(v any) bool {
	b, _ := json.Marshal(v)
	return st.raw(string(b))
}

func (st *streamer) raw(data string) bool {
	if st.sse {
		fmt.Fprintf(st.w, "data: %s\n\n", data)
	} else {
		fmt.Fprintf(st.w, "%s\n", data)
	}
	if st.fl != nil {
		st.fl.Flush()
	}
	return sleepCtx(st.ctx, st.delay)
}

// ---- /v1/chat/completions ----

type oaMessage struct {
	Role       string          `json:"role"`
	Content    json.RawMessage `json:"content"`
	ToolCallID string          `json:"tool_call_id"`
}

func (m oaMessage) text() string {
	var s string
	if json.Unmarshal(m.Content, &s) == nil {
		return s
	}
	var parts []struct {
		Text string `json:"text"`
	}
	if json.Unmarshal(m.Content, &parts) == nil {
		var b strings.Builder
		for _, p := range parts {
			b.WriteString(p.Text)
		}
		return b.String()
	}
	return ""
}

func (s *server) chatCompletions(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Model         string      `json:"model"`
		Messages      []oaMessage `json:"messages"`
		Stream        bool        `json:"stream"`
		StreamOptions struct {
			IncludeUsage bool `json:"include_usage"`
		} `json:"stream_options"`
		Tools []struct {
			Function struct {
				Name string `json:"name"`
			} `json:"function"`
		} `json:"tools"`
		ResponseFormat struct {
			Type string `json:"type"`
		} `json:"response_format"`
		Logprobs bool `json:"logprobs"`
	}
	if err := readJSON(r, &req); err != nil {
		writeJSON(w, 400, map[string]any{"error": map[string]any{"message": "invalid JSON: " + err.Error(), "type": "invalid_request_error"}})
		return
	}
	if len(req.Messages) == 0 {
		writeJSON(w, 400, map[string]any{"error": map[string]any{"message": "messages is required", "type": "invalid_request_error"}})
		return
	}
	var question, prompt string
	sawTool := false
	for _, m := range req.Messages {
		prompt += m.text() + " "
		switch m.Role {
		case "user":
			question = m.text()
		case "tool":
			sawTool = true
		}
	}
	id := fmt.Sprintf("chatcmpl-%d", s.ctr.Add(1))
	created := time.Now().Unix()
	inTok := tokens(prompt)

	// Tool call turn.
	if len(req.Tools) > 0 && !sawTool && req.Tools[0].Function.Name != "" {
		name := req.Tools[0].Function.Name
		args, _ := json.Marshal(map[string]string{"query": question})
		tc := map[string]any{"id": "call_" + strconv.FormatUint(hash64(question)%100000, 10), "type": "function",
			"function": map[string]any{"name": name, "arguments": string(args)}}
		if req.Stream {
			st := s.newStreamer(w, r, true)
			st.event(oaChunk(id, created, req.Model, map[string]any{"role": "assistant", "content": nil}, nil))
			half := len(args) / 2
			st.event(oaChunk(id, created, req.Model, map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": tc["id"], "type": "function",
				"function": map[string]any{"name": name, "arguments": string(args[:half])}}}}, nil))
			st.event(oaChunk(id, created, req.Model, map[string]any{"tool_calls": []any{map[string]any{"index": 0,
				"function": map[string]any{"arguments": string(args[half:])}}}}, nil))
			st.event(oaChunk(id, created, req.Model, map[string]any{}, "tool_calls"))
			if req.StreamOptions.IncludeUsage {
				st.event(map[string]any{"id": id, "object": "chat.completion.chunk", "created": created, "model": req.Model, "choices": []any{},
					"usage": map[string]int{"prompt_tokens": inTok, "completion_tokens": 8, "total_tokens": inTok + 8}})
			}
			st.raw("[DONE]")
			return
		}
		writeJSON(w, 200, map[string]any{"id": id, "object": "chat.completion", "created": created, "model": req.Model,
			"choices": []any{map[string]any{"index": 0, "finish_reason": "tool_calls",
				"message": map[string]any{"role": "assistant", "content": nil, "tool_calls": []any{tc}}}},
			"usage": map[string]int{"prompt_tokens": inTok, "completion_tokens": 8, "total_tokens": inTok + 8}})
		return
	}

	content := s.answer(r, question, "")
	if sawTool {
		content = "Based on the tool result: " + content
	}
	if req.ResponseFormat.Type == "json_object" || req.ResponseFormat.Type == "json_schema" {
		score := float64(hash64("judge:"+question+prompt)%101) / 100
		b, _ := json.Marshal(map[string]any{"answer": content, "score": score, "passed": score >= 0.5, "reason": "mock verdict"})
		content = string(b)
	}
	outTok := tokens(content)

	if req.Stream {
		st := s.newStreamer(w, r, true)
		st.event(oaChunk(id, created, req.Model, map[string]any{"role": "assistant", "content": ""}, nil))
		for _, wd := range words(content) {
			if !st.event(oaChunk(id, created, req.Model, map[string]any{"content": wd}, nil)) {
				return
			}
		}
		st.event(oaChunk(id, created, req.Model, map[string]any{}, "stop"))
		if req.StreamOptions.IncludeUsage {
			st.event(map[string]any{"id": id, "object": "chat.completion.chunk", "created": created, "model": req.Model, "choices": []any{},
				"usage": map[string]int{"prompt_tokens": inTok, "completion_tokens": outTok, "total_tokens": inTok + outTok}})
		}
		st.raw("[DONE]")
		return
	}
	choice := map[string]any{"index": 0, "finish_reason": "stop", "message": map[string]any{"role": "assistant", "content": content}}
	if req.Logprobs {
		choice["logprobs"] = map[string]any{"content": []any{map[string]any{"token": "Yes", "logprob": -0.1,
			"top_logprobs": []any{map[string]any{"token": "Yes", "logprob": -0.1}, map[string]any{"token": "No", "logprob": -2.4}}}}}
	}
	writeJSON(w, 200, map[string]any{"id": id, "object": "chat.completion", "created": created, "model": req.Model,
		"choices": []any{choice},
		"usage":   map[string]int{"prompt_tokens": inTok, "completion_tokens": outTok, "total_tokens": inTok + outTok}})
}

func oaChunk(id string, created int64, model string, delta map[string]any, finish any) map[string]any {
	return map[string]any{"id": id, "object": "chat.completion.chunk", "created": created, "model": model,
		"choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": finish}}}
}

// ---- generic endpoints ----

// question extracts the question text from a generic JSON body.
func question(body map[string]any, raw string) string {
	for _, k := range []string{"question", "input", "prompt", "query", "message", "text"} {
		if s, ok := body[k].(string); ok && s != "" {
			return s
		}
	}
	if in, ok := body["input"].(map[string]any); ok {
		return question(in, raw)
	}
	return raw
}

func (s *server) streamAnswer(w http.ResponseWriter, r *http.Request, answer string, u usage) {
	mode := r.URL.Query().Get("stream")
	st := s.newStreamer(w, r, mode == "sse")
	for _, wd := range words(answer) {
		if !st.event(map[string]any{"delta": wd}) {
			return
		}
	}
	st.event(map[string]any{"done": true, "usage": u, "output": answer})
}

func (s *server) echo(w http.ResponseWriter, r *http.Request) {
	rawb, _ := io.ReadAll(io.LimitReader(r.Body, 8<<20))
	raw := string(rawb)
	var body map[string]any
	_ = json.Unmarshal(rawb, &body)
	q := question(body, raw)
	if q == "" {
		q = r.URL.Query().Get("q")
	}
	u := usage{tokens(q), tokens(q)}
	if m := r.URL.Query().Get("stream"); m == "sse" || m == "ndjson" {
		s.streamAnswer(w, r, q, u)
		return
	}
	var received any = raw
	if body != nil {
		received = body
	}
	writeJSON(w, 200, map[string]any{"output": q, "received": received, "method": r.Method, "usage": u})
}

func (s *server) rag(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, 400, map[string]any{"error": "invalid JSON"})
		return
	}
	q := question(body, "")
	expected, _ := body["expected"].(string)
	ans := s.answer(r, q, expected)
	docs := []map[string]any{
		{"id": "doc-gold", "text": fmt.Sprintf("Regarding %q: %s", q, Truth(q)), "score": 0.93},
		{"id": "doc-2", "text": "Unrelated reference material about " + strings.ToLower(q[:min(len(q), 24)]), "score": 0.41},
		{"id": "doc-3", "text": "General background document.", "score": 0.22},
	}
	u := usage{tokens(q) + 60, tokens(ans)}
	// Priced like a small hosted model ($0.15 / $0.60 per million input / output tokens).
	cost := (float64(u.InputTokens)*0.15 + float64(u.OutputTokens)*0.60) / 1e6
	writeJSON(w, 200, map[string]any{"answer": ans, "context": docs, "usage": u, "cost_usd": cost})
}

func (s *server) agent(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, 400, map[string]any{"error": "invalid JSON"})
		return
	}
	q := question(body, "")
	expected, _ := body["expected"].(string)
	ans := s.answer(r, q, expected)
	args := map[string]string{"query": q}
	result := Truth(q)
	writeJSON(w, 200, map[string]any{
		"output":     ans,
		"tool_calls": []any{map[string]any{"id": "call_1", "name": "search", "arguments": args, "result": result}},
		"trace": []any{
			map[string]any{"kind": "llm", "name": "plan", "input": q, "output": "call search", "duration_ms": 12},
			map[string]any{"kind": "tool", "name": "search", "input": args, "output": result, "duration_ms": 30},
			map[string]any{"kind": "llm", "name": "answer", "input": result, "output": ans, "duration_ms": 18},
		},
		"usage": usage{tokens(q) + 80, tokens(ans) + 12},
	})
}

func (s *server) chat(w http.ResponseWriter, r *http.Request) {
	var body struct {
		SessionID string `json:"session_id"`
		Message   string `json:"message"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, 400, map[string]any{"error": "invalid JSON"})
		return
	}
	s.sessMu.Lock()
	if body.SessionID == "" {
		body.SessionID = fmt.Sprintf("sess-%d", s.ctr.Add(1))
	}
	turn := s.sessions[body.SessionID]
	s.sessions[body.SessionID] = turn + 1
	if len(s.sessions) > 100000 { // bound memory in long benchmarks
		s.sessions = map[string]int{body.SessionID: turn + 1}
	}
	s.sessMu.Unlock()

	low := strings.ToLower(body.Message)
	done := strings.Contains(low, "bye") || strings.Contains(low, "thanks")
	var reply string
	switch {
	case done:
		reply = "Goodbye! Glad I could help."
	case turn == 0:
		reply = "Hello! " + s.answer(r, body.Message, "")
	default:
		reply = fmt.Sprintf("(turn %d) %s", turn+1, s.answer(r, body.Message, ""))
	}
	u := usage{tokens(body.Message), tokens(reply)}
	if m := r.URL.Query().Get("stream"); m == "sse" || m == "ndjson" {
		s.streamAnswer(w, r, reply, u)
		return
	}
	writeJSON(w, 200, map[string]any{"session_id": body.SessionID, "reply": reply, "turn": turn + 1, "done": done, "usage": u})
}
