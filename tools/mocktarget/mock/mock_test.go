package mock

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func post(t *testing.T, h http.Handler, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("POST", path, strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestEndpoints(t *testing.T) {
	h := NewServer(Config{Accuracy: 1})
	tests := []struct {
		name, path, body string
		check            func(t *testing.T, body map[string]any)
	}{
		{"chat completions", "/v1/chat/completions", `{"model":"m","messages":[{"role":"user","content":"hi"}]}`, func(t *testing.T, b map[string]any) {
			msg := b["choices"].([]any)[0].(map[string]any)["message"].(map[string]any)
			if msg["content"] != Truth("hi") {
				t.Fatalf("%v", msg)
			}
		}},
		{"rag", "/rag", `{"question":"q1"}`, func(t *testing.T, b map[string]any) {
			if b["answer"] != Truth("q1") || len(b["context"].([]any)) != 3 {
				t.Fatalf("%v", b)
			}
		}},
		{"agent", "/agent", `{"question":"q1"}`, func(t *testing.T, b map[string]any) {
			if len(b["trace"].([]any)) != 3 || len(b["tool_calls"].([]any)) != 1 {
				t.Fatalf("%v", b)
			}
		}},
		{"echo", "/echo", `{"input":"ping"}`, func(t *testing.T, b map[string]any) {
			if b["output"] != "ping" {
				t.Fatalf("%v", b)
			}
		}},
		{"chat new session", "/chat", `{"message":"hello"}`, func(t *testing.T, b map[string]any) {
			if b["session_id"] == "" || b["turn"].(float64) != 1 {
				t.Fatalf("%v", b)
			}
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := post(t, h, tc.path, tc.body)
			if rec.Code != 200 {
				t.Fatalf("status %d: %s", rec.Code, rec.Body)
			}
			var b map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &b); err != nil {
				t.Fatal(err)
			}
			tc.check(t, b)
		})
	}
}

func TestChatCompletionsStreamAndTools(t *testing.T) {
	h := NewServer(Config{Accuracy: 1})
	rec := post(t, h, "/v1/chat/completions", `{"stream":true,"stream_options":{"include_usage":true},"messages":[{"role":"user","content":"a b c"}]}`)
	body := rec.Body.String()
	if !strings.Contains(body, "data: [DONE]") || !strings.Contains(body, `"usage"`) || strings.Count(body, `"content"`) < 4 {
		t.Fatalf("stream body: %s", body)
	}
	rec = post(t, h, "/v1/chat/completions", `{"tools":[{"type":"function","function":{"name":"lookup"}}],"messages":[{"role":"user","content":"x"}]}`)
	if !strings.Contains(rec.Body.String(), `"tool_calls"`) || !strings.Contains(rec.Body.String(), "lookup") {
		t.Fatalf("tool body: %s", rec.Body)
	}
	// After a tool result the model answers.
	rec = post(t, h, "/v1/chat/completions", `{"tools":[{"type":"function","function":{"name":"lookup"}}],"messages":[{"role":"user","content":"x"},{"role":"tool","content":"r"}]}`)
	if strings.Contains(rec.Body.String(), `"tool_calls"`) {
		t.Fatalf("expected final answer: %s", rec.Body)
	}
}

func TestFaultInjectionAndAccuracy(t *testing.T) {
	h := NewServer(Config{RateLimitRate: 1, RetryAfter: 2 * time.Second})
	rec := post(t, h, "/rag", `{"question":"q"}`)
	if rec.Code != 429 || rec.Header().Get("Retry-After") != "2" {
		t.Fatalf("%d %v", rec.Code, rec.Header())
	}
	h = NewServer(Config{Accuracy: 1})
	req := httptest.NewRequest("POST", "/rag?mock_status=503", strings.NewReader(`{}`))
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 503 {
		t.Fatalf("override: %d", rec.Code)
	}
	n := 0
	for i := 0; i < 1000; i++ {
		if IsCorrect(strings.Repeat("q", i%50)+string(rune('a'+i%26))+strings.Repeat("z", i), 0.3) {
			n++
		}
	}
	if n < 230 || n > 370 {
		t.Fatalf("accuracy 0.3 produced %d/1000", n)
	}
	if IsCorrect("anything", -1) {
		t.Fatal("negative accuracy must mean 0%")
	}
}

func TestLatency(t *testing.T) {
	srv := httptest.NewServer(NewServer(Config{LatencyMean: 40 * time.Millisecond, Accuracy: 1}))
	defer srv.Close()
	start := time.Now()
	resp, err := http.Post(srv.URL+"/echo", "application/json", strings.NewReader(`{"q":"x"}`))
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if time.Since(start) < 35*time.Millisecond {
		t.Fatal("latency not applied")
	}
}
