package api

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/google/uuid"

	"github.com/airval/airval/internal/api/gen"
	"github.com/airval/airval/internal/auth"
	"github.com/airval/airval/internal/store"
)

// ---- projects ----

func (s *Server) ListProjects(ctx context.Context, req gen.ListProjectsRequestObject) (gen.ListProjectsResponseObject, error) {
	f := store.ProjectFilter{Slug: deref(req.Params.Slug, "")}
	orgIDs, onlyProject, err := s.Authz.OrgIDs(ctx, s.principal(ctx))
	if err != nil {
		return nil, err
	}
	if req.Params.OrgId != nil {
		if err := s.require(ctx, *req.Params.OrgId, onlyProject, store.RoleViewer); err != nil {
			return nil, err
		}
		orgIDs = orgIDs[:0:0]
		orgIDs = append(orgIDs, *req.Params.OrgId)
	}
	f.OrgIDs = orgIDs
	if onlyProject != nil {
		f.IDs = append(f.IDs, *onlyProject)
	}
	pg, err := s.Store.ListProjects(ctx, f, listParams(req.Params.Cursor, req.Params.Limit))
	if err != nil {
		return nil, err
	}
	return gen.ListProjects200JSONResponse{Items: mapSlice(pg.Items, toProject), NextCursor: nextCursor(pg.NextCursor)}, nil
}

func settingsJSON(st *gen.ProjectSettings, cur json.RawMessage) json.RawMessage {
	if st == nil {
		return rawOr(cur, "{}")
	}
	return mustJSON(st)
}

func (s *Server) CreateProject(ctx context.Context, req gen.CreateProjectRequestObject) (gen.CreateProjectResponseObject, error) {
	b := req.Body
	if b == nil || strings.TrimSpace(b.Name) == "" {
		return nil, invalid("org_id and name are required")
	}
	if err := s.require(ctx, b.OrgId, nil, store.RoleEditor); err != nil {
		return nil, err
	}
	p := store.Project{OrgID: b.OrgId, Name: strings.TrimSpace(b.Name), Slug: trimmed(b.Slug), Description: trimmed(b.Description),
		Color: trimmed(b.Color), Icon: trimmed(b.Icon), Settings: settingsJSON(b.Settings, nil)}
	if p.Slug == "" {
		p.Slug = auth.Slugify(p.Name)
	}
	if err := s.Store.CreateProject(ctx, &p); err != nil {
		return nil, err
	}
	return gen.CreateProject201JSONResponse(toProject(p)), nil
}

func (s *Server) GetProject(ctx context.Context, req gen.GetProjectRequestObject) (gen.GetProjectResponseObject, error) {
	p, err := s.project(ctx, req.Project, store.RoleViewer)
	if err != nil {
		return nil, err
	}
	return gen.GetProject200JSONResponse(toProject(p)), nil
}

func (s *Server) UpdateProject(ctx context.Context, req gen.UpdateProjectRequestObject) (gen.UpdateProjectResponseObject, error) {
	p, err := s.project(ctx, req.Project, store.RoleEditor)
	if err != nil {
		return nil, err
	}
	if b := req.Body; b != nil {
		if b.Name != nil && strings.TrimSpace(*b.Name) != "" {
			p.Name = strings.TrimSpace(*b.Name)
		}
		if b.Slug != nil && *b.Slug != "" {
			p.Slug = *b.Slug
		}
		if b.Description != nil {
			p.Description = strings.TrimSpace(*b.Description)
		}
		if b.Color != nil {
			p.Color = *b.Color
		}
		if b.Icon != nil {
			p.Icon = *b.Icon
		}
		p.Settings = settingsJSON(b.Settings, p.Settings)
	}
	if err := s.Store.UpdateProject(ctx, &p); err != nil {
		return nil, err
	}
	return gen.UpdateProject200JSONResponse(toProject(p)), nil
}

func (s *Server) DeleteProject(ctx context.Context, req gen.DeleteProjectRequestObject) (gen.DeleteProjectResponseObject, error) {
	p, err := s.project(ctx, req.Project, store.RoleAdmin)
	if err != nil {
		return nil, err
	}
	if err := s.Store.DeleteProject(ctx, p.ID); err != nil {
		return nil, err
	}
	s.audit(ctx, p.OrgID, "project.delete", "project:"+p.ID.String(), map[string]string{"name": p.Name})
	return gen.DeleteProject204Response{}, nil
}

func (s *Server) GetTrends(ctx context.Context, req gen.GetTrendsRequestObject) (gen.GetTrendsResponseObject, error) {
	if _, err := s.project(ctx, req.Project, store.RoleViewer); err != nil {
		return nil, err
	}
	// TODO(wave): build series from ListRuns + ListRunAggregatesForRuns (milestone 7).
	return nil, notImplemented("trends")
}

// ---- targets ----

var targetKinds = map[string]bool{"http": true, "openai": true, "anthropic": true, "bedrock": true, "vertex": true, "agent": true}

func (s *Server) target(ctx context.Context, id uuid.UUID, min store.Role) (store.Target, error) {
	t, err := s.Store.GetTarget(ctx, id)
	if err != nil {
		return t, err
	}
	return t, s.require(ctx, t.OrgID, &t.ProjectID, min)
}

func (s *Server) ListTargets(ctx context.Context, req gen.ListTargetsRequestObject) (gen.ListTargetsResponseObject, error) {
	if _, err := s.project(ctx, req.Project, store.RoleViewer); err != nil {
		return nil, err
	}
	pg, err := s.Store.ListTargets(ctx, req.Project, listParams(req.Params.Cursor, req.Params.Limit))
	if err != nil {
		return nil, err
	}
	return gen.ListTargets200JSONResponse{Items: mapSlice(pg.Items, toTarget), NextCursor: nextCursor(pg.NextCursor)}, nil
}

func (s *Server) CreateTarget(ctx context.Context, req gen.CreateTargetRequestObject) (gen.CreateTargetResponseObject, error) {
	p, err := s.project(ctx, req.Project, store.RoleEditor)
	if err != nil {
		return nil, err
	}
	b := req.Body
	if b == nil || strings.TrimSpace(b.Name) == "" {
		return nil, invalid("name is required")
	}
	if !targetKinds[string(b.Kind)] {
		return nil, invalid("unknown target kind %q", b.Kind)
	}
	cfg, err := jsonObject(b.Config, "config")
	if err != nil {
		return nil, err
	}
	t := store.Target{OrgID: p.OrgID, ProjectID: p.ID, Name: strings.TrimSpace(b.Name), Description: trimmed(b.Description),
		Kind: string(b.Kind)}
	if err := s.Store.CreateTarget(ctx, &t, cfg, s.principal(ctx).UserID()); err != nil {
		return nil, err
	}
	return gen.CreateTarget201JSONResponse(toTarget(t)), nil
}

func (s *Server) GetTarget(ctx context.Context, req gen.GetTargetRequestObject) (gen.GetTargetResponseObject, error) {
	t, err := s.target(ctx, req.Target, store.RoleViewer)
	if err != nil {
		return nil, err
	}
	return gen.GetTarget200JSONResponse(toTarget(t)), nil
}

func (s *Server) UpdateTarget(ctx context.Context, req gen.UpdateTargetRequestObject) (gen.UpdateTargetResponseObject, error) {
	t, err := s.target(ctx, req.Target, store.RoleEditor)
	if err != nil {
		return nil, err
	}
	b := req.Body
	if b == nil {
		return gen.UpdateTarget200JSONResponse(toTarget(t)), nil
	}
	if b.Name != nil || b.Description != nil {
		if b.Name != nil && strings.TrimSpace(*b.Name) != "" {
			t.Name = strings.TrimSpace(*b.Name)
		}
		if b.Description != nil {
			t.Description = strings.TrimSpace(*b.Description)
		}
		if err := s.Store.UpdateTarget(ctx, &t); err != nil {
			return nil, err
		}
	}
	if b.Config != nil {
		cfg, err := jsonObject(*b.Config, "config")
		if err != nil {
			return nil, err
		}
		// New version only when the config actually changed.
		if t.CurrentVersion == nil || string(canonicalJSON(t.CurrentVersion.Config)) != string(canonicalJSON(cfg)) {
			if _, err := s.Store.AddTargetVersion(ctx, t.ID, cfg, s.principal(ctx).UserID()); err != nil {
				return nil, err
			}
		}
	}
	t, err = s.Store.GetTarget(ctx, t.ID)
	if err != nil {
		return nil, err
	}
	return gen.UpdateTarget200JSONResponse(toTarget(t)), nil
}

func canonicalJSON(raw json.RawMessage) []byte {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return raw
	}
	return mustJSON(v)
}

func (s *Server) DeleteTarget(ctx context.Context, req gen.DeleteTargetRequestObject) (gen.DeleteTargetResponseObject, error) {
	t, err := s.target(ctx, req.Target, store.RoleEditor)
	if err != nil {
		return nil, err
	}
	if err := s.Store.DeleteTarget(ctx, t.ID); err != nil {
		return nil, err
	}
	return gen.DeleteTarget204Response{}, nil
}

func (s *Server) ListTargetVersions(ctx context.Context, req gen.ListTargetVersionsRequestObject) (gen.ListTargetVersionsResponseObject, error) {
	if _, err := s.target(ctx, req.Target, store.RoleViewer); err != nil {
		return nil, err
	}
	pg, err := s.Store.ListTargetVersions(ctx, req.Target, listParams(req.Params.Cursor, req.Params.Limit))
	if err != nil {
		return nil, err
	}
	return gen.ListTargetVersions200JSONResponse{Items: mapSlice(pg.Items, toTargetVersion), NextCursor: nextCursor(pg.NextCursor)}, nil
}

func (s *Server) GetTargetVersion(ctx context.Context, req gen.GetTargetVersionRequestObject) (gen.GetTargetVersionResponseObject, error) {
	if _, err := s.target(ctx, req.Target, store.RoleViewer); err != nil {
		return nil, err
	}
	v, err := s.Store.GetTargetVersion(ctx, req.Version)
	if err != nil {
		return nil, err
	}
	if v.TargetID != req.Target {
		return nil, notFound("target version")
	}
	return gen.GetTargetVersion200JSONResponse(toTargetVersion(v)), nil
}

func (s *Server) TestTarget(ctx context.Context, req gen.TestTargetRequestObject) (gen.TestTargetResponseObject, error) {
	if _, err := s.target(ctx, req.Target, store.RoleEditor); err != nil {
		return nil, err
	}
	// TODO(wave): run one example through internal/targets with the current version (milestone 3).
	return nil, notImplemented("target test connection")
}

func (s *Server) TestTargetDraft(ctx context.Context, req gen.TestTargetDraftRequestObject) (gen.TestTargetDraftResponseObject, error) {
	if _, err := s.project(ctx, req.Project, store.RoleEditor); err != nil {
		return nil, err
	}
	// TODO(wave): same as TestTarget, for an unsaved config (milestone 3).
	return nil, notImplemented("target test connection")
}
