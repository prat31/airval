//go:build ee

// Package retention implements per-project data retention: finished runs (with their results and scores)
// older than N days are deleted hourly by the cluster leader; runs pinned as a suite baseline are kept
// unless the policy says otherwise.
package retention

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/prat31/airval/ee/internal/httpx"
	"github.com/prat31/airval/internal/auth"
	"github.com/prat31/airval/internal/rbac"
	"github.com/prat31/airval/internal/store"
)

// LeaseName is the cluster lease held by the replica that applies retention.
const LeaseName = "ee.retention"

const batch = 500

// Service applies and administers retention policies.
type Service struct {
	h   httpx.H
	Now func() time.Time
}

// New returns the retention service.
func New(h httpx.H) *Service {
	return &Service{h: h, Now: func() time.Time { return time.Now().UTC() }}
}

// Routes registers policy endpoints.
func (s *Service) Routes(r chi.Router) {
	a := r.With(s.h.Host.Authenticate)
	a.Get("/v1/orgs/{org}/retention-policies", s.h.Wrap("listRetentionPolicies", s.list))
	a.Get("/v1/projects/{project}/retention-policy", s.h.Wrap("getRetentionPolicy", s.get))
	a.Put("/v1/projects/{project}/retention-policy", s.h.Wrap("putRetentionPolicy", s.put))
	a.Delete("/v1/projects/{project}/retention-policy", s.h.Wrap("deleteRetentionPolicy", s.remove))
}

type policyJSON struct {
	ProjectID     uuid.UUID  `json:"project_id"`
	Enabled       bool       `json:"enabled"`
	RunDays       int        `json:"run_days"`
	KeepBaselines bool       `json:"keep_baselines"`
	LastAppliedAt *time.Time `json:"last_applied_at"`
	LastDeleted   int        `json:"last_deleted"`
	TotalDeleted  int64      `json:"total_deleted"`
	EligibleRuns  int        `json:"eligible_runs"`
}

func (s *Service) toJSON(ctx context.Context, p store.RetentionPolicy) (policyJSON, error) {
	out := policyJSON{ProjectID: p.ProjectID, Enabled: p.Enabled, RunDays: p.RunDays, KeepBaselines: p.KeepBaselines,
		LastAppliedAt: p.LastAppliedAt, LastDeleted: p.LastDeleted, TotalDeleted: p.TotalDeleted}
	if p.RunDays > 0 {
		ids, err := s.h.Host.Store().ListExpiredRuns(ctx, p.ProjectID, s.cutoff(p), p.KeepBaselines, 1000)
		if err != nil {
			return out, err
		}
		out.EligibleRuns = len(ids)
	}
	return out, nil
}

func (s *Service) cutoff(p store.RetentionPolicy) time.Time { return s.Now().AddDate(0, 0, -p.RunDays) }

// project loads the {project} parameter and checks the caller's role in it.
func (s *Service) project(r *http.Request, min store.Role) (store.Project, *auth.Principal, error) {
	id, err := httpx.UUIDParam(r, "project")
	if err != nil {
		return store.Project{}, nil, err
	}
	pr, err := s.h.Host.Store().GetProject(r.Context(), id)
	if err != nil {
		return pr, nil, err
	}
	p := auth.FromContext(r.Context())
	if p == nil {
		return pr, nil, rbac.ErrUnauthenticated
	}
	return pr, p, s.h.Host.Authz().Require(r.Context(), p, pr.OrgID, &pr.ID, min)
}

func (s *Service) list(w http.ResponseWriter, r *http.Request) error {
	orgID, _, err := s.h.RequireOrg(r, store.RoleAdmin)
	if err != nil {
		return err
	}
	ps, err := s.h.Host.Store().ListRetentionPolicies(r.Context(), orgID)
	if err != nil {
		return err
	}
	items := make([]policyJSON, 0, len(ps))
	for _, p := range ps {
		j, err := s.toJSON(r.Context(), p)
		if err != nil {
			return err
		}
		items = append(items, j)
	}
	return httpx.JSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Service) get(w http.ResponseWriter, r *http.Request) error {
	pr, _, err := s.project(r, store.RoleViewer)
	if err != nil {
		return err
	}
	p, err := s.h.Host.Store().GetRetentionPolicy(r.Context(), pr.ID)
	if errors.Is(err, store.ErrNotFound) {
		// No policy: runs are kept forever. Report a disabled default so clients can render a form.
		p = store.RetentionPolicy{ProjectID: pr.ID, OrgID: pr.OrgID, RunDays: 90, KeepBaselines: true}
	} else if err != nil {
		return err
	}
	j, err := s.toJSON(r.Context(), p)
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusOK, j)
}

func (s *Service) put(w http.ResponseWriter, r *http.Request) error {
	pr, p, err := s.project(r, store.RoleAdmin)
	if err != nil {
		return err
	}
	if err := httpx.RequireUser(p); err != nil {
		return err
	}
	var in struct {
		Enabled       *bool `json:"enabled"`
		RunDays       int   `json:"run_days"`
		KeepBaselines *bool `json:"keep_baselines"`
	}
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	if in.RunDays < 1 || in.RunDays > 3650 {
		return httpx.Invalid("run_days must be between 1 and 3650")
	}
	pol := store.RetentionPolicy{ProjectID: pr.ID, OrgID: pr.OrgID, Enabled: in.Enabled == nil || *in.Enabled,
		RunDays: in.RunDays, KeepBaselines: in.KeepBaselines == nil || *in.KeepBaselines}
	if err := s.h.Host.Store().UpsertRetentionPolicy(r.Context(), &pol); err != nil {
		return err
	}
	saved, err := s.h.Host.Store().GetRetentionPolicy(r.Context(), pr.ID)
	if err != nil {
		return err
	}
	s.h.Audit(r, pr.OrgID, "retention_policy.update", "project:"+pr.ID.String(),
		map[string]any{"enabled": pol.Enabled, "run_days": pol.RunDays, "keep_baselines": pol.KeepBaselines})
	j, err := s.toJSON(r.Context(), saved)
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusOK, j)
}

func (s *Service) remove(w http.ResponseWriter, r *http.Request) error {
	pr, p, err := s.project(r, store.RoleAdmin)
	if err != nil {
		return err
	}
	if err := httpx.RequireUser(p); err != nil {
		return err
	}
	if err := s.h.Host.Store().DeleteRetentionPolicy(r.Context(), pr.ID); err != nil {
		return err
	}
	s.h.Audit(r, pr.OrgID, "retention_policy.delete", "project:"+pr.ID.String(), nil)
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// Apply runs every enabled policy once. It deletes in batches and returns the number of runs deleted.
func (s *Service) Apply(ctx context.Context) (int, error) {
	st := s.h.Host.Store()
	ps, err := st.ListRetentionPolicies(ctx, uuid.Nil)
	if err != nil {
		return 0, err
	}
	total := 0
	for _, p := range ps {
		if !p.Enabled || p.RunDays <= 0 {
			continue
		}
		deleted := 0
		cutoff := s.cutoff(p)
		for ctx.Err() == nil {
			ids, err := st.ListExpiredRuns(ctx, p.ProjectID, cutoff, p.KeepBaselines, batch)
			if err != nil {
				return total, err
			}
			if len(ids) == 0 {
				break
			}
			n, err := st.DeleteRuns(ctx, ids)
			if err != nil {
				return total, err
			}
			deleted += n
			if len(ids) < batch {
				break
			}
		}
		if err := st.RecordRetentionApplied(ctx, p.ProjectID, s.Now(), deleted); err != nil {
			return total, err
		}
		if deleted > 0 {
			e := store.AuditEntry{OrgID: p.OrgID, Actor: "system:retention", Action: "retention.apply",
				Target: "project:" + p.ProjectID.String()}
			e.Diff = []byte(`{"deleted_runs":` + strconv.Itoa(deleted) + `}`)
			_ = st.AppendAudit(ctx, &e)
		}
		total += deleted
	}
	return total, nil
}
