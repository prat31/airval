//go:build ee

package ee_test

import (
	"crypto/rand"
	"crypto/rsa"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/prat31/airval/internal/ext/exttest"
	"github.com/prat31/airval/internal/store"
)

type oidcFixture struct {
	e       *exttest.Env
	iss     *fakeIssuer
	owner   *exttest.Client
	org     string
	provID  string
	slug    string
	adminRk string
}

func setupOIDC(t *testing.T, cfg obj) *oidcFixture {
	t.Helper()
	e := start(t)
	owner, org := e.Setup(t, "owner@acme.io")
	iss := newIssuer(t, "s3cret")
	base := obj{"issuer": iss.srv.URL, "client_id": "airval-web", "allowed_domains": []string{"acme.io"},
		"role_claim": "groups", "role_mapping": []obj{{"value": "eval-admins", "role": "admin"}, {"value": "eval-editors", "role": "editor"}},
		"default_role": "viewer", "jit_provisioning": true}
	for k, v := range cfg {
		base[k] = v
	}
	var p obj
	owner.Do("POST", "/v1/orgs/"+org+"/sso-providers", obj{"kind": "oidc", "name": "Okta", "slug": "okta", "config": base,
		"client_secret": "s3cret"}, 201, &p)
	return &oidcFixture{e: e, iss: iss, owner: owner, org: org, provID: p["id"].(string), slug: "okta"}
}

// login runs the browser flow and returns the client and the final redirect location.
func (f *oidcFixture) login(t *testing.T, claims map[string]any, redirect string) (*exttest.Client, string) {
	t.Helper()
	c := f.e.Browser(t)
	resp, body := c.Raw("GET", "/v1/auth/oidc/"+f.slug+"/start?redirect="+url.QueryEscape(redirect), nil)
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("start: %d %s", resp.StatusCode, body)
	}
	cb := f.iss.authorize(resp.Header.Get("Location"), claims)
	resp, body = c.Raw("GET", cb, nil)
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("callback: %d %s", resp.StatusCode, body)
	}
	return c, resp.Header.Get("Location")
}

func me(t *testing.T, c *exttest.Client) obj {
	t.Helper()
	var m obj
	c.Do("GET", "/v1/auth/me", nil, 200, &m)
	c.CSRF, _ = m["csrf_token"].(string)
	return m
}

func TestOIDCLoginJITAndRoleMapping(t *testing.T) {
	f := setupOIDC(t, nil)

	// Public provider listing for the sign-in page.
	var ps obj
	f.e.Browser(t).Do("GET", "/v1/auth/providers", nil, 200, &ps)
	items := ps["items"].([]any)
	if len(items) != 1 || items[0].(obj)["start_url"] != "/v1/auth/oidc/okta/start" || items[0].(obj)["kind"] != "oidc" {
		t.Fatalf("providers: %v", ps)
	}

	// The client secret is never returned.
	var p obj
	f.owner.Do("GET", "/v1/orgs/"+f.org+"/sso-providers/"+f.provID, nil, 200, &p)
	if p["has_client_secret"] != true || strings.Contains(mustJSON(p), "s3cret") {
		t.Fatalf("provider leaks or lacks secret: %v", p)
	}
	if p["redirect_uri"] != f.e.Server.URL+"/v1/auth/oidc/okta/callback" {
		t.Fatalf("redirect_uri: %v", p["redirect_uri"])
	}

	c, loc := f.login(t, map[string]any{"sub": "u-1", "email": "Ada@Acme.io", "email_verified": true, "name": "Ada",
		"groups": []string{"eval-editors", "eval-admins"}}, "/o/acme/p/x/runs?tab=1")
	if loc != "/o/acme/p/x/runs?tab=1" {
		t.Fatalf("redirect: %s", loc)
	}
	m := me(t, c)
	if m["user"].(obj)["email"] != "ada@acme.io" {
		t.Fatalf("me: %v", m)
	}
	if role := m["orgs"].([]any)[0].(obj)["role"]; role != "admin" {
		t.Fatalf("mapped role: %v", role)
	}
	// PKCE verifier and confidential client auth were used.
	if f.iss.lastToken.Get("code_verifier") == "" {
		t.Fatal("no code_verifier sent")
	}

	// Second login with the same subject reuses the user; without sync_roles the role is kept.
	c2, _ := f.login(t, map[string]any{"sub": "u-1", "email": "ada@acme.io", "groups": []string{}}, "/")
	if r := me(t, c2)["orgs"].([]any)[0].(obj)["role"]; r != "admin" {
		t.Fatalf("role changed without sync_roles: %v", r)
	}

	// The login is audited.
	var au obj
	f.owner.Do("GET", "/v1/orgs/"+f.org+"/audit-log?action=auth.sso_login", nil, 200, &au)
	if len(au["items"].([]any)) != 2 {
		t.Fatalf("sso login audit: %v", au)
	}
}

func mustJSON(v any) string {
	b, _ := jsonMarshal(v)
	return string(b)
}

func TestOIDCSecurityChecks(t *testing.T) {
	f := setupOIDC(t, nil)
	good := func() map[string]any {
		return map[string]any{"sub": "u-2", "email": "bob@acme.io", "email_verified": true}
	}
	expectErr := func(name, code string, claims map[string]any, mutate func()) {
		t.Helper()
		t.Run(name, func(t *testing.T) {
			if mutate != nil {
				mutate()
				defer func() { f.iss.claims, f.iss.signWith = nil, nil }()
			}
			c, loc := f.login(t, claims, "/")
			if loc != "/login?sso_error="+code {
				t.Fatalf("location %q, want sso_error=%s", loc, code)
			}
			c.Do("GET", "/v1/auth/me", nil, 401, nil)
		})
	}
	other, _ := rsa.GenerateKey(rand.Reader, 2048)
	expectErr("wrong signing key", "invalid_token", good(), func() { f.iss.signWith = other })
	expectErr("wrong audience", "invalid_token", good(), func() { f.iss.claims = func(c map[string]any) { c["aud"] = "someone-else" } })
	expectErr("wrong issuer", "invalid_token", good(), func() { f.iss.claims = func(c map[string]any) { c["iss"] = "https://evil.example" } })
	expectErr("expired", "invalid_token", good(), func() {
		f.iss.claims = func(c map[string]any) { c["exp"] = int64(1000) }
	})
	expectErr("nonce replay", "nonce_mismatch", good(), func() { f.iss.claims = func(c map[string]any) { c["nonce"] = "attacker" } })
	expectErr("unverified email", "email_unverified", map[string]any{"sub": "u-3", "email": "eve@acme.io", "email_verified": false}, nil)
	expectErr("domain not allowed", "domain_not_allowed", map[string]any{"sub": "u-4", "email": "eve@evil.io"}, nil)
	expectErr("no email", "no_email", map[string]any{"sub": "u-5"}, nil)

	t.Run("state mismatch", func(t *testing.T) {
		c := f.e.Browser(t)
		resp, _ := c.Raw("GET", "/v1/auth/oidc/okta/start", nil)
		cb := f.iss.authorize(resp.Header.Get("Location"), good())
		u, _ := url.Parse(cb)
		q := u.Query()
		q.Set("state", "forged")
		resp, _ = c.Raw("GET", u.Path+"?"+q.Encode(), nil)
		if resp.Header.Get("Location") != "/login?sso_error=state_mismatch" {
			t.Fatalf("location %q", resp.Header.Get("Location"))
		}
	})
	t.Run("callback without the state cookie (login CSRF)", func(t *testing.T) {
		victim := f.e.Browser(t)
		attacker := f.e.Browser(t)
		resp, _ := attacker.Raw("GET", "/v1/auth/oidc/okta/start", nil)
		cb := f.iss.authorize(resp.Header.Get("Location"), good())
		resp, _ = victim.Raw("GET", cb, nil)
		if resp.Header.Get("Location") != "/login?sso_error=state_missing" {
			t.Fatalf("location %q", resp.Header.Get("Location"))
		}
	})
	t.Run("open redirect is neutralized", func(t *testing.T) {
		for _, bad := range []string{"//evil.example/x", "https://evil.example", `/\evil.example`, "javascript:alert(1)"} {
			_, loc := f.login(t, good(), bad)
			if loc != "/" {
				t.Fatalf("redirect %q -> %q", bad, loc)
			}
		}
	})
	t.Run("disabled provider", func(t *testing.T) {
		f.owner.Do("PATCH", "/v1/orgs/"+f.org+"/sso-providers/"+f.provID, obj{"enabled": false}, 200, nil)
		defer f.owner.Do("PATCH", "/v1/orgs/"+f.org+"/sso-providers/"+f.provID, obj{"enabled": true}, 200, nil)
		resp, _ := f.e.Browser(t).Raw("GET", "/v1/auth/oidc/okta/start", nil)
		if resp.Header.Get("Location") != "/login?sso_error=unknown_provider" {
			t.Fatalf("location %q", resp.Header.Get("Location"))
		}
		var ps obj
		f.e.Browser(t).Do("GET", "/v1/auth/providers", nil, 200, &ps)
		if len(ps["items"].([]any)) != 0 {
			t.Fatal("disabled provider listed")
		}
	})
}

func TestOIDCProvisioningPolicies(t *testing.T) {
	f := setupOIDC(t, obj{"jit_provisioning": false, "sync_roles": true})
	// Unknown users are rejected without JIT.
	_, loc := f.login(t, map[string]any{"sub": "x", "email": "new@acme.io"}, "/")
	if loc != "/login?sso_error=not_provisioned" {
		t.Fatalf("location %q", loc)
	}
	// Invited members link by verified email; sync_roles applies the mapping...
	_, uid := f.e.Member(t, f.owner, f.org, "carol@acme.io", "viewer")
	c, _ := f.login(t, map[string]any{"sub": "carol-sub", "email": "carol@acme.io", "groups": []string{"eval-editors"}}, "/")
	if r := me(t, c)["orgs"].([]any)[0].(obj)["role"]; r != "editor" {
		t.Fatalf("synced role: %v", r)
	}
	ident, err := f.e.Store.GetSSOIdentity(t.Context(), mustUUID(f.provID), "carol-sub")
	if err != nil || ident.UserID.String() != uid {
		t.Fatalf("identity link: %v %v", ident, err)
	}
	// ...but never demotes an owner.
	c, _ = f.login(t, map[string]any{"sub": "owner-sub", "email": "owner@acme.io", "groups": []string{"eval-editors"}}, "/")
	if r := me(t, c)["orgs"].([]any)[0].(obj)["role"]; r != string(store.RoleOwner) {
		t.Fatalf("owner demoted to %v", r)
	}
}

func TestSSOProviderAdmin(t *testing.T) {
	e := start(t)
	owner, org := e.Setup(t, "owner@acme.io")
	admin, _ := e.Member(t, owner, org, "admin@acme.io", "admin")
	cfg := obj{"issuer": "https://idp.example", "client_id": "c"}
	// Only owners configure SSO.
	admin.Do("POST", "/v1/orgs/"+org+"/sso-providers", obj{"kind": "oidc", "name": "X", "config": cfg}, 403, nil)
	// Validation.
	owner.Do("POST", "/v1/orgs/"+org+"/sso-providers", obj{"kind": "oidc", "name": "X", "config": obj{"issuer": "ftp://x", "client_id": "c"}}, 422, nil)
	owner.Do("POST", "/v1/orgs/"+org+"/sso-providers", obj{"kind": "oidc", "name": "X",
		"config": obj{"issuer": "https://idp.example", "client_id": "c", "role_mapping": []obj{{"value": "g", "role": "owner"}}}}, 422, nil)
	owner.Do("POST", "/v1/orgs/"+org+"/sso-providers", obj{"kind": "saml", "name": "X", "config": obj{}}, 422, nil)
	var p obj
	owner.Do("POST", "/v1/orgs/"+org+"/sso-providers", obj{"kind": "oidc", "name": "Google Workspace", "config": cfg}, 201, &p)
	if p["slug"] != "google-workspace" || p["has_client_secret"] != false {
		t.Fatalf("provider: %v", p)
	}
	owner.Do("POST", "/v1/orgs/"+org+"/sso-providers", obj{"kind": "oidc", "name": "Google Workspace", "config": cfg}, 409, nil)
	// Admins can read; another org can't see it.
	var list obj
	admin.Do("GET", "/v1/orgs/"+org+"/sso-providers", nil, 200, &list)
	if len(list["items"].([]any)) != 1 {
		t.Fatal("list")
	}
	var other obj
	owner.Do("POST", "/v1/orgs", obj{"name": "Other"}, 201, &other)
	owner.Do("GET", "/v1/orgs/"+other["id"].(string)+"/sso-providers/"+p["id"].(string), nil, 404, nil)
	owner.Do("DELETE", "/v1/orgs/"+org+"/sso-providers/"+p["id"].(string), nil, 204, nil)
	var au obj
	owner.Do("GET", "/v1/orgs/"+org+"/audit-log?action=sso_provider.", nil, 200, &au)
	if len(au["items"].([]any)) != 2 {
		t.Fatalf("audit: %v", au)
	}
}
