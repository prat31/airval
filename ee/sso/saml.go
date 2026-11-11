//go:build ee

package sso

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/crewjam/saml"
	"github.com/go-chi/chi/v5"

	"github.com/prat31/airval/ee/internal/httpx"
	"github.com/prat31/airval/internal/store"
)

// spKey is the SP signing key + self-signed certificate, sealed in ee_sso_providers.secret.
type spKey struct {
	KeyPEM  string `json:"key"`
	CertPEM string `json:"cert"`
}

func newSPKey(slug string) ([]byte, error) {
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}
	serial, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 62))
	tmpl := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: "airval SAML SP " + slug},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().AddDate(10, 0, 0),
		KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &k.PublicKey, k)
	if err != nil {
		return nil, err
	}
	return json.Marshal(spKey{
		KeyPEM:  string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(k)})),
		CertPEM: string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})),
	})
}

type cachedSAML struct {
	sp  *saml.ServiceProvider
	at  time.Time // provider UpdatedAt the entry was built from
	exp time.Time
}

func mustURL(s string) url.URL {
	u, _ := url.Parse(s)
	return *u
}

// serviceProvider builds (and caches for an hour) the SAML SP for a provider.
func (s *Service) serviceProvider(ctx context.Context, sp store.SSOProvider, withIDP bool) (*saml.ServiceProvider, error) {
	s.mu.Lock()
	c, ok := s.samlCache[sp.ID]
	s.mu.Unlock()
	if ok && c.at.Equal(sp.UpdatedAt) && s.now().Before(c.exp) && (c.sp.IDPMetadata != nil || !withIDP) {
		return c.sp, nil
	}
	raw, err := s.h.Host.Keyring().Unseal(sp.Secret, sp.KeyVersion, s.secretAAD(sp.ID))
	if err != nil {
		return nil, fmt.Errorf("decrypt SP key: %w", err)
	}
	var k spKey
	if err := json.Unmarshal(raw, &k); err != nil {
		return nil, err
	}
	kb, _ := pem.Decode([]byte(k.KeyPEM))
	cb, _ := pem.Decode([]byte(k.CertPEM))
	if kb == nil || cb == nil {
		return nil, errors.New("invalid SP key material")
	}
	key, err := x509.ParsePKCS1PrivateKey(kb.Bytes)
	if err != nil {
		return nil, err
	}
	cert, err := x509.ParseCertificate(cb.Bytes)
	if err != nil {
		return nil, err
	}
	base := s.baseURL() + "/v1/auth/saml/" + sp.Slug
	out := &saml.ServiceProvider{EntityID: base + "/metadata", Key: key, Certificate: cert,
		MetadataURL: mustURL(base + "/metadata"), AcsURL: mustURL(base + "/acs"), HTTPClient: s.client,
		AuthnNameIDFormat: saml.UnspecifiedNameIDFormat, AllowIDPInitiated: false}
	if withIDP {
		md, err := s.idpMetadata(ctx, ParseConfig(sp.Config))
		if err != nil {
			return nil, err
		}
		out.IDPMetadata = md
	}
	s.mu.Lock()
	s.samlCache[sp.ID] = cachedSAML{sp: out, at: sp.UpdatedAt, exp: s.now().Add(time.Hour)}
	s.mu.Unlock()
	return out, nil
}

func (s *Service) idpMetadata(ctx context.Context, cfg Config) (*saml.EntityDescriptor, error) {
	data := []byte(cfg.IDPMetadataXML)
	if cfg.IDPMetadataURL != "" {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, cfg.IDPMetadataURL, nil)
		if err != nil {
			return nil, err
		}
		resp, err := s.client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("fetch IdP metadata: %w", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("fetch IdP metadata: HTTP %d", resp.StatusCode)
		}
		if data, err = io.ReadAll(io.LimitReader(resp.Body, 1<<20)); err != nil {
			return nil, err
		}
	}
	return parseMetadata(data)
}

// parseMetadata accepts an EntityDescriptor or an EntitiesDescriptor (first entity with an IdP role).
func parseMetadata(data []byte) (*saml.EntityDescriptor, error) {
	var ed saml.EntityDescriptor
	if err := xml.Unmarshal(data, &ed); err == nil && len(ed.IDPSSODescriptors) > 0 {
		return &ed, nil
	}
	var eds saml.EntitiesDescriptor
	if err := xml.Unmarshal(data, &eds); err != nil {
		return nil, fmt.Errorf("parse IdP metadata: %w", err)
	}
	for i := range eds.EntityDescriptors {
		if len(eds.EntityDescriptors[i].IDPSSODescriptors) > 0 {
			return &eds.EntityDescriptors[i], nil
		}
	}
	return nil, errors.New("parse IdP metadata: no IdP SSO descriptor found")
}

func (s *Service) samlMetadata(w http.ResponseWriter, r *http.Request) {
	sp, err := s.h.Host.Store().GetSSOProviderBySlug(r.Context(), chi.URLParam(r, "provider"))
	if err != nil || sp.Kind != "saml" {
		s.h.Host.WriteError(w, r, store.ErrNotFound)
		return
	}
	ssp, err := s.serviceProvider(r.Context(), sp, false)
	if err != nil {
		s.h.Host.WriteError(w, r, err)
		return
	}
	b, err := xml.MarshalIndent(ssp.Metadata(), "", "  ")
	if err != nil {
		s.h.Host.WriteError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/samlmetadata+xml")
	_, _ = w.Write(b)
}

// relay is the server-side state of an SP-initiated login, keyed by the RelayState token (cache table).
type relay struct {
	Provider  string `json:"p"`
	RequestID string `json:"id"`
	Redirect  string `json:"r"`
}

const relayPrefix = "ee:saml:relay:"

func (s *Service) samlStart(w http.ResponseWriter, r *http.Request) {
	sp, err := s.enabledProvider(r, "saml")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	ssp, err := s.serviceProvider(r.Context(), sp, true)
	if err != nil {
		s.fail(w, r, failf("provider_unavailable", "%v", err))
		return
	}
	loc := ssp.GetSSOBindingLocation(saml.HTTPRedirectBinding)
	if loc == "" {
		s.fail(w, r, failf("provider_unavailable", "IdP metadata has no HTTP-Redirect SSO endpoint"))
		return
	}
	req, err := ssp.MakeAuthenticationRequest(loc, saml.HTTPRedirectBinding, saml.HTTPPostBinding)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	token := randomToken(24) // 32 chars: within the 80-byte RelayState limit
	st, _ := json.Marshal(relay{Provider: sp.Slug, RequestID: req.ID, Redirect: httpx.SafeRedirect(r.URL.Query().Get("redirect"))})
	if err := s.h.Host.Store().CachePut(r.Context(), relayPrefix+token, st, stateTTL); err != nil {
		s.fail(w, r, err)
		return
	}
	u, err := req.Redirect(token, ssp)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	http.Redirect(w, r, u.String(), http.StatusFound)
}

func (s *Service) samlACS(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sp, err := s.enabledProvider(r, "saml")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := r.ParseForm(); err != nil {
		s.fail(w, r, failf("invalid_response", "invalid form"))
		return
	}
	token := r.PostForm.Get("RelayState")
	raw, ok, err := s.h.Host.Store().CacheGet(ctx, relayPrefix+token)
	if err != nil || !ok || token == "" {
		s.fail(w, r, failf("state_expired", "sign-in session expired; try again"))
		return
	}
	// Single use: expire the relay state immediately.
	_ = s.h.Host.Store().CachePut(ctx, relayPrefix+token, []byte(`{}`), time.Nanosecond)
	var st relay
	if json.Unmarshal(raw, &st) != nil || st.Provider != sp.Slug || st.RequestID == "" {
		s.fail(w, r, failf("state_mismatch", "sign-in state does not match"))
		return
	}
	ssp, err := s.serviceProvider(ctx, sp, true)
	if err != nil {
		s.fail(w, r, failf("provider_unavailable", "%v", err))
		return
	}
	a, err := ssp.ParseResponse(r, []string{st.RequestID})
	if err != nil {
		var ie *saml.InvalidResponseError
		if errors.As(err, &ie) {
			err = fmt.Errorf("%w: %v", err, ie.PrivateErr)
		}
		s.fail(w, r, failf("invalid_assertion", "SAML response rejected: %v", err))
		return
	}
	// Replay protection: an assertion id is accepted once.
	if a.ID == "" {
		s.fail(w, r, failf("invalid_assertion", "assertion has no id"))
		return
	}
	if _, seen, _ := s.h.Host.Store().CacheGet(ctx, "ee:saml:assertion:"+a.ID); seen {
		s.fail(w, r, failf("replayed_assertion", "this SAML assertion was already used"))
		return
	}
	_ = s.h.Host.Store().CachePut(ctx, "ee:saml:assertion:"+a.ID, []byte("1"), 24*time.Hour)
	s.finish(w, r, sp, samlIdentity(a, ParseConfig(sp.Config)), st.Redirect)
}

var emailAttributes = []string{"email", "mail", "emailaddress", "urn:oid:0.9.2342.19200300.100.1.3",
	"http://schemas.xmlsoap.org/ws/2005/05/identity/claims/emailaddress"}

var nameAttributes = []string{"name", "displayname", "cn", "urn:oid:2.5.4.3",
	"http://schemas.xmlsoap.org/ws/2005/05/identity/claims/name"}

func attrValues(a *saml.Assertion, names ...string) []string {
	var out []string
	for _, st := range a.AttributeStatements {
		for _, at := range st.Attributes {
			for _, n := range names {
				if n != "" && (strings.EqualFold(at.Name, n) || strings.EqualFold(at.FriendlyName, n)) {
					for _, v := range at.Values {
						if v.Value != "" {
							out = append(out, v.Value)
						}
					}
				}
			}
		}
	}
	return out
}

func first(v []string) string {
	if len(v) > 0 {
		return v[0]
	}
	return ""
}

func samlIdentity(a *saml.Assertion, cfg Config) Identity {
	id := Identity{EmailVerified: true}
	if a.Subject != nil && a.Subject.NameID != nil {
		id.Subject = a.Subject.NameID.Value
	}
	if cfg.EmailAttribute != "" {
		id.Email = first(attrValues(a, cfg.EmailAttribute))
	} else {
		id.Email = first(attrValues(a, emailAttributes...))
		if id.Email == "" && strings.Contains(id.Subject, "@") {
			id.Email = id.Subject
		}
	}
	if cfg.NameAttribute != "" {
		id.Name = first(attrValues(a, cfg.NameAttribute))
	} else {
		id.Name = first(attrValues(a, nameAttributes...))
	}
	if cfg.RoleClaim != "" {
		id.Values = attrValues(a, cfg.RoleClaim)
	}
	return id
}
