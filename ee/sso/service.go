//go:build ee

package sso

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/prat31/airval/ee/internal/httpx"
	"github.com/prat31/airval/internal/auth"
	"github.com/prat31/airval/internal/ext"
	"github.com/prat31/airval/internal/store"
)

// Service serves SSO administration and sign-in.
type Service struct {
	h      httpx.H
	client *http.Client
	now    func() time.Time

	mu        sync.Mutex
	oidcCache map[string]cachedOIDC // by issuer
	samlCache map[uuid.UUID]cachedSAML
}

// New returns the SSO service. client may be nil (a 10s-timeout client is used).
func New(h ext.Host, client *http.Client) *Service {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	return &Service{h: httpx.H{Host: h}, client: client, now: func() time.Time { return time.Now().UTC() },
		oidcCache: map[string]cachedOIDC{}, samlCache: map[uuid.UUID]cachedSAML{}}
}

// Routes registers admin and sign-in endpoints.
func (s *Service) Routes(r chi.Router) {
	h := s.h
	authed := r.With(h.Host.Authenticate)
	authed.Get("/v1/orgs/{org}/sso-providers", h.Wrap("listSSOProviders", s.list))
	authed.Post("/v1/orgs/{org}/sso-providers", h.Wrap("createSSOProvider", s.create))
	authed.Get("/v1/orgs/{org}/sso-providers/{id}", h.Wrap("getSSOProvider", s.get))
	authed.Patch("/v1/orgs/{org}/sso-providers/{id}", h.Wrap("updateSSOProvider", s.update))
	authed.Delete("/v1/orgs/{org}/sso-providers/{id}", h.Wrap("deleteSSOProvider", s.remove))

	// Sign-in endpoints are public (no session yet).
	r.Get("/v1/auth/oidc/{provider}/start", s.oidcStart)
	r.Get("/v1/auth/oidc/{provider}/callback", s.oidcCallback)
	r.Get("/v1/auth/saml/{provider}/metadata", s.samlMetadata)
	r.Get("/v1/auth/saml/{provider}/start", s.samlStart)
	r.Post("/v1/auth/saml/{provider}/acs", s.samlACS)
}

// AuthProviders lists enabled providers for the sign-in page.
func (s *Service) AuthProviders(ctx context.Context) ([]ext.AuthProvider, error) {
	ps, err := s.h.Host.Store().ListSSOProviders(ctx, uuid.Nil)
	if err != nil {
		return nil, err
	}
	out := []ext.AuthProvider{}
	for _, p := range ps {
		if p.Enabled {
			out = append(out, ext.AuthProvider{ID: p.Slug, Kind: p.Kind, Name: p.Name,
				StartURL: "/v1/auth/" + p.Kind + "/" + p.Slug + "/start"})
		}
	}
	return out, nil
}

func (s *Service) baseURL() string {
	if c := s.h.Host.Config(); c != nil && c.Server.BaseURL != "" {
		return strings.TrimRight(c.Server.BaseURL, "/")
	}
	return "http://localhost:8080"
}

func (s *Service) toJSON(p store.SSOProvider) providerJSON {
	out := providerJSON{ID: p.ID, OrgID: p.OrgID, Slug: p.Slug, Kind: p.Kind, Name: p.Name, Enabled: p.Enabled,
		Config: ParseConfig(p.Config), CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt}
	base := s.baseURL()
	switch p.Kind {
	case "oidc":
		out.HasClientSecret = len(p.Secret) > 0
		out.RedirectURI = base + "/v1/auth/oidc/" + p.Slug + "/callback"
	case "saml":
		out.RedirectURI = base + "/v1/auth/saml/" + p.Slug + "/acs"
		out.MetadataURL = base + "/v1/auth/saml/" + p.Slug + "/metadata"
		out.EntityID = out.MetadataURL
	}
	return out
}

// ---- admin ----

func (s *Service) list(w http.ResponseWriter, r *http.Request) error {
	orgID, _, err := s.h.RequireOrg(r, store.RoleAdmin)
	if err != nil {
		return err
	}
	ps, err := s.h.Host.Store().ListSSOProviders(r.Context(), orgID)
	if err != nil {
		return err
	}
	items := make([]providerJSON, len(ps))
	for i, p := range ps {
		items[i] = s.toJSON(p)
	}
	return httpx.JSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Service) load(r *http.Request, min store.Role) (store.SSOProvider, *auth.Principal, error) {
	orgID, p, err := s.h.RequireOrg(r, min)
	if err != nil {
		return store.SSOProvider{}, nil, err
	}
	id, err := httpx.UUIDParam(r, "id")
	if err != nil {
		return store.SSOProvider{}, nil, err
	}
	sp, err := s.h.Host.Store().GetSSOProvider(r.Context(), id)
	if err != nil || sp.OrgID != orgID {
		return store.SSOProvider{}, nil, store.ErrNotFound
	}
	return sp, p, nil
}

func (s *Service) get(w http.ResponseWriter, r *http.Request) error {
	sp, _, err := s.load(r, store.RoleAdmin)
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusOK, s.toJSON(sp))
}

var slugRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,62}$`)

type providerWrite struct {
	Kind         string  `json:"kind"`
	Name         *string `json:"name"`
	Slug         string  `json:"slug"`
	Enabled      *bool   `json:"enabled"`
	Config       *Config `json:"config"`
	ClientSecret *string `json:"client_secret"`
}

func (s *Service) secretAAD(id uuid.UUID) []byte { return []byte("sso-provider:" + id.String()) }

func (s *Service) create(w http.ResponseWriter, r *http.Request) error {
	orgID, p, err := s.h.RequireOrg(r, store.RoleOwner)
	if err != nil {
		return err
	}
	if err := httpx.RequireUser(p); err != nil {
		return err
	}
	var in providerWrite
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	if in.Name == nil || strings.TrimSpace(*in.Name) == "" || len(*in.Name) > 100 {
		return httpx.Invalid("name must be 1–100 characters")
	}
	if in.Config == nil {
		return httpx.Invalid("config is required")
	}
	ctx := r.Context()
	st := s.h.Host.Store()
	crs, err := st.ListCustomRoles(ctx, orgID)
	if err != nil {
		return err
	}
	if err := in.Config.Normalize(in.Kind, crs); err != nil {
		return httpx.Invalid("%v", err)
	}
	slug := strings.ToLower(strings.TrimSpace(in.Slug))
	if slug == "" {
		slug = auth.Slugify(*in.Name)
	}
	if !slugRe.MatchString(slug) {
		return httpx.Invalid("slug must be 2–63 lowercase letters, digits or dashes")
	}
	cfg, _ := json.Marshal(in.Config)
	sp := store.SSOProvider{ID: store.NewID(), OrgID: orgID, Slug: slug, Kind: in.Kind, Name: strings.TrimSpace(*in.Name),
		Enabled: in.Enabled == nil || *in.Enabled, Config: cfg}
	switch in.Kind {
	case "oidc":
		if in.ClientSecret != nil && *in.ClientSecret != "" {
			if sp.Secret, sp.KeyVersion, err = s.h.Host.Keyring().Seal([]byte(*in.ClientSecret), s.secretAAD(sp.ID)); err != nil {
				return err
			}
		}
	case "saml":
		key, err := newSPKey(slug)
		if err != nil {
			return err
		}
		if sp.Secret, sp.KeyVersion, err = s.h.Host.Keyring().Seal(key, s.secretAAD(sp.ID)); err != nil {
			return err
		}
	}
	if err := st.CreateSSOProvider(ctx, &sp); errors.Is(err, store.ErrConflict) {
		return httpx.Errorf(http.StatusConflict, "conflict", "the slug %q is already in use", slug)
	} else if err != nil {
		return err
	}
	s.h.Audit(r, orgID, "sso_provider.create", "sso_provider:"+sp.ID.String(), map[string]any{"name": sp.Name, "kind": sp.Kind, "slug": sp.Slug})
	return httpx.JSON(w, http.StatusCreated, s.toJSON(sp))
}

func (s *Service) update(w http.ResponseWriter, r *http.Request) error {
	sp, p, err := s.load(r, store.RoleOwner)
	if err != nil {
		return err
	}
	if err := httpx.RequireUser(p); err != nil {
		return err
	}
	var in providerWrite
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	changed := []string{}
	if in.Name != nil {
		n := strings.TrimSpace(*in.Name)
		if n == "" || len(n) > 100 {
			return httpx.Invalid("name must be 1–100 characters")
		}
		sp.Name = n
		changed = append(changed, "name")
	}
	if in.Enabled != nil {
		sp.Enabled = *in.Enabled
		changed = append(changed, "enabled")
	}
	if in.Config != nil {
		crs, err := s.h.Host.Store().ListCustomRoles(r.Context(), sp.OrgID)
		if err != nil {
			return err
		}
		if err := in.Config.Normalize(sp.Kind, crs); err != nil {
			return httpx.Invalid("%v", err)
		}
		sp.Config, _ = json.Marshal(in.Config)
		changed = append(changed, "config")
		s.forget(sp)
	}
	if in.ClientSecret != nil && sp.Kind == "oidc" {
		if *in.ClientSecret == "" {
			sp.Secret, sp.KeyVersion = nil, 0
		} else if sp.Secret, sp.KeyVersion, err = s.h.Host.Keyring().Seal([]byte(*in.ClientSecret), s.secretAAD(sp.ID)); err != nil {
			return err
		}
		changed = append(changed, "client_secret")
	}
	if err := s.h.Host.Store().UpdateSSOProvider(r.Context(), &sp); err != nil {
		return err
	}
	s.h.Audit(r, sp.OrgID, "sso_provider.update", "sso_provider:"+sp.ID.String(), map[string]any{"name": sp.Name, "changed": changed})
	return httpx.JSON(w, http.StatusOK, s.toJSON(sp))
}

func (s *Service) remove(w http.ResponseWriter, r *http.Request) error {
	sp, p, err := s.load(r, store.RoleOwner)
	if err != nil {
		return err
	}
	if err := httpx.RequireUser(p); err != nil {
		return err
	}
	if err := s.h.Host.Store().DeleteSSOProvider(r.Context(), sp.ID); err != nil {
		return err
	}
	s.forget(sp)
	s.h.Audit(r, sp.OrgID, "sso_provider.delete", "sso_provider:"+sp.ID.String(), map[string]any{"name": sp.Name})
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Service) forget(sp store.SSOProvider) {
	s.mu.Lock()
	delete(s.samlCache, sp.ID)
	delete(s.oidcCache, ParseConfig(sp.Config).Issuer)
	s.mu.Unlock()
}

// ---- sign-in completion (shared by OIDC and SAML) ----

// Identity is what an IdP asserted about the user.
type Identity struct {
	Subject       string
	Email         string
	EmailVerified bool
	Name          string
	Values        []string // role claim / attribute values (groups)
}

// loginError is a sign-in failure shown on the login page (?sso_error=code).
type loginError struct {
	code string
	msg  string
}

func (e *loginError) Error() string { return e.msg }

func failf(code, format string, args ...any) error {
	return &loginError{code: code, msg: fmt.Sprintf(format, args...)}
}

// Complete links or provisions the user, applies role mapping and opens a session.
func (s *Service) Complete(ctx context.Context, sp store.SSOProvider, id Identity, userAgent, ip string) (store.Session, string, error) {
	cfg := ParseConfig(sp.Config)
	email := strings.ToLower(strings.TrimSpace(id.Email))
	if id.Subject == "" {
		return store.Session{}, "", failf("invalid_response", "identity provider returned no subject")
	}
	if email == "" || !strings.Contains(email, "@") {
		return store.Session{}, "", failf("no_email", "identity provider returned no email address")
	}
	if !id.EmailVerified {
		return store.Session{}, "", failf("email_unverified", "email address is not verified by the identity provider")
	}
	if !cfg.DomainAllowed(email) {
		return store.Session{}, "", failf("domain_not_allowed", "email domain is not allowed for this provider")
	}
	mappedRole, mappedCustom, mapped := MapRole(cfg.RoleMapping, id.Values)
	st := s.h.Host.Store()
	var user store.User
	err := st.WithTx(ctx, func(tx store.Store) error {
		ident, err := tx.GetSSOIdentity(ctx, sp.ID, id.Subject)
		switch {
		case err == nil:
			if user, err = tx.GetUser(ctx, ident.UserID); err != nil {
				return err
			}
		case errors.Is(err, store.ErrNotFound):
			user, err = tx.GetUserByEmail(ctx, email)
			if errors.Is(err, store.ErrNotFound) {
				if !cfg.JITProvisioning {
					return failf("not_provisioned", "no account exists for %s; ask an admin to invite you", email)
				}
				name := strings.TrimSpace(id.Name)
				if name == "" {
					name, _, _ = strings.Cut(email, "@")
				}
				user = store.User{Email: email, Name: name}
				err = tx.CreateUser(ctx, &user)
			}
			if err != nil {
				return err
			}
		default:
			return err
		}
		// Users deprovisioned through SCIM must not be re-created by just-in-time provisioning.
		if su, err := tx.GetSCIMUser(ctx, sp.OrgID, user.ID); err == nil && !su.Active {
			return failf("deactivated", "this account has been deactivated")
		}
		m, err := tx.GetMembership(ctx, sp.OrgID, user.ID)
		switch {
		case errors.Is(err, store.ErrNotFound):
			if !cfg.JITProvisioning {
				return failf("not_provisioned", "%s is not a member of this organization", email)
			}
			role := cfg.defaultRole()
			if mapped && mappedRole != "" {
				role = mappedRole
			}
			m = store.Membership{OrgID: sp.OrgID, UserID: user.ID, Role: role}
			if err := tx.UpsertMembership(ctx, &m); err != nil {
				return err
			}
			if mappedCustom != nil {
				if err := tx.SetRoleAssignment(ctx, &store.RoleAssignment{OrgID: sp.OrgID, UserID: user.ID,
					ProjectID: store.NilID, CustomRoleID: mappedCustom, Source: "sso"}); err != nil {
					return err
				}
			}
		case err != nil:
			return err
		case cfg.SyncRoles && mapped && m.Role != store.RoleOwner:
			if mappedRole != "" && m.Role != mappedRole {
				m.Role = mappedRole
				if err := tx.UpsertMembership(ctx, &m); err != nil {
					return err
				}
			}
			if mappedCustom != nil {
				if err := tx.SetRoleAssignment(ctx, &store.RoleAssignment{OrgID: sp.OrgID, UserID: user.ID,
					ProjectID: store.NilID, CustomRoleID: mappedCustom, Source: "sso"}); err != nil {
					return err
				}
			}
		}
		return tx.UpsertSSOIdentity(ctx, &store.SSOIdentity{ProviderID: sp.ID, Subject: id.Subject, UserID: user.ID,
			Email: email, LastLoginAt: s.now()})
	})
	if err != nil {
		return store.Session{}, "", err
	}
	se, tok, err := s.h.Host.Auth().NewSession(ctx, user.ID, userAgent, ip)
	if err != nil {
		return store.Session{}, "", err
	}
	e := store.AuditEntry{OrgID: sp.OrgID, ActorID: &user.ID, Actor: user.Email, Action: "auth.sso_login",
		Target: "sso_provider:" + sp.ID.String(), IP: ip, UserAgent: userAgent}
	e.Diff, _ = json.Marshal(map[string]string{"provider": sp.Slug, "kind": sp.Kind})
	if err := st.AppendAudit(context.WithoutCancel(ctx), &e); err != nil {
		s.h.Host.Logger().Warn("sso: audit login", "err", err)
	}
	return se, tok, nil
}

// finish sets the session cookies and redirects into the app, or back to the login page on failure.
func (s *Service) finish(w http.ResponseWriter, r *http.Request, sp store.SSOProvider, id Identity, redirect string) {
	se, tok, err := s.Complete(r.Context(), sp, id, r.UserAgent(), httpx.ClientIP(r))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.h.Host.Auth().SetSessionCookies(w, se, tok)
	http.Redirect(w, r, httpx.SafeRedirect(redirect), http.StatusFound)
}

// fail redirects to the login page with an error code (details go to the log, never to the URL).
func (s *Service) fail(w http.ResponseWriter, r *http.Request, err error) {
	code := "sso_failed"
	var le *loginError
	if errors.As(err, &le) {
		code = le.code
	}
	s.h.Host.Logger().Warn("sso: sign-in failed", "path", r.URL.Path, "code", code, "err", err)
	http.Redirect(w, r, "/login?sso_error="+url.QueryEscape(code), http.StatusFound)
}

func (s *Service) enabledProvider(r *http.Request, kind string) (store.SSOProvider, error) {
	sp, err := s.h.Host.Store().GetSSOProviderBySlug(r.Context(), chi.URLParam(r, "provider"))
	if err != nil || sp.Kind != kind || !sp.Enabled {
		return store.SSOProvider{}, failf("unknown_provider", "unknown or disabled SSO provider")
	}
	return sp, nil
}

func randomToken(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}
