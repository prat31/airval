// Package rbac resolves a principal's role in an organization/project and enforces minimum roles.
//
// Rules (core): org members have their membership role everywhere in the org. API keys have their
// key role in their org; project-scoped keys only reach that project and cannot perform org-level
// operations. Per-project role overrides are an ee feature (see ext.RoleResolver).
package rbac

import (
	"context"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/airval/airval/internal/auth"
	"github.com/airval/airval/internal/store"
)

var (
	// ErrUnauthenticated: no principal.
	ErrUnauthenticated = errors.New("rbac: authentication required")
	// ErrNoAccess: the caller is not a member (reported as 404 to avoid leaking existence).
	ErrNoAccess = errors.New("rbac: no access")
	// ErrForbidden: member, but the role is insufficient.
	ErrForbidden = errors.New("rbac: insufficient role")
)

// Override lets ee adjust the effective role (e.g. per-project roles). It returns ok=false to keep the core role.
type Override func(ctx context.Context, p *auth.Principal, orgID uuid.UUID, projectID *uuid.UUID, base store.Role) (store.Role, bool, error)

// Authorizer answers access questions.
type Authorizer struct {
	Store    store.OrgStore
	Override Override
}

// Role returns the caller's effective role for orgID (and optionally projectID).
func (a *Authorizer) Role(ctx context.Context, p *auth.Principal, orgID uuid.UUID, projectID *uuid.UUID) (store.Role, error) {
	if p == nil {
		return "", ErrUnauthenticated
	}
	var role store.Role
	switch {
	case p.APIKey != nil:
		k := p.APIKey
		if k.OrgID != orgID {
			return "", ErrNoAccess
		}
		if k.ProjectID != nil && (projectID == nil || *projectID != *k.ProjectID) {
			return "", ErrNoAccess
		}
		role = k.Role
	case p.User != nil:
		m, err := a.Store.GetMembership(ctx, orgID, p.User.ID)
		if errors.Is(err, store.ErrNotFound) {
			return "", ErrNoAccess
		} else if err != nil {
			return "", err
		}
		role = m.Role
	default:
		return "", ErrUnauthenticated
	}
	if a.Override != nil {
		if r, ok, err := a.Override(ctx, p, orgID, projectID, role); err != nil {
			return "", err
		} else if ok {
			role = r
		}
	}
	return role, nil
}

// Require returns nil when the caller has at least min in orgID (and projectID when given).
func (a *Authorizer) Require(ctx context.Context, p *auth.Principal, orgID uuid.UUID, projectID *uuid.UUID, min store.Role) error {
	role, err := a.Role(ctx, p, orgID, projectID)
	if err != nil {
		return err
	}
	if !role.AtLeast(min) {
		return ErrForbidden
	}
	return nil
}

// OrgIDs returns the organizations the caller can see (for unscoped list queries) and, for
// project-scoped API keys, the single project it is restricted to.
func (a *Authorizer) OrgIDs(ctx context.Context, p *auth.Principal) ([]uuid.UUID, *uuid.UUID, error) {
	switch {
	case p == nil:
		return nil, nil, ErrUnauthenticated
	case p.APIKey != nil:
		return []uuid.UUID{p.APIKey.OrgID}, p.APIKey.ProjectID, nil
	case p.User != nil:
		ms, err := a.Store.ListUserOrgs(ctx, p.User.ID)
		if err != nil {
			return nil, nil, err
		}
		ids := make([]uuid.UUID, len(ms))
		for i, m := range ms {
			ids[i] = m.ID
		}
		return ids, nil, nil
	}
	return nil, nil, ErrUnauthenticated
}

// RequireOrgRole is chi middleware for raw (non-generated) routes with an {orgParam} URL parameter.
// onErr writes the error response.
func (a *Authorizer) RequireOrgRole(min store.Role, orgParam string, onErr func(http.ResponseWriter, *http.Request, error)) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			orgID, err := uuid.Parse(chi.URLParam(r, orgParam))
			if err != nil {
				onErr(w, r, ErrNoAccess)
				return
			}
			if err := a.Require(r.Context(), auth.FromContext(r.Context()), orgID, nil, min); err != nil {
				onErr(w, r, err)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
