//go:build ee

package roles

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/prat31/airval/internal/auth"
	"github.com/prat31/airval/internal/rbac"
	"github.com/prat31/airval/internal/store"
)

// CustomPrefix marks a custom role in a RoleRef ("custom:<id>").
const CustomPrefix = "custom:"

// Ref formats an assignment's role as a RoleRef.
func Ref(a store.RoleAssignment) string {
	if a.CustomRoleID != nil {
		return CustomPrefix + a.CustomRoleID.String()
	}
	return string(a.Role)
}

// ParseRef parses a RoleRef into a built-in role or a custom role id.
func ParseRef(ref string) (store.Role, *uuid.UUID, error) {
	if id, ok := strings.CutPrefix(ref, CustomPrefix); ok {
		u, err := uuid.Parse(id)
		if err != nil {
			return "", nil, fmt.Errorf("invalid custom role id %q", id)
		}
		return "", &u, nil
	}
	r := store.Role(ref)
	if !r.Valid() {
		return "", nil, fmt.Errorf("unknown role %q", ref)
	}
	return r, nil, nil
}

// Authz plugs assignments into the core authorizer.
type Authz struct{ Store store.Store }

type assignKey struct {
	org, user uuid.UUID
}

type customRoleKey uuid.UUID

// assignments loads (and memoizes per request) the member's assignments in org.
func (a *Authz) assignments(ctx context.Context, orgID, userID uuid.UUID) ([]store.RoleAssignment, error) {
	v, err := rbac.ScopeFrom(ctx).Memo(assignKey{orgID, userID}, func() (any, error) {
		return a.Store.ListRoleAssignments(ctx, store.RoleAssignmentFilter{OrgID: orgID, UserID: &userID})
	})
	if err != nil {
		return nil, err
	}
	return v.([]store.RoleAssignment), nil
}

// effective returns the assignment that applies to projectID (project first, then org-wide), or nil.
func effective(as []store.RoleAssignment, projectID *uuid.UUID) *store.RoleAssignment {
	if projectID != nil {
		for i := range as {
			if as[i].ProjectID == *projectID {
				return &as[i]
			}
		}
	}
	for i := range as {
		if as[i].ProjectID == store.NilID {
			return &as[i]
		}
	}
	return nil
}

// Override applies built-in per-project role assignments (core rbac.Override).
func (a *Authz) Override(ctx context.Context, p *auth.Principal, orgID uuid.UUID, projectID *uuid.UUID, base store.Role) (store.Role, bool, error) {
	if p == nil || p.User == nil || base == store.RoleOwner || projectID == nil {
		return "", false, nil
	}
	as, err := a.assignments(ctx, orgID, p.User.ID)
	if err != nil {
		return "", false, err
	}
	e := effective(as, projectID)
	if e == nil || e.CustomRoleID != nil || e.ProjectID == store.NilID || !e.Role.Valid() || e.Role == store.RoleOwner {
		return "", false, nil
	}
	return e.Role, true, nil
}

func (a *Authz) customRole(ctx context.Context, id uuid.UUID) (store.CustomRole, error) {
	v, err := rbac.ScopeFrom(ctx).Memo(customRoleKey(id), func() (any, error) { return a.Store.GetCustomRole(ctx, id) })
	if err != nil {
		return store.CustomRole{}, err
	}
	return v.(store.CustomRole), nil
}

// Decide applies custom roles (core rbac.Decider). Decisions:
//   - API keys, owners, admin/ee operations and members without a custom role: core rule.
//   - Mapped operation: allowed iff the custom role holds the operation's permission.
//   - Unmapped operation: allowed iff the custom role's built-in tier (see Tier) meets the minimum role.
func (a *Authz) Decide(ctx context.Context, p *auth.Principal, orgID uuid.UUID, projectID *uuid.UUID, role, min store.Role) (bool, bool, error) {
	if p == nil || p.User == nil || role == store.RoleOwner {
		return false, false, nil
	}
	op := rbac.ScopeFrom(ctx).Operation()
	if op == "" || IsAdminOp(op) {
		return false, false, nil
	}
	as, err := a.assignments(ctx, orgID, p.User.ID)
	if err != nil {
		return false, false, err
	}
	e := effective(as, projectID)
	if e == nil || e.CustomRoleID == nil {
		return false, false, nil
	}
	cr, err := a.customRole(ctx, *e.CustomRoleID)
	if errors.Is(err, store.ErrNotFound) {
		return false, false, nil // deleted concurrently: assignments cascade away
	} else if err != nil {
		return false, false, err
	}
	if perm, ok := PermissionFor(op); ok {
		return slices.Contains(cr.Permissions, perm), true, nil
	}
	return Tier(cr.Permissions).AtLeast(min), true, nil
}
