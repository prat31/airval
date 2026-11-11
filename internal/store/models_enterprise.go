package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Enterprise-edition persistence (used only by ee/, build tag `ee`). The tables live in the core migrations
// (prefixed ee_) so one schema serves both editions; see migrations/*/00003_enterprise.sql.

// SSOProvider is an OIDC or SAML identity provider configured for an organization.
type SSOProvider struct {
	ID         uuid.UUID
	OrgID      uuid.UUID
	Slug       string // global; used in login URLs
	Kind       string // "oidc" | "saml"
	Name       string
	Enabled    bool
	Config     json.RawMessage
	Secret     []byte // sealed with the secrets keyring (nil = none)
	KeyVersion int
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// SSOIdentity links an IdP subject to a user.
type SSOIdentity struct {
	ProviderID  uuid.UUID
	Subject     string
	UserID      uuid.UUID
	Email       string
	LastLoginAt time.Time
	CreatedAt   time.Time
}

// CustomRole is a named permission set.
type CustomRole struct {
	ID          uuid.UUID
	OrgID       uuid.UUID
	Key         string
	Name        string
	Description string
	Permissions []string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// RoleAssignment gives a member a built-in Role or a CustomRoleID, org-wide (ProjectID = NilID) or per project.
type RoleAssignment struct {
	OrgID        uuid.UUID
	UserID       uuid.UUID
	ProjectID    uuid.UUID // NilID = org-wide
	Role         Role      // built-in role, or "" when CustomRoleID is set
	CustomRoleID *uuid.UUID
	Source       string // manual | scim | sso
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// RoleAssignmentFilter narrows ListRoleAssignments.
type RoleAssignmentFilter struct {
	OrgID     uuid.UUID
	UserID    *uuid.UUID
	ProjectID *uuid.UUID
}

type SCIMToken struct {
	ID         uuid.UUID
	OrgID      uuid.UUID
	Name       string
	Prefix     string
	Hash       string
	CreatedBy  *uuid.UUID
	LastUsedAt *time.Time
	CreatedAt  time.Time
}

// SCIMUser is the SCIM view of a provisioned member.
type SCIMUser struct {
	OrgID      uuid.UUID
	UserID     uuid.UUID
	ExternalID string
	UserName   string
	Active     bool
	Data       json.RawMessage // name, emails... as last provisioned
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

type SCIMUserFilter struct {
	OrgID      uuid.UUID
	UserName   string // exact, case-insensitive
	ExternalID string
	Offset     int
	Limit      int
}

type SCIMGroup struct {
	ID          uuid.UUID
	OrgID       uuid.UUID
	DisplayName string
	ExternalID  string
	Members     []uuid.UUID
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type SCIMGroupFilter struct {
	OrgID       uuid.UUID
	DisplayName string
	ExternalID  string
	Offset      int
	Limit       int
}

// RetentionPolicy deletes a project's runs older than RunDays.
type RetentionPolicy struct {
	ProjectID     uuid.UUID
	OrgID         uuid.UUID
	Enabled       bool
	RunDays       int
	KeepBaselines bool
	LastAppliedAt *time.Time
	LastDeleted   int
	TotalDeleted  int64
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// EnterpriseStore persists enterprise-edition data.
type EnterpriseStore interface {
	CreateSSOProvider(ctx context.Context, p *SSOProvider) error
	GetSSOProvider(ctx context.Context, id uuid.UUID) (SSOProvider, error)
	GetSSOProviderBySlug(ctx context.Context, slug string) (SSOProvider, error)
	// ListSSOProviders lists an org's providers; uuid.Nil lists every org's (login page).
	ListSSOProviders(ctx context.Context, orgID uuid.UUID) ([]SSOProvider, error)
	UpdateSSOProvider(ctx context.Context, p *SSOProvider) error
	DeleteSSOProvider(ctx context.Context, id uuid.UUID) error
	UpsertSSOIdentity(ctx context.Context, i *SSOIdentity) error
	GetSSOIdentity(ctx context.Context, providerID uuid.UUID, subject string) (SSOIdentity, error)

	CreateCustomRole(ctx context.Context, r *CustomRole) error
	GetCustomRole(ctx context.Context, id uuid.UUID) (CustomRole, error)
	ListCustomRoles(ctx context.Context, orgID uuid.UUID) ([]CustomRole, error)
	UpdateCustomRole(ctx context.Context, r *CustomRole) error
	DeleteCustomRole(ctx context.Context, id uuid.UUID) error

	SetRoleAssignment(ctx context.Context, a *RoleAssignment) error
	DeleteRoleAssignment(ctx context.Context, orgID, userID, projectID uuid.UUID) error
	// ListRoleAssignments returns assignments ordered by user, project.
	ListRoleAssignments(ctx context.Context, f RoleAssignmentFilter) ([]RoleAssignment, error)

	CreateSCIMToken(ctx context.Context, t *SCIMToken) error
	GetSCIMTokenByHash(ctx context.Context, hash string) (SCIMToken, error)
	ListSCIMTokens(ctx context.Context, orgID uuid.UUID) ([]SCIMToken, error)
	DeleteSCIMToken(ctx context.Context, orgID, id uuid.UUID) error
	TouchSCIMToken(ctx context.Context, id uuid.UUID, at time.Time) error

	UpsertSCIMUser(ctx context.Context, u *SCIMUser) error
	GetSCIMUser(ctx context.Context, orgID, userID uuid.UUID) (SCIMUser, error)
	// ListSCIMUsers returns one page and the total match count.
	ListSCIMUsers(ctx context.Context, f SCIMUserFilter) ([]SCIMUser, int, error)
	DeleteSCIMUser(ctx context.Context, orgID, userID uuid.UUID) error

	CreateSCIMGroup(ctx context.Context, g *SCIMGroup) error
	GetSCIMGroup(ctx context.Context, orgID, id uuid.UUID) (SCIMGroup, error)
	ListSCIMGroups(ctx context.Context, f SCIMGroupFilter) ([]SCIMGroup, int, error)
	// UpdateSCIMGroup saves name/external id and replaces the member list.
	UpdateSCIMGroup(ctx context.Context, g *SCIMGroup) error
	DeleteSCIMGroup(ctx context.Context, orgID, id uuid.UUID) error
	// ListUserSCIMGroups returns the org's groups that contain userID.
	ListUserSCIMGroups(ctx context.Context, orgID, userID uuid.UUID) ([]SCIMGroup, error)

	// GetOrgSetting returns ErrNotFound when unset.
	GetOrgSetting(ctx context.Context, orgID uuid.UUID, key string) (json.RawMessage, error)
	PutOrgSetting(ctx context.Context, orgID uuid.UUID, key string, value json.RawMessage) error
	// ListOrgSettings returns key's value for every org that has it.
	ListOrgSettings(ctx context.Context, key string) (map[uuid.UUID]json.RawMessage, error)

	UpsertRetentionPolicy(ctx context.Context, p *RetentionPolicy) error
	GetRetentionPolicy(ctx context.Context, projectID uuid.UUID) (RetentionPolicy, error)
	// ListRetentionPolicies lists an org's policies; uuid.Nil lists all.
	ListRetentionPolicies(ctx context.Context, orgID uuid.UUID) ([]RetentionPolicy, error)
	DeleteRetentionPolicy(ctx context.Context, projectID uuid.UUID) error
	RecordRetentionApplied(ctx context.Context, projectID uuid.UUID, at time.Time, deleted int) error
	// ListExpiredRuns returns ids of finished runs of projectID created before `before` (oldest first),
	// excluding runs pinned as a suite baseline when keepBaselines.
	ListExpiredRuns(ctx context.Context, projectID uuid.UUID, before time.Time, keepBaselines bool, limit int) ([]uuid.UUID, error)
	// DeleteRuns deletes runs (and, by cascade, their results and scores) by id.
	DeleteRuns(ctx context.Context, ids []uuid.UUID) (int, error)
	// DeleteAuditBefore removes an org's audit entries older than before.
	DeleteAuditBefore(ctx context.Context, orgID uuid.UUID, before time.Time) (int, error)
}
