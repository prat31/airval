//go:build ee

package ee_test

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/google/uuid"

	"github.com/prat31/airval/ee"
	"github.com/prat31/airval/internal/ext"
	"github.com/prat31/airval/internal/ext/exttest"
)

type obj = exttest.Obj

var jsonMarshal = json.Marshal

func mustUUID(s string) uuid.UUID { return uuid.MustParse(s) }

func start(t *testing.T) *exttest.Env {
	t.Helper()
	return exttest.Start(t, []ext.Extension{ee.Module{}}, exttest.Options{})
}

// fakeIssuer is an in-process OIDC provider: discovery, JWKS, an authorization step driven by the test
// and a token endpoint that checks the client secret and PKCE before minting a signed ID token.
type fakeIssuer struct {
	t      *testing.T
	srv    *httptest.Server
	key    *rsa.PrivateKey
	secret string

	mu    sync.Mutex
	codes map[string]pendingCode
	// Hooks to misbehave.
	claims    func(c map[string]any) // mutate ID token claims
	signWith  *rsa.PrivateKey        // sign with another key
	lastToken url.Values
}

type pendingCode struct {
	clientID, redirect, challenge, nonce string
	claims                               map[string]any
}

func newIssuer(t *testing.T, secret string) *fakeIssuer {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeIssuer{t: t, key: key, secret: secret, codes: map[string]pendingCode{}}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"issuer": f.srv.URL, "authorization_endpoint": f.srv.URL + "/authorize",
			"token_endpoint": f.srv.URL + "/token", "jwks_uri": f.srv.URL + "/jwks",
			"id_token_signing_alg_values_supported": []string{"RS256"}, "response_types_supported": []string{"code"},
			"subject_types_supported": []string{"public"}})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &key.PublicKey, KeyID: "k1", Algorithm: "RS256", Use: "sig"}}})
	})
	mux.HandleFunc("/token", f.token)
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

// authorize plays the user's visit to the IdP: it validates the authorization request and returns the
// callback URL (relative to the airval server) with a fresh code.
func (f *fakeIssuer) authorize(location string, claims map[string]any) string {
	f.t.Helper()
	u, err := url.Parse(location)
	if err != nil || !strings.HasPrefix(location, f.srv.URL+"/authorize") {
		f.t.Fatalf("unexpected IdP redirect %q", location)
	}
	q := u.Query()
	for _, k := range []string{"client_id", "redirect_uri", "state", "nonce", "code_challenge"} {
		if q.Get(k) == "" {
			f.t.Fatalf("authorization request without %s: %s", k, location)
		}
	}
	if q.Get("code_challenge_method") != "S256" || q.Get("response_type") != "code" {
		f.t.Fatalf("authorization request must use code + PKCE S256: %s", location)
	}
	if !strings.Contains(q.Get("scope"), "openid") {
		f.t.Fatalf("scope must include openid: %s", q.Get("scope"))
	}
	code := base64.RawURLEncoding.EncodeToString([]byte(time.Now().String()))
	f.mu.Lock()
	f.codes[code] = pendingCode{clientID: q.Get("client_id"), redirect: q.Get("redirect_uri"), challenge: q.Get("code_challenge"),
		nonce: q.Get("nonce"), claims: claims}
	f.mu.Unlock()
	cb, _ := url.Parse(q.Get("redirect_uri"))
	return cb.Path + "?code=" + url.QueryEscape(code) + "&state=" + url.QueryEscape(q.Get("state"))
}

func (f *fakeIssuer) token(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	f.mu.Lock()
	f.lastToken = r.PostForm
	pc, ok := f.codes[r.PostForm.Get("code")]
	delete(f.codes, r.PostForm.Get("code"))
	f.mu.Unlock()
	id, secret, basic := r.BasicAuth()
	if !basic {
		id, secret = r.PostForm.Get("client_id"), r.PostForm.Get("client_secret")
	}
	sum := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
	switch {
	case !ok:
		http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
		return
	case id != pc.clientID || secret != f.secret:
		http.Error(w, `{"error":"invalid_client"}`, http.StatusUnauthorized)
		return
	case base64.RawURLEncoding.EncodeToString(sum[:]) != pc.challenge:
		http.Error(w, `{"error":"invalid_grant","error_description":"PKCE verification failed"}`, http.StatusBadRequest)
		return
	case r.PostForm.Get("redirect_uri") != pc.redirect:
		http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
		return
	}
	now := time.Now()
	claims := map[string]any{"iss": f.srv.URL, "aud": pc.clientID, "iat": now.Unix(), "exp": now.Add(5 * time.Minute).Unix(),
		"nonce": pc.nonce}
	for k, v := range pc.claims {
		claims[k] = v
	}
	if f.claims != nil {
		f.claims(claims)
	}
	key := f.key
	if f.signWith != nil {
		key = f.signWith
	}
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: jose.JSONWebKey{Key: key, KeyID: "k1"}},
		(&jose.SignerOptions{}).WithType("JWT"))
	if err != nil {
		f.t.Fatal(err)
	}
	payload, _ := json.Marshal(claims)
	jws, err := signer.Sign(payload)
	if err != nil {
		f.t.Fatal(err)
	}
	idt, _ := jws.CompactSerialize()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "at", "token_type": "Bearer", "expires_in": 300, "id_token": idt})
}
