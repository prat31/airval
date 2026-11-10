package targets

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"text/template"
	"text/template/parse"

	"github.com/airval/airval/internal/evalcore"
)

// envKey is the root-map key holding the per-call *renderEnv.
const envKey = "__env"

// renderEnv is the per-call state of template rendering: the secret resolver
// and the secret values that were substituted, for redaction.
type renderEnv struct {
	sec  evalcore.SecretResolver
	used []string
}

func newEnv(sec evalcore.SecretResolver) *renderEnv { return &renderEnv{sec: sec} }

func (e *renderEnv) resolve(name string) (string, error) {
	if e.sec == nil {
		return "", fmt.Errorf("secret %q requested but no secret resolver configured", name)
	}
	v, err := e.sec.Resolve(name)
	if err != nil {
		return "", fmt.Errorf("resolve secret %q: %w", name, err)
	}
	if v != "" {
		e.used = append(e.used, v)
	}
	return v, nil
}

// redactor returns a function that replaces every secret value substituted so
// far with [REDACTED].
func (e *renderEnv) redactor() func(string) string {
	if len(e.used) == 0 {
		return func(s string) string { return s }
	}
	pairs := make([]string, 0, len(e.used)*2)
	for _, u := range e.used {
		pairs = append(pairs, u, redacted)
	}
	r := strings.NewReplacer(pairs...)
	return r.Replace
}

const redacted = "[REDACTED]"

var funcMap = template.FuncMap{
	// secret is rewritten at compile time to receive the root data (`secret $ "NAME"`).
	"secret": func(root any, name string) (string, error) {
		m, ok := root.(map[string]any)
		if !ok {
			return "", fmt.Errorf("secret: no render environment")
		}
		env, ok := m[envKey].(*renderEnv)
		if !ok {
			return "", fmt.Errorf("secret: no render environment")
		}
		return env.resolve(name)
	},
	"json": func(v any) (string, error) {
		b, err := json.Marshal(v)
		return string(b), err
	},
	"default": func(def, v any) any {
		if isEmpty(v) {
			return def
		}
		return v
	},
	"upper": strings.ToUpper,
	"lower": strings.ToLower,
	"trim":  strings.TrimSpace,
	"join": func(sep string, v any) string {
		switch x := v.(type) {
		case []string:
			return strings.Join(x, sep)
		case []any:
			parts := make([]string, len(x))
			for i, e := range x {
				parts[i] = asString(e)
			}
			return strings.Join(parts, sep)
		}
		return asString(v)
	},
	"replace":  func(old, new, s string) string { return strings.ReplaceAll(s, old, new) },
	"contains": func(sub, s string) bool { return strings.Contains(s, sub) },
}

func isEmpty(v any) bool {
	switch x := v.(type) {
	case nil:
		return true
	case string:
		return x == ""
	case bool:
		return !x
	case float64:
		return x == 0
	case int:
		return x == 0
	case []any:
		return len(x) == 0
	case map[string]any:
		return len(x) == 0
	}
	return false
}

// Template is a pre-compiled text/template. Literal sources (no actions) are
// returned as-is without executing anything.
type Template struct {
	src    string
	static bool
	t      *template.Template
}

// CompileTemplate parses src once; the result is safe for concurrent use.
func CompileTemplate(name, src string) (*Template, error) {
	if !strings.Contains(src, "{{") {
		return &Template{src: src, static: true}, nil
	}
	t, err := template.New(name).Funcs(funcMap).Option("missingkey=zero").Parse(src)
	if err != nil {
		return nil, fmt.Errorf("template %s: %w", name, err)
	}
	if t.Tree != nil {
		rewriteSecret(t.Tree.Root)
	}
	return &Template{src: src, t: t}, nil
}

// Source returns the original template text.
func (t *Template) Source() string { return t.src }

// IsStatic reports whether the template has no actions.
func (t *Template) IsStatic() bool { return t.static }

var bufPool = sync.Pool{New: func() any { return new(bytes.Buffer) }}

var noValue = []byte("<no value>")

// Render executes the template against root, which must contain the render
// environment under envKey when the template uses `secret`.
func (t *Template) Render(root map[string]any) (string, error) {
	if t == nil {
		return "", nil
	}
	if t.static {
		return t.src, nil
	}
	buf := bufPool.Get().(*bytes.Buffer)
	buf.Reset()
	defer bufPool.Put(buf)
	if err := t.t.Execute(buf, root); err != nil {
		return "", fmt.Errorf("render template %s: %w", t.t.Name(), err)
	}
	b := buf.Bytes()
	if bytes.Contains(b, noValue) {
		b = bytes.ReplaceAll(b, noValue, nil)
	}
	return string(b), nil
}

// rewriteSecret turns `secret "X"` into `secret $ "X"` in the parse tree so the
// function can reach the per-call environment.
func rewriteSecret(n parse.Node) {
	switch n := n.(type) {
	case *parse.ListNode:
		if n == nil {
			return
		}
		for _, c := range n.Nodes {
			rewriteSecret(c)
		}
	case *parse.ActionNode:
		rewritePipe(n.Pipe)
	case *parse.IfNode:
		rewriteBranch(&n.BranchNode)
	case *parse.RangeNode:
		rewriteBranch(&n.BranchNode)
	case *parse.WithNode:
		rewriteBranch(&n.BranchNode)
	case *parse.TemplateNode:
		rewritePipe(n.Pipe)
	}
}

func rewriteBranch(b *parse.BranchNode) {
	rewritePipe(b.Pipe)
	rewriteSecret(b.List)
	if b.ElseList != nil {
		rewriteSecret(b.ElseList)
	}
}

func rewritePipe(p *parse.PipeNode) {
	if p == nil {
		return
	}
	for _, cmd := range p.Cmds {
		for _, a := range cmd.Args {
			if sub, ok := a.(*parse.PipeNode); ok {
				rewritePipe(sub)
			}
		}
		if len(cmd.Args) > 0 {
			if id, ok := cmd.Args[0].(*parse.IdentifierNode); ok && id.Ident == "secret" {
				root := &parse.VariableNode{NodeType: parse.NodeVariable, Pos: id.Pos, Ident: []string{"$"}}
				cmd.Args = append(cmd.Args[:1], append([]parse.Node{root}, cmd.Args[1:]...)...)
			}
		}
	}
}

// ---- template data ----

// turnCtx is everything one single request needs.
type turnCtx struct {
	ex       evalcore.Example
	opts     InvokeOptions
	messages []evalcore.Message // conversation so far incl. latest user message; empty for single-turn
	turn     int                // 0-based
	session  string
}

func (tc *turnCtx) data(env *renderEnv) map[string]any {
	ex := &tc.ex
	exMap := map[string]any{
		"id":       ex.ID,
		"metadata": ex.Metadata,
		"expected": ex.Expected,
		"tags":     ex.Tags,
		"context":  ex.Context,
	}
	d := map[string]any{
		"input":   ex.Input,
		"example": exMap,
		"trial":   tc.opts.Trial,
		"turn":    tc.turn,
		"session": tc.session,
		envKey:    env,
	}
	if len(tc.messages) > 0 {
		msgs := make([]any, len(tc.messages))
		last := ""
		for i, m := range tc.messages {
			msgs[i] = map[string]any{"role": m.Role, "content": m.Content}
			if m.Role == "user" {
				last = m.Content
			}
		}
		d["messages"] = msgs
		d["message"] = last
	} else {
		d["messages"] = []any{}
		d["message"] = ""
	}
	return d
}

// defaultUserText derives the user prompt from an example's input when the
// target has no user template: a lone string value is used verbatim, else JSON.
func defaultUserText(input map[string]any) string {
	if len(input) == 1 {
		for _, v := range input {
			if s, ok := v.(string); ok {
				return s
			}
		}
	}
	for _, k := range []string{"question", "prompt", "input", "query", "message"} {
		if s, ok := input[k].(string); ok {
			return s
		}
	}
	b, _ := json.Marshal(input)
	return string(b)
}
