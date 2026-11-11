package storetest

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/prat31/airval/internal/store"
)

func testEnterprise(t *testing.T, s store.Store) {
	ctx := context.Background()
	f := setup(t, s)

	// SSO providers + identities.
	p := store.SSOProvider{OrgID: f.org.ID, Slug: "okta-" + uuid.NewString()[:6], Kind: "oidc", Name: "Okta", Enabled: true,
		Config: json.RawMessage(`{"issuer":"https://idp"}`), Secret: []byte{1, 2, 3}, KeyVersion: 1}
	must(t, s.CreateSSOProvider(ctx, &p))
	got, err := s.GetSSOProviderBySlug(ctx, p.Slug)
	must(t, err)
	eq(t, "secret", string(got.Secret), string([]byte{1, 2, 3}))
	eq(t, "kind", got.Kind, "oidc")
	dup := p
	dup.ID = uuid.Nil
	if err := s.CreateSSOProvider(ctx, &dup); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("dup slug: %v", err)
	}
	got.Enabled, got.Secret = false, nil
	must(t, s.UpdateSSOProvider(ctx, &got))
	got, _ = s.GetSSOProvider(ctx, p.ID)
	eq(t, "disabled", got.Enabled, false)
	eq(t, "secret cleared", len(got.Secret), 0)
	all, err := s.ListSSOProviders(ctx, uuid.Nil)
	must(t, err)
	eq(t, "all providers", len(all), 1)
	must(t, s.UpsertSSOIdentity(ctx, &store.SSOIdentity{ProviderID: p.ID, Subject: "sub-1", UserID: f.user.ID, Email: "a@b"}))
	must(t, s.UpsertSSOIdentity(ctx, &store.SSOIdentity{ProviderID: p.ID, Subject: "sub-1", UserID: f.user.ID, Email: "c@d"}))
	id, err := s.GetSSOIdentity(ctx, p.ID, "sub-1")
	must(t, err)
	eq(t, "identity email", id.Email, "c@d")
	if _, err := s.GetSSOIdentity(ctx, p.ID, "nope"); !errors.Is(err, store.ErrNotFound) {
		t.Fatal(err)
	}

	// Custom roles + assignments.
	cr := store.CustomRole{OrgID: f.org.ID, Key: "runner", Name: "Runner", Permissions: []string{"runs:read", "runs:execute"}}
	must(t, s.CreateCustomRole(ctx, &cr))
	crs, _ := s.ListCustomRoles(ctx, f.org.ID)
	eq(t, "roles", len(crs), 1)
	eq(t, "perms", len(crs[0].Permissions), 2)
	cr.Permissions = append(cr.Permissions, "datasets:read")
	must(t, s.UpdateCustomRole(ctx, &cr))
	cr2, _ := s.GetCustomRole(ctx, cr.ID)
	eq(t, "perms updated", len(cr2.Permissions), 3)
	must(t, s.SetRoleAssignment(ctx, &store.RoleAssignment{OrgID: f.org.ID, UserID: f.user.ID, ProjectID: store.NilID, Role: store.RoleViewer}))
	must(t, s.SetRoleAssignment(ctx, &store.RoleAssignment{OrgID: f.org.ID, UserID: f.user.ID, ProjectID: f.proj.ID, CustomRoleID: &cr.ID}))
	must(t, s.SetRoleAssignment(ctx, &store.RoleAssignment{OrgID: f.org.ID, UserID: f.user.ID, ProjectID: store.NilID, Role: store.RoleEditor, Source: "scim"}))
	as, err := s.ListRoleAssignments(ctx, store.RoleAssignmentFilter{OrgID: f.org.ID, UserID: &f.user.ID})
	must(t, err)
	eq(t, "assignments", len(as), 2)
	eq(t, "org-wide first", as[0].ProjectID, store.NilID)
	eq(t, "upserted", as[0].Role, store.RoleEditor)
	eq(t, "source", as[0].Source, "scim")
	if as[1].CustomRoleID == nil || *as[1].CustomRoleID != cr.ID {
		t.Fatal("custom role assignment")
	}
	pa, _ := s.ListRoleAssignments(ctx, store.RoleAssignmentFilter{OrgID: f.org.ID, ProjectID: &f.proj.ID})
	eq(t, "project assignments", len(pa), 1)
	// Deleting the custom role drops its assignments.
	must(t, s.DeleteCustomRole(ctx, cr.ID))
	as, _ = s.ListRoleAssignments(ctx, store.RoleAssignmentFilter{OrgID: f.org.ID})
	eq(t, "cascade", len(as), 1)
	must(t, s.DeleteRoleAssignment(ctx, f.org.ID, f.user.ID, store.NilID))
	if err := s.DeleteRoleAssignment(ctx, f.org.ID, f.user.ID, store.NilID); !errors.Is(err, store.ErrNotFound) {
		t.Fatal(err)
	}
	// Removing a membership removes its assignments.
	must(t, s.SetRoleAssignment(ctx, &store.RoleAssignment{OrgID: f.org.ID, UserID: f.user.ID, ProjectID: f.proj.ID, Role: store.RoleAdmin}))
	u2 := store.User{Email: "second-" + uuid.NewString()[:6] + "@x.io"}
	must(t, s.CreateUser(ctx, &u2))
	must(t, s.UpsertMembership(ctx, &store.Membership{OrgID: f.org.ID, UserID: u2.ID, Role: store.RoleViewer}))
	must(t, s.SetRoleAssignment(ctx, &store.RoleAssignment{OrgID: f.org.ID, UserID: u2.ID, ProjectID: f.proj.ID, Role: store.RoleEditor}))
	must(t, s.DeleteMembership(ctx, f.org.ID, u2.ID))
	as, _ = s.ListRoleAssignments(ctx, store.RoleAssignmentFilter{OrgID: f.org.ID})
	eq(t, "membership cleanup", len(as), 1)

	// SCIM tokens, users, groups.
	tok := store.SCIMToken{OrgID: f.org.ID, Name: "Okta", Prefix: "airvscim_ab", Hash: "h-" + uuid.NewString()}
	must(t, s.CreateSCIMToken(ctx, &tok))
	gt, err := s.GetSCIMTokenByHash(ctx, tok.Hash)
	must(t, err)
	eq(t, "token org", gt.OrgID, f.org.ID)
	must(t, s.TouchSCIMToken(ctx, tok.ID, time.Now()))
	ts, _ := s.ListSCIMTokens(ctx, f.org.ID)
	if len(ts) != 1 || ts[0].LastUsedAt == nil {
		t.Fatal("touch token")
	}
	must(t, s.DeleteSCIMToken(ctx, f.org.ID, tok.ID))

	su := store.SCIMUser{OrgID: f.org.ID, UserID: f.user.ID, ExternalID: "ext-1", UserName: "Alice@Example.com", Active: true,
		Data: json.RawMessage(`{"name":{"givenName":"Alice"}}`)}
	must(t, s.UpsertSCIMUser(ctx, &su))
	must(t, s.UpsertSCIMUser(ctx, &store.SCIMUser{OrgID: f.org.ID, UserID: u2.ID, UserName: "bob@example.com", Active: true}))
	us, total, err := s.ListSCIMUsers(ctx, store.SCIMUserFilter{OrgID: f.org.ID, UserName: "ALICE@example.com"})
	must(t, err)
	eq(t, "filter total", total, 1)
	eq(t, "filter user", us[0].UserID, f.user.ID)
	us, total, _ = s.ListSCIMUsers(ctx, store.SCIMUserFilter{OrgID: f.org.ID, Offset: 1, Limit: 1})
	eq(t, "total", total, 2)
	eq(t, "page", len(us), 1)
	us, _, _ = s.ListSCIMUsers(ctx, store.SCIMUserFilter{OrgID: f.org.ID, ExternalID: "ext-1"})
	eq(t, "by external id", len(us), 1)
	su.Active = false
	must(t, s.UpsertSCIMUser(ctx, &su))
	gsu, _ := s.GetSCIMUser(ctx, f.org.ID, f.user.ID)
	eq(t, "deactivated", gsu.Active, false)

	g := store.SCIMGroup{OrgID: f.org.ID, DisplayName: "Engineering", Members: []uuid.UUID{f.user.ID, u2.ID, f.user.ID}}
	must(t, s.CreateSCIMGroup(ctx, &g))
	gg, err := s.GetSCIMGroup(ctx, f.org.ID, g.ID)
	must(t, err)
	eq(t, "members", len(gg.Members), 2)
	gg.Members = []uuid.UUID{u2.ID}
	gg.DisplayName = "Eng"
	must(t, s.UpdateSCIMGroup(ctx, &gg))
	gs, total, _ := s.ListSCIMGroups(ctx, store.SCIMGroupFilter{OrgID: f.org.ID, DisplayName: "eng"})
	eq(t, "group filter", total, 1)
	eq(t, "group members", len(gs[0].Members), 1)
	ug, _ := s.ListUserSCIMGroups(ctx, f.org.ID, u2.ID)
	eq(t, "user groups", len(ug), 1)
	ug, _ = s.ListUserSCIMGroups(ctx, f.org.ID, f.user.ID)
	eq(t, "user groups none", len(ug), 0)
	if _, err := s.GetSCIMGroup(ctx, uuid.New(), g.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("group must be org-scoped")
	}
	must(t, s.DeleteSCIMGroup(ctx, f.org.ID, g.ID))
	must(t, s.DeleteSCIMUser(ctx, f.org.ID, u2.ID))

	// Org settings.
	if _, err := s.GetOrgSetting(ctx, f.org.ID, "branding"); !errors.Is(err, store.ErrNotFound) {
		t.Fatal(err)
	}
	must(t, s.PutOrgSetting(ctx, f.org.ID, "branding", json.RawMessage(`{"product_name":"Acme Evals"}`)))
	must(t, s.PutOrgSetting(ctx, f.org.ID, "branding", json.RawMessage(`{"product_name":"Acme"}`)))
	v, err := s.GetOrgSetting(ctx, f.org.ID, "branding")
	must(t, err)
	var br map[string]string
	must(t, json.Unmarshal(v, &br))
	eq(t, "setting", br["product_name"], "Acme")
	m, _ := s.ListOrgSettings(ctx, "branding")
	eq(t, "settings by key", len(m), 1)

	// Retention: old finished runs go, running runs and pinned baselines stay.
	old := time.Now().Add(-40 * 24 * time.Hour)
	suite := store.Suite{OrgID: f.org.ID, ProjectID: f.proj.ID, Name: "R", Slug: "r"}
	must(t, s.CreateSuite(ctx, &suite))
	mk := func(status string, at time.Time) store.Run {
		return newRun(t, s, f, func(r *store.Run) { r.Status, r.CreatedAt, r.SuiteID = status, at, &suite.ID })
	}
	oldDone := mk(store.RunSucceeded, old)
	oldBaseline := mk(store.RunFailed, old)
	_ = mk(store.RunRunning, old)
	_ = mk(store.RunSucceeded, time.Now())
	suite.BaselineRunID = &oldBaseline.ID
	must(t, s.UpdateSuite(ctx, &suite))
	cutoff := time.Now().Add(-30 * 24 * time.Hour)
	ids, err := s.ListExpiredRuns(ctx, f.proj.ID, cutoff, true, 100)
	must(t, err)
	eq(t, "expired (keep baselines)", len(ids), 1)
	eq(t, "expired id", ids[0], oldDone.ID)
	ids, _ = s.ListExpiredRuns(ctx, f.proj.ID, cutoff, false, 100)
	eq(t, "expired (all)", len(ids), 2)
	n, err := s.DeleteRuns(ctx, ids)
	must(t, err)
	eq(t, "deleted", n, 2)
	rp := store.RetentionPolicy{ProjectID: f.proj.ID, OrgID: f.org.ID, Enabled: true, RunDays: 30, KeepBaselines: true}
	must(t, s.UpsertRetentionPolicy(ctx, &rp))
	must(t, s.RecordRetentionApplied(ctx, f.proj.ID, time.Now(), 2))
	must(t, s.RecordRetentionApplied(ctx, f.proj.ID, time.Now(), 3))
	grp, err := s.GetRetentionPolicy(ctx, f.proj.ID)
	must(t, err)
	eq(t, "total deleted", grp.TotalDeleted, int64(5))
	eq(t, "last deleted", grp.LastDeleted, 3)
	if grp.LastAppliedAt == nil {
		t.Fatal("last applied")
	}
	rp.RunDays = 7
	must(t, s.UpsertRetentionPolicy(ctx, &rp))
	rps, _ := s.ListRetentionPolicies(ctx, uuid.Nil)
	eq(t, "policies", len(rps), 1)
	eq(t, "run days", rps[0].RunDays, 7)
	eq(t, "upsert keeps counters", rps[0].TotalDeleted, int64(5))
	must(t, s.DeleteRetentionPolicy(ctx, f.proj.ID))

	// Audit: filters + retention.
	for i, a := range []string{"secret.create", "secret.delete", "dataset.create"} {
		must(t, s.AppendAudit(ctx, &store.AuditEntry{OrgID: f.org.ID, Actor: []string{"alice@x.io", "bob@x.io", "apikey:airv_1"}[i],
			Action: a, Target: "x:" + a, IP: "10.0.0.1", UserAgent: "test", Status: 200, At: time.Now().Add(-time.Duration(i) * 48 * time.Hour)}))
	}
	pg, err := s.ListAudit(ctx, store.AuditFilter{OrgID: f.org.ID, Action: "secret."}, store.ListParams{})
	must(t, err)
	eq(t, "action prefix", len(pg.Items), 2)
	eq(t, "ip", pg.Items[0].IP, "10.0.0.1")
	eq(t, "status", pg.Items[0].Status, 200)
	pg, _ = s.ListAudit(ctx, store.AuditFilter{OrgID: f.org.ID, Actor: "BOB"}, store.ListParams{})
	eq(t, "actor", len(pg.Items), 1)
	pg, _ = s.ListAudit(ctx, store.AuditFilter{OrgID: f.org.ID, Action: "secret_"}, store.ListParams{})
	eq(t, "like escaped", len(pg.Items), 0)
	from := time.Now().Add(-24 * time.Hour)
	pg, _ = s.ListAudit(ctx, store.AuditFilter{OrgID: f.org.ID, From: &from}, store.ListParams{})
	eq(t, "from", len(pg.Items), 1)
	n, err = s.DeleteAuditBefore(ctx, f.org.ID, time.Now().Add(-72*time.Hour))
	must(t, err)
	eq(t, "audit purged", n, 1)
}
