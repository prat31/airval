//go:build ee

package ee_test

import (
	"net/url"
	"strings"
	"testing"

	"github.com/prat31/airval/internal/ext/exttest"
)

func scimClient(t *testing.T, e *exttest.Env, owner *exttest.Client, org string) *exttest.Client {
	t.Helper()
	var tok obj
	owner.Do("POST", "/v1/orgs/"+org+"/scim-tokens", obj{"name": "Okta"}, 201, &tok)
	if !strings.HasPrefix(tok["token"].(string), "airvscim_") || tok["base_url"] != e.Server.URL+"/scim/v2" {
		t.Fatalf("token: %v", tok)
	}
	c := e.Browser(t)
	c.Bearer = tok["token"].(string)
	return c
}

func roleOf(t *testing.T, owner *exttest.Client, org, userID string) string {
	t.Helper()
	var ms obj
	owner.Do("GET", "/v1/orgs/"+org+"/members?limit=200", nil, 200, &ms)
	for _, m := range ms["items"].([]any) {
		if m.(obj)["user"].(obj)["id"] == userID {
			return m.(obj)["role"].(string)
		}
	}
	return ""
}

func TestSCIMUsersAndGroups(t *testing.T) {
	e := start(t)
	owner, org := e.Setup(t, "owner@acme.io")
	var analysts obj
	owner.Do("POST", "/v1/orgs/"+org+"/roles", obj{"name": "Analyst", "permissions": []string{"projects:read", "runs:read"}}, 201, &analysts)
	owner.Do("PUT", "/v1/orgs/"+org+"/scim-settings", obj{"default_role": "viewer", "group_roles": []obj{
		{"value": "Eval Admins", "role": "admin"}, {"value": "Eval Editors", "role": "editor"},
		{"value": "Analysts", "role": "custom:" + analysts["id"].(string)}}}, 200, nil)
	owner.Do("PUT", "/v1/orgs/"+org+"/scim-settings", obj{"default_role": "owner", "group_roles": []obj{}}, 422, nil)
	owner.Do("PUT", "/v1/orgs/"+org+"/scim-settings", obj{"default_role": "viewer", "group_roles": []obj{{"value": "x", "role": "owner"}}}, 422, nil)

	sc := scimClient(t, e, owner, org)

	// Auth.
	anon := e.Browser(t)
	anon.Do("GET", "/scim/v2/Users", nil, 401, nil)
	anon.Bearer = "airvscim_nope"
	anon.Do("GET", "/scim/v2/Users", nil, 401, nil)
	anon.Bearer = "airv_notscim"
	anon.Do("GET", "/scim/v2/Users", nil, 401, nil)

	sc.Do("GET", "/scim/v2/ServiceProviderConfig", nil, 200, nil)

	// Create.
	var u obj
	sc.Do("POST", "/scim/v2/Users", obj{"schemas": []string{"urn:ietf:params:scim:schemas:core:2.0:User"},
		"userName": "ada@acme.io", "externalId": "00u1", "name": obj{"givenName": "Ada", "familyName": "Lovelace"},
		"emails": []obj{{"value": "ada@acme.io", "primary": true}}, "active": true}, 201, &u)
	uid := u["id"].(string)
	if u["userName"] != "ada@acme.io" || u["active"] != true || u["meta"].(obj)["resourceType"] != "User" {
		t.Fatalf("user: %v", u)
	}
	if roleOf(t, owner, org, uid) != "viewer" {
		t.Fatal("provisioned user should get the default role")
	}
	sc.Do("POST", "/scim/v2/Users", obj{"userName": "ADA@acme.io"}, 409, nil)
	sc.Do("POST", "/scim/v2/Users", obj{"userName": "no-email"}, 400, nil)
	var bob obj
	sc.Do("POST", "/scim/v2/Users", obj{"userName": "bob@acme.io", "active": true}, 201, &bob)

	// Filter + paging.
	var l obj
	sc.Do("GET", "/scim/v2/Users?filter="+url.QueryEscape(`userName eq "ADA@acme.io"`), nil, 200, &l)
	if l["totalResults"].(float64) != 1 || l["Resources"].([]any)[0].(obj)["id"] != uid {
		t.Fatalf("filter: %v", l)
	}
	sc.Do("GET", "/scim/v2/Users?filter="+url.QueryEscape(`externalId eq "00u1"`), nil, 200, &l)
	if l["totalResults"].(float64) != 1 {
		t.Fatalf("externalId filter: %v", l)
	}
	sc.Do("GET", "/scim/v2/Users?startIndex=2&count=1", nil, 200, &l)
	if l["totalResults"].(float64) != 2 || l["itemsPerPage"].(float64) != 1 || l["startIndex"].(float64) != 2 {
		t.Fatalf("paging: %v", l)
	}
	sc.Do("GET", "/scim/v2/Users?filter="+url.QueryEscape(`title co "x"`), nil, 400, nil)

	// Groups map to roles; highest built-in wins.
	var g obj
	sc.Do("POST", "/scim/v2/Groups", obj{"displayName": "Eval Editors", "members": []obj{{"value": uid}}}, 201, &g)
	gid := g["id"].(string)
	if roleOf(t, owner, org, uid) != "editor" {
		t.Fatal("group role not applied")
	}
	sc.Do("POST", "/scim/v2/Groups", obj{"displayName": "Bad", "members": []obj{{"value": "not-a-user"}}}, 400, nil)
	var admins obj
	sc.Do("POST", "/scim/v2/Groups", obj{"displayName": "Eval Admins"}, 201, &admins)
	sc.Do("PATCH", "/scim/v2/Groups/"+admins["id"].(string), obj{"schemas": []string{"urn:ietf:params:scim:api:messages:2.0:PatchOp"},
		"Operations": []obj{{"op": "add", "path": "members", "value": []obj{{"value": uid}, {"value": bob["id"]}}}}}, 200, nil)
	if roleOf(t, owner, org, uid) != "admin" || roleOf(t, owner, org, bob["id"].(string)) != "admin" {
		t.Fatal("admin group role")
	}
	sc.Do("PATCH", "/scim/v2/Groups/"+admins["id"].(string), obj{"Operations": []obj{
		{"op": "remove", "path": `members[value eq "` + uid + `"]`}}}, 200, nil)
	if roleOf(t, owner, org, uid) != "editor" || roleOf(t, owner, org, bob["id"].(string)) != "admin" {
		t.Fatal("remove member should drop back to editor")
	}
	sc.Do("GET", "/scim/v2/Groups?filter="+url.QueryEscape(`displayName eq "eval editors"`), nil, 200, &l)
	if l["totalResults"].(float64) != 1 {
		t.Fatalf("group filter: %v", l)
	}
	sc.Do("GET", "/scim/v2/Users/"+uid, nil, 200, &u)
	if len(u["groups"].([]any)) != 1 {
		t.Fatalf("user groups: %v", u["groups"])
	}

	// Custom role from a group becomes an org-wide scim assignment.
	var an obj
	sc.Do("POST", "/scim/v2/Groups", obj{"displayName": "Analysts", "members": []obj{{"value": bob["id"]}}}, 201, &an)
	var as obj
	owner.Do("GET", "/v1/orgs/"+org+"/role-assignments?user_id="+bob["id"].(string), nil, 200, &as)
	if len(as["items"].([]any)) != 0 { // bob is admin via "Eval Admins": built-in wins
		t.Fatalf("assignments: %v", as)
	}
	sc.Do("DELETE", "/scim/v2/Groups/"+admins["id"].(string), nil, 204, nil)
	owner.Do("GET", "/v1/orgs/"+org+"/role-assignments?user_id="+bob["id"].(string), nil, 200, &as)
	if len(as["items"].([]any)) != 1 || as["items"].([]any)[0].(obj)["source"] != "scim" || roleOf(t, owner, org, bob["id"].(string)) != "viewer" {
		t.Fatalf("custom role via group: %v role=%s", as, roleOf(t, owner, org, bob["id"].(string)))
	}

	// Replace & patch users (Azure sends "False" strings).
	sc.Do("PUT", "/scim/v2/Users/"+uid, obj{"userName": "ada@acme.io", "displayName": "Ada L.", "active": true}, 200, &u)
	if u["displayName"] != "Ada L." {
		t.Fatalf("replace: %v", u)
	}
	adaBrowser := e.Browser(t)
	owner.Do("PATCH", "/v1/orgs/"+org+"/members/"+uid, obj{"role": "editor"}, 200, nil)
	_ = adaBrowser
	sc.Do("PATCH", "/scim/v2/Users/"+uid, obj{"Operations": []obj{{"op": "replace", "path": "active", "value": "False"}}}, 200, &u)
	if u["active"] != false || roleOf(t, owner, org, uid) != "" {
		t.Fatalf("deactivate: %v", u)
	}
	sc.Do("PATCH", "/scim/v2/Users/"+uid, obj{"Operations": []obj{{"op": "replace", "value": obj{"active": true}}}}, 200, &u)
	if roleOf(t, owner, org, uid) != "editor" {
		t.Fatal("reactivated user gets group role back")
	}

	// Delete deprovisions.
	sc.Do("DELETE", "/scim/v2/Users/"+uid, nil, 204, nil)
	sc.Do("GET", "/scim/v2/Users/"+uid, nil, 404, nil)
	if roleOf(t, owner, org, uid) != "" {
		t.Fatal("membership must be removed")
	}
	sc.Do("GET", "/scim/v2/Groups/"+gid, nil, 200, &g)
	if len(g["members"].([]any)) != 0 {
		t.Fatalf("group still lists deleted user: %v", g)
	}

	// Tokens are org-scoped and revocable; the last owner can't be deprovisioned.
	var ownerSCIM obj
	ownerID := me(t, owner)["user"].(obj)["id"].(string)
	sc.Do("POST", "/scim/v2/Users", obj{"userName": "owner@acme.io"}, 201, &ownerSCIM)
	sc.Do("PATCH", "/scim/v2/Users/"+ownerID, obj{"Operations": []obj{{"op": "replace", "path": "active", "value": false}}}, 409, nil)
	var toks obj
	owner.Do("GET", "/v1/orgs/"+org+"/scim-tokens", nil, 200, &toks)
	if strings.Contains(mustJSON(toks), sc.Bearer) {
		t.Fatal("token value must not be listed")
	}
	owner.Do("DELETE", "/v1/orgs/"+org+"/scim-tokens/"+toks["items"].([]any)[0].(obj)["id"].(string), nil, 204, nil)
	sc.Do("GET", "/scim/v2/Users", nil, 401, nil)

	var au obj
	owner.Do("GET", "/v1/orgs/"+org+"/audit-log?action=scim.user", nil, 200, &au)
	if len(au["items"].([]any)) < 5 || !strings.HasPrefix(au["items"].([]any)[0].(obj)["actor"].(string), "scim:") {
		t.Fatalf("scim audit: %v", au)
	}
}

func TestSCIMDeactivationBlocksSSO(t *testing.T) {
	f := setupOIDC(t, nil)
	sc := scimClient(t, f.e, f.owner, f.org)
	var u obj
	sc.Do("POST", "/scim/v2/Users", obj{"userName": "dan@acme.io", "active": true}, 201, &u)
	c, _ := f.login(t, map[string]any{"sub": "dan", "email": "dan@acme.io"}, "/")
	me(t, c)
	sc.Do("PATCH", "/scim/v2/Users/"+u["id"].(string), obj{"Operations": []obj{{"op": "replace", "path": "active", "value": false}}}, 200, nil)
	c.Do("GET", "/v1/auth/me", nil, 401, nil) // sessions revoked
	_, loc := f.login(t, map[string]any{"sub": "dan", "email": "dan@acme.io"}, "/")
	if loc != "/login?sso_error=deactivated" {
		t.Fatalf("deactivated user signed in again via JIT: %s", loc)
	}
}
