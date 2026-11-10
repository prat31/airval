package sqlstore

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/airval/airval/internal/store"
)

const targetSelect = `SELECT t.id, t.org_id, t.project_id, t.name, t.description, t.kind, t.current_version_id,
	t.created_at, t.updated_at, v.id, v.number, v.config, v.created_by, v.created_at
	FROM targets t LEFT JOIN target_versions v ON v.id = t.current_version_id`

func scanTarget(r scanner) (store.Target, error) {
	var (
		t       store.Target
		vid     *uuid.UUID
		vnum    *int
		vcfg    json.RawMessage
		vby     *uuid.UUID
		vcreate *time.Time
	)
	err := r.Scan(&t.ID, &t.OrgID, &t.ProjectID, &t.Name, &t.Description, &t.Kind, &t.CurrentVersionID,
		&t.CreatedAt, &t.UpdatedAt, &vid, &vnum, rawJSON{&vcfg}, &vby, &vcreate)
	if err != nil {
		return t, err
	}
	if vid != nil {
		t.CurrentVersion = &store.TargetVersion{ID: *vid, OrgID: t.OrgID, ProjectID: t.ProjectID, TargetID: t.ID,
			Number: *vnum, Config: vcfg, CreatedBy: vby, CreatedAt: *vcreate}
	}
	return t, nil
}

func (s *Store) CreateTarget(ctx context.Context, t *store.Target, config []byte, createdBy *uuid.UUID) error {
	if t.ID == uuid.Nil {
		t.ID = store.NewID()
	}
	stampTimes(&t.CreatedAt, &t.UpdatedAt)
	return s.withTx(ctx, func(tx *Store) error {
		if err := tx.exec(ctx, `INSERT INTO targets (id, org_id, project_id, name, description, kind, created_at, updated_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
			t.ID, t.OrgID, t.ProjectID, t.Name, t.Description, t.Kind, t.CreatedAt, t.UpdatedAt); err != nil {
			return err
		}
		v, err := tx.AddTargetVersion(ctx, t.ID, config, createdBy)
		if err != nil {
			return err
		}
		t.CurrentVersionID, t.CurrentVersion = &v.ID, &v
		t.UpdatedAt = v.CreatedAt
		return nil
	})
}

func (s *Store) GetTarget(ctx context.Context, id uuid.UUID) (store.Target, error) {
	return one(ctx, s, targetSelect+` WHERE t.id = $1`, []any{id}, scanTarget)
}

func (s *Store) ListTargets(ctx context.Context, projectID uuid.UUID, p store.ListParams) (store.Page[store.Target], error) {
	b := &qb{}
	b.where("t.project_id = " + b.arg(projectID))
	tail, limit, err := b.paginate("t.id", p, false)
	if err != nil {
		return store.Page[store.Target]{}, err
	}
	items, err := collect(ctx, s, targetSelect+b.whereSQL()+tail, b.args, scanTarget)
	if err != nil {
		return store.Page[store.Target]{}, err
	}
	return finishPage(items, limit, func(t store.Target) uuid.UUID { return t.ID }), nil
}

func (s *Store) UpdateTarget(ctx context.Context, t *store.Target) error {
	stampTimes(nil, &t.UpdatedAt)
	return s.execAffect(ctx, `UPDATE targets SET name = $2, description = $3, kind = $4, updated_at = $5 WHERE id = $1`,
		t.ID, t.Name, t.Description, t.Kind, t.UpdatedAt)
}

func (s *Store) DeleteTarget(ctx context.Context, id uuid.UUID) error {
	return s.execAffect(ctx, `DELETE FROM targets WHERE id = $1`, id)
}

func (s *Store) AddTargetVersion(ctx context.Context, targetID uuid.UUID, config []byte, createdBy *uuid.UUID) (store.TargetVersion, error) {
	var v store.TargetVersion
	err := s.withTx(ctx, func(tx *Store) error {
		// Touch the target first: takes the row lock on Postgres and validates existence.
		t := now()
		if err := tx.execAffect(ctx, `UPDATE targets SET updated_at = $2 WHERE id = $1`, targetID, t); err != nil {
			return err
		}
		err := tx.q.QueryRowContext(ctx, `SELECT org_id, project_id,
			(SELECT COALESCE(MAX(number), 0) + 1 FROM target_versions WHERE target_id = $1)
			FROM targets WHERE id = $1`, targetID).Scan(&v.OrgID, &v.ProjectID, &v.Number)
		if err != nil {
			return tx.mapErr(err)
		}
		v.ID, v.TargetID, v.Config, v.CreatedBy, v.CreatedAt = store.NewID(), targetID, config, createdBy, t
		if err := tx.exec(ctx, `INSERT INTO target_versions (id, org_id, project_id, target_id, number, config, created_by, created_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
			v.ID, v.OrgID, v.ProjectID, v.TargetID, v.Number, jsonArg(config, "{}"), createdBy, v.CreatedAt); err != nil {
			return err
		}
		return tx.exec(ctx, `UPDATE targets SET current_version_id = $2 WHERE id = $1`, targetID, v.ID)
	})
	return v, err
}

const targetVersionCols = "id, org_id, project_id, target_id, number, config, created_by, created_at"

func scanTargetVersion(r scanner) (store.TargetVersion, error) {
	var v store.TargetVersion
	err := r.Scan(&v.ID, &v.OrgID, &v.ProjectID, &v.TargetID, &v.Number, rawJSON{&v.Config}, &v.CreatedBy, &v.CreatedAt)
	return v, err
}

func (s *Store) GetTargetVersion(ctx context.Context, id uuid.UUID) (store.TargetVersion, error) {
	return one(ctx, s, `SELECT `+targetVersionCols+` FROM target_versions WHERE id = $1`, []any{id}, scanTargetVersion)
}

func (s *Store) ListTargetVersions(ctx context.Context, targetID uuid.UUID, p store.ListParams) (store.Page[store.TargetVersion], error) {
	b := &qb{}
	b.where("target_id = " + b.arg(targetID))
	tail, limit, err := b.paginate("id", p, true)
	if err != nil {
		return store.Page[store.TargetVersion]{}, err
	}
	items, err := collect(ctx, s, `SELECT `+targetVersionCols+` FROM target_versions`+b.whereSQL()+tail, b.args, scanTargetVersion)
	if err != nil {
		return store.Page[store.TargetVersion]{}, err
	}
	return finishPage(items, limit, func(v store.TargetVersion) uuid.UUID { return v.ID }), nil
}
