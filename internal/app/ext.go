package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/prat31/airval/internal/auth"
	"github.com/prat31/airval/internal/config"
	"github.com/prat31/airval/internal/ext"
	"github.com/prat31/airval/internal/rbac"
	"github.com/prat31/airval/internal/secrets"
	"github.com/prat31/airval/internal/store"
)

// host implements ext.Host on top of the app.
type host struct{ a *App }

func (h *host) Router() chi.Router {
	// Routes are collected on a sub-router and replayed onto the root router when the handler is built.
	r := chi.NewRouter()
	h.a.API.AddRoutes(func(root chi.Router) {
		_ = chi.Walk(r, func(method, route string, h http.Handler, mws ...func(http.Handler) http.Handler) error {
			root.With(mws...).Method(method, route, h)
			return nil
		})
	})
	return r
}

func (h *host) Store() store.Store                          { return h.a.Store }
func (h *host) Auth() *auth.Service                         { return h.a.Auth }
func (h *host) Authz() *rbac.Authorizer                     { return h.a.Authz }
func (h *host) Keyring() *secrets.Keyring                   { return h.a.Keyring }
func (h *host) Config() *config.Config                      { return h.a.Config }
func (h *host) Logger() *slog.Logger                        { return h.a.Logger }
func (h *host) Authenticate(next http.Handler) http.Handler { return h.a.API.Authenticate(next) }
func (h *host) WriteError(w http.ResponseWriter, r *http.Request, err error) {
	h.a.API.WriteError(w, r, err)
}
func (h *host) SetRoleOverride(o rbac.Override)          { h.a.Authz.Override = o }
func (h *host) SetDecider(d rbac.Decider)                { h.a.Authz.Decider = d }
func (h *host) AddSSOProvider(name string)               { h.a.API.AddSSOProvider(name) }
func (h *host) AddAuditSink(s ext.AuditSink)             { h.a.API.AddAuditSink(s) }
func (h *host) AddCallObserver(o ext.CallObserver)       { h.a.API.AddCallObserver(o) }
func (h *host) SetAuthProviders(f ext.AuthProvidersFunc) { h.a.API.SetAuthProviders(f) }
func (h *host) EnableFeatures(features ...string)        { h.a.API.EnableFeatures(features...) }

func (h *host) Go(name string, fn func(ctx context.Context)) {
	h.a.background = append(h.a.background, backgroundTask{name: name, fn: fn})
}

func (h *host) RunAsLeader(ctx context.Context, lease string, fn func(ctx context.Context)) {
	h.a.Queue.RunAsLeader(ctx, lease, h.a.workerID(), 30*time.Second, fn)
}

// backgroundTask is a long-running extension task started with the HTTP server.
type backgroundTask struct {
	name string
	fn   func(ctx context.Context)
}

func (a *App) initExtensions(ctx context.Context) error {
	for _, e := range ext.All() {
		if err := e.Init(ctx, &host{a: a}); err != nil {
			return fmt.Errorf("extension %s: %w", e.Name(), err)
		}
		a.Logger.Info("extension loaded", "name", e.Name())
	}
	return nil
}
