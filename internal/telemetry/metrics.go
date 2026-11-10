package telemetry

import (
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Metrics holds every airval Prometheus collector. Create once per process with NewMetrics.
type Metrics struct {
	Registry *prometheus.Registry

	HTTPRequests *prometheus.CounterVec   // route, method, code
	HTTPDuration *prometheus.HistogramVec // route, method

	ExamplesProcessed *prometheus.CounterVec   // status
	QueueLag          *prometheus.GaugeVec     // stream/consumer
	TargetLatency     *prometheus.HistogramVec // target kind
	TargetThrottled   *prometheus.CounterVec   // target kind (429 / Retry-After)
	JudgeCostUSD      *prometheus.CounterVec   // provider
	DBFlushLatency    prometheus.Histogram
	RunsFinished      *prometheus.CounterVec // status
}

// NewMetrics registers collectors on a fresh registry (plus Go/process collectors).
func NewMetrics() *Metrics {
	r := prometheus.NewRegistry()
	r.MustRegister(collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	m := &Metrics{
		Registry: r,
		HTTPRequests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "airval", Name: "http_requests_total", Help: "HTTP requests by route, method and status code.",
		}, []string{"route", "method", "code"}),
		HTTPDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "airval", Name: "http_request_duration_seconds", Help: "HTTP request latency.",
			Buckets: prometheus.DefBuckets,
		}, []string{"route", "method"}),
		ExamplesProcessed: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "airval", Name: "examples_processed_total", Help: "Examples processed by workers, by result status.",
		}, []string{"status"}),
		QueueLag: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: "airval", Name: "queue_pending_messages", Help: "Pending JetStream messages per consumer.",
		}, []string{"consumer"}),
		TargetLatency: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "airval", Name: "target_latency_seconds", Help: "Target call latency.",
			Buckets: []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10, 30, 60},
		}, []string{"kind"}),
		TargetThrottled: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "airval", Name: "target_throttled_total", Help: "Target responses that were rate limited (429/Retry-After).",
		}, []string{"kind"}),
		JudgeCostUSD: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "airval", Name: "judge_cost_usd_total", Help: "LLM judge spend in USD.",
		}, []string{"provider"}),
		DBFlushLatency: prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace: "airval", Name: "db_flush_duration_seconds", Help: "Result batch flush latency.",
			Buckets: []float64{.001, .0025, .005, .01, .025, .05, .1, .25, .5, 1},
		}),
		RunsFinished: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "airval", Name: "runs_finished_total", Help: "Finished runs by status.",
		}, []string{"status"}),
	}
	r.MustRegister(m.HTTPRequests, m.HTTPDuration, m.ExamplesProcessed, m.QueueLag, m.TargetLatency,
		m.TargetThrottled, m.JudgeCostUSD, m.DBFlushLatency, m.RunsFinished)
	return m
}

// Handler serves /metrics.
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.Registry, promhttp.HandlerOpts{Registry: m.Registry})
}

// HTTPMiddleware records request counts and latency. route returns a low-cardinality route label.
func (m *Metrics) HTTPMiddleware(route func(*http.Request) string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			sw := &statusWriter{ResponseWriter: w, code: http.StatusOK}
			next.ServeHTTP(sw, r)
			rt := route(r)
			m.HTTPRequests.WithLabelValues(rt, r.Method, strconv.Itoa(sw.code)).Inc()
			m.HTTPDuration.WithLabelValues(rt, r.Method).Observe(time.Since(start).Seconds())
		})
	}
}

type statusWriter struct {
	http.ResponseWriter
	code int
}

func (w *statusWriter) WriteHeader(code int) {
	w.code = code
	w.ResponseWriter.WriteHeader(code)
}

// Flush keeps SSE working through the middleware.
func (w *statusWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Unwrap lets http.ResponseController reach the underlying writer.
func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
