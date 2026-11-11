package storetest

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/prat31/airval/internal/store"
)

func testEngine(t *testing.T, s store.Store) {
	ctx := context.Background()
	_, run, exs, evA, evB := seedRun(t, s)

	// Shards: completion is idempotent and counts down to zero.
	must(t, s.CreateRunShards(ctx, run.ID, []int{4, 4, 2}))
	left, err := s.CompleteRunShard(ctx, run.ID, 1, json.RawMessage(`{"attempts":4}`))
	must(t, err)
	eq(t, "remaining after 1", left, 2)
	left, _ = s.CompleteRunShard(ctx, run.ID, 1, json.RawMessage(`{"attempts":99}`))
	eq(t, "idempotent", left, 2)
	_, _ = s.CompleteRunShard(ctx, run.ID, 0, nil)
	left, _ = s.CompleteRunShard(ctx, run.ID, 2, nil)
	eq(t, "remaining after all", left, 0)
	shards, err := s.ListRunShards(ctx, run.ID)
	must(t, err)
	eq(t, "shards", len(shards), 3)
	eq(t, "shard items", shards[2].Items, 2)
	var st struct{ Attempts int }
	must(t, json.Unmarshal(shards[1].Stats, &st))
	eq(t, "first stats kept", st.Attempts, 4)
	if shards[0].DoneAt == nil {
		t.Fatal("done_at not set")
	}

	// Counters.
	run.Total = 10
	must(t, s.UpdateRun(ctx, &run))
	c, err := s.IncrRunProgress(ctx, run.ID, store.RunProgress{Completed: 3, Failed: 1, CostUSD: 0.5, TokensIn: 7})
	must(t, err)
	c, err = s.IncrRunProgress(ctx, run.ID, store.RunProgress{Completed: 2, Errored: 1, TokensOut: 3})
	must(t, err)
	eq(t, "total", c.Total, 10)
	eq(t, "completed", c.Completed, 5)
	eq(t, "failed", c.Failed, 1)
	eq(t, "errored", c.Errored, 1)
	eq(t, "tokens in", c.TokensIn, int64(7))
	eq(t, "tokens out", c.TokensOut, int64(3))

	must(t, s.SetRunError(ctx, run.ID, "stopped early"))
	got, _ := s.GetRun(ctx, run.ID)
	eq(t, "error", got.Error, "stopped early")
	eq(t, "counters kept", got.Completed, 5)

	// Results lookups.
	rs, ss := makeResults(run, exs, evA, evB, 0)
	must(t, s.InsertResults(ctx, rs, ss))
	refs, err := s.ListRunResultRefs(ctx, run.ID, []string{store.ResultFailed})
	must(t, err)
	eq(t, "failed refs", len(refs), 4)
	all, _ := s.ListRunResultRefs(ctx, run.ID, nil)
	eq(t, "all refs", len(all), 10)
	eq(t, "ref revision", all[0].ExampleRevisionID != uuid.Nil, true)
	byEx, err := s.ListResultsForExamples(ctx, run.ID, []uuid.UUID{exs[0].ID, exs[1].ID, uuid.New()})
	must(t, err)
	eq(t, "results for examples", len(byEx), 2)

	// Finish: status applies while running; summary/gate are written.
	ok, _ := s.TransitionRun(ctx, run.ID, []string{store.RunQueued}, store.RunRunning, time.Now())
	eq(t, "running", ok, true)
	gp := false
	status, err := s.FinishRun(ctx, run.ID, store.RunFinish{Status: store.RunFailed, Completed: 10, Failed: 4,
		Summary: json.RawMessage(`{"pass_rate":0.6}`), GatePassed: &gp, FinishedAt: time.Now()})
	must(t, err)
	eq(t, "finished status", status, store.RunFailed)
	got, _ = s.GetRun(ctx, run.ID)
	eq(t, "completed final", got.Completed, 10)
	eq(t, "error kept", got.Error, "stopped early")
	if got.GatePassed == nil || *got.GatePassed || got.FinishedAt == nil || len(got.Summary) == 0 {
		t.Fatalf("finish not applied: %+v", got)
	}
	// A canceled run keeps its status.
	r2 := store.Run{OrgID: run.OrgID, ProjectID: run.ProjectID, Status: store.RunCanceled}
	must(t, s.CreateRun(ctx, &r2))
	status, err = s.FinishRun(ctx, r2.ID, store.RunFinish{Status: store.RunSucceeded, Error: "canceled by user"})
	must(t, err)
	eq(t, "canceled kept", status, store.RunCanceled)
	if _, err := s.FinishRun(ctx, uuid.New(), store.RunFinish{Status: store.RunSucceeded}); err == nil {
		t.Fatal("want not found")
	}
}
