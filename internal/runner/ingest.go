package runner

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/prat31/airval/internal/queue"
	"github.com/prat31/airval/internal/store"
)

// pending is one evaluated result waiting to be persisted.
type pending struct {
	rs        *runState
	res       store.Result
	scores    []store.Score
	reviews   []reviewReq
	judgeCost float64
	done      func(error)
}

// ingester batches results from every shard of the process into multi-row writes: a batch is
// flushed every FlushInterval or as soon as FlushRows results are buffered. Postgres takes a few
// flushes in parallel (COPY); SQLite gets one writer at a time.
type ingester struct {
	w       *Engine
	in      chan *pending
	batches chan []*pending
	loopEnd chan struct{}
	flushed chan struct{}
	writers int
}

func newIngester(w *Engine) *ingester {
	writers := 1
	if w.d.Store.Dialect() == "postgres" {
		writers = 4
	}
	return &ingester{w: w, in: make(chan *pending, 4*w.d.FlushRows), batches: make(chan []*pending, writers),
		loopEnd: make(chan struct{}), flushed: make(chan struct{}), writers: writers}
}

func (g *ingester) start() {
	go g.loop()
	done := make(chan struct{}, g.writers)
	for i := 0; i < g.writers; i++ {
		go func() {
			for b := range g.batches {
				g.flush(b)
			}
			done <- struct{}{}
		}()
	}
	go func() {
		for i := 0; i < g.writers; i++ {
			<-done
		}
		close(g.flushed)
	}()
}

func (g *ingester) submit(p *pending) { g.in <- p }

// stop flushes everything buffered and waits for the writers. No submit may happen afterwards.
func (g *ingester) stop() {
	close(g.in)
	<-g.flushed
}

func (g *ingester) loop() {
	defer close(g.batches)
	max := g.w.d.FlushRows
	t := time.NewTicker(g.w.d.FlushInterval)
	defer t.Stop()
	var buf []*pending
	send := func() {
		if len(buf) > 0 {
			g.batches <- buf
			buf = make([]*pending, 0, max)
		}
	}
	for {
		select {
		case p, ok := <-g.in:
			if !ok {
				send()
				return
			}
			buf = append(buf, p)
			if len(buf) >= max {
				send()
			}
		case <-t.C:
			send()
		}
	}
}

type runDelta struct {
	rs      *runState
	prog    store.RunProgress
	compact []map[string]any
}

const maxResultEventsPerFlush = 100

func (g *ingester) flush(batch []*pending) {
	w := g.w
	results := make([]store.Result, len(batch))
	var scores []store.Score
	for i, p := range batch {
		results[i] = p.res
		scores = append(scores, p.scores...)
	}
	ctx, cancel := context.WithTimeout(w.bg, 60*time.Second)
	defer cancel()
	start := time.Now()
	var err error
	for attempt := 1; attempt <= 3; attempt++ {
		if err = w.d.Store.InsertResults(ctx, results, scores); err == nil || ctx.Err() != nil {
			break
		}
		w.d.Logger.Warn("ingest: flush failed, retrying", "rows", len(results), "attempt", attempt, "err", err)
		_ = sleepCtx(ctx, time.Duration(attempt)*200*time.Millisecond)
	}
	el := time.Since(start)
	w.d.Metrics.DBFlushLatency.Observe(el.Seconds())
	w.statMu.Lock()
	w.flushes++
	w.rows += len(results)
	w.flushMs += float64(el) / float64(time.Millisecond)
	w.statMu.Unlock()
	if err != nil {
		w.d.Logger.Error("ingest: flush failed", "rows", len(results), "err", err)
		for _, p := range batch {
			p.done(err)
		}
		return
	}

	// Review items (results exist now).
	var items []store.ReviewItem
	deltas := map[uuid.UUID]*runDelta{}
	var order []uuid.UUID
	for _, p := range batch {
		for _, r := range p.reviews {
			q, err := w.reviewQueue(ctx, p.rs, r.evaluatorID, r.review)
			if err != nil {
				w.d.Logger.Warn("review queue", "run_id", p.rs.id, "err", err)
				continue
			}
			items = append(items, store.ReviewItem{OrgID: p.rs.run.OrgID, ProjectID: p.rs.run.ProjectID, QueueID: q,
				RunID: p.rs.id, ResultID: p.res.ID, Status: store.ReviewPending})
		}
		d := deltas[p.rs.id]
		if d == nil {
			d = &runDelta{rs: p.rs}
			deltas[p.rs.id] = d
			order = append(order, p.rs.id)
		}
		d.prog.Completed++
		switch p.res.Status {
		case store.ResultFailed:
			d.prog.Failed++
		case store.ResultErrored:
			d.prog.Errored++
		}
		d.prog.CostUSD += p.res.CostUSD + p.judgeCost
		d.prog.TokensIn += int64(p.res.TokensIn)
		d.prog.TokensOut += int64(p.res.TokensOut)
		if len(d.compact) < maxResultEventsPerFlush {
			d.compact = append(d.compact, compactResult(p))
		}
	}
	if len(items) > 0 {
		if _, err := w.d.Store.AddReviewItems(ctx, items); err != nil {
			w.d.Logger.Warn("add review items", "err", err)
		}
	}
	for _, id := range order {
		d := deltas[id]
		c, err := w.d.Store.IncrRunProgress(ctx, id, d.prog)
		if err != nil {
			w.d.Logger.Warn("run progress", "run_id", id, "err", err)
			continue
		}
		w.publish(id, queue.EventProgress, map[string]any{"completed": min(c.Completed, max(c.Total, c.Completed)),
			"failed": c.Failed, "errored": c.Errored, "total": c.Total, "cost_usd": c.CostUSD,
			"tokens_in": c.TokensIn, "tokens_out": c.TokensOut})
		w.publish(id, queue.EventResult, map[string]any{"items": d.compact, "count": int(d.prog.Completed)})
	}
	for _, p := range batch {
		p.done(nil)
	}
}

// compactResult is the row shape of "result" events (enough for a live results table).
func compactResult(p *pending) map[string]any {
	r := &p.res
	out := r.Output
	if len(out) > 280 {
		out = out[:280] + "…"
	}
	scores := make([]map[string]any, len(p.scores))
	for i, s := range p.scores {
		scores[i] = map[string]any{"evaluator_id": s.EvaluatorID, "value": s.Value, "passed": s.Passed, "label": s.Label}
	}
	m := map[string]any{"id": r.ID, "example_id": r.ExampleID, "trial": r.Trial, "status": r.Status,
		"latency_ms": r.LatencyMs, "cost_usd": r.CostUSD, "cached": r.Cached, "output": out, "scores": scores}
	if r.Error != "" {
		m["error"] = r.Error
	}
	return m
}
