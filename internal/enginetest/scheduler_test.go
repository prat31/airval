package enginetest

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/airval/airval/internal/scheduler"
	"github.com/airval/airval/tools/mocktarget/mock"
)

// TestScheduler: an overdue schedule trigger fires exactly once (missed fires are not replayed)
// and its next fire time moves to the future.
func TestScheduler(t *testing.T) {
	h := Start(t, Options{Mock: mock.Config{Accuracy: 0.8}})
	f := h.NewFixture(10, 0.5, nil)
	var tr Obj
	h.Do("POST", "/v1/suites/"+f.SuiteID+"/triggers", Obj{"kind": "schedule", "name": "nightly",
		"config": Obj{"cron": "0 3 * * *", "timezone": "Europe/Berlin"}}, 201, &tr)
	ctx := context.Background()
	id := uuid.MustParse(tr["id"].(string))
	st := h.App.Store
	trig, err := st.GetTrigger(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-26 * time.Hour).UTC().Truncate(time.Second) // two missed fires
	trig.NextFireAt = &past
	if err := st.UpdateTrigger(ctx, &trig); err != nil {
		t.Fatal(err)
	}
	s := scheduler.New(scheduler.Deps{Store: st, Queue: h.App.Queue, Planner: h.App.Planner(), Holder: "test"})
	s.Tick(ctx)
	s.Tick(ctx) // nothing due any more
	var runs struct {
		Items []Obj `json:"items"`
	}
	h.Do("GET", "/v1/runs?suite_id="+f.SuiteID+"&trigger=schedule", nil, 200, &runs)
	if len(runs.Items) != 1 {
		t.Fatalf("scheduled runs: %d", len(runs.Items))
	}
	h.Stream(runs.Items[0]["id"].(string), 30*time.Second)
	trig, _ = st.GetTrigger(ctx, id)
	if trig.NextFireAt == nil || !trig.NextFireAt.After(time.Now()) || trig.LastFiredAt == nil {
		t.Fatalf("next fire not advanced: %v last=%v", trig.NextFireAt, trig.LastFiredAt)
	}
	if n, _ := scheduler.Next("0 3 * * *", "Europe/Berlin", time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)); n.Hour() != 2 {
		t.Fatalf("timezone: %v", n) // 03:00 CET = 02:00 UTC
	}
}
