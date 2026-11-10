package sqlstore

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/klauspost/compress/zstd"

	"github.com/airval/airval/internal/store"
)

// Outputs larger than this are stored zstd-compressed in output_z.
const compressThreshold = 8 << 10

var (
	zenc, _ = zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedFastest))
	zdec, _ = zstd.NewReader(nil)
)

func encodeOutput(s string) (string, []byte) {
	if len(s) <= compressThreshold {
		return s, nil
	}
	return "", zenc.EncodeAll([]byte(s), nil)
}

func decodeOutput(text string, z []byte) (string, error) {
	if len(z) == 0 {
		return text, nil
	}
	b, err := zdec.DecodeAll(z, nil)
	if err != nil {
		return "", fmt.Errorf("sqlstore: decompress output: %w", err)
	}
	return string(b), nil
}

var resultCols = []string{"id", "run_id", "example_id", "example_revision_id", "trial", "status", "output", "output_z",
	"output_json", "context", "tool_calls", "trace", "transcript", "request", "response", "latency_ms", "ttft_ms",
	"tokens_in", "tokens_out", "cost_usd", "error", "cached", "created_at"}

var scoreCols = []string{"result_id", "evaluator_id", "run_id", "value", "label", "passed", "reason", "meta",
	"latency_ms", "cost_usd", "created_at"}

func resultArgs(r *store.Result) []any {
	out, z := encodeOutput(r.Output)
	return []any{r.ID, r.RunID, r.ExampleID, r.ExampleRevisionID, r.Trial, r.Status, out, z,
		jsonArg(r.OutputJSON, ""), jsonArg(r.Context, ""), jsonArg(r.ToolCalls, ""), jsonArg(r.Trace, ""),
		jsonArg(r.Transcript, ""), jsonArg(r.Request, ""), jsonArg(r.Response, ""), r.LatencyMs, r.TTFTMs,
		r.TokensIn, r.TokensOut, r.CostUSD, r.Error, r.Cached, r.CreatedAt}
}

func scoreArgs(c *store.Score) []any {
	return []any{c.ResultID, c.EvaluatorID, c.RunID, c.Value, c.Label, c.Passed, c.Reason, jsonArg(c.Meta, ""),
		c.LatencyMs, c.CostUSD, c.CreatedAt}
}

// valuesSQL renders "($1,..,$k),($k+1,...)" for n rows of k columns. Cached per shape.
var valuesCache sync.Map

func valuesSQL(rows, cols int) string {
	key := [2]int{rows, cols}
	if v, ok := valuesCache.Load(key); ok {
		return v.(string)
	}
	var sb strings.Builder
	n := 1
	for i := 0; i < rows; i++ {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteByte('(')
		for j := 0; j < cols; j++ {
			if j > 0 {
				sb.WriteByte(',')
			}
			fmt.Fprintf(&sb, "$%d", n)
			n++
		}
		sb.WriteByte(')')
	}
	s := sb.String()
	valuesCache.Store(key, s)
	return s
}

const (
	resultBatch = 200 // 23 cols → 4600 params per statement
	scoreBatch  = 500 // 11 cols → 5500 params
)

func prepResults(results []store.Result, scores []store.Score) {
	t := now()
	for i := range results {
		if results[i].ID == uuid.Nil {
			results[i].ID = store.NewID()
		}
		if results[i].CreatedAt.IsZero() {
			results[i].CreatedAt = t
		}
	}
	for i := range scores {
		if scores[i].CreatedAt.IsZero() {
			scores[i].CreatedAt = t
		}
	}
}

func (s *Store) InsertResults(ctx context.Context, results []store.Result, scores []store.Score) error {
	if len(results) == 0 && len(scores) == 0 {
		return nil
	}
	prepResults(results, scores)
	if ok, err := s.d.bulkInsertResults(ctx, s, results, scores); err != nil || ok {
		return err
	}
	return s.withTx(ctx, func(tx *Store) error {
		for _, c := range chunks(results, resultBatch) {
			if err := tx.insertResultChunk(ctx, c); err != nil {
				return err
			}
		}
		return tx.upsertScores(ctx, scores)
	})
}

// insertResultChunk inserts rows, replacing any existing (run_id, example_id, trial) duplicates.
func (s *Store) insertResultChunk(ctx context.Context, c []store.Result) error {
	args := make([]any, 0, len(c)*len(resultCols))
	for i := range c {
		args = append(args, resultArgs(&c[i])...)
	}
	q := `INSERT INTO results (` + strings.Join(resultCols, ",") + `) VALUES ` + valuesSQL(len(c), len(resultCols)) +
		` ON CONFLICT (run_id, example_id, trial) DO NOTHING RETURNING id`
	inserted, err := collect(ctx, s, q, args, func(r scanner) (uuid.UUID, error) {
		var id uuid.UUID
		return id, r.Scan(&id)
	})
	if err != nil {
		return err
	}
	if len(inserted) == len(c) {
		return nil
	}
	// Duplicates (re-delivered work): replace them one by one.
	got := make(map[uuid.UUID]struct{}, len(inserted))
	for _, id := range inserted {
		got[id] = struct{}{}
	}
	for i := range c {
		if _, ok := got[c[i].ID]; ok {
			continue
		}
		if err := s.exec(ctx, `DELETE FROM results WHERE run_id = $1 AND example_id = $2 AND trial = $3`,
			c[i].RunID, c[i].ExampleID, c[i].Trial); err != nil {
			return err
		}
		if err := s.exec(ctx, `INSERT INTO results (`+strings.Join(resultCols, ",")+`) VALUES `+
			valuesSQL(1, len(resultCols)), resultArgs(&c[i])...); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) UpsertScores(ctx context.Context, scores []store.Score) error {
	prepResults(nil, scores)
	return s.withTx(ctx, func(tx *Store) error { return tx.upsertScores(ctx, scores) })
}

func (s *Store) upsertScores(ctx context.Context, scores []store.Score) error {
	for _, c := range chunks(scores, scoreBatch) {
		args := make([]any, 0, len(c)*len(scoreCols))
		for i := range c {
			args = append(args, scoreArgs(&c[i])...)
		}
		q := `INSERT INTO scores (` + strings.Join(scoreCols, ",") + `) VALUES ` + valuesSQL(len(c), len(scoreCols)) +
			` ON CONFLICT (result_id, evaluator_id) DO UPDATE SET value = excluded.value, label = excluded.label,
			passed = excluded.passed, reason = excluded.reason, meta = excluded.meta, latency_ms = excluded.latency_ms,
			cost_usd = excluded.cost_usd, created_at = excluded.created_at`
		if err := s.exec(ctx, q, args...); err != nil {
			return err
		}
	}
	return nil
}

const resultSelect = `SELECT res.id, res.run_id, res.example_id, res.example_revision_id, res.trial, res.status,
	res.output, res.output_z, res.output_json, res.context, res.tool_calls, res.trace, res.transcript, res.request,
	res.response, res.latency_ms, res.ttft_ms, res.tokens_in, res.tokens_out, res.cost_usd, res.error, res.cached,
	res.created_at, r.tags
	FROM results res LEFT JOIN example_revisions r ON r.id = res.example_revision_id`

func scanResult(r scanner) (store.Result, error) {
	var (
		x store.Result
		z []byte
	)
	err := r.Scan(&x.ID, &x.RunID, &x.ExampleID, &x.ExampleRevisionID, &x.Trial, &x.Status, &x.Output, &z,
		rawJSON{&x.OutputJSON}, rawJSON{&x.Context}, rawJSON{&x.ToolCalls}, rawJSON{&x.Trace}, rawJSON{&x.Transcript},
		rawJSON{&x.Request}, rawJSON{&x.Response}, &x.LatencyMs, &x.TTFTMs, &x.TokensIn, &x.TokensOut, &x.CostUSD,
		&x.Error, &x.Cached, &x.CreatedAt, jsonCol(&x.Tags))
	if err != nil {
		return x, err
	}
	if x.Tags == nil {
		x.Tags = []string{}
	}
	x.Scores = []store.Score{}
	x.Output, err = decodeOutput(x.Output, z)
	return x, err
}

const scoreSelect = `SELECT result_id, evaluator_id, run_id, value, label, passed, reason, meta, latency_ms, cost_usd,
	created_at FROM scores`

func scanScore(r scanner) (store.Score, error) {
	var c store.Score
	err := r.Scan(&c.ResultID, &c.EvaluatorID, &c.RunID, &c.Value, &c.Label, &c.Passed, &c.Reason, rawJSON{&c.Meta},
		&c.LatencyMs, &c.CostUSD, &c.CreatedAt)
	return c, err
}

// attachScores loads scores for the given results (one query per chunk).
func (s *Store) attachScores(ctx context.Context, results []store.Result) error {
	if len(results) == 0 {
		return nil
	}
	idx := make(map[uuid.UUID]int, len(results))
	ids := make([]uuid.UUID, len(results))
	for i := range results {
		idx[results[i].ID] = i
		ids[i] = results[i].ID
	}
	for _, c := range chunks(ids, chunkSize) {
		b := &qb{}
		b.where(inList(b, "result_id", c))
		scores, err := collect(ctx, s, scoreSelect+b.whereSQL()+` ORDER BY result_id, evaluator_id`, b.args, scanScore)
		if err != nil {
			return err
		}
		for _, sc := range scores {
			i := idx[sc.ResultID]
			results[i].Scores = append(results[i].Scores, sc)
		}
	}
	return nil
}

func (s *Store) GetResult(ctx context.Context, id uuid.UUID) (store.Result, error) {
	x, err := one(ctx, s, resultSelect+` WHERE res.id = $1`, []any{id}, scanResult)
	if err != nil {
		return x, err
	}
	rs := []store.Result{x}
	err = s.attachScores(ctx, rs)
	return rs[0], err
}

func (s *Store) ListResults(ctx context.Context, f store.ResultFilter, p store.ListParams) (store.Page[store.Result], error) {
	b := &qb{}
	b.where("res.run_id = " + b.arg(f.RunID))
	if len(f.Statuses) > 0 {
		b.where(inList(b, "res.status", f.Statuses))
	}
	if f.ExampleID != nil {
		b.where("res.example_id = " + b.arg(*f.ExampleID))
	}
	if f.Trial != nil {
		b.where("res.trial = " + b.arg(*f.Trial))
	}
	if f.EvaluatorID != nil && (f.Passed != nil || f.ScoreMin != nil || f.ScoreMax != nil) {
		conds := []string{"sc.result_id = res.id", "sc.evaluator_id = " + b.arg(*f.EvaluatorID)}
		if f.Passed != nil {
			conds = append(conds, "sc.passed = "+b.arg(*f.Passed))
		}
		if f.ScoreMin != nil {
			conds = append(conds, "sc.value >= "+b.arg(*f.ScoreMin))
		}
		if f.ScoreMax != nil {
			conds = append(conds, "sc.value <= "+b.arg(*f.ScoreMax))
		}
		b.where("EXISTS (SELECT 1 FROM scores sc WHERE " + strings.Join(conds, " AND ") + ")")
	}
	if f.Tag != "" {
		b.where(s.d.jsonArrayContains("r.tags", b.arg(f.Tag)))
	}
	if f.Search != "" {
		ph := b.arg("%" + f.Search + "%")
		b.where("(res.output " + s.d.like() + " " + ph + " OR " + s.d.jsonText("r.input") + " " + s.d.like() + " " + ph +
			" OR res.error " + s.d.like() + " " + ph + ")")
	}
	tail, limit, err := b.paginate("res.id", p, false)
	if err != nil {
		return store.Page[store.Result]{}, err
	}
	items, err := collect(ctx, s, resultSelect+b.whereSQL()+tail, b.args, scanResult)
	if err != nil {
		return store.Page[store.Result]{}, err
	}
	page := finishPage(items, limit, func(x store.Result) uuid.UUID { return x.ID })
	return page, s.attachScores(ctx, page.Items)
}

// IterateRunResults pages through a run's results in chunks, loading each chunk's scores with
// one range query (merge by result id). It works inside and outside transactions.
func (s *Store) IterateRunResults(ctx context.Context, runID uuid.UUID, fn func(store.Result) error) error {
	const chunk = 1000
	var after *uuid.UUID
	for {
		b := &qb{}
		b.where("res.run_id = " + b.arg(runID))
		if after != nil {
			b.where("res.id > " + b.arg(*after))
		}
		items, err := collect(ctx, s, resultSelect+b.whereSQL()+fmt.Sprintf(" ORDER BY res.id LIMIT %d", chunk), b.args, scanResult)
		if err != nil {
			return err
		}
		if len(items) == 0 {
			return nil
		}
		first, last := items[0].ID, items[len(items)-1].ID
		scores, err := collect(ctx, s, scoreSelect+` WHERE result_id >= $1 AND result_id <= $2 AND run_id = $3
			ORDER BY result_id, evaluator_id`, []any{first, last, runID}, scanScore)
		if err != nil {
			return err
		}
		j := 0
		for i := range items {
			for j < len(scores) && scores[j].ResultID.String() < items[i].ID.String() {
				j++
			}
			for j < len(scores) && scores[j].ResultID == items[i].ID {
				items[i].Scores = append(items[i].Scores, scores[j])
				j++
			}
			if err := fn(items[i]); err != nil {
				return err
			}
		}
		if len(items) < chunk {
			return nil
		}
		after = &last
	}
}

func (s *Store) CountResults(ctx context.Context, runID uuid.UUID) (map[string]int, error) {
	type kv struct {
		k string
		n int
	}
	rows, err := collect(ctx, s, `SELECT status, count(*) FROM results WHERE run_id = $1 GROUP BY status`, []any{runID},
		func(r scanner) (kv, error) {
			var x kv
			return x, r.Scan(&x.k, &x.n)
		})
	if err != nil {
		return nil, err
	}
	out := make(map[string]int, len(rows))
	for _, x := range rows {
		out[x.k] = x.n
	}
	return out, nil
}

// ---- aggregates ----

var aggCols = []string{"run_id", "evaluator_id", "metric", "slice_key", "n", "mean", "min", "max", "p50", "p90", "p95",
	"p99", "stddev", "sum", "pass_rate", "histogram", "updated_at"}

func (s *Store) UpsertRunAggregates(ctx context.Context, aggs []store.RunAggregate) error {
	t := now()
	return s.withTx(ctx, func(tx *Store) error {
		for _, c := range chunks(aggs, 300) {
			args := make([]any, 0, len(c)*len(aggCols))
			for i := range c {
				a := &c[i]
				a.UpdatedAt = t
				args = append(args, a.RunID, a.EvaluatorID, a.Metric, a.SliceKey, a.N, a.Mean, a.Min, a.Max, a.P50, a.P90,
					a.P95, a.P99, a.Stddev, a.Sum, a.PassRate, jsonArg(a.Histogram, ""), a.UpdatedAt)
			}
			set := make([]string, 0, len(aggCols)-4)
			for _, col := range aggCols[4:] {
				set = append(set, col+" = excluded."+col)
			}
			q := `INSERT INTO run_aggregates (` + strings.Join(aggCols, ",") + `) VALUES ` + valuesSQL(len(c), len(aggCols)) +
				` ON CONFLICT (run_id, evaluator_id, metric, slice_key) DO UPDATE SET ` + strings.Join(set, ", ")
			if err := tx.exec(ctx, q, args...); err != nil {
				return err
			}
		}
		return nil
	})
}

func scanAgg(r scanner) (store.RunAggregate, error) {
	var a store.RunAggregate
	err := r.Scan(&a.RunID, &a.EvaluatorID, &a.Metric, &a.SliceKey, &a.N, &a.Mean, &a.Min, &a.Max, &a.P50, &a.P90,
		&a.P95, &a.P99, &a.Stddev, &a.Sum, &a.PassRate, rawJSON{&a.Histogram}, &a.UpdatedAt)
	return a, err
}

func (s *Store) ListRunAggregates(ctx context.Context, runID uuid.UUID) ([]store.RunAggregate, error) {
	return s.ListRunAggregatesForRuns(ctx, []uuid.UUID{runID}, "", "")
}

func (s *Store) ListRunAggregatesForRuns(ctx context.Context, runIDs []uuid.UUID, metric, sliceKey string) ([]store.RunAggregate, error) {
	out := []store.RunAggregate{}
	for _, c := range chunks(runIDs, chunkSize) {
		b := &qb{}
		b.where(inList(b, "run_id", c))
		if metric != "" {
			b.where("metric = " + b.arg(metric))
		}
		if sliceKey != "" {
			b.where("slice_key = " + b.arg(sliceKey))
		}
		rows, err := collect(ctx, s, `SELECT `+strings.Join(aggCols, ", ")+` FROM run_aggregates`+b.whereSQL()+
			` ORDER BY run_id, evaluator_id, metric, slice_key`, b.args, scanAgg)
		if err != nil {
			return nil, err
		}
		out = append(out, rows...)
	}
	return out, nil
}

// ---- cache ----

func (s *Store) CacheGet(ctx context.Context, key string) ([]byte, bool, error) {
	var v []byte
	err := s.q.QueryRowContext(ctx, `SELECT value FROM cache_entries WHERE key = $1 AND (expires_at IS NULL OR expires_at > $2)`,
		key, now()).Scan(&v)
	if err != nil {
		err = s.mapErr(err)
		if err == store.ErrNotFound {
			return nil, false, nil
		}
		return nil, false, err
	}
	return v, true, nil
}

func (s *Store) CachePut(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	t := now()
	var exp *time.Time
	if ttl > 0 {
		e := t.Add(ttl)
		exp = &e
	}
	if value == nil {
		value = []byte{}
	}
	return s.exec(ctx, `INSERT INTO cache_entries (key, value, created_at, expires_at) VALUES ($1,$2,$3,$4)
		ON CONFLICT (key) DO UPDATE SET value = excluded.value, created_at = excluded.created_at, expires_at = excluded.expires_at`,
		key, value, t, exp)
}

func (s *Store) CachePurgeExpired(ctx context.Context, at time.Time) (int, error) {
	res, err := s.q.ExecContext(ctx, `DELETE FROM cache_entries WHERE expires_at IS NOT NULL AND expires_at <= $1`, at.UTC())
	if err != nil {
		return 0, s.mapErr(err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}
