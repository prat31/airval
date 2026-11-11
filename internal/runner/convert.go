package runner

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/prat31/airval/internal/aggregate"
	"github.com/prat31/airval/internal/evalcore"
	"github.com/prat31/airval/internal/store"
)

// maxRawResponse bounds the raw response kept per result.
const maxRawResponse = 64 << 10

// toEvalExample converts a stored example revision into the pipeline's Example.
func toEvalExample(e store.Example) evalcore.Example {
	ex := evalcore.Example{ID: e.ID.String(), Tags: e.Tags}
	if len(e.Input) > 0 {
		_ = json.Unmarshal(e.Input, &ex.Input)
	}
	if ex.Input == nil {
		ex.Input = map[string]any{}
	}
	if len(e.Expected) > 0 && string(e.Expected) != "null" {
		_ = json.Unmarshal(e.Expected, &ex.Expected)
	}
	if len(e.Context) > 0 && string(e.Context) != "null" {
		var raw []any
		if json.Unmarshal(e.Context, &raw) == nil {
			for _, v := range raw {
				switch t := v.(type) {
				case string:
					ex.Context = append(ex.Context, t)
				default:
					b, _ := json.Marshal(t)
					ex.Context = append(ex.Context, string(b))
				}
			}
		}
	}
	if len(e.Metadata) > 0 && string(e.Metadata) != "null" {
		_ = json.Unmarshal(e.Metadata, &ex.Metadata)
	}
	if len(e.Conversation) > 0 && string(e.Conversation) != "null" {
		var c evalcore.Conversation
		if json.Unmarshal(e.Conversation, &c) == nil && (len(c.Turns) > 0 || c.Simulation != nil) {
			ex.Conversation = &c
		}
	}
	return ex
}

func marshalOrNil(v any, empty bool) json.RawMessage {
	if empty {
		return nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return b
}

// fillResult copies a target response into a result row.
func fillResult(r *store.Result, resp *evalcore.Response) {
	r.Output = resp.Output
	r.OutputJSON = marshalOrNil(resp.OutputJSON, resp.OutputJSON == nil)
	r.Context = marshalOrNil(resp.Context, len(resp.Context) == 0)
	r.ToolCalls = marshalOrNil(resp.ToolCalls, len(resp.ToolCalls) == 0)
	r.Trace = marshalOrNil(resp.Trace, len(resp.Trace) == 0)
	r.Transcript = marshalOrNil(resp.Transcript, len(resp.Transcript) == 0)
	if len(resp.Request) > 0 && json.Valid(resp.Request) {
		r.Request = resp.Request
	}
	if len(resp.Raw) > 0 {
		raw := resp.Raw
		if len(raw) > maxRawResponse || !json.Valid(raw) {
			if len(raw) > maxRawResponse {
				raw = raw[:maxRawResponse]
			}
			raw, _ = json.Marshal(string(raw)) // keep as a JSON string
		}
		r.Response = raw
	}
	r.LatencyMs = float64(resp.Latency) / float64(time.Millisecond)
	if resp.TTFT > 0 {
		v := float64(resp.TTFT) / float64(time.Millisecond)
		r.TTFTMs = &v
	}
	r.TokensIn, r.TokensOut = resp.Usage.InputTokens, resp.Usage.OutputTokens
	r.CostUSD = resp.CostUSD
	r.Cached = resp.Cached
}

// responseFromResult rebuilds a Response from a stored result (rescore, pairwise baselines).
func responseFromResult(r store.Result) evalcore.Response {
	resp := evalcore.Response{Output: r.Output, Request: r.Request, Raw: r.Response, CostUSD: r.CostUSD, Cached: true,
		Usage:   evalcore.Usage{InputTokens: r.TokensIn, OutputTokens: r.TokensOut},
		Latency: time.Duration(r.LatencyMs * float64(time.Millisecond))}
	if r.TTFTMs != nil {
		resp.TTFT = time.Duration(*r.TTFTMs * float64(time.Millisecond))
	}
	if len(r.OutputJSON) > 0 {
		_ = json.Unmarshal(r.OutputJSON, &resp.OutputJSON)
	}
	if len(r.Context) > 0 {
		_ = json.Unmarshal(r.Context, &resp.Context)
	}
	if len(r.ToolCalls) > 0 {
		_ = json.Unmarshal(r.ToolCalls, &resp.ToolCalls)
	}
	if len(r.Trace) > 0 {
		_ = json.Unmarshal(r.Trace, &resp.Trace)
	}
	if len(r.Transcript) > 0 {
		_ = json.Unmarshal(r.Transcript, &resp.Transcript)
	}
	return resp
}

// toStoreScores converts pipeline scores to rows. Unknown (non-uuid) evaluator ids are skipped.
func toStoreScores(resultID, runID uuid.UUID, scores []evalcore.Score, at time.Time) []store.Score {
	out := make([]store.Score, 0, len(scores))
	for _, s := range scores {
		id, err := uuid.Parse(s.EvaluatorID)
		if err != nil {
			continue
		}
		row := store.Score{ResultID: resultID, EvaluatorID: id, RunID: runID, Value: s.Value, Label: s.Label,
			Passed: s.Passed, Reason: s.Reason, LatencyMs: float64(s.Latency) / float64(time.Millisecond),
			CostUSD: s.CostUSD, CreatedAt: at}
		if len(s.Meta) > 0 {
			row.Meta, _ = json.Marshal(s.Meta)
		}
		out = append(out, row)
	}
	return out
}

// fromStoreScore converts a stored score for aggregation.
func fromStoreScore(s store.Score) evalcore.Score {
	es := evalcore.Score{EvaluatorID: s.EvaluatorID.String(), Value: s.Value, Label: s.Label, Passed: s.Passed,
		Reason: s.Reason, CostUSD: s.CostUSD, Latency: time.Duration(s.LatencyMs * float64(time.Millisecond))}
	// Classify only consults Meta["error"] for verdict-less scores; avoid decoding otherwise.
	if s.Passed == nil && len(s.Meta) > 0 && strings.Contains(string(s.Meta), `"error"`) {
		_ = json.Unmarshal(s.Meta, &es.Meta)
	}
	return es
}

// resultStatus classifies a result from its target error and scores (mirrors aggregate outcomes).
func resultStatus(targetErr string, scores []evalcore.Score) string {
	if targetErr != "" {
		return store.ResultErrored
	}
	failed, errored := false, false
	for _, s := range scores {
		switch aggregate.Classify(s) {
		case aggregate.StateFailed:
			failed = true
		case aggregate.StateErrored:
			errored = true
		}
	}
	switch {
	case errored:
		return store.ResultErrored
	case failed:
		return store.ResultFailed
	}
	return store.ResultPassed
}
