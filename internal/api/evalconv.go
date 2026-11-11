package api

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/airval/airval/internal/api/gen"
	"github.com/airval/airval/internal/evalcore"
	"github.com/airval/airval/internal/secrets"
	"github.com/airval/airval/internal/store"
	"github.com/airval/airval/internal/targets"
)

// ---- store <-> evalcore example conversion ----

// toEvalExample converts a stored example revision to the execution pipeline's type.
func toEvalExample(e store.Example) evalcore.Example {
	return evalFromData(e.ID.String(), e.ExampleData)
}

func evalFromData(id string, d store.ExampleData) evalcore.Example {
	ex := evalcore.Example{ID: id, Tags: d.Tags}
	_ = json.Unmarshal(rawOr(d.Input, "{}"), &ex.Input)
	if ex.Input == nil {
		ex.Input = map[string]any{}
	}
	if len(d.Expected) > 0 && string(d.Expected) != "null" {
		_ = json.Unmarshal(d.Expected, &ex.Expected)
	}
	if len(d.Context) > 0 && string(d.Context) != "null" {
		_ = json.Unmarshal(d.Context, &ex.Context)
	}
	if len(d.Metadata) > 0 && string(d.Metadata) != "null" {
		_ = json.Unmarshal(d.Metadata, &ex.Metadata)
	}
	if len(d.Conversation) > 0 && string(d.Conversation) != "null" {
		var c evalcore.Conversation
		if json.Unmarshal(d.Conversation, &c) == nil {
			ex.Conversation = &c
		}
	}
	return ex
}

// fromEvalExample converts an imported example to a store upsert (a fresh example; the
// importer's id is kept only when it is a UUID).
func fromEvalExample(ex evalcore.Example, extraTags []string) store.ExampleUpsert {
	u := store.ExampleUpsert{}
	// Imported rows always become new examples: ids from a source (e.g. an export of another
	// dataset) are globally unique keys and must not be reused.
	u.Input = mustJSON(nonNilMap(ex.Input))
	if ex.Expected != nil {
		u.Expected = mustJSON(ex.Expected)
	}
	if len(ex.Context) > 0 {
		u.Context = mustJSON(ex.Context)
	}
	if len(ex.Metadata) > 0 {
		u.Metadata = mustJSON(ex.Metadata)
	}
	u.Tags = normalizeTags(append(append([]string{}, ex.Tags...), extraTags...))
	if ex.Conversation != nil && (len(ex.Conversation.Turns) > 0 || ex.Conversation.Simulation != nil) {
		u.Conversation = mustJSON(ex.Conversation)
	}
	return u
}

func nonNilMap(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	return m
}

// exampleDataToEval converts an API example payload.
func exampleDataToEval(d gen.ExampleData) (evalcore.Example, error) {
	u, err := fromExampleData(d)
	if err != nil {
		return evalcore.Example{}, err
	}
	return evalFromData("sample", u.ExampleData), nil
}

// defaultSample is used when a test request carries no example and no dataset.
var defaultSample = evalcore.Example{ID: "sample", Input: map[string]any{"input": "Hello! This is a test from airval.", "question": "Hello! This is a test from airval."}}

// sampleExample resolves the example a target test sends: the body's, else the first example
// of a dataset in the project, else a built-in sample.
func (s *Server) sampleExample(ctx context.Context, projectID uuid.UUID, ex *gen.ExampleData, datasetID *uuid.UUID) (evalcore.Example, error) {
	switch {
	case ex != nil:
		out, err := exampleDataToEval(*ex)
		if err != nil {
			return out, fieldError("example", "%v", err)
		}
		return out, nil
	case datasetID != nil:
		d, err := s.dataset(ctx, *datasetID, store.RoleViewer)
		if err != nil {
			return evalcore.Example{}, err
		}
		if d.ProjectID != projectID {
			return evalcore.Example{}, fieldError("dataset_id", "dataset is not in this project")
		}
		pg, err := s.Store.ListExamples(ctx, store.ExampleFilter{DatasetID: d.ID}, store.ListParams{Limit: 1})
		if err != nil {
			return evalcore.Example{}, err
		}
		if len(pg.Items) == 0 {
			return evalcore.Example{}, fieldError("dataset_id", "dataset has no examples to test with")
		}
		return toEvalExample(pg.Items[0]), nil
	}
	return defaultSample, nil
}

// ---- evalcore -> API responses ----

func convertJSON[T any](v any) (T, error) {
	var out T
	b, err := json.Marshal(v)
	if err != nil {
		return out, err
	}
	err = json.Unmarshal(b, &out)
	return out, err
}

func toTargetResponse(r evalcore.Response) gen.TargetResponse {
	out := gen.TargetResponse{Output: r.Output, CostUsd: ptr(r.CostUSD), LatencyMs: ptr(durMS(r.Latency))}
	if r.OutputJSON != nil {
		out.OutputJson = rawPtr(mustJSON(r.OutputJSON))
	}
	if len(r.Context) > 0 {
		out.Context = &r.Context
	}
	if len(r.ToolCalls) > 0 {
		if v, err := convertJSON[[]gen.ToolCall](r.ToolCalls); err == nil {
			out.ToolCalls = &v
		}
	}
	if len(r.Trace) > 0 {
		if v, err := convertJSON[[]gen.TraceStep](r.Trace); err == nil {
			out.Trace = &v
		}
	}
	if len(r.Transcript) > 0 {
		if v, err := convertJSON[[]gen.Message](r.Transcript); err == nil {
			out.Transcript = &v
		}
	}
	if r.TTFT > 0 {
		out.TtftMs = ptr(durMS(r.TTFT))
	}
	if r.Usage.InputTokens > 0 || r.Usage.OutputTokens > 0 {
		out.Usage = &gen.Usage{InputTokens: r.Usage.InputTokens, OutputTokens: r.Usage.OutputTokens}
	}
	if r.Cached {
		out.Cached = ptr(true)
	}
	return out
}

func fromTargetResponse(r gen.TargetResponse) (evalcore.Response, error) {
	out := evalcore.Response{Output: r.Output, CostUSD: deref(r.CostUsd, 0),
		Latency: time.Duration(deref(r.LatencyMs, 0) * float64(time.Millisecond)),
		TTFT:    time.Duration(deref(r.TtftMs, 0) * float64(time.Millisecond)), Cached: deref(r.Cached, false)}
	if r.OutputJson != nil && len(*r.OutputJson) > 0 {
		if err := json.Unmarshal(*r.OutputJson, &out.OutputJSON); err != nil {
			return out, fmt.Errorf("output_json: %w", err)
		}
	}
	if r.Context != nil {
		out.Context = *r.Context
	}
	var err error
	if r.ToolCalls != nil {
		if out.ToolCalls, err = convertJSON[[]evalcore.ToolCall](*r.ToolCalls); err != nil {
			return out, fmt.Errorf("tool_calls: %w", err)
		}
	}
	if r.Trace != nil {
		if out.Trace, err = convertJSON[[]evalcore.TraceStep](*r.Trace); err != nil {
			return out, fmt.Errorf("trace: %w", err)
		}
	}
	if r.Transcript != nil {
		if out.Transcript, err = convertJSON[[]evalcore.Message](*r.Transcript); err != nil {
			return out, fmt.Errorf("transcript: %w", err)
		}
	}
	if r.Usage != nil {
		out.Usage = evalcore.Usage{InputTokens: r.Usage.InputTokens, OutputTokens: r.Usage.OutputTokens}
	}
	return out, nil
}

func durMS(d time.Duration) float64 { return float64(d) / float64(time.Millisecond) }

// ---- targets plumbing ----

var (
	clientsOnce sync.Once
	clients     *targets.ClientFactory
)

// targetDeps builds the shared adapter dependencies (SSRF guard from config.Security).
func (s *Server) targetDeps() targets.Deps {
	clientsOnce.Do(func() {
		var pol targets.NetworkPolicy
		if s.Config != nil {
			pol.AllowCIDRs = splitList(s.Config.Security.AllowCIDRs)
			pol.DenyCIDRs = splitList(s.Config.Security.DenyCIDRs)
		}
		g, err := targets.NewSSRFGuard(pol)
		if err != nil {
			s.Logger.Warn("invalid security CIDR config; using the default outbound policy", "err", err)
			g = nil
		}
		clients = targets.NewClientFactory(g)
	})
	return targets.Deps{Clients: clients}
}

func splitList(v string) []string {
	var out []string
	for _, p := range strings.Split(v, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// safeResolver resolves secrets for one project; safe for concurrent use.
type safeResolver struct {
	mu sync.Mutex
	r  *secrets.Resolver
}

func (s *safeResolver) Resolve(name string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.r.Resolve(name)
}

func (s *Server) secretResolver(ctx context.Context, p store.Project) evalcore.SecretResolver {
	return &safeResolver{r: &secrets.Resolver{Ctx: ctx, Store: s.Store, Keyring: s.Keyring, OrgID: p.OrgID, ProjectID: &p.ID}}
}

// ---- config validation (400 with field details) ----

// fieldError is a 400 validation_failed error pointing at a request field.
func fieldError(field, format string, args ...any) *Error {
	e := errorf(400, "validation_failed", format, args...)
	e.Details = map[string]any{"field": field}
	return e
}

func (e *Error) with(k string, v any) *Error {
	if e.Details == nil {
		e.Details = map[string]any{}
	}
	e.Details[k] = v
	return e
}

func validateTargetConfig(kind string, cfg json.RawMessage) error {
	if err := targets.ValidateConfig(kind, cfg); err != nil {
		return fieldError("config", "%v", err).with("kind", kind)
	}
	return nil
}

func msDuration(ms float64) time.Duration { return time.Duration(ms * float64(time.Millisecond)) }
