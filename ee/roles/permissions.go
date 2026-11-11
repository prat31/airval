//go:build ee

// Package roles implements custom roles (named permission sets) and per-project role assignments.
//
// Model:
//   - Every member keeps a built-in membership role (owner > admin > editor > viewer); core enforces it.
//   - An assignment gives a member a built-in role for one project (override, owner excluded), or a
//     custom role org-wide or for one project. A project assignment wins over the org-wide one.
//   - Custom roles govern project resources and read access (see Catalog). Organization administration
//     (members, API keys, secrets, notification channels, org settings, ee settings) always follows the
//     built-in membership role, so a custom role can never be used to escalate to admin.
//   - Owners are never restricted by assignments.
package roles

import (
	"slices"
	"strings"

	"github.com/prat31/airval/internal/store"
)

// Permission is one fine-grained capability ("resource:action").
type Permission struct {
	ID          string
	Resource    string
	Action      string
	Description string
}

// Catalog lists every permission a custom role can hold, in display order.
var Catalog = []Permission{
	p("projects", "read", "View projects, overview and trends"),
	p("projects", "write", "Create, rename and delete projects"),
	p("targets", "read", "View targets and their versions"),
	p("targets", "write", "Create, edit, test and delete targets"),
	p("datasets", "read", "View and export datasets and versions"),
	p("datasets", "write", "Edit examples, import and publish dataset versions"),
	p("evaluators", "read", "View evaluators and agreement"),
	p("evaluators", "write", "Create, edit, preview and delete evaluators"),
	p("suites", "read", "View suites, gates and triggers"),
	p("suites", "write", "Edit suites, gates and triggers"),
	p("runs", "read", "View runs, results and live progress"),
	p("runs", "execute", "Start, cancel, rerun and rescore runs"),
	p("runs", "write", "Edit run labels and delete runs"),
	p("comparisons", "read", "Compare runs and view saved comparisons"),
	p("comparisons", "write", "Save and delete comparisons"),
	p("review", "read", "View review queues and annotations"),
	p("review", "annotate", "Grade review items and write annotations"),
	p("review", "manage", "Create and manage review queues"),
	p("members", "read", "View organization members"),
	p("secrets", "read", "View secret names (never values)"),
	p("notifications", "read", "View notification channels"),
	p("api_keys", "read", "View API keys (never values)"),
	p("audit", "read", "View the audit log"),
}

func p(resource, action, desc string) Permission {
	return Permission{ID: resource + ":" + action, Resource: resource, Action: action, Description: desc}
}

// Known reports whether id is in the catalog.
func Known(id string) bool {
	return slices.ContainsFunc(Catalog, func(x Permission) bool { return x.ID == id })
}

func readsOnly() []string {
	var out []string
	for _, x := range Catalog {
		if x.Action == "read" {
			out = append(out, x.ID)
		}
	}
	return out
}

// Builtin returns the permission set equivalent to a built-in role (for display and for comparing a
// custom role against the built-in tiers).
func Builtin(r store.Role) []string {
	switch r {
	case store.RoleViewer:
		return without(readsOnly(), "secrets:read", "api_keys:read", "audit:read")
	case store.RoleEditor:
		return append(without(readsOnly(), "api_keys:read", "audit:read"),
			"projects:write", "targets:write", "datasets:write", "evaluators:write", "suites:write", "runs:execute",
			"runs:write", "comparisons:write", "review:annotate", "review:manage")
	case store.RoleAdmin, store.RoleOwner:
		all := make([]string, len(Catalog))
		for i, x := range Catalog {
			all[i] = x.ID
		}
		return all
	}
	return nil
}

func without(in []string, drop ...string) []string {
	return slices.DeleteFunc(in, func(s string) bool { return slices.Contains(drop, s) })
}

// Tier is the highest built-in role whose permission set is contained in perms ("" if none). It decides
// operations that have no explicit permission mapping.
func Tier(perms []string) store.Role {
	for _, r := range []store.Role{store.RoleAdmin, store.RoleEditor, store.RoleViewer} {
		ok := true
		for _, x := range Builtin(r) {
			if !slices.Contains(perms, x) {
				ok = false
				break
			}
		}
		if ok {
			return r
		}
	}
	return ""
}

// adminOps always follow the built-in membership role (custom roles neither grant nor restrict them).
var adminOps = map[string]bool{
	"updateOrg": true, "deleteOrg": true, "addMember": true, "updateMember": true, "removeMember": true,
	"createApiKey": true, "deleteApiKey": true, "createSecret": true, "updateSecret": true, "deleteSecret": true,
	"createNotificationChannel": true, "updateNotificationChannel": true, "deleteNotificationChannel": true,
	"testNotificationChannel": true, "createOrg": true, "updateMe": true, "logout": true, "getMe": true,
	"listOrgs": true, "getOrg": true,
}

// opPermissions maps API operation ids to the permission they need.
var opPermissions = func() map[string]string {
	m := map[string]string{}
	add := func(perm string, ops ...string) {
		for _, op := range ops {
			m[op] = perm
		}
	}
	add("projects:read", "listProjects", "getProject", "getProjectOverview", "getTrends")
	add("projects:write", "createProject", "createSampleProject", "updateProject", "deleteProject")
	add("targets:read", "listTargets", "getTarget", "listTargetVersions", "getTargetVersion")
	add("targets:write", "createTarget", "updateTarget", "deleteTarget", "testTarget", "testTargetDraft")
	add("datasets:read", "listDatasets", "getDataset", "listExamples", "getExample", "listDatasetVersions",
		"getDatasetVersion", "exportDatasetVersion")
	add("datasets:write", "createDataset", "updateDataset", "deleteDataset", "upsertExamples", "deleteExamples",
		"importDataset", "addExamplesFromResults", "publishDatasetVersion")
	add("evaluators:read", "listEvaluators", "getEvaluator", "getEvaluatorCatalog", "getEvaluatorAgreement")
	add("evaluators:write", "createEvaluator", "updateEvaluator", "deleteEvaluator", "previewEvaluator")
	add("suites:read", "listSuites", "getSuite", "getSuiteBySlug", "listGates", "listTriggers", "validateGate")
	add("suites:write", "createSuite", "updateSuite", "deleteSuite", "createGate", "updateGate", "deleteGate",
		"createTrigger", "updateTrigger", "deleteTrigger", "rotateTriggerSecret")
	add("runs:read", "listRuns", "getRun", "listResults", "getResult", "listRunAggregates", "streamRunEvents")
	add("runs:execute", "createRun", "cancelRun", "rerunFailed", "rescoreRun")
	add("runs:write", "updateRun", "deleteRun")
	add("comparisons:read", "compareRuns", "listComparisons", "getComparison")
	add("comparisons:write", "createComparison", "deleteComparison")
	add("review:read", "listReviewQueues", "getReviewQueue", "listReviewItems", "getReviewItem", "listAnnotations",
		"getAnnotation")
	add("review:manage", "createReviewQueue", "updateReviewQueue", "deleteReviewQueue", "addReviewItems")
	add("review:annotate", "updateReviewItem", "createAnnotation", "updateAnnotation", "deleteAnnotation")
	add("members:read", "listMembers")
	add("secrets:read", "listSecrets", "getSecret")
	add("notifications:read", "listNotificationChannels", "getNotificationChannel")
	add("api_keys:read", "listApiKeys")
	add("audit:read", "listAuditLog")
	return m
}()

// PermissionFor returns the permission an operation needs, or ok=false when unmapped.
func PermissionFor(op string) (string, bool) {
	perm, ok := opPermissions[op]
	return perm, ok
}

// IsAdminOp reports whether op always follows the built-in role (including all ee administration).
func IsAdminOp(op string) bool { return adminOps[op] || strings.HasPrefix(op, "ee.") }

// Normalize validates, de-duplicates and sorts permissions in catalog order.
func Normalize(perms []string) ([]string, string) {
	out := make([]string, 0, len(perms))
	for _, x := range Catalog {
		if slices.Contains(perms, x.ID) {
			out = append(out, x.ID)
		}
	}
	for _, x := range perms {
		if !Known(x) {
			return nil, x
		}
	}
	return out, ""
}
