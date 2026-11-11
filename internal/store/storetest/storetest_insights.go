package storetest

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/prat31/airval/internal/store"
)

func testInsights(t *testing.T, s store.Store) {
	ctx := context.Background()
	f := setup(t, s)
	su := store.Suite{OrgID: f.org.ID, ProjectID: f.proj.ID, Name: "Smoke", Slug: "smoke"}
	must(t, s.CreateSuite(ctx, &su))
	evA, evB := store.NewID(), store.NewID()

	mk := func(status string, gate *bool, cost float64) store.Run {
		r := newRun(t, s, f, func(r *store.Run) {
			r.SuiteID, r.Status, r.GatePassed, r.CostUSD, r.Total = &su.ID, status, gate, cost, 10
			r.TokensIn, r.TokensOut = 100, 50
		})
		return r
	}
	r1 := mk(store.RunSucceeded, ptr(true), 1.5)
	r2 := mk(store.RunFailed, ptr(false), 2.5)
	rq := mk(store.RunRunning, nil, 9) // not finished: excluded from trends and gates
	_ = rq
	must(t, s.UpsertRunAggregates(ctx, []store.RunAggregate{
		{RunID: r1.ID, EvaluatorID: store.NilID, Metric: store.MetricPassRate, N: 10, PassRate: ptr(0.9)},
		{RunID: r2.ID, EvaluatorID: store.NilID, Metric: store.MetricPassRate, N: 10, Mean: ptr(0.7)},
		{RunID: r1.ID, EvaluatorID: evA, Metric: store.MetricScore, N: 10, Mean: ptr(0.8)},
		{RunID: r1.ID, EvaluatorID: evB, Metric: store.MetricScore, N: 10, Mean: ptr(0.6)},
		{RunID: r2.ID, EvaluatorID: evA, Metric: store.MetricScore, N: 10, Mean: ptr(0.5)},
		{RunID: r1.ID, EvaluatorID: evA, Metric: store.MetricScore, SliceKey: "tag:x", N: 3, Mean: ptr(0.1)},
		{RunID: r1.ID, EvaluatorID: store.NilID, Metric: store.MetricLatencyMs, N: 10, P50: ptr(100.0), P95: ptr(300.0)},
	}))

	rows, err := s.TrendRows(ctx, store.TrendQuery{ProjectID: f.proj.ID, Metric: store.TrendPassRate})
	must(t, err)
	eq(t, "pass_rate rows", len(rows), 2)
	eq(t, "oldest first", rows[0].RunID, r1.ID)
	eq(t, "pass_rate value (pass_rate col)", *rows[0].Value, 0.9)
	eq(t, "pass_rate value (mean fallback)", *rows[1].Value, 0.7)
	if rows[0].SuiteID == nil || *rows[0].SuiteID != su.ID || rows[0].At.IsZero() {
		t.Fatalf("trend row: %+v", rows[0])
	}

	rows, err = s.TrendRows(ctx, store.TrendQuery{ProjectID: f.proj.ID, Metric: store.TrendScoreMean})
	must(t, err)
	eq(t, "score rows (slices excluded)", len(rows), 3)
	rows, err = s.TrendRows(ctx, store.TrendQuery{ProjectID: f.proj.ID, Metric: store.TrendScoreMean, EvaluatorID: &evA, SuiteID: &su.ID})
	must(t, err)
	eq(t, "score rows for one evaluator", len(rows), 2)
	rows, err = s.TrendRows(ctx, store.TrendQuery{ProjectID: f.proj.ID, Metric: store.TrendLatencyP95})
	must(t, err)
	eq(t, "latency rows", len(rows), 1)
	eq(t, "p95", *rows[0].Value, 300.0)
	rows, err = s.TrendRows(ctx, store.TrendQuery{ProjectID: f.proj.ID, Metric: store.TrendCostUSD})
	must(t, err)
	eq(t, "cost rows", len(rows), 2)
	eq(t, "cost", *rows[1].Value, 2.5)
	rows, err = s.TrendRows(ctx, store.TrendQuery{ProjectID: f.proj.ID, Metric: store.TrendTokens})
	must(t, err)
	eq(t, "tokens", *rows[0].Value, 150.0)
	future := time.Now().Add(time.Hour)
	rows, err = s.TrendRows(ctx, store.TrendQuery{ProjectID: f.proj.ID, Metric: store.TrendCostUSD, From: &future})
	must(t, err)
	eq(t, "from filter", len(rows), 0)
	rows, err = s.TrendRows(ctx, store.TrendQuery{ProjectID: f.proj.ID, Metric: store.TrendCostUSD, Limit: 1})
	must(t, err)
	eq(t, "limit keeps newest", rows[0].RunID, r2.ID)
	if _, err := s.TrendRows(ctx, store.TrendQuery{ProjectID: f.proj.ID, Metric: "bogus"}); err == nil {
		t.Fatal("unknown metric should fail")
	}

	st, err := s.ProjectStats(ctx, f.proj.ID, time.Now())
	must(t, err)
	eq(t, "runs 7d", st.Runs7d, 3)
	eq(t, "runs prev", st.RunsPrev7d, 0)
	eq(t, "spend", st.Spend7dUSD, 13.0)
	eq(t, "failing gates", len(st.FailingSuiteIDs), 1)
	eq(t, "failing run", st.FailingRunIDs[0], r2.ID)
	st, err = s.ProjectStats(ctx, f.proj.ID, time.Now().Add(10*24*time.Hour))
	must(t, err)
	eq(t, "runs 7d later", st.Runs7d, 0)
	eq(t, "runs prev later", st.RunsPrev7d, 3)
	// A newer passing run clears the failing gate.
	r3 := mk(store.RunSucceeded, ptr(true), 0)
	_ = r3
	st, _ = s.ProjectStats(ctx, f.proj.ID, time.Now())
	eq(t, "failing gates cleared", len(st.FailingSuiteIDs), 0)

	// Agreement pairs.
	d := newDataset(t, s, f)
	exs, err := s.UpsertExamples(ctx, d.ID, []store.ExampleUpsert{
		{ExampleData: store.ExampleData{Input: []byte(`{"q":"a"}`)}}, {ExampleData: store.ExampleData{Input: []byte(`{"q":"b"}`)}}})
	must(t, err)
	var res []store.Result
	var sc []store.Score
	for i, e := range exs {
		r := store.Result{ID: store.NewID(), RunID: r1.ID, ExampleID: e.ID, ExampleRevisionID: e.RevisionID, Status: store.ResultPassed}
		res = append(res, r)
		sc = append(sc, store.Score{ResultID: r.ID, EvaluatorID: evA, RunID: r1.ID, Value: ptr(float64(i)), Passed: ptr(i == 0)})
	}
	must(t, s.InsertResults(ctx, res, sc))
	ann := func(result uuid.UUID, ev *uuid.UUID, passed bool, v float64) {
		a := store.Annotation{OrgID: f.org.ID, ProjectID: f.proj.ID, ResultID: result, EvaluatorID: ev, Passed: &passed, Value: &v}
		must(t, s.CreateAnnotation(ctx, &a))
	}
	ann(res[0].ID, &evA, true, 1)
	ann(res[0].ID, nil, false, 0)
	ann(res[1].ID, &evB, true, 1) // another evaluator's annotation: ignored
	pairs, err := s.AgreementPairs(ctx, evA, 0)
	must(t, err)
	eq(t, "pairs (one scored result with annotations)", len(pairs), 1)
	eq(t, "annotations", pairs[0].AnnotationCt, 2)
	eq(t, "human verdicts", len(pairs[0].HumanPassed), 2)
	eq(t, "judge passed", *pairs[0].JudgePassed, true)
}
