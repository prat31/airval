package targets

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/airval/airval/internal/evalcore"
	"github.com/airval/airval/tools/mocktarget/mock"
)

func TestOpenAINonStream(t *testing.T) {
	srv := newMock(t, mockCfg())
	a := mustNew(t, "openai", OpenAIConfig{
		ChatCommon: ChatCommon{Model: "gpt-4o-mini", SystemPrompt: "You answer for {{ .example.metadata.team }}.", UserTemplate: "Q: {{ .input.question }}", MaxTokens: 100},
		BaseURL:    srv.URL + "/v1", APIKey: `{{ secret "OPENAI" }}`,
	})
	e := ex("what?")
	e.Metadata = map[string]any{"team": "blue"}
	r := invoke(t, a, e, InvokeOptions{Secrets: mapSecrets{"OPENAI": "sk-live-abc"}})
	if r.Output != mock.Truth("Q: what?") {
		t.Fatalf("output %q", r.Output)
	}
	if r.Usage.InputTokens == 0 || r.Usage.OutputTokens == 0 {
		t.Fatalf("usage %+v", r.Usage)
	}
	wantCost := (float64(r.Usage.InputTokens)*0.15 + float64(r.Usage.OutputTokens)*0.60) / 1e6
	if r.CostUSD != wantCost || r.CostUSD == 0 {
		t.Fatalf("cost %v want %v", r.CostUSD, wantCost)
	}
	req := string(r.Request)
	if strings.Contains(req, "sk-live-abc") || !strings.Contains(req, "You answer for blue.") || !strings.Contains(req, `"max_tokens":100`) {
		t.Fatalf("request record: %s", req)
	}
}

func TestOpenAIPricingOverrideAndUnknownModel(t *testing.T) {
	srv := newMock(t, mockCfg())
	a := mustNew(t, "openai", OpenAIConfig{ChatCommon: ChatCommon{Model: "my-local-model", Pricing: &Pricing{InputPer1M: 1e6, OutputPer1M: 2e6}}, BaseURL: srv.URL + "/v1"})
	r := invoke(t, a, ex("a b c"), InvokeOptions{})
	if want := float64(r.Usage.InputTokens) + 2*float64(r.Usage.OutputTokens); r.CostUSD != want {
		t.Fatalf("cost %v want %v", r.CostUSD, want)
	}
	b := mustNew(t, "openai", OpenAIConfig{ChatCommon: ChatCommon{Model: "my-local-model"}, BaseURL: srv.URL + "/v1"})
	if r := invoke(t, b, ex("a"), InvokeOptions{}); r.CostUSD != 0 {
		t.Fatalf("unknown model cost %v", r.CostUSD)
	}
}

func TestOpenAIStreamAndTools(t *testing.T) {
	srv := newMock(t, mock.Config{Accuracy: 1, ChunkDelay: 20 * time.Millisecond})
	a := mustNew(t, "openai", OpenAIConfig{
		ChatCommon: ChatCommon{Model: "gpt-4o"}, BaseURL: srv.URL + "/v1", Stream: true,
	})
	r := invoke(t, a, ex("tell me"), InvokeOptions{})
	if r.Output != mock.Truth("tell me") {
		t.Fatalf("output %q", r.Output)
	}
	if r.TTFT <= 0 || r.Latency <= r.TTFT || r.Usage.OutputTokens == 0 {
		t.Fatalf("ttft %v latency %v usage %+v", r.TTFT, r.Latency, r.Usage)
	}

	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprintf("tools/stream=%v", stream), func(t *testing.T) {
			a := mustNew(t, "openai", OpenAIConfig{
				ChatCommon: ChatCommon{Model: "gpt-4o"}, BaseURL: srv.URL + "/v1", Stream: stream,
				Tools: json.RawMessage(`[{"type":"function","function":{"name":"lookup","parameters":{"type":"object"}}}]`),
			})
			r := invoke(t, a, ex("find x"), InvokeOptions{})
			if len(r.ToolCalls) != 1 || r.ToolCalls[0].Name != "lookup" || string(r.ToolCalls[0].Arguments) != `{"query":"find x"}` || r.ToolCalls[0].ID == "" {
				t.Fatalf("tool calls %+v", r.ToolCalls)
			}
		})
	}
}

func TestOpenAIErrorsClassified(t *testing.T) {
	var code int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "1.5")
		w.WriteHeader(code)
		io.WriteString(w, `{"error":{"message":"nope sk-live-abc"}}`)
	}))
	defer srv.Close()
	a := mustNew(t, "openai", OpenAIConfig{ChatCommon: ChatCommon{Model: "gpt-4o"}, BaseURL: srv.URL, APIKey: `{{ secret "K" }}`})
	for c, want := range map[int]Class{429: ClassRateLimited, 500: ClassTransient, 401: ClassPermanent} {
		code = c
		_, err := a.Invoke(context.Background(), ex("x"), InvokeOptions{Secrets: mapSecrets{"K": "sk-live-abc"}})
		if Classify(err) != want {
			t.Fatalf("%d: %v", c, err)
		}
		if strings.Contains(err.Error(), "sk-live-abc") {
			t.Fatalf("secret leaked in error: %v", err)
		}
		if c == 429 {
			if d, _ := RetryAfter(err); d != 1500*time.Millisecond {
				t.Fatalf("retry after %v", d)
			}
		}
	}
}

func TestAnthropic(t *testing.T) {
	var mu sync.Mutex
	var lastBody map[string]any
	var lastHdr http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		lastHdr = r.Header.Clone()
		lastBody = nil
		_ = json.Unmarshal(b, &lastBody)
		mu.Unlock()
		if r.URL.Path != "/v1/messages" {
			http.NotFound(w, r)
			return
		}
		if lastBody["stream"] == true {
			w.Header().Set("Content-Type", "text/event-stream")
			fl := w.(http.Flusher)
			ev := func(name, data string) {
				fmt.Fprintf(w, "event: %s\ndata: %s\n\n", name, data)
				fl.Flush()
			}
			ev("message_start", `{"type":"message_start","message":{"usage":{"input_tokens":25,"output_tokens":1}}}`)
			ev("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`)
			time.Sleep(20 * time.Millisecond)
			ev("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hel"}}`)
			time.Sleep(20 * time.Millisecond)
			ev("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"lo"}}`)
			ev("content_block_start", `{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"tu1","name":"calc","input":{}}}`)
			ev("content_block_delta", `{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"x\":"}}`)
			ev("content_block_delta", `{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"1}"}}`)
			ev("message_delta", `{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":12}}`)
			ev("message_stop", `{"type":"message_stop"}`)
			return
		}
		io.WriteString(w, `{"content":[{"type":"text","text":"Hi "},{"type":"text","text":"there"},{"type":"tool_use","id":"tu9","name":"calc","input":{"a":1}}],
			"usage":{"input_tokens":10,"output_tokens":4,"cache_read_input_tokens":6}}`)
	}))
	defer srv.Close()

	cfg := AnthropicConfig{
		ChatCommon: ChatCommon{Model: "claude-3-5-haiku-latest", SystemPrompt: "be brief", Temperature: ptr(0.2)},
		BaseURL:    srv.URL, APIKey: `{{ secret "AK" }}`,
	}
	a := mustNew(t, "anthropic", cfg)
	r := invoke(t, a, ex("hello"), InvokeOptions{Secrets: mapSecrets{"AK": "ant-key-1"}})
	if r.Output != "Hi there" || len(r.ToolCalls) != 1 || string(r.ToolCalls[0].Arguments) != `{"a":1}` {
		t.Fatalf("%+v", r)
	}
	if r.Usage.InputTokens != 16 || r.Usage.OutputTokens != 4 {
		t.Fatalf("usage %+v", r.Usage)
	}
	if want := (16*0.8 + 4*4.0) / 1e6; math.Abs(r.CostUSD-want) > 1e-12 {
		t.Fatalf("cost %v want %v", r.CostUSD, want)
	}
	mu.Lock()
	if lastHdr.Get("x-api-key") != "ant-key-1" || lastHdr.Get("anthropic-version") != "2023-06-01" {
		t.Fatalf("headers %v", lastHdr)
	}
	if lastBody["system"] != "be brief" || lastBody["max_tokens"].(float64) != 1024 || lastBody["temperature"].(float64) != 0.2 {
		t.Fatalf("body %v", lastBody)
	}
	mu.Unlock()
	if strings.Contains(string(r.Request), "ant-key-1") {
		t.Fatal("api key leaked")
	}

	cfg.Stream = true
	s := mustNew(t, "anthropic", cfg)
	r = invoke(t, s, ex("hello"), InvokeOptions{Secrets: mapSecrets{"AK": "ant-key-1"}})
	if r.Output != "Hello" || r.Usage.InputTokens != 25 || r.Usage.OutputTokens != 12 {
		t.Fatalf("stream: %+v", r)
	}
	if len(r.ToolCalls) != 1 || r.ToolCalls[0].ID != "tu1" || string(r.ToolCalls[0].Arguments) != `{"x":1}` {
		t.Fatalf("stream tool calls: %+v", r.ToolCalls)
	}
	if r.TTFT <= 0 || r.TTFT >= r.Latency {
		t.Fatalf("ttft %v latency %v", r.TTFT, r.Latency)
	}
}

func ptr[T any](v T) *T { return &v }

func TestSigV4KnownVector(t *testing.T) {
	// AWS Signature V4 test suite: get-vanilla.
	auth, date := signV4("GET", "/", "", map[string]string{"host": "example.amazonaws.com"}, nil,
		awsCreds{AccessKeyID: "AKIDEXAMPLE", SecretAccessKey: "wJalrXUtnFEMI/K7MDENG+bPxRfiCYEXAMPLEKEY"},
		"us-east-1", "service", time.Date(2015, 8, 30, 12, 36, 0, 0, time.UTC))
	want := "AWS4-HMAC-SHA256 Credential=AKIDEXAMPLE/20150830/us-east-1/service/aws4_request, SignedHeaders=host;x-amz-date, Signature=5fa00fa31553b73ebf1942676e86291e8372ff2a2260956d9b8aae1d763fbf31"
	if date != "20150830T123600Z" || auth != want {
		t.Fatalf("got %s %s", date, auth)
	}
}

func TestBedrockConverse(t *testing.T) {
	var gotPath, gotAuth, gotDate, gotTok string
	var gotBody map[string]any
	var rawBody []byte
	var gotHost string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHost = r.Host
		gotPath, gotAuth, gotDate, gotTok = r.URL.EscapedPath(), r.Header.Get("Authorization"), r.Header.Get("X-Amz-Date"), r.Header.Get("X-Amz-Security-Token")
		b, _ := io.ReadAll(r.Body)
		rawBody = b
		_ = json.Unmarshal(b, &gotBody)
		io.WriteString(w, `{"output":{"message":{"role":"assistant","content":[{"text":"bonjour"},{"toolUse":{"toolUseId":"t1","name":"fn","input":{"k":"v"}}}]}},"usage":{"inputTokens":11,"outputTokens":7}}`)
	}))
	defer srv.Close()
	fixed := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)
	raw := mustJSON(BedrockConfig{
		ChatCommon: ChatCommon{Model: "anthropic.claude-3-haiku-20240307-v1:0", SystemPrompt: "sys", MaxTokens: 50, Temperature: ptr(0.0)},
		Region:     "us-east-1", Endpoint: srv.URL,
		AccessKeyID: `{{ secret "AKID" }}`, SecretAccessKey: `{{ secret "SAK" }}`, SessionToken: `{{ secret "TOK" }}`,
	})
	a, err := New("bedrock", raw, Deps{Clients: NewClientFactory(nil), Now: func() time.Time { return fixed }})
	if err != nil {
		t.Fatal(err)
	}
	r := invoke(t, a, ex("salut"), InvokeOptions{Secrets: mapSecrets{"AKID": "AKIDTEST", "SAK": "sekret", "TOK": "session-token-xyz"}})
	if r.Output != "bonjour" || len(r.ToolCalls) != 1 || r.ToolCalls[0].Name != "fn" || r.Usage.InputTokens != 11 {
		t.Fatalf("%+v", r)
	}
	if gotPath != "/model/anthropic.claude-3-haiku-20240307-v1%3A0/converse" {
		t.Fatalf("path %s", gotPath)
	}
	if gotDate != "20250102T030405Z" || gotTok != "session-token-xyz" {
		t.Fatalf("date %s token %s", gotDate, gotTok)
	}
	// Recompute the signature independently from what the server saw.
	wantAuth, _ := signV4("POST", gotPath, "", map[string]string{"content-type": "application/json", "host": gotHost, "x-amz-security-token": "session-token-xyz"},
		rawBody, awsCreds{"AKIDTEST", "sekret", "session-token-xyz"}, "us-east-1", "bedrock", fixed)
	if gotAuth != wantAuth {
		t.Fatalf("signature mismatch:\n got %s\nwant %s", gotAuth, wantAuth)
	}
	if !strings.HasPrefix(gotAuth, "AWS4-HMAC-SHA256 Credential=AKIDTEST/20250102/us-east-1/bedrock/aws4_request, SignedHeaders=content-type;host;x-amz-date;x-amz-security-token, Signature=") {
		t.Fatalf("auth %s", gotAuth)
	}
	if gotBody["system"].([]any)[0].(map[string]any)["text"] != "sys" || gotBody["inferenceConfig"].(map[string]any)["maxTokens"].(float64) != 50 {
		t.Fatalf("body %v", gotBody)
	}
	if strings.Contains(string(r.Request), "sekret") || strings.Contains(string(r.Request), "session-token-xyz") || strings.Contains(string(r.Request), gotAuth[len(gotAuth)-20:]) {
		t.Fatalf("credentials leaked: %s", r.Request)
	}
	want := (11*0.25 + 7*1.25) / 1e6
	if r.CostUSD != want {
		t.Fatalf("cost %v want %v", r.CostUSD, want)
	}
}

func TestVertexGemini(t *testing.T) {
	var gotPath string
	var gotHdr http.Header
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotHdr = r.URL.Path, r.Header.Clone()
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		if strings.Contains(string(b), "blocked") {
			io.WriteString(w, `{"promptFeedback":{"blockReason":"SAFETY"}}`)
			return
		}
		io.WriteString(w, `{"candidates":[{"content":{"parts":[{"text":"ciao"},{"functionCall":{"name":"f","args":{"a":2}}}]}}],"usageMetadata":{"promptTokenCount":9,"candidatesTokenCount":3}}`)
	}))
	defer srv.Close()

	// Gemini API with API key.
	a := mustNew(t, "vertex", VertexConfig{ChatCommon: ChatCommon{Model: "gemini-2.0-flash", SystemPrompt: "sys"}, BaseURL: srv.URL, APIKey: `{{ secret "GK" }}`})
	r := invoke(t, a, ex("hola"), InvokeOptions{Secrets: mapSecrets{"GK": "goog-key"}})
	if r.Output != "ciao" || len(r.ToolCalls) != 1 || string(r.ToolCalls[0].Arguments) != `{"a":2}` || r.Usage.OutputTokens != 3 {
		t.Fatalf("%+v", r)
	}
	if gotPath != "/v1beta/models/gemini-2.0-flash:generateContent" || gotHdr.Get("x-goog-api-key") != "goog-key" {
		t.Fatalf("path %s hdr %v", gotPath, gotHdr)
	}
	if strings.Contains(string(r.Request), "goog-key") {
		t.Fatal("key leaked")
	}
	// Vertex AI with bearer token.
	v := mustNew(t, "vertex", VertexConfig{ChatCommon: ChatCommon{Model: "gemini-2.5-pro"}, BaseURL: srv.URL, ProjectID: "p1", Location: "europe-west4", AccessToken: `{{ secret "T" }}`})
	invoke(t, v, ex("hola"), InvokeOptions{Secrets: mapSecrets{"T": "ya29.token"}})
	if gotPath != "/v1/projects/p1/locations/europe-west4/publishers/google/models/gemini-2.5-pro:generateContent" || gotHdr.Get("Authorization") != "Bearer ya29.token" {
		t.Fatalf("path %s hdr %v", gotPath, gotHdr)
	}
	// Blocked prompt is a permanent failure.
	_, err := a.Invoke(context.Background(), evalcore.Example{Input: map[string]any{"q": "blocked"}}, InvokeOptions{Secrets: mapSecrets{"GK": "k"}})
	if Classify(err) != ClassPermanent {
		t.Fatalf("blocked: %v", err)
	}
}

func TestLLMImplementations(t *testing.T) {
	srv := newMock(t, mockCfg())
	llm, err := NewLLM("openai", mustJSON(OpenAIConfig{ChatCommon: ChatCommon{Model: "gpt-4o-mini"}, BaseURL: srv.URL + "/v1",
		Tools: json.RawMessage(`[{"type":"function","function":{"name":"never_sent"}}]`)}), Deps{Clients: NewClientFactory(nil)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Plain completion; configured tools must not be sent (mock would answer with a tool call).
	resp, err := llm.Complete(context.Background(), evalcore.LLMRequest{System: "judge", Messages: msgs("user", "grade this"), MaxTokens: 50})
	if err != nil || resp.Content != mock.Truth("grade this") || resp.Usage.InputTokens == 0 || resp.CostUSD == 0 {
		t.Fatalf("%+v %v", resp, err)
	}
	// JSON mode.
	resp, err = llm.Complete(context.Background(), evalcore.LLMRequest{Messages: msgs("user", "grade this"), JSONMode: true})
	var verdict struct {
		Score  float64 `json:"score"`
		Passed bool    `json:"passed"`
	}
	if err != nil || json.Unmarshal([]byte(resp.Content), &verdict) != nil {
		t.Fatalf("json mode: %q %v", resp.Content, err)
	}
	// Logprobs of the first token.
	resp, err = llm.Complete(context.Background(), evalcore.LLMRequest{Messages: msgs("user", "yes or no?"), Logprobs: true, MaxTokens: 1})
	if err != nil || resp.TopLogprobs["Yes"] != -0.1 || resp.TopLogprobs["No"] != -2.4 {
		t.Fatalf("logprobs: %+v %v", resp, err)
	}
	if _, err := NewLLM("http", mustJSON(HTTPConfig{URL: "http://x"}), Deps{}, nil); err == nil {
		t.Fatal("http cannot be an LLM")
	}
}
