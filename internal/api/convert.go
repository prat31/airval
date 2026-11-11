package api

import (
	"encoding/json"
	"strings"

	"github.com/google/uuid"

	"github.com/airval/airval/internal/aggregate"
	"github.com/airval/airval/internal/api/gen"
	"github.com/airval/airval/internal/store"
)

// ---- small helpers ----

func ptr[T any](v T) *T { return &v }

func deref[T any](p *T, def T) T {
	if p == nil {
		return def
	}
	return *p
}

func listParams(cursor *gen.Cursor, limit *gen.Limit) store.ListParams {
	return store.ListParams{Cursor: deref(cursor, ""), Limit: deref(limit, 0)}
}

func nextCursor(c string) *string {
	if c == "" {
		return nil
	}
	return &c
}

func mapSlice[T, U any](in []T, f func(T) U) []U {
	out := make([]U, len(in))
	for i, v := range in {
		out[i] = f(v)
	}
	return out
}

func mustJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

// rawOr returns raw, or def when raw is empty/null.
func rawOr(raw json.RawMessage, def string) json.RawMessage {
	if len(raw) == 0 || string(raw) == "null" {
		return json.RawMessage(def)
	}
	return raw
}

func rawPtr(raw json.RawMessage) *json.RawMessage {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	return &raw
}

// jsonObject validates that raw is a JSON object (or empty).
func jsonObject(raw json.RawMessage, field string) (json.RawMessage, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return json.RawMessage(`{}`), nil
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, invalid("%s must be a JSON object", field)
	}
	return raw, nil
}

func trimmed(p *string) string { return strings.TrimSpace(deref(p, "")) }

// ---- users & orgs ----

func toUser(u store.User) gen.User {
	return gen.User{Id: u.ID, Email: u.Email, Name: u.Name, AvatarUrl: u.AvatarURL, CreatedAt: u.CreatedAt}
}

func toOrg(o store.Organization, role store.Role) gen.Organization {
	out := gen.Organization{Id: o.ID, Name: o.Name, Slug: o.Slug, CreatedAt: o.CreatedAt, UpdatedAt: o.UpdatedAt}
	if role != "" {
		out.Role = ptr(gen.Role(role))
	}
	return out
}

func toMember(m store.Member) gen.Member {
	return gen.Member{User: toUser(m.User), Role: gen.Role(m.Role), CreatedAt: m.CreatedAt}
}

func toAPIKey(k store.APIKey) gen.ApiKey {
	return gen.ApiKey{Id: k.ID, OrgId: k.OrgID, ProjectId: k.ProjectID, Name: k.Name, Prefix: k.Prefix,
		Role: gen.Role(k.Role), CreatedBy: k.CreatedBy, LastUsedAt: k.LastUsedAt, ExpiresAt: k.ExpiresAt, CreatedAt: k.CreatedAt}
}

func toSecret(x store.Secret) gen.Secret {
	return gen.Secret{Id: x.ID, OrgId: x.OrgID, ProjectId: x.ProjectID, Name: x.Name, Description: x.Description,
		KeyVersion: ptr(x.KeyVersion), CreatedBy: x.CreatedBy, CreatedAt: x.CreatedAt, UpdatedAt: x.UpdatedAt}
}

func toChannel(c store.NotificationChannel) gen.NotificationChannel {
	return gen.NotificationChannel{Id: c.ID, OrgId: c.OrgID, Name: c.Name, Kind: gen.NotificationChannelKind(c.Kind),
		Config: rawOr(c.Config, "{}"), Events: mapSlice(c.Events, func(e string) gen.NotificationEvent { return gen.NotificationEvent(e) }),
		Enabled: c.Enabled, CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt}
}

func toAudit(e store.AuditEntry) gen.AuditEntry {
	return gen.AuditEntry{Id: e.ID, OrgId: e.OrgID, ActorId: e.ActorID, Actor: e.Actor, Action: e.Action, Target: e.Target,
		Diff: rawPtr(e.Diff), At: e.At}
}

// ---- projects ----

func toProject(p store.Project) gen.Project {
	var st gen.ProjectSettings
	_ = json.Unmarshal(rawOr(p.Settings, "{}"), &st)
	return gen.Project{Id: p.ID, OrgId: p.OrgID, Name: p.Name, Slug: p.Slug, Description: p.Description, Color: p.Color,
		Icon: p.Icon, Settings: st, CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt}
}

// ---- targets ----

func toTargetVersion(v store.TargetVersion) gen.TargetVersion {
	return gen.TargetVersion{Id: v.ID, TargetId: v.TargetID, Number: v.Number, Config: rawOr(v.Config, "{}"),
		CreatedBy: v.CreatedBy, CreatedAt: v.CreatedAt}
}

func toTarget(t store.Target) gen.Target {
	out := gen.Target{Id: t.ID, OrgId: t.OrgID, ProjectId: t.ProjectID, Name: t.Name, Description: t.Description,
		Kind: gen.TargetKind(t.Kind), CreatedAt: t.CreatedAt, UpdatedAt: t.UpdatedAt}
	if t.CurrentVersion != nil {
		out.CurrentVersion = ptr(toTargetVersion(*t.CurrentVersion))
	}
	return out
}

// ---- datasets ----

func toDataset(d store.Dataset) gen.Dataset {
	out := gen.Dataset{Id: d.ID, OrgId: d.OrgID, ProjectId: d.ProjectID, Name: d.Name, Description: d.Description,
		ExampleCount: d.ExampleCount, HasDraftChanges: d.HasDraftChanges(), CreatedAt: d.CreatedAt, UpdatedAt: d.UpdatedAt}
	if s := rawPtr(d.Schema); s != nil {
		out.Schema = s
	}
	if d.LatestVersionID != nil && d.LatestVersionNumber != nil && d.LatestVersionAt != nil {
		out.LatestVersion = &gen.DatasetVersionRef{Id: *d.LatestVersionID, Number: *d.LatestVersionNumber, CreatedAt: *d.LatestVersionAt}
	}
	return out
}

func contextSlice(raw json.RawMessage) *[]string {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var out []string
	if json.Unmarshal(raw, &out) != nil {
		return nil
	}
	return &out
}

func toExample(e store.Example) gen.Example {
	return gen.Example{Id: e.ID, DatasetId: e.DatasetID, RevisionId: e.RevisionID, Input: rawOr(e.Input, "{}"),
		Expected: rawPtr(e.Expected), Context: contextSlice(e.Context), Metadata: rawPtr(e.Metadata),
		Tags: e.Tags, Conversation: rawPtr(e.Conversation), CreatedAt: e.CreatedAt, UpdatedAt: e.UpdatedAt}
}

// fromExampleData converts an API example to store input, validating shapes.
func fromExampleData(d gen.ExampleData) (store.ExampleUpsert, error) {
	var u store.ExampleUpsert
	u.ID = d.Id
	if d.Input != nil {
		in, err := jsonObject(*d.Input, "input")
		if err != nil {
			return u, err
		}
		u.Input = in
	} else {
		u.Input = json.RawMessage(`{}`)
	}
	if d.Expected != nil {
		u.Expected = *d.Expected
	}
	if d.Context != nil {
		u.Context = mustJSON(*d.Context)
	}
	if d.Metadata != nil {
		md, err := jsonObject(*d.Metadata, "metadata")
		if err != nil {
			return u, err
		}
		u.Metadata = md
	}
	if d.Tags != nil {
		u.Tags = normalizeTags(*d.Tags)
	}
	if d.Conversation != nil && len(*d.Conversation) > 0 && string(*d.Conversation) != "null" {
		var c struct {
			Turns      []json.RawMessage `json:"turns"`
			Simulation json.RawMessage   `json:"simulation"`
		}
		if err := json.Unmarshal(*d.Conversation, &c); err != nil {
			return u, invalid("conversation must be an object with turns or simulation")
		}
		if (len(c.Turns) > 0) == (len(c.Simulation) > 0 && string(c.Simulation) != "null") {
			return u, invalid("conversation must set exactly one of turns or simulation")
		}
		u.Conversation = *d.Conversation
	}
	return u, nil
}

func normalizeTags(tags []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(tags))
	for _, t := range tags {
		t = strings.TrimSpace(t)
		if t != "" && !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	return out
}

func toDatasetVersion(v store.DatasetVersion) gen.DatasetVersion {
	return gen.DatasetVersion{Id: v.ID, DatasetId: v.DatasetID, Number: v.Number, Note: v.Note,
		ExampleCount: v.ExampleCount, CreatedBy: v.CreatedBy, CreatedAt: v.CreatedAt}
}

// ---- evaluators ----

var modelEvaluators = map[string]bool{
	"embedding_similarity": true, "llm_rubric": true, "correctness": true, "faithfulness": true, "groundedness": true,
	"answer_relevance": true, "context_relevance": true, "context_recall": true, "hallucination": true,
	"toxicity": true, "bias": true, "conciseness": true, "helpfulness": true, "conversation_goal_completion": true,
	"conversation_coherence": true, "tool_use_correctness": true, "pairwise": true,
}

func evaluatorCategory(kind string) gen.EvaluatorCategory {
	switch {
	case kind == "human":
		return gen.EvaluatorCategory("human")
	case modelEvaluators[kind]:
		return gen.EvaluatorCategory("model")
	}
	return gen.EvaluatorCategory("deterministic")
}

func toEvaluator(e store.Evaluator) gen.Evaluator {
	return gen.Evaluator{Id: e.ID, OrgId: e.OrgID, ProjectId: e.ProjectID, Name: e.Name, Description: e.Description,
		Kind: gen.EvaluatorKind(e.Kind), Category: evaluatorCategory(e.Kind), Config: rawOr(e.Config, "{}"),
		Version: e.Version, CreatedAt: e.CreatedAt, UpdatedAt: e.UpdatedAt}
}

// ---- suites ----

func toSuite(x store.Suite) gen.Suite {
	return gen.Suite{Id: x.ID, OrgId: x.OrgID, ProjectId: x.ProjectID, Name: x.Name, Slug: x.Slug, Description: x.Description,
		Config: rawOr(x.Config, "{}"), BaselineRunId: x.BaselineRunID, CreatedAt: x.CreatedAt, UpdatedAt: x.UpdatedAt}
}

func toGate(g store.Gate) gen.Gate {
	return gen.Gate{Id: g.ID, SuiteId: g.SuiteID, Name: g.Name, Expr: rawOr(g.Expr, "{}"), Text: gateTextRaw(g.Expr),
		Enabled: g.Enabled, CreatedAt: g.CreatedAt, UpdatedAt: g.UpdatedAt}
}

func (s *Server) toTrigger(t store.Trigger) gen.Trigger {
	out := gen.Trigger{Id: t.ID, SuiteId: t.SuiteID, Name: t.Name, Kind: gen.TriggerKind(t.Kind), Config: rawOr(t.Config, "{}"),
		Enabled: t.Enabled, NextFireAt: t.NextFireAt, LastFiredAt: t.LastFiredAt, CreatedAt: t.CreatedAt, UpdatedAt: t.UpdatedAt}
	if t.Kind == "webhook" {
		out.WebhookUrl = ptr(s.webhookURL(t.ID))
	}
	return out
}

func (s *Server) webhookURL(id uuid.UUID) string {
	base := "http://localhost:8080"
	if s.Config != nil && s.Config.Server.BaseURL != "" {
		base = strings.TrimRight(s.Config.Server.BaseURL, "/")
	}
	return base + "/hooks/" + id.String()
}

// ---- runs & results ----

func toRun(r store.Run) gen.Run {
	return gen.Run{Id: r.ID, OrgId: r.OrgID, ProjectId: r.ProjectID, SuiteId: r.SuiteID, TargetId: r.TargetID,
		TargetVersionId: r.TargetVersionID, DatasetVersionId: r.DatasetVersionID, Name: r.Name,
		TriggerKind: gen.TriggerKind(r.TriggerKind), TriggerRef: r.TriggerRef, Labels: gen.Labels(r.Labels),
		Status: gen.RunStatus(r.Status), Total: r.Total, Completed: r.Completed, Failed: r.Failed, Errored: r.Errored,
		StartedAt: r.StartedAt, FinishedAt: r.FinishedAt, BaselineRunId: r.BaselineRunID, ParentRunId: r.ParentRunID,
		GatePassed: r.GatePassed, Summary: rawPtr(r.Summary), CostUsd: r.CostUSD, TokensIn: r.TokensIn,
		TokensOut: r.TokensOut, Error: r.Error, CreatedBy: r.CreatedBy, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt}
}

func toScore(c store.Score) gen.Score {
	return gen.Score{EvaluatorId: c.EvaluatorID, Value: c.Value, Label: c.Label, Passed: c.Passed, Reason: c.Reason,
		Meta: rawPtr(c.Meta), LatencyMs: ptr(c.LatencyMs), CostUsd: ptr(c.CostUSD)}
}

func toResult(r store.Result, full bool) gen.Result {
	out := gen.Result{Id: r.ID, RunId: r.RunID, ExampleId: r.ExampleID, ExampleRevisionId: r.ExampleRevisionID,
		Trial: r.Trial, Status: gen.ResultStatus(r.Status), Input: rawPtr(r.Input), Expected: rawPtr(r.Expected),
		Output: r.Output, OutputJson: rawPtr(r.OutputJSON), Context: rawPtr(r.Context), ToolCalls: rawPtr(r.ToolCalls),
		Trace: rawPtr(r.Trace), Transcript: rawPtr(r.Transcript), LatencyMs: r.LatencyMs, TtftMs: r.TTFTMs,
		TokensIn: r.TokensIn, TokensOut: r.TokensOut, CostUsd: r.CostUSD, Error: r.Error, Cached: r.Cached,
		Tags: r.Tags, Scores: mapSlice(r.Scores, toScore), CreatedAt: r.CreatedAt}
	if out.Tags == nil {
		out.Tags = []string{}
	}
	if full {
		out.Request, out.Response = rawPtr(r.Request), rawPtr(r.Response)
	}
	return out
}

func toAggregate(a store.RunAggregate) gen.RunAggregate {
	out := gen.RunAggregate{RunId: a.RunID, Metric: a.Metric, SliceKey: a.SliceKey, N: a.N, Mean: a.Mean, Min: a.Min,
		Max: a.Max, P50: a.P50, P90: a.P90, P95: a.P95, P99: a.P99, Stddev: a.Stddev, Sum: a.Sum, PassRate: a.PassRate}
	if a.EvaluatorID != store.NilID {
		out.EvaluatorId = ptr(a.EvaluatorID)
	}
	if len(a.Histogram) > 0 {
		var h gen.Histogram
		if json.Unmarshal(a.Histogram, &h) == nil {
			out.Histogram = &h
		}
	}
	return out
}

// ---- review ----

func toQueue(q store.ReviewQueue) gen.ReviewQueue {
	return gen.ReviewQueue{Id: q.ID, ProjectId: q.ProjectID, Name: q.Name, Description: q.Description,
		EvaluatorId: q.EvaluatorID, Config: rawOr(q.Config, "{}"), PendingCount: q.PendingCount, DoneCount: q.DoneCount,
		CreatedAt: q.CreatedAt, UpdatedAt: q.UpdatedAt}
}

func toReviewItem(it store.ReviewItem) gen.ReviewItem {
	return gen.ReviewItem{Id: it.ID, QueueId: it.QueueID, RunId: it.RunID, ResultId: it.ResultID,
		Status: gen.ReviewItemStatus(it.Status), AssignedTo: it.AssignedTo, CompletedBy: it.CompletedBy,
		CompletedAt: it.CompletedAt, CreatedAt: it.CreatedAt, UpdatedAt: it.UpdatedAt}
}

func toAnnotation(a store.Annotation) gen.Annotation {
	return gen.Annotation{Id: a.ID, ProjectId: a.ProjectID, ResultId: a.ResultID, ReviewItemId: a.ReviewItemID,
		UserId: a.UserID, EvaluatorId: a.EvaluatorID, Value: a.Value, Label: a.Label, Passed: a.Passed,
		Comment: a.Comment, CreatedAt: a.CreatedAt, UpdatedAt: a.UpdatedAt}
}

func toComparison(c store.Comparison) gen.Comparison {
	return gen.Comparison{Id: c.ID, ProjectId: c.ProjectID, Name: c.Name, RunIds: c.RunIDs, CreatedBy: c.CreatedBy,
		CreatedAt: c.CreatedAt}
}

// gateTextRaw renders a stored gate AST (evaluators appear as ids; handlers substitute names).
func gateTextRaw(raw json.RawMessage) string {
	var n aggregate.Node
	if json.Unmarshal(raw, &n) != nil {
		return ""
	}
	return n.String()
}
