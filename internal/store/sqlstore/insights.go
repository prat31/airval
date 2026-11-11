package sqlstore

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/airval/airval/internal/store"
)

const finishedRuns = `('succeeded','failed')`

func (s *Store) TrendRows(ctx context.Context, q store.TrendQuery) ([]store.TrendRow, error) {
	limit := q.Limit
	if limit <= 0 {
		limit = store.DefaultTrendRows
	}
	b := &qb{}
	var from string
	var valueCol string
	switch q.Metric {
	case store.TrendCostUSD:
		from, valueCol = "runs r", "r.cost_usd"
	case store.TrendTokens:
		from, valueCol = "runs r", "(r.tokens_in + r.tokens_out)"
	default:
		metric, evalNil := "", true
		switch q.Metric {
		case store.TrendPassRate:
			metric, valueCol = store.MetricPassRate, "COALESCE(a.pass_rate, a.mean)"
		case store.TrendScoreMean:
			metric, valueCol, evalNil = store.MetricScore, "a.mean", false
		case store.TrendLatencyP50:
			metric, valueCol = store.MetricLatencyMs, "a.p50"
		case store.TrendLatencyP95:
			metric, valueCol = store.MetricLatencyMs, "a.p95"
		default:
			return nil, fmt.Errorf("sqlstore: unknown trend metric %q", q.Metric)
		}
		from = "runs r JOIN run_aggregates a ON a.run_id = r.id AND a.metric = " + b.arg(metric) + " AND a.slice_key = ''"
		b.where(valueCol + " IS NOT NULL")
		if evalNil {
			b.where("a.evaluator_id = " + b.arg(store.NilID))
		} else if q.EvaluatorID != nil {
			b.where("a.evaluator_id = " + b.arg(*q.EvaluatorID))
		}
	}
	evalCol, nCol := "a.evaluator_id", "a.n"
	if from == "runs r" {
		evalCol, nCol = b.arg(store.NilID), "r.total"
	}
	b.where("r.project_id = " + b.arg(q.ProjectID))
	b.where("r.status IN " + finishedRuns)
	if q.SuiteID != nil {
		b.where("r.suite_id = " + b.arg(*q.SuiteID))
	}
	if q.TargetID != nil {
		b.where("r.target_id = " + b.arg(*q.TargetID))
	}
	if q.From != nil {
		b.where("r.created_at >= " + b.arg(q.From.UTC()))
	}
	if q.To != nil {
		b.where("r.created_at < " + b.arg(q.To.UTC()))
	}
	query := fmt.Sprintf(`SELECT r.id, r.suite_id, r.target_id, r.created_at, r.finished_at, %s, %s, %s FROM %s%s ORDER BY r.id DESC LIMIT %d`,
		evalCol, valueCol, nCol, from, b.whereSQL(), limit)
	rows, err := collect(ctx, s, query, b.args, func(r scanner) (store.TrendRow, error) {
		var t store.TrendRow
		var created time.Time
		var finished *time.Time
		err := r.Scan(&t.RunID, &t.SuiteID, &t.TargetID, &created, &finished, &t.EvaluatorID, &t.Value, &t.N)
		t.At = created
		if finished != nil {
			t.At = *finished
		}
		return t, err
	})
	if err != nil {
		return nil, err
	}
	// Newest-first from the query (so LIMIT keeps the most recent); return oldest first.
	for i, j := 0, len(rows)-1; i < j; i, j = i+1, j-1 {
		rows[i], rows[j] = rows[j], rows[i]
	}
	return rows, nil
}

func (s *Store) ProjectStats(ctx context.Context, projectID uuid.UUID, now time.Time) (store.ProjectStats, error) {
	var st store.ProjectStats
	now = now.UTC()
	since7, since14 := now.Add(-7*24*time.Hour), now.Add(-14*24*time.Hour)
	err := s.q.QueryRowContext(ctx, `SELECT
		COALESCE(SUM(CASE WHEN created_at >= $2 THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN created_at >= $2 THEN 0 ELSE 1 END), 0),
		COALESCE(SUM(CASE WHEN created_at >= $2 THEN cost_usd ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN created_at >= $2 THEN 0 ELSE cost_usd END), 0)
		FROM runs WHERE project_id = $1 AND created_at >= $3`, projectID, since7, since14).
		Scan(&st.Runs7d, &st.RunsPrev7d, &st.Spend7dUSD, &st.SpendPrev7dUSD)
	if err != nil {
		return st, s.mapErr(err)
	}
	type pair struct{ suite, run uuid.UUID }
	pairs, err := collect(ctx, s, `SELECT r.suite_id, r.id FROM runs r
		WHERE r.project_id = $1 AND r.suite_id IS NOT NULL AND r.gate_passed = $2 AND r.status IN `+finishedRuns+`
		AND r.id = (SELECT r2.id FROM runs r2 WHERE r2.suite_id = r.suite_id AND r2.status IN `+finishedRuns+`
			ORDER BY r2.id DESC LIMIT 1)
		ORDER BY r.id DESC`, []any{projectID, false}, func(r scanner) (pair, error) {
		var p pair
		return p, r.Scan(&p.suite, &p.run)
	})
	if err != nil {
		return st, err
	}
	for _, p := range pairs {
		st.FailingSuiteIDs = append(st.FailingSuiteIDs, p.suite)
		st.FailingRunIDs = append(st.FailingRunIDs, p.run)
	}
	return st, nil
}

func (s *Store) AgreementPairs(ctx context.Context, evaluatorID uuid.UUID, limit int) ([]store.AgreementPair, error) {
	if limit <= 0 {
		limit = 200000
	}
	type row struct {
		result uuid.UUID
		jv     *float64
		jp     *bool
		hv     *float64
		hp     *bool
	}
	rows, err := collect(ctx, s, `SELECT s.result_id, s.value, s.passed, a.value, a.passed
		FROM scores s JOIN annotations a ON a.result_id = s.result_id AND (a.evaluator_id = $1 OR a.evaluator_id IS NULL)
		WHERE s.evaluator_id = $1 ORDER BY s.result_id, a.id LIMIT `+fmt.Sprint(limit), []any{evaluatorID},
		func(r scanner) (row, error) {
			var x row
			return x, r.Scan(&x.result, &x.jv, &x.jp, &x.hv, &x.hp)
		})
	if err != nil {
		return nil, err
	}
	var out []store.AgreementPair
	for _, r := range rows {
		if n := len(out); n == 0 || out[n-1].ResultID != r.result {
			out = append(out, store.AgreementPair{ResultID: r.result, JudgeValue: r.jv, JudgePassed: r.jp})
		}
		p := &out[len(out)-1]
		p.AnnotationCt++
		if r.hv != nil {
			p.HumanValues = append(p.HumanValues, *r.hv)
		}
		if r.hp != nil {
			p.HumanPassed = append(p.HumanPassed, *r.hp)
		}
	}
	return out, nil
}
