//go:build ee

// Package scim implements SCIM 2.0 provisioning (RFC 7643/7644) for users and groups, authenticated by a
// per-organization bearer token. Users map to members of the token's organization; SCIM groups map to roles
// through the org's SCIM settings (group display name → built-in or custom role).
//
// Deprovisioning (DELETE, or active=false) removes the membership and signs the user out; the user row is
// kept because a user can belong to several organizations.
package scim

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/prat31/airval/ee/internal/httpx"
	"github.com/prat31/airval/ee/sso"
	"github.com/prat31/airval/internal/auth"
	"github.com/prat31/airval/internal/store"
)

const (
	userSchema   = "urn:ietf:params:scim:schemas:core:2.0:User"
	groupSchema  = "urn:ietf:params:scim:schemas:core:2.0:Group"
	listSchema   = "urn:ietf:params:scim:api:messages:2.0:ListResponse"
	errorSchema  = "urn:ietf:params:scim:api:messages:2.0:Error"
	patchSchema  = "urn:ietf:params:scim:api:messages:2.0:PatchOp"
	contentType  = "application/scim+json"
	TokenPrefix  = "airvscim_"
	settingsKey  = "scim"
	maxBody      = 1 << 20
	defaultCount = 100
)

// Settings is the org's SCIM role mapping.
type Settings struct {
	DefaultRole store.Role        `json:"default_role"`
	GroupRoles  []sso.MappingRule `json:"group_roles"`
}

// Service serves SCIM and its administration.
type Service struct {
	h   httpx.H
	now func() time.Time
}

// New returns the SCIM service.
func New(h httpx.H) *Service {
	return &Service{h: h, now: func() time.Time { return time.Now().UTC() }}
}

// Routes registers SCIM and admin endpoints.
func (s *Service) Routes(r chi.Router) {
	authed := r.With(s.h.Host.Authenticate)
	authed.Get("/v1/orgs/{org}/scim-tokens", s.h.Wrap("listSCIMTokens", s.listTokens))
	authed.Post("/v1/orgs/{org}/scim-tokens", s.h.Wrap("createSCIMToken", s.createToken))
	authed.Delete("/v1/orgs/{org}/scim-tokens/{id}", s.h.Wrap("deleteSCIMToken", s.deleteToken))
	authed.Get("/v1/orgs/{org}/scim-settings", s.h.Wrap("getSCIMSettings", s.getSettings))
	authed.Put("/v1/orgs/{org}/scim-settings", s.h.Wrap("putSCIMSettings", s.putSettings))

	sc := r.With(s.authenticate)
	sc.Get("/scim/v2/ServiceProviderConfig", s.wrap(s.spConfig))
	sc.Get("/scim/v2/ResourceTypes", s.wrap(s.resourceTypes))
	sc.Get("/scim/v2/Users", s.wrap(s.listUsers))
	sc.Post("/scim/v2/Users", s.wrap(s.createUser))
	sc.Get("/scim/v2/Users/{id}", s.wrap(s.getUser))
	sc.Put("/scim/v2/Users/{id}", s.wrap(s.replaceUser))
	sc.Patch("/scim/v2/Users/{id}", s.wrap(s.patchUser))
	sc.Delete("/scim/v2/Users/{id}", s.wrap(s.deleteUser))
	sc.Get("/scim/v2/Groups", s.wrap(s.listGroups))
	sc.Post("/scim/v2/Groups", s.wrap(s.createGroup))
	sc.Get("/scim/v2/Groups/{id}", s.wrap(s.getGroup))
	sc.Put("/scim/v2/Groups/{id}", s.wrap(s.replaceGroup))
	sc.Patch("/scim/v2/Groups/{id}", s.wrap(s.patchGroup))
	sc.Delete("/scim/v2/Groups/{id}", s.wrap(s.deleteGroup))
}

// ---- errors & plumbing ----

// Error is a SCIM error response.
type Error struct {
	Status   int
	ScimType string
	Detail   string
}

func (e *Error) Error() string { return e.Detail }

func scimErr(status int, scimType, format string, args ...any) error {
	return &Error{Status: status, ScimType: scimType, Detail: fmt.Sprintf(format, args...)}
}

func writeSCIM(w http.ResponseWriter, status int, v any) error {
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(status)
	return json.NewEncoder(w).Encode(v)
}

type tokenKey struct{}

func tokenFrom(ctx context.Context) store.SCIMToken {
	t, _ := ctx.Value(tokenKey{}).(store.SCIMToken)
	return t
}

func (s *Service) wrap(fn func(w http.ResponseWriter, r *http.Request) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		err := fn(w, r)
		if err == nil {
			return
		}
		var se *Error
		switch {
		case errors.As(err, &se):
		case errors.Is(err, store.ErrNotFound):
			se = &Error{Status: http.StatusNotFound, Detail: "resource not found"}
		case errors.Is(err, store.ErrConflict):
			se = &Error{Status: http.StatusConflict, ScimType: "uniqueness", Detail: "resource already exists"}
		default:
			s.h.Host.Logger().Error("scim: request failed", "path", r.URL.Path, "err", err)
			se = &Error{Status: http.StatusInternalServerError, Detail: "internal error"}
		}
		body := map[string]any{"schemas": []string{errorSchema}, "status": strconv.Itoa(se.Status), "detail": se.Detail}
		if se.ScimType != "" {
			body["scimType"] = se.ScimType
		}
		_ = writeSCIM(w, se.Status, body)
	}
}

// authenticate checks the SCIM bearer token and binds the request to its organization.
func (s *Service) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tok, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || !strings.HasPrefix(tok, TokenPrefix) {
			s.wrap(func(http.ResponseWriter, *http.Request) error {
				return scimErr(http.StatusUnauthorized, "", "a SCIM bearer token is required")
			})(w, r)
			return
		}
		t, err := s.h.Host.Store().GetSCIMTokenByHash(r.Context(), auth.HashToken(tok))
		if err != nil {
			s.wrap(func(http.ResponseWriter, *http.Request) error {
				if errors.Is(err, store.ErrNotFound) {
					return scimErr(http.StatusUnauthorized, "", "invalid SCIM token")
				}
				return err
			})(w, r)
			return
		}
		if t.LastUsedAt == nil || s.now().Sub(*t.LastUsedAt) > time.Minute {
			_ = s.h.Host.Store().TouchSCIMToken(r.Context(), t.ID, s.now())
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), tokenKey{}, t)))
	})
}

func decode(r *http.Request, v any) error {
	if err := json.NewDecoder(io.LimitReader(r.Body, maxBody)).Decode(v); err != nil {
		return scimErr(http.StatusBadRequest, "invalidSyntax", "invalid JSON body: %v", err)
	}
	return nil
}

func (s *Service) audit(r *http.Request, action, target string, diff any) {
	t := tokenFrom(r.Context())
	e := store.AuditEntry{OrgID: t.OrgID, Actor: "scim:" + t.Prefix, Action: action, Target: target,
		IP: httpx.ClientIP(r), UserAgent: r.UserAgent()}
	if diff != nil {
		e.Diff, _ = json.Marshal(diff)
	}
	if err := s.h.Host.Store().AppendAudit(context.WithoutCancel(r.Context()), &e); err != nil {
		s.h.Host.Logger().Warn("scim: audit", "err", err)
	}
}

func (s *Service) baseURL() string {
	if c := s.h.Host.Config(); c != nil && c.Server.BaseURL != "" {
		return strings.TrimRight(c.Server.BaseURL, "/") + "/scim/v2"
	}
	return "/scim/v2"
}

// ---- filtering & paging ----

var filterRe = regexp.MustCompile(`^\s*([A-Za-z][A-Za-z0-9._]*)\s+(?i:eq)\s+"((?:[^"\\]|\\.)*)"\s*$`)

// parseFilter supports `attr eq "value"` (RFC 7644 §3.4.2.2 basics). Attribute names are case-insensitive.
func parseFilter(f string) (string, string, error) {
	if strings.TrimSpace(f) == "" {
		return "", "", nil
	}
	m := filterRe.FindStringSubmatch(f)
	if m == nil {
		return "", "", scimErr(http.StatusBadRequest, "invalidFilter", "only `attribute eq \"value\"` filters are supported")
	}
	v, err := strconv.Unquote(`"` + m[2] + `"`)
	if err != nil {
		return "", "", scimErr(http.StatusBadRequest, "invalidFilter", "invalid filter value")
	}
	return strings.ToLower(m[1]), v, nil
}

func paging(r *http.Request) (start, count int) {
	start, count = 1, defaultCount
	if v, err := strconv.Atoi(r.URL.Query().Get("startIndex")); err == nil && v > 1 {
		start = v
	}
	if v, err := strconv.Atoi(r.URL.Query().Get("count")); err == nil && v >= 0 {
		count = min(v, 1000)
	}
	return start, count
}

func listResponse(total, start int, items []any) map[string]any {
	if items == nil {
		items = []any{}
	}
	return map[string]any{"schemas": []string{listSchema}, "totalResults": total, "startIndex": start,
		"itemsPerPage": len(items), "Resources": items}
}

// ---- discovery ----

func (s *Service) spConfig(w http.ResponseWriter, _ *http.Request) error {
	return writeSCIM(w, http.StatusOK, map[string]any{
		"schemas":        []string{"urn:ietf:params:scim:schemas:core:2.0:ServiceProviderConfig"},
		"patch":          map[string]bool{"supported": true},
		"bulk":           map[string]any{"supported": false, "maxOperations": 0, "maxPayloadSize": 0},
		"filter":         map[string]any{"supported": true, "maxResults": 1000},
		"changePassword": map[string]bool{"supported": false},
		"sort":           map[string]bool{"supported": false},
		"etag":           map[string]bool{"supported": false},
		"authenticationSchemes": []map[string]any{{"type": "oauthbearertoken", "name": "Bearer token",
			"description": "Per-organization SCIM token created in airval settings", "primary": true}},
	})
}

func (s *Service) resourceTypes(w http.ResponseWriter, _ *http.Request) error {
	rt := func(name, schema string) map[string]any {
		return map[string]any{"schemas": []string{"urn:ietf:params:scim:schemas:core:2.0:ResourceType"}, "id": name,
			"name": name, "endpoint": "/" + name + "s", "schema": schema}
	}
	items := []any{rt("User", userSchema), rt("Group", groupSchema)}
	return writeSCIM(w, http.StatusOK, listResponse(len(items), 1, items))
}

// ---- admin: tokens & settings ----

type tokenJSON struct {
	ID         uuid.UUID  `json:"id"`
	Name       string     `json:"name"`
	Prefix     string     `json:"prefix"`
	LastUsedAt *time.Time `json:"last_used_at"`
	CreatedAt  time.Time  `json:"created_at"`
}

func toTokenJSON(t store.SCIMToken) tokenJSON {
	return tokenJSON{ID: t.ID, Name: t.Name, Prefix: t.Prefix, LastUsedAt: t.LastUsedAt, CreatedAt: t.CreatedAt}
}

func (s *Service) listTokens(w http.ResponseWriter, r *http.Request) error {
	orgID, _, err := s.h.RequireOrg(r, store.RoleOwner)
	if err != nil {
		return err
	}
	ts, err := s.h.Host.Store().ListSCIMTokens(r.Context(), orgID)
	if err != nil {
		return err
	}
	items := make([]tokenJSON, len(ts))
	for i, t := range ts {
		items[i] = toTokenJSON(t)
	}
	return httpx.JSON(w, http.StatusOK, map[string]any{"items": items, "base_url": s.baseURL()})
}

func (s *Service) createToken(w http.ResponseWriter, r *http.Request) error {
	orgID, p, err := s.h.RequireOrg(r, store.RoleOwner)
	if err != nil {
		return err
	}
	if err := httpx.RequireUser(p); err != nil {
		return err
	}
	var in struct {
		Name string `json:"name"`
	}
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || len(in.Name) > 100 {
		return httpx.Invalid("name must be 1–100 characters")
	}
	raw := TokenPrefix + auth.NewToken()
	t := store.SCIMToken{OrgID: orgID, Name: in.Name, Prefix: raw[:len(TokenPrefix)+6], Hash: auth.HashToken(raw), CreatedBy: p.UserID()}
	if err := s.h.Host.Store().CreateSCIMToken(r.Context(), &t); err != nil {
		return err
	}
	s.h.Audit(r, orgID, "scim_token.create", "scim_token:"+t.ID.String(), map[string]string{"name": t.Name})
	return httpx.JSON(w, http.StatusCreated, struct {
		tokenJSON
		Token   string `json:"token"`
		BaseURL string `json:"base_url"`
	}{toTokenJSON(t), raw, s.baseURL()})
}

func (s *Service) deleteToken(w http.ResponseWriter, r *http.Request) error {
	orgID, p, err := s.h.RequireOrg(r, store.RoleOwner)
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
	if err := s.h.Host.Store().DeleteSCIMToken(r.Context(), orgID, id); err != nil {
		return err
	}
	s.h.Audit(r, orgID, "scim_token.delete", "scim_token:"+id.String(), nil)
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Service) settings(ctx context.Context, orgID uuid.UUID) (Settings, error) {
	return loadSettings(ctx, s.h.Host.Store(), orgID)
}

func loadSettings(ctx context.Context, db store.Store, orgID uuid.UUID) (Settings, error) {
	st := Settings{DefaultRole: store.RoleViewer, GroupRoles: []sso.MappingRule{}}
	raw, err := db.GetOrgSetting(ctx, orgID, settingsKey)
	if errors.Is(err, store.ErrNotFound) {
		return st, nil
	} else if err != nil {
		return st, err
	}
	_ = json.Unmarshal(raw, &st)
	if !st.DefaultRole.Valid() || st.DefaultRole == store.RoleOwner {
		st.DefaultRole = store.RoleViewer
	}
	if st.GroupRoles == nil {
		st.GroupRoles = []sso.MappingRule{}
	}
	return st, nil
}

func (s *Service) getSettings(w http.ResponseWriter, r *http.Request) error {
	orgID, _, err := s.h.RequireOrg(r, store.RoleAdmin)
	if err != nil {
		return err
	}
	st, err := s.settings(r.Context(), orgID)
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusOK, st)
}

func (s *Service) putSettings(w http.ResponseWriter, r *http.Request) error {
	orgID, p, err := s.h.RequireOrg(r, store.RoleOwner)
	if err != nil {
		return err
	}
	if err := httpx.RequireUser(p); err != nil {
		return err
	}
	var in Settings
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	if !in.DefaultRole.Valid() || in.DefaultRole == store.RoleOwner {
		return httpx.Invalid("default_role must be admin, editor or viewer")
	}
	crs, err := s.h.Host.Store().ListCustomRoles(r.Context(), orgID)
	if err != nil {
		return err
	}
	if in.GroupRoles == nil {
		in.GroupRoles = []sso.MappingRule{}
	}
	for i := range in.GroupRoles {
		g := &in.GroupRoles[i]
		g.Value = strings.TrimSpace(g.Value)
		if g.Value == "" {
			return httpx.Invalid("group names must not be empty")
		}
		if err := sso.ValidateRoleRef(g.Role, crs); err != nil {
			return httpx.Invalid("%v", err)
		}
	}
	b, _ := json.Marshal(in)
	if err := s.h.Host.Store().PutOrgSetting(r.Context(), orgID, settingsKey, b); err != nil {
		return err
	}
	s.h.Audit(r, orgID, "scim_settings.update", "org:"+orgID.String(), in)
	return httpx.JSON(w, http.StatusOK, in)
}

// ---- role sync ----

// syncRoles recomputes a provisioned member's roles from their SCIM groups. Owners are never changed.
func (s *Service) syncRoles(ctx context.Context, tx store.Store, orgID, userID uuid.UUID) error {
	nilID := store.NilID
	m, err := tx.GetMembership(ctx, orgID, userID)
	if errors.Is(err, store.ErrNotFound) {
		return nil // inactive user
	} else if err != nil {
		return err
	}
	if m.Role == store.RoleOwner {
		return nil
	}
	st, err := loadSettings(ctx, tx, orgID)
	if err != nil {
		return err
	}
	groups, err := tx.ListUserSCIMGroups(ctx, orgID, userID)
	if err != nil {
		return err
	}
	names := make([]string, len(groups))
	for i, g := range groups {
		names[i] = g.DisplayName
	}
	role, custom, ok := sso.MapRole(st.GroupRoles, names)
	if !ok || role == "" {
		role = st.DefaultRole
	}
	if m.Role != role {
		m.Role = role
		if err := tx.UpsertMembership(ctx, &m); err != nil {
			return err
		}
	}
	as, err := tx.ListRoleAssignments(ctx, store.RoleAssignmentFilter{OrgID: orgID, UserID: &userID, ProjectID: &nilID})
	if err != nil {
		return err
	}
	var cur *store.RoleAssignment
	if len(as) > 0 {
		cur = &as[0]
	}
	switch {
	case custom != nil && (cur == nil || cur.Source == "scim"):
		return tx.SetRoleAssignment(ctx, &store.RoleAssignment{OrgID: orgID, UserID: userID, ProjectID: store.NilID,
			CustomRoleID: custom, Source: "scim"})
	case custom == nil && cur != nil && cur.Source == "scim":
		return tx.DeleteRoleAssignment(ctx, orgID, userID, store.NilID)
	}
	return nil
}
