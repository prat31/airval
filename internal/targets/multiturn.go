package targets

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/prat31/airval/internal/evalcore"
)

const (
	// StopToken ends a simulated conversation when the simulator emits it.
	StopToken       = "[END]"
	defaultSimTurns = 5
	maxSimTurns     = 50
)

// TurnDetail is the JSON stored in the Output of a TraceStep with Kind "turn".
type TurnDetail struct {
	Turn      int                 `json:"turn"`
	Output    string              `json:"output"`
	ToolCalls []evalcore.ToolCall `json:"tool_calls,omitempty"`
	Usage     evalcore.Usage      `json:"usage"`
	CostUSD   float64             `json:"cost_usd"`
	LatencyMS float64             `json:"latency_ms"`
	TTFTMS    float64             `json:"ttft_ms,omitempty"`
	Session   string              `json:"session,omitempty"`
	Expected  string              `json:"expected,omitempty"` // scripted per-turn expectation
	Simulator *SimulatorDetail    `json:"simulator,omitempty"`
}

// SimulatorDetail records the user-simulator's own usage for a simulated turn.
// Simulator cost is NOT included in Response.CostUSD / Usage.
type SimulatorDetail struct {
	Usage   evalcore.Usage `json:"usage"`
	CostUSD float64        `json:"cost_usd"`
}

type convState struct {
	resp    evalcore.Response
	msgs    []evalcore.Message
	session string
	turn    int
	reqs    []json.RawMessage
}

func (s *convState) send(ctx context.Context, call turnFunc, ex evalcore.Example, opts InvokeOptions,
	user string, expected string, sim *SimulatorDetail) error {

	s.msgs = append(s.msgs, evalcore.Message{Role: "user", Content: user})
	tc := &turnCtx{ex: ex, opts: opts, messages: s.msgs, turn: s.turn, session: s.session}
	startedAt := time.Now()
	r, sess, err := call(ctx, tc)
	if r.Request != nil {
		s.reqs = append(s.reqs, r.Request)
	}
	if err != nil {
		s.resp.Latency += r.Latency
		return err
	}
	if sess != "" {
		s.session = sess
	}
	s.msgs = append(s.msgs, evalcore.Message{Role: "assistant", Content: r.Output, ToolCalls: r.ToolCalls})

	in, _ := json.Marshal(map[string]string{"role": "user", "content": user})
	det := TurnDetail{
		Turn: s.turn, Output: r.Output, ToolCalls: r.ToolCalls, Usage: r.Usage, CostUSD: r.CostUSD,
		LatencyMS: float64(r.Latency) / float64(time.Millisecond), TTFTMS: float64(r.TTFT) / float64(time.Millisecond),
		Session: s.session, Expected: expected, Simulator: sim,
	}
	out, _ := json.Marshal(det)
	s.resp.Trace = append(s.resp.Trace, evalcore.TraceStep{
		Kind: "turn", Name: "turn-" + itoa(s.turn), Input: in, Output: out, StartedAt: startedAt, Duration: r.Latency,
	})
	s.resp.Trace = append(s.resp.Trace, r.Trace...)

	a := &s.resp
	a.Output, a.OutputJSON, a.Raw = r.Output, r.OutputJSON, r.Raw
	a.ToolCalls = append(a.ToolCalls, r.ToolCalls...)
	a.Context = append(a.Context, r.Context...)
	a.Usage.InputTokens += r.Usage.InputTokens
	a.Usage.OutputTokens += r.Usage.OutputTokens
	a.CostUSD += r.CostUSD
	a.Latency += r.Latency
	if s.turn == 0 {
		a.TTFT = r.TTFT
	}
	s.turn++
	return nil
}

func itoa(i int) string {
	b, _ := json.Marshal(i)
	return string(b)
}

func (s *convState) finish() evalcore.Response {
	r := s.resp
	r.Transcript = s.msgs
	if len(s.reqs) == 1 {
		r.Request = s.reqs[0]
	} else if len(s.reqs) > 1 {
		r.Request, _ = json.Marshal(s.reqs)
	}
	return r
}

// runConversation drives scripted or simulated multi-turn examples. Each turn
// is one call of the target; the transcript accumulates and is available to
// templates as .messages (and .session when a session path is configured).
// On error the partial response is returned alongside the error.
func runConversation(ctx context.Context, call turnFunc, ex evalcore.Example, opts InvokeOptions) (evalcore.Response, error) {
	conv := ex.Conversation
	st := &convState{}
	switch {
	case conv.Simulation != nil:
		if err := runSimulated(ctx, st, call, ex, opts, conv.Simulation); err != nil {
			return st.finish(), err
		}
	default:
		for _, t := range conv.Turns {
			switch t.Role {
			case "system":
				st.msgs = append(st.msgs, evalcore.Message{Role: "system", Content: t.Content})
			case "user":
				if err := st.send(ctx, call, ex, opts, t.Content, t.Expected, nil); err != nil {
					return st.finish(), err
				}
			}
			// assistant / tool turns in a script are expectations, not sent.
		}
	}
	if st.turn == 0 {
		return st.finish(), permanent("conversation has no user turns")
	}
	return st.finish(), nil
}

func runSimulated(ctx context.Context, st *convState, call turnFunc, ex evalcore.Example, opts InvokeOptions, sim *evalcore.Simulation) error {
	if opts.LLM == nil {
		return permanent("simulated conversation requires a user-simulator LLM (InvokeOptions.LLM)")
	}
	maxTurns := sim.MaxTurns
	if maxTurns <= 0 {
		maxTurns = defaultSimTurns
	}
	if maxTurns > maxSimTurns {
		maxTurns = maxSimTurns
	}
	system := simulatorPrompt(sim)
	for i := 0; i < maxTurns; i++ {
		var user string
		var detail *SimulatorDetail
		if i == 0 && strings.TrimSpace(sim.FirstMessage) != "" {
			user = sim.FirstMessage
		} else {
			resp, err := opts.LLM.Complete(ctx, evalcore.LLMRequest{
				System: system, Messages: simulatorView(st.msgs), Temperature: 0.7, MaxTokens: 400,
			})
			if err != nil {
				return err
			}
			detail = &SimulatorDetail{Usage: resp.Usage, CostUSD: resp.CostUSD}
			user = strings.TrimSpace(resp.Content)
			if strings.Contains(user, StopToken) || user == "" {
				return nil
			}
		}
		if err := st.send(ctx, call, ex, opts, user, "", detail); err != nil {
			return err
		}
	}
	return nil
}

func simulatorPrompt(sim *evalcore.Simulation) string {
	var b strings.Builder
	b.WriteString("You are role-playing a human user chatting with an AI assistant. Stay in character.\n")
	if sim.Persona != "" {
		b.WriteString("Persona: " + sim.Persona + "\n")
	}
	if sim.Goal != "" {
		b.WriteString("Goal: " + sim.Goal + "\n")
	}
	if sim.StopCondition != "" {
		b.WriteString("Stop condition: " + sim.StopCondition + "\n")
	}
	b.WriteString("Write ONLY the next message the user would send: first person, no quotes, no stage directions. ")
	b.WriteString("If the goal is achieved, the stop condition is met, or the conversation cannot progress, reply with exactly " + StopToken + " and nothing else.")
	return b.String()
}

// simulatorView flips roles so the simulator plays the "assistant" and the
// target's replies arrive as "user" messages.
func simulatorView(msgs []evalcore.Message) []evalcore.Message {
	if len(msgs) == 0 {
		return []evalcore.Message{{Role: "user", Content: "Begin the conversation now with your first message."}}
	}
	out := make([]evalcore.Message, 0, len(msgs))
	for _, m := range msgs {
		switch m.Role {
		case "user":
			out = append(out, evalcore.Message{Role: "assistant", Content: m.Content})
		case "assistant":
			out = append(out, evalcore.Message{Role: "user", Content: m.Content})
		}
	}
	return out
}
