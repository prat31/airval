package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	"github.com/airval/airval/internal/aggregate"
	"github.com/airval/airval/internal/cache"
	"github.com/airval/airval/internal/evalcore"
	"github.com/airval/airval/internal/evaluators"
	"github.com/airval/airval/internal/planner"
	"github.com/airval/airval/internal/queue"
	"github.com/airval/airval/internal/store"
	"github.com/airval/airval/internal/targets"
)

var tracer = otel.Tracer("airval/runner")

// shardStats are stored on the shard row and summed by the finalizer.
type shardStats struct {
	Items     int64 `json:"items"`
	Attempts  int64 `json:"attempts"`
	Throttled int64 `json:"throttled"`
	Transient int64 `json:"transient"`
	Cached    int64 `json:"cached"`
	Skipped   int64 `json:"skipped"`
}

// shard is one shard in execution.
type shard struct {
	rs    *runState
	wg    sync.WaitGroup
	errMu sync.Mutex
	err   error

	items, attempts, throttled, transient, cached, skipped atomic.Int64
}

func (s *shard) fail(err error) {
	s.errMu.Lock()
	if s.err == nil {
		s.err = err
	}
	s.errMu.Unlock()
}

func (s *shard) stats() shardStats {
	return shardStats{Items: s.items.Load(), Attempts: s.attempts.Load(), Throttled: s.throttled.Load(),
		Transient: s.transient.Load(), Cached: s.cached.Load(), Skipped: s.skipped.Load()}
}

// evalJob carries one item from the target stage to the evaluator stage.
type evalJob struct {
	ctx      context.Context
	sh       *shard
	it       planner.Item
	ex       *evalcore.Example
	resp     evalcore.Response
	baseline *evalcore.Response
	terr     error
	t0       time.Time
	tdur     time.Duration // time spent calling the target (incl. retries/backoff)
}

func (w *Engine) runShard(ctx context.Context, runID uuid.UUID, d queue.Delivery) error {
	var p planner.ShardPayload
	if err := json.Unmarshal(d.Job.Payload, &p); err != nil {
		return fmt.Errorf("%w: bad shard payload: %v", queue.ErrNoRetry, err)
	}
	ctx = otel.GetTextMapPropagator().Extract(ctx, propagation.MapCarrier(p.Trace))
	ctx, span := tracer.Start(ctx, "runner.shard", trace.WithAttributes(attribute.String("run.id", runID.String()),
		attribute.Int("shard", d.Job.Shard), attribute.Int("items", len(p.Items))))
	defer span.End()

	rs, err := w.state(ctx, runID)
	if errors.Is(err, store.ErrNotFound) {
		return nil // run deleted: drop the job
	} else if err != nil {
		return err
	}
	if rs.buildErr != nil {
		w.failRun(rs, rs.buildErr)
		return w.completeShard(ctx, rs, d.Job.Shard, shardStats{Skipped: int64(len(p.Items))})
	}
	if !rs.started.Swap(true) {
		if ok, err := w.d.Store.TransitionRun(ctx, runID, []string{store.RunQueued}, store.RunRunning, time.Now()); err == nil && ok {
			w.publish(runID, queue.EventRunStatus, map[string]any{"status": store.RunRunning, "total": rs.run.Total})
		}
	}

	// Heartbeat: keep JetStream from redelivering a long shard.
	hbCtx, stopHB := context.WithCancel(ctx)
	defer stopHB()
	go func() {
		t := time.NewTicker(15 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-hbCtx.Done():
				return
			case <-t.C:
				_ = d.InProgress()
			}
		}
	}()

	sh, err := w.execItems(ctx, rs, p.Items)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err // nak: the shard is redelivered; persisted results are replaced idempotently
	}
	return w.completeShard(ctx, rs, d.Job.Shard, sh.stats())
}

func (w *Engine) completeShard(ctx context.Context, rs *runState, shardIdx int, st shardStats) error {
	raw, _ := json.Marshal(st)
	ctx = context.WithoutCancel(ctx)
	remaining, err := w.d.Store.CompleteRunShard(ctx, rs.id, shardIdx, raw)
	if err != nil {
		return err
	}
	if remaining == 0 {
		return w.d.Queue.PublishJob(ctx, queue.Job{Kind: queue.JobFinalize, RunID: rs.id.String()})
	}
	return nil
}

// failRun marks a run errored (once per process) when it cannot execute.
func (w *Engine) failRun(rs *runState, cause error) {
	if rs.stopped.Swap(true) {
		return
	}
	ctx := w.bg
	w.d.Logger.Error("run cannot execute", "run_id", rs.id, "err", cause)
	if st, err := w.d.Store.FinishRun(ctx, rs.id, store.RunFinish{Status: store.RunErrored, Error: cause.Error(),
		Completed: rs.run.Completed, Failed: rs.run.Failed, Errored: rs.run.Errored}); err == nil {
		w.publish(rs.id, queue.EventRunStatus, map[string]any{"status": st, "error": cause.Error()})
	}
}

// execItems runs a shard's items through the pipeline and waits until every dispatched item
// is persisted.
func (w *Engine) execItems(ctx context.Context, rs *runState, items []planner.Item) (*shard, error) {
	sh := &shard{rs: rs}
	examples, err := w.loadExamples(ctx, items)
	if err != nil {
		return nil, err
	}
	var sources map[uuid.UUID]store.Result
	if rs.cfg.Mode == planner.ModeRescore {
		if sources, err = w.loadSources(ctx, items); err != nil {
			return nil, err
		}
	}
	baselines := w.loadBaselines(ctx, rs, items)

	for _, it := range items {
		if ctx.Err() != nil {
			sh.fail(ctx.Err())
			break
		}
		if w.cancelled(rs) {
			sh.skipped.Add(1)
			continue
		}
		acquired := false
		select {
		case w.itemSem <- struct{}{}:
			acquired = true
		case <-ctx.Done():
		}
		if ctx.Err() != nil {
			if acquired {
				<-w.itemSem
			}
			sh.fail(ctx.Err())
			break
		}
		sh.wg.Add(1)
		sh.items.Add(1)
		ex := examples[it.R]
		go w.targetStage(ctx, sh, it, ex, sources, baselines)
	}
	sh.wg.Wait()
	return sh, sh.err
}

func (w *Engine) targetStage(ctx context.Context, sh *shard, it planner.Item, ex *evalcore.Example,
	sources map[uuid.UUID]store.Result, baselines map[uuid.UUID][]store.Result) {
	rs := sh.rs
	j := &evalJob{ctx: ctx, sh: sh, it: it, ex: ex, t0: time.Now()}
	switch {
	case ex == nil:
		<-w.itemSem
		j.ex = &evalcore.Example{ID: it.E.String()}
		j.terr = targets.ErrPermanent{Err: errors.New("example revision not found")}
	case rs.cfg.Mode == planner.ModeRescore:
		<-w.itemSem
		src, ok := sources[derefID(it.S)]
		if !ok {
			j.terr = targets.ErrPermanent{Err: errors.New("source result not found")}
		} else if src.Error != "" {
			j.terr = targets.ErrPermanent{Err: errors.New(src.Error)}
			j.resp = responseFromResult(src)
		} else {
			j.resp = responseFromResult(src)
		}
	default:
		j.resp, j.terr = w.callTarget(ctx, sh, ex, it.T)
		j.tdur = time.Since(j.t0)
		<-w.itemSem
		if j.terr != nil && ctx.Err() != nil {
			sh.fail(ctx.Err()) // shutting down: don't persist an aborted call
			sh.wg.Done()
			return
		}
	}
	if b := pickBaseline(baselines[it.E], it.T); b != nil {
		resp := responseFromResult(*b)
		j.baseline = &resp
	}
	w.evalCh <- j
}

func derefID(p *uuid.UUID) uuid.UUID {
	if p == nil {
		return uuid.Nil
	}
	return *p
}

func pickBaseline(rs []store.Result, trial int) *store.Result {
	var best *store.Result
	for i := range rs {
		if rs[i].Error != "" {
			continue
		}
		if rs[i].Trial == trial {
			return &rs[i]
		}
		if best == nil {
			best = &rs[i]
		}
	}
	return best
}

// callTarget invokes the target with the response cache, limiter, retries and metrics.
func (w *Engine) callTarget(ctx context.Context, sh *shard, ex *evalcore.Example, trial int) (evalcore.Response, error) {
	rs := sh.rs
	kind := rs.adapter.Kind()
	if rs.cfg.TimeoutSeconds > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(rs.cfg.TimeoutSeconds)*time.Second)
		defer cancel()
	}
	var cacheKey string
	if rs.cfg.Cache.Target && w.cache != nil {
		cacheKey = cache.Key("target/v1", rs.targetKey, []byte(ex.ID), w.revisionKey(ex), []byte(strconv.Itoa(trial)))
		if raw, ok, _ := w.cache.Get(ctx, cacheKey); ok {
			var resp evalcore.Response
			if json.Unmarshal(raw, &resp) == nil {
				resp.Cached = true
				sh.cached.Add(1)
				return resp, nil
			}
		}
	}
	opts := targets.InvokeOptions{Secrets: rs.resolver, Trial: trial}
	var lastErr error
	for attempt := 1; ; attempt++ {
		if err := rs.lim.Acquire(ctx); err != nil {
			return evalcore.Response{}, err
		}
		sh.attempts.Add(1)
		start := time.Now()
		cctx, span := tracer.Start(ctx, "target.invoke", trace.WithAttributes(attribute.String("target.kind", kind),
			attribute.Int("attempt", attempt)))
		resp, err := rs.adapter.Invoke(cctx, *ex, opts)
		lat := time.Since(start)
		o, ra := classifyOutcome(err)
		rs.lim.Release(o, lat, ra)
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
		w.d.Metrics.TargetLatency.WithLabelValues(kind).Observe(lat.Seconds())
		switch o {
		case outcomeThrottled:
			sh.throttled.Add(1)
			w.d.Metrics.TargetThrottled.WithLabelValues(kind).Inc()
		case outcomeTransient:
			sh.transient.Add(1)
		}
		if err == nil {
			if resp.Latency == 0 {
				resp.Latency = lat
			}
			rs.lim.consumeTokens(ctx, resp.Usage.InputTokens+resp.Usage.OutputTokens)
			if cacheKey != "" {
				if raw, err := json.Marshal(resp); err == nil {
					_ = w.cache.Set(ctx, cacheKey, raw)
				}
			}
			return resp, nil
		}
		lastErr = err
		if !retryable(err) || attempt >= w.d.MaxAttempts || ctx.Err() != nil {
			return evalcore.Response{}, lastErr
		}
		if err := sleepCtx(ctx, backoff(attempt, ra)); err != nil {
			return evalcore.Response{}, lastErr
		}
	}
}

// revisionKey identifies the example content for cache keys (the example id is stable across
// revisions, so the content hash is included).
func (w *Engine) revisionKey(ex *evalcore.Example) []byte {
	b, _ := json.Marshal(ex)
	return b
}

// evalLoop is one evaluator-stage worker.
func (w *Engine) evalLoop() {
	for j := range w.evalCh {
		w.evaluate(j)
	}
}

type reviewReq struct {
	evaluatorID uuid.UUID
	review      map[string]any
}

func (w *Engine) evaluate(j *evalJob) {
	rs := j.sh.rs
	var scores []evalcore.Score
	terr := ""
	if j.terr != nil {
		terr = j.terr.Error()
	} else {
		scores = rs.pipeline.Evaluate(j.ctx, evaluators.Input{Example: j.ex, Response: &j.resp, Baseline: j.baseline})
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	res := store.Result{ID: store.NewID(), RunID: rs.id, ExampleID: j.it.E, ExampleRevisionID: j.it.R, Trial: j.it.T,
		Error: terr, CreatedAt: now}
	if j.terr == nil || rs.cfg.Mode == planner.ModeRescore {
		fillResult(&res, &j.resp)
	}
	res.Status = resultStatus(terr, scores)
	p := &pending{rs: rs, res: res, scores: toStoreScores(res.ID, rs.id, scores, now)}
	var judgeMs float64
	for _, s := range scores {
		if s.Latency > 0 && s.CostUSD > 0 { // model-based: exclude from engine overhead
			judgeMs = max(judgeMs, float64(s.Latency)/float64(time.Millisecond))
		}
		if evaluators.ReviewRequired(s) {
			if id, err := uuid.Parse(s.EvaluatorID); err == nil {
				rv, _ := s.Meta[evaluators.MetaReview].(map[string]any)
				p.reviews = append(p.reviews, reviewReq{evaluatorID: id, review: rv})
			}
		}
		p.judgeCost += s.CostUSD
	}
	if rs.budget != nil {
		rec := aggregate.Record{ExampleID: res.ExampleID.String(), Trial: res.Trial, Error: terr, Scores: scores}
		if over, why := rs.budget.Observe(rec); over {
			w.stopEarly(rs, why)
		}
	}
	w.d.Metrics.ExamplesProcessed.WithLabelValues(res.Status).Inc()
	sh := j.sh
	p.done = func(err error) {
		if err != nil {
			sh.fail(err)
		}
		sh.wg.Done()
	}
	overhead := time.Since(j.t0) - j.tdur - time.Duration(judgeMs*float64(time.Millisecond))
	w.recordOverhead(max(overhead, 0))
	w.ing.submit(p)
}

// stopEarly cancels a doomed run (failure budget exceeded).
func (w *Engine) stopEarly(rs *runState, why string) {
	if rs.stopped.Swap(true) {
		return
	}
	w.d.Logger.Info("failure budget exceeded: stopping run early", "run_id", rs.id, "reason", why)
	ctx := w.bg
	_ = w.d.Store.SetRunError(ctx, rs.id, stoppedEarlyPrefix+why)
	if err := w.d.Queue.SetCancel(ctx, rs.id.String()); err != nil {
		w.d.Logger.Warn("set cancel flag", "run_id", rs.id, "err", err)
	}
	w.publish(rs.id, queue.EventRunStatus, map[string]any{"status": store.RunRunning, "stopping": true, "reason": why})
}

const stoppedEarlyPrefix = "stopped early: "

func (w *Engine) loadExamples(ctx context.Context, items []planner.Item) (map[uuid.UUID]*evalcore.Example, error) {
	seen := map[uuid.UUID]bool{}
	ids := make([]uuid.UUID, 0, len(items))
	for _, it := range items {
		if !seen[it.R] {
			seen[it.R] = true
			ids = append(ids, it.R)
		}
	}
	revs, err := w.d.Store.GetExampleRevisions(ctx, ids)
	if err != nil {
		return nil, err
	}
	out := make(map[uuid.UUID]*evalcore.Example, len(revs))
	for _, r := range revs {
		ex := toEvalExample(r)
		out[r.RevisionID] = &ex
	}
	return out, nil
}

func (w *Engine) loadSources(ctx context.Context, items []planner.Item) (map[uuid.UUID]store.Result, error) {
	ids := make([]uuid.UUID, 0, len(items))
	for _, it := range items {
		if it.S != nil {
			ids = append(ids, *it.S)
		}
	}
	rs, err := w.d.Store.GetResults(ctx, ids)
	if err != nil {
		return nil, err
	}
	out := make(map[uuid.UUID]store.Result, len(rs))
	for _, r := range rs {
		out[r.ID] = r
	}
	return out, nil
}

// loadBaselines fetches baseline outputs for pairwise evaluators (one query per shard).
func (w *Engine) loadBaselines(ctx context.Context, rs *runState, items []planner.Item) map[uuid.UUID][]store.Result {
	if !rs.needsBaseline || rs.run.BaselineRunID == nil {
		return nil
	}
	ids := make([]uuid.UUID, 0, len(items))
	seen := map[uuid.UUID]bool{}
	for _, it := range items {
		if !seen[it.E] {
			seen[it.E] = true
			ids = append(ids, it.E)
		}
	}
	res, err := w.d.Store.ListResultsForExamples(ctx, *rs.run.BaselineRunID, ids)
	if err != nil {
		w.d.Logger.Warn("load baseline outputs", "run_id", rs.id, "err", err)
		return nil
	}
	out := map[uuid.UUID][]store.Result{}
	for _, r := range res {
		out[r.ExampleID] = append(out[r.ExampleID], r)
	}
	return out
}
