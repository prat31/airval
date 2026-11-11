//go:build ee

package scim

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/prat31/airval/internal/store"
)

type memberRef struct {
	Value   string `json:"value"`
	Display string `json:"display,omitempty"`
}

type groupIn struct {
	DisplayName string      `json:"displayName"`
	ExternalID  string      `json:"externalId"`
	Members     []memberRef `json:"members"`
}

func (s *Service) groupJSON(ctx context.Context, g store.SCIMGroup) map[string]any {
	ms := make([]map[string]string, 0, len(g.Members))
	for _, id := range g.Members {
		m := map[string]string{"value": id.String(), "$ref": s.baseURL() + "/Users/" + id.String()}
		if su, err := s.h.Host.Store().GetSCIMUser(ctx, g.OrgID, id); err == nil {
			m["display"] = su.UserName
		}
		ms = append(ms, m)
	}
	out := map[string]any{"schemas": []string{groupSchema}, "id": g.ID.String(), "displayName": g.DisplayName,
		"members": ms, "meta": map[string]any{"resourceType": "Group", "created": g.CreatedAt.Format(time.RFC3339),
			"lastModified": g.UpdatedAt.Format(time.RFC3339), "location": s.baseURL() + "/Groups/" + g.ID.String()}}
	if g.ExternalID != "" {
		out["externalId"] = g.ExternalID
	}
	return out
}

// memberIDs resolves member references to provisioned users of the org (unknown ids are rejected).
func (s *Service) memberIDs(ctx context.Context, orgID uuid.UUID, refs []memberRef) ([]uuid.UUID, error) {
	out := make([]uuid.UUID, 0, len(refs))
	for _, m := range refs {
		id, err := uuid.Parse(m.Value)
		if err != nil {
			return nil, scimErr(http.StatusBadRequest, "invalidValue", "unknown member %q", m.Value)
		}
		if _, err := s.h.Host.Store().GetSCIMUser(ctx, orgID, id); err != nil {
			return nil, scimErr(http.StatusBadRequest, "invalidValue", "unknown member %q", m.Value)
		}
		if !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	return out, nil
}

func (s *Service) listGroups(w http.ResponseWriter, r *http.Request) error {
	t := tokenFrom(r.Context())
	attr, val, err := parseFilter(r.URL.Query().Get("filter"))
	if err != nil {
		return err
	}
	f := store.SCIMGroupFilter{OrgID: t.OrgID}
	switch attr {
	case "":
	case "displayname":
		f.DisplayName = val
	case "externalid":
		f.ExternalID = val
	default:
		return scimErr(http.StatusBadRequest, "invalidFilter", "filtering on %q is not supported", attr)
	}
	start, count := paging(r)
	f.Offset, f.Limit = start-1, max(count, 1)
	gs, total, err := s.h.Host.Store().ListSCIMGroups(r.Context(), f)
	if err != nil {
		return err
	}
	var items []any
	if count > 0 {
		for _, g := range gs {
			items = append(items, s.groupJSON(r.Context(), g))
		}
	}
	return writeSCIM(w, http.StatusOK, listResponse(total, start, items))
}

func (s *Service) groupParam(r *http.Request) (store.SCIMGroup, error) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		return store.SCIMGroup{}, store.ErrNotFound
	}
	return s.h.Host.Store().GetSCIMGroup(r.Context(), tokenFrom(r.Context()).OrgID, id)
}

func (s *Service) getGroup(w http.ResponseWriter, r *http.Request) error {
	g, err := s.groupParam(r)
	if err != nil {
		return err
	}
	return writeSCIM(w, http.StatusOK, s.groupJSON(r.Context(), g))
}

// saveGroup persists g (create or update) and re-syncs roles of every member that joined or left.
func (s *Service) saveGroup(r *http.Request, g *store.SCIMGroup, before []uuid.UUID, create bool) error {
	ctx := r.Context()
	return s.h.Host.Store().WithTx(ctx, func(tx store.Store) error {
		var err error
		if create {
			err = tx.CreateSCIMGroup(ctx, g)
		} else {
			err = tx.UpdateSCIMGroup(ctx, g)
		}
		if err != nil {
			return err
		}
		affected := append(slices.Clone(before), g.Members...)
		slices.SortFunc(affected, func(a, b uuid.UUID) int { return strings.Compare(a.String(), b.String()) })
		for _, id := range slices.Compact(affected) {
			if err := s.syncRoles(ctx, tx, g.OrgID, id); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *Service) createGroup(w http.ResponseWriter, r *http.Request) error {
	var in groupIn
	if err := decode(r, &in); err != nil {
		return err
	}
	in.DisplayName = strings.TrimSpace(in.DisplayName)
	if in.DisplayName == "" {
		return scimErr(http.StatusBadRequest, "invalidValue", "displayName is required")
	}
	t := tokenFrom(r.Context())
	members, err := s.memberIDs(r.Context(), t.OrgID, in.Members)
	if err != nil {
		return err
	}
	g := store.SCIMGroup{OrgID: t.OrgID, DisplayName: in.DisplayName, ExternalID: in.ExternalID, Members: members}
	if err := s.saveGroup(r, &g, nil, true); err != nil {
		return err
	}
	s.audit(r, "scim.group.create", "scim_group:"+g.ID.String(), map[string]any{"displayName": g.DisplayName, "members": len(members)})
	w.Header().Set("Location", s.baseURL()+"/Groups/"+g.ID.String())
	return writeSCIM(w, http.StatusCreated, s.groupJSON(r.Context(), g))
}

func (s *Service) replaceGroup(w http.ResponseWriter, r *http.Request) error {
	g, err := s.groupParam(r)
	if err != nil {
		return err
	}
	var in groupIn
	if err := decode(r, &in); err != nil {
		return err
	}
	if strings.TrimSpace(in.DisplayName) == "" {
		return scimErr(http.StatusBadRequest, "invalidValue", "displayName is required")
	}
	members, err := s.memberIDs(r.Context(), g.OrgID, in.Members)
	if err != nil {
		return err
	}
	before := g.Members
	g.DisplayName, g.ExternalID, g.Members = strings.TrimSpace(in.DisplayName), in.ExternalID, members
	if err := s.saveGroup(r, &g, before, false); err != nil {
		return err
	}
	s.audit(r, "scim.group.replace", "scim_group:"+g.ID.String(), map[string]any{"displayName": g.DisplayName, "members": len(members)})
	return writeSCIM(w, http.StatusOK, s.groupJSON(r.Context(), g))
}

var memberPathRe = regexp.MustCompile(`(?i)^members\[\s*value\s+eq\s+"([^"]+)"\s*\]$`)

func (s *Service) patchGroup(w http.ResponseWriter, r *http.Request) error {
	g, err := s.groupParam(r)
	if err != nil {
		return err
	}
	var in patchBody
	if err := decode(r, &in); err != nil {
		return err
	}
	before := slices.Clone(g.Members)
	ctx := r.Context()
	refs := func(raw json.RawMessage) ([]uuid.UUID, error) {
		var ms []memberRef
		if err := json.Unmarshal(raw, &ms); err != nil {
			var one memberRef
			if err := json.Unmarshal(raw, &one); err != nil {
				return nil, scimErr(http.StatusBadRequest, "invalidValue", "members must be a list of {value}")
			}
			ms = []memberRef{one}
		}
		return s.memberIDs(ctx, g.OrgID, ms)
	}
	for _, op := range in.Operations {
		kind := strings.ToLower(op.Op)
		path := strings.TrimSpace(op.Path)
		switch {
		case strings.EqualFold(path, "members") && kind == "add":
			ids, err := refs(op.Value)
			if err != nil {
				return err
			}
			for _, id := range ids {
				if !slices.Contains(g.Members, id) {
					g.Members = append(g.Members, id)
				}
			}
		case strings.EqualFold(path, "members") && kind == "replace":
			ids, err := refs(op.Value)
			if err != nil {
				return err
			}
			g.Members = ids
		case strings.EqualFold(path, "members") && kind == "remove":
			if len(op.Value) == 0 || string(op.Value) == "null" {
				g.Members = []uuid.UUID{}
				continue
			}
			var ms []memberRef
			_ = json.Unmarshal(op.Value, &ms)
			for _, m := range ms {
				if id, err := uuid.Parse(m.Value); err == nil {
					g.Members = removeID(g.Members, id)
				}
			}
		case memberPathRe.MatchString(path) && kind == "remove":
			if id, err := uuid.Parse(memberPathRe.FindStringSubmatch(path)[1]); err == nil {
				g.Members = removeID(g.Members, id)
			}
		case (strings.EqualFold(path, "displayName") || path == "") && kind != "remove":
			if path == "" {
				var obj struct {
					DisplayName string      `json:"displayName"`
					ExternalID  *string     `json:"externalId"`
					Members     []memberRef `json:"members"`
				}
				if err := json.Unmarshal(op.Value, &obj); err != nil {
					return scimErr(http.StatusBadRequest, "invalidValue", "invalid patch value")
				}
				if obj.DisplayName != "" {
					g.DisplayName = obj.DisplayName
				}
				if obj.ExternalID != nil {
					g.ExternalID = *obj.ExternalID
				}
				if obj.Members != nil {
					ids, err := s.memberIDs(ctx, g.OrgID, obj.Members)
					if err != nil {
						return err
					}
					if kind == "add" {
						for _, id := range ids {
							if !slices.Contains(g.Members, id) {
								g.Members = append(g.Members, id)
							}
						}
					} else {
						g.Members = ids
					}
				}
				continue
			}
			var v string
			if err := json.Unmarshal(op.Value, &v); err != nil || strings.TrimSpace(v) == "" {
				return scimErr(http.StatusBadRequest, "invalidValue", "displayName must be a non-empty string")
			}
			g.DisplayName = strings.TrimSpace(v)
		case strings.EqualFold(path, "externalId"):
			var v string
			_ = json.Unmarshal(op.Value, &v)
			g.ExternalID = v
		default:
			return scimErr(http.StatusBadRequest, "invalidPath", "unsupported patch %s %q", op.Op, op.Path)
		}
	}
	if err := s.saveGroup(r, &g, before, false); err != nil {
		return err
	}
	s.audit(r, "scim.group.patch", "scim_group:"+g.ID.String(), map[string]any{"displayName": g.DisplayName, "members": len(g.Members)})
	return writeSCIM(w, http.StatusOK, s.groupJSON(ctx, g))
}

func (s *Service) deleteGroup(w http.ResponseWriter, r *http.Request) error {
	g, err := s.groupParam(r)
	if err != nil {
		return err
	}
	ctx := r.Context()
	err = s.h.Host.Store().WithTx(ctx, func(tx store.Store) error {
		if err := tx.DeleteSCIMGroup(ctx, g.OrgID, g.ID); err != nil {
			return err
		}
		for _, id := range g.Members {
			if err := s.syncRoles(ctx, tx, g.OrgID, id); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	s.audit(r, "scim.group.delete", "scim_group:"+g.ID.String(), map[string]any{"displayName": g.DisplayName})
	w.WriteHeader(http.StatusNoContent)
	return nil
}
