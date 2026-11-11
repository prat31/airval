package api

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/airval/airval/internal/api/gen"
	"github.com/airval/airval/internal/auth"
	"github.com/airval/airval/internal/store"
)

// ---- system ----

func (s *Server) GetMeta(ctx context.Context, _ gen.GetMetaRequestObject) (gen.GetMetaResponseObject, error) {
	s.mu.RLock()
	providers := append([]string{}, s.ssoProviders...)
	s.mu.RUnlock()
	m := gen.Meta{Version: s.Version, Edition: gen.MetaEdition(s.Edition)}
	if s.Commit != "" {
		m.Commit = &s.Commit
	}
	m.Features.Sso = len(providers) > 0
	m.Features.SsoProviders = &providers
	m.Features.AuditLog = true
	if s.Config != nil {
		m.Features.Signup = ptr(s.Config.Auth.AllowSignup)
	}
	return gen.GetMeta200JSONResponse(m), nil
}

func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

func (s *Server) handleReadyz(w http.ResponseWriter, r *http.Request) {
	checks := map[string]string{}
	ok := true
	if err := s.Store.Ping(r.Context()); err != nil {
		checks["database"], ok = err.Error(), false
	} else {
		checks["database"] = "ok"
	}
	if s.Queue != nil {
		if err := s.Queue.Ready(r.Context()); err != nil {
			checks["nats"], ok = err.Error(), false
		} else {
			checks["nats"] = "ok"
		}
	}
	status, code := gen.HealthStatus("ok"), http.StatusOK
	if !ok {
		status, code = gen.HealthStatus("unavailable"), http.StatusServiceUnavailable
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_, _ = w.Write(mustJSON(gen.Health{Status: status, Checks: &checks}))
}

func (s *Server) handleOIDCNotAvailable(w http.ResponseWriter, r *http.Request) {
	s.writeError(w, r, errorf(http.StatusNotImplemented, "not_implemented", "SSO requires the enterprise edition"))
}

// ---- setup & sessions ----

// cookieResponse wraps a generated response and sets/clears session cookies before writing it.
type cookieResponse[T any] struct {
	set   func(http.ResponseWriter)
	inner T
	visit func(T, http.ResponseWriter) error
}

func (c cookieResponse[T]) write(w http.ResponseWriter) error {
	c.set(w)
	return c.visit(c.inner, w)
}

type setupResponse struct {
	cookieResponse[gen.CompleteSetup201JSONResponse]
}

func (r setupResponse) VisitCompleteSetupResponse(w http.ResponseWriter) error { return r.write(w) }

type loginResponse struct {
	cookieResponse[gen.Login200JSONResponse]
}

func (r loginResponse) VisitLoginResponse(w http.ResponseWriter) error { return r.write(w) }

type logoutResponse struct {
	cookieResponse[gen.Logout204Response]
}

func (r logoutResponse) VisitLogoutResponse(w http.ResponseWriter) error { return r.write(w) }

func (s *Server) GetSetupStatus(ctx context.Context, _ gen.GetSetupStatusRequestObject) (gen.GetSetupStatusResponseObject, error) {
	n, err := s.Store.CountUsers(ctx)
	if err != nil {
		return nil, err
	}
	return gen.GetSetupStatus200JSONResponse{NeedsSetup: n == 0}, nil
}

func (s *Server) CompleteSetup(ctx context.Context, req gen.CompleteSetupRequestObject) (gen.CompleteSetupResponseObject, error) {
	b := req.Body
	if b == nil || strings.TrimSpace(string(b.Email)) == "" || strings.TrimSpace(b.OrgName) == "" {
		return nil, invalid("email, password and org_name are required")
	}
	u, _, err := s.Auth.Setup(ctx, auth.SetupInput{Email: string(b.Email), Password: deref(b.Password, ""),
		Name: trimmed(b.Name), OrgName: strings.TrimSpace(b.OrgName), OrgSlug: trimmed(b.OrgSlug)})
	if err != nil {
		return nil, err
	}
	ri := requestInfo(ctx)
	se, tok, err := s.Auth.NewSession(ctx, u.ID, ri.UserAgent, ri.IP)
	if err != nil {
		return nil, err
	}
	me, err := s.me(ctx, &auth.Principal{User: &u, Session: &se})
	if err != nil {
		return nil, err
	}
	return setupResponse{cookieResponse[gen.CompleteSetup201JSONResponse]{
		set: func(w http.ResponseWriter) { s.Auth.SetSessionCookies(w, se, tok) }, inner: gen.CompleteSetup201JSONResponse(me),
		visit: gen.CompleteSetup201JSONResponse.VisitCompleteSetupResponse}}, nil
}

func (s *Server) Login(ctx context.Context, req gen.LoginRequestObject) (gen.LoginResponseObject, error) {
	if req.Body == nil {
		return nil, invalid("email and password are required")
	}
	ri := requestInfo(ctx)
	u, se, tok, err := s.Auth.Login(ctx, string(req.Body.Email), deref(req.Body.Password, ""), ri.UserAgent, ri.IP)
	if err != nil {
		return nil, err
	}
	me, err := s.me(ctx, &auth.Principal{User: &u, Session: &se})
	if err != nil {
		return nil, err
	}
	return loginResponse{cookieResponse[gen.Login200JSONResponse]{
		set: func(w http.ResponseWriter) { s.Auth.SetSessionCookies(w, se, tok) }, inner: gen.Login200JSONResponse(me),
		visit: gen.Login200JSONResponse.VisitLoginResponse}}, nil
}

func (s *Server) Logout(ctx context.Context, _ gen.LogoutRequestObject) (gen.LogoutResponseObject, error) {
	if p := s.principal(ctx); p != nil && p.Session != nil {
		if err := s.Store.DeleteSession(ctx, p.Session.ID); err != nil {
			return nil, err
		}
	}
	return logoutResponse{cookieResponse[gen.Logout204Response]{
		set: s.Auth.ClearSessionCookies, visit: gen.Logout204Response.VisitLogoutResponse}}, nil
}

func (s *Server) GetMe(ctx context.Context, _ gen.GetMeRequestObject) (gen.GetMeResponseObject, error) {
	me, err := s.me(ctx, s.principal(ctx))
	if err != nil {
		return nil, err
	}
	return gen.GetMe200JSONResponse(me), nil
}

func (s *Server) UpdateMe(ctx context.Context, req gen.UpdateMeRequestObject) (gen.UpdateMeResponseObject, error) {
	p, err := s.requireSession(ctx)
	if err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, invalid("request body required")
	}
	u, err := s.Store.GetUser(ctx, p.User.ID)
	if err != nil {
		return nil, err
	}
	b := req.Body
	if b.Name != nil {
		u.Name = strings.TrimSpace(*b.Name)
	}
	if b.AvatarUrl != nil {
		u.AvatarURL = strings.TrimSpace(*b.AvatarUrl)
	}
	if b.Password != nil {
		if u.PasswordHash != nil {
			ok, _ := auth.VerifyPassword(*u.PasswordHash, deref(b.CurrentPassword, ""))
			if !ok {
				return nil, errorf(http.StatusForbidden, "forbidden", "current_password is incorrect")
			}
		}
		if len(*b.Password) < auth.MinPasswordLen {
			return nil, auth.ErrWeakPassword
		}
		h, err := auth.HashPassword(*b.Password)
		if err != nil {
			return nil, err
		}
		u.PasswordHash = &h
	}
	if err := s.Store.UpdateUser(ctx, &u); err != nil {
		return nil, err
	}
	p.User = &u
	me, err := s.me(ctx, p)
	if err != nil {
		return nil, err
	}
	return gen.UpdateMe200JSONResponse(me), nil
}

// me builds the Me response for a principal.
func (s *Server) me(ctx context.Context, p *auth.Principal) (gen.Me, error) {
	switch {
	case p == nil:
		return gen.Me{}, auth.ErrUnauthenticated
	case p.User != nil:
		orgs, err := s.Store.ListUserOrgs(ctx, p.User.ID)
		if err != nil {
			return gen.Me{}, err
		}
		u := toUser(*p.User)
		me := gen.Me{AuthMethod: gen.MeAuthMethod("session"), User: &u,
			Orgs: mapSlice(orgs, func(o store.OrgMembership) gen.Organization { return toOrg(o.Organization, o.Role) })}
		if p.Session != nil {
			me.CsrfToken = p.Session.CSRFToken
		}
		return me, nil
	case p.APIKey != nil:
		org, err := s.Store.GetOrg(ctx, p.APIKey.OrgID)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return gen.Me{}, err
		}
		k := toAPIKey(*p.APIKey)
		return gen.Me{AuthMethod: gen.MeAuthMethod("api_key"), ApiKey: &k,
			Orgs: []gen.Organization{toOrg(org, p.APIKey.Role)}}, nil
	}
	return gen.Me{}, auth.ErrUnauthenticated
}
