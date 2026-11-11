package evaluators

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/prat31/airval/internal/evalcore"
)

// ---------------------------------------------------------------- is_json

// IsJSONConfig configures is_json.
type IsJSONConfig struct {
	FieldSelector
	AllowFenced *bool  `json:"allow_fenced,omitempty" title:"Allow code fence" default:"true" desc:"Accept JSON wrapped in a Markdown code fence (three backticks)."`
	Type        string `json:"type,omitempty" title:"Top-level type" enum:"any|object|array" default:"any" desc:"Require the top-level JSON value to be an object or an array."`
}

func (c *IsJSONConfig) Validate() error {
	if c.AllowFenced == nil {
		c.AllowFenced = boolp(true)
	}
	switch c.Type {
	case "":
		c.Type = "any"
	case "any", "object", "array":
	default:
		return fmt.Errorf("type: must be any, object or array (got %q)", c.Type)
	}
	return c.FieldSelector.validate()
}

func init() {
	register(KindInfo{
		Kind: "is_json", Name: "Valid JSON", Category: CategoryDeterministic, Group: groupStructure, DefaultThreshold: 1,
		Description: "Passes when the output is well-formed JSON, optionally requiring an object or array at the top level.",
	}, func(c *IsJSONConfig, _ Deps) (Evaluator, error) {
		return free("is_json", func(_ context.Context, in Input) (evalcore.Score, error) {
			var v any
			if c.Field == FieldOutputJSON {
				var err error
				if v, err = c.FieldSelector.value(in); err != nil {
					return boolScore(false, err.Error()), nil
				}
			} else {
				text, err := c.FieldSelector.text(in)
				if err != nil {
					return boolScore(false, err.Error()), nil
				}
				text = strings.TrimSpace(text)
				if *c.AllowFenced {
					text = stripFence(text)
				}
				if !json.Valid([]byte(text)) {
					var x any
					err := json.Unmarshal([]byte(text), &x)
					return boolScore(false, "not valid JSON: "+err.Error()), nil
				}
				if c.Type == "any" {
					return boolScore(true, "valid JSON"), nil
				}
				_ = json.Unmarshal([]byte(text), &v)
			}
			switch c.Type {
			case "object":
				if _, ok := v.(map[string]any); !ok {
					return boolScore(false, "valid JSON, but not an object"), nil
				}
			case "array":
				if _, ok := v.([]any); !ok {
					return boolScore(false, "valid JSON, but not an array"), nil
				}
			}
			return boolScore(true, "valid JSON"), nil
		}), nil
	})
}

// ---------------------------------------------------------------- json_schema

// JSONSchemaConfig configures json_schema.
type JSONSchemaConfig struct {
	FieldSelector
	Schema json.RawMessage `json:"schema,omitempty" title:"JSON Schema" ui:"json" desc:"JSON Schema (draft 2020-12 by default; $schema selects another draft) the output must satisfy. Leave empty to use each example's expected value as the schema."`

	compiled *jsonschema.Schema
}

func (c *JSONSchemaConfig) Validate() error {
	if err := c.FieldSelector.validate(); err != nil {
		return err
	}
	if len(c.Schema) > 0 && string(c.Schema) != "null" {
		s, err := compileSchema(c.Schema)
		if err != nil {
			return fmt.Errorf("schema: %w", err)
		}
		c.compiled = s
	}
	return nil
}

// noLoader refuses every external $ref so user schemas cannot read local
// files or reach the network.
type noLoader struct{}

func (noLoader) Load(url string) (any, error) {
	return nil, fmt.Errorf("external $ref %q is not allowed", url)
}

func compileSchema(raw []byte) (*jsonschema.Schema, error) {
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("not valid JSON: %w", err)
	}
	c := jsonschema.NewCompiler()
	c.UseLoader(noLoader{})
	const loc = "mem://airval/schema.json"
	if err := c.AddResource(loc, doc); err != nil {
		return nil, err
	}
	return c.Compile(loc)
}

var dynSchemaCache sync.Map // schema JSON → *jsonschema.Schema

func init() {
	register(KindInfo{
		Kind: "json_schema", Name: "JSON Schema", Category: CategoryDeterministic, Group: groupStructure, DefaultThreshold: 1,
		Description: "Passes when the output is JSON that satisfies a JSON Schema — required fields, types, enums and formats. Failures list exactly which rules broke.",
	}, func(c *JSONSchemaConfig, _ Deps) (Evaluator, error) {
		return free("json_schema", func(_ context.Context, in Input) (evalcore.Score, error) {
			sch := c.compiled
			if sch == nil {
				exp := in.example().Expected
				if exp == nil {
					return evalcore.Score{}, errors.New("no schema: set one in the evaluator config or put the schema in the example's expected value")
				}
				var raw []byte
				if s, ok := exp.(string); ok {
					raw = []byte(s)
				} else {
					var err error
					if raw, err = json.Marshal(exp); err != nil {
						return evalcore.Score{}, err
					}
				}
				if v, ok := dynSchemaCache.Load(string(raw)); ok {
					sch = v.(*jsonschema.Schema)
				} else {
					s, err := compileSchema(raw)
					if err != nil {
						return evalcore.Score{}, fmt.Errorf("example expected value is not a valid schema: %w", err)
					}
					dynSchemaCache.Store(string(raw), s)
					sch = s
				}
			}
			var inst any
			if c.Field == FieldOutputJSON {
				v, err := c.FieldSelector.value(in)
				if err != nil {
					return boolScore(false, err.Error()), nil
				}
				b, _ := json.Marshal(v)
				inst, _ = jsonschema.UnmarshalJSON(bytes.NewReader(b))
			} else {
				text, err := c.FieldSelector.text(in)
				if err != nil {
					return boolScore(false, err.Error()), nil
				}
				inst, err = jsonschema.UnmarshalJSON(strings.NewReader(stripFence(strings.TrimSpace(text))))
				if err != nil {
					return boolScore(false, "output is not valid JSON"), nil
				}
			}
			if err := sch.Validate(inst); err != nil {
				var ve *jsonschema.ValidationError
				msg := err.Error()
				if errors.As(err, &ve) {
					msg = schemaErrorSummary(ve)
				}
				return boolScore(false, msg), nil
			}
			return boolScore(true, "output satisfies the schema"), nil
		}), nil
	})
}

// schemaErrorSummary flattens a validation error tree into "at /a/b: msg"
// lines (leaf causes only, at most 5).
func schemaErrorSummary(ve *jsonschema.ValidationError) string {
	var leaves []string
	var walk func(e *jsonschema.ValidationError)
	walk = func(e *jsonschema.ValidationError) {
		if len(e.Causes) == 0 {
			loc := "/" + strings.Join(e.InstanceLocation, "/")
			msg := e.Error()
			// e.Error() includes a header line; keep the last line.
			if i := strings.LastIndex(msg, "\n"); i >= 0 {
				msg = strings.TrimSpace(msg[i+1:])
			}
			msg = strings.TrimPrefix(msg, "- ")
			if !strings.HasPrefix(msg, "at ") {
				msg = "at " + loc + ": " + msg
			}
			leaves = append(leaves, msg)
			return
		}
		for _, c := range e.Causes {
			walk(c)
		}
	}
	walk(ve)
	if len(leaves) > 5 {
		leaves = append(leaves[:5], fmt.Sprintf("… and %d more", len(leaves)-5))
	}
	return "schema violations: " + strings.Join(leaves, "; ")
}

// ---------------------------------------------------------------- jsonpath_equals

// JSONPathEqualsConfig configures jsonpath_equals.
type JSONPathEqualsConfig struct {
	JSONPath string `json:"json_path" title:"JSON path" desc:"JSONPath to the value in the output, e.g. $.order.status or $.items[*].id."`
	ExpectedSource
	IgnoreCase bool    `json:"ignore_case,omitempty" title:"Ignore case" desc:"Compare strings case-insensitively."`
	Tolerance  float64 `json:"tolerance,omitempty" title:"Numeric tolerance" min:"0" desc:"Allowed absolute difference when comparing numbers."`

	jp *jsonPath
}

func (c *JSONPathEqualsConfig) Validate() error {
	if c.JSONPath == "" {
		return errors.New("json_path is required")
	}
	jp, err := compileJSONPath(c.JSONPath)
	if err != nil {
		return fmt.Errorf("json_path: %w", err)
	}
	c.jp = jp
	if c.Tolerance < 0 {
		return errors.New("tolerance must be >= 0")
	}
	return c.ExpectedSource.validate()
}

func (c *JSONPathEqualsConfig) equal(a, b any) bool {
	if x, ok := toFloat(a); ok {
		if y, ok := toFloat(b); ok {
			return math.Abs(x-y) <= c.Tolerance
		}
	}
	if x, ok := a.(string); ok {
		if y, ok := b.(string); ok {
			if c.IgnoreCase {
				return strings.EqualFold(x, y)
			}
			return x == y
		}
		// "42" vs 42: compare textually.
		return x == toText(b)
	}
	if y, ok := b.(string); ok {
		return toText(a) == y
	}
	return jsonEqual(a, b)
}

func init() {
	register(KindInfo{
		Kind: "jsonpath_equals", Name: "JSON field equals", Category: CategoryDeterministic, Group: groupStructure, DefaultThreshold: 1,
		Requires:    []string{NeedsExpected},
		Description: "Picks a value out of the JSON output with a JSONPath and checks it equals the expected value. Numbers can use a tolerance.",
	}, func(c *JSONPathEqualsConfig, _ Deps) (Evaluator, error) {
		return free("jsonpath_equals", func(_ context.Context, in Input) (evalcore.Score, error) {
			exp, err := c.ExpectedSource.resolve(in)
			if err != nil {
				return evalcore.Score{}, err
			}
			root, err := outputJSON(in.response())
			if err != nil {
				return boolScore(false, err.Error()), nil
			}
			got, ok := c.jp.get(root)
			if !ok {
				return boolScore(false, fmt.Sprintf("%s not found in output", c.JSONPath)), nil
			}
			exp = normalizeMust(exp)
			if c.equal(got, exp) {
				return boolScore(true, fmt.Sprintf("%s = %s", c.JSONPath, trunc(toText(got), 80))), nil
			}
			return boolScore(false, fmt.Sprintf("%s: expected %s, got %s", c.JSONPath, trunc(toText(exp), 80), trunc(toText(got), 80))), nil
		}), nil
	})
}

// ---------------------------------------------------------------- numeric_close

// NumericCloseConfig configures numeric_close.
type NumericCloseConfig struct {
	FieldSelector
	ExpectedSource
	AbsTol  *float64 `json:"abs_tol,omitempty" title:"Absolute tolerance" min:"0" desc:"Pass if |output - expected| <= this."`
	RelTol  *float64 `json:"rel_tol,omitempty" title:"Relative tolerance" min:"0" desc:"Pass if |output - expected| <= this × |expected| (e.g. 0.01 = within 1%)."`
	Extract *bool    `json:"extract,omitempty" title:"Extract number" default:"true" desc:"Use the first number found in the text (e.g. \"The answer is 42.\" → 42). Off: the whole field must be a number."`
}

func (c *NumericCloseConfig) Validate() error {
	if c.Extract == nil {
		c.Extract = boolp(true)
	}
	if c.AbsTol == nil && c.RelTol == nil {
		c.AbsTol = f64(1e-9)
	}
	if (c.AbsTol != nil && *c.AbsTol < 0) || (c.RelTol != nil && *c.RelTol < 0) {
		return errors.New("tolerances must be >= 0")
	}
	if err := c.FieldSelector.validate(); err != nil {
		return err
	}
	return c.ExpectedSource.validate()
}

var numberRe = regexp.MustCompile(`[-+]?(?:(?:\d{1,3}(?:,\d{3})+|\d+)(?:\.\d+)?|\.\d+)(?:[eE][-+]?\d+)?`)

// firstNumber extracts the first number in s ("1,234.5" → 1234.5, "-3e2" → -300).
func firstNumber(s string) (float64, bool) {
	m := numberRe.FindString(s)
	if m == "" {
		return 0, false
	}
	f, err := strconv.ParseFloat(strings.ReplaceAll(m, ",", ""), 64)
	return f, err == nil
}

func parseNumber(v any, extract bool) (float64, bool) {
	if f, ok := toFloat(v); ok {
		return f, true
	}
	s := strings.TrimSpace(toText(v))
	if f, err := strconv.ParseFloat(strings.ReplaceAll(s, ",", ""), 64); err == nil {
		return f, true
	}
	if extract {
		return firstNumber(s)
	}
	return 0, false
}

func init() {
	register(KindInfo{
		Kind: "numeric_close", Name: "Number close to", Category: CategoryDeterministic, Group: groupStructure, DefaultThreshold: 1,
		Requires:    []string{NeedsExpected},
		Description: "Reads a number from the output and passes when it is within an absolute or relative tolerance of the expected number. Ideal for math and data-extraction answers.",
	}, func(c *NumericCloseConfig, _ Deps) (Evaluator, error) {
		return free("numeric_close", func(_ context.Context, in Input) (evalcore.Score, error) {
			ev, err := c.ExpectedSource.resolve(in)
			if err != nil {
				return evalcore.Score{}, err
			}
			exp, ok := parseNumber(ev, true)
			if !ok {
				return evalcore.Score{}, fmt.Errorf("expected value %q is not a number", trunc(toText(ev), 40))
			}
			gv, err := c.FieldSelector.value(in)
			if err != nil {
				return boolScore(false, err.Error()), nil
			}
			got, ok := parseNumber(gv, *c.Extract)
			if !ok {
				return boolScore(false, "no number found in output"), nil
			}
			diff := math.Abs(got - exp)
			tol := 0.0
			if c.AbsTol != nil {
				tol = *c.AbsTol
			}
			if c.RelTol != nil {
				tol = math.Max(tol, *c.RelTol*math.Abs(exp))
			}
			s := boolScore(diff <= tol, "")
			if *s.Passed {
				s.Reason = fmt.Sprintf("%g is within %g of %g", got, tol, exp)
			} else {
				s.Reason = fmt.Sprintf("%g differs from %g by %g (tolerance %g)", got, exp, diff, tol)
			}
			s.Meta = map[string]any{"actual": got, "expected": exp, "diff": diff}
			return s, nil
		}), nil
	})
}
