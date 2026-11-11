package api

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/airval/airval/internal/api/gen"
	"github.com/airval/airval/internal/auth"
	"github.com/airval/airval/internal/store"
	"github.com/airval/airval/internal/targets"
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

// ---- targets ----

func validTargetKind(k string) bool {
	for _, x := range targets.Kinds() {
		if x == k {
			return true
		}
	}
	return false
}

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
	if !validTargetKind(string(b.Kind)) {
		return nil, invalid("unknown target kind %q", b.Kind)
	}
	cfg, err := jsonObject(b.Config, "config")
	if err != nil {
		return nil, err
	}
	if err := validateTargetConfig(string(b.Kind), cfg); err != nil {
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
		if err := validateTargetConfig(t.Kind, cfg); err != nil {
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
	t, err := s.target(ctx, req.Target, store.RoleEditor)
	if err != nil {
		return nil, err
	}
	if t.CurrentVersion == nil {
		return nil, invalid("target has no configuration version")
	}
	b := req.Body
	if b == nil {
		b = &gen.TargetTestRequest{}
	}
	res, err := s.runTargetTest(ctx, t.ProjectID, t.Kind, t.CurrentVersion.Config, b.Example, b.DatasetId)
	if err != nil {
		return nil, err
	}
	return gen.TestTarget200JSONResponse(res), nil
}

func (s *Server) TestTargetDraft(ctx context.Context, req gen.TestTargetDraftRequestObject) (gen.TestTargetDraftResponseObject, error) {
	if _, err := s.project(ctx, req.Project, store.RoleEditor); err != nil {
		return nil, err
	}
	b := req.Body
	if b == nil {
		return nil, badRequest("request body is required")
	}
	if !validTargetKind(string(b.Kind)) {
		return nil, fieldError("kind", "unknown target kind %q", b.Kind)
	}
	cfg, err := jsonObject(b.Config, "config")
	if err != nil {
		return nil, err
	}
	res, err := s.runTargetTest(ctx, req.Project, string(b.Kind), cfg, b.Example, b.DatasetId)
	if err != nil {
		return nil, err
	}
	return gen.TestTargetDraft200JSONResponse(res), nil
}

// runTargetTest builds an adapter (secrets resolved server-side), sends one sample example and
// reports the redacted request, raw body, mapped response and per-path mapping matches.
func (s *Server) runTargetTest(ctx context.Context, projectID uuid.UUID, kind string, cfg json.RawMessage,
	ex *gen.ExampleData, datasetID *uuid.UUID) (gen.TargetTestResult, error) {
	proj, err := s.Store.GetProject(ctx, projectID)
	if err != nil {
		return gen.TargetTestResult{}, err
	}
	sample, err := s.sampleExample(ctx, projectID, ex, datasetID)
	if err != nil {
		return gen.TargetTestResult{}, err
	}
	// A config that fails validation is a client error, not a failed connection.
	if err := validateTargetConfig(kind, cfg); err != nil {
		return gen.TargetTestResult{}, err
	}
	ad, err := targets.New(kind, cfg, s.targetDeps())
	if err != nil {
		return gen.TargetTestResult{}, fieldError("config", "%v", err).with("kind", kind)
	}
	tctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	rep := targets.TestConnection(tctx, ad, sample, targets.InvokeOptions{Secrets: s.secretResolver(ctx, proj)})
	out := gen.TargetTestResult{Ok: rep.OK, LatencyMs: durMS(rep.Elapsed), Request: rawPtr(rep.Request),
		RawResponse: rawPtr(rep.Raw)}
	if rep.Error != "" {
		out.Error = ptr(rep.Error)
	}
	if rep.ErrorClass != "" {
		out.ErrorClass = ptr(rep.ErrorClass)
	}
	if rep.RetryAfter > 0 {
		out.RetryAfterMs = ptr(rep.RetryAfter.Milliseconds())
	}
	resp := toTargetResponse(rep.Response)
	out.Response = &resp
	if len(rep.Mapping) > 0 {
		m := make([]struct {
			Field   string         `json:"field"`
			Matched bool           `json:"matched"`
			Path    string         `json:"path"`
			Value   *gen.JSONValue `json:"value,omitempty"`
		}, len(rep.Mapping))
		for i, r := range rep.Mapping {
			m[i].Field, m[i].Path, m[i].Matched = r.Field, r.Path, r.Matched
			if r.Value != nil {
				m[i].Value = rawPtr(mustJSON(r.Value))
			}
		}
		out.Mapping = &m
	}
	return out, nil
}
