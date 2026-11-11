// Package planner turns a suite (plus per-run overrides) into a run: it resolves pinned versions
// (target config, dataset versions, evaluators, gates) into an immutable config snapshot, selects
// and samples examples, splits example × trial items into shards and publishes one JetStream job
// per shard. It is the single entrypoint used by the API, the scheduler, inbound webhooks and the
// CLI (through the API).
package planner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"

	"github.com/prat31/airval/internal/evaluators"
	"github.com/prat31/airval/internal/queue"
	"github.com/prat31/airval/internal/store"
)

// Publisher is the queue surface the planner needs (*queue.Queue implements it).
type Publisher interface {
	PublishJobs(ctx context.Context, jobs []queue.Job) error
	PublishEvent(ctx context.Context, e queue.Event) error
}

// Deps are the planner's collaborators.
type Deps struct {
	Store  store.Store
	Queue  Publisher
	Logger *slog.Logger
	// Now overrides the clock (tests).
	Now func() time.Time
}

// Service plans and starts runs.
type Service struct {
	d Deps
}

// New returns a planner.
func New(d Deps) *Service {
	if d.Logger == nil {
		d.Logger = slog.Default()
	}
	if d.Now == nil {
		d.Now = func() time.Time { return time.Now().UTC() }
	}
	return &Service{d: d}
}

// StartRequest describes a run to start.
type StartRequest struct {
	// Kind: KindSuite (default), KindRerunFailed or KindRescore.
	Kind string
	// SuiteID is required for KindSuite.
	SuiteID uuid.UUID
	// SourceRunID is required for KindRerunFailed and KindRescore.
	SourceRunID uuid.UUID
	// TargetID overrides the suite's target (KindSuite). Nil uses the suite's first target.
	TargetID *uuid.UUID

	Name          string
	Labels        map[string]string
	Sampling      *Sampling
	Trials        *int
	BaselineRunID *uuid.UUID
	Variables     map[string]string
	// EvaluatorIDs selects the evaluators of a rescore (default: the suite's current evaluators).
	EvaluatorIDs []uuid.UUID

	TriggerKind string // manual | schedule | webhook | api (default manual)
	TriggerRef  string
	CreatedBy   *uuid.UUID
}

// suiteConfig mirrors the SuiteConfig schema of the API.
type suiteConfig struct {
	TargetIDs []uuid.UUID `json:"target_ids"`
	Datasets  []struct {
		DatasetID uuid.UUID `json:"dataset_id"`
		Version   *int      `json:"version"`
	} `json:"datasets"`
	Evaluators []struct {
		EvaluatorID   uuid.UUID `json:"evaluator_id"`
		Weight        *float64  `json:"weight"`
		Threshold     *float64  `json:"threshold"`
		Severity      string    `json:"severity"`
		ApplyTo       string    `json:"apply_to"`
		FailFast      bool      `json:"fail_fast"`
		TurnAggregate string    `json:"turn_aggregate"`
	} `json:"evaluators"`
	Trials   int       `json:"trials"`
	Sampling *Sampling `json:"sampling"`
	Limits   *Limits   `json:"limits"`
	Cache    *struct {
		Target *bool `json:"target"`
		Judges *bool `json:"judges"`
	} `json:"cache"`
	Baseline *struct {
		Policy   string     `json:"policy"`
		RunID    *uuid.UUID `json:"run_id"`
		TargetID *uuid.UUID `json:"target_id"`
	} `json:"baseline"`
	FailureBudget  *FailureBudget    `json:"failure_budget"`
	Simulator      *Simulator        `json:"simulator"`
	TimeoutSeconds int               `json:"timeout_seconds"`
	Variables      map[string]string `json:"variables"`
}

func parseSuiteConfig(raw json.RawMessage) (suiteConfig, error) {
	var c suiteConfig
	if len(raw) == 0 {
		return c, nil
	}
	if err := json.Unmarshal(raw, &c); err != nil {
		return c, invalidf("suite config is invalid: %v", err)
	}
	return c, nil
}

// TargetIDs returns the targets a suite runs against by default.
func TargetIDs(s store.Suite) []uuid.UUID {
	c, _ := parseSuiteConfig(s.Config)
	return c.TargetIDs
}

// StartSuite starts one run per target: req.TargetID when set, else every target of the suite.
func (s *Service) StartSuite(ctx context.Context, req StartRequest, targetIDs []uuid.UUID) ([]store.Run, error) {
	if len(targetIDs) == 0 {
		if req.TargetID != nil {
			targetIDs = []uuid.UUID{*req.TargetID}
		} else {
			su, err := s.d.Store.GetSuite(ctx, req.SuiteID)
			if err != nil {
				return nil, err
			}
			targetIDs = TargetIDs(su)
		}
	}
	if len(targetIDs) == 0 {
		return nil, invalidf("the suite has no target: add one to the suite or pass target_ids")
	}
	runs := make([]store.Run, 0, len(targetIDs))
	for _, t := range targetIDs {
		r := req
		r.TargetID = &t
		run, err := s.Start(ctx, r)
		if err != nil {
			return runs, err
		}
		runs = append(runs, run)
	}
	return runs, nil
}

// Start plans and starts one run. The returned run is queued; shard jobs are already published.
func (s *Service) Start(ctx context.Context, req StartRequest) (store.Run, error) {
	ctx, span := otel.Tracer("airval/planner").Start(ctx, "planner.start")
	defer span.End()
	run, items, cfg, err := s.plan(ctx, req)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return store.Run{}, err
	}
	span.SetAttributes(attribute.String("run.id", run.ID.String()), attribute.Int("run.items", len(items)))
	if err := s.launch(ctx, &run, items, cfg); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return run, err
	}
	return run, nil
}

func (s *Service) plan(ctx context.Context, req StartRequest) (store.Run, []Item, RunConfig, error) {
	switch req.Kind {
	case KindSuite:
		return s.planSuite(ctx, req)
	case KindRerunFailed, KindRescore:
		return s.planChild(ctx, req)
	}
	return store.Run{}, nil, RunConfig{}, invalidf("unknown run kind %q", req.Kind)
}

func (s *Service) planSuite(ctx context.Context, req StartRequest) (store.Run, []Item, RunConfig, error) {
	st := s.d.Store
	su, err := st.GetSuite(ctx, req.SuiteID)
	if err != nil {
		return store.Run{}, nil, RunConfig{}, err
	}
	sc, err := parseSuiteConfig(su.Config)
	if err != nil {
		return store.Run{}, nil, RunConfig{}, err
	}
	// Target (pinned to its current config version).
	var targetID uuid.UUID
	switch {
	case req.TargetID != nil:
		targetID = *req.TargetID
	case len(sc.TargetIDs) > 0:
		targetID = sc.TargetIDs[0]
	default:
		return store.Run{}, nil, RunConfig{}, invalidf("the suite has no target: add one to the suite or pass target_ids")
	}
	tgt, err := st.GetTarget(ctx, targetID)
	if errors.Is(err, store.ErrNotFound) || (err == nil && tgt.ProjectID != su.ProjectID) {
		return store.Run{}, nil, RunConfig{}, invalidf("target %s is not a target of this project", targetID)
	} else if err != nil {
		return store.Run{}, nil, RunConfig{}, err
	}
	if tgt.CurrentVersionID == nil {
		return store.Run{}, nil, RunConfig{}, invalidf("target %q has no configuration", tgt.Name)
	}

	cfg := RunConfig{Version: 1, Mode: ModeExecute, SuiteName: su.Name, TargetName: tgt.Name, TargetKind: tgt.Kind,
		Trials: max(1, sc.Trials), TimeoutSeconds: sc.TimeoutSeconds, FailureBudget: sc.FailureBudget, Simulator: sc.Simulator,
		Cache: CachePolicy{Target: false, Judges: true}}
	if sc.Limits != nil {
		cfg.Limits = *sc.Limits
	}
	if sc.Cache != nil {
		if sc.Cache.Target != nil {
			cfg.Cache.Target = *sc.Cache.Target
		}
		if sc.Cache.Judges != nil {
			cfg.Cache.Judges = *sc.Cache.Judges
		}
	}
	if req.Trials != nil {
		if *req.Trials < 1 || *req.Trials > 10 {
			return store.Run{}, nil, RunConfig{}, invalidf("trials must be between 1 and 10")
		}
		cfg.Trials = *req.Trials
	}
	cfg.Trials = min(cfg.Trials, 10)
	cfg.Variables = map[string]string{}
	maps.Copy(cfg.Variables, sc.Variables)
	maps.Copy(cfg.Variables, req.Variables)
	if len(cfg.Variables) == 0 {
		cfg.Variables = nil
	}

	// Evaluators and gates (snapshotted).
	if cfg.Evaluators, err = s.suiteEvaluators(ctx, su, sc, nil); err != nil {
		return store.Run{}, nil, RunConfig{}, err
	}
	if cfg.Gates, err = s.gates(ctx, su.ID); err != nil {
		return store.Run{}, nil, RunConfig{}, err
	}

	// Examples: pinned dataset versions, sampled, expanded by trials.
	if len(sc.Datasets) == 0 {
		return store.Run{}, nil, RunConfig{}, invalidf("the suite has no dataset")
	}
	var cands []candidate
	smp := sc.Sampling
	if req.Sampling != nil {
		smp = req.Sampling
	}
	needTags := smp != nil && smp.Mode == "stratified"
	for _, d := range sc.Datasets {
		vid, err := s.datasetVersion(ctx, su, d.DatasetID, d.Version, req.CreatedBy)
		if err != nil {
			return store.Run{}, nil, RunConfig{}, err
		}
		cfg.DatasetVersionIDs = append(cfg.DatasetVersionIDs, vid)
		vitems, err := st.ListDatasetVersionItems(ctx, vid)
		if err != nil {
			return store.Run{}, nil, RunConfig{}, err
		}
		tags := map[uuid.UUID]string{}
		if needTags {
			if tags, err = s.revisionTags(ctx, vitems); err != nil {
				return store.Run{}, nil, RunConfig{}, err
			}
		}
		for _, it := range vitems {
			cands = append(cands, candidate{item: Item{E: it.ExampleID, R: it.RevisionID}, tag: tags[it.RevisionID]})
		}
	}
	if smp.Active() {
		seed := suiteSeed(su.ID)
		if smp.Seed != nil {
			seed = *smp.Seed
		}
		cp := *smp
		cp.Seed = &seed
		cfg.Sampling = &cp
		cands = sample(cands, &cp, seed)
	}
	items := make([]Item, 0, len(cands)*cfg.Trials)
	for t := 0; t < cfg.Trials; t++ {
		for _, c := range cands {
			it := c.item
			it.T = t
			items = append(items, it)
		}
	}

	run := store.Run{ID: store.NewID(), OrgID: su.OrgID, ProjectID: su.ProjectID, SuiteID: &su.ID, TargetID: &tgt.ID,
		TargetVersionID: tgt.CurrentVersionID, Name: req.Name, Labels: req.Labels, TriggerKind: req.TriggerKind,
		TriggerRef: req.TriggerRef, CreatedBy: req.CreatedBy}
	if len(cfg.DatasetVersionIDs) > 0 {
		run.DatasetVersionID = &cfg.DatasetVersionIDs[0]
	}
	if run.Name == "" {
		run.Name = su.Name + " · " + tgt.Name
	}
	// Baseline.
	if run.BaselineRunID, cfg.Baseline, err = s.baseline(ctx, su, sc, tgt.ID, req.BaselineRunID); err != nil {
		return store.Run{}, nil, RunConfig{}, err
	}
	return run, items, cfg, nil
}

// planChild plans a re-run of failed results or a re-score of a source run.
func (s *Service) planChild(ctx context.Context, req StartRequest) (store.Run, []Item, RunConfig, error) {
	st := s.d.Store
	src, err := st.GetRun(ctx, req.SourceRunID)
	if err != nil {
		return store.Run{}, nil, RunConfig{}, err
	}
	switch src.Status {
	case store.RunQueued, store.RunRunning:
		return store.Run{}, nil, RunConfig{}, invalidf("the run is still in progress")
	}
	cfg, err := ParseRunConfig(src.Config)
	if err != nil {
		return store.Run{}, nil, RunConfig{}, invalidf("the run has no execution snapshot (was it created by this version of airval?)")
	}
	cfg.SourceRunID = &src.ID
	cfg.Sampling = nil
	run := store.Run{ID: store.NewID(), OrgID: src.OrgID, ProjectID: src.ProjectID, SuiteID: src.SuiteID,
		TargetID: src.TargetID, TargetVersionID: src.TargetVersionID, DatasetVersionID: src.DatasetVersionID,
		ParentRunID: &src.ID, BaselineRunID: src.BaselineRunID, Labels: map[string]string{}, Name: req.Name,
		TriggerKind: req.TriggerKind, TriggerRef: req.TriggerRef, CreatedBy: req.CreatedBy}
	maps.Copy(run.Labels, src.Labels)
	maps.Copy(run.Labels, req.Labels)
	var items []Item
	if req.Kind == KindRerunFailed {
		if cfg.Mode == ModeRescore {
			return store.Run{}, nil, RunConfig{}, invalidf("a re-score run cannot be re-run; re-run its source run instead")
		}
		refs, err := st.ListRunResultRefs(ctx, src.ID, []string{store.ResultFailed, store.ResultErrored})
		if err != nil {
			return store.Run{}, nil, RunConfig{}, err
		}
		if len(refs) == 0 {
			return store.Run{}, nil, RunConfig{}, invalidf("the run has no failed or errored results")
		}
		for _, r := range refs {
			items = append(items, Item{E: r.ExampleID, R: r.ExampleRevisionID, T: r.Trial})
		}
		if run.Name == "" {
			run.Name = strings.TrimSpace(src.Name + " · re-run failed")
		}
		run.Labels["rerun_of"] = src.ID.String()
	} else {
		cfg.Mode = ModeRescore
		if src.SuiteID != nil {
			su, err := st.GetSuite(ctx, *src.SuiteID)
			if err != nil && !errors.Is(err, store.ErrNotFound) {
				return store.Run{}, nil, RunConfig{}, err
			}
			if err == nil {
				sc, err := parseSuiteConfig(su.Config)
				if err != nil {
					return store.Run{}, nil, RunConfig{}, err
				}
				if cfg.Evaluators, err = s.suiteEvaluators(ctx, su, sc, req.EvaluatorIDs); err != nil {
					return store.Run{}, nil, RunConfig{}, err
				}
				if cfg.Gates, err = s.gates(ctx, su.ID); err != nil {
					return store.Run{}, nil, RunConfig{}, err
				}
			}
		} else if len(req.EvaluatorIDs) > 0 {
			if cfg.Evaluators, err = s.loadEvaluators(ctx, src.ProjectID, req.EvaluatorIDs, nil); err != nil {
				return store.Run{}, nil, RunConfig{}, err
			}
		}
		if len(cfg.Evaluators) == 0 {
			return store.Run{}, nil, RunConfig{}, invalidf("no evaluators to apply")
		}
		refs, err := st.ListRunResultRefs(ctx, src.ID, nil)
		if err != nil {
			return store.Run{}, nil, RunConfig{}, err
		}
		if len(refs) == 0 {
			return store.Run{}, nil, RunConfig{}, invalidf("the run has no results to re-score")
		}
		for _, r := range refs {
			id := r.ID
			items = append(items, Item{E: r.ExampleID, R: r.ExampleRevisionID, T: r.Trial, S: &id})
		}
		if run.Name == "" {
			run.Name = strings.TrimSpace(src.Name + " · re-score")
		}
		run.Labels["rescore_of"] = src.ID.String()
	}
	return run, items, cfg, nil
}

// launch persists the run and its shards, then publishes the shard jobs.
func (s *Service) launch(ctx context.Context, run *store.Run, items []Item, cfg RunConfig) error {
	st := s.d.Store
	size := ShardSize(len(items))
	var shards [][]Item
	for i := 0; i < len(items); i += size {
		shards = append(shards, items[i:min(i+size, len(items))])
	}
	cfg.Shards, cfg.ShardSize = len(shards), size
	raw, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	if run.TriggerKind == "" {
		run.TriggerKind = "manual"
	}
	if run.Labels == nil {
		run.Labels = map[string]string{}
	}
	run.Config, run.Total, run.Status = raw, len(items), store.RunQueued
	counts := make([]int, len(shards))
	for i, sh := range shards {
		counts[i] = len(sh)
	}
	if err := st.WithTx(ctx, func(tx store.Store) error {
		if err := tx.CreateRun(ctx, run); err != nil {
			return err
		}
		return tx.CreateRunShards(ctx, run.ID, counts)
	}); err != nil {
		return err
	}
	trace := map[string]string{}
	otel.GetTextMapPropagator().Inject(ctx, propagation.MapCarrier(trace))
	kind := queue.JobExecute
	if cfg.Mode == ModeRescore {
		kind = queue.JobRescore
	}
	jobs := make([]queue.Job, 0, max(1, len(shards)))
	for i, sh := range shards {
		p, err := json.Marshal(ShardPayload{Items: sh, Trace: trace})
		if err != nil {
			return err
		}
		jobs = append(jobs, queue.Job{Kind: kind, RunID: run.ID.String(), Shard: i, ShardCount: len(shards), Payload: p})
	}
	if len(shards) == 0 { // nothing to execute: finalize right away (empty summary)
		jobs = append(jobs, queue.Job{Kind: queue.JobFinalize, RunID: run.ID.String()})
	}
	if s.d.Queue == nil {
		return s.fail(ctx, run, errors.New("no job queue configured"))
	}
	if err := s.d.Queue.PublishJobs(ctx, jobs); err != nil {
		return s.fail(ctx, run, fmt.Errorf("publish shard jobs: %w", err))
	}
	ev, _ := json.Marshal(map[string]any{"status": run.Status, "total": run.Total})
	_ = s.d.Queue.PublishEvent(ctx, queue.Event{Type: queue.EventRunStatus, RunID: run.ID.String(), Data: ev})
	s.d.Logger.Info("run planned", "run_id", run.ID, "items", len(items), "shards", len(shards), "shard_size", size,
		"mode", cfg.Mode, "trigger", run.TriggerKind)
	return nil
}

func (s *Service) fail(ctx context.Context, run *store.Run, cause error) error {
	ctx = context.WithoutCancel(ctx)
	if st, err := s.d.Store.FinishRun(ctx, run.ID, store.RunFinish{Status: store.RunErrored, Error: cause.Error(),
		FinishedAt: s.d.Now()}); err == nil {
		run.Status = st
	}
	run.Error = cause.Error()
	return cause
}

// suiteEvaluators snapshots the suite's evaluators (optionally restricted to ids, which may also
// name project evaluators outside the suite).
func (s *Service) suiteEvaluators(ctx context.Context, su store.Suite, sc suiteConfig, only []uuid.UUID) ([]EvaluatorRef, error) {
	settings := map[uuid.UUID]EvaluatorRef{}
	var ids []uuid.UUID
	for _, e := range sc.Evaluators {
		ref := EvaluatorRef{ID: e.EvaluatorID, Threshold: e.Threshold, Severity: e.Severity, ApplyTo: e.ApplyTo,
			FailFast: e.FailFast, TurnAggregate: e.TurnAggregate, Weight: 1}
		if e.Weight != nil {
			ref.Weight = *e.Weight
		}
		settings[e.EvaluatorID] = ref
		ids = append(ids, e.EvaluatorID)
	}
	if len(only) > 0 {
		ids = only
	}
	return s.loadEvaluators(ctx, su.ProjectID, ids, settings)
}

func (s *Service) loadEvaluators(ctx context.Context, projectID uuid.UUID, ids []uuid.UUID, settings map[uuid.UUID]EvaluatorRef) ([]EvaluatorRef, error) {
	if len(ids) == 0 {
		return []EvaluatorRef{}, nil
	}
	evs, err := s.d.Store.GetEvaluators(ctx, ids)
	if err != nil {
		return nil, err
	}
	byID := map[uuid.UUID]store.Evaluator{}
	for _, e := range evs {
		if e.ProjectID == projectID {
			byID[e.ID] = e
		}
	}
	out := make([]EvaluatorRef, 0, len(ids))
	seen := map[uuid.UUID]bool{}
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		e, ok := byID[id]
		if !ok {
			return nil, invalidf("evaluator %s is not an evaluator of this project", id)
		}
		if err := evaluators.ValidateConfig(e.Kind, e.Config); err != nil {
			return nil, invalidf("evaluator %q: %v", e.Name, err)
		}
		ref, ok := settings[id]
		if !ok {
			ref = EvaluatorRef{ID: id, Weight: 1}
		}
		ref.Name, ref.Kind, ref.Config, ref.Version = e.Name, e.Kind, e.Config, e.Version
		out = append(out, ref)
	}
	return out, nil
}

func (s *Service) gates(ctx context.Context, suiteID uuid.UUID) ([]GateRef, error) {
	gs, err := s.d.Store.ListGates(ctx, suiteID)
	if err != nil {
		return nil, err
	}
	var out []GateRef
	for _, g := range gs {
		if g.Enabled {
			out = append(out, GateRef{ID: g.ID, Name: g.Name, Expr: g.Expr})
		}
	}
	return out, nil
}

// datasetVersion resolves a suite dataset to a version id: the pinned number, else the latest
// published version. A dataset that was never published is published once automatically so a
// first run works straight after an import.
func (s *Service) datasetVersion(ctx context.Context, su store.Suite, id uuid.UUID, number *int, by *uuid.UUID) (uuid.UUID, error) {
	st := s.d.Store
	ds, err := st.GetDataset(ctx, id)
	if errors.Is(err, store.ErrNotFound) || (err == nil && ds.ProjectID != su.ProjectID) {
		return uuid.Nil, invalidf("dataset %s is not a dataset of this project", id)
	} else if err != nil {
		return uuid.Nil, err
	}
	if number != nil {
		v, err := st.GetDatasetVersionByNumber(ctx, id, *number)
		if errors.Is(err, store.ErrNotFound) {
			return uuid.Nil, invalidf("dataset %q has no version %d", ds.Name, *number)
		}
		return v.ID, err
	}
	if ds.LatestVersionID != nil {
		return *ds.LatestVersionID, nil
	}
	if ds.ExampleCount == 0 {
		return uuid.Nil, invalidf("dataset %q has no examples", ds.Name)
	}
	v, err := st.PublishDatasetVersion(ctx, id, "Published automatically for the first run", by)
	if err != nil {
		return uuid.Nil, err
	}
	return v.ID, nil
}

func (s *Service) revisionTags(ctx context.Context, items []store.VersionItem) (map[uuid.UUID]string, error) {
	out := make(map[uuid.UUID]string, len(items))
	ids := make([]uuid.UUID, len(items))
	for i, it := range items {
		ids[i] = it.RevisionID
	}
	for i := 0; i < len(ids); i += 1000 {
		revs, err := s.d.Store.GetExampleRevisions(ctx, ids[i:min(i+1000, len(ids))])
		if err != nil {
			return nil, err
		}
		for _, r := range revs {
			if len(r.Tags) > 0 {
				out[r.RevisionID] = slices.Min(r.Tags)
			}
		}
	}
	return out, nil
}

// baseline resolves the run's baseline: an explicit override, else the suite policy
// (pinned run | last passing run on a target | none). Default policy: last_passing.
func (s *Service) baseline(ctx context.Context, su store.Suite, sc suiteConfig, targetID uuid.UUID, override *uuid.UUID) (*uuid.UUID, string, error) {
	st := s.d.Store
	if override != nil {
		r, err := st.GetRun(ctx, *override)
		if err != nil || r.ProjectID != su.ProjectID {
			return nil, "", invalidf("baseline_run_id must be a run of this project")
		}
		return override, "override", nil
	}
	policy := "last_passing"
	var pinned, onTarget *uuid.UUID
	if sc.Baseline != nil {
		if sc.Baseline.Policy != "" {
			policy = sc.Baseline.Policy
		}
		pinned, onTarget = sc.Baseline.RunID, sc.Baseline.TargetID
	}
	if su.BaselineRunID != nil && (sc.Baseline == nil || sc.Baseline.Policy == "") {
		policy, pinned = "pinned", su.BaselineRunID
	}
	switch policy {
	case "none":
		return nil, policy, nil
	case "pinned":
		if pinned == nil {
			pinned = su.BaselineRunID
		}
		return pinned, policy, nil
	case "last_passing":
		t := targetID
		if onTarget != nil {
			t = *onTarget
		}
		r, err := st.LatestRun(ctx, store.RunFilter{SuiteID: &su.ID, TargetID: &t, Statuses: []string{store.RunSucceeded}})
		if errors.Is(err, store.ErrNotFound) {
			return nil, policy, nil
		} else if err != nil {
			return nil, policy, err
		}
		return &r.ID, policy, nil
	}
	return nil, "", invalidf("unknown baseline policy %q", policy)
}

// suiteSeed is the default sampling seed: stable per suite, so repeated PR checks sample the same
// examples and their results stay comparable.
func suiteSeed(id uuid.UUID) int64 {
	h := fnv.New64a()
	h.Write(id[:])
	return int64(h.Sum64() >> 1)
}
