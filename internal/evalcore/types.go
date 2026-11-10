// Package evalcore holds the small, dependency-free domain types shared by the
// execution pipeline: target adapters (internal/targets) produce a Response for
// an Example, and evaluators (internal/evaluators) turn (Example, Response) into
// Scores. Store models map to and from these types; nothing here imports store.
//
// This is a frozen contract between parallel workstreams. Extend it only by
// adding optional fields.
package evalcore

import (
	"context"
	"encoding/json"
	"time"
)

// Example is one dataset row as seen by the execution pipeline.
type Example struct {
	ID           string         `json:"id"`
	Input        map[string]any `json:"input"`              // template variables, e.g. {"question": "..."}
	Expected     any            `json:"expected,omitempty"` // reference answer (string or structured)
	Context      []string       `json:"context,omitempty"`  // reference / gold context passages
	Metadata     map[string]any `json:"metadata,omitempty"`
	Tags         []string       `json:"tags,omitempty"`
	Conversation *Conversation  `json:"conversation,omitempty"` // multi-turn script or simulation
}

// Conversation describes a multi-turn example. Exactly one of Turns or
// Simulation is set.
type Conversation struct {
	Turns      []Turn      `json:"turns,omitempty"`      // scripted user turns (role=user); assistant turns optional as expectations
	Simulation *Simulation `json:"simulation,omitempty"` // LLM-simulated user
}

type Turn struct {
	Role     string `json:"role"` // user | assistant | system | tool
	Content  string `json:"content"`
	Expected string `json:"expected,omitempty"` // optional per-turn expectation for user turns
}

type Simulation struct {
	Persona       string `json:"persona"`
	Goal          string `json:"goal"`
	FirstMessage  string `json:"first_message,omitempty"`
	MaxTurns      int    `json:"max_turns"`
	StopCondition string `json:"stop_condition,omitempty"` // natural-language condition judged by the simulator
}

// ToolCall is a tool/function invocation made by the target.
type ToolCall struct {
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
	Result    json.RawMessage `json:"result,omitempty"`
}

// TraceStep is one intermediate step of an agent / RAG pipeline.
type TraceStep struct {
	Kind      string          `json:"kind"` // llm | tool | retrieval | custom
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	Output    json.RawMessage `json:"output,omitempty"`
	StartedAt time.Time       `json:"started_at,omitempty"`
	Duration  time.Duration   `json:"duration,omitempty"`
}

type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// Message is a chat message in a multi-turn transcript.
type Message struct {
	Role      string     `json:"role"`
	Content   string     `json:"content"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
}

// Response is what a target adapter returns for one Example (one trial).
type Response struct {
	Output     string          `json:"output"`                // final text output
	OutputJSON any             `json:"output_json,omitempty"` // parsed structured output, if any
	Context    []string        `json:"context,omitempty"`     // retrieved context (RAG)
	ToolCalls  []ToolCall      `json:"tool_calls,omitempty"`
	Trace      []TraceStep     `json:"trace,omitempty"`
	Transcript []Message       `json:"transcript,omitempty"` // full multi-turn transcript
	Usage      Usage           `json:"usage"`
	CostUSD    float64         `json:"cost_usd"`
	Latency    time.Duration   `json:"latency"`
	TTFT       time.Duration   `json:"ttft,omitempty"`
	Raw        json.RawMessage `json:"raw,omitempty"`     // raw response body (may be truncated)
	Request    json.RawMessage `json:"request,omitempty"` // rendered request (secrets redacted)
	Cached     bool            `json:"cached,omitempty"`
}

// Score is an evaluator's verdict on one Response.
type Score struct {
	EvaluatorID string         `json:"evaluator_id"`
	Value       *float64       `json:"value,omitempty"` // normalized 0..1
	Label       string         `json:"label,omitempty"`
	Passed      *bool          `json:"passed,omitempty"`
	Reason      string         `json:"reason,omitempty"`
	Meta        map[string]any `json:"meta,omitempty"`
	Latency     time.Duration  `json:"latency"`
	CostUSD     float64        `json:"cost_usd"`
}

// Severity of an evaluator failure, used by gates.
type Severity string

const (
	SeverityCritical Severity = "critical"
	SeverityMajor    Severity = "major"
	SeverityMinor    Severity = "minor"
)

// ApplyTo selects what an evaluator scores.
type ApplyTo string

const (
	ApplyFinal        ApplyTo = "final"
	ApplyPerTurn      ApplyTo = "per_turn"
	ApplyConversation ApplyTo = "conversation"
	ApplyTrace        ApplyTo = "trace"
)

// SecretResolver resolves secret names to plaintext at execution time.
type SecretResolver interface {
	Resolve(name string) (string, error)
}

// LLM is the minimal chat-completion interface used by LLM judges and the user
// simulator. Implemented by provider clients in internal/targets.
type LLM interface {
	Complete(ctx context.Context, req LLMRequest) (LLMResponse, error)
}

type LLMRequest struct {
	Model       string    `json:"model"`
	System      string    `json:"system,omitempty"`
	Messages    []Message `json:"messages"`
	Temperature float64   `json:"temperature"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
	JSONMode    bool      `json:"json_mode,omitempty"`
	Logprobs    bool      `json:"logprobs,omitempty"`
}

type LLMResponse struct {
	Content     string             `json:"content"`
	Usage       Usage              `json:"usage"`
	CostUSD     float64            `json:"cost_usd"`
	TopLogprobs map[string]float64 `json:"top_logprobs,omitempty"` // first-token alternatives, if supported
}
