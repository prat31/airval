package api_test

import (
	"context"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"

	"github.com/prat31/airval/internal/api"
	"github.com/prat31/airval/internal/ext"
	"github.com/prat31/airval/internal/ext/exttest"
)

func TestCommunityEditionHooks(t *testing.T) {
	e := exttest.Start(t, nil, exttest.Options{})
	owner, org := e.Setup(t, "owner@acme.io")
	anon := e.Browser(t)

	var ed exttest.Obj
	anon.Do("GET", "/v1/edition", nil, 200, &ed)
	if ed["edition"] != "community" || len(ed["features"].([]any)) != 0 {
		t.Fatalf("edition: %v", ed)
	}
	var ps exttest.Obj
	anon.Do("GET", "/v1/auth/providers", nil, 200, &ps)
	if len(ps["items"].([]any)) != 0 {
		t.Fatalf("providers: %v", ps)
	}
	// Documented ee endpoints answer 501 in community builds, public or not.
	var errBody exttest.Obj
	owner.Do("GET", "/v1/orgs/"+org+"/sso-providers", nil, 501, &errBody)
	if errBody["code"] != "enterprise_required" {
		t.Fatalf("code: %v", errBody)
	}
	owner.Do("PUT", "/v1/orgs/"+org+"/branding", exttest.Obj{}, 501, nil)
	anon.Do("GET", "/scim/v2/Users", nil, 501, nil)
	anon.Do("GET", "/v1/auth/oidc/x/start", nil, 501, nil)
	anon.Do("GET", "/v1/auth/saml/x/metadata", nil, 501, nil)

	// Core audit entries carry request metadata and can be filtered.
	owner.Do("POST", "/v1/orgs/"+org+"/secrets", exttest.Obj{"name": "OPENAI_KEY", "value": "sk-1"}, 201, nil)
	owner.Do("POST", "/v1/orgs/"+org+"/api-keys", exttest.Obj{"name": "ci", "role": "editor"}, 201, nil)
	var au exttest.Obj
	owner.Do("GET", "/v1/orgs/"+org+"/audit-log?action=secret.", nil, 200, &au)
	items := au["items"].([]any)
	if len(items) != 1 || items[0].(exttest.Obj)["action"] != "secret.create" {
		t.Fatalf("audit filter: %v", au)
	}
	if items[0].(exttest.Obj)["ip"] == "" || items[0].(exttest.Obj)["user_agent"] == "" {
		t.Fatalf("audit request metadata missing: %v", items[0])
	}
	owner.Do("GET", "/v1/orgs/"+org+"/audit-log?actor=nobody", nil, 200, &au)
	if len(au["items"].([]any)) != 0 {
		t.Fatal("actor filter")
	}
}

func TestEERoutesMatchSpec(t *testing.T) {
	doc, err := openapi3.NewLoader().LoadFromFile("../../api/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	want := map[[2]string]bool{}
	for path, item := range doc.Paths.Map() {
		for method, op := range item.Operations() {
			if slices.Contains(op.Tags, "ee") {
				want[[2]string{method, path}] = true
			}
		}
	}
	got := map[[2]string]bool{}
	for _, r := range api.EERoutes {
		got[r] = true
		if !want[r] {
			t.Errorf("api.EERoutes has %v, which is not an ee operation in the spec", r)
		}
	}
	for r := range want {
		if !got[r] {
			t.Errorf("spec ee operation %v is missing from api.EERoutes", r)
		}
	}
}

type observerExt struct {
	mu    sync.Mutex
	calls []ext.APICall
}

func (*observerExt) Name() string { return "observer" }
func (o *observerExt) Init(_ context.Context, h ext.Host) error {
	h.AddCallObserver(func(_ context.Context, c ext.APICall) {
		o.mu.Lock()
		o.calls = append(o.calls, c)
		o.mu.Unlock()
	})
	h.EnableFeatures("audit_log")
	return nil
}

func TestCallObserver(t *testing.T) {
	obs := &observerExt{}
	e := exttest.Start(t, []ext.Extension{obs}, exttest.Options{})
	owner, org := e.Setup(t, "owner@acme.io")
	var proj exttest.Obj
	owner.Do("POST", "/v1/projects", exttest.Obj{"org_id": org, "name": "Bot"}, 201, &proj)
	pid := proj["id"].(string)
	var ds exttest.Obj
	owner.Do("POST", "/v1/projects/"+pid+"/datasets", exttest.Obj{"name": "golden"}, 201, &ds)
	owner.Do("PATCH", "/v1/datasets/"+ds["id"].(string), exttest.Obj{"description": "x"}, 200, nil)
	owner.Do("GET", "/v1/projects/"+pid, nil, 200, nil) // reads are not observed

	var ed exttest.Obj
	e.Browser(t).Do("GET", "/v1/edition", nil, 200, &ed)
	if ed["edition"] != "enterprise" || ed["features"].([]any)[0] != "audit_log" {
		t.Fatalf("edition: %v", ed)
	}

	obs.mu.Lock()
	defer obs.mu.Unlock()
	var create, patch *ext.APICall
	for i := range obs.calls {
		switch obs.calls[i].Operation {
		case "createDataset":
			create = &obs.calls[i]
		case "updateDataset":
			patch = &obs.calls[i]
		case "getProject":
			t.Fatal("GET must not be observed")
		}
	}
	if create == nil || patch == nil {
		t.Fatalf("missing calls: %+v", obs.calls)
	}
	if create.OrgID.String() != org || create.ProjectID == nil || create.ProjectID.String() != pid {
		t.Fatalf("attribution: org %v project %v", create.OrgID, create.ProjectID)
	}
	if create.Status != 201 || create.CreatedID != ds["id"] || !strings.Contains(string(create.Body), "golden") {
		t.Fatalf("create call: %+v", create)
	}
	if patch.Route != "/v1/datasets/{dataset}" || patch.Params["dataset"] != ds["id"] || patch.Principal == nil {
		t.Fatalf("patch call: %+v", patch)
	}
}
