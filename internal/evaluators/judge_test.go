package evaluators

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"sync"
	"testing"

	"github.com/airval/airval/internal/evalcore"
)

// fakeLLM records requests and answers with respond(req, callIndex).
type fakeLLM struct {
	mu      sync.Mutex
	calls   []evalcore.LLMRequest
	respond func(req evalcore.LLMRequest, n int) (evalcore.LLMResponse, error)
}

func (f *fakeLLM) Complete(_ context.Context, req evalcore.LLMRequest) (evalcore.LLMResponse, error) {
	f.mu.Lock()
	n := len(f.calls)
	f.calls = append(f.calls, req)
	f.mu.Unlock()
	return f.respond(req, n)
}

func (f *fakeLLM) n() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func replies(rs ...string) func(evalcore.LLMRequest, int) (evalcore.LLMResponse, error) {
	return func(_ evalcore.LLMRequest, n int) (evalcore.LLMResponse, error) {
		if n >= len(rs) {
			n = len(rs) - 1
		}
		return evalcore.LLMResponse{Content: rs[n], CostUSD: 0.001}, nil
	}
}

func depsFor(llm evalcore.LLM, cache Cache) Deps {
	return Deps{
		LLMs: func(provider, model, secret string) (evalcore.LLM, error) {
			if provider == "broken" {
				return nil, errors.New("no such provider")
			}
			return llm, nil
		},
		Cache: cache,
	}
}

const judgeCfg = `{"provider":"openai","model":"gpt-test"}`

func ragInput() Input {
	return Input{
		Example: &evalcore.Example{
			ID:       "e1",
			Input:    map[string]any{"question": "What is the capital of France?"},
			Expected: "Paris",
		},
		Response: &evalcore.Response{Output: "Paris is the capital.", Context: []string{"Paris is the capital of France."}},
	}
}

func evalJudge(t *testing.T, kind, cfg string, llm *fakeLLM, cache Cache, in Input) (evalcore.Score, error) {
	t.Helper()
	ev, err := New(kind, json.RawMessage(cfg), depsFor(llm, cache))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if ev.Cost() != CostModel {
		t.Fatalf("%s should be CostModel", kind)
	}
	return ev.Evaluate(context.Background(), in)
}

func TestPresetJudgeBasic(t *testing.T) {
	llm := &fakeLLM{respond: replies(`{"reason":"Matches the reference.","score":5}`)}
	s, err := evalJudge(t, "correctness", judgeCfg, llm, nil, ragInput())
	if err != nil {
		t.Fatal(err)
	}
	if *s.Value != 1 || !*s.Passed || s.Reason != "Matches the reference." {
		t.Fatalf("score = %+v", s)
	}
	if s.CostUSD != 0.001 {
		t.Fatalf("cost = %v", s.CostUSD)
	}
	req := llm.calls[0]
	if req.Temperature != 0 || !req.JSONMode || req.Model != "gpt-test" || !req.Logprobs {
		t.Fatalf("request = %+v", req)
	}
	if !strings.Contains(req.System, "impartial") {
		t.Fatal("system prompt missing")
	}
	user := req.Messages[0].Content
	for _, want := range []string{"<expected>\nParis\n</expected>", "What is the capital of France?", "Paris is the capital.", `"score": <integer from 1 to 5>`} {
		if !strings.Contains(user, want) {
			t.Errorf("prompt missing %q:\n%s", want, user)
		}
	}
	if s.Meta[MetaPromptVersion] != "correctness@1" {
		t.Fatalf("meta = %v", s.Meta)
	}
}

func TestPresetRequires(t *testing.T) {
	llm := &fakeLLM{respond: replies(`{"score":5}`)}
	in := ragInput()
	in.Example.Expected = nil
	if _, err := evalJudge(t, "correctness", judgeCfg, llm, nil, in); err == nil || !strings.Contains(err.Error(), "expected output") {
		t.Fatalf("want missing expected error, got %v", err)
	}
	in = ragInput()
	in.Response.Context = nil
	if _, err := evalJudge(t, "faithfulness", judgeCfg, llm, nil, in); err == nil || !strings.Contains(err.Error(), "context") {
		t.Fatalf("want missing context error, got %v", err)
	}
	// Example context is the fallback.
	in.Example.Context = []string{"gold"}
	if _, err := evalJudge(t, "faithfulness", judgeCfg, llm, nil, in); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(llm.calls[len(llm.calls)-1].Messages[0].Content, "[1] gold") {
		t.Fatal("context not rendered")
	}
	if llm.n() != 1 {
		t.Fatalf("missing data must not call the LLM; calls = %d", llm.n())
	}
}

func TestAllPresetPromptsRender(t *testing.T) {
	in := ragInput()
	in.Response.ToolCalls = []evalcore.ToolCall{{Name: "lookup", Arguments: json.RawMessage(`{"city":"Paris"}`), Result: json.RawMessage(`"ok"`)}}
	in.Response.Transcript = []evalcore.Message{{Role: "user", Content: "hi"}, {Role: "assistant", Content: "Paris is the capital."}}
	in.Example.Metadata = map[string]any{"goal": "learn the capital"}
	for _, p := range presets {
		llm := &fakeLLM{respond: replies(`{"reason":"r","score":4}`)}
		s, err := evalJudge(t, p.kind, judgeCfg, llm, nil, in)
		if err != nil {
			t.Fatalf("%s: %v", p.kind, err)
		}
		if *s.Value != 0.75 {
			t.Fatalf("%s: value %v", p.kind, *s.Value)
		}
		user := llm.calls[0].Messages[0].Content
		if strings.Contains(user, "<no value>") || !strings.Contains(user, `"score"`) {
			t.Fatalf("%s: bad prompt:\n%s", p.kind, user)
		}
		info, _ := Info(p.kind)
		if info.Prompt == "" || info.PromptVersion == "" {
			t.Fatalf("%s: catalog prompt missing", p.kind)
		}
	}
}

func TestLogprobWeighting(t *testing.T) {
	llm := &fakeLLM{respond: func(req evalcore.LLMRequest, n int) (evalcore.LLMResponse, error) {
		return evalcore.LLMResponse{
			Content:     `{"reason":"ok","score":4}`,
			TopLogprobs: map[string]float64{"4": math.Log(0.6), " 5": math.Log(0.4), "{": math.Log(0.001)},
		}, nil
	}}
	s, err := evalJudge(t, "helpfulness", judgeCfg, llm, nil, ragInput())
	if err != nil {
		t.Fatal(err)
	}
	// weighted = 0.6*4 + 0.4*5 = 4.4 → (4.4-1)/4 = 0.85
	if math.Abs(*s.Value-0.85) > 1e-9 || s.Meta["logprob_weighted"] != true {
		t.Fatalf("value = %v meta = %v", *s.Value, s.Meta)
	}
	// Off: integer score is used.
	s, _ = evalJudge(t, "helpfulness", `{"provider":"p","model":"m","logprobs":"off"}`, llm, nil, ragInput())
	if *s.Value != 0.75 {
		t.Fatalf("off: value = %v", *s.Value)
	}
	// Auto with non-score first-token alternatives: integer score.
	llm2 := &fakeLLM{respond: func(evalcore.LLMRequest, int) (evalcore.LLMResponse, error) {
		return evalcore.LLMResponse{Content: `{"reason":"ok","score":2}`, TopLogprobs: map[string]float64{"{": 0}}, nil
	}}
	s, _ = evalJudge(t, "helpfulness", judgeCfg, llm2, nil, ragInput())
	if *s.Value != 0.25 {
		t.Fatalf("auto fallback: value = %v", *s.Value)
	}
}

func TestLogprobTwoPass(t *testing.T) {
	llm := &fakeLLM{respond: func(req evalcore.LLMRequest, n int) (evalcore.LLMResponse, error) {
		if n == 0 {
			if req.Logprobs {
				t.Error("two_pass: the reasoning call should not request logprobs")
			}
			return evalcore.LLMResponse{Content: `{"reason":"solid","score":3}`, CostUSD: 0.01}, nil
		}
		if req.MaxTokens != 1 || !req.Logprobs || len(req.Messages) != 3 || req.Messages[1].Role != "assistant" {
			t.Errorf("bad follow-up request %+v", req)
		}
		return evalcore.LLMResponse{Content: "3", CostUSD: 0.001, TopLogprobs: map[string]float64{"3": math.Log(0.5), "2": math.Log(0.5)}}, nil
	}}
	s, err := evalJudge(t, "conciseness", `{"provider":"p","model":"m","logprobs":"two_pass"}`, llm, nil, ragInput())
	if err != nil {
		t.Fatal(err)
	}
	// 2.5 → 0.375
	if math.Abs(*s.Value-0.375) > 1e-9 || llm.n() != 2 || math.Abs(s.CostUSD-0.011) > 1e-12 {
		t.Fatalf("value = %v calls = %d cost = %v", *s.Value, llm.n(), s.CostUSD)
	}
}

func TestJudgeRepair(t *testing.T) {
	llm := &fakeLLM{respond: replies("The answer looks great, I'd give it a high score.", `{"reason":"fixed","score":4}`)}
	s, err := evalJudge(t, "correctness", judgeCfg, llm, nil, ragInput())
	if err != nil {
		t.Fatal(err)
	}
	if *s.Value != 0.75 || llm.n() != 2 {
		t.Fatalf("value %v calls %d", *s.Value, llm.n())
	}
	rep := llm.calls[1]
	if len(rep.Messages) != 3 || !strings.Contains(rep.Messages[2].Content, "could not be parsed") || rep.Logprobs {
		t.Fatalf("bad repair request %+v", rep.Messages)
	}
	if math.Abs(s.CostUSD-0.002) > 1e-12 {
		t.Fatalf("repair cost not counted: %v", s.CostUSD)
	}

	// Out-of-range score also triggers repair.
	llm = &fakeLLM{respond: replies(`{"score":9}`, `{"score":"5/5","reason":"x"}`)}
	s, err = evalJudge(t, "correctness", judgeCfg, llm, nil, ragInput())
	if err != nil || *s.Value != 1 || llm.n() != 2 {
		t.Fatalf("out-of-range score should be repaired: %+v %v", s, err)
	}

	llm = &fakeLLM{respond: replies("nope", "still nope")}
	if _, err := evalJudge(t, "correctness", judgeCfg, llm, nil, ragInput()); err == nil || !strings.Contains(err.Error(), "after one repair") {
		t.Fatalf("want repair failure, got %v", err)
	}
	if llm.n() != 2 {
		t.Fatalf("exactly one repair retry expected, calls = %d", llm.n())
	}
}

func TestJudgeLLMError(t *testing.T) {
	llm := &fakeLLM{respond: func(evalcore.LLMRequest, int) (evalcore.LLMResponse, error) {
		return evalcore.LLMResponse{}, errors.New("429 rate limited")
	}}
	_, err := evalJudge(t, "correctness", judgeCfg, llm, nil, ragInput())
	if err == nil || !strings.Contains(err.Error(), "429") {
		t.Fatalf("got %v", err)
	}
}

func TestJudgeVoting(t *testing.T) {
	var mu sync.Mutex
	scores := []string{"2", "5", "4"}
	i := 0
	llm := &fakeLLM{respond: func(evalcore.LLMRequest, int) (evalcore.LLMResponse, error) {
		mu.Lock()
		defer mu.Unlock()
		s := scores[i%3]
		i++
		return evalcore.LLMResponse{Content: `{"reason":"r` + s + `","score":` + s + `}`, CostUSD: 0.01}, nil
	}}
	s, err := evalJudge(t, "helpfulness", `{"provider":"p","model":"m","samples":3,"temperature":0.7,"logprobs":"off"}`, llm, nil, ragInput())
	if err != nil {
		t.Fatal(err)
	}
	if *s.Value != 0.75 || s.Reason != "r4" || llm.n() != 3 {
		t.Fatalf("value %v reason %q calls %d", *s.Value, s.Reason, llm.n())
	}
	if llm.calls[0].Temperature != 0.7 {
		t.Fatal("temperature not passed")
	}
	if got := s.Meta["samples"].([]float64); len(got) != 3 {
		t.Fatalf("samples meta = %v", got)
	}
	if math.Abs(s.CostUSD-0.03) > 1e-12 {
		t.Fatalf("cost %v", s.CostUSD)
	}
}

func TestJudgeVotingPartialFailure(t *testing.T) {
	llm := &fakeLLM{respond: func(_ evalcore.LLMRequest, n int) (evalcore.LLMResponse, error) {
		if n == 0 {
			return evalcore.LLMResponse{}, errors.New("timeout")
		}
		return evalcore.LLMResponse{Content: `{"score":5}`}, nil
	}}
	s, err := evalJudge(t, "helpfulness", `{"provider":"p","model":"m","samples":3}`, llm, nil, ragInput())
	if err != nil {
		t.Fatal(err)
	}
	if *s.Value != 1 || s.Meta["sample_errors"] != 1 {
		t.Fatalf("score %+v", s)
	}
}

func TestJudgeCache(t *testing.T) {
	cache := NewMemoryCache(100)
	llm := &fakeLLM{respond: replies(`{"reason":"r","score":4}`)}
	s1, err := evalJudge(t, "correctness", judgeCfg, llm, cache, ragInput())
	if err != nil {
		t.Fatal(err)
	}
	s2, err := evalJudge(t, "correctness", judgeCfg, llm, cache, ragInput())
	if err != nil {
		t.Fatal(err)
	}
	if llm.n() != 1 {
		t.Fatalf("second evaluation should hit the cache; calls = %d", llm.n())
	}
	if *s2.Value != *s1.Value || s2.Reason != s1.Reason || s2.CostUSD != 0 || s2.Meta[MetaCached] != true {
		t.Fatalf("cached score = %+v", s2)
	}
	// Different output → different key.
	in := ragInput()
	in.Response.Output = "Lyon"
	_, _ = evalJudge(t, "correctness", judgeCfg, llm, cache, in)
	// Different judge kind with same data → different key.
	_, _ = evalJudge(t, "helpfulness", judgeCfg, llm, cache, ragInput())
	// Different model → different key.
	_, _ = evalJudge(t, "correctness", `{"provider":"openai","model":"other"}`, llm, cache, ragInput())
	// no_cache bypasses.
	_, _ = evalJudge(t, "correctness", `{"provider":"openai","model":"gpt-test","no_cache":true}`, llm, cache, ragInput())
	if llm.n() != 5 {
		t.Fatalf("calls = %d, want 5", llm.n())
	}
}

func TestJudgeConfigErrors(t *testing.T) {
	for _, cfg := range []string{
		`{}`, `{"provider":"p"}`, `{"provider":"p","model":"m","samples":10}`,
		`{"provider":"p","model":"m","temperature":3}`, `{"provider":"p","model":"m","logprobs":"maybe"}`,
	} {
		if err := ValidateConfig("correctness", json.RawMessage(cfg)); err == nil {
			t.Errorf("%s: want error", cfg)
		}
	}
	if _, err := New("correctness", json.RawMessage(judgeCfg), Deps{}); !errors.Is(err, ErrNoLLM) {
		t.Fatalf("want ErrNoLLM, got %v", err)
	}
	if _, err := New("correctness", json.RawMessage(`{"provider":"broken","model":"m"}`), depsFor(&fakeLLM{}, nil)); err == nil {
		t.Fatal("factory error must surface")
	}
}

func TestRubric(t *testing.T) {
	llm := &fakeLLM{respond: replies(`Let me think. {"reason":"meets most","score":7}`)}
	cfg := `{"provider":"p","model":"m","rubric":"Mentions the refund window.","criteria":["polite","cites policy"],"scale_min":0,"scale_max":10,"anchors":{"10":"perfect","0":"useless"}}`
	s, err := evalJudge(t, "llm_rubric", cfg, llm, nil, ragInput())
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(*s.Value-0.7) > 1e-9 {
		t.Fatalf("value %v", *s.Value)
	}
	user := llm.calls[0].Messages[0].Content
	for _, want := range []string{"Mentions the refund window.", "1. polite", "2. cites policy", "10 - perfect", "0 - useless", "integer from 0 to 10"} {
		if !strings.Contains(user, want) {
			t.Errorf("rubric prompt missing %q:\n%s", want, user)
		}
	}
	for _, bad := range []string{
		`{"provider":"p","model":"m"}`,
		`{"provider":"p","model":"m","rubric":"x","scale_min":5,"scale_max":5}`,
		`{"provider":"p","model":"m","rubric":"x","anchors":{"9":"y"}}`,
	} {
		if err := ValidateConfig("llm_rubric", json.RawMessage(bad)); err == nil {
			t.Errorf("%s: want error", bad)
		}
	}
}

func TestPairwise(t *testing.T) {
	in := ragInput()
	in.Response.Output = "CANDIDATE answer"

	// No baseline: no value, skipped.
	llm := &fakeLLM{respond: replies(`{"verdict":"A"}`)}
	s, err := evalJudge(t, "pairwise", judgeCfg, llm, nil, in)
	if err != nil || s.Value != nil || s.Label != LabelNoBaseline || llm.n() != 0 {
		t.Fatalf("no baseline: %+v %v", s, err)
	}

	in.Baseline = &evalcore.Response{Output: "BASELINE answer"}
	// A judge that prefers the candidate wherever it is placed → win.
	prefersCand := func(req evalcore.LLMRequest, _ int) (evalcore.LLMResponse, error) {
		u := req.Messages[0].Content
		a := u[strings.Index(u, "<response_a>"):strings.Index(u, "</response_a>")]
		if strings.Contains(a, "CANDIDATE") {
			return evalcore.LLMResponse{Content: `{"reason":"A better","verdict":"A"}`}, nil
		}
		return evalcore.LLMResponse{Content: `{"reason":"B better","verdict":"B"}`}, nil
	}
	llm = &fakeLLM{respond: prefersCand}
	s, err = evalJudge(t, "pairwise", judgeCfg, llm, nil, in)
	if err != nil {
		t.Fatal(err)
	}
	if *s.Value != 1 || s.Label != LabelWin || !*s.Passed || llm.n() != 2 || s.Meta["position_consistent"] != true {
		t.Fatalf("win: %+v calls %d", s, llm.n())
	}
	// Both orders must have been asked.
	a0 := strings.Contains(llm.calls[0].Messages[0].Content, "<response_a>\nCANDIDATE")
	a1 := strings.Contains(llm.calls[1].Messages[0].Content, "<response_a>\nCANDIDATE")
	if a0 == a1 {
		t.Fatal("position swap did not happen")
	}

	// A position-biased judge (always "A") → tie, inconsistent.
	llm = &fakeLLM{respond: replies(`{"reason":"first is better","verdict":"A"}`)}
	s, _ = evalJudge(t, "pairwise", judgeCfg, llm, nil, in)
	if *s.Value != 0.5 || s.Label != LabelTie || s.Meta["position_consistent"] != false || !strings.Contains(s.Reason, "order was swapped") {
		t.Fatalf("biased: %+v", s)
	}

	// Judge prefers baseline → loss.
	llm = &fakeLLM{respond: func(req evalcore.LLMRequest, n int) (evalcore.LLMResponse, error) {
		r, _ := prefersCand(req, n)
		if strings.Contains(r.Content, `"A"`) {
			r.Content = `{"verdict":"B"}`
		} else {
			r.Content = `{"verdict":"A"}`
		}
		return r, nil
	}}
	s, _ = evalJudge(t, "pairwise", judgeCfg, llm, nil, in)
	if *s.Value != 0 || s.Label != LabelLoss || *s.Passed {
		t.Fatalf("loss: %+v", s)
	}

	// no_swap: a single call.
	llm = &fakeLLM{respond: prefersCand}
	s, _ = evalJudge(t, "pairwise", `{"provider":"p","model":"m","no_swap":true}`, llm, nil, in)
	if llm.n() != 1 || *s.Value != 1 {
		t.Fatalf("no_swap: calls %d value %v", llm.n(), *s.Value)
	}
}

func TestPairwiseVoting(t *testing.T) {
	in := ragInput()
	in.Baseline = &evalcore.Response{Output: "base"}
	// Per ordering, samples say [A, A, tie] → majority A for both orderings:
	// order 1 (cand=A) win, order 2 (cand=B) loss → 0.5.
	var mu sync.Mutex
	perPrompt := map[string]int{}
	llm := &fakeLLM{respond: func(req evalcore.LLMRequest, _ int) (evalcore.LLMResponse, error) {
		mu.Lock()
		k := perPrompt[req.Messages[0].Content]
		perPrompt[req.Messages[0].Content]++
		mu.Unlock()
		v := []string{"A", "A", "tie"}[k%3]
		return evalcore.LLMResponse{Content: `{"verdict":"` + v + `"}`}, nil
	}}
	s, err := evalJudge(t, "pairwise", `{"provider":"p","model":"m","samples":3}`, llm, nil, in)
	if err != nil {
		t.Fatal(err)
	}
	if llm.n() != 6 || *s.Value != 0.5 {
		t.Fatalf("calls %d value %v", llm.n(), *s.Value)
	}
	if got := s.Meta["verdicts"].([]string); got[0] != "A" || got[1] != "A" {
		t.Fatalf("verdicts %v", got)
	}
}

func TestMajority(t *testing.T) {
	cases := map[string][]string{
		"A":    {"A", "A", "B"},
		"B":    {"B"},
		"tie":  {"A", "B"},
		"tie ": {"tie", "tie", "A"},
	}
	for want, in := range cases {
		if got := majority(in); got != strings.TrimSpace(want) {
			t.Errorf("majority(%v) = %q, want %q", in, got, want)
		}
	}
}

func TestExtractJSON(t *testing.T) {
	cases := []struct {
		in    string
		score float64
		ok    bool
	}{
		{`{"score": 4}`, 4, true},
		{"```json\n{\"reason\": \"x\", \"score\": 3}\n```", 3, true},
		{"Reasoning: the answer has {braces} in it.\n\nFinal: {\"reason\": \"has } brace\", \"score\": 2}\nHope this helps!", 2, true},
		{`{"reason": "a", "score": 5,}`, 5, true},
		{"\u201creason\u201d is odd", 0, false},
		{`{"reason": "nested {\"score\": 1}", "score": 4}`, 4, true},
		{`{"a": {"b": 1}}`, 0, false},
		{`no json here`, 0, false},
		{`First {"score": 1} then corrected {"score": 3}`, 3, true},
	}
	for _, c := range cases {
		obj, err := extractJSON(c.in, "score")
		if (err == nil) != c.ok {
			t.Errorf("%q: err = %v", c.in, err)
			continue
		}
		if c.ok && obj["score"].(float64) != c.score {
			t.Errorf("%q: score = %v", c.in, obj["score"])
		}
	}
}

func TestParseScore(t *testing.T) {
	for in, want := range map[any]float64{4.0: 4, "4": 4, "4/5": 4, " 3 out of 5": 3, 2.5: 2.5} {
		got, err := parseScore(in, 1, 5)
		if err != nil || got != want {
			t.Errorf("parseScore(%v) = %v, %v", in, got, err)
		}
	}
	for _, in := range []any{0.0, 6.0, "high", nil, true} {
		if _, err := parseScore(in, 1, 5); err == nil {
			t.Errorf("parseScore(%v): want error", in)
		}
	}
}

type fakeEmbedder struct {
	calls int
	texts int
}

func (f *fakeEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	f.calls++
	f.texts += len(texts)
	out := make([][]float32, len(texts))
	for i, t := range texts {
		switch {
		case strings.Contains(t, "cat"):
			out[i] = []float32{1, 0.1, 0}
		case strings.Contains(t, "kitten"):
			out[i] = []float32{0.9, 0.2, 0}
		default:
			out[i] = []float32{0, 0, 1}
		}
	}
	return out, nil
}

func TestEmbeddingSimilarity(t *testing.T) {
	emb := &fakeEmbedder{}
	cache := NewMemoryCache(10)
	deps := Deps{Embedders: func(p, m, s string) (Embedder, error) { return emb, nil }, Cache: cache}
	ev, err := New("embedding_similarity", json.RawMessage(`{"provider":"openai","model":"te3"}`), deps)
	if err != nil {
		t.Fatal(err)
	}
	s, err := ev.Evaluate(context.Background(), outIn("a cat", "a kitten"))
	if err != nil {
		t.Fatal(err)
	}
	a, b := []float32{1, 0.1, 0}, []float32{0.9, 0.2, 0}
	want, _ := cosine(a, b)
	if math.Abs(*s.Value-want) > 1e-6 || !*s.Passed {
		t.Fatalf("value %v want %v", *s.Value, want)
	}
	s, _ = ev.Evaluate(context.Background(), outIn("a cat", "dog"))
	if *s.Value != 0 || *s.Passed {
		t.Fatalf("orthogonal: %v", *s.Value)
	}
	// "a cat" was cached: only "dog" embedded in the 2nd call.
	if emb.calls != 2 || emb.texts != 3 {
		t.Fatalf("calls %d texts %d", emb.calls, emb.texts)
	}
	s, _ = ev.Evaluate(context.Background(), outIn("a cat", "a kitten"))
	if emb.calls != 2 || s.Meta[MetaCached] != true {
		t.Fatal("fully cached evaluation should not call the embedder")
	}
	if _, err := New("embedding_similarity", json.RawMessage(`{"provider":"openai","model":"te3"}`), Deps{}); !errors.Is(err, ErrNoEmbedder) {
		t.Fatalf("want ErrNoEmbedder, got %v", err)
	}
}
