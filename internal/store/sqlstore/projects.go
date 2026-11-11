package sqlstore

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"

	"github.com/prat31/airval/internal/store"
)

// ---- projects ----

const projectCols = "id, org_id, name, slug, description, color, icon, settings, created_at, updated_at"

func scanProject(r scanner) (store.Project, error) {
	var p store.Project
	err := r.Scan(&p.ID, &p.OrgID, &p.Name, &p.Slug, &p.Description, &p.Color, &p.Icon,
		rawJSON{&p.Settings}, &p.CreatedAt, &p.UpdatedAt)
	return p, err
}

func (s *Store) CreateProject(ctx context.Context, p *store.Project) error {
	if p.ID == uuid.Nil {
		p.ID = store.NewID()
	}
	stampTimes(&p.CreatedAt, &p.UpdatedAt)
	if len(p.Settings) == 0 {
		p.Settings = json.RawMessage(`{}`)
	}
	return s.exec(ctx, `INSERT INTO projects (`+projectCols+`) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		p.ID, p.OrgID, p.Name, p.Slug, p.Description, p.Color, p.Icon, jsonArg(p.Settings, "{}"), p.CreatedAt, p.UpdatedAt)
}

func (s *Store) GetProject(ctx context.Context, id uuid.UUID) (store.Project, error) {
	return one(ctx, s, `SELECT `+projectCols+` FROM projects WHERE id = $1`, []any{id}, scanProject)
}

func (s *Store) ListProjects(ctx context.Context, f store.ProjectFilter, p store.ListParams) (store.Page[store.Project], error) {
	if len(f.OrgIDs) == 0 {
		return store.Page[store.Project]{Items: []store.Project{}}, nil
	}
	b := &qb{}
	b.where(inList(b, "org_id", f.OrgIDs))
	if len(f.IDs) > 0 {
		b.where(inList(b, "id", f.IDs))
	}
	if f.Slug != "" {
		b.where("slug = " + b.arg(f.Slug))
	}
	tail, limit, err := b.paginate("id", p, true)
	if err != nil {
		return store.Page[store.Project]{}, err
	}
	items, err := collect(ctx, s, `SELECT `+projectCols+` FROM projects`+b.whereSQL()+tail, b.args, scanProject)
	if err != nil {
		return store.Page[store.Project]{}, err
	}
	return finishPage(items, limit, func(p store.Project) uuid.UUID { return p.ID }), nil
}

func (s *Store) UpdateProject(ctx context.Context, p *store.Project) error {
	stampTimes(nil, &p.UpdatedAt)
	return s.execAffect(ctx, `UPDATE projects SET name = $2, slug = $3, description = $4, color = $5, icon = $6,
		settings = $7, updated_at = $8 WHERE id = $1`,
		p.ID, p.Name, p.Slug, p.Description, p.Color, p.Icon, jsonArg(p.Settings, "{}"), p.UpdatedAt)
}

func (s *Store) DeleteProject(ctx context.Context, id uuid.UUID) error {
	return s.execAffect(ctx, `DELETE FROM projects WHERE id = $1`, id)
}

// ---- secrets ----

const secretCols = "id, org_id, project_id, name, description, ciphertext, nonce, key_version, created_by, created_at, updated_at"

func scanSecret(r scanner) (store.Secret, error) {
	var x store.Secret
	err := r.Scan(&x.ID, &x.OrgID, &x.ProjectID, &x.Name, &x.Description, &x.Ciphertext, &x.Nonce,
		&x.KeyVersion, &x.CreatedBy, &x.CreatedAt, &x.UpdatedAt)
	return x, err
}

func (s *Store) CreateSecret(ctx context.Context, x *store.Secret) error {
	if x.ID == uuid.Nil {
		x.ID = store.NewID()
	}
	stampTimes(&x.CreatedAt, &x.UpdatedAt)
	return s.exec(ctx, `INSERT INTO secrets (`+secretCols+`) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		x.ID, x.OrgID, x.ProjectID, x.Name, x.Description, x.Ciphertext, x.Nonce, x.KeyVersion, x.CreatedBy,
		x.CreatedAt, x.UpdatedAt)
}

func (s *Store) GetSecret(ctx context.Context, id uuid.UUID) (store.Secret, error) {
	return one(ctx, s, `SELECT `+secretCols+` FROM secrets WHERE id = $1`, []any{id}, scanSecret)
}

func (s *Store) ResolveSecret(ctx context.Context, orgID uuid.UUID, projectID *uuid.UUID, name string) (store.Secret, error) {
	if projectID != nil {
		x, err := one(ctx, s, `SELECT `+secretCols+` FROM secrets WHERE org_id = $1 AND project_id = $2 AND name = $3`,
			[]any{orgID, *projectID, name}, scanSecret)
		if err == nil || err != store.ErrNotFound {
			return x, err
		}
	}
	return one(ctx, s, `SELECT `+secretCols+` FROM secrets WHERE org_id = $1 AND project_id IS NULL AND name = $2`,
		[]any{orgID, name}, scanSecret)
}

func (s *Store) ListSecrets(ctx context.Context, f store.SecretFilter, p store.ListParams) (store.Page[store.Secret], error) {
	b := &qb{}
	b.where("org_id = " + b.arg(f.OrgID))
	switch {
	case f.AllScopes:
	case f.ProjectID != nil:
		b.where("project_id = " + b.arg(*f.ProjectID))
	default:
		b.where("project_id IS NULL")
	}
	tail, limit, err := b.paginate("id", p, true)
	if err != nil {
		return store.Page[store.Secret]{}, err
	}
	items, err := collect(ctx, s, `SELECT `+secretCols+` FROM secrets`+b.whereSQL()+tail, b.args, scanSecret)
	if err != nil {
		return store.Page[store.Secret]{}, err
	}
	return finishPage(items, limit, func(x store.Secret) uuid.UUID { return x.ID }), nil
}

func (s *Store) UpdateSecret(ctx context.Context, x *store.Secret) error {
	stampTimes(nil, &x.UpdatedAt)
	return s.execAffect(ctx, `UPDATE secrets SET name = $2, description = $3, ciphertext = $4, nonce = $5,
		key_version = $6, updated_at = $7 WHERE id = $1`,
		x.ID, x.Name, x.Description, x.Ciphertext, x.Nonce, x.KeyVersion, x.UpdatedAt)
}

func (s *Store) DeleteSecret(ctx context.Context, id uuid.UUID) error {
	return s.execAffect(ctx, `DELETE FROM secrets WHERE id = $1`, id)
}

func (s *Store) ListSecretsNotAtKeyVersion(ctx context.Context, version, limit int) ([]store.Secret, error) {
	if limit <= 0 {
		limit = 500
	}
	return collect(ctx, s, `SELECT `+secretCols+` FROM secrets WHERE key_version <> $1 ORDER BY id LIMIT $2`,
		[]any{version, limit}, scanSecret)
}

// ---- comparisons ----

const comparisonCols = "id, org_id, project_id, name, run_ids, created_by, created_at, updated_at"

func scanComparison(r scanner) (store.Comparison, error) {
	var c store.Comparison
	err := r.Scan(&c.ID, &c.OrgID, &c.ProjectID, &c.Name, jsonCol(&c.RunIDs), &c.CreatedBy, &c.CreatedAt, &c.UpdatedAt)
	return c, err
}

func (s *Store) CreateComparison(ctx context.Context, c *store.Comparison) error {
	if c.ID == uuid.Nil {
		c.ID = store.NewID()
	}
	stampTimes(&c.CreatedAt, &c.UpdatedAt)
	if c.RunIDs == nil {
		c.RunIDs = []uuid.UUID{}
	}
	return s.exec(ctx, `INSERT INTO comparisons (`+comparisonCols+`) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		c.ID, c.OrgID, c.ProjectID, c.Name, jsonValue(c.RunIDs), c.CreatedBy, c.CreatedAt, c.UpdatedAt)
}

func (s *Store) GetComparison(ctx context.Context, id uuid.UUID) (store.Comparison, error) {
	return one(ctx, s, `SELECT `+comparisonCols+` FROM comparisons WHERE id = $1`, []any{id}, scanComparison)
}

func (s *Store) ListComparisons(ctx context.Context, projectID uuid.UUID, p store.ListParams) (store.Page[store.Comparison], error) {
	b := &qb{}
	b.where("project_id = " + b.arg(projectID))
	tail, limit, err := b.paginate("id", p, true)
	if err != nil {
		return store.Page[store.Comparison]{}, err
	}
	items, err := collect(ctx, s, `SELECT `+comparisonCols+` FROM comparisons`+b.whereSQL()+tail, b.args, scanComparison)
	if err != nil {
		return store.Page[store.Comparison]{}, err
	}
	return finishPage(items, limit, func(c store.Comparison) uuid.UUID { return c.ID }), nil
}

func (s *Store) DeleteComparison(ctx context.Context, id uuid.UUID) error {
	return s.execAffect(ctx, `DELETE FROM comparisons WHERE id = $1`, id)
}

// ---- notification channels ----

const channelCols = "id, org_id, name, kind, config, events, enabled, created_at, updated_at"

func scanChannel(r scanner) (store.NotificationChannel, error) {
	var c store.NotificationChannel
	err := r.Scan(&c.ID, &c.OrgID, &c.Name, &c.Kind, rawJSON{&c.Config}, jsonCol(&c.Events), &c.Enabled,
		&c.CreatedAt, &c.UpdatedAt)
	return c, err
}

func (s *Store) CreateNotificationChannel(ctx context.Context, c *store.NotificationChannel) error {
	if c.ID == uuid.Nil {
		c.ID = store.NewID()
	}
	stampTimes(&c.CreatedAt, &c.UpdatedAt)
	return s.exec(ctx, `INSERT INTO notification_channels (`+channelCols+`) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		c.ID, c.OrgID, c.Name, c.Kind, jsonArg(c.Config, "{}"), jsonValue(strs(c.Events)), c.Enabled, c.CreatedAt, c.UpdatedAt)
}

func (s *Store) GetNotificationChannel(ctx context.Context, id uuid.UUID) (store.NotificationChannel, error) {
	return one(ctx, s, `SELECT `+channelCols+` FROM notification_channels WHERE id = $1`, []any{id}, scanChannel)
}

func (s *Store) ListNotificationChannels(ctx context.Context, orgID uuid.UUID, p store.ListParams) (store.Page[store.NotificationChannel], error) {
	b := &qb{}
	b.where("org_id = " + b.arg(orgID))
	tail, limit, err := b.paginate("id", p, false)
	if err != nil {
		return store.Page[store.NotificationChannel]{}, err
	}
	items, err := collect(ctx, s, `SELECT `+channelCols+` FROM notification_channels`+b.whereSQL()+tail, b.args, scanChannel)
	if err != nil {
		return store.Page[store.NotificationChannel]{}, err
	}
	return finishPage(items, limit, func(c store.NotificationChannel) uuid.UUID { return c.ID }), nil
}

func (s *Store) UpdateNotificationChannel(ctx context.Context, c *store.NotificationChannel) error {
	stampTimes(nil, &c.UpdatedAt)
	return s.execAffect(ctx, `UPDATE notification_channels SET name = $2, kind = $3, config = $4, events = $5,
		enabled = $6, updated_at = $7 WHERE id = $1`,
		c.ID, c.Name, c.Kind, jsonArg(c.Config, "{}"), jsonValue(strs(c.Events)), c.Enabled, c.UpdatedAt)
}

func (s *Store) DeleteNotificationChannel(ctx context.Context, id uuid.UUID) error {
	return s.execAffect(ctx, `DELETE FROM notification_channels WHERE id = $1`, id)
}

// ---- audit log ----

func (s *Store) AppendAudit(ctx context.Context, e *store.AuditEntry) error {
	if e.ID == uuid.Nil {
		e.ID = store.NewID()
	}
	if e.At.IsZero() {
		e.At = now()
	}
	return s.exec(ctx, `INSERT INTO audit_log (id, org_id, actor_id, actor, action, target, diff, at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`, e.ID, e.OrgID, e.ActorID, e.Actor, e.Action, e.Target, jsonArg(e.Diff, ""), e.At)
}

func (s *Store) ListAudit(ctx context.Context, orgID uuid.UUID, p store.ListParams) (store.Page[store.AuditEntry], error) {
	b := &qb{}
	b.where("org_id = " + b.arg(orgID))
	tail, limit, err := b.paginate("id", p, true)
	if err != nil {
		return store.Page[store.AuditEntry]{}, err
	}
	items, err := collect(ctx, s, `SELECT id, org_id, actor_id, actor, action, target, diff, at FROM audit_log`+
		b.whereSQL()+tail, b.args, func(r scanner) (store.AuditEntry, error) {
		var e store.AuditEntry
		err := r.Scan(&e.ID, &e.OrgID, &e.ActorID, &e.Actor, &e.Action, &e.Target, rawJSON{&e.Diff}, &e.At)
		return e, err
	})
	if err != nil {
		return store.Page[store.AuditEntry]{}, err
	}
	return finishPage(items, limit, func(e store.AuditEntry) uuid.UUID { return e.ID }), nil
}
