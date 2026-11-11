// Package enginetest boots a complete airval in-process (store + embedded NATS + API server +
// worker + mock AI service) for end-to-end tests and the execution-engine benchmark.
package enginetest

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/prat31/airval/internal/app"
	"github.com/prat31/airval/internal/auth"
	"github.com/prat31/airval/internal/config"
	"github.com/prat31/airval/internal/runner"
	"github.com/prat31/airval/internal/secrets"
	"github.com/prat31/airval/internal/store"
	"github.com/prat31/airval/tools/mocktarget/mock"
)

// Options configure a harness.
type Options struct {
	// DatabaseURL: empty = SQLite in a temp dir; postgres://… = a throwaway schema in that database.
	DatabaseURL string
	Concurrency int // worker concurrency (default 256)
	Mock        mock.Config
	LogLevel    string // default "error"
}

// Harness is a running airval.
type Harness struct {
	tb     testing.TB
	App    *app.App
	Server *httptest.Server
	Worker *runner.Engine
	Mock   *httptest.Server
	APIKey string
	OrgID  uuid.UUID
	hc     *http.Client
	cancel context.CancelFunc
	done   chan error
}

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Start boots the harness; it is torn down with tb.Cleanup.
func Start(tb testing.TB, o Options) *Harness {
	tb.Helper()
	ctx := context.Background()
	if o.Concurrency <= 0 {
		o.Concurrency = 256
	}
	if o.LogLevel == "" {
		o.LogLevel = "error"
	}
	dir := tb.TempDir()
	dbURL := o.DatabaseURL
	if dbURL == "" {
		dbURL = "sqlite://" + filepath.Join(dir, "airval.db")
	} else if strings.HasPrefix(dbURL, "postgres") {
		dbURL = pgSchema(tb, dbURL)
	}
	cfg, err := config.LoadUnvalidated(config.ModeServer, "")
	if err != nil {
		tb.Fatal(err)
	}
	cfg.Database.URL, cfg.Database.AutoMigrate, cfg.Database.MaxOpenConns = dbURL, true, 0
	cfg.NATS = config.NATS{StoreDir: filepath.Join(dir, "nats"), Port: -1, Replicas: 1, StreamMaxAgeHrs: 1}
	cfg.Secrets = config.Secrets{MasterKey: secrets.GenerateKey(), KeyVersion: 1}
	cfg.Workers.Concurrency, cfg.Workers.Embedded = o.Concurrency, false
	cfg.Workers.ShutdownTimeout = 10 * time.Second
	cfg.Telemetry.LogLevel, cfg.Telemetry.LogFormat, cfg.Telemetry.OTLPEndpoint = o.LogLevel, "text", ""
	cfg.Bootstrap = config.Bootstrap{AdminEmail: "admin@example.test", AdminPassword: randHex(12), AdminName: "Admin", OrgName: "Test"}
	a, err := app.New(ctx, config.ModeServer, cfg, app.BuildInfo{Version: "test"})
	if err != nil {
		tb.Fatal(err)
	}
	h := &Harness{tb: tb, App: a, hc: &http.Client{Timeout: 60 * time.Second}, done: make(chan error, 1)}
	h.Server = httptest.NewServer(a.API.Handler())
	a.Config.Server.BaseURL = h.Server.URL
	h.Mock = httptest.NewServer(mock.NewServer(o.Mock))
	h.Worker = a.NewWorker()
	wctx, cancel := context.WithCancel(ctx)
	h.cancel = cancel
	go func() { h.done <- h.Worker.Run(wctx) }()
	tb.Cleanup(h.Close)

	// API key for the bootstrap owner.
	u, err := a.Store.GetUserByEmail(ctx, cfg.Bootstrap.AdminEmail)
	if err != nil {
		tb.Fatal(err)
	}
	orgs, err := a.Store.ListUserOrgs(ctx, u.ID)
	if err != nil || len(orgs) == 0 {
		tb.Fatal("no org", err)
	}
	h.OrgID = orgs[0].ID
	key, prefix, hash := auth.NewAPIKey()
	if err := a.Store.CreateAPIKey(ctx, &store.APIKey{OrgID: h.OrgID, Name: "test", Prefix: prefix, Hash: hash,
		Role: store.RoleOwner, CreatedBy: &u.ID}); err != nil {
		tb.Fatal(err)
	}
	h.APIKey = key
	return h
}

// Close stops the worker, servers and app.
func (h *Harness) Close() {
	if h.cancel == nil {
		return
	}
	h.cancel()
	select {
	case <-h.done:
	case <-time.After(30 * time.Second):
		h.tb.Error("worker did not stop")
	}
	h.cancel = nil
	h.Server.Close()
	h.Mock.Close()
	h.App.Close()
}

func pgSchema(tb testing.TB, base string) string {
	ctx := context.Background()
	schema := "e_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:16]
	admin, err := sql.Open("pgx", base)
	if err != nil {
		tb.Fatal(err)
	}
	if _, err := admin.ExecContext(ctx, "CREATE SCHEMA "+schema); err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() {
		_, _ = admin.ExecContext(ctx, "DROP SCHEMA "+schema+" CASCADE")
		admin.Close()
	})
	sep := "?"
	if strings.Contains(base, "?") {
		sep = "&"
	}
	return base + sep + "search_path=" + schema
}

// Obj is a JSON object.
type Obj = map[string]any

// Do calls the API with the harness API key and decodes the response into out.
func (h *Harness) Do(method, path string, body any, want int, out any) {
	h.tb.Helper()
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, h.Server.URL+path, r)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+h.APIKey)
	resp, err := h.hc.Do(req)
	if err != nil {
		h.tb.Fatal(err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != want {
		h.tb.Fatalf("%s %s: status %d want %d: %s", method, path, resp.StatusCode, want, data)
	}
	if out != nil {
		if err := json.Unmarshal(data, out); err != nil {
			h.tb.Fatalf("%s %s: decode: %v: %s", method, path, err, data)
		}
	}
}

// Fixture is a project with a mock target, a dataset, evaluators, a suite and a gate.
type Fixture struct {
	ProjectID, TargetID, DatasetID, SuiteID string
	Evaluators                              map[string]string // name → id
	Questions                               []string
}

// Question returns the i-th fixture question.
func Question(i int) string { return fmt.Sprintf("What is fact number %d?", i) }

// NewFixture creates n examples (tagged even/odd) answered by the mock /rag endpoint, three
// deterministic evaluators (exact_match, format, latency) and the gate mean(exact_match) >= minMean.
func (h *Harness) NewFixture(n int, minMean float64, suiteExtra Obj) Fixture {
	h.tb.Helper()
	var f Fixture
	var p Obj
	h.Do("POST", "/v1/projects", Obj{"org_id": h.OrgID, "name": "Engine " + randHex(3)}, 201, &p)
	f.ProjectID = p["id"].(string)
	var t Obj
	h.Do("POST", "/v1/projects/"+f.ProjectID+"/targets", Obj{"name": "mock rag", "kind": "http", "config": Obj{
		"url":  h.Mock.URL + "/rag",
		"body": `{"question": {{ json .input.question }}, "expected": {{ json .example.expected }}}`,
		"response": Obj{"output": "$.answer", "context": "$.context[*].text",
			"input_tokens": "$.usage.input_tokens", "output_tokens": "$.usage.output_tokens"},
	}}, 201, &t)
	f.TargetID = t["id"].(string)
	var d Obj
	h.Do("POST", "/v1/projects/"+f.ProjectID+"/datasets", Obj{"name": "facts"}, 201, &d)
	f.DatasetID = d["id"].(string)
	for start := 0; start < n; start += 5000 {
		var exs []Obj
		for i := start; i < min(n, start+5000); i++ {
			q := Question(i)
			f.Questions = append(f.Questions, q)
			tag := "even"
			if i%2 == 1 {
				tag = "odd"
			}
			exs = append(exs, Obj{"input": Obj{"question": q}, "expected": mock.Truth(q), "tags": []string{tag}})
		}
		h.Do("POST", "/v1/datasets/"+f.DatasetID+"/examples", Obj{"examples": exs}, 200, nil)
	}
	h.Do("POST", "/v1/datasets/"+f.DatasetID+"/versions", Obj{"note": "v1"}, 201, nil)
	f.Evaluators = map[string]string{}
	for _, e := range []struct {
		name, kind string
		cfg        Obj
	}{
		{"exact_match", "equals", Obj{}},
		{"format", "regex", Obj{"pattern": `^The answer is [0-9a-f]{8}\.$`}},
		{"latency", "latency_ms", Obj{"max_ms": 10000}},
	} {
		var ev Obj
		h.Do("POST", "/v1/projects/"+f.ProjectID+"/evaluators", Obj{"name": e.name, "kind": e.kind, "config": e.cfg}, 201, &ev)
		f.Evaluators[e.name] = ev["id"].(string)
	}
	cfg := Obj{"target_ids": []string{f.TargetID}, "datasets": []Obj{{"dataset_id": f.DatasetID}},
		"evaluators": []Obj{{"evaluator_id": f.Evaluators["exact_match"], "severity": "critical"},
			{"evaluator_id": f.Evaluators["format"]}, {"evaluator_id": f.Evaluators["latency"], "severity": "minor"}}}
	for k, v := range suiteExtra {
		cfg[k] = v
	}
	var s Obj
	h.Do("POST", "/v1/projects/"+f.ProjectID+"/suites", Obj{"name": "facts suite", "config": cfg}, 201, &s)
	f.SuiteID = s["id"].(string)
	h.Do("POST", "/v1/suites/"+f.SuiteID+"/gates", Obj{"name": "exact match", "expr": Obj{"op": "cmp", "cmp": ">=",
		"args": []Obj{{"op": "call", "func": "mean", "params": []string{f.Evaluators["exact_match"]}}, {"op": "num", "value": minMean}}}},
		201, nil)
	return f
}

// StreamResult summarizes an SSE stream.
type StreamResult struct {
	Events   map[string]int
	Finished Obj
	LastSeq  string
}

// Stream follows /v1/runs/{id}/events until run.finished (or timeout).
func (h *Harness) Stream(runID string, timeout time.Duration) StreamResult {
	h.tb.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", h.Server.URL+"/v1/runs/"+runID+"/events", nil)
	req.Header.Set("Authorization", "Bearer "+h.APIKey)
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		h.tb.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		h.tb.Fatalf("events: %d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	res := StreamResult{Events: map[string]int{}}
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 1<<20), 8<<20)
	var typ string
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "event: "):
			typ = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "id: "):
			res.LastSeq = strings.TrimPrefix(line, "id: ")
		case strings.HasPrefix(line, "data: "):
			res.Events[typ]++
			if typ == "run.finished" {
				var e Obj
				_ = json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &e)
				res.Finished = e
				return res
			}
		}
	}
	if ctx.Err() != nil {
		h.tb.Fatalf("run %s did not finish within %s (events %v)", runID, timeout, res.Events)
	}
	return res
}

// StartRun creates runs and returns the first one.
func (h *Harness) StartRun(body Obj) Obj {
	h.tb.Helper()
	var created struct {
		Runs []Obj `json:"runs"`
	}
	h.Do("POST", "/v1/runs", body, 201, &created)
	if len(created.Runs) == 0 {
		h.tb.Fatal("no runs created")
	}
	return created.Runs[0]
}

// Run fetches a run.
func (h *Harness) Run(id string) Obj {
	var r Obj
	h.Do("GET", "/v1/runs/"+id, nil, 200, &r)
	return r
}
