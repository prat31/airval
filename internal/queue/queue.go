// Package queue wraps NATS JetStream: an embedded server for `airval dev` or an external cluster,
// the run job work-queue, per-run event streams, and KV-backed coordination primitives
// (leases for leader election / finalizer locks, cancel flags, distributed token buckets).
package queue

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// Stream, subject and bucket names.
const (
	StreamJobs    = "RUNS_JOBS"
	SubjectJobs   = "runs.jobs"
	StreamEvents  = "RUNS_EVENTS"
	SubjectEvents = "runs.events" // runs.events.<runID>
	ConsumerJobs  = "workers"

	BucketLeases  = "airval_leases"
	BucketCancels = "airval_cancels"
	BucketLimits  = "airval_ratelimit"
)

// EventsSubject is the subject carrying progress events for one run.
func EventsSubject(runID string) string { return SubjectEvents + "." + runID }

// Options configures Open.
type Options struct {
	// URL of an external NATS server/cluster; empty starts an embedded server.
	URL       string
	CredsFile string
	// StoreDir is the embedded JetStream directory (empty = temp dir).
	StoreDir string
	// Port for the embedded server; < 0 means in-process only (no TCP listener).
	Port int
	// Replicas for streams/buckets (1 for embedded; 3 in clustered production).
	Replicas int
	// EventsMaxAge bounds run event retention.
	EventsMaxAge time.Duration
	// JobAckWait is how long a worker may hold a job before redelivery (extend with InProgress).
	JobAckWait time.Duration
	// JobMaxDeliver bounds redeliveries of a failing job.
	JobMaxDeliver int
	Logger        *slog.Logger
	Name          string // client connection name
}

// Queue is a connected JetStream client plus the provisioned streams and buckets.
type Queue struct {
	nc   *nats.Conn
	js   jetstream.JetStream
	srv  *server.Server
	opts Options
	log  *slog.Logger

	leases  jetstream.KeyValue
	cancels jetstream.KeyValue
	limits  jetstream.KeyValue
}

// Open connects (starting the embedded server when URL is empty) and provisions streams and buckets.
func Open(ctx context.Context, o Options) (*Queue, error) {
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	if o.Replicas <= 0 {
		o.Replicas = 1
	}
	if o.EventsMaxAge <= 0 {
		o.EventsMaxAge = 72 * time.Hour
	}
	if o.JobAckWait <= 0 {
		o.JobAckWait = 2 * time.Minute
	}
	if o.JobMaxDeliver <= 0 {
		o.JobMaxDeliver = 5
	}
	if o.Name == "" {
		o.Name = "airval"
	}
	q := &Queue{opts: o, log: o.Logger}
	connOpts := []nats.Option{nats.Name(o.Name), nats.MaxReconnects(-1), nats.ReconnectWait(time.Second)}
	url := o.URL
	if url == "" {
		srv, err := startEmbedded(o)
		if err != nil {
			return nil, err
		}
		q.srv = srv
		connOpts = append(connOpts, nats.InProcessServer(srv))
	} else if o.CredsFile != "" {
		connOpts = append(connOpts, nats.UserCredentials(o.CredsFile))
	}
	nc, err := nats.Connect(url, connOpts...)
	if err != nil {
		q.shutdownServer()
		return nil, fmt.Errorf("queue: connect: %w", err)
	}
	q.nc = nc
	js, err := jetstream.New(nc)
	if err != nil {
		q.Close()
		return nil, err
	}
	q.js = js
	if err := q.provision(ctx); err != nil {
		q.Close()
		return nil, err
	}
	return q, nil
}

func startEmbedded(o Options) (*server.Server, error) {
	dir := o.StoreDir
	if dir == "" {
		d, err := os.MkdirTemp("", "airval-nats-*")
		if err != nil {
			return nil, err
		}
		dir = d
	} else if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, err
	}
	sopts := &server.Options{
		ServerName: "airval-embedded",
		JetStream:  true,
		StoreDir:   dir,
		Host:       "127.0.0.1",
		Port:       o.Port,
		NoSigs:     true,
		NoLog:      true,
	}
	if o.Port < 0 {
		sopts.DontListen = true
		sopts.Port = 0
	}
	srv, err := server.NewServer(sopts)
	if err != nil {
		return nil, fmt.Errorf("queue: embedded server: %w", err)
	}
	go srv.Start()
	if !srv.ReadyForConnections(15 * time.Second) {
		srv.Shutdown()
		return nil, errors.New("queue: embedded NATS server did not become ready")
	}
	return srv, nil
}

func (q *Queue) provision(ctx context.Context) error {
	_, err := q.js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:        StreamJobs,
		Description: "airval run shard jobs (work queue)",
		Subjects:    []string{SubjectJobs},
		Retention:   jetstream.WorkQueuePolicy,
		Storage:     jetstream.FileStorage,
		Replicas:    q.opts.Replicas,
		Duplicates:  10 * time.Minute,
	})
	if err != nil {
		return fmt.Errorf("queue: stream %s: %w", StreamJobs, err)
	}
	_, err = q.js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:        StreamEvents,
		Description: "airval run progress events",
		Subjects:    []string{SubjectEvents + ".>"},
		Retention:   jetstream.LimitsPolicy,
		Storage:     jetstream.FileStorage,
		Replicas:    q.opts.Replicas,
		MaxAge:      q.opts.EventsMaxAge,
		Discard:     jetstream.DiscardOld,
	})
	if err != nil {
		return fmt.Errorf("queue: stream %s: %w", StreamEvents, err)
	}
	_, err = q.js.CreateOrUpdateConsumer(ctx, StreamJobs, jetstream.ConsumerConfig{
		Durable:       ConsumerJobs,
		AckPolicy:     jetstream.AckExplicitPolicy,
		AckWait:       q.opts.JobAckWait,
		MaxDeliver:    q.opts.JobMaxDeliver,
		MaxAckPending: 10_000,
		FilterSubject: SubjectJobs,
	})
	if err != nil {
		return fmt.Errorf("queue: consumer: %w", err)
	}
	kv := func(name, desc string, ttl time.Duration) (jetstream.KeyValue, error) {
		b, err := q.js.CreateOrUpdateKeyValue(ctx, jetstream.KeyValueConfig{
			Bucket: name, Description: desc, History: 1, TTL: ttl, Replicas: q.opts.Replicas, Storage: jetstream.FileStorage,
		})
		if err != nil {
			return nil, fmt.Errorf("queue: kv %s: %w", name, err)
		}
		return b, nil
	}
	if q.leases, err = kv(BucketLeases, "leases (leader election, finalizer locks)", 0); err != nil {
		return err
	}
	if q.cancels, err = kv(BucketCancels, "run cancel flags", 7*24*time.Hour); err != nil {
		return err
	}
	if q.limits, err = kv(BucketLimits, "distributed token buckets", 24*time.Hour); err != nil {
		return err
	}
	return nil
}

// Conn returns the NATS connection.
func (q *Queue) Conn() *nats.Conn { return q.nc }

// JetStream returns the JetStream context.
func (q *Queue) JetStream() jetstream.JetStream { return q.js }

// Embedded reports whether this process runs the NATS server.
func (q *Queue) Embedded() bool { return q.srv != nil }

// ClientURL returns the embedded server's URL when it listens on TCP (empty otherwise).
func (q *Queue) ClientURL() string {
	if q.srv == nil || q.opts.Port < 0 {
		return ""
	}
	return q.srv.ClientURL()
}

// Ready checks connectivity and JetStream availability (for /readyz).
func (q *Queue) Ready(ctx context.Context) error {
	if q.nc == nil || !q.nc.IsConnected() {
		return errors.New("queue: not connected")
	}
	_, err := q.js.AccountInfo(ctx)
	return err
}

// Close drains the connection and stops the embedded server.
func (q *Queue) Close() {
	if q.nc != nil {
		_ = q.nc.Drain()
		// Wait briefly for drain to finish.
		for i := 0; i < 50 && !q.nc.IsClosed(); i++ {
			time.Sleep(20 * time.Millisecond)
		}
		q.nc.Close()
	}
	q.shutdownServer()
}

func (q *Queue) shutdownServer() {
	if q.srv != nil {
		q.srv.Shutdown()
		q.srv.WaitForShutdown()
	}
}
