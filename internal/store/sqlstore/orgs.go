package sqlstore

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/airval/airval/internal/store"
)

// ---- organizations ----

const orgCols = "id, name, slug, created_at, updated_at"

func scanOrg(r scanner) (store.Organization, error) {
	var o store.Organization
	err := r.Scan(&o.ID, &o.Name, &o.Slug, &o.CreatedAt, &o.UpdatedAt)
	return o, err
}

func (s *Store) CreateOrg(ctx context.Context, o *store.Organization) error {
	if o.ID == uuid.Nil {
		o.ID = store.NewID()
	}
	stampTimes(&o.CreatedAt, &o.UpdatedAt)
	return s.exec(ctx, `INSERT INTO organizations (`+orgCols+`) VALUES ($1,$2,$3,$4,$5)`,
		o.ID, o.Name, o.Slug, o.CreatedAt, o.UpdatedAt)
}

func (s *Store) GetOrg(ctx context.Context, id uuid.UUID) (store.Organization, error) {
	return one(ctx, s, `SELECT `+orgCols+` FROM organizations WHERE id = $1`, []any{id}, scanOrg)
}

func (s *Store) GetOrgBySlug(ctx context.Context, slug string) (store.Organization, error) {
	return one(ctx, s, `SELECT `+orgCols+` FROM organizations WHERE slug = $1`, []any{slug}, scanOrg)
}

func (s *Store) UpdateOrg(ctx context.Context, o *store.Organization) error {
	stampTimes(nil, &o.UpdatedAt)
	return s.execAffect(ctx, `UPDATE organizations SET name = $2, slug = $3, updated_at = $4 WHERE id = $1`,
		o.ID, o.Name, o.Slug, o.UpdatedAt)
}

func (s *Store) DeleteOrg(ctx context.Context, id uuid.UUID) error {
	return s.execAffect(ctx, `DELETE FROM organizations WHERE id = $1`, id)
}

func (s *Store) ListUserOrgs(ctx context.Context, userID uuid.UUID) ([]store.OrgMembership, error) {
	return collect(ctx, s, `SELECT o.id, o.name, o.slug, o.created_at, o.updated_at, m.role
		FROM organizations o JOIN memberships m ON m.org_id = o.id
		WHERE m.user_id = $1 ORDER BY o.name`, []any{userID},
		func(r scanner) (store.OrgMembership, error) {
			var om store.OrgMembership
			err := r.Scan(&om.ID, &om.Name, &om.Slug, &om.CreatedAt, &om.UpdatedAt, &om.Role)
			return om, err
		})
}

// ---- memberships ----

func (s *Store) UpsertMembership(ctx context.Context, m *store.Membership) error {
	stampTimes(&m.CreatedAt, &m.UpdatedAt)
	return s.exec(ctx, `INSERT INTO memberships (org_id, user_id, role, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5)
		ON CONFLICT (org_id, user_id) DO UPDATE SET role = excluded.role, updated_at = excluded.updated_at`,
		m.OrgID, m.UserID, m.Role, m.CreatedAt, m.UpdatedAt)
}

func (s *Store) GetMembership(ctx context.Context, orgID, userID uuid.UUID) (store.Membership, error) {
	return one(ctx, s, `SELECT org_id, user_id, role, created_at, updated_at FROM memberships
		WHERE org_id = $1 AND user_id = $2`, []any{orgID, userID},
		func(r scanner) (store.Membership, error) {
			var m store.Membership
			err := r.Scan(&m.OrgID, &m.UserID, &m.Role, &m.CreatedAt, &m.UpdatedAt)
			return m, err
		})
}

func (s *Store) ListMembers(ctx context.Context, orgID uuid.UUID, p store.ListParams) (store.Page[store.Member], error) {
	b := &qb{}
	b.where("m.org_id = " + b.arg(orgID))
	tail, limit, err := b.paginate("m.user_id", p, false)
	if err != nil {
		return store.Page[store.Member]{}, err
	}
	items, err := collect(ctx, s, `SELECT m.org_id, m.user_id, m.role, m.created_at, m.updated_at,
		u.id, u.email, u.name, u.password_hash, u.avatar_url, u.created_at, u.updated_at
		FROM memberships m JOIN users u ON u.id = m.user_id`+b.whereSQL()+tail, b.args,
		func(r scanner) (store.Member, error) {
			var m store.Member
			u := &m.User
			err := r.Scan(&m.OrgID, &m.UserID, &m.Role, &m.CreatedAt, &m.UpdatedAt,
				&u.ID, &u.Email, &u.Name, &u.PasswordHash, &u.AvatarURL, &u.CreatedAt, &u.UpdatedAt)
			return m, err
		})
	if err != nil {
		return store.Page[store.Member]{}, err
	}
	return finishPage(items, limit, func(m store.Member) uuid.UUID { return m.UserID }), nil
}

func (s *Store) DeleteMembership(ctx context.Context, orgID, userID uuid.UUID) error {
	return s.execAffect(ctx, `DELETE FROM memberships WHERE org_id = $1 AND user_id = $2`, orgID, userID)
}

func (s *Store) CountOwners(ctx context.Context, orgID uuid.UUID) (int, error) {
	var n int
	err := s.q.QueryRowContext(ctx, `SELECT count(*) FROM memberships WHERE org_id = $1 AND role = 'owner'`, orgID).Scan(&n)
	return n, s.mapErr(err)
}

// ---- users ----

const userCols = "id, email, name, password_hash, avatar_url, created_at, updated_at"

func scanUser(r scanner) (store.User, error) {
	var u store.User
	err := r.Scan(&u.ID, &u.Email, &u.Name, &u.PasswordHash, &u.AvatarURL, &u.CreatedAt, &u.UpdatedAt)
	return u, err
}

func (s *Store) CreateUser(ctx context.Context, u *store.User) error {
	if u.ID == uuid.Nil {
		u.ID = store.NewID()
	}
	u.Email = strings.ToLower(strings.TrimSpace(u.Email))
	stampTimes(&u.CreatedAt, &u.UpdatedAt)
	return s.exec(ctx, `INSERT INTO users (`+userCols+`) VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		u.ID, u.Email, u.Name, u.PasswordHash, u.AvatarURL, u.CreatedAt, u.UpdatedAt)
}

func (s *Store) GetUser(ctx context.Context, id uuid.UUID) (store.User, error) {
	return one(ctx, s, `SELECT `+userCols+` FROM users WHERE id = $1`, []any{id}, scanUser)
}

func (s *Store) GetUserByEmail(ctx context.Context, email string) (store.User, error) {
	return one(ctx, s, `SELECT `+userCols+` FROM users WHERE email = $1`,
		[]any{strings.ToLower(strings.TrimSpace(email))}, scanUser)
}

func (s *Store) UpdateUser(ctx context.Context, u *store.User) error {
	u.Email = strings.ToLower(strings.TrimSpace(u.Email))
	stampTimes(nil, &u.UpdatedAt)
	return s.execAffect(ctx, `UPDATE users SET email = $2, name = $3, password_hash = $4, avatar_url = $5,
		updated_at = $6 WHERE id = $1`, u.ID, u.Email, u.Name, u.PasswordHash, u.AvatarURL, u.UpdatedAt)
}

func (s *Store) CountUsers(ctx context.Context) (int, error) {
	var n int
	err := s.q.QueryRowContext(ctx, `SELECT count(*) FROM users`).Scan(&n)
	return n, s.mapErr(err)
}

// ---- sessions ----

const sessionCols = "id, user_id, token_hash, csrf_token, user_agent, ip, expires_at, last_seen_at, created_at"

func (s *Store) CreateSession(ctx context.Context, se *store.Session) error {
	if se.ID == uuid.Nil {
		se.ID = store.NewID()
	}
	stampTimes(&se.CreatedAt, &se.LastSeenAt)
	return s.exec(ctx, `INSERT INTO sessions (`+sessionCols+`) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		se.ID, se.UserID, se.TokenHash, se.CSRFToken, se.UserAgent, se.IP, se.ExpiresAt.UTC(), se.LastSeenAt, se.CreatedAt)
}

func (s *Store) GetSessionByTokenHash(ctx context.Context, hash string) (store.Session, error) {
	return one(ctx, s, `SELECT `+sessionCols+` FROM sessions WHERE token_hash = $1`, []any{hash},
		func(r scanner) (store.Session, error) {
			var se store.Session
			err := r.Scan(&se.ID, &se.UserID, &se.TokenHash, &se.CSRFToken, &se.UserAgent, &se.IP,
				&se.ExpiresAt, &se.LastSeenAt, &se.CreatedAt)
			return se, err
		})
}

func (s *Store) TouchSession(ctx context.Context, id uuid.UUID, seen, expires time.Time) error {
	return s.exec(ctx, `UPDATE sessions SET last_seen_at = $2, expires_at = $3 WHERE id = $1`, id, seen.UTC(), expires.UTC())
}

func (s *Store) DeleteSession(ctx context.Context, id uuid.UUID) error {
	return s.exec(ctx, `DELETE FROM sessions WHERE id = $1`, id)
}

func (s *Store) DeleteUserSessions(ctx context.Context, userID uuid.UUID) error {
	return s.exec(ctx, `DELETE FROM sessions WHERE user_id = $1`, userID)
}

func (s *Store) DeleteExpiredSessions(ctx context.Context, at time.Time) (int, error) {
	res, err := s.q.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at < $1`, at.UTC())
	if err != nil {
		return 0, s.mapErr(err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// ---- api keys ----

const apiKeyCols = "id, org_id, project_id, name, prefix, hash, role, created_by, last_used_at, expires_at, created_at"

func scanAPIKey(r scanner) (store.APIKey, error) {
	var k store.APIKey
	err := r.Scan(&k.ID, &k.OrgID, &k.ProjectID, &k.Name, &k.Prefix, &k.Hash, &k.Role, &k.CreatedBy,
		&k.LastUsedAt, &k.ExpiresAt, &k.CreatedAt)
	return k, err
}

func (s *Store) CreateAPIKey(ctx context.Context, k *store.APIKey) error {
	if k.ID == uuid.Nil {
		k.ID = store.NewID()
	}
	stampTimes(&k.CreatedAt, nil)
	return s.exec(ctx, `INSERT INTO api_keys (`+apiKeyCols+`) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		k.ID, k.OrgID, k.ProjectID, k.Name, k.Prefix, k.Hash, k.Role, k.CreatedBy, k.LastUsedAt, k.ExpiresAt, k.CreatedAt)
}

func (s *Store) GetAPIKeyByHash(ctx context.Context, hash string) (store.APIKey, error) {
	return one(ctx, s, `SELECT `+apiKeyCols+` FROM api_keys WHERE hash = $1`, []any{hash}, scanAPIKey)
}

func (s *Store) ListAPIKeys(ctx context.Context, orgID uuid.UUID, p store.ListParams) (store.Page[store.APIKey], error) {
	b := &qb{}
	b.where("org_id = " + b.arg(orgID))
	tail, limit, err := b.paginate("id", p, true)
	if err != nil {
		return store.Page[store.APIKey]{}, err
	}
	items, err := collect(ctx, s, `SELECT `+apiKeyCols+` FROM api_keys`+b.whereSQL()+tail, b.args, scanAPIKey)
	if err != nil {
		return store.Page[store.APIKey]{}, err
	}
	return finishPage(items, limit, func(k store.APIKey) uuid.UUID { return k.ID }), nil
}

func (s *Store) DeleteAPIKey(ctx context.Context, orgID, id uuid.UUID) error {
	return s.execAffect(ctx, `DELETE FROM api_keys WHERE org_id = $1 AND id = $2`, orgID, id)
}

func (s *Store) TouchAPIKey(ctx context.Context, id uuid.UUID, at time.Time) error {
	return s.exec(ctx, `UPDATE api_keys SET last_used_at = $2 WHERE id = $1`, id, at.UTC())
}
