package targets

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/airval/airval/internal/evalcore"
)

// ChatCommon holds the settings shared by the LLM-provider kinds
// (openai, anthropic, bedrock, vertex).
type ChatCommon struct {
	HTTPCommon
	Model string `json:"model"`
	// SystemPrompt is a template (may use .input, .example, secret...).
	SystemPrompt string `json:"system_prompt,omitempty"`
	// UserTemplate renders the user message of single-turn examples from the
	// example. Default: the lone/primary string in the input, else its JSON.
	UserTemplate string   `json:"user_template,omitempty"`
	Temperature  *float64 `json:"temperature,omitempty"`
	MaxTokens    int      `json:"max_tokens,omitempty"`
	// Pricing overrides the built-in price table (USD per 1M tokens).
	Pricing *Pricing `json:"pricing,omitempty"`
}

// Pricing is USD per one million tokens.
type Pricing struct {
	InputPer1M  float64 `json:"input_per_1m"`
	OutputPer1M float64 `json:"output_per_1m"`
}

func (c *ChatCommon) validate() error {
	if err := c.HTTPCommon.validate(); err != nil {
		return err
	}
	if strings.TrimSpace(c.Model) == "" {
		return fmt.Errorf("model is required")
	}
	if c.Temperature != nil && (*c.Temperature < 0 || *c.Temperature > 2) {
		return fmt.Errorf("temperature must be within [0,2]")
	}
	if c.MaxTokens < 0 {
		return fmt.Errorf("max_tokens must be >= 0")
	}
	if c.Pricing != nil && (c.Pricing.InputPer1M < 0 || c.Pricing.OutputPer1M < 0) {
		return fmt.Errorf("pricing values must be >= 0")
	}
	for _, t := range []struct{ n, s string }{{"system_prompt", c.SystemPrompt}, {"user_template", c.UserTemplate}} {
		if _, err := CompileTemplate(t.n, t.s); err != nil {
			return err
		}
	}
	return nil
}

type chatParams struct {
	Model       string
	System      string
	Messages    []evalcore.Message // no system-role messages
	Temperature *float64
	MaxTokens   int
	JSONMode    bool
	Logprobs    bool
	NoTools     bool // LLM-client use: never send configured tools
}

type chatResult struct {
	Text        string
	ToolCalls   []evalcore.ToolCall
	Usage       evalcore.Usage
	CostUSD     float64
	TTFT        time.Duration
	Latency     time.Duration
	Raw         json.RawMessage
	Request     json.RawMessage
	TopLogprobs map[string]float64
}

// provider performs one chat exchange against an LLM API.
type provider interface {
	chat(ctx context.Context, env *renderEnv, p chatParams) (chatResult, error)
	jsonConfigured() bool
}

// splitSystem moves system-role messages into the system string.
func splitSystem(system string, msgs []evalcore.Message) (string, []evalcore.Message) {
	hasSys := false
	for _, m := range msgs {
		if m.Role == "system" {
			hasSys = true
			break
		}
	}
	if !hasSys {
		return system, msgs
	}
	var parts []string
	if system != "" {
		parts = append(parts, system)
	}
	out := make([]evalcore.Message, 0, len(msgs))
	for _, m := range msgs {
		if m.Role == "system" {
			parts = append(parts, m.Content)
		} else {
			out = append(out, m)
		}
	}
	return strings.Join(parts, "\n\n"), out
}

type chatTarget struct {
	prov   provider
	cfg    *ChatCommon
	system *Template
	user   *Template
}

func newChatAdapter(kind string, cfg *ChatCommon, p provider) (Adapter, error) {
	ct := &chatTarget{prov: p, cfg: cfg}
	var err error
	if ct.system, err = CompileTemplate("system_prompt", cfg.SystemPrompt); err != nil {
		return nil, err
	}
	if cfg.UserTemplate != "" {
		if ct.user, err = CompileTemplate("user_template", cfg.UserTemplate); err != nil {
			return nil, err
		}
	}
	return &adapter{kind: kind, call: ct.call}, nil
}

func (c *chatTarget) call(ctx context.Context, tc *turnCtx) (evalcore.Response, string, error) {
	var resp evalcore.Response
	env := newEnv(tc.opts.Secrets)
	data := tc.data(env)
	system, err := c.system.Render(data)
	if err != nil {
		return resp, "", permanent("%v", err)
	}
	msgs := tc.messages
	if len(msgs) == 0 {
		text := ""
		if c.user != nil {
			if text, err = c.user.Render(data); err != nil {
				return resp, "", permanent("%v", err)
			}
		} else {
			text = defaultUserText(tc.ex.Input)
		}
		msgs = []evalcore.Message{{Role: "user", Content: text}}
	}
	system, msgs = splitSystem(system, msgs)
	res, err := c.prov.chat(ctx, env, chatParams{
		Model:       c.cfg.Model,
		System:      system,
		Messages:    msgs,
		Temperature: c.cfg.Temperature,
		MaxTokens:   c.cfg.MaxTokens,
	})
	resp.Request = res.Request
	resp.Latency = res.Latency
	if err != nil {
		return resp, "", err
	}
	resp.Output = res.Text
	resp.ToolCalls = res.ToolCalls
	resp.Usage = res.Usage
	resp.CostUSD = res.CostUSD
	resp.TTFT = res.TTFT
	resp.Raw = res.Raw
	if c.prov.jsonConfigured() {
		if t := strings.TrimSpace(res.Text); len(t) > 0 && (t[0] == '{' || t[0] == '[') {
			var v any
			if json.Unmarshal([]byte(t), &v) == nil {
				resp.OutputJSON = v
			}
		}
	}
	return resp, "", nil
}

// ---- evalcore.LLM implementation ----

type providerLLM struct {
	prov  provider
	model string
	sec   evalcore.SecretResolver
}

// NewLLM builds an evalcore.LLM (used by LLM judges and the user simulator)
// for an LLM-provider kind: openai, anthropic, bedrock or vertex. The config is
// the same as for the target kind; tools are never sent. LLMRequest.Model, when
// set, overrides the configured model.
func NewLLM(kind string, raw json.RawMessage, deps Deps, sec evalcore.SecretResolver) (evalcore.LLM, error) {
	cfg, err := ParseConfig(kind, raw)
	if err != nil {
		return nil, err
	}
	var (
		p     provider
		model string
	)
	switch c := cfg.(type) {
	case *OpenAIConfig:
		p, err = newOpenAIProvider(c, deps)
		model = c.Model
	case *AnthropicConfig:
		p, err = newAnthropicProvider(c, deps)
		model = c.Model
	case *BedrockConfig:
		p, err = newBedrockProvider(c, deps)
		model = c.Model
	case *VertexConfig:
		p, err = newVertexProvider(c, deps)
		model = c.Model
	default:
		return nil, fmt.Errorf("kind %q cannot be used as an LLM", kind)
	}
	if err != nil {
		return nil, err
	}
	return &providerLLM{prov: p, model: model, sec: sec}, nil
}

func (l *providerLLM) Complete(ctx context.Context, req evalcore.LLMRequest) (evalcore.LLMResponse, error) {
	model := req.Model
	if model == "" {
		model = l.model
	}
	temp := req.Temperature
	system, msgs := splitSystem(req.System, req.Messages)
	res, err := l.prov.chat(ctx, newEnv(l.sec), chatParams{
		Model: model, System: system, Messages: msgs, Temperature: &temp, MaxTokens: req.MaxTokens,
		JSONMode: req.JSONMode, Logprobs: req.Logprobs, NoTools: true,
	})
	if err != nil {
		return evalcore.LLMResponse{}, err
	}
	return evalcore.LLMResponse{Content: res.Text, Usage: res.Usage, CostUSD: res.CostUSD, TopLogprobs: res.TopLogprobs}, nil
}

// ---- pricing ----

type priceEntry struct {
	key     string
	in, out float64 // USD per 1M tokens
}

// Approximate list prices; override per target with `pricing`.
var builtinPrices = []priceEntry{
	// OpenAI
	{"gpt-5-nano", 0.05, 0.40}, {"gpt-5-mini", 0.25, 2}, {"gpt-5", 1.25, 10},
	{"gpt-4.1-nano", 0.10, 0.40}, {"gpt-4.1-mini", 0.40, 1.60}, {"gpt-4.1", 2, 8},
	{"gpt-4o-mini", 0.15, 0.60}, {"gpt-4o", 2.5, 10}, {"gpt-4-turbo", 10, 30}, {"gpt-4", 30, 60},
	{"gpt-3.5-turbo", 0.5, 1.5},
	{"o4-mini", 1.1, 4.4}, {"o3-mini", 1.1, 4.4}, {"o3", 2, 8}, {"o1-mini", 1.1, 4.4}, {"o1", 15, 60},
	// Anthropic (also matches Bedrock / Vertex model ids)
	{"claude-opus-4-5", 5, 25}, {"claude-sonnet-4-5", 3, 15}, {"claude-haiku-4-5", 1, 5},
	{"claude-opus-4", 15, 75}, {"claude-sonnet-4", 3, 15},
	{"claude-3-7-sonnet", 3, 15}, {"claude-3-5-sonnet", 3, 15}, {"claude-3-5-haiku", 0.8, 4},
	{"claude-3-opus", 15, 75}, {"claude-3-sonnet", 3, 15}, {"claude-3-haiku", 0.25, 1.25},
	// Google
	{"gemini-2.5-pro", 1.25, 10}, {"gemini-2.5-flash-lite", 0.1, 0.4}, {"gemini-2.5-flash", 0.3, 2.5},
	{"gemini-2.0-flash-lite", 0.075, 0.3}, {"gemini-2.0-flash", 0.1, 0.4},
	{"gemini-1.5-pro", 1.25, 5}, {"gemini-1.5-flash", 0.075, 0.3},
	// Bedrock native
	{"amazon.nova-pro", 0.8, 3.2}, {"amazon.nova-lite", 0.06, 0.24}, {"amazon.nova-micro", 0.035, 0.14},
	{"llama3-1-70b", 0.72, 0.72}, {"llama3-1-8b", 0.22, 0.22},
}

// lookupPrice finds the longest table key appearing in model at a boundary
// ("", ".", "/", ":" or "-" before it).
func lookupPrice(model string) (in, out float64, ok bool) {
	m := strings.ToLower(model)
	best := -1
	for i, e := range builtinPrices {
		idx := strings.Index(m, e.key)
		if idx < 0 {
			continue
		}
		if idx > 0 && !strings.ContainsRune("./:-", rune(m[idx-1])) {
			continue
		}
		if best < 0 || len(e.key) > len(builtinPrices[best].key) {
			best = i
		}
	}
	if best < 0 {
		return 0, 0, false
	}
	return builtinPrices[best].in, builtinPrices[best].out, true
}

func costFor(model string, u evalcore.Usage, override *Pricing) float64 {
	var in, out float64
	if override != nil {
		in, out = override.InputPer1M, override.OutputPer1M
	} else if i, o, ok := lookupPrice(model); ok {
		in, out = i, o
	}
	return (float64(u.InputTokens)*in + float64(u.OutputTokens)*out) / 1e6
}
