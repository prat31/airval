package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/airval/airval/internal/aggregate"
	"github.com/airval/airval/internal/evalcore"
	"github.com/airval/airval/internal/evaluators"
	"github.com/airval/airval/internal/notify"
	"github.com/airval/airval/internal/planner"
	"github.com/airval/airval/internal/queue"
	"github.com/airval/airval/internal/store"
)

// errFinalizerBusy makes the job redeliver later while another worker holds the lease.
var errFinalizerBusy = errors.New("runner: another worker is finalizing this run")

// finalize computes aggregates, evaluates gates, writes the summary and notifies. It runs once
// per run (lease-guarded and idempotent: a finished run with a summary is left alone).
func (w *Engine) finalize(ctx context.Context, runID uuid.UUID) error {
	ctx, span := tracer.Start(ctx, "runner.finalize", trace.WithAttributes(attribute.String("run.id", runID.String())))
	defer span.End()
	lease := "finalize." + runID.String()
	ok, err := w.d.Queue.AcquireLease(ctx, lease, w.d.WorkerID, 2*time.Minute)
	if err != nil {
		return err
	}
	if !ok {
		return errFinalizerBusy
	}
	defer func() { _ = w.d.Queue.ReleaseLease(context.WithoutCancel(ctx), lease, w.d.WorkerID) }()
	defer w.forget(runID)

	run, err := w.d.Store.GetRun(ctx, runID)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	} else if err != nil {
		return err
	}
	if run.FinishedAt != nil && len(run.Summary) > 0 {
		return nil
	}
	cfg, err := planner.ParseRunConfig(run.Config)
	if err != nil {
		_, ferr := w.d.Store.FinishRun(ctx, runID, store.RunFinish{Status: store.RunErrored, Error: err.Error()})
		return ferr
	}
	f, err := w.computeFinal(ctx, run, cfg)
	if err != nil {
		return err
	}
	if err := w.d.Store.UpsertRunAggregates(ctx, f.aggs); err != nil {
		return err
	}
	status, err := w.d.Store.FinishRun(ctx, runID, f.finish)
	if err != nil {
		return err
	}
	w.d.Metrics.RunsFinished.WithLabelValues(status).Inc()
	final, err := w.d.Store.GetRun(ctx, runID)
	if err != nil {
		return err
	}
	w.publish(runID, queue.EventProgress, map[string]any{"completed": final.Completed, "failed": final.Failed,
		"errored": final.Errored, "total": final.Total, "cost_usd": final.CostUSD, "tokens_in": final.TokensIn,
		"tokens_out": final.TokensOut})
	w.publish(runID, queue.EventRunStatus, map[string]any{"status": status, "gate_passed": final.GatePassed})
	w.publish(runID, queue.EventFinished, map[string]any{"status": status, "gate_passed": final.GatePassed,
		"summary": json.RawMessage(f.finish.Summary), "error": final.Error})
	w.d.Logger.Info("run finished", "run_id", runID, "status", status, "results", f.results,
		"pass_rate", f.event.PassRate, "duration_ms", int64(f.event.DurationMs))
	if w.d.Notifier != nil {
		ev := f.event
		ev.Run = final
		if w.d.BaseURL != "" {
			ev.URL = strings.TrimRight(w.d.BaseURL, "/") + "/runs/" + runID.String()
		}
		nctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		w.d.Notifier.RunFinished(nctx, ev)
	}
	return nil
}

type finalState struct {
	finish  store.RunFinish
	aggs    []store.RunAggregate
	event   notify.RunEvent
	results int
}

func gateTags(raw json.RawMessage, into map[string]bool) {
	var g apiGate
	if json.Unmarshal(raw, &g) != nil {
		return
	}
	var walk func(apiGate)
	walk = func(n apiGate) {
		if n.Tag != "" {
			into[n.Tag] = true
		}
		for _, a := range n.Args {
			walk(a)
		}
	}
	walk(g)
}

func (w *Engine) computeFinal(ctx context.Context, run store.Run, cfg planner.RunConfig) (*finalState, error) {
	now := time.Now().UTC()
	infos := make([]aggregate.EvaluatorInfo, 0, len(cfg.Evaluators))
	names := map[string]string{}
	weights := map[string]float64{}
	for _, e := range cfg.Evaluators {
		id := e.ID.String()
		th := 0.5
		if info, err := evaluators.Info(e.Kind); err == nil {
			th = info.DefaultThreshold
		}
		if e.Threshold != nil {
			th = *e.Threshold
		}
		sev := evalcore.Severity(e.Severity)
		if sev == "" {
			sev = evalcore.SeverityMajor
		}
		infos = append(infos, aggregate.EvaluatorInfo{ID: id, Kind: e.Kind, Severity: sev, Threshold: th})
		names[id] = e.Name
		weights[id] = e.Weight
		if weights[id] == 0 {
			weights[id] = 1
		}
	}
	tags := map[string]bool{}
	needCompare := false
	for _, g := range cfg.Gates {
		gateTags(g.Expr, tags)
		if strings.Contains(string(g.Expr), "regression_vs_baseline") {
			needCompare = true
		}
	}
	agg := aggregate.New(infos, aggregate.Options{})
	ttft := aggregate.NewStream(0)
	tagAggs := map[string]*aggregate.Aggregator{}
	tagTTFT := map[string]*aggregate.Stream{}
	for t := range tags {
		tagAggs[t] = aggregate.New(infos, aggregate.Options{NoTagSlices: true, NoPerExample: true})
		tagTTFT[t] = aggregate.NewStream(0)
	}
	var fin store.RunFinish
	cached := 0
	err := w.d.Store.IterateRunResults(ctx, run.ID, func(r store.Result) error {
		rec := aggregate.Record{ExampleID: r.ExampleID.String(), Trial: r.Trial, Tags: r.Tags,
			Latency: time.Duration(r.LatencyMs * float64(time.Millisecond)), CostUSD: r.CostUSD,
			Usage: evalcore.Usage{InputTokens: r.TokensIn, OutputTokens: r.TokensOut}, Error: r.Error}
		rec.Scores = make([]evalcore.Score, len(r.Scores))
		for i, s := range r.Scores {
			rec.Scores[i] = fromStoreScore(s)
			fin.CostUSD += s.CostUSD
		}
		agg.Add(rec)
		if r.TTFTMs != nil {
			ttft.Add(*r.TTFTMs)
		}
		for _, t := range r.Tags {
			if a, ok := tagAggs[t]; ok {
				a.Add(rec)
				if r.TTFTMs != nil {
					tagTTFT[t].Add(*r.TTFTMs)
				}
			}
		}
		fin.Completed++
		switch r.Status {
		case store.ResultFailed:
			fin.Failed++
		case store.ResultErrored:
			fin.Errored++
		}
		fin.CostUSD += r.CostUSD
		fin.TokensIn += int64(r.TokensIn)
		fin.TokensOut += int64(r.TokensOut)
		if r.Cached {
			cached++
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sum := agg.Summary()

	// Baseline comparison (regression gates).
	var cmp *aggregate.Comparison
	if needCompare && run.BaselineRunID != nil {
		base := aggregate.New(infos, aggregate.Options{NoTagSlices: true})
		err := w.d.Store.IterateRunResults(ctx, *run.BaselineRunID, func(r store.Result) error {
			rec := aggregate.Record{ExampleID: r.ExampleID.String(), Trial: r.Trial, Error: r.Error}
			for _, s := range r.Scores {
				rec.Scores = append(rec.Scores, fromStoreScore(s))
			}
			base.Add(rec)
			return nil
		})
		if err == nil {
			cmp = aggregate.Compare(base.Rollup(), agg.Rollup(), aggregate.CompareOptions{})
		} else if !errors.Is(err, store.ErrNotFound) {
			w.d.Logger.Warn("finalize: load baseline", "run_id", run.ID, "err", err)
		}
	}
	envFor := func(tag string) gateEnv {
		if tag == "" {
			return gateEnv{base: aggregate.SummaryEnv{Summary: sum, Comparison: cmp}, ttft: ttft.Dist(aggregate.HistNone)}
		}
		return gateEnv{base: aggregate.SummaryEnv{Summary: tagAggs[tag].Summary()}, ttft: tagTTFT[tag].Dist(aggregate.HistNone)}
	}
	var gates []notify.GateResult
	var gatePassed *bool
	for _, g := range cfg.Gates {
		gr := evalGate(g.ID.String(), g.Name, g.Expr, envFor, names)
		gates = append(gates, gr)
		ok := gr.Passed && (gatePassed == nil || *gatePassed)
		gatePassed = &ok
	}

	// Shard stats (cache, throttling).
	var st shardStats
	if shards, err := w.d.Store.ListRunShards(ctx, run.ID); err == nil {
		for _, s := range shards {
			var x shardStats
			if json.Unmarshal(s.Stats, &x) == nil {
				st.Attempts += x.Attempts
				st.Throttled += x.Throttled
				st.Transient += x.Transient
				st.Cached += x.Cached
				st.Skipped += x.Skipped
			}
		}
	}

	stoppedEarly := strings.HasPrefix(run.Error, stoppedEarlyPrefix)
	started := run.CreatedAt
	if run.StartedAt != nil {
		started = *run.StartedAt
	}
	dur := now.Sub(started)
	status := store.RunSucceeded
	switch {
	case stoppedEarly:
		status = store.RunFailed
		f := false
		gatePassed = &f
	case run.Total > 0 && fin.Completed == 0:
		status = store.RunErrored
		if run.Error == "" {
			fin.Error = "no results were produced"
		}
	case fin.Completed > 0 && fin.Errored == fin.Completed:
		status = store.RunErrored
		fin.Error = fmt.Sprintf("all %d results errored", fin.Completed)
	case gatePassed != nil && !*gatePassed:
		status = store.RunFailed
	}
	if run.Status == store.RunCanceled {
		gatePassed = nil
	}

	// Mean score (weighted over evaluators with values).
	var wsum, wtot float64
	evLines := make([]notify.EvaluatorLine, 0, len(sum.Evaluators))
	for _, e := range sum.Evaluators {
		line := notify.EvaluatorLine{ID: e.ID, Name: names[e.ID], PassRate: e.PassRate}
		if line.Name == "" {
			line.Name = e.ID
		}
		if e.Values.N > 0 {
			m := e.Values.Mean
			line.Mean = &m
			wsum += m * weights[e.ID]
			wtot += weights[e.ID]
		}
		evLines = append(evLines, line)
	}
	summary := map[string]any{}
	if b, err := json.Marshal(sum); err == nil {
		_ = json.Unmarshal(b, &summary)
	}
	summary["pass_rate"] = sum.PassRate
	if wtot > 0 {
		summary["mean_score"] = wsum / wtot
	}
	summary["gates"] = gates
	summary["duration_ms"] = float64(dur) / float64(time.Millisecond)
	if dur > 0 {
		summary["throughput_per_min"] = float64(fin.Completed) / dur.Minutes()
	}
	if fin.Completed > 0 {
		summary["cache_hit_ratio"] = float64(cached) / float64(fin.Completed)
	}
	if st.Attempts > 0 {
		summary["rate_limited_ratio"] = float64(st.Throttled) / float64(st.Attempts)
	}
	summary["latency_p50_ms"] = sum.LatencyMs.P50
	summary["latency_p95_ms"] = sum.LatencyMs.P95
	summary["ttft_ms"] = ttft.Dist(aggregate.HistNone)
	summary["stopped_early"] = stoppedEarly
	if stoppedEarly {
		summary["stop_reason"] = strings.TrimPrefix(run.Error, stoppedEarlyPrefix)
	}
	summary["evaluator_names"] = names
	summary["target_calls"] = st.Attempts
	summary["throttled"] = st.Throttled
	summary["skipped"] = st.Skipped
	if cmp != nil {
		summary["baseline_comparison"] = cmp
	}
	raw, err := json.Marshal(summary)
	if err != nil {
		return nil, err
	}
	fin.Status, fin.Summary, fin.GatePassed, fin.FinishedAt = status, raw, gatePassed, now

	return &finalState{finish: fin, aggs: runAggregates(run.ID, sum, ttft.Dist(aggregate.HistAuto), now),
		results: fin.Completed, event: notify.RunEvent{SuiteName: cfg.SuiteName, TargetName: cfg.TargetName,
			PassRate: sum.PassRate, Results: fin.Completed, DurationMs: float64(dur) / float64(time.Millisecond),
			StoppedEarly: stoppedEarly, Gates: gates, Evaluators: evLines}}, nil
}

func f64p(v float64) *float64 { return &v }

func distAgg(runID, evalID uuid.UUID, metric, slice string, d aggregate.Dist, at time.Time) store.RunAggregate {
	a := store.RunAggregate{RunID: runID, EvaluatorID: evalID, Metric: metric, SliceKey: slice, N: d.N, UpdatedAt: at}
	if d.N > 0 {
		a.Mean, a.Min, a.Max = f64p(d.Mean), f64p(d.Min), f64p(d.Max)
		a.P50, a.P90, a.P95, a.P99 = f64p(d.P50), f64p(d.P90), f64p(d.P95), f64p(d.P99)
		a.Stddev, a.Sum = f64p(d.Stddev), f64p(d.Sum)
	}
	if d.Histogram != nil {
		a.Histogram, _ = json.Marshal(d.Histogram)
	}
	return a
}

// runAggregates flattens a summary into run_aggregates rows: per evaluator (overall and per tag),
// and run-level latency / ttft / cost / tokens / pass rate (overall and per tag).
func runAggregates(runID uuid.UUID, sum *aggregate.Summary, ttft aggregate.Dist, at time.Time) []store.RunAggregate {
	var out []store.RunAggregate
	for _, e := range sum.Evaluators {
		id, err := uuid.Parse(e.ID)
		if err != nil {
			continue
		}
		a := distAgg(runID, id, store.MetricScore, "", e.Values, at)
		if e.Passed+e.Failed+e.Errored > 0 {
			a.PassRate = f64p(e.PassRate)
		}
		out = append(out, a)
	}
	out = append(out,
		distAgg(runID, store.NilID, store.MetricLatencyMs, "", sum.LatencyMs, at),
		distAgg(runID, store.NilID, store.MetricTTFTMs, "", ttft, at),
		distAgg(runID, store.NilID, store.MetricCostUSD, "", sum.CostUSD, at),
		distAgg(runID, store.NilID, store.MetricTokens, "", sum.Tokens, at),
		store.RunAggregate{RunID: runID, EvaluatorID: store.NilID, Metric: store.MetricPassRate, N: sum.Results,
			Mean: f64p(sum.PassRate), PassRate: f64p(sum.PassRate), UpdatedAt: at})
	for _, s := range sum.Slices {
		if s.Key != "tag" {
			continue
		}
		key := "tag:" + s.Value
		out = append(out, store.RunAggregate{RunID: runID, EvaluatorID: store.NilID, Metric: store.MetricPassRate,
			SliceKey: key, N: s.Results, Mean: f64p(s.PassRate), PassRate: f64p(s.PassRate), UpdatedAt: at})
		for eid, m := range s.Evaluators {
			id, err := uuid.Parse(eid)
			if err != nil {
				continue
			}
			out = append(out, store.RunAggregate{RunID: runID, EvaluatorID: id, Metric: store.MetricScore, SliceKey: key,
				N: m.N, Mean: f64p(m.Mean), Stddev: f64p(m.Stddev), PassRate: f64p(m.PassRate), UpdatedAt: at})
		}
	}
	return out
}
