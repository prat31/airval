//go:build ee

package scim

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/prat31/airval/internal/store"
)

type scimName struct {
	Formatted  string `json:"formatted,omitempty"`
	GivenName  string `json:"givenName,omitempty"`
	FamilyName string `json:"familyName,omitempty"`
}

type scimEmail struct {
	Value   string `json:"value"`
	Type    string `json:"type,omitempty"`
	Primary bool   `json:"primary,omitempty"`
}

// userIn is the writable part of a SCIM User.
type userIn struct {
	UserName    string      `json:"userName"`
	ExternalID  string      `json:"externalId"`
	Name        *scimName   `json:"name"`
	DisplayName string      `json:"displayName"`
	Emails      []scimEmail `json:"emails"`
	Active      *bool       `json:"active"`
}

// stored is what we keep in ee_scim_users.data.
type stored struct {
	Name        scimName `json:"name"`
	DisplayName string   `json:"displayName,omitempty"`
}

func (u userIn) email() string {
	for _, e := range u.Emails {
		if e.Primary && strings.Contains(e.Value, "@") {
			return strings.ToLower(strings.TrimSpace(e.Value))
		}
	}
	for _, e := range u.Emails {
		if strings.Contains(e.Value, "@") {
			return strings.ToLower(strings.TrimSpace(e.Value))
		}
	}
	if strings.Contains(u.UserName, "@") {
		return strings.ToLower(strings.TrimSpace(u.UserName))
	}
	return ""
}

func (u userIn) displayName() string {
	switch {
	case u.DisplayName != "":
		return u.DisplayName
	case u.Name != nil && u.Name.Formatted != "":
		return u.Name.Formatted
	case u.Name != nil:
		return strings.TrimSpace(u.Name.GivenName + " " + u.Name.FamilyName)
	}
	return ""
}

func (s *Service) userJSON(ctx context.Context, su store.SCIMUser) (map[string]any, error) {
	st := s.h.Host.Store()
	u, err := st.GetUser(ctx, su.UserID)
	if err != nil {
		return nil, err
	}
	var data stored
	_ = json.Unmarshal(su.Data, &data)
	groups, err := st.ListUserSCIMGroups(ctx, su.OrgID, su.UserID)
	if err != nil {
		return nil, err
	}
	gs := make([]map[string]string, len(groups))
	for i, g := range groups {
		gs[i] = map[string]string{"value": g.ID.String(), "display": g.DisplayName, "$ref": s.baseURL() + "/Groups/" + g.ID.String()}
	}
	out := map[string]any{
		"schemas": []string{userSchema}, "id": su.UserID.String(), "userName": su.UserName, "active": su.Active,
		"name": data.Name, "displayName": firstNonEmpty(data.DisplayName, u.Name),
		"emails": []scimEmail{{Value: u.Email, Type: "work", Primary: true}}, "groups": gs,
		"meta": map[string]any{"resourceType": "User", "created": su.CreatedAt.Format(time.RFC3339),
			"lastModified": su.UpdatedAt.Format(time.RFC3339), "location": s.baseURL() + "/Users/" + su.UserID.String()},
	}
	if su.ExternalID != "" {
		out["externalId"] = su.ExternalID
	}
	return out, nil
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if s != "" {
			return s
		}
	}
	return ""
}

func (s *Service) listUsers(w http.ResponseWriter, r *http.Request) error {
	t := tokenFrom(r.Context())
	attr, val, err := parseFilter(r.URL.Query().Get("filter"))
	if err != nil {
		return err
	}
	f := store.SCIMUserFilter{OrgID: t.OrgID}
	switch attr {
	case "":
	case "username", "emails.value", "emails":
		f.UserName = val
	case "externalid":
		f.ExternalID = val
	default:
		return scimErr(http.StatusBadRequest, "invalidFilter", "filtering on %q is not supported", attr)
	}
	start, count := paging(r)
	f.Offset, f.Limit = start-1, count
	var items []any
	total := 0
	if count > 0 {
		var us []store.SCIMUser
		if us, total, err = s.h.Host.Store().ListSCIMUsers(r.Context(), f); err != nil {
			return err
		}
		for _, su := range us {
			j, err := s.userJSON(r.Context(), su)
			if err != nil {
				return err
			}
			items = append(items, j)
		}
	} else if _, total, err = s.h.Host.Store().ListSCIMUsers(r.Context(), f); err != nil {
		return err
	}
	return writeSCIM(w, http.StatusOK, listResponse(total, start, items))
}

func (s *Service) userParam(r *http.Request) (store.SCIMUser, error) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		return store.SCIMUser{}, store.ErrNotFound
	}
	return s.h.Host.Store().GetSCIMUser(r.Context(), tokenFrom(r.Context()).OrgID, id)
}

func (s *Service) getUser(w http.ResponseWriter, r *http.Request) error {
	su, err := s.userParam(r)
	if err != nil {
		return err
	}
	j, err := s.userJSON(r.Context(), su)
	if err != nil {
		return err
	}
	return writeSCIM(w, http.StatusOK, j)
}

func (s *Service) createUser(w http.ResponseWriter, r *http.Request) error {
	var in userIn
	if err := decode(r, &in); err != nil {
		return err
	}
	in.UserName = strings.TrimSpace(in.UserName)
	if in.UserName == "" {
		return scimErr(http.StatusBadRequest, "invalidValue", "userName is required")
	}
	email := in.email()
	if email == "" {
		return scimErr(http.StatusBadRequest, "invalidValue", "an email address (userName or emails) is required")
	}
	t := tokenFrom(r.Context())
	ctx := r.Context()
	st := s.h.Host.Store()
	if _, n, err := st.ListSCIMUsers(ctx, store.SCIMUserFilter{OrgID: t.OrgID, UserName: in.UserName, Limit: 1}); err != nil {
		return err
	} else if n > 0 {
		return scimErr(http.StatusConflict, "uniqueness", "userName %q already exists", in.UserName)
	}
	var su store.SCIMUser
	err := st.WithTx(ctx, func(tx store.Store) error {
		u, err := tx.GetUserByEmail(ctx, email)
		if errors.Is(err, store.ErrNotFound) {
			u = store.User{Email: email, Name: firstNonEmpty(in.displayName(), strings.Split(email, "@")[0])}
			err = tx.CreateUser(ctx, &u)
		}
		if err != nil {
			return err
		}
		if _, err := tx.GetSCIMUser(ctx, t.OrgID, u.ID); err == nil {
			return scimErr(http.StatusConflict, "uniqueness", "user %s is already provisioned", email)
		}
		su = store.SCIMUser{OrgID: t.OrgID, UserID: u.ID, ExternalID: in.ExternalID, UserName: in.UserName,
			Active: in.Active == nil || *in.Active}
		su.Data = storedData(in)
		if err := tx.UpsertSCIMUser(ctx, &su); err != nil {
			return err
		}
		return s.applyActive(ctx, tx, su)
	})
	if err != nil {
		return err
	}
	s.audit(r, "scim.user.create", "user:"+su.UserID.String(), map[string]any{"userName": su.UserName, "active": su.Active})
	j, err := s.userJSON(ctx, su)
	if err != nil {
		return err
	}
	w.Header().Set("Location", s.baseURL()+"/Users/"+su.UserID.String())
	return writeSCIM(w, http.StatusCreated, j)
}

func storedData(in userIn) json.RawMessage {
	d := stored{DisplayName: in.DisplayName}
	if in.Name != nil {
		d.Name = *in.Name
	}
	b, _ := json.Marshal(d)
	return b
}

// applyActive makes membership match the SCIM active flag: active users are members (role from groups);
// inactive users lose their membership and sessions. The last owner is never removed.
func (s *Service) applyActive(ctx context.Context, tx store.Store, su store.SCIMUser) error {
	m, err := tx.GetMembership(ctx, su.OrgID, su.UserID)
	exists := err == nil
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return err
	}
	if su.Active {
		if !exists {
			st, err := loadSettings(ctx, tx, su.OrgID)
			if err != nil {
				return err
			}
			if err := tx.UpsertMembership(ctx, &store.Membership{OrgID: su.OrgID, UserID: su.UserID, Role: st.DefaultRole}); err != nil {
				return err
			}
		}
		return s.syncRoles(ctx, tx, su.OrgID, su.UserID)
	}
	if !exists {
		return nil
	}
	if m.Role == store.RoleOwner {
		n, err := tx.CountOwners(ctx, su.OrgID)
		if err != nil {
			return err
		}
		if n <= 1 {
			return scimErr(http.StatusConflict, "mutability", "cannot deactivate the organization's last owner")
		}
	}
	if err := tx.DeleteMembership(ctx, su.OrgID, su.UserID); err != nil {
		return err
	}
	return tx.DeleteUserSessions(ctx, su.UserID)
}

func (s *Service) saveUser(w http.ResponseWriter, r *http.Request, su store.SCIMUser, action string) error {
	ctx := r.Context()
	err := s.h.Host.Store().WithTx(ctx, func(tx store.Store) error {
		if err := tx.UpsertSCIMUser(ctx, &su); err != nil {
			return err
		}
		return s.applyActive(ctx, tx, su)
	})
	if err != nil {
		return err
	}
	s.audit(r, action, "user:"+su.UserID.String(), map[string]any{"userName": su.UserName, "active": su.Active})
	j, err := s.userJSON(ctx, su)
	if err != nil {
		return err
	}
	return writeSCIM(w, http.StatusOK, j)
}

func (s *Service) replaceUser(w http.ResponseWriter, r *http.Request) error {
	su, err := s.userParam(r)
	if err != nil {
		return err
	}
	var in userIn
	if err := decode(r, &in); err != nil {
		return err
	}
	if strings.TrimSpace(in.UserName) == "" {
		return scimErr(http.StatusBadRequest, "invalidValue", "userName is required")
	}
	if err := s.checkUserName(r, su, in.UserName); err != nil {
		return err
	}
	su.UserName, su.ExternalID, su.Data = strings.TrimSpace(in.UserName), in.ExternalID, storedData(in)
	su.Active = in.Active == nil || *in.Active
	return s.saveUser(w, r, su, "scim.user.replace")
}

func (s *Service) checkUserName(r *http.Request, su store.SCIMUser, name string) error {
	if strings.EqualFold(strings.TrimSpace(name), su.UserName) {
		return nil
	}
	us, _, err := s.h.Host.Store().ListSCIMUsers(r.Context(), store.SCIMUserFilter{OrgID: su.OrgID, UserName: name, Limit: 1})
	if err != nil {
		return err
	}
	if len(us) > 0 && us[0].UserID != su.UserID {
		return scimErr(http.StatusConflict, "uniqueness", "userName %q already exists", name)
	}
	return nil
}

type patchOp struct {
	Op    string          `json:"op"`
	Path  string          `json:"path"`
	Value json.RawMessage `json:"value"`
}

type patchBody struct {
	Schemas    []string  `json:"schemas"`
	Operations []patchOp `json:"Operations"`
}

func boolValue(raw json.RawMessage) (bool, error) {
	var b bool
	if err := json.Unmarshal(raw, &b); err == nil {
		return b, nil
	}
	var s string // Azure AD sends "False"/"True"
	if err := json.Unmarshal(raw, &s); err == nil {
		switch strings.ToLower(s) {
		case "true":
			return true, nil
		case "false":
			return false, nil
		}
	}
	return false, scimErr(http.StatusBadRequest, "invalidValue", "active must be a boolean")
}

func (s *Service) patchUser(w http.ResponseWriter, r *http.Request) error {
	su, err := s.userParam(r)
	if err != nil {
		return err
	}
	var in patchBody
	if err := decode(r, &in); err != nil {
		return err
	}
	var data stored
	_ = json.Unmarshal(su.Data, &data)
	str := func(raw json.RawMessage) (string, error) {
		var v string
		if err := json.Unmarshal(raw, &v); err != nil {
			return "", scimErr(http.StatusBadRequest, "invalidValue", "expected a string value")
		}
		return v, nil
	}
	apply := func(path string, raw json.RawMessage, remove bool) error {
		var err error
		switch strings.ToLower(path) {
		case "active":
			if remove {
				return scimErr(http.StatusBadRequest, "mutability", "active cannot be removed")
			}
			su.Active, err = boolValue(raw)
		case "username":
			var v string
			if v, err = str(raw); err == nil {
				if err = s.checkUserName(r, su, v); err == nil {
					su.UserName = strings.TrimSpace(v)
				}
			}
		case "externalid":
			if remove {
				su.ExternalID = ""
			} else {
				su.ExternalID, err = str(raw)
			}
		case "displayname":
			if remove {
				data.DisplayName = ""
			} else {
				data.DisplayName, err = str(raw)
			}
		case "name.givenname":
			data.Name.GivenName, err = str(raw)
		case "name.familyname":
			data.Name.FamilyName, err = str(raw)
		case "name.formatted":
			data.Name.Formatted, err = str(raw)
		case "name":
			err = json.Unmarshal(raw, &data.Name)
		default:
			// Unknown attributes (emails, title, enterprise extension...) are accepted and ignored.
		}
		return err
	}
	for _, op := range in.Operations {
		kind := strings.ToLower(op.Op)
		if kind != "add" && kind != "replace" && kind != "remove" {
			return scimErr(http.StatusBadRequest, "invalidSyntax", "unsupported patch op %q", op.Op)
		}
		if op.Path == "" {
			var obj map[string]json.RawMessage
			if err := json.Unmarshal(op.Value, &obj); err != nil {
				return scimErr(http.StatusBadRequest, "invalidValue", "a patch without path needs an object value")
			}
			for k, v := range obj {
				if err := apply(k, v, false); err != nil {
					return err
				}
			}
			continue
		}
		if err := apply(op.Path, op.Value, kind == "remove"); err != nil {
			return err
		}
	}
	su.Data, _ = json.Marshal(data)
	return s.saveUser(w, r, su, "scim.user.patch")
}

func (s *Service) deleteUser(w http.ResponseWriter, r *http.Request) error {
	su, err := s.userParam(r)
	if err != nil {
		return err
	}
	ctx := r.Context()
	err = s.h.Host.Store().WithTx(ctx, func(tx store.Store) error {
		su.Active = false
		if err := s.applyActive(ctx, tx, su); err != nil {
			return err
		}
		groups, err := tx.ListUserSCIMGroups(ctx, su.OrgID, su.UserID)
		if err != nil {
			return err
		}
		for _, g := range groups {
			full, err := tx.GetSCIMGroup(ctx, su.OrgID, g.ID)
			if err != nil {
				return err
			}
			full.Members = removeID(full.Members, su.UserID)
			if err := tx.UpdateSCIMGroup(ctx, &full); err != nil {
				return err
			}
		}
		return tx.DeleteSCIMUser(ctx, su.OrgID, su.UserID)
	})
	if err != nil {
		return err
	}
	s.audit(r, "scim.user.delete", "user:"+su.UserID.String(), map[string]any{"userName": su.UserName})
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func removeID(ids []uuid.UUID, id uuid.UUID) []uuid.UUID {
	out := ids[:0:0]
	for _, x := range ids {
		if x != id {
			out = append(out, x)
		}
	}
	return out
}
