package enginetest

import (
	"bytes"
	"math"
	"net/http"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/airval/airval/internal/webhooks"
	"github.com/airval/airval/tools/mocktarget/mock"
)

func num(v any) float64 {
	f, _ := v.(float64)
	return f
}

func correctCount(qs []string) int {
	n := 0
	for _, q := range qs {
		if mock.IsCorrect(q, 0.8) {
			n++
		}
	}
	return n
}

func dbURLs(t *testing.T) map[string]string {
	urls := map[string]string{"sqlite": ""}
	if pg := os.Getenv("AIRVAL_TEST_PG_URL"); pg != "" {
		urls["postgres"] = pg
	}
	return urls
}

// TestEndToEnd runs a suite against the mock target through the public API (create → SSE →
// results/aggregates/gate), then re-runs failures and re-scores, on SQLite and (when
// AIRVAL_TEST_PG_URL is set) Postgres.
func TestEndToEnd(t *testing.T) {
	for name, url := range dbURLs(t) {
		t.Run(name, func(t *testing.T) {
			h := Start(t, Options{DatabaseURL: url, Mock: mock.Config{Accuracy: 0.8, LatencyMean: time.Millisecond}})
			const n = 300
			f := h.NewFixture(n, 0.5, nil)
			correct := correctCount(f.Questions)

			run := h.StartRun(Obj{"suite_id": f.SuiteID, "labels": Obj{"git_sha": "abc123"}})
			id := run["id"].(string)
			if run["status"] != "queued" || int(num(run["total"])) != n {
				t.Fatalf("created run: %v", run)
			}
			sr := h.Stream(id, 60*time.Second)
			if sr.Events["run.progress"] == 0 || sr.Events["run.finished"] != 1 {
				t.Fatalf("events: %v", sr.Events)
			}
			r := h.Run(id)
			if r["status"] != "succeeded" || r["gate_passed"] != true {
				t.Fatalf("run: status=%v gate=%v error=%v", r["status"], r["gate_passed"], r["error"])
			}
			if int(num(r["completed"])) != n || int(num(r["failed"])) != n-correct || int(num(r["errored"])) != 0 {
				t.Fatalf("counters: completed=%v failed=%v errored=%v (want %d failed)", r["completed"], r["failed"], r["errored"], n-correct)
			}
			sum := r["summary"].(Obj)
			if got := num(sum["pass_rate"]); math.Abs(got-float64(correct)/n) > 1e-9 {
				t.Fatalf("pass_rate %v want %v", got, float64(correct)/n)
			}
			gates := sum["gates"].([]any)
			if len(gates) != 1 || gates[0].(Obj)["passed"] != true {
				t.Fatalf("gates: %v", gates)
			}

			// Results: every result has 3 scores.
			var res struct {
				Items []Obj `json:"items"`
			}
			h.Do("GET", "/v1/runs/"+id+"/results?limit=1000", nil, 200, &res)
			if len(res.Items) != n {
				t.Fatalf("results: %d", len(res.Items))
			}
			for _, it := range res.Items {
				if len(it["scores"].([]any)) != 3 {
					t.Fatalf("scores: %v", it["scores"])
				}
			}
			// Aggregates: exact_match mean and per-tag slices.
			var aggs struct {
				Items []Obj `json:"items"`
			}
			h.Do("GET", "/v1/runs/"+id+"/aggregates", nil, 200, &aggs)
			var mean float64
			slices := 0
			for _, a := range aggs.Items {
				if a["evaluator_id"] == f.Evaluators["exact_match"] && a["metric"] == "score" {
					if a["slice_key"] == "" {
						mean = num(a["mean"])
					} else {
						slices++
					}
				}
			}
			if math.Abs(mean-float64(correct)/n) > 1e-9 || slices != 2 {
				t.Fatalf("aggregates: mean=%v slices=%d", mean, slices)
			}

			// Re-run failed: only the failures, still failing → gate fails.
			var child Obj
			h.Do("POST", "/v1/runs/"+id+":rerun-failed", nil, 201, &child)
			if int(num(child["total"])) != n-correct || child["parent_run_id"] != id {
				t.Fatalf("rerun child: %v", child)
			}
			h.Stream(child["id"].(string), 60*time.Second)
			c := h.Run(child["id"].(string))
			if c["status"] != "failed" || c["gate_passed"] != false {
				t.Fatalf("rerun: %v %v", c["status"], c["gate_passed"])
			}

			// Re-score: no target calls, same verdicts.
			var rs Obj
			h.Do("POST", "/v1/runs/"+id+":rescore", Obj{}, 201, &rs)
			h.Stream(rs["id"].(string), 60*time.Second)
			rr := h.Run(rs["id"].(string))
			rsum := rr["summary"].(Obj)
			if rr["status"] != "succeeded" || num(rsum["target_calls"]) != 0 || int(num(rr["failed"])) != n-correct {
				t.Fatalf("rescore: %v calls=%v failed=%v", rr["status"], rsum["target_calls"], rr["failed"])
			}
		})
	}
}

func TestSamplingTrialsAndWebhook(t *testing.T) {
	h := Start(t, Options{Mock: mock.Config{Accuracy: 0.8}})
	f := h.NewFixture(100, 0.5, Obj{"trials": 2})

	// Stratified sample of 20 examples × 2 trials, deterministic.
	run := h.StartRun(Obj{"suite_id": f.SuiteID, "sampling": Obj{"mode": "stratified", "n": 20}})
	if int(num(run["total"])) != 40 {
		t.Fatalf("total %v", run["total"])
	}
	h.Stream(run["id"].(string), 60*time.Second)
	r := h.Run(run["id"].(string))
	trials, _ := r["summary"].(Obj)["trials"].(Obj)
	if r["status"] != "succeeded" || trials == nil || int(num(trials["max_trials"])) != 2 {
		t.Fatalf("trials run: %v %v", r["status"], r["summary"])
	}

	// Webhook trigger: signed payload → run with labels from the payload.
	var tr Obj
	h.Do("POST", "/v1/suites/"+f.SuiteID+"/triggers", Obj{"kind": "webhook", "name": "deploy",
		"config": Obj{"sampling": Obj{"mode": "random", "n": 5}, "trials": 1}}, 201, &tr)
	secret := tr["webhook_secret"].(string)
	body := []byte(`{"after":"deadbeef","ref":"refs/heads/main"}`)
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	post := func(sig string) *http.Response {
		req, _ := http.NewRequest("POST", h.Server.URL+"/hooks/"+tr["id"].(string), bytes.NewReader(body))
		req.Header.Set(webhooks.HeaderTimestamp, ts)
		req.Header.Set(webhooks.HeaderSignature, sig)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}
	if resp := post("sha256=00"); resp.StatusCode != 401 {
		t.Fatalf("bad signature accepted: %d", resp.StatusCode)
	}
	resp := post(webhooks.Sign([]byte(secret), ts, body))
	if resp.StatusCode != 202 {
		t.Fatalf("webhook: %d", resp.StatusCode)
	}
	resp.Body.Close()
	var runs struct {
		Items []Obj `json:"items"`
	}
	h.Do("GET", "/v1/runs?suite_id="+f.SuiteID+"&trigger=webhook", nil, 200, &runs)
	if len(runs.Items) != 1 {
		t.Fatalf("webhook runs: %d", len(runs.Items))
	}
	wr := runs.Items[0]
	labels := wr["labels"].(Obj)
	if labels["git_sha"] != "deadbeef" || labels["branch"] != "main" || int(num(wr["total"])) != 5 {
		t.Fatalf("webhook run: %v", wr)
	}
	h.Stream(wr["id"].(string), 60*time.Second)
}

func TestCancel(t *testing.T) {
	h := Start(t, Options{Concurrency: 4, Mock: mock.Config{LatencyMean: 20 * time.Millisecond}})
	f := h.NewFixture(400, 0.5, nil)
	run := h.StartRun(Obj{"suite_id": f.SuiteID})
	id := run["id"].(string)
	time.Sleep(300 * time.Millisecond)
	var c Obj
	h.Do("POST", "/v1/runs/"+id+":cancel", nil, 200, &c)
	if c["status"] != "canceled" {
		t.Fatalf("cancel: %v", c["status"])
	}
	h.Stream(id, 30*time.Second)
	deadline := time.Now().Add(30 * time.Second)
	for {
		r := h.Run(id)
		if r["summary"] != nil {
			if r["status"] != "canceled" || int(num(r["completed"])) >= 400 {
				t.Fatalf("canceled run: %v completed=%v", r["status"], r["completed"])
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("canceled run never finalized")
		}
		time.Sleep(100 * time.Millisecond)
	}
	h.Do("POST", "/v1/runs/"+id+":cancel", nil, 409, nil)
}

// TestDevModeDefinitionOfDone: 1,000 examples against the mock (10 ms latency) finish in < 15 s
// with SQLite + embedded NATS (plan §16).
func TestDevModeDefinitionOfDone(t *testing.T) {
	if testing.Short() {
		t.Skip("short")
	}
	h := Start(t, Options{Mock: mock.Config{Accuracy: 0.8, LatencyMean: 10 * time.Millisecond}})
	f := h.NewFixture(1000, 0.5, nil)
	start := time.Now()
	run := h.StartRun(Obj{"suite_id": f.SuiteID})
	limit := 15 * time.Second
	if raceEnabled {
		limit = 2 * time.Minute
	}
	h.Stream(run["id"].(string), limit)
	el := time.Since(start)
	r := h.Run(run["id"].(string))
	if r["status"] != "succeeded" || int(num(r["completed"])) != 1000 {
		t.Fatalf("run: %v completed=%v", r["status"], r["completed"])
	}
	t.Logf("1,000 examples in %s (%.0f examples/min)", el.Round(time.Millisecond), 1000/el.Minutes())
	if el > limit {
		t.Fatalf("took %s, want < 15s", el)
	}
}
