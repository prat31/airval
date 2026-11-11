package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/airval/airval/internal/auth"
	"github.com/airval/airval/internal/config"
	"github.com/airval/airval/internal/ext"
	"github.com/airval/airval/internal/rbac"
	"github.com/airval/airval/internal/store"
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

func (h *host) Store() store.Store              { return h.a.Store }
func (h *host) Auth() *auth.Service             { return h.a.Auth }
func (h *host) Config() *config.Config          { return h.a.Config }
func (h *host) Logger() *slog.Logger            { return h.a.Logger }
func (h *host) SetRoleOverride(o rbac.Override) { h.a.Authz.Override = o }
func (h *host) AddSSOProvider(name string)      { h.a.API.AddSSOProvider(name) }
func (h *host) AddAuditSink(s ext.AuditSink)    { h.a.API.AddAuditSink(s) }

func (a *App) initExtensions(ctx context.Context) error {
	for _, e := range ext.All() {
		if err := e.Init(ctx, &host{a: a}); err != nil {
			return fmt.Errorf("extension %s: %w", e.Name(), err)
		}
		a.Logger.Info("extension loaded", "name", e.Name())
	}
	return nil
}
