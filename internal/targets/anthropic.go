package targets

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/prat31/airval/internal/evalcore"
)

// AnthropicConfig configures the Anthropic Messages API.
type AnthropicConfig struct {
	ChatCommon
	BaseURL          string            `json:"base_url,omitempty"`          // default https://api.anthropic.com
	APIKey           string            `json:"api_key,omitempty"`           // template, e.g. {{ secret "ANTHROPIC_KEY" }}
	AnthropicVersion string            `json:"anthropic_version,omitempty"` // default 2023-06-01
	AnthropicBeta    string            `json:"anthropic_beta,omitempty"`
	Headers          map[string]string `json:"headers,omitempty"`
	Tools            json.RawMessage   `json:"tools,omitempty"` // Anthropic tools array
	ToolChoice       json.RawMessage   `json:"tool_choice,omitempty"`
	ExtraBody        map[string]any    `json:"extra_body,omitempty"`
	Stream           bool              `json:"stream,omitempty"` // stream to record TTFT
}

func (c *AnthropicConfig) Validate() error {
	if err := c.ChatCommon.validate(); err != nil {
		return err
	}
	if err := checkJSON("tools", c.Tools, '['); err != nil {
		return err
	}
	_, err := newAnthropicProvider(c, Deps{})
	return err
}

type anthropicProvider struct {
	cfg     *AnthropicConfig
	tr      *transport
	base    *Template
	apiKey  *Template
	headers headerTpls
}

func newAnthropicProvider(cfg *AnthropicConfig, deps Deps) (*anthropicProvider, error) {
	tr, err := newTransport(deps, cfg.HTTPCommon)
	if err != nil {
		return nil, err
	}
	p := &anthropicProvider{cfg: cfg, tr: tr}
	base := cfg.BaseURL
	if base == "" {
		base = "https://api.anthropic.com"
	}
	if p.base, err = CompileTemplate("base_url", base); err != nil {
		return nil, err
	}
	if p.apiKey, err = CompileTemplate("api_key", cfg.APIKey); err != nil {
		return nil, err
	}
	if p.headers, err = compileHeaders(cfg.Headers); err != nil {
		return nil, err
	}
	return p, nil
}

func (p *anthropicProvider) jsonConfigured() bool { return false }

type anUsage struct {
	InputTokens         int `json:"input_tokens"`
	OutputTokens        int `json:"output_tokens"`
	CacheCreationTokens int `json:"cache_creation_input_tokens"`
	CacheReadTokens     int `json:"cache_read_input_tokens"`
}

func (u anUsage) in() int { return u.InputTokens + u.CacheCreationTokens + u.CacheReadTokens }

type anBlock struct {
	Type  string          `json:"type"`
	Text  string          `json:"text"`
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

const anJSONHint = "Respond with a single valid JSON value and nothing else (no prose, no code fences)."

func (p *anthropicProvider) chat(ctx context.Context, env *renderEnv, cp chatParams) (chatResult, error) {
	var res chatResult
	cfg := p.cfg
	root := map[string]any{envKey: env}

	msgs := make([]map[string]any, 0, len(cp.Messages))
	for _, m := range cp.Messages {
		role := m.Role
		if role != "assistant" {
			role = "user"
		}
		content := m.Content
		if content == "" {
			content = "(empty)"
		}
		msgs = append(msgs, map[string]any{"role": role, "content": content})
	}
	maxTokens := cp.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 1024
	}
	body := map[string]any{"model": cp.Model, "max_tokens": maxTokens, "messages": msgs}
	system := cp.System
	if cp.JSONMode {
		if system != "" {
			system += "\n\n"
		}
		system += anJSONHint
	}
	if system != "" {
		body["system"] = system
	}
	if cp.Temperature != nil {
		body["temperature"] = *cp.Temperature
	}
	if !cp.NoTools && len(cfg.Tools) > 0 {
		body["tools"] = cfg.Tools
		if len(cfg.ToolChoice) > 0 {
			body["tool_choice"] = cfg.ToolChoice
		}
	}
	stream := cfg.Stream
	if stream {
		body["stream"] = true
	}
	for k, v := range cfg.ExtraBody {
		body[k] = v
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return res, permanent("encode request: %v", err)
	}

	base, err := p.base.Render(root)
	if err != nil {
		return res, permanent("%v", err)
	}
	u := strings.TrimRight(strings.TrimSpace(base), "/") + "/v1/messages"
	hdr := make(http.Header, 6)
	hdr["Content-Type"] = []string{"application/json"}
	ver := cfg.AnthropicVersion
	if ver == "" {
		ver = "2023-06-01"
	}
	hdr["Anthropic-Version"] = []string{ver}
	if cfg.AnthropicBeta != "" {
		hdr["Anthropic-Beta"] = []string{cfg.AnthropicBeta}
	}
	if stream {
		hdr["Accept-Encoding"] = []string{"identity"}
	}
	if key, err := p.apiKey.Render(root); err != nil {
		return res, permanent("%v", err)
	} else if key != "" {
		hdr["X-Api-Key"] = []string{strings.TrimSpace(key)}
	}
	if err := p.headers.apply(hdr, root); err != nil {
		return res, err
	}
	res.Request = recordRequest("POST", u, hdr, payload, env.redactor())
	wr := wireReq{method: "POST", url: u, header: hdr, body: payload}

	collect := func(blocks []anBlock) {
		var sb strings.Builder
		for _, b := range blocks {
			switch b.Type {
			case "text":
				sb.WriteString(b.Text)
			case "tool_use":
				res.ToolCalls = append(res.ToolCalls, evalcore.ToolCall{ID: b.ID, Name: b.Name, Arguments: b.Input})
			}
		}
		res.Text = sb.String()
	}

	if !stream {
		wresp, err := p.tr.do(ctx, env, wr, nil)
		res.Latency = wresp.latency
		if err != nil {
			return res, err
		}
		res.Raw = rawJSON(wresp.body)
		var r struct {
			Content []anBlock `json:"content"`
			Usage   anUsage   `json:"usage"`
		}
		if err := json.Unmarshal(wresp.body, &r); err != nil {
			return res, permanent("invalid JSON response: %v", err)
		}
		collect(r.Content)
		res.Usage = evalcore.Usage{InputTokens: r.Usage.in(), OutputTokens: r.Usage.OutputTokens}
		res.CostUSD = costFor(cp.Model, res.Usage, cfg.Pricing)
		return res, nil
	}

	type sblock struct {
		anBlock
		args strings.Builder
	}
	var blocks []*sblock
	var capture cappedBuf
	var inTok, outTok int
	sfn := func(body io.Reader, first func()) error {
		return readSSE(io.TeeReader(body, &capture), func(_ string, data []byte) (bool, error) {
			var ev struct {
				Type    string `json:"type"`
				Index   int    `json:"index"`
				Message struct {
					Usage anUsage `json:"usage"`
				} `json:"message"`
				ContentBlock anBlock `json:"content_block"`
				Delta        struct {
					Type        string `json:"type"`
					Text        string `json:"text"`
					PartialJSON string `json:"partial_json"`
				} `json:"delta"`
				Usage *anUsage `json:"usage"`
				Error *struct {
					Type    string `json:"type"`
					Message string `json:"message"`
				} `json:"error"`
			}
			if err := json.Unmarshal(data, &ev); err != nil {
				return false, nil
			}
			switch ev.Type {
			case "message_start":
				inTok = ev.Message.Usage.in()
				outTok = ev.Message.Usage.OutputTokens
			case "content_block_start":
				for len(blocks) <= ev.Index {
					blocks = append(blocks, &sblock{})
				}
				blocks[ev.Index].anBlock = ev.ContentBlock
				if ev.ContentBlock.Type == "tool_use" {
					first()
				}
			case "content_block_delta":
				for len(blocks) <= ev.Index {
					blocks = append(blocks, &sblock{})
				}
				b := blocks[ev.Index]
				if ev.Delta.Text != "" {
					first()
					b.Text += ev.Delta.Text
				}
				if ev.Delta.PartialJSON != "" {
					b.args.WriteString(ev.Delta.PartialJSON)
				}
			case "message_delta":
				if ev.Usage != nil && ev.Usage.OutputTokens > 0 {
					outTok = ev.Usage.OutputTokens
				}
			case "error":
				msg := "stream error"
				if ev.Error != nil {
					msg = ev.Error.Type + ": " + ev.Error.Message
					if ev.Error.Type == "invalid_request_error" {
						return true, ErrPermanent{Err: fmt.Errorf("%s", msg)}
					}
				}
				return true, ErrTransient{Err: fmt.Errorf("%s", msg)}
			case "message_stop":
				return true, nil
			}
			return false, nil
		})
	}
	wresp, err := p.tr.do(ctx, env, wr, sfn)
	res.Latency, res.TTFT = wresp.latency, wresp.ttft
	if err != nil {
		return res, err
	}
	plain := make([]anBlock, len(blocks))
	for i, b := range blocks {
		plain[i] = b.anBlock
		if b.Type == "tool_use" {
			if a := strings.TrimSpace(b.args.String()); a != "" {
				plain[i].Input = json.RawMessage(a)
				if !json.Valid(plain[i].Input) {
					plain[i].Input = toRaw(a)
				}
			}
		}
	}
	collect(plain)
	res.Usage = evalcore.Usage{InputTokens: inTok, OutputTokens: outTok}
	res.CostUSD = costFor(cp.Model, res.Usage, cfg.Pricing)
	rb, _ := json.Marshal(string(capture.b))
	res.Raw = rb
	return res, nil
}
