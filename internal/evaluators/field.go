package evaluators

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/airval/airval/internal/evalcore"
)

// Field values for FieldSelector.Field.
const (
	FieldOutput     = "output"
	FieldOutputJSON = "output_json"
	FieldContext    = "context"
	FieldTranscript = "transcript"
)

// FieldSelector picks which part of the response an evaluator scores.
type FieldSelector struct {
	Field string `json:"field,omitempty" title:"Field" enum:"output|output_json|context|transcript" default:"output" desc:"What to score: the final text output, a value inside the structured (JSON) output, the retrieved context, or the full conversation transcript."`
	Path  string `json:"path,omitempty" title:"JSON path" desc:"With field = output_json: JSONPath to the value to score, e.g. $.answer or $.items[0].name. Empty means the whole JSON output."`

	jp *jsonPath
}

func (f *FieldSelector) validate() error {
	switch f.Field {
	case "":
		f.Field = FieldOutput
	case FieldOutput, FieldOutputJSON, FieldContext, FieldTranscript:
	default:
		return fmt.Errorf("field: must be one of output, output_json, context, transcript (got %q)", f.Field)
	}
	if f.Path != "" {
		if f.Field != FieldOutputJSON {
			return errors.New("path: only valid with field = output_json")
		}
		jp, err := compileJSONPath(f.Path)
		if err != nil {
			return fmt.Errorf("path: %w", err)
		}
		f.jp = jp
	}
	return nil
}

// text returns the selected field as text. Structured values are rendered as
// compact JSON.
func (f *FieldSelector) text(in Input) (string, error) {
	r := in.response()
	switch f.Field {
	case "", FieldOutput:
		return r.Output, nil
	case FieldContext:
		return strings.Join(r.Context, "\n\n"), nil
	case FieldTranscript:
		return renderTranscript(r), nil
	}
	v, err := f.value(in)
	if err != nil {
		return "", err
	}
	return toText(v), nil
}

// value returns the selected field as a JSON-like value (string, float64,
// bool, nil, []any, map[string]any).
func (f *FieldSelector) value(in Input) (any, error) {
	r := in.response()
	switch f.Field {
	case "", FieldOutput:
		return r.Output, nil
	case FieldContext:
		out := make([]any, len(r.Context))
		for i, c := range r.Context {
			out[i] = c
		}
		return out, nil
	case FieldTranscript:
		return renderTranscript(r), nil
	}
	v, err := outputJSON(r)
	if err != nil {
		return nil, err
	}
	if f.jp != nil {
		got, ok := f.jp.get(v)
		if !ok {
			return nil, fmt.Errorf("path %s not found in output JSON", f.Path)
		}
		return got, nil
	}
	return v, nil
}

// outputJSON returns the response's structured output, parsing Output when
// OutputJSON is not set.
func outputJSON(r *evalcore.Response) (any, error) {
	if r.OutputJSON != nil {
		return normalizeJSON(r.OutputJSON)
	}
	v, err := parseJSONText(r.Output)
	if err != nil {
		return nil, fmt.Errorf("output is not valid JSON: %w", err)
	}
	return v, nil
}

// parseJSONText parses s as JSON, tolerating surrounding whitespace and a
// Markdown code fence.
func parseJSONText(s string) (any, error) {
	s = stripFence(strings.TrimSpace(s))
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		return nil, err
	}
	return v, nil
}

// stripFence removes a surrounding ```lang ... ``` fence, if any.
func stripFence(s string) string {
	if !strings.HasPrefix(s, "```") || !strings.HasSuffix(s, "```") || len(s) < 6 {
		return s
	}
	inner := s[3 : len(s)-3]
	if nl := strings.IndexByte(inner, '\n'); nl >= 0 {
		first := strings.TrimSpace(inner[:nl])
		if first == "" || isIdent(first) {
			inner = inner[nl+1:]
		}
	}
	return strings.TrimSpace(inner)
}

func isIdent(s string) bool {
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

// normalizeJSON converts arbitrary Go values to generic JSON values.
func normalizeJSON(v any) (any, error) {
	switch v.(type) {
	case nil, string, float64, bool, map[string]any, []any:
		return v, nil
	case json.RawMessage:
		var out any
		err := json.Unmarshal(v.(json.RawMessage), &out)
		return out, err
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var out any
	err = json.Unmarshal(b, &out)
	return out, err
}

func renderTranscript(r *evalcore.Response) string {
	if len(r.Transcript) == 0 {
		return r.Output
	}
	var b strings.Builder
	for i, m := range r.Transcript {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(m.Role)
		b.WriteString(": ")
		b.WriteString(m.Content)
		for _, tc := range m.ToolCalls {
			b.WriteString("\n  [tool call] ")
			b.WriteString(tc.Name)
			if len(tc.Arguments) > 0 {
				b.WriteByte(' ')
				b.Write(tc.Arguments)
			}
		}
	}
	return b.String()
}

// toText renders a JSON-like value as text: strings verbatim, numbers in
// shortest form, everything else as compact JSON.
func toText(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(t)
	case json.Number:
		return t.String()
	}
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(b)
}

// ExpectedSource lets an evaluator take its reference value from the config
// or from the example.
type ExpectedSource struct {
	Expected     any    `json:"expected,omitempty" title:"Expected value" desc:"Fixed expected value. Leave empty to use each example's expected output."`
	ExpectedPath string `json:"expected_path,omitempty" title:"Expected path" desc:"JSONPath into the example's structured expected value, e.g. $.answer. Ignored when a fixed expected value is set."`

	ejp *jsonPath
}

func (e *ExpectedSource) validate() error {
	if e.ExpectedPath != "" {
		jp, err := compileJSONPath(e.ExpectedPath)
		if err != nil {
			return fmt.Errorf("expected_path: %w", err)
		}
		e.ejp = jp
	}
	return nil
}

var errNoExpected = errors.New("no expected value: set one in the evaluator config or add an expected output to the example")

// resolve returns the expected value for in.
func (e *ExpectedSource) resolve(in Input) (any, error) {
	if e.Expected != nil {
		return e.Expected, nil
	}
	v := in.example().Expected
	if v == nil {
		return nil, errNoExpected
	}
	if e.ejp != nil {
		if s, ok := v.(string); ok {
			parsed, err := parseJSONText(s)
			if err != nil {
				return nil, fmt.Errorf("expected_path set but example expected value is not JSON")
			}
			v = parsed
		} else {
			var err error
			if v, err = normalizeJSON(v); err != nil {
				return nil, err
			}
		}
		got, ok := e.ejp.get(v)
		if !ok {
			return nil, fmt.Errorf("expected_path %s not found in example expected value", e.ExpectedPath)
		}
		return got, nil
	}
	return v, nil
}

func (e *ExpectedSource) resolveText(in Input) (string, error) {
	v, err := e.resolve(in)
	if err != nil {
		return "", err
	}
	return toText(v), nil
}

// resolveList returns the expected value as a list of strings (a string
// yields a single-element list).
func (e *ExpectedSource) resolveList(in Input) ([]string, error) {
	v, err := e.resolve(in)
	if err != nil {
		return nil, err
	}
	return asStringList(v), nil
}

func asStringList(v any) []string {
	switch t := v.(type) {
	case []string:
		return t
	case []any:
		out := make([]string, len(t))
		for i, x := range t {
			out[i] = toText(x)
		}
		return out
	case nil:
		return nil
	}
	return []string{toText(v)}
}

func sortedKeys(m map[string]any) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}
