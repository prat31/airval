//go:build ee

package sso

import (
	"context"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"github.com/prat31/airval/ee/internal/httpx"
	"github.com/prat31/airval/internal/store"
)

const (
	stateCookie = "airval_sso_state"
	stateTTL    = 10 * time.Minute
)

type cachedOIDC struct {
	p   *oidc.Provider
	exp time.Time
}

// oidcState round-trips through a sealed (AES-GCM, keyring) HttpOnly cookie between start and callback.
type oidcState struct {
	Provider string `json:"p"`
	State    string `json:"s"`
	Nonce    string `json:"n"`
	Verifier string `json:"v"`
	Redirect string `json:"r"`
	Expires  int64  `json:"e"`
}

func (s *Service) discover(ctx context.Context, issuer string) (*oidc.Provider, error) {
	s.mu.Lock()
	c, ok := s.oidcCache[issuer]
	s.mu.Unlock()
	if ok && s.now().Before(c.exp) {
		return c.p, nil
	}
	p, err := oidc.NewProvider(oidc.ClientContext(ctx, s.client), issuer)
	if err != nil {
		return nil, fmt.Errorf("oidc discovery for %s: %w", issuer, err)
	}
	s.mu.Lock()
	s.oidcCache[issuer] = cachedOIDC{p: p, exp: s.now().Add(time.Hour)}
	s.mu.Unlock()
	return p, nil
}

func (s *Service) oauthConfig(ctx context.Context, sp store.SSOProvider) (*oauth2.Config, *oidc.Provider, error) {
	cfg := ParseConfig(sp.Config)
	p, err := s.discover(ctx, cfg.Issuer)
	if err != nil {
		return nil, nil, err
	}
	var secret string
	if len(sp.Secret) > 0 {
		b, err := s.h.Host.Keyring().Unseal(sp.Secret, sp.KeyVersion, s.secretAAD(sp.ID))
		if err != nil {
			return nil, nil, fmt.Errorf("decrypt client secret: %w", err)
		}
		secret = string(b)
	}
	return &oauth2.Config{ClientID: cfg.ClientID, ClientSecret: secret, Endpoint: p.Endpoint(), Scopes: cfg.scopes(),
		RedirectURL: s.baseURL() + "/v1/auth/oidc/" + sp.Slug + "/callback"}, p, nil
}

func (s *Service) stateAAD() []byte { return []byte("airval-sso-state") }

func (s *Service) oidcStart(w http.ResponseWriter, r *http.Request) {
	sp, err := s.enabledProvider(r, "oidc")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	oc, _, err := s.oauthConfig(r.Context(), sp)
	if err != nil {
		s.fail(w, r, failf("provider_unavailable", "%v", err))
		return
	}
	st := oidcState{Provider: sp.Slug, State: randomToken(24), Nonce: randomToken(24), Verifier: oauth2.GenerateVerifier(),
		Redirect: httpx.SafeRedirect(r.URL.Query().Get("redirect")), Expires: s.now().Add(stateTTL).Unix()}
	raw, _ := json.Marshal(st)
	sealed, ver, err := s.h.Host.Keyring().Seal(raw, s.stateAAD())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	cfg := s.h.Host.Config()
	secure := cfg != nil && cfg.Auth.CookieSecure
	http.SetCookie(w, &http.Cookie{Name: stateCookie, Value: fmt.Sprintf("%d.%s", ver, base64.RawURLEncoding.EncodeToString(sealed)),
		Path: "/v1/auth/", MaxAge: int(stateTTL.Seconds()), HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode})
	http.Redirect(w, r, oc.AuthCodeURL(st.State, oidc.Nonce(st.Nonce), oauth2.S256ChallengeOption(st.Verifier)), http.StatusFound)
}

func (s *Service) readState(r *http.Request) (oidcState, error) {
	var st oidcState
	c, err := r.Cookie(stateCookie)
	if err != nil {
		return st, failf("state_missing", "sign-in session expired; try again")
	}
	verStr, data, ok := strings.Cut(c.Value, ".")
	var ver int
	if _, err := fmt.Sscanf(verStr, "%d", &ver); !ok || err != nil {
		return st, failf("state_invalid", "invalid sign-in state")
	}
	blob, err := base64.RawURLEncoding.DecodeString(data)
	if err != nil {
		return st, failf("state_invalid", "invalid sign-in state")
	}
	raw, err := s.h.Host.Keyring().Unseal(blob, ver, s.stateAAD())
	if err != nil || json.Unmarshal(raw, &st) != nil {
		return st, failf("state_invalid", "invalid sign-in state")
	}
	if s.now().Unix() > st.Expires {
		return st, failf("state_expired", "sign-in took too long; try again")
	}
	return st, nil
}

func (s *Service) oidcCallback(w http.ResponseWriter, r *http.Request) {
	// The state cookie is single-use.
	cfg := s.h.Host.Config()
	http.SetCookie(w, &http.Cookie{Name: stateCookie, Value: "", Path: "/v1/auth/", MaxAge: -1, HttpOnly: true,
		Secure: cfg != nil && cfg.Auth.CookieSecure, SameSite: http.SameSiteLaxMode})
	sp, err := s.enabledProvider(r, "oidc")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	q := r.URL.Query()
	if e := q.Get("error"); e != "" {
		s.fail(w, r, failf("idp_error", "identity provider returned %q", e))
		return
	}
	st, err := s.readState(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if st.Provider != sp.Slug || subtle.ConstantTimeCompare([]byte(st.State), []byte(q.Get("state"))) != 1 {
		s.fail(w, r, failf("state_mismatch", "sign-in state does not match"))
		return
	}
	code := q.Get("code")
	if code == "" {
		s.fail(w, r, failf("invalid_response", "missing authorization code"))
		return
	}
	ctx := oidc.ClientContext(r.Context(), s.client)
	oc, provider, err := s.oauthConfig(ctx, sp)
	if err != nil {
		s.fail(w, r, failf("provider_unavailable", "%v", err))
		return
	}
	tok, err := oc.Exchange(ctx, code, oauth2.VerifierOption(st.Verifier))
	if err != nil {
		s.fail(w, r, failf("token_exchange", "code exchange failed: %v", err))
		return
	}
	rawID, _ := tok.Extra("id_token").(string)
	if rawID == "" {
		s.fail(w, r, failf("invalid_response", "token response has no id_token"))
		return
	}
	idt, err := provider.VerifierContext(ctx, &oidc.Config{ClientID: oc.ClientID, Now: s.now}).Verify(ctx, rawID)
	if err != nil {
		s.fail(w, r, failf("invalid_token", "id token verification failed: %v", err))
		return
	}
	if subtle.ConstantTimeCompare([]byte(idt.Nonce), []byte(st.Nonce)) != 1 {
		s.fail(w, r, failf("nonce_mismatch", "id token nonce does not match"))
		return
	}
	var claims map[string]any
	if err := idt.Claims(&claims); err != nil {
		s.fail(w, r, failf("invalid_token", "id token claims: %v", err))
		return
	}
	pcfg := ParseConfig(sp.Config)
	id := Identity{Subject: idt.Subject, Email: str(claims["email"]), Name: str(claims["name"]), EmailVerified: true}
	if v, ok := claims["email_verified"]; ok {
		id.EmailVerified = truthy(v)
	}
	if pcfg.RoleClaim != "" {
		id.Values = strs(claims[pcfg.RoleClaim])
	}
	s.finish(w, r, sp, id, st.Redirect)
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

func truthy(v any) bool {
	switch x := v.(type) {
	case bool:
		return x
	case string:
		return strings.EqualFold(x, "true")
	}
	return false
}

// strs reads a claim that may be a string, a space/comma separated string or an array.
func strs(v any) []string {
	switch x := v.(type) {
	case string:
		return strings.FieldsFunc(x, func(r rune) bool { return r == ',' || r == ' ' })
	case []any:
		out := make([]string, 0, len(x))
		for _, e := range x {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}
