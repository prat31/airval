package targets

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/airval/airval/internal/evalcore"
	"github.com/airval/airval/tools/mocktarget/mock"
)

type mapSecrets map[string]string

func (m mapSecrets) Resolve(name string) (string, error) {
	v, ok := m[name]
	if !ok {
		return "", fmt.Errorf("secret %q not found", name)
	}
	return v, nil
}

func newMock(t testing.TB, cfg mock.Config) *httptest.Server {
	t.Helper()
	if cfg.Accuracy == 0 {
		cfg.Accuracy = 1
	}
	srv := httptest.NewServer(mock.NewServer(cfg))
	t.Cleanup(srv.Close)
	return srv
}

func mustNew(t testing.TB, kind string, cfg any) Adapter {
	t.Helper()
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	a, err := New(kind, raw, Deps{Clients: NewClientFactory(nil)})
	if err != nil {
		t.Fatalf("New(%s): %v", kind, err)
	}
	return a
}

func ex(q string) evalcore.Example {
	return evalcore.Example{ID: "e1", Input: map[string]any{"question": q}}
}

func invoke(t testing.TB, a Adapter, e evalcore.Example, opts InvokeOptions) evalcore.Response {
	t.Helper()
	r, err := a.Invoke(context.Background(), e, opts)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	return r
}

// scriptedLLM returns canned user-simulator replies.
type scriptedLLM struct {
	replies []string
	calls   []evalcore.LLMRequest
}

func (s *scriptedLLM) Complete(_ context.Context, req evalcore.LLMRequest) (evalcore.LLMResponse, error) {
	s.calls = append(s.calls, req)
	i := len(s.calls) - 1
	if i >= len(s.replies) {
		return evalcore.LLMResponse{Content: StopToken}, nil
	}
	return evalcore.LLMResponse{Content: s.replies[i], Usage: evalcore.Usage{InputTokens: 5, OutputTokens: 3}, CostUSD: 0.001}, nil
}

var _ = http.StatusOK

func mockCfg() mock.Config { return mock.Config{Accuracy: 1} }

func mustJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

// msgs builds messages from alternating role, content pairs.
func msgs(pairs ...string) []evalcore.Message {
	var out []evalcore.Message
	for i := 0; i+1 < len(pairs); i += 2 {
		out = append(out, evalcore.Message{Role: pairs[i], Content: pairs[i+1]})
	}
	return out
}

// testCertPEM returns the httptest TLS server's leaf certificate and key as PEM.
func testCertPEM(t testing.TB, srv *httptest.Server) (certPEM, keyPEM string) {
	t.Helper()
	c := srv.TLS.Certificates[0]
	certPEM = string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: c.Certificate[0]}))
	kb, err := x509.MarshalPKCS8PrivateKey(c.PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	keyPEM = string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: kb}))
	return
}
