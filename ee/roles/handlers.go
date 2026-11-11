//go:build ee

package roles

import (
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/prat31/airval/ee/internal/httpx"
	"github.com/prat31/airval/internal/auth"
	"github.com/prat31/airval/internal/store"
)

// Routes registers the role and assignment endpoints.
func Routes(r chi.Router, h httpx.H) {
	r = r.With(h.Host.Authenticate)
	r.Get("/v1/orgs/{org}/roles", h.Wrap("listRoles", func(w http.ResponseWriter, r *http.Request) error { return listRoles(w, r, h) }))
	r.Post("/v1/orgs/{org}/roles", h.Wrap("createCustomRole", func(w http.ResponseWriter, r *http.Request) error { return writeRole(w, r, h, true) }))
	r.Patch("/v1/orgs/{org}/roles/{id}", h.Wrap("updateCustomRole", func(w http.ResponseWriter, r *http.Request) error { return writeRole(w, r, h, false) }))
	r.Delete("/v1/orgs/{org}/roles/{id}", h.Wrap("deleteCustomRole", func(w http.ResponseWriter, r *http.Request) error { return deleteRole(w, r, h) }))
	r.Get("/v1/orgs/{org}/role-assignments", h.Wrap("listRoleAssignments", func(w http.ResponseWriter, r *http.Request) error { return listAssignments(w, r, h) }))
	r.Put("/v1/orgs/{org}/role-assignments", h.Wrap("setRoleAssignment", func(w http.ResponseWriter, r *http.Request) error { return setAssignment(w, r, h) }))
	r.Delete("/v1/orgs/{org}/role-assignments", h.Wrap("deleteRoleAssignment", func(w http.ResponseWriter, r *http.Request) error { return deleteAssignment(w, r, h) }))
}

type permissionJSON struct {
	ID          string `json:"id"`
	Resource    string `json:"resource"`
	Action      string `json:"action"`
	Description string `json:"description"`
}

type customRoleJSON struct {
	ID          uuid.UUID `json:"id"`
	Key         string    `json:"key"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Permissions []string  `json:"permissions"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func toRoleJSON(c store.CustomRole) customRoleJSON {
	perms := c.Permissions
	if perms == nil {
		perms = []string{}
	}
	return customRoleJSON{ID: c.ID, Key: c.Key, Name: c.Name, Description: c.Description, Permissions: perms,
		CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt}
}

func listRoles(w http.ResponseWriter, r *http.Request, h httpx.H) error {
	orgID, _, err := h.RequireOrg(r, store.RoleViewer)
	if err != nil {
		return err
	}
	custom, err := h.Host.Store().ListCustomRoles(r.Context(), orgID)
	if err != nil {
		return err
	}
	type builtin struct {
		Role        store.Role `json:"role"`
		Permissions []string   `json:"permissions"`
	}
	out := struct {
		Permissions []permissionJSON `json:"permissions"`
		Builtin     []builtin        `json:"builtin"`
		Custom      []customRoleJSON `json:"custom"`
	}{Permissions: []permissionJSON{}, Builtin: []builtin{}, Custom: []customRoleJSON{}}
	for _, p := range Catalog {
		out.Permissions = append(out.Permissions, permissionJSON(p))
	}
	for _, role := range []store.Role{store.RoleOwner, store.RoleAdmin, store.RoleEditor, store.RoleViewer} {
		out.Builtin = append(out.Builtin, builtin{Role: role, Permissions: Builtin(role)})
	}
	for _, c := range custom {
		out.Custom = append(out.Custom, toRoleJSON(c))
	}
	return httpx.JSON(w, http.StatusOK, out)
}

var keyRe = regexp.MustCompile(`[^a-z0-9]+`)

func roleKey(name string) string {
	k := strings.Trim(keyRe.ReplaceAllString(strings.ToLower(name), "-"), "-")
	if len(k) > 48 {
		k = strings.Trim(k[:48], "-")
	}
	if k == "" {
		k = "role"
	}
	return k
}

func writeRole(w http.ResponseWriter, r *http.Request, h httpx.H, create bool) error {
	orgID, p, err := h.RequireOrg(r, store.RoleAdmin)
	if err != nil {
		return err
	}
	if err := httpx.RequireUser(p); err != nil {
		return err
	}
	var in struct {
		Name        string   `json:"name"`
		Description string   `json:"description"`
		Permissions []string `json:"permissions"`
	}
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || len(in.Name) > 64 {
		return httpx.Invalid("name must be 1–64 characters")
	}
	if len(in.Description) > 500 {
		return httpx.Invalid("description must be at most 500 characters")
	}
	if builtinName(in.Name) {
		return httpx.Invalid("%q is a built-in role name", in.Name)
	}
	perms, bad := Normalize(in.Permissions)
	if bad != "" {
		return httpx.Invalid("unknown permission %q", bad)
	}
	st := h.Host.Store()
	var cr store.CustomRole
	if create {
		cr = store.CustomRole{OrgID: orgID, Key: roleKey(in.Name), Name: in.Name, Description: in.Description, Permissions: perms}
		err = st.CreateCustomRole(r.Context(), &cr)
	} else {
		id, err := httpx.UUIDParam(r, "id")
		if err != nil {
			return err
		}
		if cr, err = st.GetCustomRole(r.Context(), id); err != nil || cr.OrgID != orgID {
			return store.ErrNotFound
		}
		cr.Name, cr.Key, cr.Description, cr.Permissions = in.Name, roleKey(in.Name), in.Description, perms
		err = st.UpdateCustomRole(r.Context(), &cr)
	}
	if errors.Is(err, store.ErrConflict) {
		return httpx.Errorf(http.StatusConflict, "conflict", "a role named %q already exists", in.Name)
	} else if err != nil {
		return err
	}
	action, status := "role.update", http.StatusOK
	if create {
		action, status = "role.create", http.StatusCreated
	}
	h.Audit(r, orgID, action, "role:"+cr.ID.String(), map[string]any{"name": cr.Name, "permissions": cr.Permissions})
	return httpx.JSON(w, status, toRoleJSON(cr))
}

func builtinName(n string) bool {
	switch store.Role(strings.ToLower(n)) {
	case store.RoleOwner, store.RoleAdmin, store.RoleEditor, store.RoleViewer:
		return true
	}
	return false
}

func deleteRole(w http.ResponseWriter, r *http.Request, h httpx.H) error {
	orgID, p, err := h.RequireOrg(r, store.RoleAdmin)
	if err != nil {
		return err
	}
	if err := httpx.RequireUser(p); err != nil {
		return err
	}
	id, err := httpx.UUIDParam(r, "id")
	if err != nil {
		return err
	}
	cr, err := h.Host.Store().GetCustomRole(r.Context(), id)
	if err != nil || cr.OrgID != orgID {
		return store.ErrNotFound
	}
	if err := h.Host.Store().DeleteCustomRole(r.Context(), id); err != nil {
		return err
	}
	h.Audit(r, orgID, "role.delete", "role:"+id.String(), map[string]string{"name": cr.Name})
	w.WriteHeader(http.StatusNoContent)
	return nil
}

type assignmentJSON struct {
	UserID    uuid.UUID  `json:"user_id"`
	ProjectID *uuid.UUID `json:"project_id"`
	Role      string     `json:"role"`
	Source    string     `json:"source"`
	CreatedAt time.Time  `json:"created_at"`
}

func toAssignmentJSON(a store.RoleAssignment) assignmentJSON {
	out := assignmentJSON{UserID: a.UserID, Role: Ref(a), Source: a.Source, CreatedAt: a.CreatedAt}
	if a.ProjectID != store.NilID {
		id := a.ProjectID
		out.ProjectID = &id
	}
	return out
}

func optUUID(r *http.Request, name string) (*uuid.UUID, error) {
	v := r.URL.Query().Get(name)
	if v == "" {
		return nil, nil
	}
	id, err := uuid.Parse(v)
	if err != nil {
		return nil, httpx.Errorf(http.StatusBadRequest, "bad_request", "invalid %s", name)
	}
	return &id, nil
}

func listAssignments(w http.ResponseWriter, r *http.Request, h httpx.H) error {
	orgID, _, err := h.RequireOrg(r, store.RoleAdmin)
	if err != nil {
		return err
	}
	f := store.RoleAssignmentFilter{OrgID: orgID}
	if f.UserID, err = optUUID(r, "user_id"); err != nil {
		return err
	}
	if f.ProjectID, err = optUUID(r, "project_id"); err != nil {
		return err
	}
	as, err := h.Host.Store().ListRoleAssignments(r.Context(), f)
	if err != nil {
		return err
	}
	items := make([]assignmentJSON, len(as))
	for i, a := range as {
		items[i] = toAssignmentJSON(a)
	}
	return httpx.JSON(w, http.StatusOK, map[string]any{"items": items})
}

// Validate checks an assignment against the org: the member exists and is not an owner, the project and
// custom role belong to the org, built-in roles are per-project overrides only and never owner.
func Validate(r *http.Request, st store.Store, a *store.RoleAssignment) error {
	ctx := r.Context()
	m, err := st.GetMembership(ctx, a.OrgID, a.UserID)
	if errors.Is(err, store.ErrNotFound) {
		return httpx.Invalid("user is not a member of this organization")
	} else if err != nil {
		return err
	}
	if m.Role == store.RoleOwner {
		return httpx.Invalid("owners always have full access; change their membership role instead")
	}
	if a.ProjectID != store.NilID {
		pr, err := st.GetProject(ctx, a.ProjectID)
		if err != nil || pr.OrgID != a.OrgID {
			return httpx.Invalid("project not found in this organization")
		}
	}
	if a.CustomRoleID != nil {
		cr, err := st.GetCustomRole(ctx, *a.CustomRoleID)
		if err != nil || cr.OrgID != a.OrgID {
			return httpx.Invalid("custom role not found in this organization")
		}
		return nil
	}
	if a.ProjectID == store.NilID {
		return httpx.Invalid("org-wide built-in roles are set on the membership; assign a custom role or pick a project")
	}
	if a.Role == store.RoleOwner {
		return httpx.Invalid("the owner role cannot be assigned per project")
	}
	return nil
}

func setAssignment(w http.ResponseWriter, r *http.Request, h httpx.H) error {
	orgID, p, err := h.RequireOrg(r, store.RoleAdmin)
	if err != nil {
		return err
	}
	if err := httpx.RequireUser(p); err != nil {
		return err
	}
	var in struct {
		UserID    uuid.UUID  `json:"user_id"`
		ProjectID *uuid.UUID `json:"project_id"`
		Role      string     `json:"role"`
	}
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	role, custom, err := ParseRef(in.Role)
	if err != nil {
		return httpx.Invalid("%v", err)
	}
	a := store.RoleAssignment{OrgID: orgID, UserID: in.UserID, ProjectID: store.NilID, Role: role, CustomRoleID: custom, Source: "manual"}
	if in.ProjectID != nil {
		a.ProjectID = *in.ProjectID
	}
	if err := Validate(r, h.Host.Store(), &a); err != nil {
		return err
	}
	if !callerMayGrant(r, h, orgID, p, role) {
		return httpx.Errorf(http.StatusForbidden, "forbidden", "you cannot grant a role above your own")
	}
	if err := h.Host.Store().SetRoleAssignment(r.Context(), &a); err != nil {
		return err
	}
	h.Audit(r, orgID, "role_assignment.set", "user:"+a.UserID.String(), map[string]any{"role": Ref(a), "project_id": in.ProjectID})
	return httpx.JSON(w, http.StatusOK, toAssignmentJSON(a))
}

func callerMayGrant(r *http.Request, h httpx.H, orgID uuid.UUID, p *auth.Principal, role store.Role) bool {
	if role == "" {
		return true
	}
	m, err := h.Host.Store().GetMembership(r.Context(), orgID, p.User.ID)
	return err == nil && m.Role.AtLeast(role)
}

func deleteAssignment(w http.ResponseWriter, r *http.Request, h httpx.H) error {
	orgID, p, err := h.RequireOrg(r, store.RoleAdmin)
	if err != nil {
		return err
	}
	if err := httpx.RequireUser(p); err != nil {
		return err
	}
	user, err := optUUID(r, "user_id")
	if err != nil {
		return err
	}
	if user == nil {
		return httpx.Invalid("user_id is required")
	}
	project, err := optUUID(r, "project_id")
	if err != nil {
		return err
	}
	pid := store.NilID
	if project != nil {
		pid = *project
	}
	if err := h.Host.Store().DeleteRoleAssignment(r.Context(), orgID, *user, pid); err != nil {
		return err
	}
	h.Audit(r, orgID, "role_assignment.delete", "user:"+user.String(), map[string]any{"project_id": project})
	w.WriteHeader(http.StatusNoContent)
	return nil
}
