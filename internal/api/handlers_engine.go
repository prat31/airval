package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/prat31/airval/internal/api/gen"
	"github.com/prat31/airval/internal/planner"
	"github.com/prat31/airval/internal/queue"
	"github.com/prat31/airval/internal/store"
)

// ---- run execution (engine) ----

var plannerOnce sync.Map // *Server → *planner.Service

// planner returns the server's run planner (built on first use from Store + Queue).
func (s *Server) planner() *planner.Service {
	if p, ok := plannerOnce.Load(s); ok {
		return p.(*planner.Service)
	}
	var pub planner.Publisher
	if s.Queue != nil {
		pub = s.Queue
	}
	p, _ := plannerOnce.LoadOrStore(s, planner.New(planner.Deps{Store: s.Store, Queue: pub, Logger: s.Logger}))
	return p.(*planner.Service)
}

// engineErr maps planner validation errors to 400 validation_failed.
func engineErr(err error) error {
	var ie *planner.InvalidError
	if errors.As(err, &ie) {
		return invalid("%s", ie.Msg)
	}
	return err
}

func (s *Server) requireQueue() error {
	if s.Queue == nil {
		return errorf(http.StatusServiceUnavailable, "queue_unavailable", "the job queue is not available")
	}
	return nil
}

func fromSampling(g *gen.Sampling) *planner.Sampling {
	if g == nil {
		return nil
	}
	out := &planner.Sampling{Seed: g.Seed}
	if g.Mode != nil {
		out.Mode = string(*g.Mode)
	}
	if g.N != nil {
		out.N = *g.N
	}
	if g.Percent != nil {
		out.Percent = *g.Percent
	}
	return out
}

func (s *Server) triggerKind(ctx context.Context) string {
	if s.principal(ctx).IsAPIKey() {
		return "api"
	}
	return "manual"
}

func (s *Server) CreateRun(ctx context.Context, req gen.CreateRunRequestObject) (gen.CreateRunResponseObject, error) {
	b := req.Body
	if b == nil {
		return nil, invalid("suite_id is required")
	}
	su, err := s.suite(ctx, b.SuiteId, store.RoleEditor)
	if err != nil {
		return nil, err
	}
	if err := s.requireQueue(); err != nil {
		return nil, err
	}
	sr := planner.StartRequest{SuiteID: su.ID, Name: deref(b.Name, ""), Sampling: fromSampling(b.Sampling), Trials: b.Trials,
		BaselineRunID: b.BaselineRunId, TriggerKind: s.triggerKind(ctx), TriggerRef: deref(b.TriggerRef, ""),
		CreatedBy: s.principal(ctx).UserID()}
	if b.Labels != nil {
		sr.Labels = map[string]string(*b.Labels)
	}
	if b.Variables != nil {
		sr.Variables = *b.Variables
	}
	var targets []uuid.UUID
	if b.TargetIds != nil {
		targets = *b.TargetIds
	}
	runs, err := s.planner().StartSuite(ctx, sr, targets)
	if err != nil {
		return nil, engineErr(err)
	}
	return gen.CreateRun201JSONResponse{Runs: mapSlice(runs, toRun)}, nil
}

func (s *Server) CancelRun(ctx context.Context, req gen.CancelRunRequestObject) (gen.CancelRunResponseObject, error) {
	r, err := s.run(ctx, req.Run, store.RoleEditor)
	if err != nil {
		return nil, err
	}
	if r.Status != store.RunQueued && r.Status != store.RunRunning {
		return nil, errorf(http.StatusConflict, "run_finished", "the run has already finished (%s)", r.Status)
	}
	if err := s.requireQueue(); err != nil {
		return nil, err
	}
	if err := s.Queue.SetCancel(ctx, r.ID.String()); err != nil {
		return nil, err
	}
	if ok, err := s.Store.TransitionRun(ctx, r.ID, []string{store.RunQueued, store.RunRunning}, store.RunCanceled, time.Now()); err != nil {
		return nil, err
	} else if ok {
		data, _ := json.Marshal(map[string]any{"status": store.RunCanceled})
		_ = s.Queue.PublishEvent(ctx, queue.Event{Type: queue.EventRunStatus, RunID: r.ID.String(), Data: data})
		s.audit(ctx, r.OrgID, "run.cancel", "run:"+r.ID.String(), nil)
	}
	if r, err = s.Store.GetRun(ctx, r.ID); err != nil {
		return nil, err
	}
	return gen.CancelRun200JSONResponse(toRun(r)), nil
}

func (s *Server) RerunFailed(ctx context.Context, req gen.RerunFailedRequestObject) (gen.RerunFailedResponseObject, error) {
	r, err := s.run(ctx, req.Run, store.RoleEditor)
	if err != nil {
		return nil, err
	}
	if err := s.requireQueue(); err != nil {
		return nil, err
	}
	child, err := s.planner().Start(ctx, planner.StartRequest{Kind: planner.KindRerunFailed, SourceRunID: r.ID,
		TriggerKind: s.triggerKind(ctx), CreatedBy: s.principal(ctx).UserID()})
	if err != nil {
		return nil, engineErr(err)
	}
	return gen.RerunFailed201JSONResponse(toRun(child)), nil
}

func (s *Server) RescoreRun(ctx context.Context, req gen.RescoreRunRequestObject) (gen.RescoreRunResponseObject, error) {
	r, err := s.run(ctx, req.Run, store.RoleEditor)
	if err != nil {
		return nil, err
	}
	if err := s.requireQueue(); err != nil {
		return nil, err
	}
	sr := planner.StartRequest{Kind: planner.KindRescore, SourceRunID: r.ID, TriggerKind: s.triggerKind(ctx),
		CreatedBy: s.principal(ctx).UserID()}
	if req.Body != nil && req.Body.EvaluatorIds != nil {
		sr.EvaluatorIDs = *req.Body.EvaluatorIds
	}
	child, err := s.planner().Start(ctx, sr)
	if err != nil {
		return nil, engineErr(err)
	}
	return gen.RescoreRun201JSONResponse(toRun(child)), nil
}

// ---- live events (SSE) ----

func (s *Server) StreamRunEvents(ctx context.Context, req gen.StreamRunEventsRequestObject) (gen.StreamRunEventsResponseObject, error) {
	r, err := s.run(ctx, req.Run, store.RoleViewer)
	if err != nil {
		return nil, err
	}
	if err := s.requireQueue(); err != nil {
		return nil, err
	}
	var last uint64
	if req.Params.LastEventID != nil {
		last, _ = strconv.ParseUint(strings.TrimSpace(*req.Params.LastEventID), 10, 64)
	}
	return sseStream{s: s, ctx: ctx, run: r, last: last}, nil
}

// sseStream writes a run's events as Server-Sent Events. A fresh connection first gets a
// snapshot (run.status + run.progress from the database), then live events from JetStream
// (so it works on any server replica); a reconnect with Last-Event-ID resumes exactly after that
// sequence. Comment heartbeats every 15 s keep proxies from closing idle streams; the stream ends
// after run.finished.
type sseStream struct {
	s    *Server
	ctx  context.Context
	run  store.Run
	last uint64
}

const sseHeartbeat = 15 * time.Second

func isTerminal(status string) bool {
	switch status {
	case store.RunSucceeded, store.RunFailed, store.RunErrored, store.RunCanceled:
		return true
	}
	return false
}

func (st sseStream) VisitStreamRunEventsResponse(w http.ResponseWriter) error {
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache, no-transform")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no") // nginx: don't buffer
	w.WriteHeader(http.StatusOK)
	rc := http.NewResponseController(w)
	_ = rc.SetWriteDeadline(time.Time{})
	flush := func() { _ = rc.Flush() }
	write := func(e queue.Event) error {
		b, err := json.Marshal(e)
		if err != nil {
			return err
		}
		if e.Seq > 0 {
			if _, err := fmt.Fprintf(w, "id: %d\n", e.Seq); err != nil {
				return err
			}
		}
		_, err = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", e.Type, b)
		flush()
		return err
	}
	if _, err := io.WriteString(w, "retry: 3000\n\n"); err != nil {
		return nil
	}
	ctx, cancel := context.WithCancel(st.ctx)
	defer cancel()
	runID := st.run.ID.String()
	events, err := st.s.Queue.SubscribeEvents(ctx, runID, st.last)
	if err != nil {
		st.s.Logger.Warn("sse: subscribe", "run_id", runID, "err", err)
		return nil
	}
	snapAt := time.Now()
	if st.last == 0 {
		r, err := st.s.Store.GetRun(ctx, st.run.ID)
		if err != nil {
			return nil
		}
		for _, e := range snapshotEvents(r) {
			if write(e) != nil {
				return nil
			}
		}
		if isTerminal(r.Status) {
			return nil // snapshot already carries run.finished
		}
	}
	hb := time.NewTicker(sseHeartbeat)
	defer hb.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case e, ok := <-events:
			if !ok {
				return nil
			}
			// Replayed history older than the snapshot is redundant (except the final event).
			if st.last == 0 && e.At.Before(snapAt) && e.Type != queue.EventFinished {
				continue
			}
			if write(e) != nil {
				return nil
			}
			if e.Type == queue.EventFinished {
				return nil
			}
		case <-hb.C:
			if _, err := io.WriteString(w, ": ping\n\n"); err != nil {
				return nil
			}
			flush()
			// Events may have expired from the stream: fall back to the database.
			if r, err := st.s.Store.GetRun(ctx, st.run.ID); err == nil && isTerminal(r.Status) && r.FinishedAt != nil &&
				time.Since(*r.FinishedAt) > 5*time.Second {
				for _, e := range snapshotEvents(r) {
					_ = write(e)
				}
				return nil
			}
		}
	}
}

// snapshotEvents renders the current state of a run as events (no sequence ids).
func snapshotEvents(r store.Run) []queue.Event {
	now := time.Now().UTC()
	id := r.ID.String()
	mk := func(typ string, data any) queue.Event {
		b, _ := json.Marshal(data)
		return queue.Event{Type: typ, RunID: id, At: now, Data: b}
	}
	out := []queue.Event{
		mk(queue.EventRunStatus, map[string]any{"status": r.Status, "total": r.Total, "gate_passed": r.GatePassed, "snapshot": true}),
		mk(queue.EventProgress, map[string]any{"completed": r.Completed, "failed": r.Failed, "errored": r.Errored,
			"total": r.Total, "cost_usd": r.CostUSD, "tokens_in": r.TokensIn, "tokens_out": r.TokensOut, "snapshot": true}),
	}
	if isTerminal(r.Status) {
		out = append(out, mk(queue.EventFinished, map[string]any{"status": r.Status, "gate_passed": r.GatePassed,
			"summary": json.RawMessage(rawOr(r.Summary, "{}")), "error": r.Error, "snapshot": true}))
	}
	return out
}
