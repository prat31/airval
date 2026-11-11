package targets

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/airval/airval/internal/evalcore"
	"github.com/airval/airval/tools/mocktarget/mock"
)

func scripted(users ...string) *evalcore.Conversation {
	c := &evalcore.Conversation{}
	for _, u := range users {
		c.Turns = append(c.Turns, evalcore.Turn{Role: "user", Content: u})
	}
	return c
}

func turnDetails(t *testing.T, r evalcore.Response) []TurnDetail {
	t.Helper()
	var out []TurnDetail
	for _, s := range r.Trace {
		if s.Kind != "turn" {
			continue
		}
		var d TurnDetail
		if err := json.Unmarshal(s.Output, &d); err != nil {
			t.Fatal(err)
		}
		out = append(out, d)
	}
	return out
}

func TestScriptedConversationWithSessionID(t *testing.T) {
	srv := newMock(t, mockCfg())
	a := mustNew(t, "http", HTTPConfig{
		URL:      srv.URL + "/chat",
		Body:     `{"session_id": {{ json .session }}, "message": {{ json .message }}}`,
		Response: ResponseMapping{Output: "$.reply", InputTokens: "$.usage.input_tokens", OutputTokens: "$.usage.output_tokens"},
		Session:  &SessionConfig{Path: "$.session_id"},
	})
	e := evalcore.Example{ID: "c1", Conversation: scripted("hi there", "what next", "ok thanks bye")}
	e.Conversation.Turns = append(e.Conversation.Turns[:1], append([]evalcore.Turn{{Role: "assistant", Content: "expected, never sent"}}, e.Conversation.Turns[1:]...)...)
	r := invoke(t, a, e, InvokeOptions{})
	if len(r.Transcript) != 6 {
		t.Fatalf("transcript %+v", r.Transcript)
	}
	if r.Transcript[0].Role != "user" || r.Transcript[1].Role != "assistant" || !strings.HasPrefix(r.Transcript[1].Content, "Hello! ") {
		t.Fatalf("transcript %+v", r.Transcript[:2])
	}
	if !strings.HasPrefix(r.Transcript[3].Content, "(turn 2)") {
		t.Fatalf("session id not carried across turns: %+v", r.Transcript)
	}
	if r.Output != "Goodbye! Glad I could help." {
		t.Fatalf("final output %q", r.Output)
	}
	d := turnDetails(t, r)
	if len(d) != 3 || d[2].Turn != 2 || d[0].Session == "" || d[0].Session != d[2].Session {
		t.Fatalf("turn details %+v", d)
	}
	var sumIn int
	for _, x := range d {
		sumIn += x.Usage.InputTokens
	}
	if r.Usage.InputTokens != sumIn || sumIn == 0 {
		t.Fatalf("aggregated usage %+v vs %d", r.Usage, sumIn)
	}
	if r.Latency <= 0 {
		t.Fatal("latency not aggregated")
	}
}

func TestScriptedConversationAccumulatesMessages(t *testing.T) {
	srv := newMock(t, mockCfg())
	a := mustNew(t, "openai", OpenAIConfig{ChatCommon: ChatCommon{Model: "gpt-4o", SystemPrompt: "sys"}, BaseURL: srv.URL + "/v1"})
	e := evalcore.Example{Conversation: scripted("first q", "second q")}
	e.Conversation.Turns = append([]evalcore.Turn{{Role: "system", Content: "extra rule"}}, e.Conversation.Turns...)
	r := invoke(t, a, e, InvokeOptions{})
	if len(r.Transcript) != 5 { // system, u, a, u, a
		t.Fatalf("transcript %+v", r.Transcript)
	}
	// The last request carried the whole history (system merged, 3 messages + system).
	var rec struct {
		Body struct {
			Messages []map[string]string `json:"messages"`
		} `json:"body"`
	}
	var reqs []json.RawMessage
	if err := json.Unmarshal(r.Request, &reqs); err != nil || len(reqs) != 2 {
		t.Fatalf("requests: %s (%v)", r.Request, err)
	}
	if err := json.Unmarshal(reqs[1], &rec); err != nil {
		t.Fatal(err)
	}
	roles := []string{}
	for _, m := range rec.Body.Messages {
		roles = append(roles, m["role"])
	}
	if strings.Join(roles, ",") != "system,user,assistant,user" || !strings.Contains(rec.Body.Messages[0]["content"], "sys\n\nextra rule") {
		t.Fatalf("messages sent on turn 2: %v", rec.Body.Messages)
	}
	if got := len(turnDetails(t, r)); got != 2 {
		t.Fatalf("turn steps %d", got)
	}
}

func TestSimulatedConversation(t *testing.T) {
	srv := newMock(t, mockCfg())
	a := mustNew(t, "http", HTTPConfig{
		URL: srv.URL + "/chat", Body: `{"session_id": {{ json .session }}, "message": {{ json .message }}}`,
		Response: ResponseMapping{Output: "$.reply"}, Session: &SessionConfig{Path: "$.session_id"},
	})
	t.Run("stops on token", func(t *testing.T) {
		sim := &scriptedLLM{replies: []string{"I need help with my order", "it is order 55", StopToken}}
		e := evalcore.Example{Conversation: &evalcore.Conversation{Simulation: &evalcore.Simulation{
			Persona: "impatient customer", Goal: "get refund status", MaxTurns: 8, StopCondition: "refund status given"}}}
		r := invoke(t, a, e, InvokeOptions{LLM: sim})
		if len(r.Transcript) != 4 || r.Transcript[0].Content != "I need help with my order" || r.Transcript[2].Content != "it is order 55" {
			t.Fatalf("transcript %+v", r.Transcript)
		}
		if len(sim.calls) != 3 {
			t.Fatalf("simulator calls %d", len(sim.calls))
		}
		// Simulator saw persona/goal and roles flipped: its own words are "assistant".
		first, last := sim.calls[0], sim.calls[2]
		if !strings.Contains(first.System, "impatient customer") || !strings.Contains(first.System, "get refund status") || !strings.Contains(first.System, "refund status given") {
			t.Fatalf("system prompt: %s", first.System)
		}
		if len(last.Messages) != 4 || last.Messages[0].Role != "assistant" || last.Messages[1].Role != "user" || last.Messages[1].Content != r.Transcript[1].Content {
			t.Fatalf("flipped view: %+v", last.Messages)
		}
		d := turnDetails(t, r)
		if len(d) != 2 || d[0].Simulator == nil || d[0].Simulator.CostUSD != 0.001 {
			t.Fatalf("details %+v", d)
		}
		if r.CostUSD != 0 { // simulator cost excluded from target cost
			t.Fatalf("cost %v", r.CostUSD)
		}
	})
	t.Run("max turns and first message", func(t *testing.T) {
		sim := &scriptedLLM{replies: []string{"u2", "u3", "u4", "u5"}}
		e := evalcore.Example{Conversation: &evalcore.Conversation{Simulation: &evalcore.Simulation{FirstMessage: "hello!", MaxTurns: 3}}}
		r := invoke(t, a, e, InvokeOptions{LLM: sim})
		if len(r.Transcript) != 6 || r.Transcript[0].Content != "hello!" || len(sim.calls) != 2 {
			t.Fatalf("transcript %d calls %d", len(r.Transcript), len(sim.calls))
		}
	})
	t.Run("needs an LLM", func(t *testing.T) {
		e := evalcore.Example{Conversation: &evalcore.Conversation{Simulation: &evalcore.Simulation{Goal: "x"}}}
		_, err := a.Invoke(context.Background(), e, InvokeOptions{})
		if Classify(err) != ClassPermanent {
			t.Fatalf("%v", err)
		}
	})
}

func TestConversationErrorReturnsPartial(t *testing.T) {
	srv := newMock(t, mock.Config{Accuracy: 1})
	a := mustNew(t, "http", HTTPConfig{
		URL:  srv.URL + "/chat?mock_status={{ if eq .turn 1 }}503{{ end }}",
		Body: `{"message": {{ json .message }}}`, Response: ResponseMapping{Output: "$.reply"},
	})
	r, err := a.Invoke(context.Background(), evalcore.Example{Conversation: scripted("a", "b", "c")}, InvokeOptions{})
	if Classify(err) != ClassTransient {
		t.Fatalf("err %v", err)
	}
	if len(r.Transcript) != 3 { // u, a, u(failed)
		t.Fatalf("partial transcript %+v", r.Transcript)
	}
	if _, err := a.Invoke(context.Background(), evalcore.Example{Conversation: &evalcore.Conversation{}}, InvokeOptions{}); Classify(err) != ClassPermanent {
		t.Fatalf("empty conversation: %v", err)
	}
}
