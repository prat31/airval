package api

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/google/uuid"

	"github.com/airval/airval/internal/api/gen"
	"github.com/airval/airval/internal/store"
)

func (s *Server) evaluator(ctx context.Context, id uuid.UUID, min store.Role) (store.Evaluator, error) {
	e, err := s.Store.GetEvaluator(ctx, id)
	if err != nil {
		return e, err
	}
	return e, s.require(ctx, e.OrgID, &e.ProjectID, min)
}

func validEvaluatorKind(k gen.EvaluatorKind) bool { return k.Valid() }

func (s *Server) ListEvaluators(ctx context.Context, req gen.ListEvaluatorsRequestObject) (gen.ListEvaluatorsResponseObject, error) {
	if _, err := s.project(ctx, req.Project, store.RoleViewer); err != nil {
		return nil, err
	}
	pg, err := s.Store.ListEvaluators(ctx, req.Project, listParams(req.Params.Cursor, req.Params.Limit))
	if err != nil {
		return nil, err
	}
	return gen.ListEvaluators200JSONResponse{Items: mapSlice(pg.Items, toEvaluator), NextCursor: nextCursor(pg.NextCursor)}, nil
}

func (s *Server) CreateEvaluator(ctx context.Context, req gen.CreateEvaluatorRequestObject) (gen.CreateEvaluatorResponseObject, error) {
	p, err := s.project(ctx, req.Project, store.RoleEditor)
	if err != nil {
		return nil, err
	}
	b := req.Body
	if b == nil || strings.TrimSpace(b.Name) == "" {
		return nil, invalid("name is required")
	}
	if !validEvaluatorKind(b.Kind) {
		return nil, invalid("unknown evaluator kind %q", b.Kind)
	}
	cfg := json.RawMessage(`{}`)
	if b.Config != nil {
		if cfg, err = jsonObject(*b.Config, "config"); err != nil {
			return nil, err
		}
	}
	e := store.Evaluator{OrgID: p.OrgID, ProjectID: p.ID, Name: strings.TrimSpace(b.Name), Description: trimmed(b.Description),
		Kind: string(b.Kind), Config: cfg}
	if err := s.Store.CreateEvaluator(ctx, &e); err != nil {
		return nil, err
	}
	return gen.CreateEvaluator201JSONResponse(toEvaluator(e)), nil
}

func (s *Server) GetEvaluator(ctx context.Context, req gen.GetEvaluatorRequestObject) (gen.GetEvaluatorResponseObject, error) {
	e, err := s.evaluator(ctx, req.Evaluator, store.RoleViewer)
	if err != nil {
		return nil, err
	}
	return gen.GetEvaluator200JSONResponse(toEvaluator(e)), nil
}

func (s *Server) UpdateEvaluator(ctx context.Context, req gen.UpdateEvaluatorRequestObject) (gen.UpdateEvaluatorResponseObject, error) {
	e, err := s.evaluator(ctx, req.Evaluator, store.RoleEditor)
	if err != nil {
		return nil, err
	}
	if b := req.Body; b != nil {
		changed := false
		if b.Name != nil && strings.TrimSpace(*b.Name) != "" {
			e.Name = strings.TrimSpace(*b.Name)
		}
		if b.Description != nil {
			e.Description = strings.TrimSpace(*b.Description)
		}
		if b.Kind != nil && string(*b.Kind) != e.Kind {
			if !validEvaluatorKind(*b.Kind) {
				return nil, invalid("unknown evaluator kind %q", *b.Kind)
			}
			e.Kind, changed = string(*b.Kind), true
		}
		if b.Config != nil {
			cfg, err := jsonObject(*b.Config, "config")
			if err != nil {
				return nil, err
			}
			if string(canonicalJSON(cfg)) != string(canonicalJSON(e.Config)) {
				e.Config, changed = cfg, true
			}
		}
		if changed {
			e.Version++
		}
	}
	if err := s.Store.UpdateEvaluator(ctx, &e); err != nil {
		return nil, err
	}
	return gen.UpdateEvaluator200JSONResponse(toEvaluator(e)), nil
}

func (s *Server) DeleteEvaluator(ctx context.Context, req gen.DeleteEvaluatorRequestObject) (gen.DeleteEvaluatorResponseObject, error) {
	e, err := s.evaluator(ctx, req.Evaluator, store.RoleEditor)
	if err != nil {
		return nil, err
	}
	if err := s.Store.DeleteEvaluator(ctx, e.ID); err != nil {
		return nil, err
	}
	return gen.DeleteEvaluator204Response{}, nil
}

func (s *Server) GetEvaluatorAgreement(ctx context.Context, req gen.GetEvaluatorAgreementRequestObject) (gen.GetEvaluatorAgreementResponseObject, error) {
	if _, err := s.evaluator(ctx, req.Evaluator, store.RoleViewer); err != nil {
		return nil, err
	}
	// TODO(wave): pair scores with annotations and compute Cohen's kappa (milestone 10).
	return nil, notImplemented("evaluator agreement")
}

func (s *Server) PreviewEvaluator(ctx context.Context, req gen.PreviewEvaluatorRequestObject) (gen.PreviewEvaluatorResponseObject, error) {
	if req.Body != nil && req.Body.ProjectId != nil {
		if _, err := s.project(ctx, *req.Body.ProjectId, store.RoleEditor); err != nil {
			return nil, err
		}
	}
	// TODO(wave): score via internal/evaluators registry (milestone 5).
	return nil, notImplemented("evaluator preview")
}
