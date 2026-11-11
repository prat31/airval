package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/airval/airval/internal/api"
	"github.com/airval/airval/internal/evalcore"
	"github.com/airval/airval/internal/notify"
	"github.com/airval/airval/internal/planner"
	"github.com/airval/airval/internal/runner"
	"github.com/airval/airval/internal/scheduler"
	"github.com/airval/airval/internal/secrets"
	"github.com/airval/airval/internal/store"
	"github.com/airval/airval/internal/targets"
)

func splitCIDRs(v string) []string {
	var out []string
	for _, p := range strings.Split(v, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// clientFactory builds the shared outbound HTTP client factory with the configured SSRF guard.
func (a *App) clientFactory() *targets.ClientFactory {
	if a.clients != nil {
		return a.clients
	}
	g, err := targets.NewSSRFGuard(targets.NetworkPolicy{AllowCIDRs: splitCIDRs(a.Config.Security.AllowCIDRs),
		DenyCIDRs: splitCIDRs(a.Config.Security.DenyCIDRs)})
	if err != nil {
		a.Logger.Warn("invalid security CIDR config; using the default outbound policy", "err", err)
		g = nil
	}
	a.clients = targets.NewClientFactory(g)
	return a.clients
}

// guardedTransport routes every request through the SSRF-guarded per-host clients.
type guardedTransport struct{ f *targets.ClientFactory }

func (t guardedTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	host := r.URL.Hostname()
	port := r.URL.Port()
	if port == "" {
		port = map[string]string{"https": "443", "http": "80"}[r.URL.Scheme]
	}
	return t.f.Client(net.JoinHostPort(host, port), targets.ClientOptions{Concurrency: 8}).Transport.RoundTrip(r)
}

// notifier builds the run notifier (Slack, webhooks, email) with secrets resolved per org/project.
func (a *App) notifier() *notify.Notifier {
	if a.notif != nil {
		return a.notif
	}
	a.notif = &notify.Notifier{Store: a.Store, Logger: a.Logger,
		HTTP: &http.Client{Timeout: 15 * time.Second, Transport: guardedTransport{a.clientFactory()}},
		Secrets: func(ctx context.Context, orgID uuid.UUID, projectID *uuid.UUID, name string) (string, error) {
			r := &secrets.Resolver{Ctx: ctx, Store: a.Store, Keyring: a.Keyring, OrgID: orgID, ProjectID: projectID}
			return r.Resolve(name)
		}}
	return a.notif
}

// channelSender adapts internal/notify to the API's notification-test hook.
func (a *App) channelSender() api.ChannelSender {
	n := a.notifier()
	return func(ctx context.Context, ch store.NotificationChannel, sec evalcore.SecretResolver, msg api.ChannelMessage) error {
		return n.SendMessage(ctx, ch, sec, notify.Message{Event: msg.Event, Title: msg.Title, Text: msg.Text, URL: msg.URL,
			Fields: msg.Fields, At: msg.At})
	}
}

func (a *App) workerID() string {
	host, _ := os.Hostname()
	return fmt.Sprintf("%s-%d", host, os.Getpid())
}

// NewWorker builds the execution engine wired to the app's store, queue, notifier and clients.
func (a *App) NewWorker() *runner.Engine {
	return runner.NewEngine(runner.Deps{Store: a.Store, Queue: a.Queue, Keyring: a.Keyring, Config: a.Config.Workers,
		Logger: a.Logger, Metrics: a.Metrics, WorkerID: a.workerID(), BaseURL: a.Config.Server.BaseURL,
		Clients: a.clientFactory(), Notifier: a.notifier()})
}

// Planner returns a run planner bound to the app's store and queue.
func (a *App) Planner() *planner.Service {
	return planner.New(planner.Deps{Store: a.Store, Queue: a.Queue, Logger: a.Logger})
}

// runScheduler fires schedule triggers while this replica holds the scheduler lease.
func (a *App) runScheduler(ctx context.Context) {
	scheduler.New(scheduler.Deps{Store: a.Store, Queue: a.Queue, Planner: a.Planner(), Logger: a.Logger,
		Holder: a.workerID()}).Run(ctx)
}

// serveWorkerOps serves /healthz, /readyz and /metrics for `airval worker` (Kubernetes probes).
func (a *App) serveWorkerOps(ctx context.Context, addr string) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		rctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		checks := map[string]string{"database": "ok", "nats": "ok"}
		status := http.StatusOK
		if err := a.Store.Ping(rctx); err != nil {
			checks["database"], status = err.Error(), http.StatusServiceUnavailable
		}
		if err := a.Queue.Ready(rctx); err != nil {
			checks["nats"], status = err.Error(), http.StatusServiceUnavailable
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]any{"status": map[bool]string{true: "ok", false: "unavailable"}[status == 200],
			"checks": checks})
	})
	if a.Config.Telemetry.Metrics {
		mux.Handle("/metrics", a.Metrics.Handler())
	}
	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("worker listener %s: %w", addr, err)
	}
	a.Logger.Info("worker ops listener", "addr", ln.Addr().String())
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(sctx)
	}()
	if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
