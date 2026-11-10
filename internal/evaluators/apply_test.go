package evaluators

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/airval/airval/internal/evalcore"
)

func convInput() Input {
	return Input{
		Example: &evalcore.Example{
			ID: "conv",
			Conversation: &evalcore.Conversation{Turns: []evalcore.Turn{
				{Role: "user", Content: "hi", Expected: "hello"},
				{Role: "user", Content: "bye", Expected: "goodbye"},
			}},
		},
		Response: &evalcore.Response{
			Output: "see you",
			Transcript: []evalcore.Message{
				{Role: "user", Content: "hi"},
				{Role: "assistant", Content: "hello"},
				{Role: "user", Content: "bye"},
				{Role: "assistant", Content: "see you", ToolCalls: []evalcore.ToolCall{{Name: "end_chat"}}},
			},
		},
	}
}

func build(t *testing.T, spec Spec, deps Deps) *Bound {
	t.Helper()
	b, err := Build(spec, deps)
	if err != nil {
		t.Fatalf("Build(%+v): %v", spec, err)
	}
	return b
}

func TestPerTurn(t *testing.T) {
	ctx := context.Background()
	mean := build(t, Spec{ID: "eq", Kind: "equals", ApplyTo: evalcore.ApplyPerTurn}, Deps{})
	s := mean.Evaluate(ctx, convInput())
	if s.EvaluatorID != "eq" || *s.Value != 0.5 || *s.Passed {
		t.Fatalf("mean: %+v", s)
	}
	ts := s.Meta[MetaTurnScores].([]TurnScore)
	if len(ts) != 2 || ts[0].Turn != 1 || ts[1].Turn != 3 || !*ts[0].Passed || *ts[1].Passed {
		t.Fatalf("turn scores: %+v", ts)
	}
	if !strings.Contains(s.Reason, "Worst turn 3") {
		t.Fatalf("reason: %s", s.Reason)
	}
	half := 0.5
	minB := build(t, Spec{Kind: "equals", ApplyTo: evalcore.ApplyPerTurn, TurnAggregate: TurnMin, Threshold: &half}, Deps{})
	if s := minB.Evaluate(ctx, convInput()); *s.Value != 0 || *s.Passed {
		t.Fatalf("min: %+v", s)
	}
	// Per-turn tool calls come from each assistant message.
	tool := build(t, Spec{Kind: "tool_called", Config: json.RawMessage(`{"name":"end_chat"}`), ApplyTo: evalcore.ApplyPerTurn, TurnAggregate: TurnMin}, Deps{})
	if s := tool.Evaluate(ctx, convInput()); *s.Value != 0 {
		t.Fatalf("tool per turn min: %+v", s)
	}
	// Without a transcript, per-turn falls back to the final output.
	in := outIn("x", "x")
	if s := mean.Evaluate(ctx, in); *s.Value != 1 {
		t.Fatalf("fallback: %+v", s)
	}
}

func TestTurnInputsWithoutScriptExpectations(t *testing.T) {
	in := convInput()
	in.Example.Conversation = nil
	in.Example.Expected = "final"
	turns := TurnInputs(in)
	if len(turns) != 2 || turns[0].Example.Expected != "final" || turns[0].Response.Output != "hello" || len(turns[0].Response.Transcript) != 2 {
		t.Fatalf("turns: %+v", turns[0])
	}
}

func TestConversationAndTraceInput(t *testing.T) {
	ctx := context.Background()
	b := build(t, Spec{Kind: "contains", Config: json.RawMessage(`{"values":["user: bye","assistant: see you"],"mode":"all"}`), ApplyTo: evalcore.ApplyConversation}, Deps{})
	if s := b.Evaluate(ctx, convInput()); !*s.Passed {
		t.Fatalf("conversation: %+v", s)
	}
	tr := Input{Response: &evalcore.Response{Trace: []evalcore.TraceStep{
		{Kind: "retrieval", Output: json.RawMessage(`[{"text":"doc one"},"doc two"]`)},
		{Kind: "tool", Name: "search", Input: json.RawMessage(`{"q":"x"}`)},
	}}}
	ti := TraceInput(tr)
	if len(ti.Response.Context) != 2 || ti.Response.Context[0] != "doc one" || ti.Response.ToolCalls[0].Name != "search" {
		t.Fatalf("trace input: %+v", ti.Response)
	}
	tb := build(t, Spec{Kind: "tool_called", Config: json.RawMessage(`{"name":"search","arguments":{"q":"x"}}`), ApplyTo: evalcore.ApplyTrace}, Deps{})
	if s := tb.Evaluate(ctx, tr); !*s.Passed {
		t.Fatalf("trace: %+v", s)
	}
}

func TestBuildValidation(t *testing.T) {
	bad := []Spec{
		{Kind: "nope"},
		{Kind: "equal"},
		{Kind: "equals", Severity: "blocker"},
		{Kind: "latency_ms", Config: json.RawMessage(`{"max_ms":1}`), ApplyTo: evalcore.ApplyPerTurn},
		{Kind: "equals", TurnAggregate: "max"},
		{Kind: "equals", Threshold: f64(1.5)},
		{Kind: "length", Config: json.RawMessage(`{}`)},
	}
	for _, s := range bad {
		if _, err := Build(s, Deps{}); err == nil {
			t.Errorf("%+v: want error", s)
		}
	}
	_, err := Build(Spec{Kind: "equal"}, Deps{})
	if !errors.Is(err, ErrUnknownKind) || !strings.Contains(err.Error(), `did you mean "equals"`) {
		t.Fatalf("suggestion missing: %v", err)
	}
	b := build(t, Spec{Kind: "levenshtein"}, Deps{})
	if b.ID != "levenshtein" || b.Severity != evalcore.SeverityMajor || b.Threshold() != 0.8 || b.Weight != 1 {
		t.Fatalf("defaults: %+v", b.Spec)
	}
	// Threshold override flips Passed.
	low := build(t, Spec{Kind: "levenshtein", Threshold: f64(0.5)}, Deps{})
	if s := low.Evaluate(context.Background(), outIn("kitten", "sitting")); !*s.Passed || s.Label != LabelPass {
		t.Fatalf("override: %+v", s)
	}
	// Errors become error scores.
	if s := b.Evaluate(context.Background(), outIn("x", nil)); s.Label != LabelError || s.Passed != nil || s.Meta[MetaError] == nil {
		t.Fatalf("error score: %+v", s)
	}
}

func TestPipelineFailFast(t *testing.T) {
	llm := &fakeLLM{respond: replies(`{"score":5}`)}
	deps := depsFor(llm, nil)
	specs := []Spec{
		{ID: "judge", Kind: "helpfulness", Config: json.RawMessage(judgeCfg)},
		{ID: "review", Kind: "human", Config: json.RawMessage(`{"mode":"failures"}`)},
		{ID: "json", Kind: "is_json", FailFast: true, Severity: evalcore.SeverityCritical},
		{ID: "len", Kind: "length", Config: json.RawMessage(`{"max":1000}`)},
	}
	var bounds []*Bound
	for _, s := range specs {
		bounds = append(bounds, build(t, s, deps))
	}
	p, err := NewPipeline(bounds)
	if err != nil {
		t.Fatal(err)
	}
	if got := p.Bounds(); got[0].ID != "json" || got[1].ID != "len" || got[2].ID != "judge" || got[3].ID != "review" {
		t.Fatalf("order: %v %v %v %v", got[0].ID, got[1].ID, got[2].ID, got[3].ID)
	}
	ctx := context.Background()
	scores := p.Evaluate(ctx, outIn("not json", nil))
	if scores[0].EvaluatorID != "judge" || scores[0].Label != LabelSkipped || llm.n() != 0 {
		t.Fatalf("judge should be skipped: %+v (calls %d)", scores[0], llm.n())
	}
	if !ReviewRequired(scores[1]) || scores[1].Label != LabelPending || scores[1].Value != nil {
		t.Fatalf("human should be pending for failures: %+v", scores[1])
	}
	if *scores[2].Passed || !*scores[3].Passed {
		t.Fatalf("det scores: %+v %+v", scores[2], scores[3])
	}
	scores = p.Evaluate(ctx, outIn(`{"ok":true}`, nil))
	if *scores[0].Value != 1 || llm.n() != 1 {
		t.Fatalf("judge should run: %+v", scores[0])
	}
	if ReviewRequired(scores[1]) || scores[1].Label != LabelSkipped {
		t.Fatalf("no failures → no review: %+v", scores[1])
	}
	if _, err := NewPipeline([]*Bound{bounds[0], bounds[0]}); !errors.Is(err, ErrDuplicateID) {
		t.Fatalf("want duplicate id error, got %v", err)
	}
	p.MaxParallel = 1
	if s := p.Evaluate(ctx, outIn(`{}`, nil)); s[0].Value == nil {
		t.Fatal("bounded parallelism should still evaluate")
	}
}

func TestHuman(t *testing.T) {
	ctx := context.Background()
	all := mustNew(t, "human", `{"scale":"labels","labels":["good","bad"],"instructions":"Check tone","queue":"tone"}`)
	if all.Cost() != CostHuman {
		t.Fatal("human should be CostHuman")
	}
	s, _ := all.Evaluate(ctx, outIn("x", nil))
	rv := s.Meta[MetaReview].(map[string]any)
	if s.Value != nil || s.Label != LabelPending || !ReviewRequired(s) || rv["queue"] != "tone" || rv["scale"] != "labels" {
		t.Fatalf("all: %+v", s)
	}
	sample := mustNew(t, "human", `{"mode":"sample","sample_rate":0.3}`)
	n := 0
	for i := 0; i < 2000; i++ {
		in := outIn("x", nil)
		in.Example.ID = "ex-" + string(rune('a'+i%26)) + string(rune('a'+i/26%26)) + string(rune('a'+i/676))
		s, _ := sample.Evaluate(ctx, in)
		if ReviewRequired(s) {
			n++
		}
		s2, _ := sample.Evaluate(ctx, in)
		if ReviewRequired(s) != ReviewRequired(s2) {
			t.Fatal("sampling must be deterministic")
		}
	}
	if n < 500 || n > 700 {
		t.Fatalf("sample rate off: %d/2000", n)
	}
	if err := ValidateConfig("human", json.RawMessage(`{"scale":"labels"}`)); err == nil {
		t.Fatal("labels required")
	}
}

func TestCatalog(t *testing.T) {
	want := []string{
		"equals", "contains", "regex", "starts_with", "ends_with", "is_json", "json_schema", "jsonpath_equals",
		"numeric_close", "levenshtein", "rouge_l", "bleu", "length", "latency_ms", "cost_usd", "tokens",
		"tool_called", "tool_sequence", "no_pii", "refusal", "embedding_similarity", "llm_rubric",
		"correctness", "faithfulness", "groundedness", "answer_relevance", "context_relevance", "context_recall",
		"hallucination", "toxicity", "bias", "conciseness", "helpfulness", "conversation_goal_completion",
		"conversation_coherence", "tool_use_correctness", "pairwise", "human",
	}
	kinds := map[string]bool{}
	for _, k := range Kinds() {
		kinds[k] = true
	}
	for _, k := range want {
		if !kinds[k] {
			t.Errorf("kind %s not registered", k)
		}
	}
	cat := Catalog()
	if len(cat) != len(Kinds()) {
		t.Fatal("catalog size mismatch")
	}
	lastCat := -1
	for _, ki := range cat {
		if ki.Name == "" || len(ki.Description) < 40 || ki.Group == "" || len(ki.ApplyTo) == 0 {
			t.Errorf("%s: incomplete catalog entry %+v", ki.Kind, ki)
		}
		if ki.DefaultThreshold < 0 || ki.DefaultThreshold > 1 {
			t.Errorf("%s: bad default threshold", ki.Kind)
		}
		if c := categoryOrder[ki.Category]; c < lastCat {
			t.Errorf("catalog not ordered by category at %s", ki.Kind)
		} else {
			lastCat = c
		}
		if (ki.Category == CategoryModel) != (ki.Cost == CostModel) {
			t.Errorf("%s: category/cost mismatch", ki.Kind)
		}
		// Each config schema must be a valid JSON Schema.
		doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(ki.ConfigSchema))
		if err != nil {
			t.Fatalf("%s: %v", ki.Kind, err)
		}
		c := jsonschema.NewCompiler()
		if err := c.AddResource("s.json", doc); err != nil {
			t.Fatal(err)
		}
		if _, err := c.Compile("s.json"); err != nil {
			t.Errorf("%s: invalid config schema: %v", ki.Kind, err)
		}
	}
	// Spot-check schema content.
	info, _ := Info("contains")
	var sch map[string]any
	_ = json.Unmarshal(info.ConfigSchema, &sch)
	props := sch["properties"].(map[string]any)
	mode := props["mode"].(map[string]any)
	if mode["default"] != "any" || len(mode["enum"].([]any)) != 3 || props["field"] == nil || sch["additionalProperties"] != false {
		t.Fatalf("contains schema: %s", info.ConfigSchema)
	}
	info, _ = Info("correctness")
	_ = json.Unmarshal(info.ConfigSchema, &sch)
	req := sch["required"].([]any)
	if len(req) != 2 || req[0] != "provider" || req[1] != "model" {
		t.Fatalf("judge required fields: %v", req)
	}
	b, err := json.Marshal(cat[0])
	if err != nil || !strings.Contains(string(b), `"cost":"free"`) {
		t.Fatalf("catalog JSON: %s %v", b, err)
	}
}

func TestMemoryCacheEviction(t *testing.T) {
	c := NewMemoryCache(2)
	ctx := context.Background()
	_ = c.Set(ctx, "a", []byte("1"))
	_ = c.Set(ctx, "b", []byte("2"))
	_, _, _ = c.Get(ctx, "a")
	_ = c.Set(ctx, "c", []byte("3"))
	if _, ok, _ := c.Get(ctx, "b"); ok {
		t.Fatal("b should be evicted")
	}
	if v, ok, _ := c.Get(ctx, "a"); !ok || string(v) != "1" || c.Len() != 2 {
		t.Fatal("a should survive")
	}
}
