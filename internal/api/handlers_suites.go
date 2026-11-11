package api

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/robfig/cron/v3"

	"github.com/airval/airval/internal/api/gen"
	"github.com/airval/airval/internal/auth"
	"github.com/airval/airval/internal/store"
)

func (s *Server) suite(ctx context.Context, id uuid.UUID, min store.Role) (store.Suite, error) {
	x, err := s.Store.GetSuite(ctx, id)
	if err != nil {
		return x, err
	}
	return x, s.require(ctx, x.OrgID, &x.ProjectID, min)
}

// ---- suites ----

func (s *Server) withLastRun(ctx context.Context, x store.Suite) gen.Suite {
	out := toSuite(x)
	if r, err := s.Store.LatestRun(ctx, store.RunFilter{SuiteID: &x.ID}); err == nil {
		gr := toRun(r)
		out.LastRun = &gr
	}
	return out
}

func (s *Server) ListSuites(ctx context.Context, req gen.ListSuitesRequestObject) (gen.ListSuitesResponseObject, error) {
	if _, err := s.project(ctx, req.Project, store.RoleViewer); err != nil {
		return nil, err
	}
	pg, err := s.Store.ListSuites(ctx, req.Project, listParams(req.Params.Cursor, req.Params.Limit))
	if err != nil {
		return nil, err
	}
	return gen.ListSuites200JSONResponse{Items: mapSlice(pg.Items, toSuite), NextCursor: nextCursor(pg.NextCursor)}, nil
}

func (s *Server) CreateSuite(ctx context.Context, req gen.CreateSuiteRequestObject) (gen.CreateSuiteResponseObject, error) {
	p, err := s.project(ctx, req.Project, store.RoleEditor)
	if err != nil {
		return nil, err
	}
	b := req.Body
	if b == nil || strings.TrimSpace(b.Name) == "" {
		return nil, invalid("name is required")
	}
	cfg, err := jsonObject(b.Config, "config")
	if err != nil {
		return nil, err
	}
	if err := s.validateSuiteConfig(ctx, p.ID, cfg); err != nil {
		return nil, err
	}
	x := store.Suite{OrgID: p.OrgID, ProjectID: p.ID, Name: strings.TrimSpace(b.Name), Slug: trimmed(b.Slug),
		Description: trimmed(b.Description), Config: cfg}
	if x.Slug == "" {
		x.Slug = auth.Slugify(x.Name)
	}
	if err := s.Store.CreateSuite(ctx, &x); err != nil {
		return nil, err
	}
	return gen.CreateSuite201JSONResponse(toSuite(x)), nil
}

func (s *Server) GetSuite(ctx context.Context, req gen.GetSuiteRequestObject) (gen.GetSuiteResponseObject, error) {
	x, err := s.suite(ctx, req.Suite, store.RoleViewer)
	if err != nil {
		return nil, err
	}
	return gen.GetSuite200JSONResponse(s.withLastRun(ctx, x)), nil
}

func (s *Server) GetSuiteBySlug(ctx context.Context, req gen.GetSuiteBySlugRequestObject) (gen.GetSuiteBySlugResponseObject, error) {
	if _, err := s.project(ctx, req.Project, store.RoleViewer); err != nil {
		return nil, err
	}
	x, err := s.Store.GetSuiteBySlug(ctx, req.Project, req.Slug)
	if err != nil {
		return nil, err
	}
	return gen.GetSuiteBySlug200JSONResponse(s.withLastRun(ctx, x)), nil
}

func (s *Server) UpdateSuite(ctx context.Context, req gen.UpdateSuiteRequestObject) (gen.UpdateSuiteResponseObject, error) {
	x, err := s.suite(ctx, req.Suite, store.RoleEditor)
	if err != nil {
		return nil, err
	}
	if b := req.Body; b != nil {
		if b.Name != nil && strings.TrimSpace(*b.Name) != "" {
			x.Name = strings.TrimSpace(*b.Name)
		}
		if b.Slug != nil && *b.Slug != "" {
			x.Slug = *b.Slug
		}
		if b.Description != nil {
			x.Description = strings.TrimSpace(*b.Description)
		}
		if b.Config != nil {
			if x.Config, err = jsonObject(*b.Config, "config"); err != nil {
				return nil, err
			}
			if err := s.validateSuiteConfig(ctx, x.ProjectID, x.Config); err != nil {
				return nil, err
			}
		}
		if b.BaselineRunId != nil {
			r, err := s.Store.GetRun(ctx, *b.BaselineRunId)
			if err != nil || r.ProjectID != x.ProjectID {
				return nil, invalid("baseline_run_id must be a run in this project")
			}
			x.BaselineRunID = b.BaselineRunId
		}
	}
	if err := s.Store.UpdateSuite(ctx, &x); err != nil {
		return nil, err
	}
	return gen.UpdateSuite200JSONResponse(toSuite(x)), nil
}

func (s *Server) DeleteSuite(ctx context.Context, req gen.DeleteSuiteRequestObject) (gen.DeleteSuiteResponseObject, error) {
	x, err := s.suite(ctx, req.Suite, store.RoleEditor)
	if err != nil {
		return nil, err
	}
	if err := s.Store.DeleteSuite(ctx, x.ID); err != nil {
		return nil, err
	}
	return gen.DeleteSuite204Response{}, nil
}

func set(v ...string) map[string]bool {
	m := make(map[string]bool, len(v))
	for _, s := range v {
		m[s] = true
	}
	return m
}

// ---- gates (see handlers_gates.go) ----

func (s *Server) suiteGate(ctx context.Context, suiteID, gateID uuid.UUID, min store.Role) (store.Gate, error) {
	if _, err := s.suite(ctx, suiteID, min); err != nil {
		return store.Gate{}, err
	}
	g, err := s.Store.GetGate(ctx, gateID)
	if err == nil && g.SuiteID != suiteID {
		err = store.ErrNotFound
	}
	return g, err
}

func (s *Server) DeleteGate(ctx context.Context, req gen.DeleteGateRequestObject) (gen.DeleteGateResponseObject, error) {
	g, err := s.suiteGate(ctx, req.Suite, req.Gate, store.RoleEditor)
	if err != nil {
		return nil, err
	}
	if err := s.Store.DeleteGate(ctx, g.ID); err != nil {
		return nil, err
	}
	return gen.DeleteGate204Response{}, nil
}

// ---- triggers ----

var cronParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)

type triggerCfg struct {
	Cron     string `json:"cron"`
	Timezone string `json:"timezone"`
}

// NextFire computes the next fire time of a schedule trigger config after t.
func NextFire(cfg json.RawMessage, after time.Time) (*time.Time, error) {
	var c triggerCfg
	_ = json.Unmarshal(cfg, &c)
	if strings.TrimSpace(c.Cron) == "" {
		return nil, invalid("config.cron is required for schedule triggers")
	}
	loc := time.UTC
	if c.Timezone != "" {
		l, err := time.LoadLocation(c.Timezone)
		if err != nil {
			return nil, invalid("unknown timezone %q", c.Timezone)
		}
		loc = l
	}
	sched, err := cronParser.Parse(c.Cron)
	if err != nil {
		return nil, invalid("invalid cron expression: %v", err)
	}
	next := sched.Next(after.In(loc)).UTC()
	return &next, nil
}

func (s *Server) ListTriggers(ctx context.Context, req gen.ListTriggersRequestObject) (gen.ListTriggersResponseObject, error) {
	if _, err := s.suite(ctx, req.Suite, store.RoleViewer); err != nil {
		return nil, err
	}
	ts, err := s.Store.ListTriggers(ctx, req.Suite)
	if err != nil {
		return nil, err
	}
	return gen.ListTriggers200JSONResponse{Items: mapSlice(ts, s.toTrigger)}, nil
}

// prepareTrigger validates config and derives next_fire_at for schedules.
func prepareTrigger(t *store.Trigger) error {
	cfg, err := jsonObject(t.Config, "config")
	if err != nil {
		return err
	}
	t.Config = cfg
	t.NextFireAt = nil
	if t.Kind == "schedule" {
		next, err := NextFire(cfg, time.Now())
		if err != nil {
			return err
		}
		if t.Enabled {
			t.NextFireAt = next
		}
	}
	return nil
}

func (s *Server) newWebhookSecret(t *store.Trigger) (string, error) {
	secret := "whsec_" + auth.NewToken()
	blob, v, err := s.Keyring.Seal([]byte(secret), t.ID[:])
	if err != nil {
		return "", err
	}
	t.SecretEnc, t.SecretKeyVersion = blob, v
	return secret, nil
}

func (s *Server) triggerCreated(t store.Trigger, secret string) gen.TriggerCreated {
	g := s.toTrigger(t)
	out := gen.TriggerCreated{Id: g.Id, SuiteId: g.SuiteId, Name: g.Name, Kind: g.Kind, Config: g.Config, Enabled: g.Enabled,
		NextFireAt: g.NextFireAt, LastFiredAt: g.LastFiredAt, WebhookUrl: g.WebhookUrl, CreatedAt: g.CreatedAt, UpdatedAt: g.UpdatedAt}
	if secret != "" {
		out.WebhookSecret = &secret
	}
	return out
}

func (s *Server) CreateTrigger(ctx context.Context, req gen.CreateTriggerRequestObject) (gen.CreateTriggerResponseObject, error) {
	x, err := s.suite(ctx, req.Suite, store.RoleEditor)
	if err != nil {
		return nil, err
	}
	b := req.Body
	if b == nil || !b.Kind.Valid() {
		return nil, invalid("kind must be manual, schedule, webhook or api")
	}
	t := store.Trigger{ID: store.NewID(), OrgID: x.OrgID, ProjectID: x.ProjectID, SuiteID: x.ID, Name: trimmed(b.Name),
		Kind: string(b.Kind), Enabled: deref(b.Enabled, true)}
	if b.Config != nil {
		t.Config = *b.Config
	}
	if err := prepareTrigger(&t); err != nil {
		return nil, err
	}
	var secret string
	if t.Kind == "webhook" {
		if secret, err = s.newWebhookSecret(&t); err != nil {
			return nil, err
		}
	}
	if err := s.Store.CreateTrigger(ctx, &t); err != nil {
		return nil, err
	}
	return gen.CreateTrigger201JSONResponse(s.triggerCreated(t, secret)), nil
}

func (s *Server) suiteTrigger(ctx context.Context, suiteID, id uuid.UUID, min store.Role) (store.Trigger, error) {
	if _, err := s.suite(ctx, suiteID, min); err != nil {
		return store.Trigger{}, err
	}
	t, err := s.Store.GetTrigger(ctx, id)
	if err == nil && t.SuiteID != suiteID {
		err = store.ErrNotFound
	}
	return t, err
}

func (s *Server) UpdateTrigger(ctx context.Context, req gen.UpdateTriggerRequestObject) (gen.UpdateTriggerResponseObject, error) {
	t, err := s.suiteTrigger(ctx, req.Suite, req.Trigger, store.RoleEditor)
	if err != nil {
		return nil, err
	}
	if b := req.Body; b != nil {
		if b.Name != nil {
			t.Name = strings.TrimSpace(*b.Name)
		}
		if b.Config != nil {
			t.Config = *b.Config
		}
		if b.Enabled != nil {
			t.Enabled = *b.Enabled
		}
	}
	if err := prepareTrigger(&t); err != nil {
		return nil, err
	}
	if err := s.Store.UpdateTrigger(ctx, &t); err != nil {
		return nil, err
	}
	return gen.UpdateTrigger200JSONResponse(s.toTrigger(t)), nil
}

func (s *Server) DeleteTrigger(ctx context.Context, req gen.DeleteTriggerRequestObject) (gen.DeleteTriggerResponseObject, error) {
	t, err := s.suiteTrigger(ctx, req.Suite, req.Trigger, store.RoleEditor)
	if err != nil {
		return nil, err
	}
	if err := s.Store.DeleteTrigger(ctx, t.ID); err != nil {
		return nil, err
	}
	return gen.DeleteTrigger204Response{}, nil
}

func (s *Server) RotateTriggerSecret(ctx context.Context, req gen.RotateTriggerSecretRequestObject) (gen.RotateTriggerSecretResponseObject, error) {
	t, err := s.suiteTrigger(ctx, req.Suite, req.Trigger, store.RoleEditor)
	if err != nil {
		return nil, err
	}
	if t.Kind != "webhook" {
		return nil, invalid("only webhook triggers have a secret")
	}
	secret, err := s.newWebhookSecret(&t)
	if err != nil {
		return nil, err
	}
	if err := s.Store.UpdateTrigger(ctx, &t); err != nil {
		return nil, err
	}
	s.audit(ctx, t.OrgID, "trigger.rotate_secret", "trigger:"+t.ID.String(), nil)
	return gen.RotateTriggerSecret200JSONResponse(s.triggerCreated(t, secret)), nil
}
