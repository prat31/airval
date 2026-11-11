package targets

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/prat31/airval/internal/evalcore"
)

// OpenAIConfig configures any OpenAI-compatible chat completions API (OpenAI,
// Azure OpenAI, Ollama, vLLM, LM Studio, Together, Groq...).
type OpenAIConfig struct {
	ChatCommon
	BaseURL        string            `json:"base_url,omitempty"`         // default https://api.openai.com/v1
	Path           string            `json:"path,omitempty"`             // default /chat/completions
	APIKey         string            `json:"api_key,omitempty"`          // template, e.g. {{ secret "OPENAI_KEY" }}
	AuthHeader     string            `json:"auth_header,omitempty"`      // default Authorization (Bearer); use api-key for Azure
	Headers        map[string]string `json:"headers,omitempty"`          // extra headers (templates)
	Query          map[string]string `json:"query,omitempty"`            // extra query params, e.g. api-version for Azure
	MaxTokensParam string            `json:"max_tokens_param,omitempty"` // default max_tokens; max_completion_tokens for o-series
	Tools          json.RawMessage   `json:"tools,omitempty"`            // OpenAI tools array
	ToolChoice     json.RawMessage   `json:"tool_choice,omitempty"`
	ResponseFormat json.RawMessage   `json:"response_format,omitempty"` // e.g. {"type":"json_object"}
	ExtraBody      map[string]any    `json:"extra_body,omitempty"`      // merged into the request body last
	Stream         bool              `json:"stream,omitempty"`          // stream to record TTFT
}

func (c *OpenAIConfig) Validate() error {
	if err := c.ChatCommon.validate(); err != nil {
		return err
	}
	if err := checkJSON("tools", c.Tools, '['); err != nil {
		return err
	}
	if err := checkJSON("response_format", c.ResponseFormat, '{'); err != nil {
		return err
	}
	if c.BaseURL != "" && !strings.Contains(c.BaseURL, "{{") {
		if u, err := url.Parse(c.BaseURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return fmt.Errorf("base_url must be an absolute http(s) URL")
		}
	}
	_, err := newOpenAIProvider(c, Deps{})
	return err
}

func checkJSON(name string, raw json.RawMessage, first byte) error {
	if len(raw) == 0 {
		return nil
	}
	t := bytes.TrimSpace(raw)
	if !json.Valid(t) || t[0] != first {
		return fmt.Errorf("%s must be a JSON %s", name, map[byte]string{'[': "array", '{': "object"}[first])
	}
	return nil
}

type headerTpls []hdrTpl

func compileHeaders(m map[string]string) (headerTpls, error) {
	var out headerTpls
	for k, v := range m {
		t, err := CompileTemplate("header "+k, v)
		if err != nil {
			return nil, err
		}
		out = append(out, hdrTpl{http.CanonicalHeaderKey(k), t})
	}
	return out, nil
}

func (h headerTpls) apply(dst http.Header, root map[string]any) error {
	for _, ht := range h {
		v, err := ht.tpl.Render(root)
		if err != nil {
			return permanent("%v", err)
		}
		dst[ht.key] = []string{strings.TrimSpace(v)}
	}
	return nil
}

type openAIProvider struct {
	cfg     *OpenAIConfig
	tr      *transport
	base    *Template
	apiKey  *Template
	headers headerTpls
	path    string
	authHdr string
	query   string
}

func newOpenAIProvider(cfg *OpenAIConfig, deps Deps) (*openAIProvider, error) {
	tr, err := newTransport(deps, cfg.HTTPCommon)
	if err != nil {
		return nil, err
	}
	p := &openAIProvider{cfg: cfg, tr: tr, path: cfg.Path, authHdr: http.CanonicalHeaderKey(cfg.AuthHeader)}
	base := cfg.BaseURL
	if base == "" {
		base = "https://api.openai.com/v1"
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
	if p.path == "" {
		p.path = "/chat/completions"
	}
	if p.authHdr == "" {
		p.authHdr = "Authorization"
	}
	if len(cfg.Query) > 0 {
		q := url.Values{}
		for k, v := range cfg.Query {
			q.Set(k, v)
		}
		p.query = q.Encode()
	}
	return p, nil
}

func (p *openAIProvider) jsonConfigured() bool { return len(p.cfg.ResponseFormat) > 0 }

type oaToolCall struct {
	Index    int    `json:"index"`
	ID       string `json:"id"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type oaUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}

type oaLogprobs struct {
	Content []struct {
		TopLogprobs []struct {
			Token   string  `json:"token"`
			Logprob float64 `json:"logprob"`
		} `json:"top_logprobs"`
	} `json:"content"`
}

type oaErrBody struct {
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

func (p *openAIProvider) chat(ctx context.Context, env *renderEnv, cp chatParams) (chatResult, error) {
	var res chatResult
	cfg := p.cfg
	root := map[string]any{envKey: env}

	msgs := make([]map[string]any, 0, len(cp.Messages)+1)
	if cp.System != "" {
		msgs = append(msgs, map[string]any{"role": "system", "content": cp.System})
	}
	for _, m := range cp.Messages {
		msgs = append(msgs, map[string]any{"role": m.Role, "content": m.Content})
	}
	body := map[string]any{"model": cp.Model, "messages": msgs}
	if cp.Temperature != nil {
		body["temperature"] = *cp.Temperature
	}
	if cp.MaxTokens > 0 {
		k := cfg.MaxTokensParam
		if k == "" {
			k = "max_tokens"
		}
		body[k] = cp.MaxTokens
	}
	if !cp.NoTools {
		if len(cfg.Tools) > 0 {
			body["tools"] = cfg.Tools
			if len(cfg.ToolChoice) > 0 {
				body["tool_choice"] = cfg.ToolChoice
			}
		}
	}
	switch {
	case len(cfg.ResponseFormat) > 0 && !cp.NoTools:
		body["response_format"] = cfg.ResponseFormat
	case cp.JSONMode:
		body["response_format"] = map[string]any{"type": "json_object"}
	}
	if cp.Logprobs {
		body["logprobs"] = true
		body["top_logprobs"] = 20
	}
	stream := cfg.Stream && !cp.Logprobs
	if stream {
		body["stream"] = true
		body["stream_options"] = map[string]any{"include_usage": true}
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
	u := strings.TrimRight(strings.TrimSpace(base), "/") + p.path
	if p.query != "" {
		u += "?" + p.query
	}
	hdr := make(http.Header, 6)
	hdr["Content-Type"] = []string{"application/json"}
	if stream {
		hdr["Accept"] = []string{"text/event-stream"}
		hdr["Accept-Encoding"] = []string{"identity"}
	}
	if key, err := p.apiKey.Render(root); err != nil {
		return res, permanent("%v", err)
	} else if key != "" {
		if p.authHdr == "Authorization" {
			hdr["Authorization"] = []string{"Bearer " + strings.TrimSpace(key)}
		} else {
			hdr[p.authHdr] = []string{strings.TrimSpace(key)}
		}
	}
	if err := p.headers.apply(hdr, root); err != nil {
		return res, err
	}
	redact := env.redactor()
	res.Request = recordRequest("POST", u, hdr, payload, redact)
	wr := wireReq{method: "POST", url: u, header: hdr, body: payload}

	if !stream {
		wresp, err := p.tr.do(ctx, env, wr, nil)
		res.Latency = wresp.latency
		if err != nil {
			return res, err
		}
		res.Raw = rawJSON(wresp.body)
		var r struct {
			Choices []struct {
				Message struct {
					Content   string       `json:"content"`
					ToolCalls []oaToolCall `json:"tool_calls"`
				} `json:"message"`
				Logprobs *oaLogprobs `json:"logprobs"`
			} `json:"choices"`
			Usage *oaUsage `json:"usage"`
			oaErrBody
		}
		if err := json.Unmarshal(wresp.body, &r); err != nil {
			return res, permanent("invalid JSON response: %v", err)
		}
		if r.Error != nil {
			return res, permanent("api error: %s", r.Error.Message)
		}
		if len(r.Choices) == 0 {
			return res, permanent("response has no choices")
		}
		ch := r.Choices[0]
		res.Text = ch.Message.Content
		res.ToolCalls = oaCalls(ch.Message.ToolCalls)
		if r.Usage != nil {
			res.Usage = evalcore.Usage{InputTokens: r.Usage.PromptTokens, OutputTokens: r.Usage.CompletionTokens}
		}
		if ch.Logprobs != nil && len(ch.Logprobs.Content) > 0 {
			top := ch.Logprobs.Content[0].TopLogprobs
			res.TopLogprobs = make(map[string]float64, len(top))
			for _, t := range top {
				res.TopLogprobs[t.Token] = t.Logprob
			}
		}
		res.CostUSD = costFor(cp.Model, res.Usage, cfg.Pricing)
		return res, nil
	}

	// Streaming.
	var text strings.Builder
	var calls []oaToolCall
	var capture cappedBuf
	sfn := func(body io.Reader, first func()) error {
		return readSSE(io.TeeReader(body, &capture), func(_ string, data []byte) (bool, error) {
			if string(bytes.TrimSpace(data)) == "[DONE]" {
				return true, nil
			}
			var c struct {
				Choices []struct {
					Delta struct {
						Content   string       `json:"content"`
						ToolCalls []oaToolCall `json:"tool_calls"`
					} `json:"delta"`
				} `json:"choices"`
				Usage *oaUsage `json:"usage"`
				oaErrBody
			}
			if err := json.Unmarshal(data, &c); err != nil {
				return false, nil
			}
			if c.Error != nil {
				return true, ErrTransient{Err: fmt.Errorf("stream error: %s", c.Error.Message)}
			}
			if c.Usage != nil {
				res.Usage = evalcore.Usage{InputTokens: c.Usage.PromptTokens, OutputTokens: c.Usage.CompletionTokens}
			}
			for _, ch := range c.Choices {
				if ch.Delta.Content != "" {
					first()
					text.WriteString(ch.Delta.Content)
				}
				for _, tcd := range ch.Delta.ToolCalls {
					first()
					for len(calls) <= tcd.Index {
						calls = append(calls, oaToolCall{})
					}
					cur := &calls[tcd.Index]
					if tcd.ID != "" {
						cur.ID = tcd.ID
					}
					if tcd.Function.Name != "" {
						cur.Function.Name = tcd.Function.Name
					}
					cur.Function.Arguments += tcd.Function.Arguments
				}
			}
			return false, nil
		})
	}
	wresp, err := p.tr.do(ctx, env, wr, sfn)
	res.Latency, res.TTFT = wresp.latency, wresp.ttft
	if err != nil {
		return res, err
	}
	res.Text = text.String()
	res.ToolCalls = oaCalls(calls)
	rb, _ := json.Marshal(string(capture.b))
	res.Raw = rb
	res.CostUSD = costFor(cp.Model, res.Usage, cfg.Pricing)
	return res, nil
}

func oaCalls(in []oaToolCall) []evalcore.ToolCall {
	if len(in) == 0 {
		return nil
	}
	out := make([]evalcore.ToolCall, len(in))
	for i, c := range in {
		out[i] = evalcore.ToolCall{ID: c.ID, Name: c.Function.Name, Arguments: argsRaw(c.Function.Arguments)}
	}
	return out
}
