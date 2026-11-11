// Package exttest runs the API server with extensions in-process for tests (SQLite store, no NATS).
package exttest

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/prat31/airval/internal/api"
	"github.com/prat31/airval/internal/auth"
	"github.com/prat31/airval/internal/config"
	"github.com/prat31/airval/internal/ext"
	"github.com/prat31/airval/internal/rbac"
	"github.com/prat31/airval/internal/secrets"
	"github.com/prat31/airval/internal/store"
	"github.com/prat31/airval/internal/store/sqlstore"
)

// Env is a running test server.
type Env struct {
	Server  *httptest.Server
	API     *api.Server
	Store   store.Store
	Auth    *auth.Service
	Authz   *rbac.Authorizer
	Keyring *secrets.Keyring
	Config  *config.Config
	// Tasks are the background tasks extensions registered with Host.Go (not started automatically).
	Tasks map[string]func(ctx context.Context)
}

// Options tweak the environment.
type Options struct {
	// Configure may adjust the config before the server starts (BaseURL is set to the test server URL).
	Configure func(*config.Config)
}

// Start opens a migrated SQLite store, initializes exts against it and serves the API.
func Start(t testing.TB, exts []ext.Extension, opts Options) *Env {
	t.Helper()
	st, err := sqlstore.Open(context.Background(), filepath.Join(t.TempDir(), "airval.db"), sqlstore.Options{Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	kr, err := secrets.NewKeyring(secrets.GenerateKey(), 1, "")
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	var handler http.Handler
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { handler.ServeHTTP(w, r) }))
	t.Cleanup(srv.Close)
	cfg := &config.Config{Server: config.Server{BaseURL: srv.URL}, Telemetry: config.Telemetry{Metrics: false},
		Auth: config.Auth{SessionTTL: 24 * time.Hour}}
	if opts.Configure != nil {
		opts.Configure(cfg)
	}
	e := &Env{Server: srv, Store: st, Keyring: kr, Config: cfg, Tasks: map[string]func(context.Context){}}
	e.Auth = &auth.Service{Store: st, Cfg: cfg.Auth, Log: log}
	e.Authz = &rbac.Authorizer{Store: st}
	edition := "core"
	if len(exts) > 0 {
		edition = "ee"
	}
	e.API = api.New(api.Deps{Store: st, Keyring: kr, Auth: e.Auth, Authz: e.Authz, Config: cfg, Logger: log,
		Version: "test", Edition: edition})
	h := &host{e: e, log: log}
	for _, x := range exts {
		if err := x.Init(context.Background(), h); err != nil {
			t.Fatal(err)
		}
	}
	handler = e.API.Handler()
	return e
}

type host struct {
	e   *Env
	log *slog.Logger
}

func (h *host) Router() chi.Router {
	r := chi.NewRouter()
	h.e.API.AddRoutes(func(root chi.Router) {
		_ = chi.Walk(r, func(method, route string, hd http.Handler, mws ...func(http.Handler) http.Handler) error {
			root.With(mws...).Method(method, route, hd)
			return nil
		})
	})
	return r
}

func (h *host) Store() store.Store                          { return h.e.Store }
func (h *host) Auth() *auth.Service                         { return h.e.Auth }
func (h *host) Authz() *rbac.Authorizer                     { return h.e.Authz }
func (h *host) Keyring() *secrets.Keyring                   { return h.e.Keyring }
func (h *host) Config() *config.Config                      { return h.e.Config }
func (h *host) Logger() *slog.Logger                        { return h.log }
func (h *host) Authenticate(next http.Handler) http.Handler { return h.e.API.Authenticate(next) }
func (h *host) WriteError(w http.ResponseWriter, r *http.Request, err error) {
	h.e.API.WriteError(w, r, err)
}
func (h *host) SetRoleOverride(o rbac.Override)                                         { h.e.Authz.Override = o }
func (h *host) SetDecider(d rbac.Decider)                                               { h.e.Authz.Decider = d }
func (h *host) AddAuditSink(s ext.AuditSink)                                            { h.e.API.AddAuditSink(s) }
func (h *host) AddCallObserver(o ext.CallObserver)                                      { h.e.API.AddCallObserver(o) }
func (h *host) AddSSOProvider(name string)                                              { h.e.API.AddSSOProvider(name) }
func (h *host) SetAuthProviders(f ext.AuthProvidersFunc)                                { h.e.API.SetAuthProviders(f) }
func (h *host) EnableFeatures(features ...string)                                       { h.e.API.EnableFeatures(features...) }
func (h *host) Go(name string, fn func(ctx context.Context))                            { h.e.Tasks[name] = fn }
func (h *host) RunAsLeader(ctx context.Context, _ string, fn func(ctx context.Context)) { fn(ctx) }

var _ ext.Host = (*host)(nil)
