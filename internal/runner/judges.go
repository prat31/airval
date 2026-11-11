package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/prat31/airval/internal/evalcore"
	"github.com/prat31/airval/internal/evaluators"
	"github.com/prat31/airval/internal/targets"
	"github.com/prat31/airval/internal/telemetry"
)

// judgePool owns one limiter per judge provider so slow or throttled judges never consume target
// concurrency, and one LLM client per (provider, model, secret, project).
type judgePool struct {
	concurrency int
	maxAttempts int
	clients     *targets.ClientFactory
	metrics     *telemetry.Metrics

	mu       sync.Mutex
	limiters map[string]*limiter
}

func newJudgePool(concurrency, maxAttempts int, clients *targets.ClientFactory, m *telemetry.Metrics) *judgePool {
	if concurrency <= 0 {
		concurrency = 32
	}
	return &judgePool{concurrency: concurrency, maxAttempts: maxAttempts, clients: clients, metrics: m,
		limiters: map[string]*limiter{}}
}

func (p *judgePool) limiter(provider string) *limiter {
	p.mu.Lock()
	defer p.mu.Unlock()
	l := p.limiters[provider]
	if l == nil {
		l = newLimiter(p.concurrency, true)
		p.limiters[provider] = l
	}
	return l
}

// simulator builds the user-simulator LLM of a run (rate-limited and retried like judges).
func (p *judgePool) simulator(sec evalcore.SecretResolver, provider, model, secret, baseURL string) (evalcore.LLM, error) {
	raw, err := llmConfig(provider, model, secret)
	if err != nil {
		return nil, err
	}
	if baseURL != "" {
		var m map[string]any
		_ = json.Unmarshal(raw, &m)
		m["base_url"] = baseURL
		raw, _ = json.Marshal(m)
	}
	inner, err := targets.NewLLM(provider, raw, targets.Deps{Clients: p.clients, DefaultConcurrency: p.concurrency}, sec)
	if err != nil {
		return nil, err
	}
	return &pooledLLM{inner: inner, lim: p.limiter(provider), provider: provider, maxAttempts: p.maxAttempts, metrics: p.metrics}, nil
}

// llmConfig builds a provider config for judges from a provider name, model and API-key secret.
func llmConfig(provider, model, secret string) (json.RawMessage, error) {
	cfg := map[string]any{"model": model}
	if secret != "" {
		ref := fmt.Sprintf(`{{ secret %q }}`, secret)
		switch provider {
		case "openai", "anthropic":
			cfg["api_key"] = ref
		case "vertex":
			cfg["api_key"] = ref
		default:
			return nil, fmt.Errorf("judge provider %q is not supported (use openai, anthropic or vertex)", provider)
		}
	}
	return json.Marshal(cfg)
}

// factory returns an evaluators.LLMFactory bound to a run's secret resolver.
func (p *judgePool) factory(sec evalcore.SecretResolver) evaluators.LLMFactory {
	var mu sync.Mutex
	cache := map[string]evalcore.LLM{}
	return func(provider, model, secret string) (evalcore.LLM, error) {
		key := provider + "\x00" + model + "\x00" + secret
		mu.Lock()
		defer mu.Unlock()
		if l, ok := cache[key]; ok {
			return l, nil
		}
		raw, err := llmConfig(provider, model, secret)
		if err != nil {
			return nil, err
		}
		inner, err := targets.NewLLM(provider, raw, targets.Deps{Clients: p.clients, DefaultConcurrency: p.concurrency}, sec)
		if err != nil {
			return nil, err
		}
		l := &pooledLLM{inner: inner, lim: p.limiter(provider), provider: provider, maxAttempts: p.maxAttempts, metrics: p.metrics}
		cache[key] = l
		return l, nil
	}
}

// pooledLLM rate-limits, retries and meters judge calls.
type pooledLLM struct {
	inner       evalcore.LLM
	lim         *limiter
	provider    string
	maxAttempts int
	metrics     *telemetry.Metrics
}

func (l *pooledLLM) Complete(ctx context.Context, req evalcore.LLMRequest) (evalcore.LLMResponse, error) {
	var lastErr error
	for attempt := 1; ; attempt++ {
		if err := l.lim.Acquire(ctx); err != nil {
			return evalcore.LLMResponse{}, err
		}
		start := time.Now()
		resp, err := l.inner.Complete(ctx, req)
		o, ra := classifyOutcome(err)
		l.lim.Release(o, time.Since(start), ra)
		if err == nil {
			if l.metrics != nil && resp.CostUSD > 0 {
				l.metrics.JudgeCostUSD.WithLabelValues(l.provider).Add(resp.CostUSD)
			}
			return resp, nil
		}
		lastErr = err
		if !retryable(err) || attempt >= l.maxAttempts {
			return evalcore.LLMResponse{}, lastErr
		}
		if err := sleepCtx(ctx, backoff(attempt, ra)); err != nil {
			return evalcore.LLMResponse{}, lastErr
		}
	}
}

// ---- embeddings (OpenAI-compatible /embeddings) ----

func (p *judgePool) embedderFactory(sec evalcore.SecretResolver) evaluators.EmbedderFactory {
	return func(provider, model, secret string) (evaluators.Embedder, error) {
		base := "https://api.openai.com/v1"
		switch provider {
		case "openai":
		default:
			if strings.HasPrefix(provider, "http://") || strings.HasPrefix(provider, "https://") {
				base = strings.TrimRight(provider, "/") // custom OpenAI-compatible endpoint
			} else {
				return nil, fmt.Errorf("embedding provider %q is not supported (use openai or an OpenAI-compatible base URL)", provider)
			}
		}
		u, err := url.Parse(base)
		if err != nil {
			return nil, err
		}
		port := u.Port()
		if port == "" {
			port = map[string]string{"https": "443", "http": "80"}[u.Scheme]
		}
		hc := p.clients.Client(u.Hostname()+":"+port, targets.ClientOptions{Concurrency: p.concurrency})
		return &openAIEmbedder{base: base, model: model, secret: secret, sec: sec, hc: hc, lim: p.limiter("embed:" + provider)}, nil
	}
}

type openAIEmbedder struct {
	base, model, secret string
	sec                 evalcore.SecretResolver
	hc                  *http.Client
	lim                 *limiter
}

func (e *openAIEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	body, _ := json.Marshal(map[string]any{"model": e.model, "input": texts})
	if err := e.lim.Acquire(ctx); err != nil {
		return nil, err
	}
	start := time.Now()
	out, err := e.call(ctx, body)
	o, ra := classifyOutcome(err)
	e.lim.Release(o, time.Since(start), ra)
	return out, err
}

func (e *openAIEmbedder) call(ctx context.Context, body []byte) ([][]float32, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.base+"/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if e.secret != "" {
		if e.sec == nil {
			return nil, errors.New("no secret resolver")
		}
		key, err := e.sec.Resolve(e.secret)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+key)
	}
	resp, err := e.hc.Do(req)
	if err != nil {
		return nil, targets.ErrTransient{Err: err}
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	switch {
	case resp.StatusCode == http.StatusTooManyRequests:
		return nil, targets.ErrRateLimited{Err: fmt.Errorf("embeddings: http 429")}
	case resp.StatusCode >= 500:
		return nil, targets.ErrTransient{Err: fmt.Errorf("embeddings: http %d", resp.StatusCode), Status: resp.StatusCode}
	case resp.StatusCode >= 300:
		return nil, targets.ErrPermanent{Err: fmt.Errorf("embeddings: http %d: %.200s", resp.StatusCode, data), Status: resp.StatusCode}
	}
	var parsed struct {
		Data []struct {
			Index     int       `json:"index"`
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil, targets.ErrPermanent{Err: fmt.Errorf("embeddings: %w", err)}
	}
	out := make([][]float32, len(parsed.Data))
	for _, d := range parsed.Data {
		if d.Index >= 0 && d.Index < len(out) {
			out[d.Index] = d.Embedding
		}
	}
	return out, nil
}
