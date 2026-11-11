package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/prat31/airval/internal/api/gen"
	"github.com/prat31/airval/internal/store"
	"github.com/prat31/airval/tools/mocktarget/mock"
)

// ---- built-in mock AI service (/_mock/) ----

// SetListenAddr records the address the HTTP server actually listens on (after net.Listen), so
// the sample project's targets can point at this process's /_mock/ service.
func (s *Server) SetListenAddr(addr string) {
	s.mu.Lock()
	s.listenAddr = addr
	s.mu.Unlock()
}

func (s *Server) mockEnabled() bool { return s.Config != nil && s.Config.Server.MockTarget }

// mockHandler is the mock service with a little realistic latency (so latency charts have shape).
func (s *Server) mockHandler() http.Handler {
	cfg := mock.DefaultConfig()
	cfg.LatencyMean = 40 * time.Millisecond
	cfg.LatencyJitter = 30 * time.Millisecond
	cfg.Seed = 7
	return mock.NewServer(cfg)
}

// MockBaseURL is the URL (without trailing slash) targets use to reach /_mock/. With an embedded
// worker (airval dev) that is the loopback listener; otherwise workers reach it via server.base_url.
func (s *Server) MockBaseURL() string {
	s.mu.RLock()
	addr := s.listenAddr
	s.mu.RUnlock()
	if s.Config != nil && s.Config.Workers.Embedded && addr != "" {
		host, port, err := net.SplitHostPort(addr)
		if err == nil {
			ip := net.ParseIP(host)
			switch {
			case host == "" || (ip != nil && ip.IsUnspecified()):
				host = "127.0.0.1"
			case ip != nil && ip.To4() == nil:
				host = "[" + host + "]"
			}
			return "http://" + host + ":" + port + "/_mock"
		}
	}
	base := "http://localhost:8080"
	if s.Config != nil && s.Config.Server.BaseURL != "" {
		base = s.Config.Server.BaseURL
	}
	return strings.TrimRight(base, "/") + "/_mock"
}

// ---- sample project ----

const sampleProjectName = "Sample · Support Copilot"

var sampleTopics = []struct {
	tag       string
	templates []string
	hard      []bool
}{
	{"billing", []string{
		"Why was I charged twice for order #%d?",
		"How do I update the card on file for account %d?",
		"Can I get an invoice for order #%d with my company VAT number?",
		"My %s subscription renewed early — can I get a prorated refund?",
	}, []bool{false, false, true, true}},
	{"shipping", []string{
		"Where is my order #%d? Tracking hasn't updated in three days.",
		"Can I change the delivery address for order #%d after it shipped?",
		"Do you ship the %s to Norway, and how long does it take?",
		"Order #%d arrived with a damaged box — what should I do?",
	}, []bool{false, true, false, false}},
	{"returns", []string{
		"How do I return the %s I bought last week?",
		"Is the return window for order #%d 30 or 60 days?",
		"Can I exchange my %s for a different color instead of a refund?",
		"I lost the receipt for order #%d — can I still return it?",
	}, []bool{false, false, true, true}},
	{"account", []string{
		"How do I reset the password for account %d?",
		"Can I merge my two accounts into account %d?",
		"How do I turn on two-factor authentication for my %s app?",
		"Please delete all personal data for account %d.",
	}, []bool{false, true, false, true}},
	{"technical", []string{
		"My %s won't pair over Bluetooth after the latest update.",
		"Error E-%d appears when I start the %s — how do I fix it?",
		"How do I factory-reset the %s without losing my settings?",
		"Is firmware %d compatible with the %s?",
	}, []bool{false, true, true, true}},
}

var sampleProducts = []string{"Aero headphones", "Nimbus speaker", "Orbit smartwatch", "Pulse earbuds", "Atlas router",
	"Lumen desk lamp", "Vector keyboard", "Drift e-bike", "Halo doorbell", "Comet tablet"}

// sampleQuestions builds n distinct, deterministic support questions with tags.
func sampleQuestions(n int) []store.ExampleUpsert {
	out := make([]store.ExampleUpsert, 0, n)
	seen := map[string]bool{}
	for i := 0; len(out) < n; i++ {
		t := sampleTopics[i%len(sampleTopics)]
		k := (i / len(sampleTopics)) % len(t.templates)
		tpl := t.templates[k]
		num := 10000 + (i*7919)%89999
		prod := sampleProducts[(i/3)%len(sampleProducts)]
		var q string
		switch strings.Count(tpl, "%") {
		case 2:
			if strings.Index(tpl, "%d") < strings.Index(tpl, "%s") {
				q = fmt.Sprintf(tpl, num, prod)
			} else {
				q = fmt.Sprintf(tpl, prod, num)
			}
		default:
			if strings.Contains(tpl, "%d") {
				q = fmt.Sprintf(tpl, num)
			} else {
				q = fmt.Sprintf(tpl, prod)
			}
		}
		if seen[q] {
			continue
		}
		seen[q] = true
		difficulty := "easy"
		if t.hard[k] {
			difficulty = "hard"
		}
		lang := "en"
		if i%9 == 0 {
			lang = "en-gb"
		}
		out = append(out, store.ExampleUpsert{ExampleData: store.ExampleData{
			Input:    mustJSON(map[string]string{"question": q}),
			Expected: mustJSON(mock.Truth(q)),
			Metadata: mustJSON(map[string]any{"category": t.tag, "difficulty": difficulty, "locale": lang}),
			Tags:     []string{t.tag, difficulty},
		}})
	}
	return out
}

// sampleRAG builds a small retrieval dataset: each example carries the gold document as context.
func sampleRAG() []store.ExampleUpsert {
	docs := []struct{ topic, q string }{
		{"warranty", "What does the two-year warranty cover on the %s?"},
		{"warranty", "Is accidental damage to the %s covered?"},
		{"setup", "What are the setup steps for the %s?"},
		{"setup", "Which app do I need to configure the %s?"},
		{"specs", "What is the battery life of the %s?"},
		{"specs", "Is the %s water resistant?"},
	}
	var out []store.ExampleUpsert
	for _, p := range sampleProducts {
		for _, d := range docs {
			q := fmt.Sprintf(d.q, p)
			ans := mock.Truth(q)
			out = append(out, store.ExampleUpsert{ExampleData: store.ExampleData{
				Input:    mustJSON(map[string]string{"question": q}),
				Expected: mustJSON(ans),
				Context:  mustJSON([]string{fmt.Sprintf("Regarding %q: %s", q, ans)}),
				Metadata: mustJSON(map[string]string{"product": p, "topic": d.topic}),
				Tags:     []string{d.topic, "rag"},
			}})
		}
	}
	return out
}

func sampleTargetConfig(base, variant string, accuracy float64) json.RawMessage {
	return mustJSON(map[string]any{
		"url":    fmt.Sprintf("%s/rag?mock_variant=%s&mock_accuracy=%.2f", base, variant, accuracy),
		"method": "POST",
		"body":   `{"question": {{ json .input.question }}}`,
		"response": map[string]any{
			"output":        "$.answer",
			"context":       "$.context[*].text",
			"input_tokens":  "$.usage.input_tokens",
			"output_tokens": "$.usage.output_tokens",
			"cost_usd":      "$.cost_usd",
		},
		"timeout_ms": 10000,
	})
}

func (s *Server) CreateSampleProject(ctx context.Context, req gen.CreateSampleProjectRequestObject) (gen.CreateSampleProjectResponseObject, error) {
	if req.Body == nil {
		return nil, invalid("org_id is required")
	}
	if err := s.require(ctx, req.Body.OrgId, nil, store.RoleEditor); err != nil {
		return nil, err
	}
	if !s.mockEnabled() {
		return nil, errorf(http.StatusConflict, "mock_disabled",
			"the built-in mock AI service is disabled; set server.mock_target (AIRVAL_SERVER_MOCK_TARGET=true) to load the sample project")
	}
	if err := s.requireQueue(); err != nil {
		return nil, err
	}
	name := strings.TrimSpace(deref(req.Body.Name, ""))
	if name == "" {
		name = sampleProjectName
	}
	var (
		p   store.Project
		err error
	)
	for i := 1; i <= 50; i++ {
		slug := "sample-support-copilot"
		if i > 1 {
			slug = fmt.Sprintf("%s-%d", slug, i)
		}
		p = store.Project{OrgID: req.Body.OrgId, Name: name, Slug: slug, Color: "iris",
			Description: "A demo AI support assistant served by airval's built-in mock service. Explore runs, charts and comparisons, then try your own target.",
			Settings:    json.RawMessage(`{}`)}
		if err = s.Store.CreateProject(ctx, &p); !errors.Is(err, store.ErrConflict) {
			break
		}
	}
	if err != nil {
		return nil, err
	}
	out, err := s.populateSample(ctx, p)
	if err != nil {
		// Don't leave a half-built sample behind.
		_ = s.Store.DeleteProject(context.WithoutCancel(ctx), p.ID)
		return nil, err
	}
	s.audit(ctx, p.OrgID, "project.sample_created", "project:"+p.ID.String(), nil)
	return gen.CreateSampleProject201JSONResponse(out), nil
}

func (s *Server) populateSample(ctx context.Context, p store.Project) (gen.SampleProject, error) {
	out := gen.SampleProject{Project: toProject(p)}
	uid := s.principal(ctx).UserID()
	base := s.MockBaseURL()

	// Targets: the current production model and a candidate (different variant + accuracy).
	prod := store.Target{OrgID: p.OrgID, ProjectID: p.ID, Name: "Support Copilot · prod", Kind: "http",
		Description: "Current production model (mock service, ~78% accurate)."}
	if err := s.Store.CreateTarget(ctx, &prod, sampleTargetConfig(base, "v1", 0.78), uid); err != nil {
		return out, err
	}
	cand := store.Target{OrgID: p.OrgID, ProjectID: p.ID, Name: "Support Copilot · candidate", Kind: "http",
		Description: "Candidate model under evaluation (mock service, ~86% accurate)."}
	if err := s.Store.CreateTarget(ctx, &cand, sampleTargetConfig(base, "v2", 0.86), uid); err != nil {
		return out, err
	}
	out.TargetIds = []uuid.UUID{prod.ID, cand.ID}
	p.Settings = mustJSON(map[string]string{"default_target_id": cand.ID.String()})
	if err := s.Store.UpdateProject(ctx, &p); err == nil {
		out.Project = toProject(p)
	}

	// Datasets.
	mkDataset := func(name, desc string, items []store.ExampleUpsert) (store.Dataset, error) {
		d := store.Dataset{OrgID: p.OrgID, ProjectID: p.ID, Name: name, Description: desc}
		if err := s.Store.CreateDataset(ctx, &d); err != nil {
			return d, err
		}
		if _, err := s.Store.UpsertExamples(ctx, d.ID, items); err != nil {
			return d, err
		}
		_, err := s.Store.PublishDatasetVersion(ctx, d.ID, "Sample data", uid)
		return d, err
	}
	support, err := mkDataset("Support questions",
		"1,000 customer-support questions across five categories, tagged by category and difficulty. Expected answers come from the mock service's ground truth.",
		sampleQuestions(1000))
	if err != nil {
		return out, err
	}
	rag, err := mkDataset("Product docs (RAG)",
		"Questions about product documentation, each with the gold passage as context, for retrieval and grounding checks.",
		sampleRAG())
	if err != nil {
		return out, err
	}
	out.DatasetIds = []uuid.UUID{support.ID, rag.ID}

	// Deterministic evaluators (no LLM keys needed).
	type evSpec struct {
		name, kind, desc string
		cfg              any
		threshold        float64
		severity         string
	}
	specs := []evSpec{
		{"Correct answer", "equals", "Output matches the expected answer exactly (whitespace and case normalized).", map[string]any{}, 1, "critical"},
		{"Answer similarity", "levenshtein", "Edit similarity to the expected answer (partial credit for near misses).", map[string]any{}, 0.9, "major"},
		{"Answer format", "regex", "Answers follow the house style: \"The answer is <code>.\"",
			map[string]any{"pattern": `^The answer is [0-9a-f]{8}\.$`, "full_match": true}, 1, "minor"},
		{"Latency budget", "latency_ms", "Responds within 75 ms.", map[string]any{"max_ms": 75}, 1, "minor"},
	}
	evs := make([]map[string]any, 0, len(specs))
	for _, sp := range specs {
		r, err := s.CreateEvaluator(ctx, gen.CreateEvaluatorRequestObject{Project: p.ID, Body: &gen.CreateEvaluatorJSONRequestBody{
			Name: sp.name, Kind: gen.EvaluatorKind(sp.kind), Description: &sp.desc, Config: ptr(gen.EvaluatorConfig(mustJSON(sp.cfg)))}})
		if err != nil {
			return out, fmt.Errorf("sample evaluator %q: %w", sp.name, err)
		}
		ev, ok := r.(gen.CreateEvaluator201JSONResponse)
		if !ok {
			return out, fmt.Errorf("sample evaluator %q: unexpected response", sp.name)
		}
		evs = append(evs, map[string]any{"evaluator_id": ev.Id, "threshold": sp.threshold, "severity": sp.severity})
	}

	// Suite + gates.
	cfg := mustJSON(map[string]any{
		"target_ids": []uuid.UUID{cand.ID},
		"datasets":   []map[string]any{{"dataset_id": support.ID}},
		"evaluators": evs,
		"limits":     map[string]any{"max_concurrency": 64, "adaptive": true},
		"baseline":   map[string]any{"policy": "pinned"},
	})
	desc := "Answer quality, format and latency on 1,000 support questions. Gated on accuracy and p95 latency."
	sr, err := s.CreateSuite(ctx, gen.CreateSuiteRequestObject{Project: p.ID, Body: &gen.CreateSuiteJSONRequestBody{
		Name: "Support quality", Slug: ptr("support-quality"), Description: &desc, Config: cfg}})
	if err != nil {
		return out, fmt.Errorf("sample suite: %w", err)
	}
	suite, ok := sr.(gen.CreateSuite201JSONResponse)
	if !ok {
		return out, errors.New("sample suite: unexpected response")
	}
	out.SuiteId = suite.Id
	for _, g := range []struct{ name, text string }{
		{"Accuracy", "pass_rate(correct-answer) >= 0.8"},
		{"Latency", "p95(latency_ms) < 250"},
	} {
		if _, err := s.CreateGate(ctx, gen.CreateGateRequestObject{Suite: suite.Id, Body: &gen.CreateGateJSONRequestBody{
			Name: ptr(g.name), Text: ptr(g.text)}}); err != nil {
			return out, fmt.Errorf("sample gate %q: %w", g.name, err)
		}
	}

	// Two real runs: production first (pinned as the baseline), then the candidate against it.
	start := func(target uuid.UUID, name string, labels map[string]string, baseline *uuid.UUID) (uuid.UUID, error) {
		r, err := s.CreateRun(ctx, gen.CreateRunRequestObject{Body: &gen.CreateRunJSONRequestBody{SuiteId: suite.Id,
			TargetIds: &[]uuid.UUID{target}, Name: ptr(name), Labels: ptr(gen.Labels(labels)), BaselineRunId: baseline}})
		if err != nil {
			return uuid.Nil, err
		}
		rr, ok := r.(gen.CreateRun201JSONResponse)
		if !ok || len(rr.Runs) == 0 {
			return uuid.Nil, errors.New("sample run: no run created")
		}
		return rr.Runs[0].Id, nil
	}
	wctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 90*time.Second)
	defer cancel()
	first, err := start(prod.ID, "Baseline · prod", map[string]string{"model": "v1", "sample": "true"}, nil)
	if err != nil {
		return out, fmt.Errorf("sample run: %w", err)
	}
	out.RunIds = append(out.RunIds, first)
	if err := s.waitRun(wctx, first); err != nil {
		return out, err
	}
	if _, err := s.UpdateSuite(ctx, gen.UpdateSuiteRequestObject{Suite: suite.Id, Body: &gen.UpdateSuiteJSONRequestBody{
		BaselineRunId: &first}}); err != nil {
		return out, err
	}
	second, err := start(cand.ID, "Candidate · v2", map[string]string{"model": "v2", "sample": "true"}, &first)
	if err != nil {
		return out, fmt.Errorf("sample run: %w", err)
	}
	out.RunIds = append(out.RunIds, second)
	if err := s.waitRun(wctx, second); err != nil {
		return out, err
	}
	return out, nil
}

// waitRun polls until a run is terminal (a run that is still going when ctx ends is left running).
func (s *Server) waitRun(ctx context.Context, id uuid.UUID) error {
	t := time.NewTicker(150 * time.Millisecond)
	defer t.Stop()
	for {
		r, err := s.Store.GetRun(ctx, id)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		if isTerminal(r.Status) {
			return nil
		}
		select {
		case <-ctx.Done():
			s.Logger.Warn("sample run still running after timeout; leaving it to finish", "run_id", id)
			return nil
		case <-t.C:
		}
	}
}
