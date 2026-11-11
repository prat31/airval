package enginetest

import (
	"net/url"
	"testing"
)

// TestSampleProject loads the one-click sample project: targets on the built-in /_mock/ service,
// a 1,000-example dataset, a gated suite and two finished runs (baseline fails its gate, the
// candidate passes) so the overview, charts and comparison have data immediately.
func TestSampleProject(t *testing.T) {
	if testing.Short() {
		t.Skip("short")
	}
	h := Start(t, Options{MockTarget: true})
	var meta Obj
	h.Do("GET", "/v1/meta", nil, 200, &meta)
	if meta["features"].(Obj)["sample_project"] != true {
		t.Fatalf("meta: sample_project not advertised: %v", meta)
	}
	var out Obj
	h.Do("POST", "/v1/projects:sample", Obj{"org_id": h.OrgID}, 201, &out)
	runs := out["run_ids"].([]any)
	if len(runs) != 2 || len(out["target_ids"].([]any)) != 2 || len(out["dataset_ids"].([]any)) != 2 {
		t.Fatalf("sample: %v", out)
	}
	base, cand := h.Run(runs[0].(string)), h.Run(runs[1].(string))
	if base["status"] != "failed" || base["gate_passed"] != false || int(num(base["completed"])) != 1000 {
		t.Fatalf("baseline run: status=%v gate=%v completed=%v", base["status"], base["gate_passed"], base["completed"])
	}
	if cand["status"] != "succeeded" || cand["gate_passed"] != true || int(num(cand["completed"])) != 1000 {
		t.Fatalf("candidate run: status=%v gate=%v completed=%v", cand["status"], cand["gate_passed"], cand["completed"])
	}
	if cand["baseline_run_id"] != runs[0] {
		t.Fatalf("candidate baseline = %v, want %v", cand["baseline_run_id"], runs[0])
	}
	var suite Obj
	h.Do("GET", "/v1/suites/"+out["suite_id"].(string), nil, 200, &suite)
	if suite["baseline_run_id"] != runs[0] {
		t.Fatalf("suite baseline = %v", suite["baseline_run_id"])
	}
	// Loading it again gets a fresh slug.
	var again Obj
	h.Do("POST", "/v1/projects:sample", Obj{"org_id": h.OrgID, "name": "Second sample"}, 201, &again)
	if again["project"].(Obj)["slug"] == out["project"].(Obj)["slug"] {
		t.Fatal("second sample reused the slug")
	}

	// Test connection fills {{ .vars.NAME }} from the request's variables.
	pid := out["project"].(Obj)["id"].(string)
	draft := Obj{"kind": "http", "config": Obj{"url": "{{ .vars.mock }}/rag", "body": `{"question": {{ json .input.question }}}`,
		"response": Obj{"output": "$.answer"}}, "example": Obj{"input": Obj{"question": "hi"}}}
	var res Obj
	draft["variables"] = Obj{"mock": h.Server.URL + "/_mock"}
	h.Do("POST", "/v1/projects/"+url.PathEscape(pid)+"/targets:test", draft, 200, &res)
	if res["ok"] != true {
		t.Fatalf("test connection with variables: %v", res)
	}
	delete(draft, "variables")
	h.Do("POST", "/v1/projects/"+url.PathEscape(pid)+"/targets:test", draft, 400, nil)
}
