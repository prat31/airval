package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/prat31/airval/internal/aggregate"
	"github.com/prat31/airval/internal/evalcore"
	"github.com/prat31/airval/internal/evaluators"
	"github.com/prat31/airval/internal/planner"
	"github.com/prat31/airval/internal/secrets"
	"github.com/prat31/airval/internal/store"
	"github.com/prat31/airval/internal/targets"
)

// runState is everything a worker needs to execute a run's shards, built once per run per
// process: the config snapshot, the compiled target adapter, the evaluator pipeline, the
// target limiter, the secret resolver and the failure-budget tracker.
type runState struct {
	id            uuid.UUID
	run           store.Run
	cfg           planner.RunConfig
	adapter       targets.Adapter
	simulator     evalcore.LLM // plays the user in simulated conversations (nil = none configured)
	pipeline      *evaluators.Pipeline
	evalInfos     []aggregate.EvaluatorInfo
	needsBaseline bool
	lim           *limiter
	resolver      *syncResolver
	budget        *aggregate.BudgetTracker
	targetKey     []byte // target cache key prefix (version + variables)
	buildErr      error  // the run cannot execute (bad config): shards record errors

	started  atomic.Bool
	stopped  atomic.Bool
	lastUsed atomic.Int64

	rqMu         sync.Mutex
	reviewQueues map[string]uuid.UUID
}

func (rs *runState) touch() { rs.lastUsed.Store(time.Now().UnixNano()) }

// syncResolver makes secrets.Resolver safe for concurrent use.
type syncResolver struct {
	mu sync.Mutex
	r  *secrets.Resolver
}

func (s *syncResolver) Resolve(name string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.r.Resolve(name)
}

// applyVariables substitutes {{ .vars.NAME }} references in a raw target config with run variables
// (suite variables, overridden by run/webhook variables). See targets.ApplyVariables.
func applyVariables(raw json.RawMessage, vars map[string]string) (json.RawMessage, error) {
	return targets.ApplyVariables(raw, vars)
}

// state returns the cached runState of runID, building it on first use.
func (w *Engine) state(ctx context.Context, runID uuid.UUID) (*runState, error) {
	w.runsMu.Lock()
	rs, ok := w.runs[runID]
	if !ok {
		rs = &runState{id: runID}
		w.runs[runID] = rs
	}
	w.runsMu.Unlock()
	rs.touch()
	w.buildMu(runID).Lock()
	defer w.buildMu(runID).Unlock()
	if rs.cfg.Version != 0 || rs.buildErr != nil {
		return rs, nil
	}
	run, err := w.d.Store.GetRun(ctx, runID)
	if err != nil {
		w.runsMu.Lock()
		delete(w.runs, runID)
		w.runsMu.Unlock()
		return nil, err
	}
	rs.run = run
	cfg, err := planner.ParseRunConfig(run.Config)
	if err != nil {
		rs.buildErr = err
		return rs, nil
	}
	rs.cfg = cfg
	if cfg.Version == 0 {
		rs.cfg.Version = 1
	}
	rs.resolver = &syncResolver{r: &secrets.Resolver{Ctx: w.bg, Store: w.d.Store, Keyring: w.d.Keyring, OrgID: run.OrgID,
		ProjectID: &run.ProjectID}}
	rs.buildErr = w.build(ctx, rs)
	return rs, nil
}

func (w *Engine) buildMu(runID uuid.UUID) *sync.Mutex {
	w.runsMu.Lock()
	defer w.runsMu.Unlock()
	m, ok := w.buildLocks[runID]
	if !ok {
		m = &sync.Mutex{}
		w.buildLocks[runID] = m
	}
	return m
}

func (w *Engine) build(ctx context.Context, rs *runState) error {
	cfg := rs.cfg
	// Evaluators.
	deps := evaluators.Deps{LLMs: w.judges.factory(rs.resolver), Embedders: w.judges.embedderFactory(rs.resolver)}
	if sim := cfg.Simulator; sim != nil && sim.Provider != "" {
		l, err := w.judges.simulator(rs.resolver, sim.Provider, sim.Model, sim.Secret, sim.BaseURL)
		if err != nil {
			return fmt.Errorf("conversation simulator: %w", err)
		}
		rs.simulator = l
	}
	if cfg.Cache.Judges && w.cache != nil {
		deps.Cache = w.cache
	}
	bounds := make([]*evaluators.Bound, 0, len(cfg.Evaluators))
	for _, e := range cfg.Evaluators {
		spec := evaluators.Spec{ID: e.ID.String(), Kind: e.Kind, Config: e.Config, Threshold: e.Threshold,
			Severity: evalcore.Severity(e.Severity), ApplyTo: evalcore.ApplyTo(e.ApplyTo), TurnAggregate: e.TurnAggregate,
			FailFast: e.FailFast, Weight: e.Weight}
		b, err := evaluators.Build(spec, deps)
		if err != nil {
			return fmt.Errorf("evaluator %q: %w", e.Name, err)
		}
		bounds = append(bounds, b)
		rs.evalInfos = append(rs.evalInfos, aggregate.EvaluatorInfo{ID: spec.ID, Kind: e.Kind, Severity: b.Severity,
			Threshold: b.Threshold()})
		if info, err := evaluators.Info(e.Kind); err == nil && slices.Contains(info.Requires, evaluators.NeedsBaseline) {
			rs.needsBaseline = true
		}
	}
	p, err := evaluators.NewPipeline(bounds)
	if err != nil {
		return err
	}
	p.MaxParallel = 8
	rs.pipeline = p

	// Failure budget: explicit, plus (for CI triggers) limits implied by count(...) gate clauses.
	var budget aggregate.FailureBudget
	if fb := cfg.FailureBudget; fb != nil {
		budget = aggregate.FailureBudget{MaxCriticalFails: fb.CriticalFailures, MaxFails: fb.Failures, MaxErrors: fb.Errors,
			MaxFailRate: fb.FailRate, MinResults: fb.MinResults}
	}
	if rs.run.TriggerKind == "api" || rs.run.TriggerKind == "webhook" {
		for _, g := range cfg.Gates {
			if n, err := GateNode(g.Expr); err == nil {
				budget = budget.Merge(aggregate.BudgetFromGate(n))
			}
		}
	}
	if !budget.IsZero() {
		rs.budget = aggregate.NewBudgetTracker(budget, rs.evalInfos)
	}

	if cfg.Mode == planner.ModeRescore {
		return nil
	}
	// Target adapter, pinned to the run's target version.
	if rs.run.TargetVersionID == nil {
		return errors.New("run has no target version")
	}
	tv, err := w.d.Store.GetTargetVersion(ctx, *rs.run.TargetVersionID)
	if err != nil {
		return fmt.Errorf("load target version: %w", err)
	}
	kind := cfg.TargetKind
	if kind == "" {
		t, err := w.d.Store.GetTarget(ctx, tv.TargetID)
		if err != nil {
			return fmt.Errorf("load target: %w", err)
		}
		kind = t.Kind
	}
	raw, err := applyVariables(tv.Config, cfg.Variables)
	if err != nil {
		return err
	}
	ad, err := targets.New(kind, raw, targets.Deps{Clients: w.clients, DefaultConcurrency: w.d.Config.Concurrency})
	if err != nil {
		return fmt.Errorf("target: %w", err)
	}
	rs.adapter = ad
	// Cache key prefix: target version + variables (sorted).
	keys := make([]string, 0, len(cfg.Variables))
	for k := range cfg.Variables {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var kb strings.Builder
	kb.WriteString(tv.ID.String())
	for _, k := range keys {
		kb.WriteString("\x00" + k + "=" + cfg.Variables[k])
	}
	rs.targetKey = []byte(kb.String())
	// Limiter shared by every run of this target in the process.
	maxC := w.d.Config.Concurrency
	if cfg.Limits.MaxConcurrency > 0 {
		maxC = min(maxC, cfg.Limits.MaxConcurrency)
	}
	rs.lim = w.targetLimiter(tv.TargetID, maxC, cfg.Limits)
	return nil
}

func (w *Engine) targetLimiter(targetID uuid.UUID, maxC int, l planner.Limits) *limiter {
	w.limMu.Lock()
	defer w.limMu.Unlock()
	lim, ok := w.limiters[targetID]
	if ok {
		lim.configure(maxC, l.AdaptiveOn())
		return lim
	}
	lim = newLimiter(maxC, l.AdaptiveOn())
	// Rate buckets are attached before the limiter is shared (Acquire reads them without locks).
	if w.d.Queue != nil {
		if l.RPM > 0 {
			lim.rpm = newLeased(w, "rpm."+targetID.String(), float64(l.RPM)/60)
		}
		if l.TPM > 0 {
			lim.tpm = w.d.Queue.TokenBucket("tpm."+targetID.String(), float64(l.TPM)/60, float64(l.TPM)/6)
		}
	}
	w.limiters[targetID] = lim
	return lim
}

// reviewQueue returns (creating if needed) the review queue for an evaluator: the queue named in
// the human evaluator's config, else one named after the evaluator.
func (w *Engine) reviewQueue(ctx context.Context, rs *runState, evalID uuid.UUID, review map[string]any) (uuid.UUID, error) {
	name, _ := review["queue"].(string)
	if name == "" {
		for _, e := range rs.cfg.Evaluators {
			if e.ID == evalID {
				name = e.Name
			}
		}
		if name == "" {
			name = "Review"
		}
	}
	key := rs.run.ProjectID.String() + "\x00" + name
	w.rqMu.Lock()
	defer w.rqMu.Unlock()
	if id, ok := w.reviewQueues[key]; ok {
		return id, nil
	}
	p := store.ListParams{Limit: store.MaxLimit}
	for {
		pg, err := w.d.Store.ListReviewQueues(ctx, rs.run.ProjectID, p)
		if err != nil {
			return uuid.Nil, err
		}
		for _, q := range pg.Items {
			if q.Name == name {
				w.reviewQueues[key] = q.ID
				return q.ID, nil
			}
		}
		if pg.NextCursor == "" {
			break
		}
		p.Cursor = pg.NextCursor
	}
	cfg, _ := json.Marshal(review)
	q := store.ReviewQueue{OrgID: rs.run.OrgID, ProjectID: rs.run.ProjectID, Name: name, EvaluatorID: &evalID, Config: cfg,
		Description: "Created by airval for human review of run results"}
	if err := w.d.Store.CreateReviewQueue(ctx, &q); err != nil {
		return uuid.Nil, err
	}
	w.reviewQueues[key] = q.ID
	return q.ID, nil
}
