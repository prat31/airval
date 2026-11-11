// Package storetest is the shared contract test suite every store.Store implementation must pass.
package storetest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/airval/airval/internal/store"
)

// Opener returns a fresh, migrated, empty store for one test.
type Opener func(t *testing.T) store.Store

// Run executes the contract suite.
func Run(t *testing.T, open Opener) {
	tests := []struct {
		name string
		fn   func(t *testing.T, s store.Store)
	}{
		{"OrgsUsersMembers", testOrgsUsersMembers},
		{"SessionsAPIKeys", testSessionsAPIKeys},
		{"ProjectsPagination", testProjectsPagination},
		{"Secrets", testSecrets},
		{"TargetsVersions", testTargets},
		{"DatasetsExamplesVersions", testDatasets},
		{"EvaluatorsSuitesGatesTriggers", testSuites},
		{"RunsFilters", testRuns},
		{"ResultsHotPath", testResults},
		{"Aggregates", testAggregates},
		{"ReviewAnnotations", testReview},
		{"MiscComparisonsChannelsAuditCache", testMisc},
		{"Transactions", testTx},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := open(t)
			tc.fn(t, s)
		})
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func eq[T comparable](t *testing.T, what string, got, want T) {
	t.Helper()
	if got != want {
		t.Fatalf("%s: got %v, want %v", what, got, want)
	}
}

func ptr[T any](v T) *T { return &v }

type fixture struct {
	org  store.Organization
	user store.User
	proj store.Project
}

func setup(t *testing.T, s store.Store) fixture {
	t.Helper()
	ctx := context.Background()
	f := fixture{}
	f.org = store.Organization{Name: "Acme", Slug: "acme-" + uuid.NewString()[:8]}
	must(t, s.CreateOrg(ctx, &f.org))
	f.user = store.User{Email: fmt.Sprintf("U-%s@Example.com", uuid.NewString()[:8]), Name: "U"}
	must(t, s.CreateUser(ctx, &f.user))
	must(t, s.UpsertMembership(ctx, &store.Membership{OrgID: f.org.ID, UserID: f.user.ID, Role: store.RoleOwner}))
	f.proj = store.Project{OrgID: f.org.ID, Name: "Chatbot", Slug: "chatbot"}
	must(t, s.CreateProject(ctx, &f.proj))
	return f
}

func testOrgsUsersMembers(t *testing.T, s store.Store) {
	ctx := context.Background()
	f := setup(t, s)
	got, err := s.GetOrgBySlug(ctx, f.org.Slug)
	must(t, err)
	eq(t, "org id", got.ID, f.org.ID)
	if _, err := s.GetOrg(ctx, uuid.New()); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	dup := store.Organization{Name: "x", Slug: f.org.Slug}
	if err := s.CreateOrg(ctx, &dup); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("want ErrConflict, got %v", err)
	}
	u, err := s.GetUserByEmail(ctx, strings.ToUpper(f.user.Email))
	must(t, err)
	eq(t, "email lowercased", u.Email, strings.ToLower(f.user.Email))
	hash := "argon"
	u.PasswordHash = &hash
	must(t, s.UpdateUser(ctx, &u))
	u, _ = s.GetUser(ctx, u.ID)
	eq(t, "hash", *u.PasswordHash, "argon")

	n, err := s.CountUsers(ctx)
	must(t, err)
	eq(t, "users", n, 1)

	u2 := store.User{Email: "b@example.com"}
	must(t, s.CreateUser(ctx, &u2))
	must(t, s.UpsertMembership(ctx, &store.Membership{OrgID: f.org.ID, UserID: u2.ID, Role: store.RoleViewer}))
	must(t, s.UpsertMembership(ctx, &store.Membership{OrgID: f.org.ID, UserID: u2.ID, Role: store.RoleEditor}))
	m, err := s.GetMembership(ctx, f.org.ID, u2.ID)
	must(t, err)
	eq(t, "role", m.Role, store.RoleEditor)
	page, err := s.ListMembers(ctx, f.org.ID, store.ListParams{Limit: 1})
	must(t, err)
	eq(t, "page size", len(page.Items), 1)
	if page.NextCursor == "" {
		t.Fatal("expected next cursor")
	}
	page2, err := s.ListMembers(ctx, f.org.ID, store.ListParams{Limit: 1, Cursor: page.NextCursor})
	must(t, err)
	eq(t, "page2 size", len(page2.Items), 1)
	eq(t, "page2 last", page2.NextCursor, "")
	if page.Items[0].UserID == page2.Items[0].UserID {
		t.Fatal("pages overlap")
	}
	owners, _ := s.CountOwners(ctx, f.org.ID)
	eq(t, "owners", owners, 1)
	orgs, err := s.ListUserOrgs(ctx, u2.ID)
	must(t, err)
	eq(t, "user orgs", len(orgs), 1)
	eq(t, "user org role", orgs[0].Role, store.RoleEditor)
	must(t, s.DeleteMembership(ctx, f.org.ID, u2.ID))
	if err := s.DeleteMembership(ctx, f.org.ID, u2.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	f.org.Name = "Acme 2"
	must(t, s.UpdateOrg(ctx, &f.org))
	must(t, s.DeleteOrg(ctx, f.org.ID))
	if _, err := s.GetProject(ctx, f.proj.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("project should cascade-delete with org")
	}
}

func testSessionsAPIKeys(t *testing.T, s store.Store) {
	ctx := context.Background()
	f := setup(t, s)
	se := store.Session{UserID: f.user.ID, TokenHash: "h1", CSRFToken: "c", ExpiresAt: time.Now().Add(time.Hour)}
	must(t, s.CreateSession(ctx, &se))
	got, err := s.GetSessionByTokenHash(ctx, "h1")
	must(t, err)
	eq(t, "session", got.ID, se.ID)
	must(t, s.TouchSession(ctx, se.ID, time.Now(), time.Now().Add(-time.Minute)))
	n, err := s.DeleteExpiredSessions(ctx, time.Now())
	must(t, err)
	eq(t, "expired", n, 1)

	k := store.APIKey{OrgID: f.org.ID, ProjectID: &f.proj.ID, Name: "ci", Prefix: "airv_abc", Hash: "kh", Role: store.RoleEditor}
	must(t, s.CreateAPIKey(ctx, &k))
	gk, err := s.GetAPIKeyByHash(ctx, "kh")
	must(t, err)
	eq(t, "key project", *gk.ProjectID, f.proj.ID)
	if gk.LastUsedAt != nil {
		t.Fatal("last used should be nil")
	}
	must(t, s.TouchAPIKey(ctx, k.ID, time.Now()))
	gk, _ = s.GetAPIKeyByHash(ctx, "kh")
	if gk.LastUsedAt == nil {
		t.Fatal("last used should be set")
	}
	page, err := s.ListAPIKeys(ctx, f.org.ID, store.ListParams{})
	must(t, err)
	eq(t, "keys", len(page.Items), 1)
	must(t, s.DeleteAPIKey(ctx, f.org.ID, k.ID))
}

func testProjectsPagination(t *testing.T, s store.Store) {
	ctx := context.Background()
	f := setup(t, s)
	for i := 0; i < 6; i++ {
		must(t, s.CreateProject(ctx, &store.Project{OrgID: f.org.ID, Name: fmt.Sprint("p", i), Slug: fmt.Sprint("p", i)}))
	}
	seen := map[uuid.UUID]bool{}
	cursor := ""
	pages := 0
	for {
		pg, err := s.ListProjects(ctx, store.ProjectFilter{OrgIDs: []uuid.UUID{f.org.ID}}, store.ListParams{Limit: 3, Cursor: cursor})
		must(t, err)
		pages++
		for _, p := range pg.Items {
			if seen[p.ID] {
				t.Fatal("duplicate across pages")
			}
			seen[p.ID] = true
		}
		if pg.NextCursor == "" {
			break
		}
		cursor = pg.NextCursor
	}
	eq(t, "projects", len(seen), 7)
	eq(t, "pages", pages, 3)
	if _, err := s.ListProjects(ctx, store.ProjectFilter{OrgIDs: []uuid.UUID{f.org.ID}}, store.ListParams{Cursor: "!!"}); !errors.Is(err, store.ErrInvalidCursor) {
		t.Fatalf("want invalid cursor, got %v", err)
	}
	pg, err := s.ListProjects(ctx, store.ProjectFilter{OrgIDs: []uuid.UUID{f.org.ID}, Slug: "chatbot"}, store.ListParams{})
	must(t, err)
	eq(t, "by slug", len(pg.Items), 1)
	dup := store.Project{OrgID: f.org.ID, Name: "x", Slug: "chatbot"}
	if err := s.CreateProject(ctx, &dup); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("want conflict, got %v", err)
	}
	p := pg.Items[0]
	p.Settings = json.RawMessage(`{"retention_days":30}`)
	must(t, s.UpdateProject(ctx, &p))
	p, _ = s.GetProject(ctx, p.ID)
	var st map[string]int
	must(t, json.Unmarshal(p.Settings, &st))
	eq(t, "settings", st["retention_days"], 30)
}

func testSecrets(t *testing.T, s store.Store) {
	ctx := context.Background()
	f := setup(t, s)
	orgSec := store.Secret{OrgID: f.org.ID, Name: "OPENAI_KEY", Ciphertext: []byte{1}, Nonce: []byte{2}, KeyVersion: 1}
	must(t, s.CreateSecret(ctx, &orgSec))
	projSec := store.Secret{OrgID: f.org.ID, ProjectID: &f.proj.ID, Name: "OPENAI_KEY", Ciphertext: []byte{3}, Nonce: []byte{4}, KeyVersion: 2}
	must(t, s.CreateSecret(ctx, &projSec))
	dup := store.Secret{OrgID: f.org.ID, Name: "OPENAI_KEY", Ciphertext: []byte{1}, Nonce: []byte{2}, KeyVersion: 1}
	if err := s.CreateSecret(ctx, &dup); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("want conflict on org-level duplicate, got %v", err)
	}
	got, err := s.ResolveSecret(ctx, f.org.ID, &f.proj.ID, "OPENAI_KEY")
	must(t, err)
	eq(t, "project secret preferred", got.ID, projSec.ID)
	other := uuid.New()
	got, err = s.ResolveSecret(ctx, f.org.ID, &other, "OPENAI_KEY")
	must(t, err)
	eq(t, "fallback to org", got.ID, orgSec.ID)
	pg, err := s.ListSecrets(ctx, store.SecretFilter{OrgID: f.org.ID}, store.ListParams{})
	must(t, err)
	eq(t, "org secrets", len(pg.Items), 1)
	pg, _ = s.ListSecrets(ctx, store.SecretFilter{OrgID: f.org.ID, AllScopes: true}, store.ListParams{})
	eq(t, "all secrets", len(pg.Items), 2)
	stale, err := s.ListSecretsNotAtKeyVersion(ctx, 2, 10)
	must(t, err)
	eq(t, "stale", len(stale), 1)
	orgSec.KeyVersion = 2
	must(t, s.UpdateSecret(ctx, &orgSec))
	stale, _ = s.ListSecretsNotAtKeyVersion(ctx, 2, 10)
	eq(t, "stale after", len(stale), 0)
	must(t, s.DeleteSecret(ctx, orgSec.ID))
}

func testTargets(t *testing.T, s store.Store) {
	ctx := context.Background()
	f := setup(t, s)
	tg := store.Target{OrgID: f.org.ID, ProjectID: f.proj.ID, Name: "prod", Kind: "http"}
	must(t, s.CreateTarget(ctx, &tg, []byte(`{"url":"http://a"}`), &f.user.ID))
	if tg.CurrentVersion == nil || tg.CurrentVersion.Number != 1 {
		t.Fatal("expected version 1")
	}
	v2, err := s.AddTargetVersion(ctx, tg.ID, []byte(`{"url":"http://b"}`), nil)
	must(t, err)
	eq(t, "v2", v2.Number, 2)
	got, err := s.GetTarget(ctx, tg.ID)
	must(t, err)
	eq(t, "current", *got.CurrentVersionID, v2.ID)
	var cfg map[string]string
	must(t, json.Unmarshal(got.CurrentVersion.Config, &cfg))
	eq(t, "config", cfg["url"], "http://b")
	vs, err := s.ListTargetVersions(ctx, tg.ID, store.ListParams{})
	must(t, err)
	eq(t, "versions", len(vs.Items), 2)
	eq(t, "newest first", vs.Items[0].Number, 2)
	gv, err := s.GetTargetVersion(ctx, vs.Items[1].ID)
	must(t, err)
	eq(t, "v1", gv.Number, 1)
	got.Name = "production"
	must(t, s.UpdateTarget(ctx, &got))
	lst, _ := s.ListTargets(ctx, f.proj.ID, store.ListParams{})
	eq(t, "targets", len(lst.Items), 1)
	eq(t, "renamed", lst.Items[0].Name, "production")
	if _, err := s.AddTargetVersion(ctx, uuid.New(), []byte(`{}`), nil); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("want not found, got %v", err)
	}
	must(t, s.DeleteTarget(ctx, tg.ID))
}

func newDataset(t *testing.T, s store.Store, f fixture) store.Dataset {
	d := store.Dataset{OrgID: f.org.ID, ProjectID: f.proj.ID, Name: "qa-" + uuid.NewString()[:6]}
	must(t, s.CreateDataset(context.Background(), &d))
	return d
}

func testDatasets(t *testing.T, s store.Store) {
	ctx := context.Background()
	f := setup(t, s)
	d := newDataset(t, s, f)
	items := []store.ExampleUpsert{
		{ExampleData: store.ExampleData{Input: json.RawMessage(`{"q":"capital of France?"}`), Expected: json.RawMessage(`"Paris"`), Tags: []string{"geo"}}},
		{ExampleData: store.ExampleData{Input: json.RawMessage(`{"q":"2+2"}`), Expected: json.RawMessage(`"4"`), Tags: []string{"math"}}},
		{ExampleData: store.ExampleData{Input: json.RawMessage(`{"q":"hi"}`), Conversation: json.RawMessage(`{"turns":[{"role":"user","content":"hi"}]}`)}},
	}
	exs, err := s.UpsertExamples(ctx, d.ID, items)
	must(t, err)
	eq(t, "inserted", len(exs), 3)
	gd, _ := s.GetDataset(ctx, d.ID)
	eq(t, "count", gd.ExampleCount, 3)
	if !gd.HasDraftChanges() {
		t.Fatal("expected draft changes")
	}
	v1, err := s.PublishDatasetVersion(ctx, d.ID, "first", &f.user.ID)
	must(t, err)
	eq(t, "v1 number", v1.Number, 1)
	eq(t, "v1 count", v1.ExampleCount, 3)
	gd, _ = s.GetDataset(ctx, d.ID)
	if gd.HasDraftChanges() {
		t.Fatal("no draft changes after publish")
	}
	eq(t, "latest number", *gd.LatestVersionNumber, 1)

	// Unchanged upsert keeps the revision; changed upsert creates a new one.
	same, err := s.UpsertExamples(ctx, d.ID, []store.ExampleUpsert{{ID: &exs[0].ID, ExampleData: store.ExampleData{
		Input: json.RawMessage(`{ "q" : "capital of France?" }`), Expected: json.RawMessage(`"Paris"`), Tags: []string{"geo"}}}})
	must(t, err)
	eq(t, "same revision", same[0].RevisionID, exs[0].RevisionID)
	changed, err := s.UpsertExamples(ctx, d.ID, []store.ExampleUpsert{{ID: &exs[1].ID, ExampleData: store.ExampleData{
		Input: json.RawMessage(`{"q":"2+2"}`), Expected: json.RawMessage(`"four"`), Tags: []string{"math", "edited"}}}})
	must(t, err)
	if changed[0].RevisionID == exs[1].RevisionID {
		t.Fatal("expected new revision")
	}
	n, err := s.DeleteExamples(ctx, d.ID, []uuid.UUID{exs[2].ID})
	must(t, err)
	eq(t, "deleted", n, 1)
	if _, err := s.GetExample(ctx, exs[2].ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("deleted example should be hidden")
	}
	v2, err := s.PublishDatasetVersion(ctx, d.ID, "", nil)
	must(t, err)
	eq(t, "v2 count", v2.ExampleCount, 2)

	// Version 1 is immutable: still 3 examples with the original "4".
	old, err := s.ListExamples(ctx, store.ExampleFilter{DatasetID: d.ID, VersionID: &v1.ID}, store.ListParams{})
	must(t, err)
	eq(t, "v1 examples", len(old.Items), 3)
	for _, e := range old.Items {
		if e.ID == exs[1].ID && string(canonical(e.Expected)) != `"4"` {
			t.Fatalf("v1 changed: %s", e.Expected)
		}
	}
	draft, err := s.ListExamples(ctx, store.ExampleFilter{DatasetID: d.ID, Tag: "edited"}, store.ListParams{})
	must(t, err)
	eq(t, "tag filter", len(draft.Items), 1)
	srch, err := s.ListExamples(ctx, store.ExampleFilter{DatasetID: d.ID, Search: "FRANCE"}, store.ListParams{})
	must(t, err)
	eq(t, "search", len(srch.Items), 1)
	items2, err := s.ListDatasetVersionItems(ctx, v2.ID)
	must(t, err)
	eq(t, "items", len(items2), 2)
	revs, err := s.GetExampleRevisions(ctx, []uuid.UUID{items2[0].RevisionID, items2[1].RevisionID})
	must(t, err)
	eq(t, "revisions", len(revs), 2)
	vb, err := s.GetDatasetVersionByNumber(ctx, d.ID, 2)
	must(t, err)
	eq(t, "by number", vb.ID, v2.ID)
	vl, _ := s.ListDatasetVersions(ctx, d.ID, store.ListParams{})
	eq(t, "versions", len(vl.Items), 2)

	// Revive a deleted example by upserting its id.
	revived, err := s.UpsertExamples(ctx, d.ID, []store.ExampleUpsert{{ID: &exs[2].ID, ExampleData: store.ExampleData{
		Input: json.RawMessage(`{"q":"hi"}`), Conversation: json.RawMessage(`{"turns":[{"role":"user","content":"hi"}]}`)}}})
	must(t, err)
	eq(t, "revived id", revived[0].ID, exs[2].ID)
	if _, err := s.GetExample(ctx, exs[2].ID); err != nil {
		t.Fatalf("revived: %v", err)
	}

	// Bulk: 1200 examples in one call (exercises chunking).
	bulk := make([]store.ExampleUpsert, 1200)
	for i := range bulk {
		bulk[i].Input = json.RawMessage(fmt.Sprintf(`{"i":%d}`, i))
	}
	_, err = s.UpsertExamples(ctx, d.ID, bulk)
	must(t, err)
	gd, _ = s.GetDataset(ctx, d.ID)
	eq(t, "bulk count", gd.ExampleCount, 1203)
	gd.Description = "desc"
	must(t, s.UpdateDataset(ctx, &gd))
	dl, _ := s.ListDatasets(ctx, f.proj.ID, store.ListParams{})
	eq(t, "datasets", len(dl.Items), 1)
	must(t, s.DeleteDataset(ctx, d.ID))
}

func canonical(raw json.RawMessage) []byte {
	var v any
	_ = json.Unmarshal(raw, &v)
	b, _ := json.Marshal(v)
	return b
}

func testSuites(t *testing.T, s store.Store) {
	ctx := context.Background()
	f := setup(t, s)
	ev := store.Evaluator{OrgID: f.org.ID, ProjectID: f.proj.ID, Name: "exact", Kind: "equals", Config: json.RawMessage(`{"field":"output"}`)}
	must(t, s.CreateEvaluator(ctx, &ev))
	eq(t, "ev version", ev.Version, 1)
	ev.Version++
	must(t, s.UpdateEvaluator(ctx, &ev))
	evs, err := s.GetEvaluators(ctx, []uuid.UUID{ev.ID})
	must(t, err)
	eq(t, "ev v2", evs[0].Version, 2)
	el, _ := s.ListEvaluators(ctx, f.proj.ID, store.ListParams{})
	eq(t, "evaluators", len(el.Items), 1)

	su := store.Suite{OrgID: f.org.ID, ProjectID: f.proj.ID, Name: "Smoke", Slug: "smoke", Config: json.RawMessage(`{"trials":1}`)}
	must(t, s.CreateSuite(ctx, &su))
	bs, err := s.GetSuiteBySlug(ctx, f.proj.ID, "smoke")
	must(t, err)
	eq(t, "suite by slug", bs.ID, su.ID)
	sl, _ := s.ListSuites(ctx, f.proj.ID, store.ListParams{})
	eq(t, "suites", len(sl.Items), 1)
	su.Description = "d"
	must(t, s.UpdateSuite(ctx, &su))

	g := store.Gate{OrgID: f.org.ID, ProjectID: f.proj.ID, SuiteID: su.ID, Name: "quality", Expr: json.RawMessage(`{"op":"and","args":[]}`), Enabled: true}
	must(t, s.CreateGate(ctx, &g))
	gl, _ := s.ListGates(ctx, su.ID)
	eq(t, "gates", len(gl), 1)
	g.Enabled = false
	must(t, s.UpdateGate(ctx, &g))
	gg, _ := s.GetGate(ctx, g.ID)
	eq(t, "gate disabled", gg.Enabled, false)

	past := time.Now().Add(-time.Minute).UTC()
	future := time.Now().Add(time.Hour).UTC()
	tr := store.Trigger{OrgID: f.org.ID, ProjectID: f.proj.ID, SuiteID: su.ID, Kind: "schedule", Enabled: true,
		Config: json.RawMessage(`{"cron":"*/5 * * * *","timezone":"UTC"}`), NextFireAt: &past}
	must(t, s.CreateTrigger(ctx, &tr))
	tr2 := store.Trigger{OrgID: f.org.ID, ProjectID: f.proj.ID, SuiteID: su.ID, Kind: "schedule", Enabled: true, NextFireAt: &future}
	must(t, s.CreateTrigger(ctx, &tr2))
	wh := store.Trigger{OrgID: f.org.ID, ProjectID: f.proj.ID, SuiteID: su.ID, Kind: "webhook", Enabled: true, SecretEnc: []byte{9, 9}, SecretKeyVersion: 1}
	must(t, s.CreateTrigger(ctx, &wh))
	due, err := s.ListDueTriggers(ctx, time.Now(), 10)
	must(t, err)
	eq(t, "due", len(due), 1)
	eq(t, "due id", due[0].ID, tr.ID)
	ok, err := s.MarkTriggerFired(ctx, tr.ID, *due[0].NextFireAt, time.Now(), &future)
	must(t, err)
	eq(t, "fired", ok, true)
	ok, err = s.MarkTriggerFired(ctx, tr.ID, *due[0].NextFireAt, time.Now(), &future)
	must(t, err)
	eq(t, "second fire loses race", ok, false)
	due, _ = s.ListDueTriggers(ctx, time.Now(), 10)
	eq(t, "none due", len(due), 0)
	gw, _ := s.GetTrigger(ctx, wh.ID)
	eq(t, "secret", len(gw.SecretEnc), 2)
	tl, _ := s.ListTriggers(ctx, su.ID)
	eq(t, "triggers", len(tl), 3)
	gw.Enabled = false
	must(t, s.UpdateTrigger(ctx, &gw))
	must(t, s.DeleteTrigger(ctx, wh.ID))
	must(t, s.DeleteGate(ctx, g.ID))
	must(t, s.DeleteSuite(ctx, su.ID))
	must(t, s.DeleteEvaluator(ctx, ev.ID))
}

func newRun(t *testing.T, s store.Store, f fixture, mut func(*store.Run)) store.Run {
	r := store.Run{OrgID: f.org.ID, ProjectID: f.proj.ID, Labels: map[string]string{}}
	if mut != nil {
		mut(&r)
	}
	must(t, s.CreateRun(context.Background(), &r))
	return r
}

func testRuns(t *testing.T, s store.Store) {
	ctx := context.Background()
	f := setup(t, s)
	su := store.Suite{OrgID: f.org.ID, ProjectID: f.proj.ID, Name: "S", Slug: "s"}
	must(t, s.CreateSuite(ctx, &su))
	r1 := newRun(t, s, f, func(r *store.Run) {
		r.SuiteID = &su.ID
		r.Labels = map[string]string{"branch": "main", "git_sha": "abc"}
	})
	r2 := newRun(t, s, f, func(r *store.Run) {
		r.SuiteID = &su.ID
		r.TriggerKind = "api"
		r.Labels = map[string]string{"branch": "dev"}
	})
	_ = newRun(t, s, f, nil)
	eq(t, "default status", r1.Status, store.RunQueued)

	pg, err := s.ListRuns(ctx, store.RunFilter{ProjectID: &f.proj.ID}, store.ListParams{})
	must(t, err)
	eq(t, "all runs", len(pg.Items), 3)
	pg, _ = s.ListRuns(ctx, store.RunFilter{SuiteID: &su.ID}, store.ListParams{})
	eq(t, "suite runs", len(pg.Items), 2)
	eq(t, "newest first", pg.Items[0].ID, r2.ID)
	pg, err = s.ListRuns(ctx, store.RunFilter{ProjectID: &f.proj.ID, Labels: map[string]string{"branch": "main"}}, store.ListParams{})
	must(t, err)
	eq(t, "label filter", len(pg.Items), 1)
	eq(t, "label match", pg.Items[0].Labels["git_sha"], "abc")
	pg, _ = s.ListRuns(ctx, store.RunFilter{OrgIDs: []uuid.UUID{f.org.ID}, TriggerKind: "api"}, store.ListParams{})
	eq(t, "trigger filter", len(pg.Items), 1)

	ok, err := s.TransitionRun(ctx, r1.ID, []string{store.RunQueued}, store.RunRunning, time.Now())
	must(t, err)
	eq(t, "transition", ok, true)
	ok, _ = s.TransitionRun(ctx, r1.ID, []string{store.RunQueued}, store.RunRunning, time.Now())
	eq(t, "transition guarded", ok, false)
	must(t, s.AddRunProgress(ctx, r1.ID, store.RunProgress{Completed: 5, Failed: 2, CostUSD: 0.5, TokensIn: 100}))
	must(t, s.AddRunProgress(ctx, r1.ID, store.RunProgress{Completed: 5, Errored: 1, CostUSD: 0.25}))
	got, _ := s.GetRun(ctx, r1.ID)
	eq(t, "completed", got.Completed, 10)
	eq(t, "failed", got.Failed, 2)
	eq(t, "errored", got.Errored, 1)
	eq(t, "cost", got.CostUSD, 0.75)
	if got.StartedAt == nil {
		t.Fatal("started_at should be set")
	}
	pg, _ = s.ListRuns(ctx, store.RunFilter{ProjectID: &f.proj.ID, Statuses: []string{store.RunRunning}}, store.ListParams{})
	eq(t, "status filter", len(pg.Items), 1)

	_, _ = s.TransitionRun(ctx, r1.ID, nil, store.RunSucceeded, time.Now())
	got, _ = s.GetRun(ctx, r1.ID)
	got.Status = store.RunSucceeded
	got.GatePassed = ptr(true)
	got.Summary = json.RawMessage(`{"pass_rate":0.9}`)
	got.Completed = 10
	must(t, s.UpdateRun(ctx, &got))
	latest, err := s.LatestRun(ctx, store.RunFilter{SuiteID: &su.ID, Statuses: []string{store.RunSucceeded}})
	must(t, err)
	eq(t, "latest passing", latest.ID, r1.ID)
	eq(t, "gate", *latest.GatePassed, true)
	if latest.FinishedAt == nil {
		t.Fatal("finished_at should be set")
	}
	must(t, s.DeleteRun(ctx, r2.ID))
}

func seedRun(t *testing.T, s store.Store) (fixture, store.Run, []store.Example, uuid.UUID, uuid.UUID) {
	ctx := context.Background()
	f := setup(t, s)
	d := newDataset(t, s, f)
	in := make([]store.ExampleUpsert, 10)
	for i := range in {
		tag := "even"
		if i%2 == 1 {
			tag = "odd"
		}
		in[i].Input = json.RawMessage(fmt.Sprintf(`{"q":"question %d"}`, i))
		in[i].Tags = []string{tag}
	}
	exs, err := s.UpsertExamples(ctx, d.ID, in)
	must(t, err)
	run := newRun(t, s, f, nil)
	return f, run, exs, store.NewID(), store.NewID()
}

func makeResults(run store.Run, exs []store.Example, evA, evB uuid.UUID, trial int) ([]store.Result, []store.Score) {
	var rs []store.Result
	var ss []store.Score
	for i, e := range exs {
		st := store.ResultPassed
		if i%3 == 0 {
			st = store.ResultFailed
		}
		r := store.Result{ID: store.NewID(), RunID: run.ID, ExampleID: e.ID, ExampleRevisionID: e.RevisionID, Trial: trial,
			Status: st, Output: fmt.Sprintf("answer %d", i), LatencyMs: float64(10 + i), TokensIn: 5, TokensOut: 7,
			ToolCalls: json.RawMessage(`[{"name":"search","arguments":{"q":"x"}}]`)}
		if i == 0 {
			r.Output = strings.Repeat("big output ", 2000) // > 8 KiB → compressed
		}
		rs = append(rs, r)
		ss = append(ss,
			store.Score{ResultID: r.ID, EvaluatorID: evA, RunID: run.ID, Value: ptr(float64(i) / 10), Passed: ptr(st == store.ResultPassed)},
			store.Score{ResultID: r.ID, EvaluatorID: evB, RunID: run.ID, Label: "ok", Reason: "fine", Meta: json.RawMessage(`{"k":1}`)})
	}
	return rs, ss
}

func testResults(t *testing.T, s store.Store) {
	ctx := context.Background()
	_, run, exs, evA, evB := seedRun(t, s)
	rs, ss := makeResults(run, exs, evA, evB, 0)
	must(t, s.InsertResults(ctx, rs, ss))

	got, err := s.GetResult(ctx, rs[0].ID)
	must(t, err)
	eq(t, "compressed output roundtrip", got.Output, rs[0].Output)
	eq(t, "scores", len(got.Scores), 2)
	eq(t, "tags", got.Tags[0], "even")
	if !strings.Contains(string(got.Input), "question 0") {
		t.Fatalf("input not joined: %s", got.Input)
	}
	many, err := s.GetResults(ctx, []uuid.UUID{rs[1].ID, rs[2].ID, uuid.New()})
	must(t, err)
	eq(t, "get results", len(many), 2)
	eq(t, "get results scores", len(many[0].Scores), 2)

	counts, err := s.CountResults(ctx, run.ID)
	must(t, err)
	eq(t, "failed count", counts[store.ResultFailed], 4)
	eq(t, "passed count", counts[store.ResultPassed], 6)

	// Re-delivery: same (example, trial) with new ids replaces the old rows.
	rs2, ss2 := makeResults(run, exs[:3], evA, evB, 0)
	for i := range rs2 {
		rs2[i].Status = store.ResultErrored
		rs2[i].Error = "boom"
	}
	must(t, s.InsertResults(ctx, rs2, ss2))
	counts, _ = s.CountResults(ctx, run.ID)
	eq(t, "errored after replace", counts[store.ResultErrored], 3)
	total := 0
	for _, n := range counts {
		total += n
	}
	eq(t, "no duplicates", total, 10)
	if _, err := s.GetResult(ctx, rs[0].ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("replaced result should be gone, got %v", err)
	}

	list := func(f store.ResultFilter) []store.Result {
		t.Helper()
		f.RunID = run.ID
		pg, err := s.ListResults(ctx, f, store.ListParams{Limit: 100})
		must(t, err)
		return pg.Items
	}
	eq(t, "status filter", len(list(store.ResultFilter{Statuses: []string{store.ResultErrored}})), 3)
	eq(t, "tag filter", len(list(store.ResultFilter{Tag: "odd"})), 5)
	eq(t, "passed filter", len(list(store.ResultFilter{EvaluatorID: &evA, Passed: ptr(true)})), 6)
	eq(t, "score range", len(list(store.ResultFilter{EvaluatorID: &evA, ScoreMin: ptr(0.5), ScoreMax: ptr(0.8)})), 4)
	eq(t, "search output", len(list(store.ResultFilter{Search: "ANSWER 7"})), 1)
	eq(t, "search input", len(list(store.ResultFilter{Search: "question 4"})), 1)
	all := list(store.ResultFilter{})
	eq(t, "all", len(all), 10)
	for _, r := range all {
		eq(t, "scores attached", len(r.Scores), 2)
	}
	pg, err := s.ListResults(ctx, store.ResultFilter{RunID: run.ID}, store.ListParams{Limit: 4})
	must(t, err)
	eq(t, "page", len(pg.Items), 4)
	pg2, err := s.ListResults(ctx, store.ResultFilter{RunID: run.ID}, store.ListParams{Limit: 4, Cursor: pg.NextCursor})
	must(t, err)
	if pg2.Items[0].ID.String() <= pg.Items[3].ID.String() {
		t.Fatal("cursor order broken")
	}

	n, scores := 0, 0
	must(t, s.IterateRunResults(ctx, run.ID, func(r store.Result) error {
		n++
		scores += len(r.Scores)
		return nil
	}))
	eq(t, "iterated", n, 10)
	eq(t, "iterated scores", scores, 20)

	// Re-score: overwrite evaluator A scores.
	var upd []store.Score
	for _, r := range all {
		upd = append(upd, store.Score{ResultID: r.ID, EvaluatorID: evA, RunID: run.ID, Value: ptr(1.0), Passed: ptr(true)})
	}
	must(t, s.UpsertScores(ctx, upd))
	eq(t, "rescored", len(list(store.ResultFilter{EvaluatorID: &evA, Passed: ptr(true)})), 10)

	// Larger batch exercises chunking (> resultBatch rows).
	big := newRun(t, s, fixture{org: store.Organization{ID: run.OrgID}, proj: store.Project{ID: run.ProjectID}}, nil)
	var brs []store.Result
	var bss []store.Score
	for trial := 0; trial < 50; trial++ {
		r, sc := makeResults(big, exs, evA, evB, trial)
		brs, bss = append(brs, r...), append(bss, sc...)
	}
	start := time.Now()
	must(t, s.InsertResults(ctx, brs, bss))
	t.Logf("%s: inserted %d results + %d scores in %s", s.Dialect(), len(brs), len(bss), time.Since(start))
	counts, _ = s.CountResults(ctx, big.ID)
	eq(t, "big total", counts[store.ResultPassed]+counts[store.ResultFailed], 500)
}

func testAggregates(t *testing.T, s store.Store) {
	ctx := context.Background()
	_, run, _, evA, _ := seedRun(t, s)
	aggs := []store.RunAggregate{
		{RunID: run.ID, EvaluatorID: evA, Metric: store.MetricScore, N: 10, Mean: ptr(0.5), PassRate: ptr(0.6), Histogram: json.RawMessage(`{"edges":[0,1],"counts":[10]}`)},
		{RunID: run.ID, EvaluatorID: evA, Metric: store.MetricScore, SliceKey: "tag:odd", N: 5, Mean: ptr(0.4)},
		{RunID: run.ID, EvaluatorID: store.NilID, Metric: store.MetricLatencyMs, N: 10, P95: ptr(120.0)},
	}
	must(t, s.UpsertRunAggregates(ctx, aggs))
	aggs[0].Mean = ptr(0.7)
	must(t, s.UpsertRunAggregates(ctx, aggs[:1]))
	got, err := s.ListRunAggregates(ctx, run.ID)
	must(t, err)
	eq(t, "aggs", len(got), 3)
	for _, a := range got {
		if a.EvaluatorID == evA && a.SliceKey == "" {
			eq(t, "updated mean", *a.Mean, 0.7)
		}
	}
	lat, err := s.ListRunAggregatesForRuns(ctx, []uuid.UUID{run.ID}, store.MetricLatencyMs, "")
	must(t, err)
	eq(t, "latency", len(lat), 1)
	eq(t, "p95", *lat[0].P95, 120.0)
}

func testReview(t *testing.T, s store.Store) {
	ctx := context.Background()
	f, run, exs, evA, evB := seedRun(t, s)
	rs, ss := makeResults(run, exs, evA, evB, 0)
	must(t, s.InsertResults(ctx, rs, ss))
	q := store.ReviewQueue{OrgID: f.org.ID, ProjectID: f.proj.ID, Name: "Failures", Config: json.RawMessage(`{"mode":"failures"}`)}
	must(t, s.CreateReviewQueue(ctx, &q))
	var items []store.ReviewItem
	for _, r := range rs[:4] {
		items = append(items, store.ReviewItem{OrgID: f.org.ID, ProjectID: f.proj.ID, QueueID: q.ID, RunID: run.ID, ResultID: r.ID})
	}
	n, err := s.AddReviewItems(ctx, items)
	must(t, err)
	eq(t, "added", n, 4)
	for i := range items {
		items[i].ID = uuid.Nil
	}
	n, _ = s.AddReviewItems(ctx, items)
	eq(t, "idempotent", n, 0)
	gq, _ := s.GetReviewQueue(ctx, q.ID)
	eq(t, "pending", gq.PendingCount, 4)
	pg, err := s.ListReviewItems(ctx, store.ReviewItemFilter{QueueID: q.ID, Statuses: []string{store.ReviewPending}}, store.ListParams{})
	must(t, err)
	eq(t, "items", len(pg.Items), 4)
	it := pg.Items[0]
	it.Status = store.ReviewDone
	it.CompletedBy = &f.user.ID
	it.CompletedAt = ptr(time.Now())
	must(t, s.UpdateReviewItem(ctx, &it))
	gq, _ = s.GetReviewQueue(ctx, q.ID)
	eq(t, "done", gq.DoneCount, 1)
	ql, _ := s.ListReviewQueues(ctx, f.proj.ID, store.ListParams{})
	eq(t, "queues", len(ql.Items), 1)
	q.Name = "F2"
	must(t, s.UpdateReviewQueue(ctx, &q))

	a := store.Annotation{OrgID: f.org.ID, ProjectID: f.proj.ID, ResultID: it.ResultID, ReviewItemID: &it.ID, UserID: &f.user.ID,
		EvaluatorID: &evA, Value: ptr(0.8), Passed: ptr(true), Comment: "good"}
	must(t, s.CreateAnnotation(ctx, &a))
	al, err := s.ListAnnotations(ctx, store.AnnotationFilter{ResultID: &it.ResultID}, store.ListParams{})
	must(t, err)
	eq(t, "annotations", len(al.Items), 1)
	a.Comment = "great"
	must(t, s.UpdateAnnotation(ctx, &a))
	ga, _ := s.GetAnnotation(ctx, a.ID)
	eq(t, "comment", ga.Comment, "great")
	al, _ = s.ListAnnotations(ctx, store.AnnotationFilter{ProjectID: &f.proj.ID, EvaluatorID: &evA}, store.ListParams{})
	eq(t, "by evaluator", len(al.Items), 1)
	must(t, s.DeleteAnnotation(ctx, a.ID))
	must(t, s.DeleteReviewQueue(ctx, q.ID))
}

func testMisc(t *testing.T, s store.Store) {
	ctx := context.Background()
	f := setup(t, s)
	r1, r2 := newRun(t, s, f, nil), newRun(t, s, f, nil)
	c := store.Comparison{OrgID: f.org.ID, ProjectID: f.proj.ID, Name: "A vs B", RunIDs: []uuid.UUID{r1.ID, r2.ID}}
	must(t, s.CreateComparison(ctx, &c))
	gc, err := s.GetComparison(ctx, c.ID)
	must(t, err)
	eq(t, "run ids", len(gc.RunIDs), 2)
	eq(t, "run id", gc.RunIDs[1], r2.ID)
	cl, _ := s.ListComparisons(ctx, f.proj.ID, store.ListParams{})
	eq(t, "comparisons", len(cl.Items), 1)
	must(t, s.DeleteComparison(ctx, c.ID))

	ch := store.NotificationChannel{OrgID: f.org.ID, Name: "slack", Kind: "slack", Config: json.RawMessage(`{"url":"x"}`),
		Events: []string{"run.failed"}, Enabled: true}
	must(t, s.CreateNotificationChannel(ctx, &ch))
	ch.Events = append(ch.Events, "gate.failed")
	must(t, s.UpdateNotificationChannel(ctx, &ch))
	gch, _ := s.GetNotificationChannel(ctx, ch.ID)
	eq(t, "events", len(gch.Events), 2)
	chl, _ := s.ListNotificationChannels(ctx, f.org.ID, store.ListParams{})
	eq(t, "channels", len(chl.Items), 1)
	must(t, s.DeleteNotificationChannel(ctx, ch.ID))

	must(t, s.AppendAudit(ctx, &store.AuditEntry{OrgID: f.org.ID, ActorID: &f.user.ID, Actor: f.user.Email, Action: "secret.create", Target: "secret:1"}))
	au, err := s.ListAudit(ctx, f.org.ID, store.ListParams{})
	must(t, err)
	eq(t, "audit", len(au.Items), 1)

	must(t, s.CachePut(ctx, "k1", []byte("v1"), 0))
	must(t, s.CachePut(ctx, "k1", []byte("v2"), time.Hour))
	v, ok, err := s.CacheGet(ctx, "k1")
	must(t, err)
	eq(t, "cache hit", ok, true)
	eq(t, "cache value", string(v), "v2")
	must(t, s.CachePut(ctx, "k2", []byte("x"), time.Millisecond))
	time.Sleep(5 * time.Millisecond)
	_, ok, _ = s.CacheGet(ctx, "k2")
	eq(t, "expired miss", ok, false)
	_, ok, _ = s.CacheGet(ctx, "nope")
	eq(t, "miss", ok, false)
	n, err := s.CachePurgeExpired(ctx, time.Now())
	must(t, err)
	eq(t, "purged", n, 1)
}

func testTx(t *testing.T, s store.Store) {
	ctx := context.Background()
	f := setup(t, s)
	boom := errors.New("boom")
	err := s.WithTx(ctx, func(tx store.Store) error {
		must(t, tx.CreateProject(ctx, &store.Project{OrgID: f.org.ID, Name: "tx", Slug: "tx"}))
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("want boom, got %v", err)
	}
	pg, _ := s.ListProjects(ctx, store.ProjectFilter{OrgIDs: []uuid.UUID{f.org.ID}, Slug: "tx"}, store.ListParams{})
	eq(t, "rolled back", len(pg.Items), 0)
	must(t, s.WithTx(ctx, func(tx store.Store) error {
		return tx.CreateProject(ctx, &store.Project{OrgID: f.org.ID, Name: "tx", Slug: "tx"})
	}))
	pg, _ = s.ListProjects(ctx, store.ProjectFilter{OrgIDs: []uuid.UUID{f.org.ID}, Slug: "tx"}, store.ListParams{})
	eq(t, "committed", len(pg.Items), 1)
	must(t, s.Ping(ctx))
}
