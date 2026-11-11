package evaluators

import (
	"encoding/json"
	"reflect"
	"strconv"
	"strings"
)

// Config structs describe themselves with struct tags, and schemaFor turns
// them into a JSON Schema (draft 2020-12 subset) for the UI:
//
//	json:"name,omitempty"   property name (required unless omitempty or required:"false")
//	title:"..."             short field label
//	desc:"..."              help text shown under the field
//	enum:"a|b|c"            allowed values
//	default:"..."           default value (parsed per field type; JSON for slices)
//	min:"0" max:"1"         numeric bounds
//	required:"true|false"   override the omitempty-based requirement
//	ui:"textarea|code|secret|json|list"  rendering hint, emitted as x-ui
//
// Anonymous embedded structs are flattened.

var rawMessageType = reflect.TypeOf(json.RawMessage(nil))

func schemaFor(cfg any) json.RawMessage {
	t := reflect.TypeOf(cfg)
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	s := objectSchema(t)
	s["$schema"] = "https://json-schema.org/draft/2020-12/schema"
	b, err := json.Marshal(s)
	if err != nil {
		panic(err) // static data; a failure is a programming error
	}
	return b
}

func objectSchema(t reflect.Type) map[string]any {
	props := map[string]any{}
	var required []string
	var order []string
	collectFields(t, props, &required, &order)
	s := map[string]any{
		"type":                 "object",
		"properties":           props,
		"additionalProperties": false,
	}
	if len(order) > 0 {
		s["x-order"] = order
	}
	if len(required) > 0 {
		s["required"] = required
	}
	return s
}

func collectFields(t reflect.Type, props map[string]any, required, order *[]string) {
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.Anonymous && f.Type.Kind() == reflect.Struct {
			collectFields(f.Type, props, required, order)
			continue
		}
		if !f.IsExported() {
			continue
		}
		tag := f.Tag.Get("json")
		if tag == "-" {
			continue
		}
		name, opts, _ := strings.Cut(tag, ",")
		if name == "" {
			name = f.Name
		}
		ps := typeSchema(f.Type)
		if v := f.Tag.Get("title"); v != "" {
			ps["title"] = v
		}
		if v := f.Tag.Get("desc"); v != "" {
			ps["description"] = v
		}
		if v := f.Tag.Get("enum"); v != "" {
			vals := strings.Split(v, "|")
			enum := make([]any, len(vals))
			for i, e := range vals {
				enum[i] = e
			}
			if ps["type"] == "array" {
				ps["items"].(map[string]any)["enum"] = enum
			} else {
				ps["enum"] = enum
			}
		}
		if v := f.Tag.Get("default"); v != "" {
			ps["default"] = parseDefault(f.Type, v)
		}
		if v := f.Tag.Get("min"); v != "" {
			n, _ := strconv.ParseFloat(v, 64)
			ps["minimum"] = n
		}
		if v := f.Tag.Get("max"); v != "" {
			n, _ := strconv.ParseFloat(v, 64)
			ps["maximum"] = n
		}
		if v := f.Tag.Get("ui"); v != "" {
			ps["x-ui"] = v
		}
		props[name] = ps
		*order = append(*order, name)
		req := !strings.Contains(opts, "omitempty")
		if v := f.Tag.Get("required"); v != "" {
			req = v == "true"
		}
		if req {
			*required = append(*required, name)
		}
	}
}

func typeSchema(t reflect.Type) map[string]any {
	if t == rawMessageType {
		return map[string]any{}
	}
	switch t.Kind() {
	case reflect.Pointer:
		return typeSchema(t.Elem())
	case reflect.String:
		return map[string]any{"type": "string"}
	case reflect.Bool:
		return map[string]any{"type": "boolean"}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return map[string]any{"type": "integer"}
	case reflect.Float32, reflect.Float64:
		return map[string]any{"type": "number"}
	case reflect.Slice, reflect.Array:
		return map[string]any{"type": "array", "items": typeSchema(t.Elem())}
	case reflect.Map:
		return map[string]any{"type": "object", "additionalProperties": typeSchema(t.Elem())}
	case reflect.Struct:
		return objectSchema(t)
	case reflect.Interface:
		return map[string]any{}
	}
	return map[string]any{}
}

func parseDefault(t reflect.Type, v string) any {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.Bool:
		return v == "true"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n, _ := strconv.ParseInt(v, 10, 64)
		return n
	case reflect.Float32, reflect.Float64:
		n, _ := strconv.ParseFloat(v, 64)
		return n
	case reflect.String:
		return v
	}
	var out any
	if json.Unmarshal([]byte(v), &out) == nil {
		return out
	}
	return v
}
