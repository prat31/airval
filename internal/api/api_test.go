package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/airval/airval/internal/api"
	"github.com/airval/airval/internal/auth"
	"github.com/airval/airval/internal/config"
	"github.com/airval/airval/internal/rbac"
	"github.com/airval/airval/internal/secrets"
	"github.com/airval/airval/internal/store"
	"github.com/airval/airval/internal/store/sqlstore"
)

type env struct {
	t     *testing.T
	srv   *httptest.Server
	store store.Store
}

func newEnv(t *testing.T) *env {
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
	s := api.New(api.Deps{Store: st, Keyring: kr, Auth: &auth.Service{Store: st, Log: log}, Authz: &rbac.Authorizer{Store: st},
		Config: cfg, Logger: log, Version: "test",
		UI: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("spa")) })})
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)
	return &env{t: t, srv: srv, store: st}
}

// client is a browser-like client (cookie jar + CSRF) or an API-key client.
type client struct {
	e      *env
	hc     *http.Client
	csrf   string
	apiKey string
}

func (e *env) browser() *client {
	jar, _ := cookiejar.New(nil)
	return &client{e: e, hc: &http.Client{Jar: jar}}
}

func (c *client) do(method, path string, body any, want int, out any) {
	c.e.t.Helper()
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, c.e.srv.URL+path, r)
	req.Header.Set("Content-Type", "application/json")
	if c.csrf != "" {
		req.Header.Set(auth.CSRFHeader, c.csrf)
	}
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		c.e.t.Fatal(err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != want {
		c.e.t.Fatalf("%s %s: status %d want %d: %s", method, path, resp.StatusCode, want, data)
	}
	if out != nil {
		if s, ok := out.(*string); ok {
			*s = string(data)
			return
		}
		if err := json.Unmarshal(data, out); err != nil {
			c.e.t.Fatalf("%s %s: decode: %v: %s", method, path, err, data)
		}
	}
}

type obj = map[string]any

func TestAPIEndToEnd(t *testing.T) {
	e := newEnv(t)
	owner := e.browser()

	var status obj
	owner.do("GET", "/v1/setup", nil, 200, &status)
	if status["needs_setup"] != true {
		t.Fatal("expected needs_setup")
	}
	var me obj
	owner.do("POST", "/v1/setup", obj{"email": "Owner@Example.com", "password": "supersecret", "org_name": "Acme Inc"}, 201, &me)
	owner.csrf = me["csrf_token"].(string)
	orgID := me["orgs"].([]any)[0].(obj)["id"].(string)
	owner.do("POST", "/v1/setup", obj{"email": "x@y.z", "password": "supersecret", "org_name": "X"}, 409, nil)

	// CSRF is enforced for cookie sessions.
	noCSRF := &client{e: e, hc: owner.hc}
	noCSRF.do("POST", "/v1/projects", obj{"org_id": orgID, "name": "x"}, 403, nil)
	// Anonymous calls are rejected.
	e.browser().do("GET", "/v1/projects", nil, 401, nil)

	var proj obj
	owner.do("POST", "/v1/projects", obj{"org_id": orgID, "name": "Support Bot"}, 201, &proj)
	pid := proj["id"].(string)
	if proj["slug"] != "support-bot" {
		t.Fatalf("slug %v", proj["slug"])
	}
	var plist obj
	owner.do("GET", "/v1/projects", nil, 200, &plist)
	if len(plist["items"].([]any)) != 1 {
		t.Fatal("project list")
	}

	// Targets: config change creates a new version, same config doesn't.
	var tg obj
	owner.do("POST", "/v1/projects/"+pid+"/targets", obj{"name": "prod", "kind": "http",
		"config": obj{"http": obj{"url": "http://svc/chat"}}}, 201, &tg)
	tid := tg["id"].(string)
	owner.do("PATCH", "/v1/targets/"+tid, obj{"config": obj{"http": obj{"url": "http://svc/chat"}}}, 200, &tg)
	owner.do("PATCH", "/v1/targets/"+tid, obj{"config": obj{"http": obj{"url": "http://svc/v2"}}}, 200, &tg)
	if n := tg["current_version"].(obj)["number"].(float64); n != 2 {
		t.Fatalf("target version %v", n)
	}
	owner.do("POST", "/v1/targets/"+tid+":test", obj{}, 501, nil)
	owner.do("POST", "/v1/projects/"+pid+"/targets", obj{"name": "bad", "kind": "nope", "config": obj{}}, 422, nil)

	// Datasets: upsert, publish, export.
	var ds obj
	owner.do("POST", "/v1/projects/"+pid+"/datasets", obj{"name": "qa"}, 201, &ds)
	did := ds["id"].(string)
	var up obj
	owner.do("POST", "/v1/datasets/"+did+"/examples", obj{"examples": []obj{
		{"input": obj{"question": "capital of France?"}, "expected": "Paris", "tags": []string{"geo"}},
		{"input": obj{"question": "2+2"}, "expected": "4", "context": []string{"math facts"}},
		{"input": obj{"question": "hi"}, "conversation": obj{"turns": []obj{{"role": "user", "content": "hi"}}}},
	}}, 200, &up)
	if up["count"].(float64) != 3 {
		t.Fatal("upsert count")
	}
	owner.do("POST", "/v1/datasets/"+did+"/examples", obj{"examples": []obj{{"input": "not an object"}}}, 422, nil)
	var ver obj
	owner.do("POST", "/v1/datasets/"+did+"/versions", obj{"note": "v1"}, 201, &ver)
	if ver["number"].(float64) != 1 || ver["example_count"].(float64) != 3 {
		t.Fatalf("version %v", ver)
	}
	var exl obj
	owner.do("GET", "/v1/datasets/"+did+"/examples?version=1&tag=geo", nil, 200, &exl)
	if len(exl["items"].([]any)) != 1 {
		t.Fatal("version tag filter")
	}
	var jsonl string
	owner.do("GET", "/v1/datasets/"+did+"/versions/1/export", nil, 200, &jsonl)
	if strings.Count(jsonl, "\n") != 3 || !strings.Contains(jsonl, "capital of France") {
		t.Fatalf("export jsonl: %q", jsonl)
	}
	var csvOut string
	owner.do("GET", "/v1/datasets/"+did+"/versions/1/export?format=csv", nil, 200, &csvOut)
	if !strings.HasPrefix(csvOut, "id,input,expected") {
		t.Fatalf("export csv: %q", csvOut)
	}

	// Evaluators, suites, gates, triggers.
	var ev obj
	owner.do("POST", "/v1/projects/"+pid+"/evaluators", obj{"name": "exact", "kind": "equals"}, 201, &ev)
	if ev["category"] != "deterministic" || ev["version"].(float64) != 1 {
		t.Fatalf("evaluator %v", ev)
	}
	owner.do("PATCH", "/v1/evaluators/"+ev["id"].(string), obj{"config": obj{"case_sensitive": false}}, 200, &ev)
	if ev["version"].(float64) != 2 {
		t.Fatal("evaluator version bump")
	}
	var su obj
	owner.do("POST", "/v1/projects/"+pid+"/suites", obj{"name": "Smoke Test", "config": obj{"target_ids": []string{tid},
		"datasets": []obj{{"dataset_id": did, "version": 1}}, "evaluators": []obj{{"evaluator_id": ev["id"], "threshold": 1}}}}, 201, &su)
	sid := su["id"].(string)
	owner.do("GET", "/v1/projects/"+pid+"/suites/by-slug/smoke-test", nil, 200, nil)
	var gate obj
	owner.do("POST", "/v1/suites/"+sid+"/gates", obj{"name": "quality", "expr": obj{"op": "and", "args": []obj{
		{"op": "compare", "fn": "mean", "metric": "evaluator", "evaluator_id": ev["id"], "comparator": ">=", "value": 0.85},
		{"op": "compare", "fn": "count", "metric": "results", "selector": "critical_fail", "comparator": "==", "value": 0},
	}}}, 201, &gate)
	if !strings.Contains(gate["text"].(string), ">= 0.85 and count(critical_fail) == 0") {
		t.Fatalf("gate text %q", gate["text"])
	}
	owner.do("POST", "/v1/suites/"+sid+"/gates", obj{"expr": obj{"op": "compare", "fn": "bogus"}}, 422, nil)
	var trig obj
	owner.do("POST", "/v1/suites/"+sid+"/triggers", obj{"kind": "schedule", "config": obj{"cron": "0 3 * * *", "timezone": "Europe/Berlin"}}, 201, &trig)
	if trig["next_fire_at"] == nil {
		t.Fatal("next_fire_at not computed")
	}
	owner.do("POST", "/v1/suites/"+sid+"/triggers", obj{"kind": "schedule", "config": obj{"cron": "nope"}}, 422, nil)
	var wh obj
	owner.do("POST", "/v1/suites/"+sid+"/triggers", obj{"kind": "webhook"}, 201, &wh)
	if !strings.HasPrefix(wh["webhook_secret"].(string), "whsec_") || !strings.HasPrefix(wh["webhook_url"].(string), "http://airval.test/hooks/") {
		t.Fatalf("webhook trigger %v", wh)
	}
	var tl obj
	owner.do("GET", "/v1/suites/"+sid+"/triggers", nil, 200, &tl)
	for _, it := range tl["items"].([]any) {
		if _, ok := it.(obj)["webhook_secret"]; ok {
			t.Fatal("webhook secret leaked in list")
		}
	}
	owner.do("POST", "/v1/runs", obj{"suite_id": sid}, 501, nil)

	// Secrets are write-only.
	var sec obj
	owner.do("POST", "/v1/orgs/"+orgID+"/secrets", obj{"name": "OPENAI_KEY", "value": "sk-live-123"}, 201, &sec)
	var raw string
	owner.do("GET", "/v1/orgs/"+orgID+"/secrets", nil, 200, &raw)
	if strings.Contains(raw, "sk-live-123") || !strings.Contains(raw, "OPENAI_KEY") {
		t.Fatalf("secret list: %s", raw)
	}
	got, _ := e.store.GetSecret(context.Background(), uuidOf(t, sec["id"]))
	if bytes.Contains(got.Ciphertext, []byte("sk-live-123")) {
		t.Fatal("secret stored in plaintext")
	}

	// Members and RBAC: a viewer can read but not write.
	owner.do("POST", "/v1/orgs/"+orgID+"/members", obj{"email": "viewer@example.com", "role": "viewer", "password": "viewerpass"}, 201, nil)
	viewer := e.browser()
	var vme obj
	viewer.do("POST", "/v1/auth/login", obj{"email": "viewer@example.com", "password": "viewerpass"}, 200, &vme)
	viewer.csrf = vme["csrf_token"].(string)
	viewer.do("GET", "/v1/projects/"+pid+"/datasets", nil, 200, nil)
	viewer.do("POST", "/v1/projects/"+pid+"/datasets", obj{"name": "nope"}, 403, nil)
	viewer.do("GET", "/v1/orgs/"+orgID+"/secrets", nil, 403, nil)
	e.browser().do("POST", "/v1/auth/login", obj{"email": "viewer@example.com", "password": "wrong"}, 401, nil)
	// Last owner protection.
	ownerID := me["user"].(obj)["id"].(string)
	owner.do("PATCH", "/v1/orgs/"+orgID+"/members/"+ownerID, obj{"role": "admin"}, 409, nil)

	// API keys: project-scoped editor key.
	var key obj
	owner.do("POST", "/v1/orgs/"+orgID+"/api-keys", obj{"name": "ci", "role": "editor", "project_id": pid}, 201, &key)
	ci := &client{e: e, hc: http.DefaultClient, apiKey: key["key"].(string)}
	var kme obj
	ci.do("GET", "/v1/auth/me", nil, 200, &kme)
	if kme["auth_method"] != "api_key" {
		t.Fatal("api key auth")
	}
	ci.do("POST", "/v1/projects/"+pid+"/datasets", obj{"name": "from-ci"}, 201, nil) // no CSRF needed
	ci.do("GET", "/v1/orgs/"+orgID+"/members", nil, 404, nil)                        // org-level: out of scope
	var other obj
	owner.do("POST", "/v1/projects", obj{"org_id": orgID, "name": "Other"}, 201, &other)
	ci.do("GET", "/v1/projects/"+other["id"].(string), nil, 404, nil)
	var ciProjects obj
	ci.do("GET", "/v1/projects", nil, 200, &ciProjects)
	if len(ciProjects["items"].([]any)) != 1 {
		t.Fatal("project-scoped key should list one project")
	}
	bad := &client{e: e, hc: http.DefaultClient, apiKey: "airv_invalidinvalidinvalid"}
	bad.do("GET", "/v1/auth/me", nil, 401, nil)

	// Audit log captured security actions.
	var audit obj
	owner.do("GET", "/v1/orgs/"+orgID+"/audit-log", nil, 200, &audit)
	if len(audit["items"].([]any)) < 3 {
		t.Fatalf("audit entries: %v", audit)
	}

	// Runs/results/review reads over seeded data.
	run := store.Run{OrgID: uuidOf(t, orgID), ProjectID: uuidOf(t, pid), SuiteID: ptr(uuidOf(t, sid)), Status: store.RunSucceeded,
		Labels: map[string]string{"git_sha": "abc"}}
	if err := e.store.CreateRun(context.Background(), &run); err != nil {
		t.Fatal(err)
	}
	ex := up["examples"].([]any)[0].(obj)
	res := store.Result{ID: store.NewID(), RunID: run.ID, ExampleID: uuidOf(t, ex["id"]), ExampleRevisionID: uuidOf(t, ex["revision_id"]),
		Status: store.ResultFailed, Output: "Lyon", Request: json.RawMessage(`{"q":1}`)}
	if err := e.store.InsertResults(context.Background(), []store.Result{res}, []store.Score{{ResultID: res.ID,
		EvaluatorID: uuidOf(t, ev["id"]), RunID: run.ID, Value: ptr(0.0), Passed: ptr(false)}}); err != nil {
		t.Fatal(err)
	}
	var runs obj
	owner.do("GET", "/v1/runs?project_id="+pid+"&label=git_sha:abc", nil, 200, &runs)
	if len(runs["items"].([]any)) != 1 {
		t.Fatal("runs label filter")
	}
	var results obj
	owner.do("GET", "/v1/runs/"+run.ID.String()+"/results?status=failed&evaluator_id="+ev["id"].(string)+"&passed=false", nil, 200, &results)
	items := results["items"].([]any)
	if len(items) != 1 || items[0].(obj)["input"] == nil || items[0].(obj)["request"] != nil {
		t.Fatalf("results %v", results)
	}
	var rq obj
	owner.do("POST", "/v1/projects/"+pid+"/review-queues", obj{"name": "Failures"}, 201, &rq)
	var added obj
	owner.do("POST", "/v1/review-queues/"+rq["id"].(string)+"/items", obj{"run_id": run.ID}, 200, &added)
	if added["count"].(float64) != 1 {
		t.Fatal("review items add")
	}
	var ri obj
	owner.do("GET", "/v1/review-queues/"+rq["id"].(string)+"/items?status=pending&expand=true", nil, 200, &ri)
	item := ri["items"].([]any)[0].(obj)
	if item["result"] == nil {
		t.Fatal("expanded result missing")
	}
	owner.do("POST", "/v1/annotations", obj{"result_id": res.ID, "review_item_id": item["id"], "passed": false, "comment": "wrong city"}, 201, nil)
	owner.do("GET", "/v1/review-queues/"+rq["id"].(string), nil, 200, &rq)
	if rq["done_count"].(float64) != 1 {
		t.Fatal("annotation should complete the item")
	}
	var fromRes obj
	owner.do("POST", "/v1/datasets/"+did+":from-results", obj{"result_ids": []any{res.ID}, "use_output_as_expected": true}, 200, &fromRes)
	if fromRes["count"].(float64) != 1 {
		t.Fatal("from-results")
	}

	// SPA fallback and API 404s.
	var page string
	owner.do("GET", "/projects/x/runs", nil, 200, &page)
	if page != "spa" {
		t.Fatal("spa fallback")
	}
	owner.do("GET", "/v1/does-not-exist", nil, 404, nil)
	owner.do("POST", "/hooks/"+wh["id"].(string), obj{}, 501, nil)
	owner.do("GET", "/v1/auth/oidc/google/start", nil, 501, nil)
	owner.do("GET", "/v1/meta", nil, 200, nil)

	owner.do("POST", "/v1/auth/logout", nil, 204, nil)
	owner.do("GET", "/v1/auth/me", nil, 401, nil)
}

func ptr[T any](v T) *T { return &v }

func uuidOf(t *testing.T, v any) uuid.UUID {
	t.Helper()
	s, _ := v.(string)
	if u, ok := v.(uuid.UUID); ok {
		return u
	}
	id, err := uuid.Parse(s)
	if err != nil {
		t.Fatalf("not a uuid: %v", v)
	}
	return id
}
