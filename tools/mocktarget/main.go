// Command mocktarget runs a fake AI service for trying out airval locally and
// for benchmarks: go run ./tools/mocktarget -addr :9090
package main

import (
	"flag"
	"log"
	"net/http"
	"time"

	"github.com/airval/airval/tools/mocktarget/mock"
)

func main() {
	cfg := mock.DefaultConfig()
	addr := flag.String("addr", ":9090", "listen address")
	flag.DurationVar(&cfg.LatencyMean, "latency", 0, "mean artificial latency per request")
	flag.DurationVar(&cfg.LatencyJitter, "jitter", 0, "uniform +/- latency jitter")
	flag.Float64Var(&cfg.ErrorRate, "error-rate", 0, "fraction of requests failing with 5xx (0..1)")
	flag.Float64Var(&cfg.RateLimitRate, "rate-limit-rate", 0, "fraction of requests answered 429 (0..1)")
	flag.DurationVar(&cfg.RetryAfter, "retry-after", time.Second, "Retry-After advertised on 429")
	flag.Float64Var(&cfg.Accuracy, "accuracy", 0.8, "fraction of questions answered correctly (0..1; negative = 0)")
	flag.Uint64Var(&cfg.Seed, "seed", 1, "random seed for latency/error injection")
	flag.DurationVar(&cfg.ChunkDelay, "chunk-delay", 0, "delay between streamed chunks")
	flag.Parse()

	srv := &http.Server{Addr: *addr, Handler: mock.NewServer(cfg), ReadHeaderTimeout: 10 * time.Second}
	log.Printf("mocktarget listening on %s (latency=%s±%s errors=%.2f 429=%.2f accuracy=%.2f)",
		*addr, cfg.LatencyMean, cfg.LatencyJitter, cfg.ErrorRate, cfg.RateLimitRate, cfg.Accuracy)
	log.Fatal(srv.ListenAndServe())
}
