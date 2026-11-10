package sqlstore

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/airval/airval/internal/store"
)

// ---- evaluators ----

const evaluatorCols = "id, org_id, project_id, name, description, kind, config, version, created_at, updated_at"

func scanEvaluator(r scanner) (store.Evaluator, error) {
	var e store.Evaluator
	err := r.Scan(&e.ID, &e.OrgID, &e.ProjectID, &e.Name, &e.Description, &e.Kind, rawJSON{&e.Config}, &e.Version,
		&e.CreatedAt, &e.UpdatedAt)
	return e, err
}

func (s *Store) CreateEvaluator(ctx context.Context, e *store.Evaluator) error {
	if e.ID == uuid.Nil {
		e.ID = store.NewID()
	}
	if e.Version == 0 {
		e.Version = 1
	}
	stampTimes(&e.CreatedAt, &e.UpdatedAt)
	return s.exec(ctx, `INSERT INTO evaluators (`+evaluatorCols+`) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		e.ID, e.OrgID, e.ProjectID, e.Name, e.Description, e.Kind, jsonArg(e.Config, "{}"), e.Version, e.CreatedAt, e.UpdatedAt)
}

func (s *Store) GetEvaluator(ctx context.Context, id uuid.UUID) (store.Evaluator, error) {
	return one(ctx, s, `SELECT `+evaluatorCols+` FROM evaluators WHERE id = $1`, []any{id}, scanEvaluator)
}

func (s *Store) GetEvaluators(ctx context.Context, ids []uuid.UUID) ([]store.Evaluator, error) {
	var out []store.Evaluator
	for _, c := range chunks(ids, chunkSize) {
		b := &qb{}
		b.where(inList(b, "id", c))
		rows, err := collect(ctx, s, `SELECT `+evaluatorCols+` FROM evaluators`+b.whereSQL()+` ORDER BY id`, b.args, scanEvaluator)
		if err != nil {
			return nil, err
		}
		out = append(out, rows...)
	}
	return out, nil
}

func (s *Store) ListEvaluators(ctx context.Context, projectID uuid.UUID, p store.ListParams) (store.Page[store.Evaluator], error) {
	b := &qb{}
	b.where("project_id = " + b.arg(projectID))
	tail, limit, err := b.paginate("id", p, false)
	if err != nil {
		return store.Page[store.Evaluator]{}, err
	}
	items, err := collect(ctx, s, `SELECT `+evaluatorCols+` FROM evaluators`+b.whereSQL()+tail, b.args, scanEvaluator)
	if err != nil {
		return store.Page[store.Evaluator]{}, err
	}
	return finishPage(items, limit, func(e store.Evaluator) uuid.UUID { return e.ID }), nil
}

func (s *Store) UpdateEvaluator(ctx context.Context, e *store.Evaluator) error {
	stampTimes(nil, &e.UpdatedAt)
	return s.execAffect(ctx, `UPDATE evaluators SET name = $2, description = $3, kind = $4, config = $5, version = $6,
		updated_at = $7 WHERE id = $1`, e.ID, e.Name, e.Description, e.Kind, jsonArg(e.Config, "{}"), e.Version, e.UpdatedAt)
}

func (s *Store) DeleteEvaluator(ctx context.Context, id uuid.UUID) error {
	return s.execAffect(ctx, `DELETE FROM evaluators WHERE id = $1`, id)
}

// ---- suites ----

const suiteCols = "id, org_id, project_id, name, slug, description, config, baseline_run_id, created_at, updated_at"

func scanSuite(r scanner) (store.Suite, error) {
	var x store.Suite
	err := r.Scan(&x.ID, &x.OrgID, &x.ProjectID, &x.Name, &x.Slug, &x.Description, rawJSON{&x.Config}, &x.BaselineRunID,
		&x.CreatedAt, &x.UpdatedAt)
	return x, err
}

func (s *Store) CreateSuite(ctx context.Context, x *store.Suite) error {
	if x.ID == uuid.Nil {
		x.ID = store.NewID()
	}
	stampTimes(&x.CreatedAt, &x.UpdatedAt)
	return s.exec(ctx, `INSERT INTO suites (`+suiteCols+`) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		x.ID, x.OrgID, x.ProjectID, x.Name, x.Slug, x.Description, jsonArg(x.Config, "{}"), x.BaselineRunID, x.CreatedAt, x.UpdatedAt)
}

func (s *Store) GetSuite(ctx context.Context, id uuid.UUID) (store.Suite, error) {
	return one(ctx, s, `SELECT `+suiteCols+` FROM suites WHERE id = $1`, []any{id}, scanSuite)
}

func (s *Store) GetSuiteBySlug(ctx context.Context, projectID uuid.UUID, slug string) (store.Suite, error) {
	return one(ctx, s, `SELECT `+suiteCols+` FROM suites WHERE project_id = $1 AND slug = $2`, []any{projectID, slug}, scanSuite)
}

func (s *Store) ListSuites(ctx context.Context, projectID uuid.UUID, p store.ListParams) (store.Page[store.Suite], error) {
	b := &qb{}
	b.where("project_id = " + b.arg(projectID))
	tail, limit, err := b.paginate("id", p, true)
	if err != nil {
		return store.Page[store.Suite]{}, err
	}
	items, err := collect(ctx, s, `SELECT `+suiteCols+` FROM suites`+b.whereSQL()+tail, b.args, scanSuite)
	if err != nil {
		return store.Page[store.Suite]{}, err
	}
	return finishPage(items, limit, func(x store.Suite) uuid.UUID { return x.ID }), nil
}

func (s *Store) UpdateSuite(ctx context.Context, x *store.Suite) error {
	stampTimes(nil, &x.UpdatedAt)
	return s.execAffect(ctx, `UPDATE suites SET name = $2, slug = $3, description = $4, config = $5, baseline_run_id = $6,
		updated_at = $7 WHERE id = $1`, x.ID, x.Name, x.Slug, x.Description, jsonArg(x.Config, "{}"), x.BaselineRunID, x.UpdatedAt)
}

func (s *Store) DeleteSuite(ctx context.Context, id uuid.UUID) error {
	return s.execAffect(ctx, `DELETE FROM suites WHERE id = $1`, id)
}

// ---- gates ----

const gateCols = "id, org_id, project_id, suite_id, name, expr, enabled, created_at, updated_at"

func scanGate(r scanner) (store.Gate, error) {
	var g store.Gate
	err := r.Scan(&g.ID, &g.OrgID, &g.ProjectID, &g.SuiteID, &g.Name, rawJSON{&g.Expr}, &g.Enabled, &g.CreatedAt, &g.UpdatedAt)
	return g, err
}

func (s *Store) CreateGate(ctx context.Context, g *store.Gate) error {
	if g.ID == uuid.Nil {
		g.ID = store.NewID()
	}
	stampTimes(&g.CreatedAt, &g.UpdatedAt)
	return s.exec(ctx, `INSERT INTO gates (`+gateCols+`) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		g.ID, g.OrgID, g.ProjectID, g.SuiteID, g.Name, jsonArg(g.Expr, "{}"), g.Enabled, g.CreatedAt, g.UpdatedAt)
}

func (s *Store) GetGate(ctx context.Context, id uuid.UUID) (store.Gate, error) {
	return one(ctx, s, `SELECT `+gateCols+` FROM gates WHERE id = $1`, []any{id}, scanGate)
}

func (s *Store) ListGates(ctx context.Context, suiteID uuid.UUID) ([]store.Gate, error) {
	items, err := collect(ctx, s, `SELECT `+gateCols+` FROM gates WHERE suite_id = $1 ORDER BY id`, []any{suiteID}, scanGate)
	if items == nil && err == nil {
		items = []store.Gate{}
	}
	return items, err
}

func (s *Store) UpdateGate(ctx context.Context, g *store.Gate) error {
	stampTimes(nil, &g.UpdatedAt)
	return s.execAffect(ctx, `UPDATE gates SET name = $2, expr = $3, enabled = $4, updated_at = $5 WHERE id = $1`,
		g.ID, g.Name, jsonArg(g.Expr, "{}"), g.Enabled, g.UpdatedAt)
}

func (s *Store) DeleteGate(ctx context.Context, id uuid.UUID) error {
	return s.execAffect(ctx, `DELETE FROM gates WHERE id = $1`, id)
}

// ---- triggers ----

const triggerCols = `id, org_id, project_id, suite_id, name, kind, config, enabled, secret_enc, secret_key_version,
	next_fire_at, last_fired_at, created_at, updated_at`

func scanTrigger(r scanner) (store.Trigger, error) {
	var t store.Trigger
	err := r.Scan(&t.ID, &t.OrgID, &t.ProjectID, &t.SuiteID, &t.Name, &t.Kind, rawJSON{&t.Config}, &t.Enabled,
		&t.SecretEnc, &t.SecretKeyVersion, &t.NextFireAt, &t.LastFiredAt, &t.CreatedAt, &t.UpdatedAt)
	return t, err
}

func utcPtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}

func (s *Store) CreateTrigger(ctx context.Context, t *store.Trigger) error {
	if t.ID == uuid.Nil {
		t.ID = store.NewID()
	}
	stampTimes(&t.CreatedAt, &t.UpdatedAt)
	return s.exec(ctx, `INSERT INTO triggers (`+triggerCols+`) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`,
		t.ID, t.OrgID, t.ProjectID, t.SuiteID, t.Name, t.Kind, jsonArg(t.Config, "{}"), t.Enabled, t.SecretEnc,
		t.SecretKeyVersion, utcPtr(t.NextFireAt), utcPtr(t.LastFiredAt), t.CreatedAt, t.UpdatedAt)
}

func (s *Store) GetTrigger(ctx context.Context, id uuid.UUID) (store.Trigger, error) {
	return one(ctx, s, `SELECT `+triggerCols+` FROM triggers WHERE id = $1`, []any{id}, scanTrigger)
}

func (s *Store) ListTriggers(ctx context.Context, suiteID uuid.UUID) ([]store.Trigger, error) {
	items, err := collect(ctx, s, `SELECT `+triggerCols+` FROM triggers WHERE suite_id = $1 ORDER BY id`, []any{suiteID}, scanTrigger)
	if items == nil && err == nil {
		items = []store.Trigger{}
	}
	return items, err
}

func (s *Store) UpdateTrigger(ctx context.Context, t *store.Trigger) error {
	stampTimes(nil, &t.UpdatedAt)
	return s.execAffect(ctx, `UPDATE triggers SET name = $2, kind = $3, config = $4, enabled = $5, secret_enc = $6,
		secret_key_version = $7, next_fire_at = $8, last_fired_at = $9, updated_at = $10 WHERE id = $1`,
		t.ID, t.Name, t.Kind, jsonArg(t.Config, "{}"), t.Enabled, t.SecretEnc, t.SecretKeyVersion,
		utcPtr(t.NextFireAt), utcPtr(t.LastFiredAt), t.UpdatedAt)
}

func (s *Store) DeleteTrigger(ctx context.Context, id uuid.UUID) error {
	return s.execAffect(ctx, `DELETE FROM triggers WHERE id = $1`, id)
}

func (s *Store) ListDueTriggers(ctx context.Context, at time.Time, limit int) ([]store.Trigger, error) {
	if limit <= 0 {
		limit = 100
	}
	return collect(ctx, s, `SELECT `+triggerCols+` FROM triggers
		WHERE enabled = TRUE AND kind = 'schedule' AND next_fire_at IS NOT NULL AND next_fire_at <= $1
		ORDER BY next_fire_at LIMIT $2`, []any{at.UTC(), limit}, scanTrigger)
}

func (s *Store) MarkTriggerFired(ctx context.Context, id uuid.UUID, expectNext, firedAt time.Time, next *time.Time) (bool, error) {
	res, err := s.q.ExecContext(ctx, `UPDATE triggers SET last_fired_at = $3, next_fire_at = $4, updated_at = $3
		WHERE id = $1 AND next_fire_at = $2`, id, expectNext.UTC(), firedAt.UTC(), utcPtr(next))
	if err != nil {
		return false, s.mapErr(err)
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}
