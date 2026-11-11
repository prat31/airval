package api

import (
	"context"
	"encoding/json"
	"math"
	"sort"

	"github.com/google/uuid"

	"github.com/prat31/airval/internal/aggregate"
	"github.com/prat31/airval/internal/api/gen"
	"github.com/prat31/airval/internal/evalcore"
	"github.com/prat31/airval/internal/evaluators"
	"github.com/prat31/airval/internal/store"
)

const (
	maxScatterPoints = 2000
	inputPreviewLen  = 140
)

// runData is everything a comparison needs from one run, collected in a single streaming pass.
type runData struct {
	run      store.Run
	rollup   aggregate.Rollup
	summary  *aggregate.Summary
	inputs   map[string]string // example id -> input preview (baseline only)
	wins     int
	ties     int
	losses   int
	examples int
}

// collectRun streams a run's results once into an aggregator (constant memory per result).
func (s *Server) collectRun(ctx context.Context, r store.Run, withInputs bool) (*runData, error) {
	d := &runData{run: r}
	agg := aggregate.New(nil, aggregate.Options{NoTagSlices: true})
	if withInputs {
		d.inputs = map[string]string{}
	}
	err := s.Store.IterateRunResults(ctx, r.ID, func(res store.Result) error {
		rec := aggregate.Record{ExampleID: res.ExampleID.String(), Trial: res.Trial, Error: res.Error, CostUSD: res.CostUSD,
			Latency: msDuration(res.LatencyMs), Usage: evalcore.Usage{InputTokens: res.TokensIn, OutputTokens: res.TokensOut}}
		if res.Status == store.ResultErrored && rec.Error == "" {
			rec.Error = "errored"
		}
		rec.Scores = make([]evalcore.Score, len(res.Scores))
		for i, sc := range res.Scores {
			rec.Scores[i] = evalcore.Score{EvaluatorID: sc.EvaluatorID.String(), Value: sc.Value, Label: sc.Label,
				Passed: sc.Passed, Reason: sc.Reason, CostUSD: sc.CostUSD}
			if sc.Passed == nil && len(sc.Meta) > 0 {
				var m map[string]any
				if json.Unmarshal(sc.Meta, &m) == nil {
					rec.Scores[i].Meta = m
				}
			}
			switch sc.Label {
			case evaluators.LabelWin:
				d.wins++
			case evaluators.LabelTie:
				d.ties++
			case evaluators.LabelLoss:
				d.losses++
			}
		}
		agg.Add(rec)
		if withInputs && res.Trial == 0 {
			d.inputs[rec.ExampleID] = previewJSON(res.Input)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	d.rollup, d.summary = agg.Rollup(), agg.Summary()
	return d, nil
}

func previewJSON(raw json.RawMessage) string {
	var v map[string]any
	if json.Unmarshal(raw, &v) == nil && len(v) == 1 {
		for _, x := range v {
			if str, ok := x.(string); ok {
				return truncRunes(str, inputPreviewLen)
			}
		}
	}
	return truncRunes(string(raw), inputPreviewLen)
}

func truncRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// ---- report (JSON-shaped mirror of gen.ComparisonReport, converted at the end) ----

type cmpCell struct {
	RunID       uuid.UUID `json:"run_id"`
	N           int       `json:"n"`
	Mean        *float64  `json:"mean,omitempty"`
	PassRate    *float64  `json:"pass_rate,omitempty"`
	DeltaMean   *float64  `json:"delta_mean,omitempty"`
	DeltaPass   *float64  `json:"delta_pass_rate,omitempty"`
	CILow       *float64  `json:"ci_low,omitempty"`
	CIHigh      *float64  `json:"ci_high,omitempty"`
	PValue      *float64  `json:"p_value,omitempty"`
	Significant bool      `json:"significant"`
	Paired      *int      `json:"paired,omitempty"`
}

type cmpRow struct {
	Key         string     `json:"key"`
	Label       string     `json:"label"`
	EvaluatorID *uuid.UUID `json:"evaluator_id,omitempty"`
	Cells       []cmpCell  `json:"cells"`
}

type cmpDelta struct {
	ExampleID       string  `json:"example_id"`
	InputPreview    string  `json:"input_preview,omitempty"`
	Baseline        float64 `json:"baseline"`
	Candidate       float64 `json:"candidate"`
	Delta           float64 `json:"delta"`
	BaselinePassed  *bool   `json:"baseline_passed,omitempty"`
	CandidatePassed *bool   `json:"candidate_passed,omitempty"`
}

type cmpAligned struct {
	ExampleID    string `json:"example_id"`
	InputPreview string `json:"input_preview,omitempty"`
	Cells        []struct {
		RunID  uuid.UUID `json:"run_id"`
		Value  *float64  `json:"value,omitempty"`
		Passed *bool     `json:"passed,omitempty"`
	} `json:"cells"`
}

type cmpReport struct {
	Runs        []gen.Run  `json:"runs"`
	EvaluatorID *uuid.UUID `json:"evaluator_id,omitempty"`
	Evaluators  []struct {
		ID   uuid.UUID `json:"id"`
		Name string    `json:"name"`
	} `json:"evaluators,omitempty"`
	AgainstRunID      *uuid.UUID `json:"against_run_id,omitempty"`
	Scorecard         []cmpRow   `json:"scorecard"`
	Regressions       []cmpDelta `json:"regressions"`
	RegressionsTotal  int        `json:"regressions_total"`
	Improvements      []cmpDelta `json:"improvements"`
	ImprovementsTotal int        `json:"improvements_total"`
	Scatter           []struct {
		ExampleID string  `json:"example_id"`
		A         float64 `json:"a"`
		B         float64 `json:"b"`
	} `json:"scatter,omitempty"`
	Rows      []cmpAligned `json:"rows,omitempty"`
	RowsTotal int          `json:"rows_total"`
	Pairwise  []struct {
		RunID   uuid.UUID `json:"run_id"`
		Wins    int       `json:"wins"`
		Ties    int       `json:"ties"`
		Losses  int       `json:"losses"`
		WinRate float64   `json:"win_rate"`
	} `json:"pairwise,omitempty"`
}

func fptr(v float64) *float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return nil
	}
	return &v
}

func passedOfRollup(r aggregate.ExampleRollup) *bool {
	if r.Passed+r.Failed == 0 {
		return nil
	}
	p := r.Passed >= r.Failed
	return &p
}

func (s *Server) CompareRuns(ctx context.Context, req gen.CompareRunsRequestObject) (gen.CompareRunsResponseObject, error) {
	ids := req.Params.Runs
	if n := len(ids); n < 2 || n > 4 {
		return nil, badRequest("compare 2 to 4 runs")
	}
	limit := min(max(deref(req.Params.Limit, 50), 1), 500)
	offset := max(deref(req.Params.Offset, 0), 0)

	runs := make([]store.Run, len(ids))
	seen := map[uuid.UUID]bool{}
	for i, id := range ids {
		if seen[id] {
			return nil, badRequest("runs must be distinct")
		}
		seen[id] = true
		r, err := s.run(ctx, id, store.RoleViewer)
		if err != nil {
			return nil, err
		}
		if r.ProjectID != runs[0].ProjectID && i > 0 {
			return nil, badRequest("runs must belong to the same project")
		}
		runs[i] = r
	}
	against := 1
	if a := req.Params.Against; a != nil {
		against = -1
		for i, id := range ids {
			if id == *a && i > 0 {
				against = i
			}
		}
		if against < 0 {
			return nil, fieldError("against", "against must be one of the compared runs other than the baseline")
		}
	}

	data := make([]*runData, len(runs))
	for i, r := range runs {
		d, err := s.collectRun(ctx, r, i == 0)
		if err != nil {
			return nil, err
		}
		data[i] = d
	}
	base := data[0]

	// Paired comparisons of every candidate against the baseline.
	opts := aggregate.CompareOptions{MaxListed: offset + limit}
	cmps := make([]*aggregate.Comparison, len(data))
	for i := 1; i < len(data); i++ {
		cmps[i] = aggregate.Compare(base.rollup, data[i].rollup, opts)
	}

	// Evaluator names.
	evalIDs := map[string]bool{}
	for _, d := range data {
		for id := range d.rollup {
			evalIDs[id] = true
		}
	}
	names := s.evaluatorNames(ctx, evalIDs)
	nameOf := func(id string) string {
		if n, ok := names[id]; ok {
			return n
		}
		return id
	}

	rep := cmpReport{Runs: mapSlice(runs, toRun)}
	// Common evaluators (in the baseline and at least one other run), by name.
	var common []string
	for id := range base.rollup {
		for i := 1; i < len(data); i++ {
			if _, ok := data[i].rollup[id]; ok {
				common = append(common, id)
				break
			}
		}
	}
	sort.Slice(common, func(i, j int) bool { return nameOf(common[i]) < nameOf(common[j]) })
	for _, id := range common {
		if u, err := uuid.Parse(id); err == nil {
			rep.Evaluators = append(rep.Evaluators, struct {
				ID   uuid.UUID `json:"id"`
				Name string    `json:"name"`
			}{u, nameOf(id)})
		}
	}
	chosen := ""
	if e := req.Params.EvaluatorId; e != nil {
		chosen = e.String()
	} else if len(common) > 0 {
		chosen = common[0]
	}

	// Scorecard: one row per evaluator, then run-level metrics.
	allEvals := make([]string, 0, len(evalIDs))
	for id := range evalIDs {
		allEvals = append(allEvals, id)
	}
	sort.Slice(allEvals, func(i, j int) bool { return nameOf(allEvals[i]) < nameOf(allEvals[j]) })
	for _, id := range allEvals {
		row := cmpRow{Key: id, Label: nameOf(id)}
		if u, err := uuid.Parse(id); err == nil {
			row.EvaluatorID = &u
		}
		var baseCell *cmpCell
		for i, d := range data {
			cell := cmpCell{RunID: d.run.ID}
			if es, ok := d.summary.Evaluator(id); ok {
				cell.N = es.Values.N
				if es.Values.N > 0 {
					cell.Mean = fptr(es.Values.Mean)
				}
				if es.Passed+es.Failed+es.Errored > 0 {
					cell.PassRate = fptr(es.PassRate)
				}
			}
			if i == 0 {
				baseCell = &cell
			} else {
				if ec, ok := cmps[i].Evaluator(id); ok && ec.Paired > 0 {
					cell.DeltaMean = fptr(ec.MeanDelta)
					cell.CILow, cell.CIHigh, cell.PValue = fptr(ec.CILow), fptr(ec.CIHigh), fptr(ec.PValue)
					cell.Significant, cell.Paired = ec.Significant, ptr(ec.Paired)
				}
				if cell.PassRate != nil && baseCell.PassRate != nil {
					cell.DeltaPass = fptr(*cell.PassRate - *baseCell.PassRate)
				}
			}
			row.Cells = append(row.Cells, cell)
		}
		rep.Scorecard = append(rep.Scorecard, row)
	}
	runLevel := []struct {
		key, label string
		get        func(*aggregate.Summary) (float64, bool)
	}{
		{"pass_rate", "Pass rate", func(s *aggregate.Summary) (float64, bool) { return s.PassRate, s.Results > 0 }},
		{"latency_p95", "Latency p95 (ms)", func(s *aggregate.Summary) (float64, bool) { return s.LatencyMs.P95, s.LatencyMs.N > 0 }},
		{"cost_usd", "Cost (USD)", func(s *aggregate.Summary) (float64, bool) { return s.TotalCostUSD, s.Results > 0 }},
	}
	for _, m := range runLevel {
		row := cmpRow{Key: m.key, Label: m.label}
		var baseV *float64
		for i, d := range data {
			cell := cmpCell{RunID: d.run.ID, N: d.summary.Results}
			if v, ok := m.get(d.summary); ok {
				cell.Mean = fptr(v)
				if m.key == "pass_rate" {
					cell.PassRate = cell.Mean
				}
			}
			if i == 0 {
				baseV = cell.Mean
			} else if cell.Mean != nil && baseV != nil {
				cell.DeltaMean = fptr(*cell.Mean - *baseV)
				if m.key == "pass_rate" {
					cell.DeltaPass = cell.DeltaMean
				}
			}
			row.Cells = append(row.Cells, cell)
		}
		rep.Scorecard = append(rep.Scorecard, row)
	}

	// Ranked regressions/improvements, scatter and aligned rows for the chosen evaluator.
	rep.Regressions, rep.Improvements = []cmpDelta{}, []cmpDelta{}
	if chosen != "" {
		if u, err := uuid.Parse(chosen); err == nil {
			rep.EvaluatorID = &u
		}
		ar := runs[against].ID
		rep.AgainstRunID = &ar
		if ec, ok := cmps[against].Evaluator(chosen); ok {
			rep.RegressionsTotal, rep.ImprovementsTotal = ec.Regressed, ec.Improved
			rep.Regressions = pageDeltas(ec.Regressions, offset, limit, base.inputs)
			rep.Improvements = pageDeltas(ec.Improvements, offset, limit, base.inputs)
		}
		rep.Scatter = scatter(base.rollup[chosen], data[against].rollup[chosen])
		rep.Rows, rep.RowsTotal = alignedRows(chosen, data, offset, limit)
	}

	for i := 1; i < len(data); i++ {
		d := data[i]
		if decided := d.wins + d.ties + d.losses; decided > 0 {
			rep.Pairwise = append(rep.Pairwise, struct {
				RunID   uuid.UUID `json:"run_id"`
				Wins    int       `json:"wins"`
				Ties    int       `json:"ties"`
				Losses  int       `json:"losses"`
				WinRate float64   `json:"win_rate"`
			}{d.run.ID, d.wins, d.ties, d.losses, (float64(d.wins) + float64(d.ties)/2) / float64(decided)})
		}
	}

	out, err := convertJSON[gen.ComparisonReport](rep)
	if err != nil {
		return nil, err
	}
	return gen.CompareRuns200JSONResponse(out), nil
}

func pageDeltas(all []aggregate.ExampleDelta, offset, limit int, inputs map[string]string) []cmpDelta {
	if offset >= len(all) {
		return []cmpDelta{}
	}
	end := min(offset+limit, len(all))
	out := make([]cmpDelta, 0, end-offset)
	for _, d := range all[offset:end] {
		out = append(out, cmpDelta{ExampleID: d.ExampleID, InputPreview: inputs[d.ExampleID], Baseline: d.Baseline,
			Candidate: d.Candidate, Delta: d.Delta, BaselinePassed: d.BaselinePassed, CandidatePassed: d.CandidatePassed})
	}
	return out
}

func scatter(base, cand map[string]aggregate.ExampleRollup) []struct {
	ExampleID string  `json:"example_id"`
	A         float64 `json:"a"`
	B         float64 `json:"b"`
} {
	type pt = struct {
		ExampleID string  `json:"example_id"`
		A         float64 `json:"a"`
		B         float64 `json:"b"`
	}
	ids := make([]string, 0, len(cand))
	for id := range cand {
		if _, ok := base[id]; ok {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	step := 1
	if len(ids) > maxScatterPoints {
		step = (len(ids) + maxScatterPoints - 1) / maxScatterPoints
	}
	var out []pt
	for i := 0; i < len(ids); i += step {
		out = append(out, pt{ids[i], base[ids[i]].Mean, cand[ids[i]].Mean})
	}
	return out
}

// alignedRows lines up every example's score across all runs, most changed (vs the baseline) first.
func alignedRows(evaluatorID string, data []*runData, offset, limit int) ([]cmpAligned, int) {
	type ranked struct {
		id    string
		score float64
	}
	seen := map[string]bool{}
	var all []ranked
	for _, d := range data {
		for ex := range d.rollup[evaluatorID] {
			if seen[ex] {
				continue
			}
			seen[ex] = true
			b, hasBase := data[0].rollup[evaluatorID][ex]
			var maxDelta float64
			if hasBase {
				for _, o := range data[1:] {
					if r, ok := o.rollup[evaluatorID][ex]; ok {
						maxDelta = math.Max(maxDelta, math.Abs(r.Mean-b.Mean))
					}
				}
			}
			all = append(all, ranked{ex, maxDelta})
		}
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].score != all[j].score {
			return all[i].score > all[j].score
		}
		return all[i].id < all[j].id
	})
	total := len(all)
	if offset >= total {
		return []cmpAligned{}, total
	}
	end := min(offset+limit, total)
	out := make([]cmpAligned, 0, end-offset)
	for _, r := range all[offset:end] {
		row := cmpAligned{ExampleID: r.id, InputPreview: data[0].inputs[r.id]}
		for _, d := range data {
			cell := struct {
				RunID  uuid.UUID `json:"run_id"`
				Value  *float64  `json:"value,omitempty"`
				Passed *bool     `json:"passed,omitempty"`
			}{RunID: d.run.ID}
			if er, ok := d.rollup[evaluatorID][r.id]; ok {
				cell.Value, cell.Passed = fptr(er.Mean), passedOfRollup(er)
			}
			row.Cells = append(row.Cells, cell)
		}
		out = append(out, row)
	}
	return out, total
}

// evaluatorNames resolves evaluator ids (uuid strings) to display names.
func (s *Server) evaluatorNames(ctx context.Context, ids map[string]bool) map[string]string {
	var uu []uuid.UUID
	for id := range ids {
		if u, err := uuid.Parse(id); err == nil {
			uu = append(uu, u)
		}
	}
	out := map[string]string{}
	if len(uu) == 0 {
		return out
	}
	if evs, err := s.Store.GetEvaluators(ctx, uu); err == nil {
		for _, e := range evs {
			out[e.ID.String()] = e.Name
		}
	}
	return out
}
