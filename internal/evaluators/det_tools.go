package evaluators

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/airval/airval/internal/evalcore"
)

// toolCalls returns the tool calls the target made: Response.ToolCalls, else
// the calls recorded in the transcript, else tool steps of the trace.
func toolCalls(r *evalcore.Response) []evalcore.ToolCall {
	if len(r.ToolCalls) > 0 {
		return r.ToolCalls
	}
	var out []evalcore.ToolCall
	for _, m := range r.Transcript {
		out = append(out, m.ToolCalls...)
	}
	if len(out) > 0 {
		return out
	}
	for _, st := range r.Trace {
		if st.Kind == "tool" {
			out = append(out, evalcore.ToolCall{Name: st.Name, Arguments: st.Input, Result: st.Output})
		}
	}
	return out
}

func toolNames(calls []evalcore.ToolCall) []string {
	out := make([]string, len(calls))
	for i, c := range calls {
		out[i] = c.Name
	}
	return out
}

// jsonSubset reports whether want is contained in got: objects recursively
// (extra keys in got are fine), arrays and scalars by equality.
func jsonSubset(want, got any) bool {
	if wm, ok := want.(map[string]any); ok {
		gm, ok := got.(map[string]any)
		if !ok {
			return false
		}
		for k, wv := range wm {
			gv, ok := gm[k]
			if !ok || !jsonSubset(wv, gv) {
				return false
			}
		}
		return true
	}
	return jsonEqual(want, got)
}

func decodeArgs(raw json.RawMessage) any {
	if len(raw) == 0 {
		return map[string]any{}
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil
	}
	// Some providers send arguments as a JSON-encoded string.
	if s, ok := v.(string); ok {
		var inner any
		if json.Unmarshal([]byte(s), &inner) == nil {
			return inner
		}
	}
	return v
}

// ToolCalledConfig configures tool_called.
type ToolCalledConfig struct {
	Name      string         `json:"name,omitempty" title:"Tool name" desc:"Tool/function that must be called. Leave empty to read it from the example's expected value ({\"name\": ..., \"arguments\": {...}} or just the name)."`
	Arguments map[string]any `json:"arguments,omitempty" title:"Arguments" ui:"json" desc:"Arguments the call must include. Extra arguments are fine unless exact_args is on."`
	ExactArgs bool           `json:"exact_args,omitempty" title:"Exact arguments" desc:"Require the arguments to match exactly, with no extra keys."`
	MinCount  *int           `json:"min_count,omitempty" title:"Min calls" default:"1" min:"0" desc:"Minimum number of matching calls."`
	MaxCount  *int           `json:"max_count,omitempty" title:"Max calls" min:"0" desc:"Maximum number of matching calls. Set min 0 and max 0 to require that the tool is NOT called."`
}

func (c *ToolCalledConfig) Validate() error {
	if c.MinCount == nil {
		one := 1
		c.MinCount = &one
	}
	if *c.MinCount < 0 || (c.MaxCount != nil && *c.MaxCount < 0) {
		return errors.New("counts must be >= 0")
	}
	if c.MaxCount != nil && *c.MinCount > *c.MaxCount {
		return errors.New("min_count must not exceed max_count")
	}
	return nil
}

func init() {
	register(KindInfo{
		Kind: "tool_called", Name: "Tool called", Category: CategoryDeterministic, Group: groupTools, DefaultThreshold: 1,
		Requires:    []string{NeedsToolCalls},
		ApplyTo:     []evalcore.ApplyTo{evalcore.ApplyFinal, evalcore.ApplyPerTurn, evalcore.ApplyConversation, evalcore.ApplyTrace},
		Description: "Checks that the agent called a specific tool — optionally with certain arguments and a number of times — or that it did not call it.",
	}, func(c *ToolCalledConfig, _ Deps) (Evaluator, error) {
		var wantArgs any
		if c.Arguments != nil {
			wantArgs = normalizeMust(map[string]any(c.Arguments))
		}
		return free("tool_called", func(_ context.Context, in Input) (evalcore.Score, error) {
			name, args := c.Name, wantArgs
			if name == "" {
				exp := in.example().Expected
				switch t := exp.(type) {
				case string:
					name = t
				case map[string]any:
					name, _ = t["name"].(string)
					if a, ok := t["arguments"]; ok && args == nil {
						args = a
					}
				}
				if name == "" {
					return evalcore.Score{}, errors.New("no tool name: set name in the config or put it in the example's expected value")
				}
			}
			calls := toolCalls(in.response())
			n := 0
			nameOnly := 0
			for _, tc := range calls {
				if tc.Name != name {
					continue
				}
				nameOnly++
				if args != nil {
					got := decodeArgs(tc.Arguments)
					if c.ExactArgs && !jsonEqual(args, got) || !c.ExactArgs && !jsonSubset(args, got) {
						continue
					}
				}
				n++
			}
			ok := n >= *c.MinCount && (c.MaxCount == nil || n <= *c.MaxCount)
			var reason string
			switch {
			case ok && c.MaxCount != nil && *c.MaxCount == 0:
				reason = fmt.Sprintf("%s was not called", name)
			case ok:
				reason = fmt.Sprintf("%s called %d time(s) with matching arguments", name, n)
			case n == 0 && nameOnly > 0:
				reason = fmt.Sprintf("%s was called %d time(s) but never with the expected arguments", name, nameOnly)
			case n == 0:
				reason = fmt.Sprintf("%s was never called (calls: %s)", name, strings.Join(toolNames(calls), ", "))
			case n < *c.MinCount:
				reason = fmt.Sprintf("%s matched %d time(s), expected at least %d", name, n, *c.MinCount)
			default:
				reason = fmt.Sprintf("%s matched %d time(s), expected at most %d", name, n, *c.MaxCount)
			}
			s := boolScore(ok, reason)
			s.Meta = map[string]any{"matches": n}
			return s, nil
		}), nil
	})
}

// ToolSequenceConfig configures tool_sequence.
type ToolSequenceConfig struct {
	Sequence []string `json:"sequence,omitempty" title:"Expected tools" ui:"list" desc:"Tool names in the expected order. Leave empty to use the example's expected value (a list of names)."`
	Mode     string   `json:"mode,omitempty" title:"Match mode" enum:"exact|in_order|any_order" default:"in_order" desc:"exact: the calls must be exactly this list. in_order: these tools in this order, other calls allowed in between. any_order: all of these tools, in any order."`
}

func (c *ToolSequenceConfig) Validate() error {
	switch c.Mode {
	case "":
		c.Mode = "in_order"
	case "exact", "in_order", "any_order":
	default:
		return fmt.Errorf("mode: must be exact, in_order or any_order (got %q)", c.Mode)
	}
	return nil
}

func init() {
	register(KindInfo{
		Kind: "tool_sequence", Name: "Tool sequence", Category: CategoryDeterministic, Group: groupTools, DefaultThreshold: 1,
		Requires:    []string{NeedsToolCalls},
		ApplyTo:     []evalcore.ApplyTo{evalcore.ApplyFinal, evalcore.ApplyConversation, evalcore.ApplyTrace},
		Description: "Checks the order of the agent's tool calls: an exact list, an ordered subsequence, or a set in any order. Partial credit shows how much of the plan was followed.",
	}, func(c *ToolSequenceConfig, _ Deps) (Evaluator, error) {
		return free("tool_sequence", func(_ context.Context, in Input) (evalcore.Score, error) {
			want := c.Sequence
			if len(want) == 0 {
				var err error
				if want, err = (&ExpectedSource{}).resolveList(in); err != nil {
					return evalcore.Score{}, err
				}
			}
			if len(want) == 0 {
				return evalcore.Score{}, errors.New("expected tool sequence is empty")
			}
			got := toolNames(toolCalls(in.response()))
			gotStr := strings.Join(got, " → ")
			if gotStr == "" {
				gotStr = "(no tool calls)"
			}
			switch c.Mode {
			case "exact":
				eq := len(got) == len(want)
				for i := 0; eq && i < len(want); i++ {
					eq = got[i] == want[i]
				}
				if eq {
					return boolScore(true, "tool calls match exactly: "+gotStr), nil
				}
				return boolScore(false, fmt.Sprintf("expected %s, got %s", strings.Join(want, " → "), gotStr)), nil
			case "any_order":
				counts := make(map[string]int, len(got))
				for _, g := range got {
					counts[g]++
				}
				matched := 0
				var missing []string
				for _, w := range want {
					if counts[w] > 0 {
						counts[w]--
						matched++
					} else {
						missing = append(missing, w)
					}
				}
				v := float64(matched) / float64(len(want))
				if len(missing) == 0 {
					return valueScore(1, 1, "all expected tools called: "+gotStr), nil
				}
				return valueScore(v, 1, "missing tools: "+strings.Join(missing, ", ")+"; got "+gotStr), nil
			default:
				l := lcsLen(want, got)
				v := float64(l) / float64(len(want))
				if l == len(want) {
					return valueScore(1, 1, "expected tools called in order: "+gotStr), nil
				}
				return valueScore(v, 1, fmt.Sprintf("%d of %d expected tools called in order; got %s", l, len(want), gotStr)), nil
			}
		}), nil
	})
}
