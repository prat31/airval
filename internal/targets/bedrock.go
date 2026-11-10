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

// BedrockConfig configures AWS Bedrock via the Converse API (SigV4-signed).
// Streaming is not used, so TTFT is not recorded for this kind.
type BedrockConfig struct {
	ChatCommon
	Region          string `json:"region"`
	Endpoint        string `json:"endpoint,omitempty"` // default https://bedrock-runtime.{region}.amazonaws.com
	AccessKeyID     string `json:"access_key_id"`      // template, e.g. {{ secret "AWS_ACCESS_KEY_ID" }}
	SecretAccessKey string `json:"secret_access_key"`
	SessionToken    string `json:"session_token,omitempty"`
	// ToolConfig is the Converse toolConfig object ({"tools":[{"toolSpec":...}]}).
	ToolConfig json.RawMessage `json:"tool_config,omitempty"`
	// AdditionalModelRequestFields is passed through (model-specific params).
	AdditionalModelRequestFields json.RawMessage `json:"additional_model_request_fields,omitempty"`
	ExtraBody                    map[string]any  `json:"extra_body,omitempty"`
}

func (c *BedrockConfig) Validate() error {
	if err := c.ChatCommon.validate(); err != nil {
		return err
	}
	if c.Region == "" {
		return fmt.Errorf("region is required")
	}
	if c.AccessKeyID == "" || c.SecretAccessKey == "" {
		return fmt.Errorf("access_key_id and secret_access_key are required")
	}
	if err := checkJSON("tool_config", c.ToolConfig, '{'); err != nil {
		return err
	}
	if err := checkJSON("additional_model_request_fields", c.AdditionalModelRequestFields, '{'); err != nil {
		return err
	}
	_, err := newBedrockProvider(c, Deps{})
	return err
}

type bedrockProvider struct {
	cfg              *BedrockConfig
	tr               *transport
	endpoint         *Template
	akid, sak, token *Template
	deps             Deps
}

func newBedrockProvider(cfg *BedrockConfig, deps Deps) (*bedrockProvider, error) {
	tr, err := newTransport(deps, cfg.HTTPCommon)
	if err != nil {
		return nil, err
	}
	p := &bedrockProvider{cfg: cfg, tr: tr, deps: deps}
	ep := cfg.Endpoint
	if ep == "" {
		ep = "https://bedrock-runtime." + cfg.Region + ".amazonaws.com"
	}
	if p.endpoint, err = CompileTemplate("endpoint", ep); err != nil {
		return nil, err
	}
	if p.akid, err = CompileTemplate("access_key_id", cfg.AccessKeyID); err != nil {
		return nil, err
	}
	if p.sak, err = CompileTemplate("secret_access_key", cfg.SecretAccessKey); err != nil {
		return nil, err
	}
	if p.token, err = CompileTemplate("session_token", cfg.SessionToken); err != nil {
		return nil, err
	}
	return p, nil
}

func (p *bedrockProvider) jsonConfigured() bool { return false }

func (p *bedrockProvider) chat(ctx context.Context, env *renderEnv, cp chatParams) (chatResult, error) {
	var res chatResult
	cfg := p.cfg
	root := map[string]any{envKey: env}

	msgs := make([]map[string]any, 0, len(cp.Messages))
	for _, m := range cp.Messages {
		role := m.Role
		if role != "assistant" {
			role = "user"
		}
		text := m.Content
		if text == "" {
			text = "(empty)"
		}
		msgs = append(msgs, map[string]any{"role": role, "content": []map[string]any{{"text": text}}})
	}
	body := map[string]any{"messages": msgs}
	system := cp.System
	if cp.JSONMode {
		if system != "" {
			system += "\n\n"
		}
		system += anJSONHint
	}
	if system != "" {
		body["system"] = []map[string]any{{"text": system}}
	}
	inf := map[string]any{}
	if cp.MaxTokens > 0 {
		inf["maxTokens"] = cp.MaxTokens
	}
	if cp.Temperature != nil {
		inf["temperature"] = *cp.Temperature
	}
	if len(inf) > 0 {
		body["inferenceConfig"] = inf
	}
	if !cp.NoTools && len(cfg.ToolConfig) > 0 {
		body["toolConfig"] = cfg.ToolConfig
	}
	if len(cfg.AdditionalModelRequestFields) > 0 {
		body["additionalModelRequestFields"] = cfg.AdditionalModelRequestFields
	}
	for k, v := range cfg.ExtraBody {
		body[k] = v
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return res, permanent("encode request: %v", err)
	}

	ep, err := p.endpoint.Render(root)
	if err != nil {
		return res, permanent("%v", err)
	}
	akid, err := p.akid.Render(root)
	if err != nil {
		return res, permanent("%v", err)
	}
	sak, err := p.sak.Render(root)
	if err != nil {
		return res, permanent("%v", err)
	}
	token, err := p.token.Render(root)
	if err != nil {
		return res, permanent("%v", err)
	}
	if akid == "" || sak == "" {
		return res, permanent("empty AWS credentials")
	}
	wirePath := "/model/" + awsURIEscape(cp.Model, false) + "/converse"
	u := strings.TrimRight(strings.TrimSpace(ep), "/") + wirePath
	pu, err := url.Parse(u)
	if err != nil || pu.Host == "" {
		return res, permanent("invalid endpoint %q", ep)
	}

	signHdr := map[string]string{
		"content-type": "application/json",
		"host":         pu.Host,
	}
	if token != "" {
		signHdr["x-amz-security-token"] = token
	}
	// The endpoint may carry a path prefix (proxies); sign the full wire path.
	auth, amzDate := signV4("POST", pu.EscapedPath(), "", signHdr, payload,
		awsCreds{akid, sak, token}, cfg.Region, "bedrock", p.deps.now())
	hdr := make(http.Header, 6)
	hdr["Content-Type"] = []string{"application/json"}
	hdr["X-Amz-Date"] = []string{amzDate}
	hdr["Authorization"] = []string{auth}
	if token != "" {
		hdr["X-Amz-Security-Token"] = []string{token}
	}
	res.Request = recordRequest("POST", u, hdr, payload, env.redactor())

	wresp, err := p.tr.do(ctx, env, wireReq{method: "POST", url: u, header: hdr, body: payload}, nil)
	res.Latency = wresp.latency
	if err != nil {
		return res, err
	}
	res.Raw = rawJSON(wresp.body)
	var r struct {
		Output struct {
			Message struct {
				Content []struct {
					Text    *string `json:"text"`
					ToolUse *struct {
						ToolUseID string          `json:"toolUseId"`
						Name      string          `json:"name"`
						Input     json.RawMessage `json:"input"`
					} `json:"toolUse"`
				} `json:"content"`
			} `json:"message"`
		} `json:"output"`
		Usage struct {
			InputTokens  int `json:"inputTokens"`
			OutputTokens int `json:"outputTokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(wresp.body, &r); err != nil {
		return res, permanent("invalid JSON response: %v", err)
	}
	var sb strings.Builder
	for _, c := range r.Output.Message.Content {
		if c.Text != nil {
			sb.WriteString(*c.Text)
		}
		if c.ToolUse != nil {
			res.ToolCalls = append(res.ToolCalls, evalcore.ToolCall{ID: c.ToolUse.ToolUseID, Name: c.ToolUse.Name, Arguments: c.ToolUse.Input})
		}
	}
	res.Text = sb.String()
	res.Usage = evalcore.Usage{InputTokens: r.Usage.InputTokens, OutputTokens: r.Usage.OutputTokens}
	res.CostUSD = costFor(cp.Model, res.Usage, cfg.Pricing)
	return res, nil
}
