package api_test

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/prat31/airval/internal/api"
	"github.com/prat31/airval/internal/auth"
	"github.com/prat31/airval/internal/config"
	"github.com/prat31/airval/internal/rbac"
	"github.com/prat31/airval/internal/secrets"
	"github.com/prat31/airval/internal/store"
	"github.com/prat31/airval/internal/store/sqlstore"
)

// newEnvWith is newEnv with a config tweak.
func newEnvWith(t *testing.T, tweak func(*config.Config)) *env {
	t.Helper()
	st, err := sqlstore.Open(context.Background(), filepath.Join(t.TempDir(), "api.db"), sqlstore.Options{Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	kr, err := secrets.NewKeyring(secrets.GenerateKey(), 1, "")
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := &config.Config{Server: config.Server{BaseURL: "http://airval.test"}, Telemetry: config.Telemetry{Metrics: true}}
	if tweak != nil {
		tweak(cfg)
	}
	s := api.New(api.Deps{Store: st, Keyring: kr, Auth: &auth.Service{Store: st, Log: log}, Authz: &rbac.Authorizer{Store: st},
		Config: cfg, Logger: log, Version: "test"})
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)
	return &env{t: t, srv: srv, store: st}
}

type world struct {
	e      *env
	owner  *client
	viewer *client
	org    string
	pid    string
}

func newWorld(t *testing.T, tweak func(*config.Config)) *world {
	t.Helper()
	e := newEnvWith(t, tweak)
	w := &world{e: e, owner: e.browser()}
	var me obj
	w.owner.do("POST", "/v1/setup", obj{"email": "owner@example.com", "password": "supersecret", "org_name": "Acme"}, 201, &me)
	w.owner.csrf = me["csrf_token"].(string)
	w.org = me["orgs"].([]any)[0].(obj)["id"].(string)
	var proj obj
	w.owner.do("POST", "/v1/projects", obj{"org_id": w.org, "name": "Bot"}, 201, &proj)
	w.pid = proj["id"].(string)
	w.owner.do("POST", "/v1/orgs/"+w.org+"/members", obj{"email": "viewer@example.com", "role": "viewer", "password": "viewerpass"}, 201, nil)
	w.viewer = e.browser()
	var vme obj
	w.viewer.do("POST", "/v1/auth/login", obj{"email": "viewer@example.com", "password": "viewerpass"}, 200, &vme)
	w.viewer.csrf = vme["csrf_token"].(string)
	return w
}

func (w *world) evaluator(name, kind string, cfg obj) string {
	var ev obj
	w.owner.do("POST", "/v1/projects/"+w.pid+"/evaluators", obj{"name": name, "kind": kind, "config": cfg}, 201, &ev)
	return ev["id"].(string)
}

func (w *world) dataset(name string, examples ...obj) string {
	var ds obj
	w.owner.do("POST", "/v1/projects/"+w.pid+"/datasets", obj{"name": name}, 201, &ds)
	if len(examples) > 0 {
		w.owner.do("POST", "/v1/datasets/"+ds["id"].(string)+"/examples", obj{"examples": examples}, 200, nil)
	}
	return ds["id"].(string)
}

func (c *client) upload(path, filename, content string, options obj, want int, out any) {
	c.e.t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if options != nil {
		ob, _ := json.Marshal(options)
		_ = mw.WriteField("options", string(ob))
	}
	fw, _ := mw.CreateFormFile("file", filename)
	_, _ = fw.Write([]byte(content))
	_ = mw.Close()
	req, _ := http.NewRequest("POST", c.e.srv.URL+path, &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	if c.csrf != "" {
		req.Header.Set(auth.CSRFHeader, c.csrf)
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		c.e.t.Fatal(err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != want {
		c.e.t.Fatalf("POST %s: status %d want %d: %s", path, resp.StatusCode, want, data)
	}
	if out != nil {
		if err := json.Unmarshal(data, out); err != nil {
			c.e.t.Fatalf("decode: %v: %s", err, data)
		}
	}
}

func num(v any) float64 { f, _ := v.(float64); return f }

// ---- targets ----

func TestTargetTestEndpoints(t *testing.T) {
	w := newWorld(t, nil)
	var gotAuth string
	mock := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		b, _ := io.ReadAll(r.Body)
		if strings.Contains(string(b), "boom") {
			http.Error(rw, "kaput", 500)
			return
		}
		_ = json.NewEncoder(rw).Encode(obj{"reply": "echo: " + string(b), "usage": obj{"in": 3, "out": 5}})
	}))
	defer mock.Close()
	w.owner.do("POST", "/v1/orgs/"+w.org+"/secrets", obj{"name": "SVC_KEY", "value": "sk-test-123"}, 201, nil)
	cfg := obj{"url": mock.URL, "headers": obj{"Authorization": `Bearer {{ secret "SVC_KEY" }}`},
		"body":     `{"q": {{ json .input.question }}}`,
		"response": obj{"output": "$.reply", "input_tokens": "$.usage.in", "output_tokens": "$.usage.out", "context": "$.nothing"}}
	var tg obj
	w.owner.do("POST", "/v1/projects/"+w.pid+"/targets", obj{"name": "svc", "kind": "http", "config": cfg}, 201, &tg)
	tid := tg["id"].(string)

	// Saved target, explicit example.
	var raw string
	w.owner.do("POST", "/v1/targets/"+tid+":test", obj{"example": obj{"input": obj{"question": "hi"}}}, 200, &raw)
	var res obj
	_ = json.Unmarshal([]byte(raw), &res)
	if res["ok"] != true || res["response"].(obj)["output"] == "" || !strings.Contains(res["response"].(obj)["output"].(string), `"q": "hi"`) {
		t.Fatalf("test result: %s", raw)
	}
	if gotAuth != "Bearer sk-test-123" {
		t.Fatalf("secret was not resolved server-side: %q", gotAuth)
	}
	if strings.Contains(raw, "sk-test-123") || !strings.Contains(raw, "[REDACTED]") {
		t.Fatalf("secret leaked or request not redacted: %s", raw)
	}
	if res["latency_ms"] == nil || res["raw_response"] == nil || res["request"] == nil {
		t.Fatalf("missing report fields: %s", raw)
	}
	var matched, unmatched bool
	for _, m := range res["mapping"].([]any) {
		row := m.(obj)
		if row["field"] == "output" && row["matched"] == true {
			matched = true
		}
		if row["field"] == "context" && row["matched"] == false {
			unmatched = true
		}
	}
	if !matched || !unmatched {
		t.Fatalf("mapping rows: %v", res["mapping"])
	}

	// Sample from a dataset's first example; empty body falls back to the built-in sample.
	did := w.dataset("qa", obj{"input": obj{"question": "from dataset"}})
	w.owner.do("POST", "/v1/targets/"+tid+":test", obj{"dataset_id": did}, 200, &res)
	if !strings.Contains(res["response"].(obj)["output"].(string), "from dataset") {
		t.Fatalf("dataset sample not used: %v", res)
	}
	w.owner.do("POST", "/v1/targets/"+tid+":test", nil, 200, &res)
	if res["ok"] != true {
		t.Fatalf("default sample: %v", res)
	}

	// Failures are reported, not thrown.
	w.owner.do("POST", "/v1/targets/"+tid+":test", obj{"example": obj{"input": obj{"question": "boom"}}}, 200, &res)
	if res["ok"] != false || res["error"] == "" || res["error_class"] == "" {
		t.Fatalf("failure report: %v", res)
	}

	// Draft test of an unsaved config; invalid configs are 400s with the field.
	w.owner.do("POST", "/v1/projects/"+w.pid+"/targets:test", obj{"kind": "http", "config": cfg, "example": obj{"input": obj{"question": "draft"}}}, 200, &res)
	if res["ok"] != true {
		t.Fatalf("draft: %v", res)
	}
	var bad obj
	w.owner.do("POST", "/v1/projects/"+w.pid+"/targets:test", obj{"kind": "http", "config": obj{"urll": "x"}}, 400, &bad)
	if bad["details"].(obj)["field"] != "config" || !strings.Contains(bad["message"].(string), "urll") {
		t.Fatalf("validation error: %v", bad)
	}
	w.owner.do("POST", "/v1/projects/"+w.pid+"/targets", obj{"name": "bad", "kind": "http", "config": obj{"method": "POST"}}, 400, nil) // url required
	w.owner.do("PATCH", "/v1/targets/"+tid, obj{"config": obj{"url": ""}}, 400, nil)

	// RBAC: viewers cannot test targets (they can send requests to arbitrary hosts).
	w.viewer.do("POST", "/v1/targets/"+tid+":test", obj{}, 403, nil)
	w.viewer.do("POST", "/v1/projects/"+w.pid+"/targets:test", obj{"kind": "http", "config": cfg}, 403, nil)
}

// ---- evaluators & gates & suites ----

func TestEvaluatorCatalogPreviewAndValidation(t *testing.T) {
	w := newWorld(t, nil)
	var cat obj
	w.viewer.do("GET", "/v1/evaluators/catalog", nil, 200, &cat)
	kinds := map[string]obj{}
	for _, it := range cat["items"].([]any) {
		kinds[it.(obj)["kind"].(string)] = it.(obj)
	}
	if eq := kinds["equals"]; eq == nil || eq["category"] != "deterministic" || eq["config_schema"] == nil || len(eq["apply_to"].([]any)) == 0 {
		t.Fatalf("catalog equals entry: %v", eq)
	}
	if kinds["faithfulness"]["category"] != "model" || kinds["human"]["category"] != "human" {
		t.Fatalf("catalog categories: %v %v", kinds["faithfulness"], kinds["human"])
	}
	e := newEnv(t)
	e.browser().do("GET", "/v1/evaluators/catalog", nil, 401, nil)

	// Live preview (deterministic).
	body := obj{"project_id": w.pid, "kind": "contains", "config": obj{"values": []string{"Paris"}},
		"example": obj{"input": obj{"q": "capital of France?"}}, "response": obj{"output": "The capital is Paris."}}
	var sc obj
	w.owner.do("POST", "/v1/evaluators:preview", body, 200, &sc)
	if sc["passed"] != true || num(sc["value"]) != 1 {
		t.Fatalf("preview pass: %v", sc)
	}
	body["response"] = obj{"output": "London."}
	w.owner.do("POST", "/v1/evaluators:preview", body, 200, &sc)
	if sc["passed"] != false {
		t.Fatalf("preview fail: %v", sc)
	}
	// Validation errors.
	body["config"] = obj{"bogus": 1}
	var bad obj
	w.owner.do("POST", "/v1/evaluators:preview", body, 400, &bad)
	if bad["details"].(obj)["field"] != "config" {
		t.Fatalf("preview validation: %v", bad)
	}
	body["config"], body["kind"] = obj{}, "contanes"
	w.owner.do("POST", "/v1/evaluators:preview", body, 400, &bad)
	if !strings.Contains(bad["message"].(string), "contains") { // did you mean
		t.Fatalf("kind suggestion: %v", bad)
	}
	// Judges need a project with a secret and a supported provider.
	body["kind"], body["config"] = "llm_rubric", obj{"rubric": "Be nice", "provider": "openai", "model": "gpt-x", "secret": "NOPE"}
	w.owner.do("POST", "/v1/evaluators:preview", body, 502, &bad)
	// RBAC: viewers cannot preview within a project (editor required).
	w.viewer.do("POST", "/v1/evaluators:preview", obj{"project_id": w.pid, "kind": "equals", "response": obj{"output": "x"}}, 403, nil)

	// CRUD validates configs and kinds.
	w.owner.do("POST", "/v1/projects/"+w.pid+"/evaluators", obj{"name": "bad", "kind": "contains", "config": obj{"nope": true}}, 400, nil)
	w.owner.do("POST", "/v1/projects/"+w.pid+"/evaluators", obj{"name": "bad", "kind": "not_a_kind"}, 400, nil)
	id := w.evaluator("has paris", "contains", obj{"values": []string{"Paris"}})
	w.owner.do("PATCH", "/v1/evaluators/"+id, obj{"config": obj{"expected": 3, "extra": 1}}, 400, nil)
	w.viewer.do("PATCH", "/v1/evaluators/"+id, obj{"name": "x"}, 403, nil)
}

func TestGatesTextASTAndSuiteValidation(t *testing.T) {
	w := newWorld(t, nil)
	faith := w.evaluator("Faithfulness", "contains", obj{"values": []string{"x"}})
	tone := w.evaluator("tone", "regex", obj{"pattern": "^hi"})
	var tg obj
	w.owner.do("POST", "/v1/projects/"+w.pid+"/targets", obj{"name": "t", "kind": "http", "config": obj{"url": "http://svc/x"}}, 201, &tg)
	did := w.dataset("d", obj{"input": obj{"q": "a"}})
	var su obj
	w.owner.do("POST", "/v1/projects/"+w.pid+"/suites", obj{"name": "s", "config": obj{"target_ids": []string{tg["id"].(string)},
		"datasets":   []obj{{"dataset_id": did}},
		"evaluators": []obj{{"evaluator_id": faith, "threshold": 0.5}, {"evaluator_id": tone}}}}, 201, &su)
	sid := su["id"].(string)

	// Suite config validation: references must exist, ranges are checked, details name the field.
	var bad obj
	missing := "00000000-0000-4000-8000-000000000001"
	w.owner.do("POST", "/v1/projects/"+w.pid+"/suites", obj{"name": "x", "config": obj{"target_ids": []string{missing}}}, 400, &bad)
	if bad["details"].(obj)["field"] != "config.target_ids[0]" {
		t.Fatalf("suite target validation: %v", bad)
	}
	w.owner.do("POST", "/v1/projects/"+w.pid+"/suites", obj{"name": "x", "config": obj{"datasets": []obj{{"dataset_id": did, "version": 7}}}}, 400, &bad)
	if bad["details"].(obj)["field"] != "config.datasets[0].version" {
		t.Fatalf("suite dataset version: %v", bad)
	}
	w.owner.do("POST", "/v1/projects/"+w.pid+"/suites", obj{"name": "x", "config": obj{"evaluators": []obj{{"evaluator_id": faith, "threshold": 2}}}}, 400, &bad)
	if bad["details"].(obj)["field"] != "config.evaluators[0].threshold" {
		t.Fatalf("suite threshold: %v", bad)
	}
	w.owner.do("PATCH", "/v1/suites/"+sid, obj{"config": obj{"trials": 99}}, 400, nil)

	// Gate from text: names resolve to ids, canonical text is returned with names.
	var g obj
	w.owner.do("POST", "/v1/suites/"+sid+"/gates", obj{"name": "q", "text": "MEAN(faithfulness) >= 85% AND count(critical_fail) == 0"}, 201, &g)
	if g["text"] != "mean(faithfulness) >= 0.85 and count(critical_fail) == 0" {
		t.Fatalf("gate text: %q", g["text"])
	}
	exprJSON, _ := json.Marshal(g["expr"])
	if !strings.Contains(string(exprJSON), faith) || strings.Contains(string(exprJSON), `"faithfulness"`) {
		t.Fatalf("expr should reference the evaluator id: %s", exprJSON)
	}
	// AST in, names allowed in the AST too.
	var g2 obj
	w.owner.do("POST", "/v1/suites/"+sid+"/gates", obj{"expr": obj{"op": "cmp", "cmp": ">=", "args": []obj{
		{"op": "call", "func": "pass_rate", "params": []string{tone}}, {"op": "num", "value": 0.9}}}}, 201, &g2)
	if g2["text"] != "pass_rate(tone) >= 0.9" {
		t.Fatalf("gate2 text: %v", g2["text"])
	}
	// Friendly errors: did-you-mean, parse position, both/neither of text and expr.
	w.owner.do("POST", "/v1/suites/"+sid+"/gates", obj{"text": "mean(faithfulnes) >= 0.8"}, 400, &bad)
	d := bad["details"].(obj)
	if d["field"] != "text" || d["suggestion"] != "faithfulness" || !strings.Contains(bad["message"].(string), "did you mean") {
		t.Fatalf("unknown evaluator error: %v", bad)
	}
	w.owner.do("POST", "/v1/suites/"+sid+"/gates", obj{"text": "mean(faithfulness) >="}, 400, &bad)
	if _, ok := bad["details"].(obj)["position"]; !ok {
		t.Fatalf("parse error position: %v", bad)
	}
	w.owner.do("POST", "/v1/suites/"+sid+"/gates", obj{"text": "mean(tone) >= 1", "expr": g["expr"]}, 400, nil)
	w.owner.do("POST", "/v1/suites/"+sid+"/gates", obj{}, 400, nil)
	w.owner.do("POST", "/v1/suites/"+sid+"/gates", obj{"text": "mean(tone)"}, 400, nil) // not a condition

	// Update by text, validate endpoint, rename follows through.
	var up obj
	w.owner.do("PATCH", "/v1/suites/"+sid+"/gates/"+g["id"].(string), obj{"text": "min(tone) > 0.5"}, 200, &up)
	if up["text"] != "min(tone) > 0.5" {
		t.Fatalf("updated gate: %v", up)
	}
	var vr obj
	w.viewer.do("POST", "/v1/suites/"+sid+"/gates:validate", obj{"text": "mean(tone) >= 0.5"}, 200, &vr)
	if vr["ok"] != true || vr["expr"] == nil || vr["text"] != "mean(tone) >= 0.5" {
		t.Fatalf("validate ok: %v", vr)
	}
	w.viewer.do("POST", "/v1/suites/"+sid+"/gates:validate", obj{"text": "mean(tonne) >= 0.5"}, 200, &vr)
	if vr["ok"] != false || vr["suggestion"] != "tone" {
		t.Fatalf("validate error: %v", vr)
	}
	w.owner.do("PATCH", "/v1/evaluators/"+tone, obj{"name": "Politeness"}, 200, nil)
	var gl obj
	w.owner.do("GET", "/v1/suites/"+sid+"/gates", nil, 200, &gl)
	texts := ""
	for _, it := range gl["items"].([]any) {
		texts += it.(obj)["text"].(string) + "|"
	}
	if !strings.Contains(texts, "min(politeness) > 0.5") {
		t.Fatalf("rename should show in gate text: %s", texts)
	}
	// RBAC.
	w.viewer.do("POST", "/v1/suites/"+sid+"/gates", obj{"text": "mean(politeness) >= 0.5"}, 403, nil)
}

func TestEvaluatorAgreement(t *testing.T) {
	w := newWorld(t, nil)
	ev := w.evaluator("judge", "contains", obj{"values": []string{"x"}})
	evID := uuidOf(t, ev)
	ctx := context.Background()
	did := w.dataset("d")
	var up obj
	var exs []obj
	for i := 0; i < 4; i++ {
		exs = append(exs, obj{"input": obj{"q": fmt.Sprint(i)}})
	}
	w.owner.do("POST", "/v1/datasets/"+did+"/examples", obj{"examples": exs}, 200, &up)
	run := store.Run{OrgID: uuidOf(t, w.org), ProjectID: uuidOf(t, w.pid), Status: store.RunSucceeded}
	if err := w.e.store.CreateRun(ctx, &run); err != nil {
		t.Fatal(err)
	}
	judge := []bool{true, true, false, false}
	human := []bool{true, false, false, false}
	judgeVal := []float64{1, 0.9, 0.2, 0.1}
	humanVal := []float64{1, 0.3, 0.1, 0}
	var rs []store.Result
	var ss []store.Score
	for i, e := range up["examples"].([]any) {
		ex := e.(obj)
		r := store.Result{ID: store.NewID(), RunID: run.ID, ExampleID: uuidOf(t, ex["id"]), ExampleRevisionID: uuidOf(t, ex["revision_id"]), Status: store.ResultPassed}
		rs = append(rs, r)
		ss = append(ss, store.Score{ResultID: r.ID, EvaluatorID: evID, RunID: run.ID, Value: ptr(judgeVal[i]), Passed: ptr(judge[i])})
	}
	if err := w.e.store.InsertResults(ctx, rs, ss); err != nil {
		t.Fatal(err)
	}
	for i, r := range rs {
		a := store.Annotation{OrgID: run.OrgID, ProjectID: run.ProjectID, ResultID: r.ID, Passed: ptr(human[i]), Value: ptr(humanVal[i])}
		if err := w.e.store.CreateAnnotation(ctx, &a); err != nil {
			t.Fatal(err)
		}
	}
	var ag obj
	w.viewer.do("GET", "/v1/evaluators/"+ev+"/agreement", nil, 200, &ag)
	// po = 3/4, human pass rate .25, judge pass rate .5 -> pe = .5 -> kappa = .5
	if num(ag["n"]) != 4 || num(ag["n_binary"]) != 4 || num(ag["cohens_kappa"]) != 0.5 || num(ag["percent_agreement"]) != 0.75 {
		t.Fatalf("agreement: %v", ag)
	}
	conf := ag["confusion"].(obj)
	if num(conf["tp"]) != 1 || num(conf["fp"]) != 1 || num(conf["tn"]) != 2 || num(conf["fn"]) != 0 {
		t.Fatalf("confusion: %v", conf)
	}
	if p := num(ag["pearson"]); p < 0.8 || p > 1 {
		t.Fatalf("pearson: %v", ag["pearson"])
	}
	if mae := num(ag["mean_abs_error"]); mae < 0.19 || mae > 0.21 {
		t.Fatalf("mae: %v", mae)
	}
	// Evaluator without annotations: n = 0 and no statistics.
	other := w.evaluator("other", "contains", obj{"values": []string{"y"}})
	ag = nil
	w.viewer.do("GET", "/v1/evaluators/"+other+"/agreement", nil, 200, &ag)
	if num(ag["n"]) != 0 || ag["cohens_kappa"] != nil {
		t.Fatalf("empty agreement: %v", ag)
	}
}

// ---- datasets: import / export ----

const csvData = "question,answer,topic,notes\nWhat is 2+2?,4,math,easy\nCapital of France?,Paris,geo,\nCapital of Peru?,Lima,geo,hard\n"

func TestDatasetImportPreviewCommitExport(t *testing.T) {
	w := newWorld(t, nil)
	did := w.dataset("imp")
	path := "/v1/datasets/" + did + ":import"

	// Phase 1: preview returns the parsed columns, sample rows and a suggested mapping; nothing is written.
	var pv obj
	w.owner.upload(path+"?preview=true", "qa.csv", csvData, nil, 200, &pv)
	sp := pv["source_preview"].(obj)
	if len(sp["columns"].([]any)) != 4 || len(sp["rows"].([]any)) != 3 || sp["format"] != "csv" {
		t.Fatalf("source preview: %v", sp)
	}
	sm := pv["suggested_mapping"].(obj)
	if sm["expected_column"] != "answer" || sm["input_columns"].(obj)["question"] == nil {
		t.Fatalf("suggested mapping: %v", sm)
	}
	var ds obj
	w.owner.do("GET", "/v1/datasets/"+did, nil, 200, &ds)
	if num(ds["example_count"]) != 0 {
		t.Fatal("preview must not write")
	}

	// Preview with a mapping shows the mapped examples.
	mapping := obj{"input_columns": obj{"question": "question"}, "expected_column": "answer", "tags_column": "topic", "metadata_columns": []string{"notes"}}
	w.owner.upload(path+"?preview=true", "qa.csv", csvData, obj{"mapping": mapping}, 200, &pv)
	prev := pv["preview"].([]any)
	if len(prev) != 3 || prev[0].(obj)["expected"] != "4" || prev[1].(obj)["tags"].([]any)[0] != "geo" {
		t.Fatalf("mapped preview: %v", pv["preview"])
	}
	// A mapping referencing a missing column is a clear 400.
	var bad obj
	w.owner.upload(path+"?preview=true", "qa.csv", csvData, obj{"mapping": obj{"input_columns": obj{"nope": "x"}}}, 400, &bad)

	// Phase 2: commit streams into the draft (and can tag + publish).
	var res obj
	w.owner.upload(path, "qa.csv", csvData, obj{"mapping": mapping, "tags": []string{"imported"}, "publish": true}, 200, &res)
	if num(res["count"]) != 3 || num(res["total"]) != 3 || num(res["failed"]) != 0 || res["version"].(obj)["number"] != float64(1) {
		t.Fatalf("commit result: %v", res)
	}
	var exl obj
	w.owner.do("GET", "/v1/datasets/"+did+"/examples?tag=imported", nil, 200, &exl)
	if len(exl["items"].([]any)) != 3 {
		t.Fatalf("imported examples: %v", exl)
	}
	// Round trip through export (jsonl and csv).
	var jsonl, csvOut string
	w.owner.do("GET", "/v1/datasets/"+did+"/versions/1/export", nil, 200, &jsonl)
	if strings.Count(jsonl, "\n") != 3 || !strings.Contains(jsonl, "Capital of Peru?") {
		t.Fatalf("export jsonl: %q", jsonl)
	}
	w.owner.do("GET", "/v1/datasets/"+did+"/versions/1/export?format=csv", nil, 200, &csvOut)
	if !strings.Contains(csvOut, "Lima") {
		t.Fatalf("export csv: %q", csvOut)
	}
	// Re-import the CSV export into a fresh dataset: auto-mapping works without an explicit mapping.
	did2 := w.dataset("roundtrip")
	w.owner.upload("/v1/datasets/"+did2+":import", "export.csv", csvOut, nil, 200, &res)
	if num(res["count"]) != 3 {
		t.Fatalf("auto-mapped re-import: %v", res)
	}

	// Append vs replace; limit; inline JSONL.
	jl := `{"question":"a","answer":"1"}` + "\n" + `{"question":"b","answer":"2"}` + "\n" + `{"question":"c","answer":"3"}`
	inline := obj{"source": obj{"kind": "inline", "format": "jsonl", "data": jl, "limit": 2},
		"mapping": obj{"input_columns": obj{"question": "question"}, "expected_column": "answer"}}
	w.owner.do("POST", path, inline, 200, &res)
	if num(res["count"]) != 2 {
		t.Fatalf("limit: %v", res)
	}
	w.owner.do("GET", "/v1/datasets/"+did, nil, 200, &ds)
	if num(ds["example_count"]) != 5 {
		t.Fatalf("append should keep existing: %v", ds["example_count"])
	}
	inline["mode"] = "replace"
	w.owner.do("POST", path, inline, 200, &res)
	w.owner.do("GET", "/v1/datasets/"+did, nil, 200, &ds)
	if num(ds["example_count"]) != 2 {
		t.Fatalf("replace should drop old draft examples: %v", ds["example_count"])
	}
	// Bad rows are reported, not fatal.
	w.owner.do("POST", path, obj{"source": obj{"kind": "inline", "format": "jsonl", "data": `{"q":"ok"}` + "\n" + `not json` + "\n" + `{"q":"ok2"}`},
		"mapping": obj{"whole_row_as_input": true}}, 200, &res)
	if num(res["count"]) != 2 || num(res["failed"]) != 1 || len(res["errors"].([]any)) != 1 {
		t.Fatalf("row errors: %v", res)
	}

	// Source validation.
	w.owner.do("POST", path, obj{}, 400, nil)
	w.owner.do("POST", path, obj{"source": obj{"kind": "upload"}}, 400, nil)
	w.owner.do("POST", path, obj{"source": obj{"kind": "huggingface"}}, 400, nil)
	// RBAC: viewers can export but not import.
	w.viewer.upload(path, "qa.csv", csvData, obj{"mapping": mapping}, 403, nil)
	w.viewer.do("GET", "/v1/datasets/"+did+"/versions/1/export", nil, 200, nil)
}

func TestDatasetImportUploadLimit(t *testing.T) {
	w := newWorld(t, func(c *config.Config) { c.Server.MaxUploadMB = 1 })
	did := w.dataset("big")
	big := "question\n" + strings.Repeat("0123456789abcdef0123456789abcdef\n", 40000) // ~1.3 MB
	w.owner.upload("/v1/datasets/"+did+":import?preview=true", "big.csv", big, nil, 413, nil)
	w.owner.upload("/v1/datasets/"+did+":import?preview=true", "ok.csv", "question\nhello\n", nil, 200, nil)
}

// ---- comparisons, trends, overview ----

func seedRunWithScores(t *testing.T, w *world, ev string, exs []any, vals []float64, label func(i int) string, mut func(*store.Run)) store.Run {
	t.Helper()
	ctx := context.Background()
	run := store.Run{OrgID: uuidOf(t, w.org), ProjectID: uuidOf(t, w.pid), Status: store.RunSucceeded, Total: len(exs), Completed: len(exs)}
	if mut != nil {
		mut(&run)
	}
	if err := w.e.store.CreateRun(ctx, &run); err != nil {
		t.Fatal(err)
	}
	var rs []store.Result
	var ss []store.Score
	for i, e := range exs {
		ex := e.(obj)
		r := store.Result{ID: store.NewID(), RunID: run.ID, ExampleID: uuidOf(t, ex["id"]), ExampleRevisionID: uuidOf(t, ex["revision_id"]),
			Status: store.ResultPassed, Output: "o", LatencyMs: float64(100 * (i + 1))}
		rs = append(rs, r)
		sc := store.Score{ResultID: r.ID, EvaluatorID: uuidOf(t, ev), RunID: run.ID, Value: ptr(vals[i]), Passed: ptr(vals[i] >= 0.5)}
		if label != nil {
			sc.Label = label(i)
		}
		ss = append(ss, sc)
	}
	if err := w.e.store.InsertResults(ctx, rs, ss); err != nil {
		t.Fatal(err)
	}
	return run
}

func TestCompareRunsMath(t *testing.T) {
	w := newWorld(t, nil)
	ev := w.evaluator("accuracy", "contains", obj{"values": []string{"x"}})
	did := w.dataset("d")
	var up obj
	var exs []obj
	for i := 0; i < 4; i++ {
		exs = append(exs, obj{"input": obj{"question": fmt.Sprintf("question %d", i)}})
	}
	w.owner.do("POST", "/v1/datasets/"+did+"/examples", obj{"examples": exs}, 200, &up)
	ex := up["examples"].([]any)
	a := seedRunWithScores(t, w, ev, ex, []float64{1, 1, 0.5, 0}, nil, nil)
	b := seedRunWithScores(t, w, ev, ex, []float64{1, 0, 0.5, 0.5}, func(i int) string { return []string{"win", "loss", "win", "win"}[i] }, nil)
	c := seedRunWithScores(t, w, ev, ex, []float64{1, 1, 1, 1}, nil, nil)

	var rep obj
	w.viewer.do("GET", fmt.Sprintf("/v1/comparisons?runs=%s,%s", a.ID, b.ID), nil, 200, &rep)
	if len(rep["runs"].([]any)) != 2 || rep["evaluator_id"] != ev {
		t.Fatalf("report header: %v", rep["runs"])
	}
	var row obj
	for _, r := range rep["scorecard"].([]any) {
		if r.(obj)["key"] == ev {
			row = r.(obj)
		}
	}
	base, cand := row["cells"].([]any)[0].(obj), row["cells"].([]any)[1].(obj)
	// baseline mean .625; candidate .5; paired mean delta -.125; pass rates .75 -> .5
	if num(base["mean"]) != 0.625 || num(cand["mean"]) != 0.5 || num(cand["delta_mean"]) != -0.125 || num(cand["delta_pass_rate"]) != 0 {
		t.Fatalf("scorecard cells: base=%v cand=%v", base, cand)
	}
	if cand["ci_low"] == nil || cand["ci_high"] == nil || num(cand["ci_low"]) > num(cand["delta_mean"]) || num(cand["ci_high"]) < num(cand["delta_mean"]) || num(cand["paired"]) != 4 {
		t.Fatalf("bootstrap CI should bracket the delta: %v", cand)
	}
	if base["delta_mean"] != nil {
		t.Fatal("baseline has no delta")
	}
	// Run-level rows exist.
	keys := ""
	for _, r := range rep["scorecard"].([]any) {
		keys += r.(obj)["key"].(string) + ","
	}
	if !strings.Contains(keys, "pass_rate") || !strings.Contains(keys, "latency_p95") || !strings.Contains(keys, "cost_usd") {
		t.Fatalf("run-level rows: %s", keys)
	}
	reg, imp := rep["regressions"].([]any), rep["improvements"].([]any)
	if len(reg) != 1 || len(imp) != 1 || num(rep["regressions_total"]) != 1 || num(rep["improvements_total"]) != 1 {
		t.Fatalf("regressions/improvements: %v / %v", reg, imp)
	}
	r0 := reg[0].(obj)
	if r0["example_id"] != ex[1].(obj)["id"] || num(r0["delta"]) != -1 || r0["input_preview"] != "question 1" {
		t.Fatalf("regression row: %v", r0)
	}
	if imp[0].(obj)["example_id"] != ex[3].(obj)["id"] || num(imp[0].(obj)["delta"]) != 0.5 {
		t.Fatalf("improvement row: %v", imp[0])
	}
	// Aligned rows: most changed example first, one cell per run, in order.
	rows := rep["rows"].([]any)
	if num(rep["rows_total"]) != 4 || len(rows) != 4 || rows[0].(obj)["example_id"] != ex[1].(obj)["id"] {
		t.Fatalf("rows: %v", rep["rows"])
	}
	cells := rows[0].(obj)["cells"].([]any)
	if len(cells) != 2 || num(cells[0].(obj)["value"]) != 1 || num(cells[1].(obj)["value"]) != 0 || cells[1].(obj)["passed"] != false {
		t.Fatalf("aligned cells: %v", cells)
	}
	if len(rep["scatter"].([]any)) != 4 {
		t.Fatalf("scatter: %v", rep["scatter"])
	}
	pw := rep["pairwise"].([]any)
	if len(pw) != 1 || num(pw[0].(obj)["wins"]) != 3 || num(pw[0].(obj)["losses"]) != 1 || num(pw[0].(obj)["win_rate"]) != 0.75 {
		t.Fatalf("pairwise: %v", rep["pairwise"])
	}

	// Pagination of ranked lists and rows; third run compared against the baseline.
	w.viewer.do("GET", fmt.Sprintf("/v1/comparisons?runs=%s,%s,%s&against=%s&limit=2&offset=2", a.ID, b.ID, c.ID, c.ID), nil, 200, &rep)
	if rep["against_run_id"] != c.ID.String() || len(rep["rows"].([]any)) != 2 || num(rep["rows_total"]) != 4 {
		t.Fatalf("paged report: rows=%d against=%v", len(rep["rows"].([]any)), rep["against_run_id"])
	}
	if len(rep["regressions"].([]any)) != 0 || num(rep["improvements_total"]) != 2 || len(rep["improvements"].([]any)) != 0 {
		t.Fatalf("offset past the end: %v %v", rep["regressions"], rep["improvements"])
	}
	row = obj{}
	for _, r := range rep["scorecard"].([]any) {
		if r.(obj)["key"] == ev {
			row = r.(obj)
		}
	}
	if len(row["cells"].([]any)) != 3 {
		t.Fatalf("three cells expected: %v", row)
	}

	// Input validation and RBAC.
	w.viewer.do("GET", "/v1/comparisons?runs="+a.ID.String(), nil, 400, nil)
	w.viewer.do("GET", fmt.Sprintf("/v1/comparisons?runs=%s,%s", a.ID, a.ID), nil, 400, nil)
	w.viewer.do("GET", fmt.Sprintf("/v1/comparisons?runs=%s,%s&against=%s", a.ID, b.ID, a.ID), nil, 400, nil)
	other := newWorld(t, nil)
	other.owner.do("GET", fmt.Sprintf("/v1/comparisons?runs=%s,%s", a.ID, b.ID), nil, 404, nil)

	// Saved comparisons CRUD with RBAC.
	var sc obj
	w.owner.do("POST", "/v1/projects/"+w.pid+"/comparisons", obj{"name": "v1 vs v2", "run_ids": []string{a.ID.String(), b.ID.String()}}, 201, &sc)
	w.viewer.do("POST", "/v1/projects/"+w.pid+"/comparisons", obj{"run_ids": []string{a.ID.String(), b.ID.String()}}, 403, nil)
	var list obj
	w.viewer.do("GET", "/v1/projects/"+w.pid+"/comparisons", nil, 200, &list)
	if len(list["items"].([]any)) != 1 {
		t.Fatalf("saved comparisons: %v", list)
	}
	w.viewer.do("DELETE", "/v1/comparisons/"+sc["id"].(string), nil, 403, nil)
	w.owner.do("DELETE", "/v1/comparisons/"+sc["id"].(string), nil, 204, nil)
}

func TestTrendsAndOverview(t *testing.T) {
	w := newWorld(t, nil)
	ctx := context.Background()
	st := w.e.store
	var tg obj
	w.owner.do("POST", "/v1/projects/"+w.pid+"/targets", obj{"name": "prod", "kind": "http", "config": obj{"url": "http://svc/x"}}, 201, &tg)
	var su obj
	w.owner.do("POST", "/v1/projects/"+w.pid+"/suites", obj{"name": "Nightly"}, 201, &su)
	suite, target := uuidOf(t, su["id"]), uuidOf(t, tg["id"])
	ev := w.evaluator("accuracy", "contains", obj{"values": []string{"x"}})

	mk := func(status string, gate *bool, cost float64, pass, score float64) store.Run {
		r := store.Run{OrgID: uuidOf(t, w.org), ProjectID: uuidOf(t, w.pid), SuiteID: &suite, TargetID: &target, Status: status,
			GatePassed: gate, CostUSD: cost, Total: 10, Completed: 10}
		if err := st.CreateRun(ctx, &r); err != nil {
			t.Fatal(err)
		}
		if err := st.UpsertRunAggregates(ctx, []store.RunAggregate{
			{RunID: r.ID, EvaluatorID: store.NilID, Metric: store.MetricPassRate, N: 10, PassRate: ptr(pass)},
			{RunID: r.ID, EvaluatorID: uuidOf(t, ev), Metric: store.MetricScore, N: 10, Mean: ptr(score)},
			{RunID: r.ID, EvaluatorID: store.NilID, Metric: store.MetricLatencyMs, N: 10, P50: ptr(120.0), P95: ptr(400.0)},
		}); err != nil {
			t.Fatal(err)
		}
		return r
	}
	r1 := mk(store.RunSucceeded, ptr(true), 1, 0.9, 0.8)
	r2 := mk(store.RunFailed, ptr(false), 3, 0.6, 0.5)

	var tr obj
	w.viewer.do("GET", "/v1/projects/"+w.pid+"/trends?metric=pass_rate", nil, 200, &tr)
	ser := tr["series"].([]any)
	if len(ser) != 1 || ser[0].(obj)["label"] != "Nightly · prod" {
		t.Fatalf("series: %v", tr)
	}
	pts := ser[0].(obj)["points"].([]any)
	if len(pts) != 2 || num(pts[0].(obj)["value"]) != 0.9 || pts[0].(obj)["run_id"] != r1.ID.String() || pts[1].(obj)["run_id"] != r2.ID.String() {
		t.Fatalf("run points: %v", pts)
	}
	// Daily bucket: runs created today collapse into one weighted-mean point; cost sums.
	w.viewer.do("GET", "/v1/projects/"+w.pid+"/trends?metric=pass_rate&bucket=day", nil, 200, &tr)
	pts = tr["series"].([]any)[0].(obj)["points"].([]any)
	if len(pts) != 1 || num(pts[0].(obj)["value"]) != 0.75 || num(pts[0].(obj)["n"]) != 20 {
		t.Fatalf("day bucket: %v", pts)
	}
	w.viewer.do("GET", "/v1/projects/"+w.pid+"/trends?metric=cost_usd&bucket=week", nil, 200, &tr)
	if v := num(tr["series"].([]any)[0].(obj)["points"].([]any)[0].(obj)["value"]); v != 4 {
		t.Fatalf("cost sum: %v", v)
	}
	w.viewer.do("GET", "/v1/projects/"+w.pid+"/trends?metric=score_mean&evaluator_id="+ev, nil, 200, &tr)
	if s := tr["series"].([]any)[0].(obj); !strings.HasSuffix(s["label"].(string), "accuracy") || len(s["points"].([]any)) != 2 {
		t.Fatalf("score series: %v", s)
	}
	w.viewer.do("GET", "/v1/projects/"+w.pid+"/trends?metric=latency_p95&target_id="+target.String(), nil, 200, &tr)
	if num(tr["series"].([]any)[0].(obj)["points"].([]any)[0].(obj)["value"]) != 400 {
		t.Fatalf("latency: %v", tr)
	}
	w.viewer.do("GET", "/v1/projects/"+w.pid+"/trends?suite_id="+uuid4()+"&metric=pass_rate", nil, 200, &tr)
	if len(tr["series"].([]any)) != 0 {
		t.Fatalf("unknown suite should be empty: %v", tr)
	}
	w.viewer.do("GET", "/v1/projects/"+w.pid+"/trends?bucket=year", nil, 400, nil)

	// Overview.
	var ov obj
	w.viewer.do("GET", "/v1/projects/"+w.pid+"/overview", nil, 200, &ov)
	if num(ov["runs_7d"]) != 2 || num(ov["spend_7d_usd"]) != 4 || num(ov["failing_gates"]) != 1 {
		t.Fatalf("overview counters: %v", ov)
	}
	if num(ov["latest_pass_rate"]) != 0.6 || ov["latest_run"].(obj)["id"] != r2.ID.String() {
		t.Fatalf("latest: %v", ov)
	}
	if d := num(ov["pass_rate_delta"]); d > -0.29 || d < -0.31 {
		t.Fatalf("delta vs previous run: %v", ov["pass_rate_delta"])
	}
	if num(ov["avg_score"]) != 0.5 {
		t.Fatalf("avg score: %v", ov["avg_score"])
	}
	if fs := ov["failing_suites"].([]any); len(fs) != 1 || fs[0].(obj)["name"] != "Nightly" || fs[0].(obj)["run_id"] != r2.ID.String() {
		t.Fatalf("failing suites: %v", ov["failing_suites"])
	}
	other := newWorld(t, nil)
	other.owner.do("GET", "/v1/projects/"+w.pid+"/overview", nil, 404, nil)
	e := newEnv(t)
	e.browser().do("GET", "/v1/projects/"+w.pid+"/overview", nil, 401, nil)
}

func uuid4() string { return "00000000-0000-4000-8000-0000000000aa" }

// ---- notification channels ----

func TestNotificationChannelTest(t *testing.T) {
	w := newWorld(t, nil)
	type hit struct {
		body   []byte
		header http.Header
	}
	hits := make(chan hit, 4)
	status := 200
	recv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		hits <- hit{b, r.Header.Clone()}
		rw.WriteHeader(status)
	}))
	defer recv.Close()
	w.owner.do("POST", "/v1/orgs/"+w.org+"/secrets", obj{"name": "HOOK_SECRET", "value": "s3cr3t"}, 201, nil)
	w.owner.do("POST", "/v1/orgs/"+w.org+"/secrets", obj{"name": "SLACK_PATH", "value": "services/T/B/X"}, 201, nil)

	var wh, sl, em obj
	w.owner.do("POST", "/v1/orgs/"+w.org+"/notification-channels", obj{"name": "hook", "kind": "webhook",
		"config": obj{"url": recv.URL + "/in", "signing_secret": "HOOK_SECRET", "headers": obj{"X-Env": "test"}}}, 201, &wh)
	w.owner.do("POST", "/v1/orgs/"+w.org+"/notification-channels", obj{"name": "slack", "kind": "slack",
		"config": obj{"url": recv.URL + `/{{ secret "SLACK_PATH" }}`}}, 201, &sl)
	w.owner.do("POST", "/v1/orgs/"+w.org+"/notification-channels", obj{"name": "mail", "kind": "email",
		"config": obj{"recipients": []string{"a@b.co"}}}, 201, &em)

	w.owner.do("POST", "/v1/orgs/"+w.org+"/notification-channels/"+wh["id"].(string)+":test", nil, 204, nil)
	h := <-hits
	var msg obj
	if err := json.Unmarshal(h.body, &msg); err != nil || msg["event"] != "test" || msg["title"] == "" {
		t.Fatalf("webhook payload: %s", h.body)
	}
	ts := h.header.Get("X-Airval-Timestamp")
	mac := hmac.New(sha256.New, []byte("s3cr3t"))
	mac.Write([]byte(ts + "."))
	mac.Write(h.body)
	if got, want := h.header.Get("X-Airval-Signature"), "sha256="+hex.EncodeToString(mac.Sum(nil)); got != want {
		t.Fatalf("signature %q want %q", got, want)
	}
	if h.header.Get("X-Env") != "test" {
		t.Fatal("custom header missing")
	}

	w.owner.do("POST", "/v1/orgs/"+w.org+"/notification-channels/"+sl["id"].(string)+":test", nil, 204, nil)
	h = <-hits
	if !strings.Contains(string(h.body), `"text"`) {
		t.Fatalf("slack payload: %s", h.body)
	}

	status = 500
	var bad obj
	w.owner.do("POST", "/v1/orgs/"+w.org+"/notification-channels/"+wh["id"].(string)+":test", nil, 502, &bad)
	<-hits
	if bad["code"] != "delivery_failed" {
		t.Fatalf("failure: %v", bad)
	}
	w.owner.do("POST", "/v1/orgs/"+w.org+"/notification-channels/"+em["id"].(string)+":test", nil, 501, nil)
	w.viewer.do("POST", "/v1/orgs/"+w.org+"/notification-channels/"+wh["id"].(string)+":test", nil, 403, nil)

	// A plugged-in sender (the engine's notifier) takes over.
	_ = time.Second
}
