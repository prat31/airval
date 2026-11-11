package targets

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/prat31/airval/internal/evalcore"
	"github.com/prat31/airval/tools/mocktarget/mock"
)

func benchExample() evalcore.Example {
	return evalcore.Example{ID: "b1", Input: map[string]any{"question": "What is the capital of France?"},
		Metadata: map[string]any{"lang": "en"}}
}

func BenchmarkTemplateRender(b *testing.B) {
	tpl, err := CompileTemplate("body", `{"question": {{ json .input.question }}, "lang": "{{ default "en" .example.metadata.lang }}", "trial": {{ .trial }}, "key": "{{ secret "K" }}"}`)
	if err != nil {
		b.Fatal(err)
	}
	tc := &turnCtx{ex: benchExample(), opts: InvokeOptions{Trial: 1}}
	sec := mapSecrets{"K": "secret"}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		env := newEnv(sec)
		if _, err := tpl.Render(tc.data(env)); err != nil {
			b.Fatal(err)
		}
	}
}

// stubRT answers every request with a canned JSON body without touching the
// network: benchmarks built on it measure pure adapter overhead (render,
// request build, record/redact, parse, map).
type stubRT struct{ body []byte }

func (s stubRT) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.Body != nil {
		io.Copy(io.Discard, r.Body)
		r.Body.Close()
	}
	return &http.Response{
		StatusCode: 200, Status: "200 OK", Proto: "HTTP/1.1", ProtoMajor: 1, ProtoMinor: 1,
		Header:        http.Header{"Content-Type": {"application/json"}},
		Body:          io.NopCloser(bytes.NewReader(s.body)),
		ContentLength: int64(len(s.body)),
		Request:       r,
	}, nil
}

// BenchmarkHTTPAdapterOverhead measures the adapter's own CPU cost per call
// (target: < 50us).
func BenchmarkHTTPAdapterOverhead(b *testing.B) {
	body := []byte(`{"answer":"The answer is 1234abcd.","context":[{"text":"doc a"},{"text":"doc b"},{"text":"doc c"}],"usage":{"input_tokens":42,"output_tokens":7}}`)
	f := NewClientFactory(nil)
	f.rt = stubRT{body}
	a, err := New("http", mustJSON(HTTPConfig{
		URL:     "http://target.invalid/rag",
		Headers: map[string]string{"Authorization": `Bearer {{ secret "TOK" }}`},
		Body:    `{"question": {{ json .input.question }}, "trial": {{ .trial }}}`,
		Response: ResponseMapping{Output: "$.answer", Context: "$.context[*].text",
			InputTokens: "$.usage.input_tokens", OutputTokens: "$.usage.output_tokens"},
	}), Deps{Clients: f})
	if err != nil {
		b.Fatal(err)
	}
	e, opts := benchExample(), InvokeOptions{Secrets: mapSecrets{"TOK": "t"}}
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := a.Invoke(ctx, e, opts); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkOpenAIAdapterOverhead(b *testing.B) {
	body := []byte(`{"choices":[{"message":{"role":"assistant","content":"The answer is 1234abcd."}}],"usage":{"prompt_tokens":42,"completion_tokens":7}}`)
	f := NewClientFactory(nil)
	f.rt = stubRT{body}
	a, err := New("openai", mustJSON(OpenAIConfig{ChatCommon: ChatCommon{Model: "gpt-4o-mini", SystemPrompt: "Be brief."},
		BaseURL: "http://target.invalid/v1", APIKey: `{{ secret "K" }}`}), Deps{Clients: f})
	if err != nil {
		b.Fatal(err)
	}
	e, opts := benchExample(), InvokeOptions{Secrets: mapSecrets{"K": "k"}}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := a.Invoke(context.Background(), e, opts); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkHTTPInvokeMock runs against the in-process mock over loopback TCP.
// Compare with BenchmarkRawHTTPBaseline: the difference is the adapter overhead.
func BenchmarkHTTPInvokeMock(b *testing.B) {
	srv := httptest.NewServer(mock.NewServer(mock.Config{Accuracy: 1}))
	defer srv.Close()
	a, err := New("http", mustJSON(HTTPConfig{
		URL: srv.URL + "/rag", Body: `{"question": {{ json .input.question }}}`,
		Response:   ResponseMapping{Output: "$.answer", Context: "$.context[*].text", InputTokens: "$.usage.input_tokens", OutputTokens: "$.usage.output_tokens"},
		HTTPCommon: HTTPCommon{MaxConcurrency: 64},
	}), Deps{Clients: NewClientFactory(nil)})
	if err != nil {
		b.Fatal(err)
	}
	e := benchExample()
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if _, err := a.Invoke(context.Background(), e, InvokeOptions{}); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkRawHTTPBaseline(b *testing.B) {
	srv := httptest.NewServer(mock.NewServer(mock.Config{Accuracy: 1}))
	defer srv.Close()
	f := NewClientFactory(nil)
	c := f.Client(hostPort("http", srv.Listener.Addr().String()), ClientOptions{Concurrency: 64})
	payload := []byte(`{"question":"What is the capital of France?"}`)
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			req, _ := http.NewRequest("POST", srv.URL+"/rag", bytes.NewReader(payload))
			req.Header.Set("Content-Type", "application/json")
			resp, err := c.Do(req)
			if err != nil {
				b.Fatal(err)
			}
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}
	})
}

func BenchmarkJSONPathFirst(b *testing.B) {
	doc := map[string]any{"a": map[string]any{"b": []any{map[string]any{"c": "x"}}}}
	p := MustCompilePath("$.a.b[0].c")
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		p.First(doc)
	}
}
