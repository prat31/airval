package api

import (
	"context"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/prat31/airval/internal/api/gen"
	"github.com/prat31/airval/internal/store"
)

func (s *Server) run(ctx context.Context, id uuid.UUID, min store.Role) (store.Run, error) {
	r, err := s.Store.GetRun(ctx, id)
	if err != nil {
		return r, err
	}
	return r, s.require(ctx, r.OrgID, &r.ProjectID, min)
}

func (s *Server) ListRuns(ctx context.Context, req gen.ListRunsRequestObject) (gen.ListRunsResponseObject, error) {
	p := req.Params
	f := store.RunFilter{SuiteID: p.SuiteId, TargetID: p.TargetId, ParentRunID: p.ParentRunId, CreatedAfter: p.From, CreatedBefore: p.To}
	switch {
	case p.ProjectId != nil:
		if _, err := s.project(ctx, *p.ProjectId, store.RoleViewer); err != nil {
			return nil, err
		}
		f.ProjectID = p.ProjectId
	case p.SuiteId != nil:
		x, err := s.suite(ctx, *p.SuiteId, store.RoleViewer)
		if err != nil {
			return nil, err
		}
		f.ProjectID = &x.ProjectID
	default:
		orgs, onlyProject, err := s.Authz.OrgIDs(ctx, s.principal(ctx))
		if err != nil {
			return nil, err
		}
		if len(orgs) == 0 {
			return gen.ListRuns200JSONResponse{Items: []gen.Run{}}, nil
		}
		f.OrgIDs, f.ProjectID = orgs, onlyProject
	}
	if p.Status != nil {
		for _, st := range *p.Status {
			f.Statuses = append(f.Statuses, string(st))
		}
	}
	if p.Trigger != nil {
		f.TriggerKind = string(*p.Trigger)
	}
	if p.Label != nil {
		f.Labels = map[string]string{}
		for _, l := range *p.Label {
			k, v, ok := strings.Cut(l, ":")
			if !ok || k == "" {
				return nil, badRequest("label filters must be key:value")
			}
			f.Labels[k] = v
		}
	}
	pg, err := s.Store.ListRuns(ctx, f, listParams(p.Cursor, p.Limit))
	if err != nil {
		return nil, err
	}
	return gen.ListRuns200JSONResponse{Items: mapSlice(pg.Items, toRun), NextCursor: nextCursor(pg.NextCursor)}, nil
}

func (s *Server) GetRun(ctx context.Context, req gen.GetRunRequestObject) (gen.GetRunResponseObject, error) {
	r, err := s.run(ctx, req.Run, store.RoleViewer)
	if err != nil {
		return nil, err
	}
	return gen.GetRun200JSONResponse(toRun(r)), nil
}

func (s *Server) UpdateRun(ctx context.Context, req gen.UpdateRunRequestObject) (gen.UpdateRunResponseObject, error) {
	r, err := s.run(ctx, req.Run, store.RoleEditor)
	if err != nil {
		return nil, err
	}
	if b := req.Body; b != nil {
		if b.Name != nil {
			r.Name = strings.TrimSpace(*b.Name)
		}
		if b.Labels != nil {
			r.Labels = map[string]string(*b.Labels)
		}
	}
	if err := s.Store.UpdateRun(ctx, &r); err != nil {
		return nil, err
	}
	return gen.UpdateRun200JSONResponse(toRun(r)), nil
}

func (s *Server) DeleteRun(ctx context.Context, req gen.DeleteRunRequestObject) (gen.DeleteRunResponseObject, error) {
	r, err := s.run(ctx, req.Run, store.RoleEditor)
	if err != nil {
		return nil, err
	}
	if r.Status == store.RunQueued || r.Status == store.RunRunning {
		return nil, errorf(http.StatusConflict, "run_active", "cancel the run before deleting it")
	}
	if err := s.Store.DeleteRun(ctx, r.ID); err != nil {
		return nil, err
	}
	return gen.DeleteRun204Response{}, nil
}

func (s *Server) ListResults(ctx context.Context, req gen.ListResultsRequestObject) (gen.ListResultsResponseObject, error) {
	if _, err := s.run(ctx, req.Run, store.RoleViewer); err != nil {
		return nil, err
	}
	p := req.Params
	f := store.ResultFilter{RunID: req.Run, EvaluatorID: p.EvaluatorId, Passed: p.Passed, ScoreMin: p.ScoreMin,
		ScoreMax: p.ScoreMax, Tag: deref(p.Tag, ""), Search: deref(p.Q, ""), ExampleID: p.ExampleId, Trial: p.Trial}
	if (p.Passed != nil || p.ScoreMin != nil || p.ScoreMax != nil) && p.EvaluatorId == nil {
		return nil, badRequest("passed/score_min/score_max require evaluator_id")
	}
	if p.Status != nil {
		for _, st := range *p.Status {
			f.Statuses = append(f.Statuses, string(st))
		}
	}
	pg, err := s.Store.ListResults(ctx, f, listParams(p.Cursor, p.Limit))
	if err != nil {
		return nil, err
	}
	full := deref(p.Full, false)
	return gen.ListResults200JSONResponse{Items: mapSlice(pg.Items, func(r store.Result) gen.Result { return toResult(r, full) }),
		NextCursor: nextCursor(pg.NextCursor)}, nil
}

func (s *Server) GetResult(ctx context.Context, req gen.GetResultRequestObject) (gen.GetResultResponseObject, error) {
	if _, err := s.run(ctx, req.Run, store.RoleViewer); err != nil {
		return nil, err
	}
	r, err := s.Store.GetResult(ctx, req.Result)
	if err != nil {
		return nil, err
	}
	if r.RunID != req.Run {
		return nil, notFound("result")
	}
	return gen.GetResult200JSONResponse(toResult(r, true)), nil
}

func (s *Server) ListRunAggregates(ctx context.Context, req gen.ListRunAggregatesRequestObject) (gen.ListRunAggregatesResponseObject, error) {
	if _, err := s.run(ctx, req.Run, store.RoleViewer); err != nil {
		return nil, err
	}
	aggs, err := s.Store.ListRunAggregates(ctx, req.Run)
	if err != nil {
		return nil, err
	}
	return gen.ListRunAggregates200JSONResponse{Items: mapSlice(aggs, toAggregate)}, nil
}

// Execution endpoints (create, cancel, re-run failed, re-score, live events) live in handlers_engine.go.
