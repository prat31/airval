//go:build ee

package ee_test

import (
	"slices"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"

	"github.com/prat31/airval/ee/roles"
	"github.com/prat31/airval/internal/store"
)

// Every operation in the spec must be classified for custom roles: mapped to a permission, an admin
// operation (built-in role only), public, or enterprise administration.
func TestEveryOperationIsClassified(t *testing.T) {
	doc, err := openapi3.NewLoader().LoadFromFile("../api/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	for path, item := range doc.Paths.Map() {
		for method, op := range item.Operations() {
			if slices.Contains(op.Tags, "ee") || (op.Security != nil && len(*op.Security) == 0) || path == "/hooks/{trigger}" {
				continue
			}
			if _, ok := roles.PermissionFor(op.OperationID); !ok && !roles.IsAdminOp(op.OperationID) {
				t.Errorf("%s %s (%s) has no permission mapping", method, path, op.OperationID)
			}
		}
	}
}

func TestPermissionTiers(t *testing.T) {
	for _, r := range []store.Role{store.RoleViewer, store.RoleEditor, store.RoleAdmin} {
		if got := roles.Tier(roles.Builtin(r)); got != r {
			t.Errorf("Tier(Builtin(%s)) = %s", r, got)
		}
	}
	if roles.Tier([]string{"runs:read"}) != "" {
		t.Error("a narrow role has no built-in tier")
	}
	if _, bad := roles.Normalize([]string{"runs:read", "members:write"}); bad != "members:write" {
		t.Error("admin permissions must not be grantable")
	}
	got, _ := roles.Normalize([]string{"runs:execute", "projects:read", "runs:execute"})
	if !slices.Equal(got, []string{"projects:read", "runs:execute"}) {
		t.Errorf("normalize: %v", got)
	}
}

// TestRBACMatrix exercises custom roles and per-project overrides end to end through the API.
func TestRBACMatrix(t *testing.T) {
	e := start(t)
	owner, org := e.Setup(t, "owner@acme.io")
	admin, _ := e.Member(t, owner, org, "admin@acme.io", "admin")
	var p1, p2 obj
	owner.Do("POST", "/v1/projects", obj{"org_id": org, "name": "P1"}, 201, &p1)
	owner.Do("POST", "/v1/projects", obj{"org_id": org, "name": "P2"}, 201, &p2)
	P1, P2 := p1["id"].(string), p2["id"].(string)

	// Role catalog.
	var cat obj
	admin.Do("GET", "/v1/orgs/"+org+"/roles", nil, 200, &cat)
	if len(cat["permissions"].([]any)) != len(roles.Catalog) || len(cat["builtin"].([]any)) != 4 {
		t.Fatalf("catalog: %v", cat)
	}

	var runner, curator obj
	admin.Do("POST", "/v1/orgs/"+org+"/roles", obj{"name": "Runner", "permissions": []string{"projects:read", "datasets:read", "runs:read", "runs:execute"}}, 201, &runner)
	admin.Do("POST", "/v1/orgs/"+org+"/roles", obj{"name": "Curator", "permissions": []string{"projects:read", "datasets:read", "datasets:write"}}, 201, &curator)
	admin.Do("POST", "/v1/orgs/"+org+"/roles", obj{"name": "Bad", "permissions": []string{"members:write"}}, 422, nil)
	admin.Do("POST", "/v1/orgs/"+org+"/roles", obj{"name": "Admin", "permissions": []string{}}, 422, nil)
	admin.Do("POST", "/v1/orgs/"+org+"/roles", obj{"name": "Runner", "permissions": []string{}}, 409, nil)

	cr, crID := e.Member(t, owner, org, "cr@acme.io", "viewer")
	ov, ovID := e.Member(t, owner, org, "ov@acme.io", "viewer")
	ed, edID := e.Member(t, owner, org, "ed@acme.io", "editor")

	assign := func(user string, project any, role string, want int) {
		t.Helper()
		admin.Do("PUT", "/v1/orgs/"+org+"/role-assignments", obj{"user_id": user, "project_id": project, "role": role}, want, nil)
	}
	assign(crID, nil, "custom:"+runner["id"].(string), 200)
	assign(crID, P1, "custom:"+curator["id"].(string), 200)
	assign(ovID, P2, "editor", 200)
	assign(edID, P1, "viewer", 200)  // per-project restriction
	assign(ovID, nil, "editor", 422) // org-wide built-in roles live on the membership
	assign(ovID, P1, "owner", 422)
	ownerMe := me(t, owner)
	assign(ownerMe["user"].(obj)["id"].(string), P1, "viewer", 422) // owners are never restricted
	editorClient := ed
	editorClient.Do("PUT", "/v1/orgs/"+org+"/role-assignments", obj{"user_id": ovID, "role": "custom:" + runner["id"].(string)}, 403, nil)

	type check struct {
		who    string
		method string
		path   string
		body   any
		want   int
	}
	clients := map[string]interface {
		Do(string, string, any, int, any)
	}{"cr": cr, "ov": ov, "ed": ed}
	ds := func(p string) string { return "/v1/projects/" + p + "/datasets" }
	matrix := []check{
		// cr: org-wide Runner (no datasets:write), Curator on P1.
		{"cr", "GET", "/v1/projects/" + P2, nil, 200},
		{"cr", "GET", ds(P2), nil, 200},
		{"cr", "POST", ds(P2), obj{"name": "x"}, 403},
		{"cr", "POST", ds(P1), obj{"name": "cr-p1"}, 201},
		{"cr", "GET", "/v1/projects/" + P1 + "/evaluators", nil, 403},                                   // Curator lacks evaluators:read
		{"cr", "GET", "/v1/projects/" + P2 + "/evaluators", nil, 403},                                   // Runner lacks evaluators:read
		{"cr", "POST", "/v1/orgs/" + org + "/members", obj{"email": "z@acme.io", "role": "admin"}, 403}, // admin ops: base role
		{"cr", "POST", "/v1/orgs/" + org + "/api-keys", obj{"name": "k", "role": "admin"}, 403},
		// ov: viewer, editor on P2.
		{"ov", "POST", ds(P2), obj{"name": "ov-p2"}, 201},
		{"ov", "POST", ds(P1), obj{"name": "x"}, 403},
		{"ov", "GET", ds(P1), nil, 200},
		// ed: editor, viewer on P1.
		{"ed", "POST", ds(P1), obj{"name": "x"}, 403},
		{"ed", "POST", ds(P2), obj{"name": "ed-p2"}, 201},
	}
	for _, c := range matrix {
		clients[c.who].Do(c.method, c.path, c.body, c.want, nil)
	}

	// Listing assignments, then deleting the custom role drops its assignments.
	var as obj
	admin.Do("GET", "/v1/orgs/"+org+"/role-assignments?user_id="+crID, nil, 200, &as)
	if len(as["items"].([]any)) != 2 {
		t.Fatalf("assignments: %v", as)
	}
	admin.Do("DELETE", "/v1/orgs/"+org+"/roles/"+curator["id"].(string), nil, 204, nil)
	cr.Do("POST", ds(P1), obj{"name": "y"}, 403, nil) // back to org-wide Runner
	admin.Do("DELETE", "/v1/orgs/"+org+"/role-assignments?user_id="+crID, nil, 204, nil)
	cr.Do("GET", "/v1/projects/"+P1+"/evaluators", nil, 200, nil) // back to viewer
	admin.Do("DELETE", "/v1/orgs/"+org+"/role-assignments?user_id="+ovID+"&project_id="+P2, nil, 204, nil)
	ov.Do("POST", ds(P2), obj{"name": "z"}, 403, nil)

	// Removing a member removes their assignments.
	owner.Do("DELETE", "/v1/orgs/"+org+"/members/"+edID, nil, 204, nil)
	admin.Do("GET", "/v1/orgs/"+org+"/role-assignments?user_id="+edID, nil, 200, &as)
	if len(as["items"].([]any)) != 0 {
		t.Fatalf("stale assignments: %v", as)
	}
	// Role administration is audited.
	var au obj
	owner.Do("GET", "/v1/orgs/"+org+"/audit-log?action=role", nil, 200, &au)
	if len(au["items"].([]any)) < 5 {
		t.Fatalf("role audit entries: %d", len(au["items"].([]any)))
	}
}
