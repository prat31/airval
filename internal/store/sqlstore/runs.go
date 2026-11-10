package sqlstore

import (
	"context"
	"sort"
	"time"

	"github.com/google/uuid"

	"github.com/airval/airval/internal/store"
)

const runCols = `id, org_id, project_id, suite_id, target_id, target_version_id, dataset_version_id, name,
	trigger_kind, trigger_ref, labels, config, status, total, completed, failed, errored, started_at, finished_at,
	baseline_run_id, parent_run_id, gate_passed, summary, cost_usd, tokens_in, tokens_out, error, created_by,
	created_at, updated_at`

func scanRun(r scanner) (store.Run, error) {
	var x store.Run
	err := r.Scan(&x.ID, &x.OrgID, &x.ProjectID, &x.SuiteID, &x.TargetID, &x.TargetVersionID, &x.DatasetVersionID,
		&x.Name, &x.TriggerKind, &x.TriggerRef, jsonCol(&x.Labels), rawJSON{&x.Config}, &x.Status, &x.Total,
		&x.Completed, &x.Failed, &x.Errored, &x.StartedAt, &x.FinishedAt, &x.BaselineRunID, &x.ParentRunID,
		&x.GatePassed, rawJSON{&x.Summary}, &x.CostUSD, &x.TokensIn, &x.TokensOut, &x.Error, &x.CreatedBy,
		&x.CreatedAt, &x.UpdatedAt)
	if x.Labels == nil {
		x.Labels = map[string]string{}
	}
	return x, err
}

func labelsArg(l map[string]string) any {
	if l == nil {
		return "{}"
	}
	return jsonValue(l)
}

func (s *Store) CreateRun(ctx context.Context, x *store.Run) error {
	if x.ID == uuid.Nil {
		x.ID = store.NewID()
	}
	if x.Status == "" {
		x.Status = store.RunQueued
	}
	if x.TriggerKind == "" {
		x.TriggerKind = "manual"
	}
	stampTimes(&x.CreatedAt, &x.UpdatedAt)
	return s.exec(ctx, `INSERT INTO runs (`+runCols+`) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,
		$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26,$27,$28,$29,$30)`,
		x.ID, x.OrgID, x.ProjectID, x.SuiteID, x.TargetID, x.TargetVersionID, x.DatasetVersionID, x.Name,
		x.TriggerKind, x.TriggerRef, labelsArg(x.Labels), jsonArg(x.Config, "{}"), x.Status, x.Total, x.Completed,
		x.Failed, x.Errored, utcPtr(x.StartedAt), utcPtr(x.FinishedAt), x.BaselineRunID, x.ParentRunID, x.GatePassed,
		jsonArg(x.Summary, ""), x.CostUSD, x.TokensIn, x.TokensOut, x.Error, x.CreatedBy, x.CreatedAt, x.UpdatedAt)
}

func (s *Store) GetRun(ctx context.Context, id uuid.UUID) (store.Run, error) {
	return one(ctx, s, `SELECT `+runCols+` FROM runs WHERE id = $1`, []any{id}, scanRun)
}

func (s *Store) runWhere(b *qb, f store.RunFilter) {
	if f.ProjectID != nil {
		b.where("project_id = " + b.arg(*f.ProjectID))
	}
	if len(f.OrgIDs) > 0 {
		b.where(inList(b, "org_id", f.OrgIDs))
	}
	if f.SuiteID != nil {
		b.where("suite_id = " + b.arg(*f.SuiteID))
	}
	if f.TargetID != nil {
		b.where("target_id = " + b.arg(*f.TargetID))
	}
	if f.ParentRunID != nil {
		b.where("parent_run_id = " + b.arg(*f.ParentRunID))
	}
	if len(f.Statuses) > 0 {
		b.where(inList(b, "status", f.Statuses))
	}
	if f.TriggerKind != "" {
		b.where("trigger_kind = " + b.arg(f.TriggerKind))
	}
	keys := make([]string, 0, len(f.Labels))
	for k := range f.Labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		b.where(s.d.jsonObjectField("labels", b.arg(k)) + " = " + b.arg(f.Labels[k]))
	}
	if f.CreatedAfter != nil {
		b.where("created_at >= " + b.arg(f.CreatedAfter.UTC()))
	}
	if f.CreatedBefore != nil {
		b.where("created_at < " + b.arg(f.CreatedBefore.UTC()))
	}
}

func (s *Store) ListRuns(ctx context.Context, f store.RunFilter, p store.ListParams) (store.Page[store.Run], error) {
	b := &qb{}
	s.runWhere(b, f)
	tail, limit, err := b.paginate("id", p, true)
	if err != nil {
		return store.Page[store.Run]{}, err
	}
	items, err := collect(ctx, s, `SELECT `+runCols+` FROM runs`+b.whereSQL()+tail, b.args, scanRun)
	if err != nil {
		return store.Page[store.Run]{}, err
	}
	return finishPage(items, limit, func(x store.Run) uuid.UUID { return x.ID }), nil
}

func (s *Store) LatestRun(ctx context.Context, f store.RunFilter) (store.Run, error) {
	b := &qb{}
	s.runWhere(b, f)
	return one(ctx, s, `SELECT `+runCols+` FROM runs`+b.whereSQL()+` ORDER BY id DESC LIMIT 1`, b.args, scanRun)
}

func (s *Store) UpdateRun(ctx context.Context, x *store.Run) error {
	stampTimes(nil, &x.UpdatedAt)
	return s.execAffect(ctx, `UPDATE runs SET suite_id = $2, target_id = $3, target_version_id = $4, dataset_version_id = $5,
		name = $6, trigger_kind = $7, trigger_ref = $8, labels = $9, config = $10, status = $11, total = $12,
		completed = $13, failed = $14, errored = $15, started_at = $16, finished_at = $17, baseline_run_id = $18,
		parent_run_id = $19, gate_passed = $20, summary = $21, cost_usd = $22, tokens_in = $23, tokens_out = $24,
		error = $25, updated_at = $26 WHERE id = $1`,
		x.ID, x.SuiteID, x.TargetID, x.TargetVersionID, x.DatasetVersionID, x.Name, x.TriggerKind, x.TriggerRef,
		labelsArg(x.Labels), jsonArg(x.Config, "{}"), x.Status, x.Total, x.Completed, x.Failed, x.Errored,
		utcPtr(x.StartedAt), utcPtr(x.FinishedAt), x.BaselineRunID, x.ParentRunID, x.GatePassed, jsonArg(x.Summary, ""),
		x.CostUSD, x.TokensIn, x.TokensOut, x.Error, x.UpdatedAt)
}

// TransitionRun moves a run to status `to` if its current status is in `from`. Moving to running
// sets started_at (once); moving to a terminal status sets finished_at.
func (s *Store) TransitionRun(ctx context.Context, id uuid.UUID, from []string, to string, at time.Time) (bool, error) {
	b := &qb{}
	set := "status = " + b.arg(to) + ", updated_at = " + b.arg(at.UTC())
	switch to {
	case store.RunRunning:
		set += ", started_at = COALESCE(started_at, $2)"
	case store.RunSucceeded, store.RunFailed, store.RunErrored, store.RunCanceled:
		set += ", finished_at = $2"
	}
	b.where("id = " + b.arg(id))
	if len(from) > 0 {
		b.where(inList(b, "status", from))
	}
	res, err := s.q.ExecContext(ctx, `UPDATE runs SET `+set+b.whereSQL(), b.args...)
	if err != nil {
		return false, s.mapErr(err)
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

func (s *Store) AddRunProgress(ctx context.Context, id uuid.UUID, d store.RunProgress) error {
	return s.exec(ctx, `UPDATE runs SET completed = completed + $2, failed = failed + $3, errored = errored + $4,
		cost_usd = cost_usd + $5, tokens_in = tokens_in + $6, tokens_out = tokens_out + $7, updated_at = $8 WHERE id = $1`,
		id, d.Completed, d.Failed, d.Errored, d.CostUSD, d.TokensIn, d.TokensOut, now())
}

func (s *Store) DeleteRun(ctx context.Context, id uuid.UUID) error {
	return s.execAffect(ctx, `DELETE FROM runs WHERE id = $1`, id)
}
