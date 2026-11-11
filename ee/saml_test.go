//go:build ee

package ee_test

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/xml"
	"html"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/crewjam/saml"

	"github.com/prat31/airval/internal/ext/exttest"
)

// fakeIdP is a crewjam/saml identity provider whose session (the signed-in user) is set by the test.
type fakeIdP struct {
	idp     *saml.IdentityProvider
	session *saml.Session
	spMeta  *saml.EntityDescriptor
}

func (f *fakeIdP) GetSession(http.ResponseWriter, *http.Request, *saml.IdpAuthnRequest) *saml.Session {
	return f.session
}

func (f *fakeIdP) GetServiceProvider(_ *http.Request, id string) (*saml.EntityDescriptor, error) {
	if f.spMeta == nil || f.spMeta.EntityID != id {
		return nil, os.ErrNotExist
	}
	return f.spMeta, nil
}

func newIdP(t *testing.T) *fakeIdP {
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "test idp"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour)}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, _ := x509.ParseCertificate(der)
	f := &fakeIdP{}
	base, _ := url.Parse("https://idp.example.test")
	f.idp = &saml.IdentityProvider{Key: key, Signer: key, Certificate: cert, MetadataURL: *base.JoinPath("metadata"),
		SSOURL: *base.JoinPath("sso"), ServiceProviderProvider: f, SessionProvider: f}
	return f
}

var formField = func(name string) *regexp.Regexp {
	return regexp.MustCompile(`name="` + name + `" value="([^"]*)"`)
}

// samlLogin runs SP-initiated SSO: /start → IdP → POST /acs. It returns the browser and the redirect.
func samlLogin(t *testing.T, e *exttest.Env, f *fakeIdP, slug string, tamper func(resp, relay string) (string, string)) (*exttest.Client, string) {
	t.Helper()
	c := e.Browser(t)
	resp, body := c.Raw("GET", "/v1/auth/saml/"+slug+"/start?redirect=/o/acme", nil)
	if resp.StatusCode != http.StatusFound || !strings.HasPrefix(resp.Header.Get("Location"), "https://idp.example.test/sso?") {
		t.Fatalf("start: %d %s %s", resp.StatusCode, resp.Header.Get("Location"), body)
	}
	rec := httptest.NewRecorder()
	f.idp.ServeSSO(rec, httptest.NewRequest("GET", resp.Header.Get("Location"), nil))
	out, _ := io.ReadAll(rec.Body)
	sr, rs := formField("SAMLResponse").FindSubmatch(out), formField("RelayState").FindSubmatch(out)
	if sr == nil || rs == nil {
		t.Fatalf("IdP did not answer with a POST form (%d): %s", rec.Code, out)
	}
	samlResp, relay := html.UnescapeString(string(sr[1])), html.UnescapeString(string(rs[1]))
	if tamper != nil {
		samlResp, relay = tamper(samlResp, relay)
	}
	form := url.Values{"SAMLResponse": {samlResp}, "RelayState": {relay}}
	req, _ := http.NewRequest("POST", e.Server.URL+"/v1/auth/saml/"+slug+"/acs", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r2, err := c.HC.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	r2.Body.Close()
	if r2.StatusCode != http.StatusFound {
		t.Fatalf("acs: %d", r2.StatusCode)
	}
	return c, r2.Header.Get("Location")
}

func TestSAMLLogin(t *testing.T) {
	e := start(t)
	owner, org := e.Setup(t, "owner@acme.io")
	f := newIdP(t)
	md, _ := xml.Marshal(f.idp.Metadata())
	var p obj
	owner.Do("POST", "/v1/orgs/"+org+"/sso-providers", obj{"kind": "saml", "name": "Entra", "slug": "entra", "config": obj{
		"idp_metadata_xml": string(md), "jit_provisioning": true, "role_claim": "eduPersonAffiliation",
		"role_mapping": []obj{{"value": "evaluators", "role": "editor"}}}}, 201, &p)
	if p["entity_id"] != e.Server.URL+"/v1/auth/saml/entra/metadata" || p["redirect_uri"] != e.Server.URL+"/v1/auth/saml/entra/acs" {
		t.Fatalf("provider: %v", p)
	}

	// SP metadata is public and parses.
	var spMeta string
	e.Browser(t).Do("GET", "/v1/auth/saml/entra/metadata", nil, 200, &spMeta)
	f.spMeta = &saml.EntityDescriptor{}
	if err := xml.Unmarshal([]byte(spMeta), f.spMeta); err != nil {
		t.Fatalf("metadata: %v\n%s", err, spMeta)
	}

	f.session = &saml.Session{ID: "s1", NameID: "grace@acme.io", UserEmail: "grace@acme.io", UserCommonName: "Grace Hopper",
		Groups: []string{"evaluators"}, CreateTime: time.Now(), ExpireTime: time.Now().Add(time.Hour)}
	c, loc := samlLogin(t, e, f, "entra", nil)
	if loc != "/o/acme" {
		t.Fatalf("location: %s", loc)
	}
	m := me(t, c)
	if m["user"].(obj)["email"] != "grace@acme.io" || m["orgs"].([]any)[0].(obj)["role"] != "editor" {
		t.Fatalf("me: %v", m)
	}

	t.Run("relay state is single use", func(t *testing.T) {
		var saved string
		samlLogin(t, e, f, "entra", func(r, rs string) (string, string) { saved = rs; return r, rs })
		_, loc := samlLogin(t, e, f, "entra", func(r, _ string) (string, string) { return r, saved })
		if loc != "/login?sso_error=state_expired" {
			t.Fatalf("location: %s", loc)
		}
	})
	t.Run("tampered assertion", func(t *testing.T) {
		_, loc := samlLogin(t, e, f, "entra", func(r, rs string) (string, string) {
			return strings.Replace(r, "A", "B", 1), rs
		})
		if loc != "/login?sso_error=invalid_assertion" {
			t.Fatalf("location: %s", loc)
		}
	})
	t.Run("forged relay state", func(t *testing.T) {
		_, loc := samlLogin(t, e, f, "entra", func(r, _ string) (string, string) { return r, "forged" })
		if loc != "/login?sso_error=state_expired" {
			t.Fatalf("location: %s", loc)
		}
	})
	t.Run("response signed by another IdP", func(t *testing.T) {
		other := newIdP(t)
		other.spMeta, other.session = f.spMeta, f.session
		c := e.Browser(t)
		resp, _ := c.Raw("GET", "/v1/auth/saml/entra/start", nil)
		rec := httptest.NewRecorder()
		other.idp.ServeSSO(rec, httptest.NewRequest("GET", resp.Header.Get("Location"), nil))
		out, _ := io.ReadAll(rec.Body)
		sr, rs := formField("SAMLResponse").FindSubmatch(out), formField("RelayState").FindSubmatch(out)
		if sr == nil {
			t.Fatalf("no form: %s", out)
		}
		form := url.Values{"SAMLResponse": {html.UnescapeString(string(sr[1]))}, "RelayState": {html.UnescapeString(string(rs[1]))}}
		r2, err := c.HC.PostForm(e.Server.URL+"/v1/auth/saml/entra/acs", form)
		if err != nil {
			t.Fatal(err)
		}
		r2.Body.Close()
		if r2.Header.Get("Location") != "/login?sso_error=invalid_assertion" {
			t.Fatalf("location: %s", r2.Header.Get("Location"))
		}
	})
}
