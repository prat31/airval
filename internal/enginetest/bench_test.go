package enginetest

import (
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/prat31/airval/tools/mocktarget/mock"
)

// BenchmarkEngine reproduces plan §3.4: mock target with 10 ms latency and no rate limit,
// one worker × 256 concurrency, 3 deterministic evaluators, persistence on Postgres
// (AIRVAL_TEST_PG_URL; SQLite otherwise). It reports end-to-end examples/min and the engine's
// per-example overhead (time spent outside the target call: queueing between stages,
// evaluation, buffering) at p50/p95/p99, plus the amortized database cost per row.
//
// Acceptance: ≥ 5,000 examples/min and overhead p95 < 2 ms.
func BenchmarkEngine(b *testing.B) {
	n := 20_000
	if v, err := strconv.Atoi(os.Getenv("AIRVAL_BENCH_EXAMPLES")); err == nil && v > 0 {
		n = v
	}
	db := os.Getenv("AIRVAL_TEST_PG_URL")
	name := "postgres"
	if db == "" {
		name = "sqlite"
	}
	b.Run(name, func(b *testing.B) {
		h := Start(b, Options{DatabaseURL: db, Concurrency: 256,
			Mock: mock.Config{Accuracy: 0.8, LatencyMean: 10 * time.Millisecond}})
		f := h.NewFixture(n, 0.5, nil)
		// Warm-up run (connections, JIT of templates, caches).
		warm := h.StartRun(Obj{"suite_id": f.SuiteID, "sampling": Obj{"mode": "random", "n": 500}})
		h.Stream(warm["id"].(string), time.Minute)
		b.ResetTimer()
		var total time.Duration
		for i := 0; i < b.N; i++ {
			h.Worker.ResetStats()
			start := time.Now()
			run := h.StartRun(Obj{"suite_id": f.SuiteID})
			h.Stream(run["id"].(string), 10*time.Minute)
			total += time.Since(start)
			r := h.Run(run["id"].(string))
			if r["status"] != "succeeded" || int(num(r["completed"])) != n {
				b.Fatalf("run: %v completed=%v", r["status"], r["completed"])
			}
		}
		b.StopTimer()
		st := h.Worker.Stats()
		perMin := float64(n*b.N) / total.Minutes()
		b.ReportMetric(perMin, "examples/min")
		b.ReportMetric(st.OverheadP50Ms, "overhead_p50_ms")
		b.ReportMetric(st.OverheadP95Ms, "overhead_p95_ms")
		b.ReportMetric(st.OverheadP99Ms, "overhead_p99_ms")
		b.ReportMetric(st.FlushMsPerRow, "db_ms/row")
		b.ReportMetric(0, "ns/op")
		b.Logf("%s: %d examples in %s → %.0f examples/min; overhead p50 %.3f ms, p95 %.3f ms, p99 %.3f ms; "+
			"%d flushes, %.2f ms/flush, %.4f ms/row", name, n, (total / time.Duration(b.N)).Round(time.Millisecond), perMin,
			st.OverheadP50Ms, st.OverheadP95Ms, st.OverheadP99Ms, st.Flushes, st.MeanFlushMs, st.FlushMsPerRow)
		if perMin < 5000 {
			b.Errorf("throughput %.0f examples/min < 5,000", perMin)
		}
		if st.OverheadP95Ms >= 2 {
			b.Errorf("overhead p95 %.3f ms ≥ 2 ms", st.OverheadP95Ms)
		}
	})
}
