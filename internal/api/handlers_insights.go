package api

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"

	"github.com/airval/airval/internal/api/gen"
	"github.com/airval/airval/internal/store"
)

// GetTrends returns a time series per (suite, target[, evaluator]) from run aggregates, built
// from one indexed query (store.TrendRows) and bucketed in memory.
func (s *Server) GetTrends(ctx context.Context, req gen.GetTrendsRequestObject) (gen.GetTrendsResponseObject, error) {
	p, err := s.project(ctx, req.Project, store.RoleViewer)
	if err != nil {
		return nil, err
	}
	metric := store.TrendPassRate
	if req.Params.Metric != nil {
		metric = string(*req.Params.Metric)
	}
	bucket := "run"
	if req.Params.Bucket != nil {
		bucket = string(*req.Params.Bucket)
	}
	if !set("run", "hour", "day", "week")[bucket] {
		return nil, fieldError("bucket", "bucket must be run, hour, day or week")
	}
	if f, t := req.Params.From, req.Params.To; f != nil && t != nil && !t.After(*f) {
		return nil, fieldError("to", "to must be after from")
	}
	rows, err := s.Store.TrendRows(ctx, store.TrendQuery{ProjectID: p.ID, SuiteID: req.Params.SuiteId, TargetID: req.Params.TargetId,
		EvaluatorID: req.Params.EvaluatorId, Metric: metric, From: req.Params.From, To: req.Params.To})
	if err != nil {
		return nil, err
	}

	type key struct{ suite, target, eval uuid.UUID }
	keyOf := func(r store.TrendRow) key {
		k := key{eval: r.EvaluatorID}
		if r.SuiteID != nil {
			k.suite = *r.SuiteID
		}
		if r.TargetID != nil {
			k.target = *r.TargetID
		}
		return k
	}
	groups := map[key][]store.TrendRow{}
	var order []key
	for _, r := range rows {
		k := keyOf(r)
		if _, ok := groups[k]; !ok {
			order = append(order, k)
		}
		groups[k] = append(groups[k], r)
	}

	// Labels.
	suiteNames, targetNames, evalNames := map[uuid.UUID]string{}, map[uuid.UUID]string{}, map[uuid.UUID]string{}
	var evalIDs []uuid.UUID
	for _, k := range order {
		if k.suite != uuid.Nil {
			if _, ok := suiteNames[k.suite]; !ok {
				if x, err := s.Store.GetSuite(ctx, k.suite); err == nil {
					suiteNames[k.suite] = x.Name
				}
			}
		}
		if k.target != uuid.Nil {
			if _, ok := targetNames[k.target]; !ok {
				if x, err := s.Store.GetTarget(ctx, k.target); err == nil {
					targetNames[k.target] = x.Name
				}
			}
		}
		if k.eval != uuid.Nil {
			evalIDs = append(evalIDs, k.eval)
		}
	}
	if len(evalIDs) > 0 {
		if evs, err := s.Store.GetEvaluators(ctx, evalIDs); err == nil {
			for _, e := range evs {
				evalNames[e.ID] = e.Name
			}
		}
	}

	out := gen.Trend{Metric: gen.TrendMetric(metric), Series: make([]gen.TrendSeries, 0, len(order))}
	for _, k := range order {
		ser := gen.TrendSeries{Key: fmt.Sprintf("%s:%s:%s", k.suite, k.target, k.eval), Points: bucketPoints(groups[k], bucket, metric)}
		label := ""
		add := func(part string) {
			if part == "" {
				return
			}
			if label != "" {
				label += " · "
			}
			label += part
		}
		if k.suite != uuid.Nil {
			ser.SuiteId = ptr(k.suite)
			add(suiteNames[k.suite])
		}
		if k.target != uuid.Nil {
			ser.TargetId = ptr(k.target)
			add(targetNames[k.target])
		}
		if k.eval != uuid.Nil {
			ser.EvaluatorId = ptr(k.eval)
			add(evalNames[k.eval])
		}
		if label == "" {
			label = "All runs"
		}
		ser.Label = label
		out.Series = append(out.Series, ser)
	}
	sort.SliceStable(out.Series, func(i, j int) bool { return out.Series[i].Label < out.Series[j].Label })
	return gen.GetTrends200JSONResponse(out), nil
}

// bucketPoints turns rows (oldest first) into points: one per run, or aggregated per time bucket
// (sums for cost/tokens, run-size-weighted means otherwise).
func bucketPoints(rows []store.TrendRow, bucket, metric string) []gen.TrendPoint {
	if bucket == "run" {
		pts := make([]gen.TrendPoint, len(rows))
		for i, r := range rows {
			pts[i] = gen.TrendPoint{T: r.At, Value: r.Value, N: ptr(r.N), RunId: ptr(r.RunID)}
		}
		return pts
	}
	sum := metric == store.TrendCostUSD || metric == store.TrendTokens
	type acc struct {
		start    time.Time
		v, w     float64
		n        int
		lastRun  uuid.UUID
		anyValue bool
	}
	var accs []*acc
	byStart := map[time.Time]*acc{}
	for _, r := range rows {
		if r.Value == nil {
			continue
		}
		st := bucketStart(r.At, bucket)
		a := byStart[st]
		if a == nil {
			a = &acc{start: st}
			byStart[st] = a
			accs = append(accs, a)
		}
		w := float64(max(r.N, 1))
		if sum {
			a.v += *r.Value
		} else {
			a.v += *r.Value * w
			a.w += w
		}
		a.n += r.N
		a.lastRun = r.RunID
	}
	sort.Slice(accs, func(i, j int) bool { return accs[i].start.Before(accs[j].start) })
	pts := make([]gen.TrendPoint, len(accs))
	for i, a := range accs {
		v := a.v
		if !sum && a.w > 0 {
			v = a.v / a.w
		}
		pts[i] = gen.TrendPoint{T: a.start, Value: &v, N: ptr(a.n), RunId: ptr(a.lastRun)}
	}
	return pts
}

func bucketStart(t time.Time, bucket string) time.Time {
	t = t.UTC()
	switch bucket {
	case "hour":
		return t.Truncate(time.Hour)
	case "week":
		d := t.Truncate(24 * time.Hour)
		return d.AddDate(0, 0, -((int(d.Weekday()) + 6) % 7)) // Monday
	}
	return t.Truncate(24 * time.Hour)
}

// GetProjectOverview returns the stat tiles of the project page in one call.
func (s *Server) GetProjectOverview(ctx context.Context, req gen.GetProjectOverviewRequestObject) (gen.GetProjectOverviewResponseObject, error) {
	p, err := s.project(ctx, req.Project, store.RoleViewer)
	if err != nil {
		return nil, err
	}
	stats, err := s.Store.ProjectStats(ctx, p.ID, time.Now())
	if err != nil {
		return nil, err
	}
	out := gen.ProjectOverview{Runs7d: stats.Runs7d, RunsPrev7d: stats.RunsPrev7d, Spend7dUsd: stats.Spend7dUSD,
		SpendPrev7dUsd: ptr(stats.SpendPrev7dUSD), FailingGates: len(stats.FailingSuiteIDs)}

	finished := []string{store.RunSucceeded, store.RunFailed}
	pg, err := s.Store.ListRuns(ctx, store.RunFilter{ProjectID: &p.ID, Statuses: finished}, store.ListParams{Limit: 1})
	if err != nil {
		return nil, err
	}
	if len(pg.Items) > 0 {
		latest := pg.Items[0]
		lr := toRun(latest)
		out.LatestRun = &lr
		// Previous finished run of the same suite (any suite when the run has none).
		pf := store.RunFilter{ProjectID: &p.ID, Statuses: finished, SuiteID: latest.SuiteID}
		prevPg, err := s.Store.ListRuns(ctx, pf, store.ListParams{Limit: 2})
		if err != nil {
			return nil, err
		}
		ids := []uuid.UUID{latest.ID}
		var prev *store.Run
		for i := range prevPg.Items {
			if prevPg.Items[i].ID != latest.ID {
				prev = &prevPg.Items[i]
				ids = append(ids, prev.ID)
				break
			}
		}
		aggs, err := s.Store.ListRunAggregatesForRuns(ctx, ids, "", "")
		if err != nil {
			return nil, err
		}
		passRate := func(r store.Run) *float64 {
			for _, a := range aggs {
				if a.RunID == r.ID && a.Metric == store.MetricPassRate && a.SliceKey == "" {
					if a.PassRate != nil {
						return a.PassRate
					}
					if a.Mean != nil {
						return a.Mean
					}
				}
			}
			if r.Completed > 0 { // no aggregates yet: derive from the run counters
				v := 1 - float64(r.Failed+r.Errored)/float64(r.Completed)
				return ptr(min(max(v, 0), 1))
			}
			return nil
		}
		out.LatestPassRate = passRate(latest)
		if prev != nil && out.LatestPassRate != nil {
			if pp := passRate(*prev); pp != nil {
				out.PassRateDelta = ptr(*out.LatestPassRate - *pp)
			}
		}
		var sum float64
		var n int
		for _, a := range aggs {
			if a.RunID == latest.ID && a.Metric == store.MetricScore && a.SliceKey == "" && a.Mean != nil {
				sum += *a.Mean
				n++
			}
		}
		if n > 0 {
			out.AvgScore = ptr(sum / float64(n))
		}
	}

	if len(stats.FailingSuiteIDs) > 0 {
		fs := make([]struct {
			Name    *string   `json:"name,omitempty"`
			RunId   uuid.UUID `json:"run_id"`
			SuiteId uuid.UUID `json:"suite_id"`
		}, len(stats.FailingSuiteIDs))
		for i, sid := range stats.FailingSuiteIDs {
			fs[i].SuiteId, fs[i].RunId = sid, stats.FailingRunIDs[i]
			if x, err := s.Store.GetSuite(ctx, sid); err == nil {
				fs[i].Name = ptr(x.Name)
			}
		}
		out.FailingSuites = &fs
	}
	return gen.GetProjectOverview200JSONResponse(out), nil
}
