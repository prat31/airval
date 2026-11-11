package rbac

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/prat31/airval/internal/auth"
	"github.com/prat31/airval/internal/store"
)

type fakeOrgs struct {
	store.OrgStore
	roles map[[2]uuid.UUID]store.Role
}

func (f fakeOrgs) GetMembership(_ context.Context, org, user uuid.UUID) (store.Membership, error) {
	r, ok := f.roles[[2]uuid.UUID{org, user}]
	if !ok {
		return store.Membership{}, store.ErrNotFound
	}
	return store.Membership{OrgID: org, UserID: user, Role: r}, nil
}

func TestRequire(t *testing.T) {
	org, other, user, proj, proj2 := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	a := &Authorizer{Store: fakeOrgs{roles: map[[2]uuid.UUID]store.Role{{org, user}: store.RoleEditor}}}
	u := &auth.Principal{User: &store.User{ID: user}}
	orgKey := &auth.Principal{APIKey: &store.APIKey{OrgID: org, Role: store.RoleViewer}}
	projKey := &auth.Principal{APIKey: &store.APIKey{OrgID: org, ProjectID: &proj, Role: store.RoleAdmin}}
	tests := []struct {
		name    string
		p       *auth.Principal
		org     uuid.UUID
		project *uuid.UUID
		min     store.Role
		want    error
	}{
		{"anonymous", nil, org, nil, store.RoleViewer, ErrUnauthenticated},
		{"editor can edit", u, org, &proj, store.RoleEditor, nil},
		{"editor cannot admin", u, org, nil, store.RoleAdmin, ErrForbidden},
		{"non-member", u, other, nil, store.RoleViewer, ErrNoAccess},
		{"viewer key reads", orgKey, org, &proj, store.RoleViewer, nil},
		{"viewer key cannot edit", orgKey, org, &proj, store.RoleEditor, ErrForbidden},
		{"key other org", orgKey, other, nil, store.RoleViewer, ErrNoAccess},
		{"project key own project", projKey, org, &proj, store.RoleAdmin, nil},
		{"project key other project", projKey, org, &proj2, store.RoleViewer, ErrNoAccess},
		{"project key org-level", projKey, org, nil, store.RoleViewer, ErrNoAccess},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := a.Require(context.Background(), tc.p, tc.org, tc.project, tc.min)
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v want %v", err, tc.want)
			}
		})
	}
}
