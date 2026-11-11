// Package app wires configuration, telemetry, store, queue, auth, API, UI and worker into the
// process modes of the airval binary (dev, server, worker).
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/prat31/airval/internal/api"
	"github.com/prat31/airval/internal/auth"
	"github.com/prat31/airval/internal/config"
	"github.com/prat31/airval/internal/ext"
	"github.com/prat31/airval/internal/notify"
	"github.com/prat31/airval/internal/queue"
	"github.com/prat31/airval/internal/rbac"
	"github.com/prat31/airval/internal/secrets"
	"github.com/prat31/airval/internal/store"
	"github.com/prat31/airval/internal/store/sqlstore"
	"github.com/prat31/airval/internal/targets"
	"github.com/prat31/airval/internal/telemetry"
	"github.com/prat31/airval/internal/webui"
)

// Build info, set by main from -ldflags.
type BuildInfo struct {
	Version string
	Commit  string
}

// App holds the process's long-lived components.
type App struct {
	Mode    config.Mode
	Config  *config.Config
	Logger  *slog.Logger
	Metrics *telemetry.Metrics
	Store   *sqlstore.Store
	Queue   *queue.Queue
	Keyring *secrets.Keyring
	Auth    *auth.Service
	Authz   *rbac.Authorizer
	API     *api.Server
	Build   BuildInfo

	clients    *targets.ClientFactory
	background []backgroundTask
	notif      *notify.Notifier
	closers    []func()
}

// OpenStore opens the database from config (used by `migrate` without the rest of the app).
func OpenStore(ctx context.Context, c *config.Config, log *slog.Logger, migrate bool) (*sqlstore.Store, error) {
	return sqlstore.Open(ctx, c.Database.URL, sqlstore.Options{Migrate: migrate, Logger: log, MaxOpenConns: c.Database.MaxOpenConns})
}

// Keyring builds the secrets keyring; in dev mode a key is generated and persisted when unset.
func Keyring(mode config.Mode, c *config.Config, log *slog.Logger) (*secrets.Keyring, error) {
	key := c.Secrets.MasterKey
	if key == "" && mode == config.ModeDev {
		path := filepath.Join(config.DataDir, "master.key")
		k, err := secrets.LoadOrCreateKeyFile(path)
		if err != nil {
			return nil, fmt.Errorf("dev master key: %w", err)
		}
		log.Info("using dev secrets master key", "path", path)
		key = k
	}
	if key == "" {
		return nil, errors.New("secrets.master_key (AIRVAL_SECRETS_MASTER_KEY) is required")
	}
	return secrets.NewKeyring(key, c.Secrets.KeyVersion, c.Secrets.PreviousKeys)
}

// New builds the app for mode. Call Close when done.
func New(ctx context.Context, mode config.Mode, c *config.Config, bi BuildInfo) (*App, error) {
	a := &App{Mode: mode, Config: c, Build: bi}
	a.Logger = telemetry.NewLogger(c.Telemetry, os.Stderr)
	slog.SetDefault(a.Logger)
	a.Metrics = telemetry.NewMetrics()
	shutdownTracing, err := telemetry.SetupTracing(ctx, c.Telemetry, bi.Version)
	if err != nil {
		return nil, err
	}
	a.closers = append(a.closers, func() {
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = shutdownTracing(sctx)
	})
	if a.Keyring, err = Keyring(mode, c, a.Logger); err != nil {
		return nil, err
	}
	if a.Store, err = OpenStore(ctx, c, a.Logger, c.Database.AutoMigrate && mode != config.ModeWorker); err != nil {
		a.Close()
		return nil, err
	}
	a.closers = append(a.closers, func() { _ = a.Store.Close() })
	qo := queue.Options{URL: c.NATS.URL, CredsFile: c.NATS.CredsFile, StoreDir: c.NATS.StoreDir, Port: c.NATS.Port,
		Replicas: c.NATS.Replicas, EventsMaxAge: time.Duration(c.NATS.StreamMaxAgeHrs) * time.Hour, Logger: a.Logger,
		Name: "airval-" + string(mode)}
	if c.NATS.URL == "" && mode != config.ModeDev {
		a.Logger.Warn("nats.url is empty: starting an embedded NATS server (not suitable for multi-replica deployments)")
	}
	if a.Queue, err = queue.Open(ctx, qo); err != nil {
		a.Close()
		return nil, err
	}
	a.closers = append(a.closers, a.Queue.Close)
	a.Auth = &auth.Service{Store: a.Store, Cfg: c.Auth, Log: a.Logger}
	a.Authz = &rbac.Authorizer{Store: a.Store}
	if mode != config.ModeWorker {
		if err := a.Auth.Bootstrap(ctx, c.Bootstrap); err != nil {
			a.Close()
			return nil, err
		}
		var ui http.Handler
		if c.Server.UIDir != "" {
			ui = webui.Handler(webui.Dir(c.Server.UIDir))
		} else {
			ui = webui.Handler(webui.Embedded())
		}
		a.API = api.New(api.Deps{Store: a.Store, Queue: a.Queue, Keyring: a.Keyring, Auth: a.Auth, Authz: a.Authz,
			Config: c, Logger: a.Logger, Metrics: a.Metrics, Version: bi.Version, Commit: bi.Commit, UI: ui,
			Edition: ext.Edition(), SendNotification: a.channelSender()})
		if err := a.initExtensions(ctx); err != nil {
			a.Close()
			return nil, err
		}
	}
	return a, nil
}

// Close releases resources in reverse order.
func (a *App) Close() {
	for i := len(a.closers) - 1; i >= 0; i-- {
		a.closers[i]()
	}
	a.closers = nil
}

// Run serves until ctx is cancelled: the HTTP server (dev/server) and the worker (worker, or dev
// with workers.embedded). It shuts down gracefully.
func (a *App) Run(ctx context.Context) error {
	g, ctx := errgroup.WithContext(ctx)
	if a.API != nil {
		srv := &http.Server{Addr: a.Config.Server.Addr, Handler: a.API.Handler(), ReadHeaderTimeout: 10 * time.Second,
			BaseContext: func(net.Listener) context.Context { return context.WithoutCancel(ctx) }}
		ln, err := net.Listen("tcp", a.Config.Server.Addr)
		if err != nil {
			return err
		}
		a.Logger.Info("airval listening", "addr", ln.Addr().String(), "url", a.Config.Server.BaseURL, "mode", a.Mode,
			"db", a.Store.Dialect(), "nats_embedded", a.Queue.Embedded(), "version", a.Build.Version)
		g.Go(func() error {
			if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
				return err
			}
			return nil
		})
		g.Go(func() error {
			<-ctx.Done()
			sctx, cancel := context.WithTimeout(context.Background(), a.Config.Server.ShutdownTimeout)
			defer cancel()
			a.Logger.Info("shutting down http server")
			return srv.Shutdown(sctx)
		})
		g.Go(func() error { a.housekeeping(ctx); return nil })
		g.Go(func() error { a.runScheduler(ctx); return nil })
		for _, t := range a.background {
			a.Logger.Debug("starting extension task", "task", t.name)
			g.Go(func() error { t.fn(ctx); return nil })
		}
	}
	if a.Mode == config.ModeWorker || a.Config.Workers.Embedded {
		w := a.NewWorker()
		g.Go(func() error { return w.Run(ctx) })
	}
	if a.Mode == config.ModeWorker && a.Config.Worker.Listen != "" {
		g.Go(func() error { return a.serveWorkerOps(ctx, a.Config.Worker.Listen) })
	}
	return g.Wait()
}

// housekeeping purges expired sessions and cache entries periodically.
func (a *App) housekeeping(ctx context.Context) {
	t := time.NewTicker(10 * time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			now := time.Now()
			if _, err := a.Store.DeleteExpiredSessions(ctx, now); err != nil && ctx.Err() == nil {
				a.Logger.Warn("purge sessions", "err", err)
			}
			if _, err := a.Store.CachePurgeExpired(ctx, now); err != nil && ctx.Err() == nil {
				a.Logger.Warn("purge cache", "err", err)
			}
		}
	}
}

var _ store.Store = (*sqlstore.Store)(nil)
