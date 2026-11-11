package api

import (
	"context"
	"strings"

	"github.com/airval/airval/internal/api/gen"
	"github.com/airval/airval/internal/store"
)

func (s *Server) ListComparisons(ctx context.Context, req gen.ListComparisonsRequestObject) (gen.ListComparisonsResponseObject, error) {
	if _, err := s.project(ctx, req.Project, store.RoleViewer); err != nil {
		return nil, err
	}
	pg, err := s.Store.ListComparisons(ctx, req.Project, listParams(req.Params.Cursor, req.Params.Limit))
	if err != nil {
		return nil, err
	}
	return gen.ListComparisons200JSONResponse{Items: mapSlice(pg.Items, toComparison), NextCursor: nextCursor(pg.NextCursor)}, nil
}

func (s *Server) CreateComparison(ctx context.Context, req gen.CreateComparisonRequestObject) (gen.CreateComparisonResponseObject, error) {
	p, err := s.project(ctx, req.Project, store.RoleEditor)
	if err != nil {
		return nil, err
	}
	b := req.Body
	if b == nil || len(b.RunIds) < 2 || len(b.RunIds) > 4 {
		return nil, invalid("run_ids must contain 2 to 4 runs")
	}
	for _, id := range b.RunIds {
		r, err := s.Store.GetRun(ctx, id)
		if err != nil || r.ProjectID != p.ID {
			return nil, invalid("run %s is not in this project", id)
		}
	}
	c := store.Comparison{OrgID: p.OrgID, ProjectID: p.ID, Name: strings.TrimSpace(deref(b.Name, "")), RunIDs: b.RunIds,
		CreatedBy: s.principal(ctx).UserID()}
	if err := s.Store.CreateComparison(ctx, &c); err != nil {
		return nil, err
	}
	return gen.CreateComparison201JSONResponse(toComparison(c)), nil
}

func (s *Server) GetComparison(ctx context.Context, req gen.GetComparisonRequestObject) (gen.GetComparisonResponseObject, error) {
	c, err := s.Store.GetComparison(ctx, req.Comparison)
	if err != nil {
		return nil, err
	}
	if err := s.require(ctx, c.OrgID, &c.ProjectID, store.RoleViewer); err != nil {
		return nil, err
	}
	return gen.GetComparison200JSONResponse(toComparison(c)), nil
}

func (s *Server) DeleteComparison(ctx context.Context, req gen.DeleteComparisonRequestObject) (gen.DeleteComparisonResponseObject, error) {
	c, err := s.Store.GetComparison(ctx, req.Comparison)
	if err != nil {
		return nil, err
	}
	if err := s.require(ctx, c.OrgID, &c.ProjectID, store.RoleEditor); err != nil {
		return nil, err
	}
	if err := s.Store.DeleteComparison(ctx, c.ID); err != nil {
		return nil, err
	}
	return gen.DeleteComparison204Response{}, nil
}
