package targets

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/prat31/airval/internal/evalcore"
	"github.com/prat31/airval/tools/mocktarget/mock"
)

func TestHTTPMappingRAG(t *testing.T) {
	srv := newMock(t, mockCfg())
	a := mustNew(t, "http", HTTPConfig{
		URL:     srv.URL + "/rag",
		Headers: map[string]string{"Authorization": `Bearer {{ secret "TOK" }}`, "X-Trial": "{{ .trial }}"},
		Body:    `{"question": {{ json .input.question }}, "expected": {{ json .example.expected }}}`,
		Response: ResponseMapping{
			Output: "$.answer", Context: "$.context[*].text",
			InputTokens: "$.usage.input_tokens", OutputTokens: "$.usage.output_tokens",
		},
	})
	e := ex("What is 2+2?")
	e.Expected = "42"
	r := invoke(t, a, e, InvokeOptions{Secrets: mapSecrets{"TOK": "tok-123"}, Trial: 3})
	if r.Output != "42" { // accuracy 1 + expected passthrough
		t.Fatalf("output %q", r.Output)
	}
	if len(r.Context) != 3 || !strings.Contains(r.Context[0], mock.Truth("What is 2+2?")) {
		t.Fatalf("context %v", r.Context)
	}
	if r.Usage.InputTokens == 0 || r.Usage.OutputTokens == 0 {
		t.Fatalf("usage %+v", r.Usage)
	}
	if r.Latency <= 0 || len(r.Raw) == 0 {
		t.Fatal("latency/raw missing")
	}
	// Redaction: the request record never contains the secret.
	if strings.Contains(string(r.Request), "tok-123") || !strings.Contains(string(r.Request), "[REDACTED]") {
		t.Fatalf("request not redacted: %s", r.Request)
	}
	var rec struct {
		Method  string
		URL     string
		Headers map[string]string
		Body    map[string]any
	}
	if err := json.Unmarshal(r.Request, &rec); err != nil || rec.Method != "POST" || rec.Headers["X-Trial"] != "3" || rec.Body["question"] != "What is 2+2?" {
		t.Fatalf("request record: %s (%v)", r.Request, err)
	}
}

func TestHTTPAgentTraceAndToolCalls(t *testing.T) {
	srv := newMock(t, mockCfg())
	a := mustNew(t, "agent", AgentConfig{HTTPConfig: HTTPConfig{
		URL:  srv.URL + "/agent",
		Body: `{"question": {{ json .input.question }}}`,
		Response: ResponseMapping{
			Output:    "$.output",
			ToolCalls: &ToolCallMapping{Path: "$.tool_calls[*]"},
			Trace:     &TraceMapping{Path: "$.trace[*]"},
		},
	}})
	if a.Kind() != "agent" {
		t.Fatal(a.Kind())
	}
	r := invoke(t, a, ex("find it"), InvokeOptions{})
	if len(r.ToolCalls) != 1 || r.ToolCalls[0].Name != "search" || string(r.ToolCalls[0].Arguments) != `{"query":"find it"}` {
		t.Fatalf("tool calls %+v", r.ToolCalls)
	}
	if len(r.Trace) != 3 || r.Trace[1].Kind != "tool" || r.Trace[1].Name != "search" || r.Trace[1].Duration != 30*time.Millisecond {
		t.Fatalf("trace %+v", r.Trace)
	}
}

func TestHTTPStreaming(t *testing.T) {
	srv := newMock(t, mock.Config{Accuracy: 1, ChunkDelay: 25 * time.Millisecond})
	for _, mode := range []string{"sse", "ndjson"} {
		t.Run(mode, func(t *testing.T) {
			a := mustNew(t, "http", HTTPConfig{
				URL:      srv.URL + "/echo?stream=" + mode,
				Body:     `{"question": {{ json .input.question }}}`,
				Stream:   &StreamConfig{Mode: mode, DeltaPath: "$.delta"},
				Response: ResponseMapping{InputTokens: "$.usage.input_tokens", OutputTokens: "$.usage.output_tokens"},
			})
			r := invoke(t, a, ex("one two three four"), InvokeOptions{})
			if r.Output != "one two three four" {
				t.Fatalf("output %q", r.Output)
			}
			if r.TTFT <= 0 || r.TTFT >= r.Latency {
				t.Fatalf("ttft %v latency %v", r.TTFT, r.Latency)
			}
			if r.Latency-r.TTFT < 50*time.Millisecond { // 3 more chunks at 25ms
				t.Fatalf("stream finished too early: ttft %v latency %v", r.TTFT, r.Latency)
			}
			if r.Usage.InputTokens != 4 {
				t.Fatalf("usage from final event: %+v", r.Usage)
			}
			if len(r.Raw) == 0 {
				t.Fatal("raw empty")
			}
		})
	}
}

func TestHTTPNonJSONAndErrors(t *testing.T) {
	var status int
	var retryAfter string
	var body string
	var delay time.Duration
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		st, ra, bd, dl := status, retryAfter, body, delay
		mu.Unlock()
		time.Sleep(dl)
		if ra != "" {
			w.Header().Set("Retry-After", ra)
		}
		w.WriteHeader(st)
		_, _ = w.Write([]byte(bd))
	}))
	defer srv.Close()

	// Plain text body, no mapping -> output is the body.
	mu.Lock()
	status, body = 200, "just text"
	mu.Unlock()
	a := mustNew(t, "http", HTTPConfig{Method: "GET", URL: srv.URL})
	if r := invoke(t, a, ex("x"), InvokeOptions{}); r.Output != "just text" {
		t.Fatalf("output %q", r.Output)
	}

	tests := []struct {
		name       string
		status     int
		retryAfter string
		body       string
		cfg        HTTPConfig
		delay      time.Duration
		want       Class
		wantRA     time.Duration
	}{
		{name: "429 with retry-after", status: 429, retryAfter: "2", want: ClassRateLimited, wantRA: 2 * time.Second},
		{name: "429 without hint", status: 429, want: ClassRateLimited},
		{name: "429 quota exhausted is permanent", status: 429, body: `{"error":{"code":"insufficient_quota"}}`, want: ClassPermanent},
		{name: "500", status: 500, want: ClassTransient},
		{name: "503", status: 503, want: ClassTransient},
		{name: "408", status: 408, want: ClassTransient},
		{name: "400", status: 400, want: ClassPermanent},
		{name: "401", status: 401, want: ClassPermanent},
		{name: "404", status: 404, want: ClassPermanent},
		{name: "unexpected 202 with expect_status 200", status: 202, cfg: HTTPConfig{ExpectStatus: []int{200}}, want: ClassPermanent},
		{name: "html where json is mapped", status: 200, body: "<html>", cfg: HTTPConfig{Response: ResponseMapping{Output: "$.a"}}, want: ClassPermanent},
		{name: "timeout", status: 200, delay: 400 * time.Millisecond, cfg: HTTPConfig{HTTPCommon: HTTPCommon{TimeoutMS: 80}}, want: ClassTransient},
		{name: "oversized", status: 200, body: strings.Repeat("a", 2048), cfg: HTTPConfig{HTTPCommon: HTTPCommon{MaxResponseBytes: 1024}}, want: ClassPermanent},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mu.Lock()
			status, retryAfter, body, delay = tc.status, tc.retryAfter, tc.body, tc.delay
			mu.Unlock()
			cfg := tc.cfg
			cfg.URL, cfg.Method = srv.URL, "GET"
			a := mustNew(t, "http", cfg)
			_, err := a.Invoke(context.Background(), ex("x"), InvokeOptions{})
			if err == nil {
				t.Fatal("expected error")
			}
			if got := Classify(err); got != tc.want {
				t.Fatalf("class %v want %v (%v)", got, tc.want, err)
			}
			if tc.want == ClassRateLimited {
				if d, _ := RetryAfter(err); d != tc.wantRA {
					t.Fatalf("retry after %v want %v", d, tc.wantRA)
				}
			}
		})
	}
}

func TestHTTPConnectionResetAndCancel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, _, _ := w.(http.Hijacker).Hijack()
		conn.Close() // reset before any response
	}))
	defer srv.Close()
	a := mustNew(t, "http", HTTPConfig{Method: "GET", URL: srv.URL})
	_, err := a.Invoke(context.Background(), ex("x"), InvokeOptions{})
	if Classify(err) != ClassTransient {
		t.Fatalf("reset: %v (%v)", err, Classify(err))
	}

	// Closed port: connection refused is transient.
	dead := httptest.NewServer(http.NotFoundHandler())
	url := dead.URL
	dead.Close()
	a = mustNew(t, "http", HTTPConfig{Method: "GET", URL: url})
	if _, err := a.Invoke(context.Background(), ex("x"), InvokeOptions{}); Classify(err) != ClassTransient {
		t.Fatalf("refused: %v", err)
	}

	// Caller cancellation is not classified as target failure.
	slow := newMock(t, mock.Config{Accuracy: 1, LatencyMean: time.Second})
	a = mustNew(t, "http", HTTPConfig{Method: "GET", URL: slow.URL + "/echo"})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err = a.Invoke(ctx, ex("x"), InvokeOptions{})
	if Classify(err) != ClassCanceled || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("cancel: %v (%v)", err, Classify(err))
	}
}

func TestMockInjectedFaults(t *testing.T) {
	srv := newMock(t, mock.Config{Accuracy: 1, RateLimitRate: 1, RetryAfter: 3 * time.Second})
	a := mustNew(t, "http", HTTPConfig{Method: "GET", URL: srv.URL + "/echo"})
	_, err := a.Invoke(context.Background(), ex("x"), InvokeOptions{})
	if d, ok := RetryAfter(err); !ok || d != 3*time.Second {
		t.Fatalf("429 from mock: %v", err)
	}
	srv2 := newMock(t, mock.Config{Accuracy: 1, ErrorRate: 1})
	a = mustNew(t, "http", HTTPConfig{Method: "GET", URL: srv2.URL + "/echo"})
	if _, err := a.Invoke(context.Background(), ex("x"), InvokeOptions{}); Classify(err) != ClassTransient {
		t.Fatalf("5xx from mock: %v", err)
	}
}

func TestMockAccuracyIsDeterministic(t *testing.T) {
	srv := newMock(t, mock.Config{Accuracy: 0.5})
	a := mustNew(t, "http", HTTPConfig{URL: srv.URL + "/rag", Body: `{"question": {{ json .input.question }}}`, Response: ResponseMapping{Output: "$.answer"}})
	correct := 0
	const n = 200
	for i := 0; i < n; i++ {
		q := "question " + string(rune('a'+i%26)) + strings.Repeat("x", i)
		r1 := invoke(t, a, ex(q), InvokeOptions{})
		r2 := invoke(t, a, ex(q), InvokeOptions{})
		if r1.Output != r2.Output {
			t.Fatal("non-deterministic answer")
		}
		if r1.Output == mock.Truth(q) {
			correct++
		}
		if (r1.Output == mock.Truth(q)) != mock.IsCorrect(q, 0.5) {
			t.Fatal("IsCorrect disagrees with server")
		}
	}
	if correct < n*35/100 || correct > n*65/100 {
		t.Fatalf("accuracy %d/%d far from 50%%", correct, n)
	}
}

func TestTestConnection(t *testing.T) {
	srv := newMock(t, mockCfg())
	a := mustNew(t, "http", HTTPConfig{
		URL:      srv.URL + "/rag",
		Headers:  map[string]string{"X-Key": `{{ secret "K" }}`},
		Body:     `{"question": {{ json .input.question }}}`,
		Response: ResponseMapping{Output: "$.answer", Context: "$.context[*].text", Cost: "$.nope"},
	})
	rep := TestConnection(context.Background(), a, ex("q?"), InvokeOptions{Secrets: mapSecrets{"K": "shh-secret"}})
	if !rep.OK || rep.Response.Output == "" || len(rep.Raw) == 0 {
		t.Fatalf("%+v", rep)
	}
	if strings.Contains(string(rep.Request), "shh-secret") {
		t.Fatal("secret leaked in report")
	}
	got := map[string]MappingResult{}
	for _, m := range rep.Mapping {
		got[m.Field] = m
	}
	if !got["output"].Matched || !got["context"].Matched || got["cost_usd"].Matched {
		t.Fatalf("mapping debug %+v", rep.Mapping)
	}
	// Failure is reported, not returned.
	bad := TestConnection(context.Background(), a, ex("q"), InvokeOptions{Secrets: mapSecrets{}})
	if bad.OK || bad.ErrorClass != "permanent" || !strings.Contains(bad.Error, `secret "K"`) {
		t.Fatalf("%+v", bad)
	}
}

func TestMutualTLS(t *testing.T) {
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if len(r.TLS.PeerCertificates) == 0 {
			http.Error(w, "no client cert", 401)
			return
		}
		_, _ = w.Write([]byte(`{"ok":"mtls"}`))
	}))
	srv.EnableHTTP2 = true
	srv.StartTLS()
	defer srv.Close()
	// Use the server's own cert as CA, and as the client cert (self-signed, accepted via ClientCAs below).
	// httptest's server doesn't require client certs by default; assert the client sends one.
	srv.TLS.ClientAuth = 1 // tls.RequestClientCert
	certPEM, keyPEM := testCertPEM(t, srv)
	secrets := mapSecrets{"CA": certPEM, "CERT": certPEM, "KEY": keyPEM}
	a := mustNew(t, "http", HTTPConfig{
		Method: "GET", URL: srv.URL, Response: ResponseMapping{Output: "$.ok"},
		HTTPCommon: HTTPCommon{TLS: &TLSConfig{CACert: `{{ secret "CA" }}`, ClientCert: `{{ secret "CERT" }}`, ClientKey: `{{ secret "KEY" }}`}},
	})
	r := invoke(t, a, ex("x"), InvokeOptions{Secrets: secrets})
	if r.Output != "mtls" {
		t.Fatalf("output %q", r.Output)
	}
	// Without the CA the handshake fails permanently (unknown authority).
	a2 := mustNew(t, "http", HTTPConfig{Method: "GET", URL: srv.URL})
	if _, err := a2.Invoke(context.Background(), ex("x"), InvokeOptions{}); Classify(err) != ClassPermanent {
		t.Fatalf("untrusted cert: %v", err)
	}
}

var _ = evalcore.Example{}
