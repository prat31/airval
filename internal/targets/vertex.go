package targets

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/airval/airval/internal/evalcore"
)

// VertexConfig configures Google Gemini via generateContent. With project_id
// it targets Vertex AI (bearer access_token); without it, the Gemini API on
// generativelanguage.googleapis.com (api_key). Streaming is not used.
type VertexConfig struct {
	ChatCommon
	BaseURL     string `json:"base_url,omitempty"`
	ProjectID   string `json:"project_id,omitempty"`
	Location    string `json:"location,omitempty"`     // default us-central1 (Vertex AI only)
	APIKey      string `json:"api_key,omitempty"`      // template; sent as x-goog-api-key
	AccessToken string `json:"access_token,omitempty"` // template; OAuth bearer token
	// Tools is the Gemini tools array ([{"functionDeclarations":[...]}]).
	Tools     json.RawMessage `json:"tools,omitempty"`
	ExtraBody map[string]any  `json:"extra_body,omitempty"`
}

func (c *VertexConfig) Validate() error {
	if err := c.ChatCommon.validate(); err != nil {
		return err
	}
	if c.APIKey == "" && c.AccessToken == "" {
		return fmt.Errorf("one of api_key or access_token is required")
	}
	if err := checkJSON("tools", c.Tools, '['); err != nil {
		return err
	}
	_, err := newVertexProvider(c, Deps{})
	return err
}

type vertexProvider struct {
	cfg        *VertexConfig
	tr         *transport
	base       *Template
	key, token *Template
}

func newVertexProvider(cfg *VertexConfig, deps Deps) (*vertexProvider, error) {
	tr, err := newTransport(deps, cfg.HTTPCommon)
	if err != nil {
		return nil, err
	}
	p := &vertexProvider{cfg: cfg, tr: tr}
	base := cfg.BaseURL
	if base == "" {
		switch {
		case cfg.ProjectID == "":
			base = "https://generativelanguage.googleapis.com"
		case cfg.Location == "global":
			base = "https://aiplatform.googleapis.com"
		default:
			loc := cfg.Location
			if loc == "" {
				loc = "us-central1"
			}
			base = "https://" + loc + "-aiplatform.googleapis.com"
		}
	}
	if p.base, err = CompileTemplate("base_url", base); err != nil {
		return nil, err
	}
	if p.key, err = CompileTemplate("api_key", cfg.APIKey); err != nil {
		return nil, err
	}
	if p.token, err = CompileTemplate("access_token", cfg.AccessToken); err != nil {
		return nil, err
	}
	return p, nil
}

func (p *vertexProvider) jsonConfigured() bool { return false }

func (p *vertexProvider) chat(ctx context.Context, env *renderEnv, cp chatParams) (chatResult, error) {
	var res chatResult
	cfg := p.cfg
	root := map[string]any{envKey: env}

	contents := make([]map[string]any, 0, len(cp.Messages))
	for _, m := range cp.Messages {
		role := "user"
		if m.Role == "assistant" {
			role = "model"
		}
		contents = append(contents, map[string]any{"role": role, "parts": []map[string]any{{"text": m.Content}}})
	}
	body := map[string]any{"contents": contents}
	if cp.System != "" {
		body["systemInstruction"] = map[string]any{"parts": []map[string]any{{"text": cp.System}}}
	}
	gen := map[string]any{}
	if cp.Temperature != nil {
		gen["temperature"] = *cp.Temperature
	}
	if cp.MaxTokens > 0 {
		gen["maxOutputTokens"] = cp.MaxTokens
	}
	if cp.JSONMode {
		gen["responseMimeType"] = "application/json"
	}
	if len(gen) > 0 {
		body["generationConfig"] = gen
	}
	if !cp.NoTools && len(cfg.Tools) > 0 {
		body["tools"] = cfg.Tools
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
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	model := url.PathEscape(cp.Model)
	var u string
	if cfg.ProjectID != "" {
		loc := cfg.Location
		if loc == "" {
			loc = "us-central1"
		}
		u = fmt.Sprintf("%s/v1/projects/%s/locations/%s/publishers/google/models/%s:generateContent",
			base, url.PathEscape(cfg.ProjectID), url.PathEscape(loc), model)
	} else {
		u = fmt.Sprintf("%s/v1beta/models/%s:generateContent", base, model)
	}
	hdr := make(http.Header, 4)
	hdr["Content-Type"] = []string{"application/json"}
	if key, err := p.key.Render(root); err != nil {
		return res, permanent("%v", err)
	} else if key != "" {
		hdr["X-Goog-Api-Key"] = []string{strings.TrimSpace(key)}
	}
	if tok, err := p.token.Render(root); err != nil {
		return res, permanent("%v", err)
	} else if tok != "" {
		hdr["Authorization"] = []string{"Bearer " + strings.TrimSpace(tok)}
	}
	res.Request = recordRequest("POST", u, hdr, payload, env.redactor())

	wresp, err := p.tr.do(ctx, env, wireReq{method: "POST", url: u, header: hdr, body: payload}, nil)
	res.Latency = wresp.latency
	if err != nil {
		return res, err
	}
	res.Raw = rawJSON(wresp.body)
	var r struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text         *string `json:"text"`
					FunctionCall *struct {
						Name string          `json:"name"`
						Args json.RawMessage `json:"args"`
					} `json:"functionCall"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
		PromptFeedback struct {
			BlockReason string `json:"blockReason"`
		} `json:"promptFeedback"`
		UsageMetadata struct {
			PromptTokenCount     int `json:"promptTokenCount"`
			CandidatesTokenCount int `json:"candidatesTokenCount"`
		} `json:"usageMetadata"`
	}
	if err := json.Unmarshal(wresp.body, &r); err != nil {
		return res, permanent("invalid JSON response: %v", err)
	}
	if len(r.Candidates) == 0 {
		if r.PromptFeedback.BlockReason != "" {
			return res, permanent("prompt blocked: %s", r.PromptFeedback.BlockReason)
		}
		return res, permanent("response has no candidates")
	}
	var sb strings.Builder
	for _, part := range r.Candidates[0].Content.Parts {
		if part.Text != nil {
			sb.WriteString(*part.Text)
		}
		if part.FunctionCall != nil {
			res.ToolCalls = append(res.ToolCalls, evalcore.ToolCall{Name: part.FunctionCall.Name, Arguments: part.FunctionCall.Args})
		}
	}
	res.Text = sb.String()
	res.Usage = evalcore.Usage{InputTokens: r.UsageMetadata.PromptTokenCount, OutputTokens: r.UsageMetadata.CandidatesTokenCount}
	res.CostUSD = costFor(cp.Model, res.Usage, cfg.Pricing)
	return res, nil
}
