package importers

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/airval/airval/internal/evalcore"
)

// Mapping tells the importer how source columns become Example fields. It is
// JSON-serializable and used verbatim by the API.
type Mapping struct {
	// InputColumns maps source column -> input key. Mutually usable with
	// WholeRowAsInput (explicit entries win for naming).
	InputColumns map[string]string `json:"input_columns,omitempty"`
	// WholeRowAsInput puts every column that is not mapped elsewhere into
	// Input under its own name.
	WholeRowAsInput bool `json:"whole_row_as_input,omitempty"`

	IDColumn       string `json:"id_column,omitempty"`
	ExpectedColumn string `json:"expected_column,omitempty"`

	ContextColumn string `json:"context_column,omitempty"` // string or array
	// ContextDelimiter splits string contexts into passages ("" = one passage;
	// JSON arrays encoded in strings are always parsed).
	ContextDelimiter string `json:"context_delimiter,omitempty"`

	TagsColumn    string `json:"tags_column,omitempty"`    // array or delimited string
	TagsDelimiter string `json:"tags_delimiter,omitempty"` // default ","

	// MetadataColumns are copied into Metadata. A "metadata." prefix on the
	// column name is stripped from the key.
	MetadataColumns []string `json:"metadata_columns,omitempty"`

	// ConversationColumn holds an OpenAI-style messages array
	// ([{"role":"user","content":"..."}]) as a JSON array or JSON string.
	ConversationColumn string `json:"conversation_column,omitempty"`
}

// Validate checks that the mapping can produce usable examples.
func (m Mapping) Validate() error {
	if len(m.InputColumns) == 0 && !m.WholeRowAsInput && m.ConversationColumn == "" {
		return errors.New("mapping: map at least one input column, a conversation column, or use the whole row as input")
	}
	return nil
}

func (m Mapping) usedColumns() map[string]bool {
	u := map[string]bool{}
	for _, c := range []string{m.IDColumn, m.ExpectedColumn, m.ContextColumn, m.TagsColumn, m.ConversationColumn} {
		if c != "" {
			u[c] = true
		}
	}
	for _, c := range m.MetadataColumns {
		u[c] = true
	}
	return u
}

// referencedColumns lists every column the mapping reads.
func (m Mapping) referencedColumns() []string {
	var out []string
	for c := range m.InputColumns {
		out = append(out, c)
	}
	for c := range m.usedColumns() {
		out = append(out, c)
	}
	return out
}

// converter turns Rows into Examples according to a Mapping.
type converter struct {
	m    Mapping
	used map[string]bool
}

func newConverter(m Mapping) *converter {
	return &converter{m: m, used: m.usedColumns()}
}

func (c *converter) convert(r Row) (evalcore.Example, error) {
	m := c.m
	ex := evalcore.Example{Input: make(map[string]any, len(m.InputColumns)+1)}

	if m.WholeRowAsInput {
		for i, k := range r.Keys {
			if c.used[k] {
				continue
			}
			key := k
			if mk, ok := m.InputColumns[k]; ok && mk != "" {
				key = mk
			}
			ex.Input[key] = r.Vals[i]
		}
	}
	for col, key := range m.InputColumns {
		if key == "" {
			key = col
		}
		if v, ok := r.Get(col); ok {
			ex.Input[key] = v
		}
	}

	if m.IDColumn != "" {
		if v, ok := r.Get(m.IDColumn); ok && !isEmptyValue(v) {
			ex.ID = scalarString(v)
		}
	}
	if m.ExpectedColumn != "" {
		if v, ok := r.Get(m.ExpectedColumn); ok && !isEmptyValue(v) {
			ex.Expected = v
		}
	}
	if m.ContextColumn != "" {
		if v, ok := r.Get(m.ContextColumn); ok {
			ex.Context = toStrings(v, m.ContextDelimiter)
		}
	}
	if m.TagsColumn != "" {
		if v, ok := r.Get(m.TagsColumn); ok {
			d := m.TagsDelimiter
			if d == "" {
				d = ","
			}
			ex.Tags = toStrings(v, d)
		}
	}
	if len(m.MetadataColumns) > 0 {
		for _, col := range m.MetadataColumns {
			v, ok := r.Get(col)
			if !ok || isEmptyValue(v) {
				continue
			}
			if ex.Metadata == nil {
				ex.Metadata = map[string]any{}
			}
			ex.Metadata[strings.TrimPrefix(col, "metadata.")] = v
		}
	}
	if m.ConversationColumn != "" {
		if v, ok := r.Get(m.ConversationColumn); ok && !isEmptyValue(v) {
			conv, err := parseConversation(v)
			if err != nil {
				return ex, fmt.Errorf("conversation: %w", err)
			}
			ex.Conversation = conv
		}
	}

	if len(ex.Input) == 0 && ex.Conversation == nil {
		return ex, errors.New("row has no input values")
	}
	return ex, nil
}

func scalarString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case nil:
		return ""
	case []any, map[string]any:
		b, _ := json.Marshal(t)
		return string(b)
	}
	return fmt.Sprint(v)
}

// toStrings converts an array or (optionally delimited / JSON-encoded) string
// into a list of non-empty strings.
func toStrings(v any, delim string) []string {
	switch t := v.(type) {
	case nil:
		return nil
	case []any:
		out := make([]string, 0, len(t))
		for _, e := range t {
			if s := scalarString(e); strings.TrimSpace(s) != "" {
				out = append(out, s)
			}
		}
		return nilIfEmpty(out)
	case string:
		s := strings.TrimSpace(t)
		if s == "" {
			return nil
		}
		if s[0] == '[' {
			var arr []any
			if json.Unmarshal([]byte(s), &arr) == nil {
				return toStrings(arr, "")
			}
		}
		if delim == "" {
			return []string{t}
		}
		var out []string
		for _, p := range strings.Split(s, delim) {
			if p = strings.TrimSpace(p); p != "" {
				out = append(out, p)
			}
		}
		return nilIfEmpty(out)
	}
	return []string{scalarString(v)}
}

func nilIfEmpty(s []string) []string {
	if len(s) == 0 {
		return nil
	}
	return s
}

// parseConversation converts an OpenAI-style messages array into Turns.
func parseConversation(v any) (*evalcore.Conversation, error) {
	if s, ok := v.(string); ok {
		var x any
		if err := json.Unmarshal([]byte(strings.TrimSpace(s)), &x); err != nil {
			return nil, fmt.Errorf("not valid JSON: %w", err)
		}
		v = normalize(x)
	}
	if m, ok := v.(map[string]any); ok {
		if inner, ok := m["messages"]; ok {
			v = inner
		} else if inner, ok := m["turns"]; ok {
			v = inner
		}
	}
	arr, ok := v.([]any)
	if !ok {
		return nil, errors.New("expected an array of messages")
	}
	conv := &evalcore.Conversation{}
	for i, e := range arr {
		msg, ok := e.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("message %d is not an object", i)
		}
		role, _ := msg["role"].(string)
		if role == "" {
			return nil, fmt.Errorf("message %d has no role", i)
		}
		t := evalcore.Turn{Role: strings.ToLower(role), Content: messageText(msg["content"])}
		if exp, ok := msg["expected"].(string); ok {
			t.Expected = exp
		}
		conv.Turns = append(conv.Turns, t)
	}
	if len(conv.Turns) == 0 {
		return nil, errors.New("no messages")
	}
	return conv, nil
}

// messageText flattens string content or OpenAI content-part arrays.
func messageText(c any) string {
	switch t := c.(type) {
	case nil:
		return ""
	case string:
		return t
	case []any:
		var parts []string
		for _, p := range t {
			if m, ok := p.(map[string]any); ok {
				if s, ok := m["text"].(string); ok {
					parts = append(parts, s)
					continue
				}
			}
			parts = append(parts, scalarString(p))
		}
		return strings.Join(parts, "\n")
	}
	return scalarString(c)
}

// ---- suggestion ----

var (
	conversationNames = []string{"messages", "turns", "conversation", "conversations", "dialogue", "dialog", "chat"}
	expectedNames     = []string{"expected", "expected_output", "expected_answer", "ground_truth", "groundtruth", "gold", "reference", "reference_answer", "reference_output", "answer", "label", "output", "target", "completion", "response"}
	contextNames      = []string{"context", "contexts", "retrieved_context", "retrieved_contexts", "reference_context", "reference_contexts", "documents", "passages", "docs", "evidence"}
	tagNames          = []string{"tags", "tag", "keywords"}
	idNames           = []string{"id", "example_id", "uuid", "_id"}
	inputNames        = []string{"input", "question", "query", "prompt", "instruction", "user_input", "utterance", "message", "text", "problem", "task"}
)

func normName(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '.' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	return strings.Trim(b.String(), "_")
}

// SuggestMapping heuristically maps columns (typically from Preview) to Example
// fields. It always returns a mapping that passes Validate when there is at
// least one column.
func SuggestMapping(cols []Column) Mapping {
	m := Mapping{}
	taken := map[string]bool{}
	byNorm := map[string]*Column{}
	for i := range cols {
		byNorm[normName(cols[i].Name)] = &cols[i]
	}
	pick := func(names []string) *Column {
		for _, n := range names {
			if c, ok := byNorm[n]; ok && !taken[c.Name] {
				taken[c.Name] = true
				return c
			}
		}
		return nil
	}

	if c := pick(conversationNames); c != nil {
		m.ConversationColumn = c.Name
	} else {
		for i := range cols {
			if cols[i].InferredType == "messages" && !taken[cols[i].Name] {
				taken[cols[i].Name] = true
				m.ConversationColumn = cols[i].Name
				break
			}
		}
	}
	if c := pick(expectedNames); c != nil {
		m.ExpectedColumn = c.Name
	}
	if c := pick(contextNames); c != nil {
		m.ContextColumn = c.Name
	}
	if c := pick(tagNames); c != nil {
		m.TagsColumn = c.Name
		if c.InferredType == "string" {
			m.TagsDelimiter = ","
		}
	}
	if c := pick(idNames); c != nil {
		m.IDColumn = c.Name
	}

	// Inputs: "input.<key>" columns (our own export format), then well-known names.
	for _, c := range cols {
		if taken[c.Name] {
			continue
		}
		n := normName(c.Name)
		if strings.HasPrefix(n, "input.") {
			if m.InputColumns == nil {
				m.InputColumns = map[string]string{}
			}
			m.InputColumns[c.Name] = strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(c.Name), "input."), "Input.")
			taken[c.Name] = true
		}
	}
	if m.InputColumns == nil {
		for _, n := range inputNames {
			if c, ok := byNorm[n]; ok && !taken[c.Name] {
				taken[c.Name] = true
				m.InputColumns = map[string]string{c.Name: c.Name}
				// further well-known input names (e.g. question + system prompt) are not auto-merged
				break
			}
		}
	}
	// Fallback: first free string column becomes the input.
	if m.InputColumns == nil && m.ConversationColumn == "" {
		for _, c := range cols {
			if !taken[c.Name] && c.InferredType == "string" {
				taken[c.Name] = true
				m.InputColumns = map[string]string{c.Name: c.Name}
				break
			}
		}
	}
	if m.InputColumns == nil && m.ConversationColumn == "" {
		for _, c := range cols {
			if !taken[c.Name] {
				taken[c.Name] = true
				m.InputColumns = map[string]string{c.Name: c.Name}
				break
			}
		}
	}
	if m.InputColumns == nil && m.ConversationColumn == "" && len(cols) > 0 {
		m.WholeRowAsInput = true
	}
	// Everything left is metadata.
	for _, c := range cols {
		if !taken[c.Name] {
			m.MetadataColumns = append(m.MetadataColumns, c.Name)
		}
	}
	return m
}
