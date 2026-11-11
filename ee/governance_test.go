//go:build ee

package ee_test

import (
	"context"
	"encoding/base64"
	"encoding/csv"
	"strings"
	"testing"
	"time"

	"github.com/prat31/airval/ee/audit"
	"github.com/prat31/airval/ee/branding"
	"github.com/prat31/airval/internal/ext"
	"github.com/prat31/airval/internal/store"
)

func TestAuditMiddlewareAndExport(t *testing.T) {
	e := start(t)
	owner, org := e.Setup(t, "owner@acme.io")
	viewer, _ := e.Member(t, owner, org, "viewer@acme.io", "viewer")
	var proj obj
	owner.Do("POST", "/v1/projects", obj{"org_id": org, "name": "Bot"}, 201, &proj)
	pid := proj["id"].(string)
	var ds obj
	owner.Do("POST", "/v1/projects/"+pid+"/datasets", obj{"name": "golden", "description": "=HYPERLINK(\"x\")"}, 201, &ds)
	owner.Do("POST", "/v1/projects/"+pid+"/targets", obj{"name": "prod", "kind": "http",
		"config": obj{"url": "http://svc/chat", "headers": obj{"Authorization": "Bearer top-secret"}}}, 201, nil)
	viewer.Do("POST", "/v1/projects/"+pid+"/datasets", obj{"name": "nope"}, 403, nil) // denied calls are recorded too
	owner.Do("GET", "/v1/projects/"+pid, nil, 200, nil)                               // reads are not

	var au obj
	owner.Do("GET", "/v1/orgs/"+org+"/audit-log?action=dataset.create", nil, 200, &au)
	items := au["items"].([]any)
	if len(items) != 2 {
		t.Fatalf("dataset.create entries: %v", au)
	}
	denied, ok := items[0].(obj), items[1].(obj)
	if denied["status"].(float64) != 403 || denied["actor"] != "viewer@acme.io" {
		t.Fatalf("denied entry: %v", denied)
	}
	if ok["status"].(float64) != 201 || ok["target"] != "dataset:"+ds["id"].(string) || ok["ip"] == "" || ok["user_agent"] == "" {
		t.Fatalf("entry: %v", ok)
	}
	if ok["diff"].(obj)["request"].(obj)["name"] != "golden" || ok["diff"].(obj)["project_id"] != pid {
		t.Fatalf("diff: %v", ok["diff"])
	}
	owner.Do("GET", "/v1/orgs/"+org+"/audit-log?action=target.create", nil, 200, &au)
	if s := mustJSON(au); strings.Contains(s, "top-secret") || !strings.Contains(s, "[redacted]") {
		t.Fatalf("secrets must be redacted: %s", s)
	}
	// Core-audited actions are not duplicated by the middleware.
	owner.Do("POST", "/v1/orgs/"+org+"/secrets", obj{"name": "K", "value": "v"}, 201, nil)
	owner.Do("GET", "/v1/orgs/"+org+"/audit-log?action=secret", nil, 200, &au)
	if len(au["items"].([]any)) != 1 {
		t.Fatalf("secret entries: %v", au)
	}

	// CSV export (admin only), formula injection neutralized.
	viewer.Do("GET", "/v1/orgs/"+org+"/audit-log/export", nil, 403, nil)
	var out string
	owner.Do("GET", "/v1/orgs/"+org+"/audit-log/export?action=dataset.", nil, 200, &out)
	rows, err := csv.NewReader(strings.NewReader(out)).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 || rows[0][0] != "at" || rows[0][3] != "action" {
		t.Fatalf("csv: %v", rows)
	}
	for _, r := range rows[1:] {
		for _, cell := range r {
			if cell != "" && strings.ContainsRune("=+-@", rune(cell[0])) {
				t.Fatalf("formula not neutralized: %q", cell)
			}
		}
	}
	owner.Do("GET", "/v1/orgs/"+org+"/audit-log/export?from=yesterday", nil, 400, nil)

	// Retention settings (owner) and enforcement.
	owner.Do("PUT", "/v1/orgs/"+org+"/audit-settings", obj{"retention_days": 3}, 422, nil)
	owner.Do("PUT", "/v1/orgs/"+org+"/audit-settings", obj{"retention_days": 30}, 200, nil)
	old := store.AuditEntry{OrgID: mustUUID(org), Actor: "x", Action: "old.thing", At: time.Now().AddDate(0, 0, -45)}
	if err := e.Store.AppendAudit(context.Background(), &old); err != nil {
		t.Fatal(err)
	}
	e.Tasks["ee.retention"](cancelled(t))
	owner.Do("GET", "/v1/orgs/"+org+"/audit-log?action=old.", nil, 200, &au)
	if len(au["items"].([]any)) != 0 {
		t.Fatal("audit retention not applied")
	}
}

// cancelled returns a context that is cancelled shortly, so background loops run one iteration.
func cancelled(t *testing.T) context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	t.Cleanup(cancel)
	return ctx
}

func TestAuditActionNames(t *testing.T) {
	for op, want := range map[string]string{"createDataset": "dataset.create", "upsertExamples": "example.upsert",
		"testNotificationChannel": "notification_channel.test", "rerunFailed": "run.rerun_failed", "createApiKey": "api_key.create"} {
		if got := audit.Action(ext.APICall{Operation: op}); got != want {
			t.Errorf("%s: %s want %s", op, got, want)
		}
	}
	if got := audit.Summarize([]byte(`{"password":"p","config":{"api_key":"k","url":"u"},"value":"s"}`)); strings.Contains(mustJSON(got), `"p"`) ||
		strings.Contains(mustJSON(got), `"k"`) || strings.Contains(mustJSON(got), `"s"`) {
		t.Fatalf("summary leaks: %v", got)
	}
}

func TestRetention(t *testing.T) {
	e := start(t)
	owner, org := e.Setup(t, "owner@acme.io")
	editor, _ := e.Member(t, owner, org, "ed@acme.io", "editor")
	var proj obj
	owner.Do("POST", "/v1/projects", obj{"org_id": org, "name": "Bot"}, 201, &proj)
	pid := mustUUID(proj["id"].(string))
	ctx := context.Background()
	suite := store.Suite{OrgID: mustUUID(org), ProjectID: pid, Name: "S", Slug: "s"}
	if err := e.Store.CreateSuite(ctx, &suite); err != nil {
		t.Fatal(err)
	}
	mk := func(age time.Duration, status string) store.Run {
		r := store.Run{OrgID: mustUUID(org), ProjectID: pid, SuiteID: &suite.ID, Status: status, Labels: map[string]string{},
			CreatedAt: time.Now().Add(-age)}
		if err := e.Store.CreateRun(ctx, &r); err != nil {
			t.Fatal(err)
		}
		return r
	}
	day := 24 * time.Hour
	expired := mk(40*day, store.RunSucceeded)
	baseline := mk(50*day, store.RunSucceeded)
	running := mk(40*day, store.RunRunning)
	fresh := mk(day, store.RunFailed)
	suite.BaselineRunID = &baseline.ID
	if err := e.Store.UpdateSuite(ctx, &suite); err != nil {
		t.Fatal(err)
	}

	path := "/v1/projects/" + pid.String() + "/retention-policy"
	var pol obj
	owner.Do("GET", path, nil, 200, &pol)
	if pol["enabled"] != false {
		t.Fatalf("default policy: %v", pol)
	}
	editor.Do("PUT", path, obj{"run_days": 30}, 403, nil)
	owner.Do("PUT", path, obj{"run_days": 0}, 422, nil)
	owner.Do("PUT", path, obj{"run_days": 30}, 200, &pol)
	if pol["eligible_runs"].(float64) != 1 || pol["keep_baselines"] != true {
		t.Fatalf("policy: %v", pol)
	}
	e.Tasks["ee.retention"](cancelled(t))
	for _, r := range []store.Run{baseline, running, fresh} {
		if _, err := e.Store.GetRun(ctx, r.ID); err != nil {
			t.Fatalf("run %s should be kept: %v", r.Status, err)
		}
	}
	if _, err := e.Store.GetRun(ctx, expired.ID); err == nil {
		t.Fatal("expired run should be deleted")
	}
	var list obj
	owner.Do("GET", "/v1/orgs/"+org+"/retention-policies", nil, 200, &list)
	p := list["items"].([]any)[0].(obj)
	if p["total_deleted"].(float64) != 1 || p["last_applied_at"] == nil {
		t.Fatalf("policy stats: %v", p)
	}
	// Without keep_baselines the pinned baseline goes too.
	owner.Do("PUT", path, obj{"run_days": 30, "keep_baselines": false}, 200, nil)
	e.Tasks["ee.retention"](cancelled(t))
	if _, err := e.Store.GetRun(ctx, baseline.ID); err == nil {
		t.Fatal("baseline should be deleted when keep_baselines is false")
	}
	owner.Do("DELETE", path, nil, 204, nil)
	var au obj
	owner.Do("GET", "/v1/orgs/"+org+"/audit-log?action=retention", nil, 200, &au)
	if len(au["items"].([]any)) < 4 {
		t.Fatalf("retention audit: %v", au)
	}
}

func TestBranding(t *testing.T) {
	e := start(t)
	owner, org := e.Setup(t, "owner@acme.io")
	viewer, _ := e.Member(t, owner, org, "v@acme.io", "viewer")
	var b obj
	viewer.Do("GET", "/v1/orgs/"+org+"/branding", nil, 200, &b)
	if b["accent"] != "iris" {
		t.Fatalf("default: %v", b)
	}
	viewer.Do("PUT", "/v1/orgs/"+org+"/branding", obj{"product_name": "X"}, 403, nil)
	png := "data:image/png;base64," + base64.StdEncoding.EncodeToString([]byte("\x89PNG\r\n\x1a\nrest"))
	owner.Do("PUT", "/v1/orgs/"+org+"/branding", obj{"product_name": "Acme Evals", "logo": png, "accent": "ocean"}, 200, nil)
	viewer.Do("GET", "/v1/orgs/"+org+"/branding", nil, 200, &b)
	if b["product_name"] != "Acme Evals" || b["accent"] != "ocean" || b["logo"] != png {
		t.Fatalf("branding: %v", b)
	}
	bad := []obj{
		{"accent": "#ff0000"},
		{"product_name": strings.Repeat("x", 41)},
		{"logo": "https://evil.example/logo.png"},
		{"logo": "data:image/png;base64," + base64.StdEncoding.EncodeToString([]byte("not a png"))},
		{"logo": "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString([]byte(`<svg onload="alert(1)"></svg>`))},
		{"logo": "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString([]byte(`<svg><script>x</script></svg>`))},
		{"logo": "data:image/png;base64," + base64.StdEncoding.EncodeToString(append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, branding.MaxLogoBytes)...))},
	}
	for _, in := range bad {
		owner.Do("PUT", "/v1/orgs/"+org+"/branding", in, 422, nil)
	}
	svg := "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString([]byte(`<svg xmlns="http://www.w3.org/2000/svg"><rect width="4" height="4"/></svg>`))
	owner.Do("PUT", "/v1/orgs/"+org+"/branding", obj{"logo": svg}, 200, nil)
}

func TestEditionLists(t *testing.T) {
	e := start(t)
	var ed obj
	e.Browser(t).Do("GET", "/v1/edition", nil, 200, &ed)
	if ed["edition"] != "enterprise" || len(ed["features"].([]any)) != 7 {
		t.Fatalf("edition: %v", ed)
	}
}
