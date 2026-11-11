package api

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/prat31/airval/internal/api/gen"
	"github.com/prat31/airval/internal/evalcore"
	"github.com/prat31/airval/internal/evaluators"
	"github.com/prat31/airval/internal/store"
)

func (s *Server) evaluator(ctx context.Context, id uuid.UUID, min store.Role) (store.Evaluator, error) {
	e, err := s.Store.GetEvaluator(ctx, id)
	if err != nil {
		return e, err
	}
	return e, s.require(ctx, e.OrgID, &e.ProjectID, min)
}

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
	if err := validEvaluatorKind(string(b.Kind)); err != nil {
		return nil, err
	}
	cfg := json.RawMessage(`{}`)
	if b.Config != nil {
		if cfg, err = jsonObject(*b.Config, "config"); err != nil {
			return nil, err
		}
	}
	if err := validateEvaluatorConfig(string(b.Kind), cfg); err != nil {
		return nil, err
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
			if err := validEvaluatorKind(string(*b.Kind)); err != nil {
				return nil, err
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
			if err := validateEvaluatorConfig(e.Kind, e.Config); err != nil {
				return nil, err
			}
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

func (s *Server) GetEvaluatorCatalog(ctx context.Context, _ gen.GetEvaluatorCatalogRequestObject) (gen.GetEvaluatorCatalogResponseObject, error) {
	cat := evaluators.Catalog()
	items := make([]gen.EvaluatorKindInfo, 0, len(cat))
	for _, k := range cat {
		info := gen.EvaluatorKindInfo{Kind: k.Kind, Name: k.Name, Category: gen.EvaluatorKindInfoCategory(k.Category), Group: k.Group,
			Description: k.Description, Cost: k.Cost.String(), DefaultThreshold: k.DefaultThreshold,
			ConfigSchema: rawOr(k.ConfigSchema, "{}"), ApplyTo: mapSlice(k.ApplyTo, func(a evalcore.ApplyTo) gen.ApplyTo { return gen.ApplyTo(a) })}
		if len(k.Requires) > 0 {
			info.Requires = &k.Requires
		}
		if k.PromptVersion != "" {
			info.PromptVersion = ptr(k.PromptVersion)
		}
		if k.Prompt != "" {
			info.Prompt = ptr(k.Prompt)
		}
		items = append(items, info)
	}
	return gen.GetEvaluatorCatalog200JSONResponse{Items: items}, nil
}

func (s *Server) GetEvaluatorAgreement(ctx context.Context, req gen.GetEvaluatorAgreementRequestObject) (gen.GetEvaluatorAgreementResponseObject, error) {
	e, err := s.evaluator(ctx, req.Evaluator, store.RoleViewer)
	if err != nil {
		return nil, err
	}
	pairs, err := s.Store.AgreementPairs(ctx, e.ID, 0)
	if err != nil {
		return nil, err
	}
	return gen.GetEvaluatorAgreement200JSONResponse(computeAgreement(e.ID, pairs)), nil
}

// PreviewEvaluator scores one pasted response with an unsaved evaluator definition (the editor's
// live preview). Judges use the project's secrets and the LLM providers of internal/targets.
func (s *Server) PreviewEvaluator(ctx context.Context, req gen.PreviewEvaluatorRequestObject) (gen.PreviewEvaluatorResponseObject, error) {
	b := req.Body
	if b == nil {
		return nil, badRequest("request body is required")
	}
	var proj *store.Project
	if b.ProjectId != nil {
		p, err := s.project(ctx, *b.ProjectId, store.RoleEditor)
		if err != nil {
			return nil, err
		}
		proj = &p
	} else if s.principal(ctx) == nil {
		return nil, errorf(401, "unauthorized", "authentication required")
	}
	if err := validEvaluatorKind(string(b.Kind)); err != nil {
		return nil, err
	}
	cfg := json.RawMessage(`{}`)
	if b.Config != nil {
		var err error
		if cfg, err = jsonObject(*b.Config, "config"); err != nil {
			return nil, err
		}
	}
	if err := validateEvaluatorConfig(string(b.Kind), cfg); err != nil {
		return nil, err
	}
	ex := evalcore.Example{ID: "preview", Input: map[string]any{}}
	if b.Example != nil {
		var err error
		if ex, err = exampleDataToEval(*b.Example); err != nil {
			return nil, fieldError("example", "%v", err)
		}
	}
	resp, err := fromTargetResponse(b.Response)
	if err != nil {
		return nil, fieldError("response", "%v", err)
	}
	bound, err := evaluators.Build(evaluators.Spec{ID: string(b.Kind), Kind: string(b.Kind), Config: cfg}, s.evaluatorDeps(ctx, proj))
	if err != nil {
		return nil, fieldError("config", "%v", err)
	}
	ectx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	sc := bound.Evaluate(ectx, evaluators.Input{Example: &ex, Response: &resp})
	if sc.Label == "error" {
		msg, _ := sc.Meta[evaluators.MetaError].(string)
		if msg == "" {
			msg = sc.Reason
		}
		return nil, errorf(502, "evaluator_failed", "evaluator failed: %s", msg)
	}
	out := gen.Score{EvaluatorId: uuid.Nil, Value: sc.Value, Label: sc.Label, Passed: sc.Passed, Reason: sc.Reason,
		LatencyMs: ptr(durMS(sc.Latency)), CostUsd: ptr(sc.CostUSD)}
	if len(sc.Meta) > 0 {
		out.Meta = rawPtr(mustJSON(sc.Meta))
	}
	return gen.PreviewEvaluator200JSONResponse(out), nil
}
