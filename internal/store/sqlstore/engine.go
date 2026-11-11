package sqlstore

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"

	"github.com/prat31/airval/internal/store"
)

func (s *Store) CreateRunShards(ctx context.Context, runID uuid.UUID, items []int) error {
	if len(items) == 0 {
		return nil
	}
	return s.withTx(ctx, func(tx *Store) error {
		const cols = 3
		idx := make([]int, len(items))
		for i := range idx {
			idx[i] = i
		}
		for _, c := range chunks(idx, 1000) {
			args := make([]any, 0, len(c)*cols)
			for _, i := range c {
				args = append(args, runID, i, items[i])
			}
			if err := tx.exec(ctx, `INSERT INTO run_shards (run_id, shard, items) VALUES `+valuesSQL(len(c), cols), args...); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *Store) CompleteRunShard(ctx context.Context, runID uuid.UUID, shard int, stats json.RawMessage) (int, error) {
	var remaining int
	err := s.withTx(ctx, func(tx *Store) error {
		// Serialize completions per run so exactly one completer observes remaining == 0.
		// (SQLite transactions are already serialized by _txlock=immediate.)
		if tx.d.name() == "postgres" {
			var id uuid.UUID
			if err := tx.q.QueryRowContext(ctx, `SELECT id FROM runs WHERE id = $1 FOR UPDATE`, runID).Scan(&id); err != nil {
				return tx.mapErr(err)
			}
		}
		if err := tx.exec(ctx, `UPDATE run_shards SET done_at = $3, stats = $4 WHERE run_id = $1 AND shard = $2 AND done_at IS NULL`,
			runID, shard, now(), jsonArg(stats, "")); err != nil {
			return err
		}
		return tx.q.QueryRowContext(ctx, `SELECT COUNT(*) FROM run_shards WHERE run_id = $1 AND done_at IS NULL`, runID).Scan(&remaining)
	})
	return remaining, err
}

func (s *Store) ListRunShards(ctx context.Context, runID uuid.UUID) ([]store.RunShard, error) {
	out, err := collect(ctx, s, `SELECT run_id, shard, items, stats, done_at FROM run_shards WHERE run_id = $1 ORDER BY shard`,
		[]any{runID}, func(r scanner) (store.RunShard, error) {
			var x store.RunShard
			err := r.Scan(&x.RunID, &x.Shard, &x.Items, rawJSON{&x.Stats}, &x.DoneAt)
			return x, err
		})
	if out == nil {
		out = []store.RunShard{}
	}
	return out, err
}

func (s *Store) IncrRunProgress(ctx context.Context, id uuid.UUID, d store.RunProgress) (store.RunCounters, error) {
	var c store.RunCounters
	err := s.q.QueryRowContext(ctx, `UPDATE runs SET completed = completed + $2, failed = failed + $3, errored = errored + $4,
		cost_usd = cost_usd + $5, tokens_in = tokens_in + $6, tokens_out = tokens_out + $7, updated_at = $8 WHERE id = $1
		RETURNING total, completed, failed, errored, cost_usd, tokens_in, tokens_out`,
		id, d.Completed, d.Failed, d.Errored, d.CostUSD, d.TokensIn, d.TokensOut, now()).
		Scan(&c.Total, &c.Completed, &c.Failed, &c.Errored, &c.CostUSD, &c.TokensIn, &c.TokensOut)
	return c, s.mapErr(err)
}

func (s *Store) SetRunError(ctx context.Context, id uuid.UUID, msg string) error {
	return s.execAffect(ctx, `UPDATE runs SET error = $2, updated_at = $3 WHERE id = $1`, id, msg, now())
}

func (s *Store) FinishRun(ctx context.Context, id uuid.UUID, f store.RunFinish) (string, error) {
	at := f.FinishedAt
	if at.IsZero() {
		at = now()
	}
	var status string
	err := s.q.QueryRowContext(ctx, `UPDATE runs SET
		status = CASE WHEN status IN ('queued', 'running') THEN CAST($2 AS TEXT) ELSE status END,
		completed = $3, failed = $4, errored = $5, cost_usd = $6, tokens_in = $7, tokens_out = $8,
		summary = $9, gate_passed = $10, error = CASE WHEN CAST($11 AS TEXT) = '' THEN error ELSE CAST($11 AS TEXT) END,
		finished_at = COALESCE(finished_at, $12), started_at = COALESCE(started_at, $12), updated_at = $12
		WHERE id = $1 RETURNING status`,
		id, f.Status, f.Completed, f.Failed, f.Errored, f.CostUSD, f.TokensIn, f.TokensOut, jsonArg(f.Summary, ""),
		f.GatePassed, f.Error, at.UTC()).Scan(&status)
	return status, s.mapErr(err)
}

func (s *Store) ListRunResultRefs(ctx context.Context, runID uuid.UUID, statuses []string) ([]store.ResultRef, error) {
	b := &qb{}
	b.where("run_id = " + b.arg(runID))
	if len(statuses) > 0 {
		b.where(inList(b, "status", statuses))
	}
	out, err := collect(ctx, s, `SELECT id, example_id, example_revision_id, trial, status FROM results`+b.whereSQL()+` ORDER BY id`,
		b.args, func(r scanner) (store.ResultRef, error) {
			var x store.ResultRef
			err := r.Scan(&x.ID, &x.ExampleID, &x.ExampleRevisionID, &x.Trial, &x.Status)
			return x, err
		})
	if out == nil {
		out = []store.ResultRef{}
	}
	return out, err
}

func (s *Store) ListResultsForExamples(ctx context.Context, runID uuid.UUID, exampleIDs []uuid.UUID) ([]store.Result, error) {
	out := []store.Result{}
	for _, c := range chunks(exampleIDs, chunkSize) {
		b := &qb{}
		b.where("res.run_id = " + b.arg(runID))
		b.where(inList(b, "res.example_id", c))
		items, err := collect(ctx, s, resultSelect+b.whereSQL()+` ORDER BY res.id`, b.args, scanResult)
		if err != nil {
			return nil, err
		}
		out = append(out, items...)
	}
	return out, nil
}
