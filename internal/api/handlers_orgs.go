package api

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/prat31/airval/internal/api/gen"
	"github.com/prat31/airval/internal/auth"
	"github.com/prat31/airval/internal/store"
)

// ---- organizations ----

func (s *Server) ListOrgs(ctx context.Context, _ gen.ListOrgsRequestObject) (gen.ListOrgsResponseObject, error) {
	me, err := s.me(ctx, s.principal(ctx))
	if err != nil {
		return nil, err
	}
	return gen.ListOrgs200JSONResponse{Items: me.Orgs}, nil
}

func (s *Server) CreateOrg(ctx context.Context, req gen.CreateOrgRequestObject) (gen.CreateOrgResponseObject, error) {
	p, err := s.requireSession(ctx)
	if err != nil {
		return nil, err
	}
	if req.Body == nil || strings.TrimSpace(req.Body.Name) == "" {
		return nil, invalid("name is required")
	}
	o := store.Organization{Name: strings.TrimSpace(req.Body.Name), Slug: trimmed(req.Body.Slug)}
	if o.Slug == "" {
		o.Slug = auth.Slugify(o.Name)
	}
	err = s.Store.WithTx(ctx, func(tx store.Store) error {
		if err := tx.CreateOrg(ctx, &o); err != nil {
			return err
		}
		return tx.UpsertMembership(ctx, &store.Membership{OrgID: o.ID, UserID: p.User.ID, Role: store.RoleOwner})
	})
	if err != nil {
		return nil, err
	}
	return gen.CreateOrg201JSONResponse(toOrg(o, store.RoleOwner)), nil
}

func (s *Server) GetOrg(ctx context.Context, req gen.GetOrgRequestObject) (gen.GetOrgResponseObject, error) {
	role, err := s.Authz.Role(ctx, s.principal(ctx), req.Org, nil)
	if err != nil {
		return nil, err
	}
	o, err := s.Store.GetOrg(ctx, req.Org)
	if err != nil {
		return nil, err
	}
	return gen.GetOrg200JSONResponse(toOrg(o, role)), nil
}

func (s *Server) UpdateOrg(ctx context.Context, req gen.UpdateOrgRequestObject) (gen.UpdateOrgResponseObject, error) {
	if err := s.require(ctx, req.Org, nil, store.RoleAdmin); err != nil {
		return nil, err
	}
	o, err := s.Store.GetOrg(ctx, req.Org)
	if err != nil {
		return nil, err
	}
	if b := req.Body; b != nil {
		if b.Name != nil && strings.TrimSpace(*b.Name) != "" {
			o.Name = strings.TrimSpace(*b.Name)
		}
		if b.Slug != nil && *b.Slug != "" {
			o.Slug = *b.Slug
		}
	}
	if err := s.Store.UpdateOrg(ctx, &o); err != nil {
		return nil, err
	}
	s.audit(ctx, o.ID, "org.update", "org:"+o.ID.String(), map[string]string{"name": o.Name, "slug": o.Slug})
	role, _ := s.Authz.Role(ctx, s.principal(ctx), o.ID, nil)
	return gen.UpdateOrg200JSONResponse(toOrg(o, role)), nil
}

func (s *Server) DeleteOrg(ctx context.Context, req gen.DeleteOrgRequestObject) (gen.DeleteOrgResponseObject, error) {
	if err := s.require(ctx, req.Org, nil, store.RoleOwner); err != nil {
		return nil, err
	}
	if err := s.Store.DeleteOrg(ctx, req.Org); err != nil {
		return nil, err
	}
	return gen.DeleteOrg204Response{}, nil
}

// ---- members ----

func (s *Server) ListMembers(ctx context.Context, req gen.ListMembersRequestObject) (gen.ListMembersResponseObject, error) {
	if err := s.require(ctx, req.Org, nil, store.RoleViewer); err != nil {
		return nil, err
	}
	pg, err := s.Store.ListMembers(ctx, req.Org, listParams(req.Params.Cursor, req.Params.Limit))
	if err != nil {
		return nil, err
	}
	return gen.ListMembers200JSONResponse{Items: mapSlice(pg.Items, toMember), NextCursor: nextCursor(pg.NextCursor)}, nil
}

// callerRole returns the caller's role in org (for privilege-escalation checks).
func (s *Server) callerRole(ctx context.Context, org uuid.UUID) store.Role {
	r, _ := s.Authz.Role(ctx, s.principal(ctx), org, nil)
	return r
}

func (s *Server) AddMember(ctx context.Context, req gen.AddMemberRequestObject) (gen.AddMemberResponseObject, error) {
	if err := s.require(ctx, req.Org, nil, store.RoleAdmin); err != nil {
		return nil, err
	}
	b := req.Body
	if b == nil || strings.TrimSpace(string(b.Email)) == "" {
		return nil, invalid("email is required")
	}
	role := store.Role(b.Role)
	if !role.Valid() {
		return nil, invalid("invalid role %q", b.Role)
	}
	if role == store.RoleOwner && s.callerRole(ctx, req.Org) != store.RoleOwner {
		return nil, errorf(http.StatusForbidden, "forbidden", "only owners can grant the owner role")
	}
	var m store.Member
	err := s.Store.WithTx(ctx, func(tx store.Store) error {
		u, err := tx.GetUserByEmail(ctx, string(b.Email))
		if errors.Is(err, store.ErrNotFound) {
			if b.Password == nil {
				return invalid("no user with this email exists; provide an initial password to create the account")
			}
			if len(*b.Password) < auth.MinPasswordLen {
				return auth.ErrWeakPassword
			}
			h, err := auth.HashPassword(*b.Password)
			if err != nil {
				return err
			}
			name := trimmed(b.Name)
			if name == "" {
				name, _, _ = strings.Cut(string(b.Email), "@")
			}
			u = store.User{Email: string(b.Email), Name: name, PasswordHash: &h}
			if err := tx.CreateUser(ctx, &u); err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
		if _, err := tx.GetMembership(ctx, req.Org, u.ID); err == nil {
			return errorf(http.StatusConflict, "conflict", "%s is already a member", u.Email)
		}
		ms := store.Membership{OrgID: req.Org, UserID: u.ID, Role: role}
		if err := tx.UpsertMembership(ctx, &ms); err != nil {
			return err
		}
		m = store.Member{Membership: ms, User: u}
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.audit(ctx, req.Org, "member.add", "user:"+m.UserID.String(), map[string]string{"email": m.User.Email, "role": string(role)})
	return gen.AddMember201JSONResponse(toMember(m)), nil
}

func (s *Server) UpdateMember(ctx context.Context, req gen.UpdateMemberRequestObject) (gen.UpdateMemberResponseObject, error) {
	if err := s.require(ctx, req.Org, nil, store.RoleAdmin); err != nil {
		return nil, err
	}
	if req.Body == nil || !store.Role(req.Body.Role).Valid() {
		return nil, invalid("a valid role is required")
	}
	role := store.Role(req.Body.Role)
	caller := s.callerRole(ctx, req.Org)
	var out store.Member
	err := s.Store.WithTx(ctx, func(tx store.Store) error {
		cur, err := tx.GetMembership(ctx, req.Org, req.User)
		if err != nil {
			return err
		}
		if (role == store.RoleOwner || cur.Role == store.RoleOwner) && caller != store.RoleOwner {
			return errorf(http.StatusForbidden, "forbidden", "only owners can grant or revoke the owner role")
		}
		if cur.Role == store.RoleOwner && role != store.RoleOwner {
			if n, err := tx.CountOwners(ctx, req.Org); err != nil {
				return err
			} else if n <= 1 {
				return errorf(http.StatusConflict, "last_owner", "an organization must keep at least one owner")
			}
		}
		cur.Role = role
		if err := tx.UpsertMembership(ctx, &cur); err != nil {
			return err
		}
		u, err := tx.GetUser(ctx, req.User)
		out = store.Member{Membership: cur, User: u}
		return err
	})
	if err != nil {
		return nil, err
	}
	s.audit(ctx, req.Org, "member.update", "user:"+req.User.String(), map[string]string{"role": string(role)})
	return gen.UpdateMember200JSONResponse(toMember(out)), nil
}

func (s *Server) RemoveMember(ctx context.Context, req gen.RemoveMemberRequestObject) (gen.RemoveMemberResponseObject, error) {
	p := s.principal(ctx)
	self := p != nil && p.User != nil && p.User.ID == req.User
	min := store.RoleAdmin
	if self {
		min = store.RoleViewer
	}
	if err := s.require(ctx, req.Org, nil, min); err != nil {
		return nil, err
	}
	caller := s.callerRole(ctx, req.Org)
	err := s.Store.WithTx(ctx, func(tx store.Store) error {
		cur, err := tx.GetMembership(ctx, req.Org, req.User)
		if err != nil {
			return err
		}
		if cur.Role == store.RoleOwner {
			if caller != store.RoleOwner {
				return errorf(http.StatusForbidden, "forbidden", "only owners can remove an owner")
			}
			if n, err := tx.CountOwners(ctx, req.Org); err != nil {
				return err
			} else if n <= 1 {
				return errorf(http.StatusConflict, "last_owner", "an organization must keep at least one owner")
			}
		}
		return tx.DeleteMembership(ctx, req.Org, req.User)
	})
	if err != nil {
		return nil, err
	}
	s.audit(ctx, req.Org, "member.remove", "user:"+req.User.String(), nil)
	return gen.RemoveMember204Response{}, nil
}

// ---- API keys ----

func (s *Server) ListApiKeys(ctx context.Context, req gen.ListApiKeysRequestObject) (gen.ListApiKeysResponseObject, error) {
	if err := s.require(ctx, req.Org, nil, store.RoleAdmin); err != nil {
		return nil, err
	}
	pg, err := s.Store.ListAPIKeys(ctx, req.Org, listParams(req.Params.Cursor, req.Params.Limit))
	if err != nil {
		return nil, err
	}
	return gen.ListApiKeys200JSONResponse{Items: mapSlice(pg.Items, toAPIKey), NextCursor: nextCursor(pg.NextCursor)}, nil
}

func (s *Server) CreateApiKey(ctx context.Context, req gen.CreateApiKeyRequestObject) (gen.CreateApiKeyResponseObject, error) {
	if err := s.require(ctx, req.Org, nil, store.RoleAdmin); err != nil {
		return nil, err
	}
	b := req.Body
	if b == nil || strings.TrimSpace(b.Name) == "" {
		return nil, invalid("name is required")
	}
	role := store.Role(b.Role)
	if !role.Valid() {
		return nil, invalid("invalid role %q", b.Role)
	}
	if role.Rank() > s.callerRole(ctx, req.Org).Rank() {
		return nil, errorf(http.StatusForbidden, "forbidden", "an API key cannot have more privileges than its creator")
	}
	if b.ProjectId != nil {
		p, err := s.Store.GetProject(ctx, *b.ProjectId)
		if err != nil || p.OrgID != req.Org {
			return nil, invalid("project_id does not belong to this organization")
		}
	}
	key, prefix, hash := auth.NewAPIKey()
	k := store.APIKey{OrgID: req.Org, ProjectID: b.ProjectId, Name: strings.TrimSpace(b.Name), Prefix: prefix, Hash: hash,
		Role: role, CreatedBy: s.principal(ctx).UserID(), ExpiresAt: b.ExpiresAt}
	if err := s.Store.CreateAPIKey(ctx, &k); err != nil {
		return nil, err
	}
	s.audit(ctx, req.Org, "api_key.create", "api_key:"+k.ID.String(), map[string]string{"name": k.Name, "role": string(role)})
	a := toAPIKey(k)
	return gen.CreateApiKey201JSONResponse{Id: a.Id, OrgId: a.OrgId, ProjectId: a.ProjectId, Name: a.Name, Prefix: a.Prefix,
		Role: a.Role, CreatedBy: a.CreatedBy, ExpiresAt: a.ExpiresAt, CreatedAt: a.CreatedAt, Key: key}, nil
}

func (s *Server) DeleteApiKey(ctx context.Context, req gen.DeleteApiKeyRequestObject) (gen.DeleteApiKeyResponseObject, error) {
	if err := s.require(ctx, req.Org, nil, store.RoleAdmin); err != nil {
		return nil, err
	}
	if err := s.Store.DeleteAPIKey(ctx, req.Org, req.Key); err != nil {
		return nil, err
	}
	s.audit(ctx, req.Org, "api_key.delete", "api_key:"+req.Key.String(), nil)
	return gen.DeleteApiKey204Response{}, nil
}

// ---- audit log ----

func (s *Server) ListAuditLog(ctx context.Context, req gen.ListAuditLogRequestObject) (gen.ListAuditLogResponseObject, error) {
	if err := s.require(ctx, req.Org, nil, store.RoleAdmin); err != nil {
		return nil, err
	}
	pg, err := s.Store.ListAudit(ctx, req.Org, listParams(req.Params.Cursor, req.Params.Limit))
	if err != nil {
		return nil, err
	}
	return gen.ListAuditLog200JSONResponse{Items: mapSlice(pg.Items, toAudit), NextCursor: nextCursor(pg.NextCursor)}, nil
}
