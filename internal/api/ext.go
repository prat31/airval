package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/prat31/airval/internal/api/gen"
	"github.com/prat31/airval/internal/auth"
	"github.com/prat31/airval/internal/ext"
	"github.com/prat31/airval/internal/rbac"
)

// Extension hooks used by ee (see internal/ext). In the community edition none are set: GET /v1/auth/providers
// is empty, GET /v1/edition lists no features and documented ee endpoints answer 501.

// SetAuthProviders installs the sign-in provider listing (ee).
func (s *Server) SetAuthProviders(f ext.AuthProvidersFunc) {
	s.mu.Lock()
	s.authProviders = f
	s.mu.Unlock()
}

// EnableFeatures adds enterprise feature flags to GET /v1/edition (ee).
func (s *Server) EnableFeatures(features ...string) {
	s.mu.Lock()
	for _, f := range features {
		if !slices.Contains(s.features, f) {
			s.features = append(s.features, f)
		}
	}
	s.mu.Unlock()
}

// AddCallObserver registers an observer of completed mutating API calls (ee audit log).
func (s *Server) AddCallObserver(o ext.CallObserver) {
	s.mu.Lock()
	s.observers = append(s.observers, o)
	s.mu.Unlock()
}

// Authenticate is the API authentication middleware, exported for extension routes.
func (s *Server) Authenticate(next http.Handler) http.Handler { return s.authenticate(next) }

// WriteError renders err as an API error (exported for extension routes).
func (s *Server) WriteError(w http.ResponseWriter, r *http.Request, err error) {
	s.writeError(w, r, err)
}

func (s *Server) GetEdition(_ context.Context, _ gen.GetEditionRequestObject) (gen.GetEditionResponseObject, error) {
	s.mu.RLock()
	feats := append([]string{}, s.features...)
	s.mu.RUnlock()
	slices.Sort(feats)
	out := gen.Edition{Edition: "community", Features: make([]gen.EnterpriseFeature, 0, len(feats))}
	if s.Edition == "ee" {
		out.Edition = "enterprise"
	}
	for _, f := range feats {
		out.Features = append(out.Features, gen.EnterpriseFeature(f))
	}
	return gen.GetEdition200JSONResponse(out), nil
}

func (s *Server) ListAuthProviders(ctx context.Context, _ gen.ListAuthProvidersRequestObject) (gen.ListAuthProvidersResponseObject, error) {
	s.mu.RLock()
	f := s.authProviders
	s.mu.RUnlock()
	items := []gen.AuthProvider{}
	if f != nil {
		ps, err := f(ctx)
		if err != nil {
			return nil, err
		}
		for _, p := range ps {
			items = append(items, gen.AuthProvider{Id: p.ID, Kind: gen.AuthProviderKind(p.Kind), Name: p.Name, StartUrl: p.StartURL})
		}
	}
	return gen.ListAuthProviders200JSONResponse{Items: items}, nil
}

// EERoutes are the documented enterprise endpoints (OpenAPI tag "ee"; kept in sync with api/openapi.yaml by
// TestEERoutesMatchSpec). Core answers 501 on them; extension routes registered later replace the handlers.
var EERoutes = [][2]string{
	{"GET", "/v1/auth/saml/{provider}/metadata"},
	{"GET", "/v1/auth/saml/{provider}/start"},
	{"POST", "/v1/auth/saml/{provider}/acs"},
	{"GET", "/v1/orgs/{org}/sso-providers"},
	{"POST", "/v1/orgs/{org}/sso-providers"},
	{"GET", "/v1/orgs/{org}/sso-providers/{id}"},
	{"PATCH", "/v1/orgs/{org}/sso-providers/{id}"},
	{"DELETE", "/v1/orgs/{org}/sso-providers/{id}"},
	{"GET", "/v1/orgs/{org}/scim-tokens"},
	{"POST", "/v1/orgs/{org}/scim-tokens"},
	{"DELETE", "/v1/orgs/{org}/scim-tokens/{id}"},
	{"GET", "/v1/orgs/{org}/scim-settings"},
	{"PUT", "/v1/orgs/{org}/scim-settings"},
	{"GET", "/v1/orgs/{org}/roles"},
	{"POST", "/v1/orgs/{org}/roles"},
	{"PATCH", "/v1/orgs/{org}/roles/{id}"},
	{"DELETE", "/v1/orgs/{org}/roles/{id}"},
	{"GET", "/v1/orgs/{org}/role-assignments"},
	{"PUT", "/v1/orgs/{org}/role-assignments"},
	{"DELETE", "/v1/orgs/{org}/role-assignments"},
	{"GET", "/v1/orgs/{org}/audit-log/export"},
	{"GET", "/v1/orgs/{org}/audit-settings"},
	{"PUT", "/v1/orgs/{org}/audit-settings"},
	{"GET", "/v1/orgs/{org}/retention-policies"},
	{"GET", "/v1/projects/{project}/retention-policy"},
	{"PUT", "/v1/projects/{project}/retention-policy"},
	{"DELETE", "/v1/projects/{project}/retention-policy"},
	{"GET", "/v1/orgs/{org}/branding"},
	{"PUT", "/v1/orgs/{org}/branding"},
	{"GET", "/scim/v2/Users"},
	{"POST", "/scim/v2/Users"},
	{"GET", "/scim/v2/Users/{id}"},
	{"PUT", "/scim/v2/Users/{id}"},
	{"PATCH", "/scim/v2/Users/{id}"},
	{"DELETE", "/scim/v2/Users/{id}"},
	{"GET", "/scim/v2/Groups"},
	{"POST", "/scim/v2/Groups"},
	{"GET", "/scim/v2/Groups/{id}"},
	{"PUT", "/scim/v2/Groups/{id}"},
	{"PATCH", "/scim/v2/Groups/{id}"},
	{"DELETE", "/scim/v2/Groups/{id}"},
}

func (s *Server) eeRoutes(r chi.Router) {
	h := func(w http.ResponseWriter, r *http.Request) {
		s.writeError(w, r, errorf(http.StatusNotImplemented, "enterprise_required",
			"this feature requires the airval enterprise edition"))
	}
	for _, rt := range EERoutes {
		r.Method(rt[0], rt[1], http.HandlerFunc(h))
	}
}

// ---- call observation (audit) ----

const maxObservedBody = 64 << 10

func isMutation(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	}
	return false
}

// observedWriter records the status and the beginning of a 201 body (to find the created id).
type observedWriter struct {
	middleware.WrapResponseWriter
	buf bytes.Buffer
}

func (w *observedWriter) Write(b []byte) (int, error) {
	if w.Status() == http.StatusCreated && w.buf.Len() < 4096 {
		w.buf.Write(b[:min(len(b), 4096-w.buf.Len())])
	}
	return w.WrapResponseWriter.Write(b)
}

func (s *Server) callObservers() []ext.CallObserver {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.observers
}

// observe wraps next so that, for mutating requests, observers are told about the completed call.
func (s *Server) observe(w http.ResponseWriter, r *http.Request, sc *rbac.Scope, next http.Handler) {
	obs := s.callObservers()
	if len(obs) == 0 || !isMutation(r.Method) {
		next.ServeHTTP(w, r)
		return
	}
	start := time.Now()
	var body []byte
	if r.Body != nil {
		body, _ = io.ReadAll(io.LimitReader(r.Body, maxObservedBody))
		r.Body = struct {
			io.Reader
			io.Closer
		}{io.MultiReader(bytes.NewReader(body), r.Body), r.Body}
	}
	ow := &observedWriter{WrapResponseWriter: middleware.NewWrapResponseWriter(w, r.ProtoMajor)}
	next.ServeHTTP(ow, r)

	ctx := r.Context()
	c := ext.APICall{Principal: auth.FromContext(ctx), Method: r.Method, Path: r.URL.Path, Operation: sc.Operation(),
		Status: ow.Status(), Audited: sc.Audited(), IP: clientIP(r.RemoteAddr), UserAgent: r.UserAgent(),
		RequestID: middleware.GetReqID(ctx), Body: body, At: start.UTC(), Duration: time.Since(start)}
	if c.Status == 0 {
		c.Status = http.StatusOK
	}
	c.OrgID, c.ProjectID = sc.Resource()
	if rc := chi.RouteContext(ctx); rc != nil {
		c.Route = rc.RoutePattern()
		c.Params = map[string]string{}
		for i, k := range rc.URLParams.Keys {
			if k != "*" && i < len(rc.URLParams.Values) {
				c.Params[k] = rc.URLParams.Values[i]
			}
		}
	}
	if ow.buf.Len() > 0 {
		var created struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(ow.buf.Bytes(), &created) == nil {
			c.CreatedID = created.ID
		}
	}
	octx := context.WithoutCancel(ctx)
	for _, o := range obs {
		o(octx, c)
	}
}

func clientIP(remote string) string {
	if h, _, err := net.SplitHostPort(remote); err == nil {
		return h
	}
	return strings.Trim(remote, "[]")
}
