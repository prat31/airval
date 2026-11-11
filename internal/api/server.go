// Package api serves the public HTTP API (generated strict server from api/openapi.yaml),
// operational endpoints, inbound webhooks and the embedded UI.
//
// Handlers live in per-area files (handlers_<area>.go) so features can be built in parallel.
// Stubs that other workstreams complete are marked `// TODO(engine)` (run execution) or
// `// TODO(wave)` (later feature waves) and return 501.
package api

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"

	"github.com/prat31/airval/internal/api/gen"
	"github.com/prat31/airval/internal/auth"
	"github.com/prat31/airval/internal/config"
	"github.com/prat31/airval/internal/queue"
	"github.com/prat31/airval/internal/rbac"
	"github.com/prat31/airval/internal/secrets"
	"github.com/prat31/airval/internal/store"
	"github.com/prat31/airval/internal/telemetry"
)

// Deps are the server's collaborators.
type Deps struct {
	Store   store.Store
	Queue   *queue.Queue // may be nil in tests (readiness then skips NATS)
	Keyring *secrets.Keyring
	Auth    *auth.Service
	Authz   *rbac.Authorizer
	Config  *config.Config
	Logger  *slog.Logger
	Metrics *telemetry.Metrics
	Version string
	Commit  string
	// UI serves the single-page app for every non-API path (nil = 404).
	UI http.Handler
	// Edition is "core" or "ee".
	Edition string
	// SendNotification delivers channel notifications (test sends and, via the engine, run events).
	// Nil uses the minimal built-in slack/webhook senders (see notify_send.go).
	SendNotification ChannelSender
}

// Server implements gen.StrictServerInterface.
type Server struct {
	Deps
	mu           sync.RWMutex
	auditSinks   []func(context.Context, store.AuditEntry)
	ssoProviders []string
	extRoutes    []func(chi.Router)
	listenAddr   string
}

var _ gen.StrictServerInterface = (*Server)(nil)

// New builds a Server.
func New(d Deps) *Server {
	if d.Logger == nil {
		d.Logger = slog.Default()
	}
	if d.Metrics == nil {
		d.Metrics = telemetry.NewMetrics()
	}
	if d.Edition == "" {
		d.Edition = "core"
	}
	return &Server{Deps: d}
}

// AddAuditSink registers an additional audit consumer (ee).
func (s *Server) AddAuditSink(f func(context.Context, store.AuditEntry)) {
	s.mu.Lock()
	s.auditSinks = append(s.auditSinks, f)
	s.mu.Unlock()
}

// AddSSOProvider advertises an SSO provider in /v1/meta (ee).
func (s *Server) AddSSOProvider(name string) {
	s.mu.Lock()
	s.ssoProviders = append(s.ssoProviders, name)
	s.mu.Unlock()
}

// AddRoutes registers extra raw routes on the root router (ee). Call before Handler.
func (s *Server) AddRoutes(f func(chi.Router)) {
	s.mu.Lock()
	s.extRoutes = append(s.extRoutes, f)
	s.mu.Unlock()
}

// publicOps don't require authentication.
var publicOps = map[string]bool{
	"GetMeta":        true,
	"GetSetupStatus": true,
	"CompleteSetup":  true,
	"Login":          true,
}

// Handler builds the full HTTP handler.
func (s *Server) Handler() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID, middleware.RealIP, s.recoverer, s.requestLogger,
		s.Metrics.HTTPMiddleware(routePattern))

	// Operational endpoints (outside the generated server: no auth, plain handlers).
	r.Get("/healthz", s.handleHealthz)
	r.Get("/readyz", s.handleReadyz)
	if s.Config == nil || s.Config.Telemetry.Metrics {
		r.Handle("/metrics", s.Metrics.Handler())
	}
	// Built-in mock AI service (sample project, docs, smoke tests).
	if s.mockEnabled() {
		r.Mount("/_mock", http.StripPrefix("/_mock", s.mockHandler()))
	}
	// Inbound webhooks authenticate by HMAC, not sessions.
	r.Post("/hooks/{trigger}", s.handleHook)
	// SSO is an ee feature; core answers 501 unless an extension registers these routes.
	r.Get("/v1/auth/oidc/{provider}/start", s.handleOIDCNotAvailable)
	r.Get("/v1/auth/oidc/{provider}/callback", s.handleOIDCNotAvailable)
	s.mu.RLock()
	for _, f := range s.extRoutes {
		f(r)
	}
	s.mu.RUnlock()

	strict := gen.NewStrictHandlerWithOptions(s, []gen.StrictMiddlewareFunc{s.requireAuth}, gen.StrictHTTPServerOptions{
		RequestErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
			s.writeError(w, r, badRequest("invalid request body: %v", err))
		},
		ResponseErrorHandlerFunc: s.writeError,
	})
	gen.HandlerWithOptions(strict, gen.ChiServerOptions{
		BaseRouter: r.With(s.authenticate),
		ErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
			s.writeError(w, r, badRequest("%v", err))
		},
	})

	notFound := func(w http.ResponseWriter, r *http.Request) {
		if isAPIPath(r.URL.Path) || s.UI == nil {
			s.writeError(w, r, errorf(http.StatusNotFound, "not_found", "no route for %s %s", r.Method, r.URL.Path))
			return
		}
		s.UI.ServeHTTP(w, r)
	}
	r.NotFound(notFound)
	r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		if !isAPIPath(r.URL.Path) && r.Method == http.MethodGet {
			notFound(w, r)
			return
		}
		s.writeError(w, r, errorf(http.StatusMethodNotAllowed, "method_not_allowed", "%s not allowed on %s", r.Method, r.URL.Path))
	})
	return r
}

func isAPIPath(p string) bool {
	return strings.HasPrefix(p, "/v1/") || p == "/v1" || strings.HasPrefix(p, "/hooks/")
}

func routePattern(r *http.Request) string {
	if rc := chi.RouteContext(r.Context()); rc != nil {
		if p := rc.RoutePattern(); p != "" {
			return p
		}
	}
	if isAPIPath(r.URL.Path) {
		return "unmatched"
	}
	return "ui"
}

// authenticate resolves the principal and enforces CSRF for session-authenticated mutations.
func (s *Server) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, err := s.Auth.Authenticate(r)
		if err != nil {
			if err == auth.ErrUnauthenticated {
				// A stale cookie shouldn't block public endpoints like login: clear it and continue anonymously.
				if r.Header.Get("Authorization") == "" {
					s.Auth.ClearSessionCookies(w)
					next.ServeHTTP(w, r.WithContext(withRequestInfo(r.Context(), r)))
					return
				}
			}
			s.writeError(w, r, err)
			return
		}
		if err := auth.CheckCSRF(r, p); err != nil {
			s.writeError(w, r, err)
			return
		}
		ctx := withRequestInfo(r.Context(), r)
		if p != nil {
			ctx = auth.WithPrincipal(ctx, p)
		}
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// requireAuth rejects anonymous calls to non-public operations.
func (s *Server) requireAuth(f gen.StrictHandlerFunc, operationID string) gen.StrictHandlerFunc {
	return func(ctx context.Context, w http.ResponseWriter, r *http.Request, req any) (any, error) {
		if !publicOps[operationID] && auth.FromContext(ctx) == nil {
			return nil, rbac.ErrUnauthenticated
		}
		return f(ctx, w, r, req)
	}
}

type reqInfoKey struct{}

type reqInfo struct {
	UserAgent string
	IP        string
	RequestID string
}

func withRequestInfo(ctx context.Context, r *http.Request) context.Context {
	return context.WithValue(ctx, reqInfoKey{}, reqInfo{UserAgent: r.UserAgent(), IP: r.RemoteAddr,
		RequestID: middleware.GetReqID(r.Context())})
}

func requestInfo(ctx context.Context) reqInfo {
	ri, _ := ctx.Value(reqInfoKey{}).(reqInfo)
	return ri
}

func (s *Server) requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)
		if r.URL.Path == "/healthz" || r.URL.Path == "/readyz" || r.URL.Path == "/metrics" {
			return
		}
		lvl := slog.LevelDebug
		if isAPIPath(r.URL.Path) {
			lvl = slog.LevelInfo
		}
		if ww.Status() >= 500 {
			lvl = slog.LevelError
		}
		s.Logger.Log(r.Context(), lvl, "http", "method", r.Method, "path", r.URL.Path, "status", ww.Status(),
			"bytes", ww.BytesWritten(), "dur_ms", time.Since(start).Milliseconds(), "request_id", middleware.GetReqID(r.Context()))
	})
}

func (s *Server) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				if v == http.ErrAbortHandler {
					panic(v)
				}
				s.Logger.Error("panic", "err", v, "path", r.URL.Path, "stack", string(debugStack()))
				s.writeError(w, r, errorf(http.StatusInternalServerError, "internal", "internal error"))
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// ---- authorization helpers used by handlers ----

func (s *Server) principal(ctx context.Context) *auth.Principal { return auth.FromContext(ctx) }

// require checks the caller's role in an org (and project when non-nil).
func (s *Server) require(ctx context.Context, orgID uuid.UUID, projectID *uuid.UUID, min store.Role) error {
	return s.Authz.Require(ctx, s.principal(ctx), orgID, projectID, min)
}

// requireSession rejects API keys for user-only operations.
func (s *Server) requireSession(ctx context.Context) (*auth.Principal, error) {
	p := s.principal(ctx)
	if p == nil {
		return nil, rbac.ErrUnauthenticated
	}
	if p.User == nil {
		return nil, errorf(http.StatusForbidden, "forbidden", "this operation requires a user session")
	}
	return p, nil
}

// project loads a project and checks the caller's role in it.
func (s *Server) project(ctx context.Context, id uuid.UUID, min store.Role) (store.Project, error) {
	p, err := s.Store.GetProject(ctx, id)
	if err != nil {
		return p, err
	}
	return p, s.require(ctx, p.OrgID, &p.ID, min)
}

// audit records a security-relevant action (stored in core; ee sinks may forward it).
func (s *Server) audit(ctx context.Context, orgID uuid.UUID, action, target string, diff any) {
	p := s.principal(ctx)
	e := store.AuditEntry{OrgID: orgID, ActorID: p.UserID(), Actor: p.Actor(), Action: action, Target: target}
	if diff != nil {
		e.Diff = mustJSON(diff)
	}
	if err := s.Store.AppendAudit(context.WithoutCancel(ctx), &e); err != nil {
		s.Logger.Warn("audit append failed", "action", action, "err", err)
	}
	s.mu.RLock()
	sinks := s.auditSinks
	s.mu.RUnlock()
	for _, f := range sinks {
		f(ctx, e)
	}
}
