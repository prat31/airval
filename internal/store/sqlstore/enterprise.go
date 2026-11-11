package sqlstore

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/prat31/airval/internal/store"
)

// Enterprise-edition queries (tables prefixed ee_, see migrations/*/00003_enterprise.sql).

// ---- SSO providers ----

const ssoCols = "id, org_id, slug, kind, name, enabled, config, secret, key_version, created_at, updated_at"

func scanSSO(r scanner) (store.SSOProvider, error) {
	var p store.SSOProvider
	err := r.Scan(&p.ID, &p.OrgID, &p.Slug, &p.Kind, &p.Name, &p.Enabled, rawJSON{&p.Config}, &p.Secret, &p.KeyVersion,
		&p.CreatedAt, &p.UpdatedAt)
	return p, err
}

func blobArg(b []byte) any {
	if len(b) == 0 {
		return nil
	}
	return b
}

func (s *Store) CreateSSOProvider(ctx context.Context, p *store.SSOProvider) error {
	if p.ID == uuid.Nil {
		p.ID = store.NewID()
	}
	stampTimes(&p.CreatedAt, &p.UpdatedAt)
	return s.exec(ctx, `INSERT INTO ee_sso_providers (`+ssoCols+`) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		p.ID, p.OrgID, p.Slug, p.Kind, p.Name, p.Enabled, jsonArg(p.Config, "{}"), blobArg(p.Secret), p.KeyVersion,
		p.CreatedAt, p.UpdatedAt)
}

func (s *Store) GetSSOProvider(ctx context.Context, id uuid.UUID) (store.SSOProvider, error) {
	return one(ctx, s, `SELECT `+ssoCols+` FROM ee_sso_providers WHERE id = $1`, []any{id}, scanSSO)
}

func (s *Store) GetSSOProviderBySlug(ctx context.Context, slug string) (store.SSOProvider, error) {
	return one(ctx, s, `SELECT `+ssoCols+` FROM ee_sso_providers WHERE slug = $1`, []any{slug}, scanSSO)
}

func (s *Store) ListSSOProviders(ctx context.Context, orgID uuid.UUID) ([]store.SSOProvider, error) {
	q, args := `SELECT `+ssoCols+` FROM ee_sso_providers`, []any{}
	if orgID != uuid.Nil {
		q, args = q+` WHERE org_id = $1`, append(args, orgID)
	}
	out, err := collect(ctx, s, q+` ORDER BY name, id`, args, scanSSO)
	if out == nil {
		out = []store.SSOProvider{}
	}
	return out, err
}

func (s *Store) UpdateSSOProvider(ctx context.Context, p *store.SSOProvider) error {
	stampTimes(nil, &p.UpdatedAt)
	return s.execAffect(ctx, `UPDATE ee_sso_providers SET slug = $2, name = $3, enabled = $4, config = $5, secret = $6,
		key_version = $7, updated_at = $8 WHERE id = $1`,
		p.ID, p.Slug, p.Name, p.Enabled, jsonArg(p.Config, "{}"), blobArg(p.Secret), p.KeyVersion, p.UpdatedAt)
}

func (s *Store) DeleteSSOProvider(ctx context.Context, id uuid.UUID) error {
	return s.execAffect(ctx, `DELETE FROM ee_sso_providers WHERE id = $1`, id)
}

func (s *Store) UpsertSSOIdentity(ctx context.Context, i *store.SSOIdentity) error {
	stampTimes(&i.CreatedAt, &i.LastLoginAt)
	return s.exec(ctx, `INSERT INTO ee_sso_identities (provider_id, subject, user_id, email, last_login_at, created_at)
		VALUES ($1,$2,$3,$4,$5,$6)
		ON CONFLICT (provider_id, subject) DO UPDATE SET email = excluded.email, last_login_at = excluded.last_login_at`,
		i.ProviderID, i.Subject, i.UserID, i.Email, i.LastLoginAt, i.CreatedAt)
}

func (s *Store) GetSSOIdentity(ctx context.Context, providerID uuid.UUID, subject string) (store.SSOIdentity, error) {
	return one(ctx, s, `SELECT provider_id, subject, user_id, email, last_login_at, created_at FROM ee_sso_identities
		WHERE provider_id = $1 AND subject = $2`, []any{providerID, subject}, func(r scanner) (store.SSOIdentity, error) {
		var i store.SSOIdentity
		err := r.Scan(&i.ProviderID, &i.Subject, &i.UserID, &i.Email, &i.LastLoginAt, &i.CreatedAt)
		return i, err
	})
}

// ---- custom roles & assignments ----

const roleCols = "id, org_id, key, name, description, permissions, created_at, updated_at"

func scanCustomRole(r scanner) (store.CustomRole, error) {
	var c store.CustomRole
	err := r.Scan(&c.ID, &c.OrgID, &c.Key, &c.Name, &c.Description, jsonCol(&c.Permissions), &c.CreatedAt, &c.UpdatedAt)
	return c, err
}

func (s *Store) CreateCustomRole(ctx context.Context, r *store.CustomRole) error {
	if r.ID == uuid.Nil {
		r.ID = store.NewID()
	}
	stampTimes(&r.CreatedAt, &r.UpdatedAt)
	return s.exec(ctx, `INSERT INTO ee_custom_roles (`+roleCols+`) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		r.ID, r.OrgID, r.Key, r.Name, r.Description, jsonValue(strs(r.Permissions)), r.CreatedAt, r.UpdatedAt)
}

func (s *Store) GetCustomRole(ctx context.Context, id uuid.UUID) (store.CustomRole, error) {
	return one(ctx, s, `SELECT `+roleCols+` FROM ee_custom_roles WHERE id = $1`, []any{id}, scanCustomRole)
}

func (s *Store) ListCustomRoles(ctx context.Context, orgID uuid.UUID) ([]store.CustomRole, error) {
	out, err := collect(ctx, s, `SELECT `+roleCols+` FROM ee_custom_roles WHERE org_id = $1 ORDER BY name, id`,
		[]any{orgID}, scanCustomRole)
	if out == nil {
		out = []store.CustomRole{}
	}
	return out, err
}

func (s *Store) UpdateCustomRole(ctx context.Context, r *store.CustomRole) error {
	stampTimes(nil, &r.UpdatedAt)
	return s.execAffect(ctx, `UPDATE ee_custom_roles SET key = $2, name = $3, description = $4, permissions = $5,
		updated_at = $6 WHERE id = $1`, r.ID, r.Key, r.Name, r.Description, jsonValue(strs(r.Permissions)), r.UpdatedAt)
}

func (s *Store) DeleteCustomRole(ctx context.Context, id uuid.UUID) error {
	return s.execAffect(ctx, `DELETE FROM ee_custom_roles WHERE id = $1`, id)
}

func (s *Store) SetRoleAssignment(ctx context.Context, a *store.RoleAssignment) error {
	stampTimes(&a.CreatedAt, &a.UpdatedAt)
	if a.Source == "" {
		a.Source = "manual"
	}
	return s.exec(ctx, `INSERT INTO ee_role_assignments (org_id, user_id, project_id, role, custom_role_id, source, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		ON CONFLICT (org_id, user_id, project_id) DO UPDATE SET role = excluded.role, custom_role_id = excluded.custom_role_id,
			source = excluded.source, updated_at = excluded.updated_at`,
		a.OrgID, a.UserID, a.ProjectID, string(a.Role), a.CustomRoleID, a.Source, a.CreatedAt, a.UpdatedAt)
}

func (s *Store) DeleteRoleAssignment(ctx context.Context, orgID, userID, projectID uuid.UUID) error {
	return s.execAffect(ctx, `DELETE FROM ee_role_assignments WHERE org_id = $1 AND user_id = $2 AND project_id = $3`,
		orgID, userID, projectID)
}

func (s *Store) ListRoleAssignments(ctx context.Context, f store.RoleAssignmentFilter) ([]store.RoleAssignment, error) {
	b := &qb{}
	b.where("org_id = " + b.arg(f.OrgID))
	if f.UserID != nil {
		b.where("user_id = " + b.arg(*f.UserID))
	}
	if f.ProjectID != nil {
		b.where("project_id = " + b.arg(*f.ProjectID))
	}
	out, err := collect(ctx, s, `SELECT org_id, user_id, project_id, role, custom_role_id, source, created_at, updated_at
		FROM ee_role_assignments`+b.whereSQL()+` ORDER BY user_id, project_id`, b.args,
		func(r scanner) (store.RoleAssignment, error) {
			var a store.RoleAssignment
			err := r.Scan(&a.OrgID, &a.UserID, &a.ProjectID, &a.Role, &a.CustomRoleID, &a.Source, &a.CreatedAt, &a.UpdatedAt)
			return a, err
		})
	if out == nil {
		out = []store.RoleAssignment{}
	}
	return out, err
}

// ---- SCIM ----

const scimTokenCols = "id, org_id, name, prefix, hash, created_by, last_used_at, created_at"

func scanSCIMToken(r scanner) (store.SCIMToken, error) {
	var t store.SCIMToken
	err := r.Scan(&t.ID, &t.OrgID, &t.Name, &t.Prefix, &t.Hash, &t.CreatedBy, &t.LastUsedAt, &t.CreatedAt)
	return t, err
}

func (s *Store) CreateSCIMToken(ctx context.Context, t *store.SCIMToken) error {
	if t.ID == uuid.Nil {
		t.ID = store.NewID()
	}
	stampTimes(&t.CreatedAt, nil)
	return s.exec(ctx, `INSERT INTO ee_scim_tokens (`+scimTokenCols+`) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		t.ID, t.OrgID, t.Name, t.Prefix, t.Hash, t.CreatedBy, t.LastUsedAt, t.CreatedAt)
}

func (s *Store) GetSCIMTokenByHash(ctx context.Context, hash string) (store.SCIMToken, error) {
	return one(ctx, s, `SELECT `+scimTokenCols+` FROM ee_scim_tokens WHERE hash = $1`, []any{hash}, scanSCIMToken)
}

func (s *Store) ListSCIMTokens(ctx context.Context, orgID uuid.UUID) ([]store.SCIMToken, error) {
	out, err := collect(ctx, s, `SELECT `+scimTokenCols+` FROM ee_scim_tokens WHERE org_id = $1 ORDER BY id DESC`,
		[]any{orgID}, scanSCIMToken)
	if out == nil {
		out = []store.SCIMToken{}
	}
	return out, err
}

func (s *Store) DeleteSCIMToken(ctx context.Context, orgID, id uuid.UUID) error {
	return s.execAffect(ctx, `DELETE FROM ee_scim_tokens WHERE org_id = $1 AND id = $2`, orgID, id)
}

func (s *Store) TouchSCIMToken(ctx context.Context, id uuid.UUID, at time.Time) error {
	return s.exec(ctx, `UPDATE ee_scim_tokens SET last_used_at = $2 WHERE id = $1`, id, at.UTC())
}

const scimUserCols = "org_id, user_id, external_id, user_name, active, data, created_at, updated_at"

func scanSCIMUser(r scanner) (store.SCIMUser, error) {
	var u store.SCIMUser
	err := r.Scan(&u.OrgID, &u.UserID, &u.ExternalID, &u.UserName, &u.Active, rawJSON{&u.Data}, &u.CreatedAt, &u.UpdatedAt)
	return u, err
}

func (s *Store) UpsertSCIMUser(ctx context.Context, u *store.SCIMUser) error {
	stampTimes(&u.CreatedAt, &u.UpdatedAt)
	u.UserName = strings.ToLower(u.UserName)
	return s.exec(ctx, `INSERT INTO ee_scim_users (`+scimUserCols+`) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		ON CONFLICT (org_id, user_id) DO UPDATE SET external_id = excluded.external_id, user_name = excluded.user_name,
			active = excluded.active, data = excluded.data, updated_at = excluded.updated_at`,
		u.OrgID, u.UserID, u.ExternalID, u.UserName, u.Active, jsonArg(u.Data, "{}"), u.CreatedAt, u.UpdatedAt)
}

func (s *Store) GetSCIMUser(ctx context.Context, orgID, userID uuid.UUID) (store.SCIMUser, error) {
	return one(ctx, s, `SELECT `+scimUserCols+` FROM ee_scim_users WHERE org_id = $1 AND user_id = $2`,
		[]any{orgID, userID}, scanSCIMUser)
}

func scimPage(offset, limit int) (int, int) {
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	return offset, limit
}

func (s *Store) ListSCIMUsers(ctx context.Context, f store.SCIMUserFilter) ([]store.SCIMUser, int, error) {
	b := &qb{}
	b.where("org_id = " + b.arg(f.OrgID))
	if f.UserName != "" {
		b.where("user_name = " + b.arg(strings.ToLower(f.UserName)))
	}
	if f.ExternalID != "" {
		b.where("external_id = " + b.arg(f.ExternalID))
	}
	var total int
	if err := s.q.QueryRowContext(ctx, `SELECT count(*) FROM ee_scim_users`+b.whereSQL(), b.args...).Scan(&total); err != nil {
		return nil, 0, s.mapErr(err)
	}
	off, lim := scimPage(f.Offset, f.Limit)
	out, err := collect(ctx, s, `SELECT `+scimUserCols+` FROM ee_scim_users`+b.whereSQL()+
		` ORDER BY created_at, user_id LIMIT `+strconv.Itoa(lim)+` OFFSET `+strconv.Itoa(off), b.args, scanSCIMUser)
	if out == nil {
		out = []store.SCIMUser{}
	}
	return out, total, err
}

func (s *Store) DeleteSCIMUser(ctx context.Context, orgID, userID uuid.UUID) error {
	return s.execAffect(ctx, `DELETE FROM ee_scim_users WHERE org_id = $1 AND user_id = $2`, orgID, userID)
}

const scimGroupCols = "id, org_id, display_name, external_id, created_at, updated_at"

func scanSCIMGroup(r scanner) (store.SCIMGroup, error) {
	var g store.SCIMGroup
	err := r.Scan(&g.ID, &g.OrgID, &g.DisplayName, &g.ExternalID, &g.CreatedAt, &g.UpdatedAt)
	return g, err
}

func (s *Store) setGroupMembers(ctx context.Context, groupID uuid.UUID, members []uuid.UUID) error {
	if err := s.exec(ctx, `DELETE FROM ee_scim_group_members WHERE group_id = $1`, groupID); err != nil {
		return err
	}
	seen := map[uuid.UUID]bool{}
	var rows [][]any
	for _, m := range members {
		if !seen[m] {
			seen[m] = true
			rows = append(rows, []any{groupID, m})
		}
	}
	for start := 0; start < len(rows); start += 400 {
		chunk := rows[start:min(start+400, len(rows))]
		var sb strings.Builder
		b := &qb{}
		for i, r := range chunk {
			if i > 0 {
				sb.WriteString(",")
			}
			sb.WriteString("(" + b.arg(r[0]) + "," + b.arg(r[1]) + ")")
		}
		if err := s.exec(ctx, `INSERT INTO ee_scim_group_members (group_id, user_id) VALUES `+sb.String(), b.args...); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) loadGroupMembers(ctx context.Context, groups []store.SCIMGroup) error {
	if len(groups) == 0 {
		return nil
	}
	idx := map[uuid.UUID]int{}
	ids := make([]uuid.UUID, len(groups))
	for i, g := range groups {
		idx[g.ID], ids[i] = i, g.ID
		groups[i].Members = []uuid.UUID{}
	}
	b := &qb{}
	type pair struct{ g, u uuid.UUID }
	ps, err := collect(ctx, s, `SELECT group_id, user_id FROM ee_scim_group_members WHERE `+inList(b, "group_id", ids)+
		` ORDER BY group_id, user_id`, b.args, func(r scanner) (pair, error) {
		var p pair
		err := r.Scan(&p.g, &p.u)
		return p, err
	})
	if err != nil {
		return err
	}
	for _, p := range ps {
		g := &groups[idx[p.g]]
		g.Members = append(g.Members, p.u)
	}
	return nil
}

func (s *Store) CreateSCIMGroup(ctx context.Context, g *store.SCIMGroup) error {
	if g.ID == uuid.Nil {
		g.ID = store.NewID()
	}
	stampTimes(&g.CreatedAt, &g.UpdatedAt)
	return s.withTx(ctx, func(tx *Store) error {
		if err := tx.exec(ctx, `INSERT INTO ee_scim_groups (`+scimGroupCols+`) VALUES ($1,$2,$3,$4,$5,$6)`,
			g.ID, g.OrgID, g.DisplayName, g.ExternalID, g.CreatedAt, g.UpdatedAt); err != nil {
			return err
		}
		return tx.setGroupMembers(ctx, g.ID, g.Members)
	})
}

func (s *Store) GetSCIMGroup(ctx context.Context, orgID, id uuid.UUID) (store.SCIMGroup, error) {
	g, err := one(ctx, s, `SELECT `+scimGroupCols+` FROM ee_scim_groups WHERE org_id = $1 AND id = $2`,
		[]any{orgID, id}, scanSCIMGroup)
	if err != nil {
		return g, err
	}
	gs := []store.SCIMGroup{g}
	err = s.loadGroupMembers(ctx, gs)
	return gs[0], err
}

func (s *Store) ListSCIMGroups(ctx context.Context, f store.SCIMGroupFilter) ([]store.SCIMGroup, int, error) {
	b := &qb{}
	b.where("org_id = " + b.arg(f.OrgID))
	if f.DisplayName != "" {
		b.where("lower(display_name) = " + b.arg(strings.ToLower(f.DisplayName)))
	}
	if f.ExternalID != "" {
		b.where("external_id = " + b.arg(f.ExternalID))
	}
	var total int
	if err := s.q.QueryRowContext(ctx, `SELECT count(*) FROM ee_scim_groups`+b.whereSQL(), b.args...).Scan(&total); err != nil {
		return nil, 0, s.mapErr(err)
	}
	off, lim := scimPage(f.Offset, f.Limit)
	out, err := collect(ctx, s, `SELECT `+scimGroupCols+` FROM ee_scim_groups`+b.whereSQL()+
		` ORDER BY created_at, id LIMIT `+strconv.Itoa(lim)+` OFFSET `+strconv.Itoa(off), b.args, scanSCIMGroup)
	if err != nil {
		return nil, 0, err
	}
	if out == nil {
		out = []store.SCIMGroup{}
	}
	return out, total, s.loadGroupMembers(ctx, out)
}

func (s *Store) UpdateSCIMGroup(ctx context.Context, g *store.SCIMGroup) error {
	stampTimes(nil, &g.UpdatedAt)
	return s.withTx(ctx, func(tx *Store) error {
		if err := tx.execAffect(ctx, `UPDATE ee_scim_groups SET display_name = $3, external_id = $4, updated_at = $5
			WHERE org_id = $1 AND id = $2`, g.OrgID, g.ID, g.DisplayName, g.ExternalID, g.UpdatedAt); err != nil {
			return err
		}
		return tx.setGroupMembers(ctx, g.ID, g.Members)
	})
}

func (s *Store) DeleteSCIMGroup(ctx context.Context, orgID, id uuid.UUID) error {
	return s.execAffect(ctx, `DELETE FROM ee_scim_groups WHERE org_id = $1 AND id = $2`, orgID, id)
}

func (s *Store) ListUserSCIMGroups(ctx context.Context, orgID, userID uuid.UUID) ([]store.SCIMGroup, error) {
	out, err := collect(ctx, s, `SELECT g.id, g.org_id, g.display_name, g.external_id, g.created_at, g.updated_at
		FROM ee_scim_groups g JOIN ee_scim_group_members m ON m.group_id = g.id
		WHERE g.org_id = $1 AND m.user_id = $2 ORDER BY g.display_name`, []any{orgID, userID}, scanSCIMGroup)
	if out == nil {
		out = []store.SCIMGroup{}
	}
	return out, err
}

// ---- org settings ----

func (s *Store) GetOrgSetting(ctx context.Context, orgID uuid.UUID, key string) (json.RawMessage, error) {
	return one(ctx, s, `SELECT value FROM ee_org_settings WHERE org_id = $1 AND key = $2`, []any{orgID, key},
		func(r scanner) (json.RawMessage, error) {
			var v json.RawMessage
			err := r.Scan(rawJSON{&v})
			return v, err
		})
}

func (s *Store) PutOrgSetting(ctx context.Context, orgID uuid.UUID, key string, value json.RawMessage) error {
	return s.exec(ctx, `INSERT INTO ee_org_settings (org_id, key, value, updated_at) VALUES ($1,$2,$3,$4)
		ON CONFLICT (org_id, key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		orgID, key, jsonArg(value, "{}"), now())
}

func (s *Store) ListOrgSettings(ctx context.Context, key string) (map[uuid.UUID]json.RawMessage, error) {
	type kv struct {
		id uuid.UUID
		v  json.RawMessage
	}
	rows, err := collect(ctx, s, `SELECT org_id, value FROM ee_org_settings WHERE key = $1`, []any{key},
		func(r scanner) (kv, error) {
			var x kv
			err := r.Scan(&x.id, rawJSON{&x.v})
			return x, err
		})
	out := make(map[uuid.UUID]json.RawMessage, len(rows))
	for _, x := range rows {
		out[x.id] = x.v
	}
	return out, err
}

// ---- retention ----

const retentionCols = "project_id, org_id, enabled, run_days, keep_baselines, last_applied_at, last_deleted, total_deleted, created_at, updated_at"

func scanRetention(r scanner) (store.RetentionPolicy, error) {
	var p store.RetentionPolicy
	err := r.Scan(&p.ProjectID, &p.OrgID, &p.Enabled, &p.RunDays, &p.KeepBaselines, &p.LastAppliedAt, &p.LastDeleted,
		&p.TotalDeleted, &p.CreatedAt, &p.UpdatedAt)
	return p, err
}

func (s *Store) UpsertRetentionPolicy(ctx context.Context, p *store.RetentionPolicy) error {
	stampTimes(&p.CreatedAt, &p.UpdatedAt)
	return s.exec(ctx, `INSERT INTO ee_retention_policies (`+retentionCols+`) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		ON CONFLICT (project_id) DO UPDATE SET enabled = excluded.enabled, run_days = excluded.run_days,
			keep_baselines = excluded.keep_baselines, updated_at = excluded.updated_at`,
		p.ProjectID, p.OrgID, p.Enabled, p.RunDays, p.KeepBaselines, p.LastAppliedAt, p.LastDeleted, p.TotalDeleted,
		p.CreatedAt, p.UpdatedAt)
}

func (s *Store) GetRetentionPolicy(ctx context.Context, projectID uuid.UUID) (store.RetentionPolicy, error) {
	return one(ctx, s, `SELECT `+retentionCols+` FROM ee_retention_policies WHERE project_id = $1`, []any{projectID},
		scanRetention)
}

func (s *Store) ListRetentionPolicies(ctx context.Context, orgID uuid.UUID) ([]store.RetentionPolicy, error) {
	q, args := `SELECT `+retentionCols+` FROM ee_retention_policies`, []any{}
	if orgID != uuid.Nil {
		q, args = q+` WHERE org_id = $1`, append(args, orgID)
	}
	out, err := collect(ctx, s, q+` ORDER BY project_id`, args, scanRetention)
	if out == nil {
		out = []store.RetentionPolicy{}
	}
	return out, err
}

func (s *Store) DeleteRetentionPolicy(ctx context.Context, projectID uuid.UUID) error {
	return s.execAffect(ctx, `DELETE FROM ee_retention_policies WHERE project_id = $1`, projectID)
}

func (s *Store) RecordRetentionApplied(ctx context.Context, projectID uuid.UUID, at time.Time, deleted int) error {
	return s.exec(ctx, `UPDATE ee_retention_policies SET last_applied_at = $2, last_deleted = $3,
		total_deleted = total_deleted + $4 WHERE project_id = $1`, projectID, at.UTC(), deleted, int64(deleted))
}

func (s *Store) ListExpiredRuns(ctx context.Context, projectID uuid.UUID, before time.Time, keepBaselines bool, limit int) ([]uuid.UUID, error) {
	if limit <= 0 {
		limit = 500
	}
	q := `SELECT r.id FROM runs r WHERE r.project_id = $1 AND r.created_at < $2
		AND r.status IN ('succeeded', 'failed', 'errored', 'canceled')`
	if keepBaselines {
		q += ` AND NOT EXISTS (SELECT 1 FROM suites s WHERE s.project_id = r.project_id AND s.baseline_run_id = r.id)`
	}
	return collect(ctx, s, q+` ORDER BY r.created_at, r.id LIMIT `+strconv.Itoa(limit), []any{projectID, before.UTC()},
		func(r scanner) (uuid.UUID, error) {
			var id uuid.UUID
			err := r.Scan(&id)
			return id, err
		})
}

func (s *Store) DeleteRuns(ctx context.Context, ids []uuid.UUID) (int, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	total := 0
	err := s.withTx(ctx, func(tx *Store) error {
		for start := 0; start < len(ids); start += 500 {
			b := &qb{}
			res, err := tx.q.ExecContext(ctx, `DELETE FROM runs WHERE `+inList(b, "id", ids[start:min(start+500, len(ids))]), b.args...)
			if err != nil {
				return tx.mapErr(err)
			}
			n, _ := res.RowsAffected()
			total += int(n)
		}
		return nil
	})
	return total, err
}

func (s *Store) DeleteAuditBefore(ctx context.Context, orgID uuid.UUID, before time.Time) (int, error) {
	res, err := s.q.ExecContext(ctx, `DELETE FROM audit_log WHERE org_id = $1 AND at < $2`, orgID, before.UTC())
	if err != nil {
		return 0, s.mapErr(err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}
