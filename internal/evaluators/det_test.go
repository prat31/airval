package evaluators

import (
	"context"
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/prat31/airval/internal/evalcore"
)

func mustNew(t testing.TB, kind, cfg string) Evaluator {
	t.Helper()
	ev, err := New(kind, json.RawMessage(cfg), Deps{})
	if err != nil {
		t.Fatalf("New(%s, %s): %v", kind, cfg, err)
	}
	return ev
}

func outIn(output string, expected any) Input {
	return Input{
		Example:  &evalcore.Example{ID: "ex1", Expected: expected},
		Response: &evalcore.Response{Output: output},
	}
}

type detCase struct {
	name    string
	cfg     string
	in      Input
	pass    bool
	value   any // float64/int; checked when non-nil
	wantErr bool
	reason  string // substring
	label   string
}

func runDet(t *testing.T, kind string, cases []detCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := tc.cfg
			if cfg == "" {
				cfg = "{}"
			}
			ev := mustNew(t, kind, cfg)
			if ev.Cost() != CostFree {
				t.Fatalf("%s should be CostFree", kind)
			}
			s, err := ev.Evaluate(context.Background(), tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("want error, got score %+v", s)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if s.Passed == nil || *s.Passed != tc.pass {
				t.Fatalf("passed = %v, want %v (reason %q)", deref(s.Passed), tc.pass, s.Reason)
			}
			if tc.value != nil {
				want, _ := toFloat(tc.value)
				if s.Value == nil || math.Abs(*s.Value-want) > 1e-3 {
					t.Fatalf("value = %v, want %v (reason %q)", derefF(s.Value), tc.value, s.Reason)
				}
			}
			if tc.reason != "" && !strings.Contains(s.Reason, tc.reason) {
				t.Fatalf("reason %q does not contain %q", s.Reason, tc.reason)
			}
			if tc.label != "" && s.Label != tc.label {
				t.Fatalf("label = %q, want %q", s.Label, tc.label)
			}
		})
	}
}

func deref(b *bool) any {
	if b == nil {
		return nil
	}
	return *b
}

func derefF(f *float64) any {
	if f == nil {
		return nil
	}
	return *f
}

func jsonIn(v any, expected any) Input {
	return Input{Example: &evalcore.Example{Expected: expected}, Response: &evalcore.Response{OutputJSON: v}}
}

func TestEquals(t *testing.T) {
	runDet(t, "equals", []detCase{
		{name: "exact", in: outIn("Paris", "Paris"), pass: true, value: 1},
		{name: "trim default", in: outIn("  Paris\n", "Paris"), pass: true, value: 1},
		{name: "no trim", cfg: `{"trim":false}`, in: outIn(" Paris", "Paris"), pass: false, value: 0},
		{name: "case mismatch", in: outIn("paris", "Paris"), pass: false, reason: `expected "Paris"`},
		{name: "ignore case", cfg: `{"ignore_case":true}`, in: outIn("PARIS", "paris"), pass: true},
		{name: "collapse ws", cfg: `{"collapse_whitespace":true}`, in: outIn("New   York\ncity", "New York city"), pass: true},
		{name: "ignore punct", cfg: `{"ignore_punctuation":true,"ignore_case":true}`, in: outIn("Yes!", "yes."), pass: true},
		{name: "literal expected", cfg: `{"expected":"42"}`, in: outIn("42", "nope"), pass: true},
		{name: "number expected", in: outIn("42", 42.0), pass: true},
		{name: "json structured", cfg: `{"field":"output_json"}`, in: jsonIn(map[string]any{"a": 1.0, "b": []any{"x"}}, map[string]any{"b": []any{"x"}, "a": 1}), pass: true},
		{name: "json structured differs", cfg: `{"field":"output_json"}`, in: jsonIn(map[string]any{"a": 2.0}, map[string]any{"a": 1.0}), pass: false},
		{name: "json path", cfg: `{"field":"output_json","path":"$.answer"}`, in: outIn(`{"answer":"Paris"}`, "Paris"), pass: true},
		{name: "json path from fenced text", cfg: `{"field":"output_json","path":"answer"}`, in: outIn("```json\n{\"answer\":\"Paris\"}\n```", "Paris"), pass: true},
		{name: "expected path", cfg: `{"expected_path":"$.city"}`, in: outIn("Paris", map[string]any{"city": "Paris"}), pass: true},
		{name: "invalid json output", cfg: `{"field":"output_json","path":"$.a"}`, in: outIn("nope", "x"), pass: false, reason: "not valid JSON"},
		{name: "no expected", in: outIn("x", nil), wantErr: true},
		{name: "context field", cfg: `{"field":"context","expected":["a","b"]}`, in: Input{Response: &evalcore.Response{Context: []string{"a", "b"}}}, pass: true},
	})
}

func TestContains(t *testing.T) {
	runDet(t, "contains", []detCase{
		{name: "any hit", cfg: `{"values":["refund","return"]}`, in: outIn("You can return it", nil), pass: true, value: 1},
		{name: "any miss", cfg: `{"values":["refund"]}`, in: outIn("no", nil), pass: false, value: 0},
		{name: "all partial", cfg: `{"values":["a","b","c","d"],"mode":"all"}`, in: outIn("a b", nil), pass: false, value: 0.5, reason: "missing 2 of 4"},
		{name: "all full", cfg: `{"values":["a","b"],"mode":"all"}`, in: outIn("b a", nil), pass: true, value: 1},
		{name: "none ok", cfg: `{"values":["password"],"mode":"none"}`, in: outIn("hello", nil), pass: true},
		{name: "none violated", cfg: `{"values":["password"],"mode":"none"}`, in: outIn("your password is", nil), pass: false, reason: "forbidden"},
		{name: "case sensitive miss", cfg: `{"values":["Paris"]}`, in: outIn("paris", nil), pass: false},
		{name: "ignore case", cfg: `{"values":["Paris"],"ignore_case":true}`, in: outIn("PARIS!", nil), pass: true},
		{name: "from expected string", in: outIn("the capital is Paris", "Paris"), pass: true},
		{name: "from expected list", cfg: `{"mode":"all"}`, in: outIn("x y", []any{"x", "y"}), pass: true},
		{name: "nothing to find", in: outIn("x", nil), wantErr: true},
	})
	if _, err := New("contains", json.RawMessage(`{"mode":"some"}`), Deps{}); err == nil || !strings.Contains(err.Error(), "mode") {
		t.Fatalf("want mode error, got %v", err)
	}
}

func TestRegex(t *testing.T) {
	runDet(t, "regex", []detCase{
		{name: "match", cfg: `{"pattern":"\\d{3}-\\d{4}"}`, in: outIn("call 555-1234", nil), pass: true},
		{name: "no match", cfg: `{"pattern":"^\\d+$"}`, in: outIn("abc", nil), pass: false},
		{name: "full match", cfg: `{"pattern":"\\d+","full_match":true}`, in: outIn("12a", nil), pass: false},
		{name: "full match ok", cfg: `{"pattern":"\\d+","full_match":true}`, in: outIn("123", nil), pass: true},
		{name: "ignore case", cfg: `{"pattern":"hello","ignore_case":true}`, in: outIn("HeLLo", nil), pass: true},
		{name: "invert", cfg: `{"pattern":"(?i)sorry","invert":true}`, in: outIn("Sorry!", nil), pass: false, reason: "forbidden"},
		{name: "invert ok", cfg: `{"pattern":"sorry","invert":true}`, in: outIn("sure", nil), pass: true},
		{name: "multiline", cfg: `{"pattern":"^b$","multiline":true}`, in: outIn("a\nb\nc", nil), pass: true},
		{name: "dotall", cfg: `{"pattern":"a.b","dot_all":true}`, in: outIn("a\nb", nil), pass: true},
		{name: "pattern from expected", in: outIn("order #123", `#\d+`), pass: true},
		{name: "bad expected pattern", in: outIn("x", `(`), wantErr: true},
	})
	if _, err := New("regex", json.RawMessage(`{"pattern":"("}`), Deps{}); err == nil {
		t.Fatal("want compile error")
	}
}

func TestAffix(t *testing.T) {
	runDet(t, "starts_with", []detCase{
		{name: "ok", cfg: `{"value":"Hello"}`, in: outIn("  Hello world", nil), pass: true},
		{name: "case", cfg: `{"value":"hello"}`, in: outIn("Hello", nil), pass: false, reason: "does not start with"},
		{name: "ignore case", cfg: `{"value":"hello","ignore_case":true}`, in: outIn("HELLO there", nil), pass: true},
		{name: "expected", in: outIn("Dear Sir", "Dear"), pass: true},
	})
	runDet(t, "ends_with", []detCase{
		{name: "ok", cfg: `{"value":"."}`, in: outIn("Done.", nil), pass: true},
		{name: "miss", cfg: `{"value":"?"}`, in: outIn("Done.", nil), pass: false, reason: "does not end with"},
	})
}

func TestIsJSON(t *testing.T) {
	runDet(t, "is_json", []detCase{
		{name: "object", in: outIn(`{"a":1}`, nil), pass: true},
		{name: "fenced", in: outIn("```json\n[1,2]\n```", nil), pass: true},
		{name: "fenced disallowed", cfg: `{"allow_fenced":false}`, in: outIn("```json\n[1,2]\n```", nil), pass: false},
		{name: "invalid", in: outIn(`{"a":}`, nil), pass: false, reason: "not valid JSON"},
		{name: "type object vs array", cfg: `{"type":"object"}`, in: outIn(`[1]`, nil), pass: false, reason: "not an object"},
		{name: "type array", cfg: `{"type":"array"}`, in: outIn(`[1]`, nil), pass: true},
		{name: "output_json field", cfg: `{"field":"output_json","type":"object"}`, in: jsonIn(map[string]any{"x": 1.0}, nil), pass: true},
	})
}

func TestJSONSchema(t *testing.T) {
	schema := `{"schema":{"type":"object","required":["name","age"],"properties":{"name":{"type":"string"},"age":{"type":"integer","minimum":0}}}}`
	runDet(t, "json_schema", []detCase{
		{name: "valid", cfg: schema, in: outIn(`{"name":"Ada","age":36}`, nil), pass: true},
		{name: "missing", cfg: schema, in: outIn(`{"name":"Ada"}`, nil), pass: false, reason: "age"},
		{name: "wrong type", cfg: schema, in: outIn(`{"name":"Ada","age":-1}`, nil), pass: false, reason: "/age"},
		{name: "not json", cfg: schema, in: outIn(`hello`, nil), pass: false, reason: "not valid JSON"},
		{name: "output_json", cfg: schema, in: Input{Response: &evalcore.Response{OutputJSON: map[string]any{"name": "x", "age": 3}}}, pass: false /* field=output, Output empty */},
		{name: "output_json field", cfg: `{"field":"output_json",` + schema[1:], in: Input{Response: &evalcore.Response{OutputJSON: map[string]any{"name": "x", "age": 3}}}, pass: true},
		{name: "schema from expected", in: outIn(`{"a":1}`, map[string]any{"type": "object", "required": []any{"a"}}), pass: true},
		{name: "schema from expected fails", in: outIn(`{}`, `{"required":["a"]}`), pass: false},
		{name: "no schema", in: outIn(`{}`, nil), wantErr: true},
	})
	if _, err := New("json_schema", json.RawMessage(`{"schema":{"$ref":"file:///etc/passwd"}}`), Deps{}); err == nil {
		t.Fatal("external $ref must be refused")
	}
	if _, err := New("json_schema", json.RawMessage(`{"schema":{"type":12}}`), Deps{}); err == nil {
		t.Fatal("invalid schema must fail at config time")
	}
}

func TestJSONPathEquals(t *testing.T) {
	doc := `{"order":{"status":"shipped","total":10.004,"items":[{"id":"a"},{"id":"b"}]},"weird key]":1}`
	runDet(t, "jsonpath_equals", []detCase{
		{name: "string", cfg: `{"json_path":"$.order.status","expected":"shipped"}`, in: outIn(doc, nil), pass: true},
		{name: "ignore case", cfg: `{"json_path":"$.order.status","expected":"SHIPPED","ignore_case":true}`, in: outIn(doc, nil), pass: true},
		{name: "number tol", cfg: `{"json_path":"$.order.total","expected":10,"tolerance":0.01}`, in: outIn(doc, nil), pass: true},
		{name: "number no tol", cfg: `{"json_path":"$.order.total","expected":10}`, in: outIn(doc, nil), pass: false},
		{name: "index", cfg: `{"json_path":"$.order.items[-1].id","expected":"b"}`, in: outIn(doc, nil), pass: true},
		{name: "wildcard", cfg: `{"json_path":"$.order.items[*].id","expected":["a","b"]}`, in: outIn(doc, nil), pass: true},
		{name: "quoted key", cfg: `{"json_path":"$['weird key]']","expected":1}`, in: outIn(doc, nil), pass: true},
		{name: "missing", cfg: `{"json_path":"$.nope","expected":1}`, in: outIn(doc, nil), pass: false, reason: "not found"},
		{name: "from example expected", cfg: `{"json_path":"$.order.status"}`, in: outIn(doc, "shipped"), pass: true},
		{name: "number vs string", cfg: `{"json_path":"$.n","expected":"5"}`, in: outIn(`{"n":5}`, nil), pass: true},
	})
	if _, err := New("jsonpath_equals", json.RawMessage(`{}`), Deps{}); err == nil {
		t.Fatal("json_path required")
	}
}

func TestNumericClose(t *testing.T) {
	runDet(t, "numeric_close", []detCase{
		{name: "extract", in: outIn("The answer is 42.", 42.0), pass: true},
		{name: "commas", in: outIn("Total: $1,234.50", "1234.5"), pass: true},
		{name: "abs tol", cfg: `{"abs_tol":0.1}`, in: outIn("3.14", 3.1416), pass: true},
		{name: "abs tol fail", cfg: `{"abs_tol":0.001}`, in: outIn("3.14", 3.1416), pass: false, reason: "differs"},
		{name: "rel tol", cfg: `{"rel_tol":0.01}`, in: outIn("990", 1000), pass: true},
		{name: "rel tol fail", cfg: `{"rel_tol":0.001}`, in: outIn("990", 1000), pass: false},
		{name: "negative sci", in: outIn("x = -3e2", -300), pass: true},
		{name: "no number", in: outIn("none", 1), pass: false, reason: "no number"},
		{name: "no extract", cfg: `{"extract":false}`, in: outIn("about 5", 5), pass: false},
		{name: "literal", cfg: `{"expected":7}`, in: outIn("7", nil), pass: true},
		{name: "bad expected", in: outIn("7", "seven"), wantErr: true},
	})
}

func TestLevenshtein(t *testing.T) {
	runDet(t, "levenshtein", []detCase{
		{name: "identical", in: outIn("kitten", "kitten"), pass: true, value: 1},
		{name: "kitten sitting", in: outIn("kitten", "sitting"), pass: false, value: 1 - 3.0/7},
		{name: "typo", in: outIn("recieve the package", "receive the package"), pass: true, value: 1 - 2.0/19},
		{name: "unicode", in: outIn("café", "cafe"), pass: false, value: 0.75},
		{name: "best of refs", in: outIn("color", []any{"colour", "color"}), pass: true, value: 1},
		{name: "empty both", cfg: `{"expected":""}`, in: outIn("", nil), pass: true, value: 1},
		{name: "ignore case", cfg: `{"ignore_case":true}`, in: outIn("HELLO", "hello"), pass: true, value: 1},
	})
}

func TestRougeL(t *testing.T) {
	// cand: the cat was found under the bed (7), ref: the cat was under the bed (6); LCS 6.
	p, r := 6.0/7, 1.0
	f := 2 * p * r / (p + r)
	runDet(t, "rouge_l", []detCase{
		{name: "f1", in: outIn("The cat was found under the bed", "the cat was under the bed"), pass: true, value: f},
		{name: "recall", cfg: `{"metric":"recall"}`, in: outIn("The cat was found under the bed", "the cat was under the bed"), pass: true, value: 1},
		{name: "precision", cfg: `{"metric":"precision"}`, in: outIn("The cat was found under the bed", "the cat was under the bed"), pass: true, value: p},
		{name: "disjoint", in: outIn("alpha beta", "gamma delta"), pass: false, value: 0},
	})
}

func TestBLEU(t *testing.T) {
	runDet(t, "bleu", []detCase{
		{name: "identical", in: outIn("the quick brown fox jumps over the lazy dog", "the quick brown fox jumps over the lazy dog"), pass: true, value: 1},
		{name: "disjoint", in: outIn("alpha beta gamma delta", "one two three four"), pass: false},
		{name: "multi ref best", in: outIn("the cat is on the mat", []any{"there is a cat on the mat", "the cat is on the mat"}), pass: true, value: 1},
	})
	// Known value: BLEU-2, cand "the cat sat" vs ref "the cat sat on the mat":
	// p1 = 3/3, p2 = 2/2, BP = exp(1 - 6/3) = e^-1.
	got := sentenceBLEU(tokenize("the cat sat"), [][]string{tokenize("the cat sat on the mat")}, 2, 0.1)
	if want := math.Exp(-1); math.Abs(got-want) > 1e-9 {
		t.Fatalf("BLEU-2 = %v, want %v", got, want)
	}
	// Smoothing: unigram 2/3 match, no bigram match: exp((ln(2/3)+ln(0.1/2))/2).
	got = sentenceBLEU(tokenize("a b c"), [][]string{tokenize("a x b")}, 2, 0.1)
	want := math.Exp((math.Log(2.0/3) + math.Log(0.1/2)) / 2)
	if math.Abs(got-want) > 1e-9 {
		t.Fatalf("smoothed BLEU = %v, want %v", got, want)
	}
}

func TestLength(t *testing.T) {
	runDet(t, "length", []detCase{
		{name: "chars ok", cfg: `{"max":5}`, in: outIn("héllo", nil), pass: true},
		{name: "chars over", cfg: `{"max":4}`, in: outIn("héllo", nil), pass: false, reason: "above the maximum"},
		{name: "words under", cfg: `{"unit":"words","min":3}`, in: outIn("two  words", nil), pass: false, reason: "below the minimum"},
		{name: "words range", cfg: `{"unit":"words","min":1,"max":3}`, in: outIn(" a b\nc ", nil), pass: true},
		{name: "tokens", cfg: `{"unit":"tokens","max":3}`, in: outIn("abcdefghijkl", nil), pass: true},
		{name: "tokens over", cfg: `{"unit":"tokens","max":2}`, in: outIn("abcdefghijkl", nil), pass: false},
	})
	if _, err := New("length", nil, Deps{}); err == nil {
		t.Fatal("length requires min or max")
	}
	if _, err := New("length", json.RawMessage(`{"min":5,"max":1}`), Deps{}); err == nil {
		t.Fatal("min > max must fail")
	}
}

func TestPerf(t *testing.T) {
	resp := &evalcore.Response{Latency: 1500 * time.Millisecond, TTFT: 200 * time.Millisecond, CostUSD: 0.002, Usage: evalcore.Usage{InputTokens: 100, OutputTokens: 50}}
	in := Input{Response: resp}
	runDet(t, "latency_ms", []detCase{
		{name: "ok", cfg: `{"max_ms":2000}`, in: in, pass: true},
		{name: "slow", cfg: `{"max_ms":1000}`, in: in, pass: false, reason: "1500 ms"},
		{name: "ttft", cfg: `{"max_ms":300,"metric":"ttft"}`, in: in, pass: true},
	})
	runDet(t, "cost_usd", []detCase{
		{name: "ok", cfg: `{"max_usd":0.01}`, in: in, pass: true},
		{name: "over", cfg: `{"max_usd":0.001}`, in: in, pass: false},
	})
	runDet(t, "tokens", []detCase{
		{name: "total", cfg: `{"max":150}`, in: in, pass: true},
		{name: "total over", cfg: `{"max":149}`, in: in, pass: false},
		{name: "output min", cfg: `{"count":"output","min":60}`, in: in, pass: false},
		{name: "input", cfg: `{"count":"input","min":100,"max":100}`, in: in, pass: true},
	})
	if _, err := New("latency_ms", json.RawMessage(`{}`), Deps{}); err == nil {
		t.Fatal("max_ms required")
	}
}

func toolIn(calls ...evalcore.ToolCall) Input {
	return Input{Example: &evalcore.Example{}, Response: &evalcore.Response{ToolCalls: calls}}
}

func tc(name, args string) evalcore.ToolCall {
	return evalcore.ToolCall{Name: name, Arguments: json.RawMessage(args)}
}

func TestToolCalled(t *testing.T) {
	search := tc("search", `{"query":"weather","opts":{"units":"c","lang":"en"}}`)
	runDet(t, "tool_called", []detCase{
		{name: "called", cfg: `{"name":"search"}`, in: toolIn(search), pass: true},
		{name: "not called", cfg: `{"name":"book"}`, in: toolIn(search), pass: false, reason: "never called"},
		{name: "args subset", cfg: `{"name":"search","arguments":{"opts":{"units":"c"}}}`, in: toolIn(search), pass: true},
		{name: "args mismatch", cfg: `{"name":"search","arguments":{"query":"news"}}`, in: toolIn(search), pass: false, reason: "never with the expected arguments"},
		{name: "exact args fail", cfg: `{"name":"search","arguments":{"query":"weather"},"exact_args":true}`, in: toolIn(search), pass: false},
		{name: "string-encoded args", cfg: `{"name":"f","arguments":{"x":1}}`, in: toolIn(tc("f", `"{\"x\":1}"`)), pass: true},
		{name: "count max", cfg: `{"name":"f","max_count":1}`, in: toolIn(tc("f", `{}`), tc("f", `{}`)), pass: false, reason: "at most 1"},
		{name: "count min", cfg: `{"name":"f","min_count":2}`, in: toolIn(tc("f", `{}`), tc("f", `{}`)), pass: true},
		{name: "must not call", cfg: `{"name":"delete","min_count":0,"max_count":0}`, in: toolIn(search), pass: true, reason: "not called"},
		{name: "must not call violated", cfg: `{"name":"search","min_count":0,"max_count":0}`, in: toolIn(search), pass: false},
		{name: "from expected", in: Input{Example: &evalcore.Example{Expected: map[string]any{"name": "search", "arguments": map[string]any{"query": "weather"}}}, Response: &evalcore.Response{ToolCalls: []evalcore.ToolCall{search}}}, pass: true},
		{name: "from transcript", cfg: `{"name":"search"}`, in: Input{Response: &evalcore.Response{Transcript: []evalcore.Message{{Role: "assistant", ToolCalls: []evalcore.ToolCall{search}}}}}, pass: true},
		{name: "from trace", cfg: `{"name":"search"}`, in: Input{Response: &evalcore.Response{Trace: []evalcore.TraceStep{{Kind: "tool", Name: "search"}}}}, pass: true},
		{name: "no name", in: toolIn(search), wantErr: true},
	})
}

func TestToolSequence(t *testing.T) {
	calls := toolIn(tc("a", ""), tc("x", ""), tc("b", ""), tc("c", ""))
	runDet(t, "tool_sequence", []detCase{
		{name: "in order", cfg: `{"sequence":["a","b","c"]}`, in: calls, pass: true, value: 1},
		{name: "in order partial", cfg: `{"sequence":["a","c","b"]}`, in: calls, pass: false, value: 2.0 / 3},
		{name: "exact fail", cfg: `{"sequence":["a","b","c"],"mode":"exact"}`, in: calls, pass: false, value: 0},
		{name: "exact ok", cfg: `{"sequence":["a","x","b","c"],"mode":"exact"}`, in: calls, pass: true},
		{name: "any order", cfg: `{"sequence":["c","a"],"mode":"any_order"}`, in: calls, pass: true, value: 1},
		{name: "any order multiset", cfg: `{"sequence":["a","a"],"mode":"any_order"}`, in: calls, pass: false, value: 0.5, reason: "missing tools: a"},
		{name: "from expected", in: Input{Example: &evalcore.Example{Expected: []any{"a", "b"}}, Response: calls.Response}, pass: true},
		{name: "no calls", cfg: `{"sequence":["a"]}`, in: toolIn(), pass: false, reason: "no tool calls"},
	})
}

func TestNoPII(t *testing.T) {
	runDet(t, "no_pii", []detCase{
		{name: "clean", in: outIn("Your order ships on 2024-01-15. Order 12345.", nil), pass: true},
		{name: "email", in: outIn("mail me at jane.doe@example.com", nil), pass: false, reason: "email", label: "pii"},
		{name: "allowed email", cfg: `{"allow":["support@acme.com"]}`, in: outIn("Contact support@acme.com", nil), pass: true},
		{name: "phone us", in: outIn("Call (415) 555-0132 now", nil), pass: false, reason: "phone"},
		{name: "phone intl", in: outIn("Call +44 20 7946 0958", nil), pass: false, reason: "phone"},
		{name: "card luhn", in: outIn("card 4111 1111 1111 1111", nil), pass: false, reason: "credit_card"},
		{name: "card bad luhn not card", cfg: `{"detectors":["credit_card"]}`, in: outIn("card 4111 1111 1111 1112", nil), pass: true},
		{name: "ssn", in: outIn("SSN 123-45-6789", nil), pass: false, reason: "ssn"},
		{name: "ssn invalid area", cfg: `{"detectors":["ssn"]}`, in: outIn("id 666-45-6789", nil), pass: true},
		{name: "iban", in: outIn("IBAN GB82 WEST 1234 5698 7654 32", nil), pass: false, reason: "iban"},
		{name: "iban bad checksum", cfg: `{"detectors":["iban"]}`, in: outIn("GB83 WEST 1234 5698 7654 32", nil), pass: true},
		{name: "ipv4", in: outIn("server at 192.168.1.20", nil), pass: false, reason: "ip"},
		{name: "ipv4 invalid", cfg: `{"detectors":["ip"]}`, in: outIn("version 999.1.1.1", nil), pass: true},
		{name: "ipv6", cfg: `{"detectors":["ip"]}`, in: outIn("host 2001:db8::8a2e:370:7334", nil), pass: false},
		{name: "time not ipv6", cfg: `{"detectors":["ip"]}`, in: outIn("at 12:30:45 today", nil), pass: true},
		{name: "detector subset", cfg: `{"detectors":["ssn"]}`, in: outIn("jane@example.com", nil), pass: true},
	})
	if _, err := New("no_pii", json.RawMessage(`{"detectors":["dna"]}`), Deps{}); err == nil {
		t.Fatal("unknown detector must fail")
	}
	s, _ := mustNew(t, "no_pii", `{}`).Evaluate(context.Background(), outIn("jane.doe@example.com", nil))
	if strings.Contains(s.Reason, "jane.doe@example.com") || strings.Contains(toText(s.Meta["findings"]), "jane.doe@example.com") {
		t.Fatal("PII must be redacted in score output")
	}
}

func TestRefusal(t *testing.T) {
	runDet(t, "refusal", []detCase{
		{name: "answered", in: outIn("Sure! Paris is the capital.", nil), pass: true, label: "answered"},
		{name: "refused", in: outIn("I'm sorry, but I can't help with that request.", nil), pass: false, label: "refused"},
		{name: "curly apostrophe", in: outIn("I can’t assist with that.", nil), pass: false},
		{name: "expected refusal", cfg: `{"expect_refusal":true}`, in: outIn("I cannot provide instructions for that.", nil), pass: true},
		{name: "expected refusal missing", cfg: `{"expect_refusal":true}`, in: outIn("Here's how...", nil), pass: false},
		{name: "extra phrase", cfg: `{"extra_phrases":["That's outside what I can do"]}`, in: outIn("That's outside what I can do here.", nil), pass: false},
		{name: "late mention in long answer", in: outIn(strings.Repeat("Useful content. ", 50)+"I can't provide legal advice.", nil), pass: true},
	})
}

func TestFieldTranscript(t *testing.T) {
	in := Input{Response: &evalcore.Response{Transcript: []evalcore.Message{
		{Role: "user", Content: "hi"}, {Role: "assistant", Content: "hello there"},
	}}}
	runDet(t, "contains", []detCase{
		{name: "transcript", cfg: `{"field":"transcript","values":["user: hi","assistant: hello"],"mode":"all"}`, in: in, pass: true},
	})
}

func TestJSONPathCompile(t *testing.T) {
	for _, p := range []string{"$", "$.a", "a.b", "$['a b']", `$["x"][0]`, "$.a[*].b", "$.*", "$[ 'k' ]"} {
		if _, err := compileJSONPath(p); err != nil {
			t.Errorf("%q: %v", p, err)
		}
	}
	for _, p := range []string{"$.", "$[", "$[x]", "$['a'", "$..a"} {
		if _, err := compileJSONPath(p); err == nil {
			t.Errorf("%q: want error", p)
		}
	}
}

func TestLevenshteinDistance(t *testing.T) {
	cases := []struct {
		a, b string
		d    int
	}{
		{"", "", 0}, {"a", "", 1}, {"", "abc", 3}, {"kitten", "sitting", 3},
		{"flaw", "lawn", 2}, {"日本語", "日本", 1}, {"abc", "abc", 0}, {"ab", "ba", 2},
	}
	for _, c := range cases {
		if got := levenshteinDistance(c.a, c.b); got != c.d {
			t.Errorf("lev(%q,%q) = %d, want %d", c.a, c.b, got, c.d)
		}
	}
}

func TestUnknownConfigField(t *testing.T) {
	_, err := New("equals", json.RawMessage(`{"ignorecase":true}`), Deps{})
	if err == nil || !strings.Contains(err.Error(), "ignorecase") {
		t.Fatalf("want unknown field error, got %v", err)
	}
}
