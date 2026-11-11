package evaluators

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/prat31/airval/internal/evalcore"
)

var benchAnswer = strings.Repeat("The quick brown fox jumps over the lazy dog near the river bank. ", 8)

func BenchmarkDeterministic(b *testing.B) {
	jsonOut := `{"order":{"id":"A-1001","status":"shipped","total":42.5,"items":[{"sku":"x","qty":2},{"sku":"y","qty":1}]}}`
	cases := []struct {
		kind, cfg string
		in        Input
	}{
		{"equals", `{"ignore_case":true,"collapse_whitespace":true}`, outIn(benchAnswer, benchAnswer)},
		{"contains", `{"values":["fox","river","dog"],"mode":"all","ignore_case":true}`, outIn(benchAnswer, nil)},
		{"regex", `{"pattern":"\\bfox\\b.*\\briver\\b"}`, outIn(benchAnswer, nil)},
		{"starts_with", `{"value":"The quick"}`, outIn(benchAnswer, nil)},
		{"is_json", `{}`, outIn(jsonOut, nil)},
		{"json_schema", `{"schema":{"type":"object","required":["order"],"properties":{"order":{"type":"object","required":["id","status"]}}}}`, outIn(jsonOut, nil)},
		{"jsonpath_equals", `{"json_path":"$.order.status","expected":"shipped"}`, outIn(jsonOut, nil)},
		{"numeric_close", `{"rel_tol":0.01}`, outIn("The total comes to $1,234.50 after tax.", 1234.5)},
		{"levenshtein", `{}`, outIn(benchAnswer[:200], strings.Replace(benchAnswer[:200], "fox", "cat", 2))},
		{"rouge_l", `{}`, outIn(benchAnswer, strings.Replace(benchAnswer, "lazy", "sleepy", -1))},
		{"bleu", `{}`, outIn(benchAnswer, strings.Replace(benchAnswer, "lazy", "sleepy", -1))},
		{"length", `{"unit":"tokens","max":500}`, outIn(benchAnswer, nil)},
		{"latency_ms", `{"max_ms":2000}`, outIn("", nil)},
		{"tool_called", `{"name":"search","arguments":{"q":"weather"}}`, Input{Response: &evalcore.Response{ToolCalls: []evalcore.ToolCall{{Name: "lookup"}, {Name: "search", Arguments: json.RawMessage(`{"q":"weather","n":3}`)}}}}},
		{"tool_sequence", `{"sequence":["lookup","search"]}`, Input{Response: &evalcore.Response{ToolCalls: []evalcore.ToolCall{{Name: "lookup"}, {Name: "x"}, {Name: "search"}}}}},
		{"no_pii", `{}`, outIn(benchAnswer, nil)},
		{"refusal", `{}`, outIn(benchAnswer, nil)},
	}
	ctx := context.Background()
	for _, c := range cases {
		ev := mustNew(b, c.kind, c.cfg)
		b.Run(c.kind, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if _, err := ev.Evaluate(ctx, c.in); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkPipelineDeterministic(b *testing.B) {
	var bounds []*Bound
	for _, s := range []Spec{
		{Kind: "contains", Config: json.RawMessage(`{"values":["fox"]}`)},
		{Kind: "length", Config: json.RawMessage(`{"max":2000}`)},
		{Kind: "no_pii"},
	} {
		bd, err := Build(s, Deps{})
		if err != nil {
			b.Fatal(err)
		}
		bounds = append(bounds, bd)
	}
	p, _ := NewPipeline(bounds)
	in := outIn(benchAnswer, nil)
	ctx := context.Background()
	b.ReportAllocs()
	for b.Loop() {
		p.Evaluate(ctx, in)
	}
}
