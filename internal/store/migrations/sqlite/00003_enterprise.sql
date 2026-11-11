-- +goose Up
-- Enterprise edition (ee/, build tag `ee`) tables. They live in the core migration set so one schema serves both
-- editions and a deployment can switch editions without a separate migration track. Core never reads them
-- (apart from cleaning role assignments when a membership is removed). Every ee table is prefixed `ee_`.

-- Audit log: request metadata recorded by the audit middleware.
ALTER TABLE audit_log ADD COLUMN ip text NOT NULL DEFAULT '';
ALTER TABLE audit_log ADD COLUMN user_agent text NOT NULL DEFAULT '';
ALTER TABLE audit_log ADD COLUMN request_id text NOT NULL DEFAULT '';
ALTER TABLE audit_log ADD COLUMN status integer NOT NULL DEFAULT 0;
CREATE INDEX audit_log_org_at_idx ON audit_log (org_id, at);

-- SSO identity providers (OIDC or SAML). `slug` is global: it appears in login URLs.
-- `secret` is sealed with the secrets keyring (OIDC client secret or SAML SP private key).
CREATE TABLE ee_sso_providers (
    id          TEXT PRIMARY KEY,
    org_id      TEXT NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    slug        text NOT NULL UNIQUE,
    kind        text NOT NULL CHECK (kind IN ('oidc', 'saml')),
    name        text NOT NULL,
    enabled     boolean NOT NULL DEFAULT TRUE,
    config      TEXT NOT NULL DEFAULT '{}',
    secret      BLOB,
    key_version integer NOT NULL DEFAULT 0,
    created_at  TIMESTAMP NOT NULL,
    updated_at  TIMESTAMP NOT NULL
);
CREATE INDEX ee_sso_providers_org_idx ON ee_sso_providers (org_id);

-- Links an IdP subject to a local user (matched by subject; by verified email on first login).
CREATE TABLE ee_sso_identities (
    provider_id   TEXT NOT NULL REFERENCES ee_sso_providers (id) ON DELETE CASCADE,
    subject       text NOT NULL,
    user_id       TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    email         text NOT NULL DEFAULT '',
    last_login_at TIMESTAMP NOT NULL,
    created_at    TIMESTAMP NOT NULL,
    PRIMARY KEY (provider_id, subject)
);
CREATE INDEX ee_sso_identities_user_idx ON ee_sso_identities (user_id);

-- Custom roles: named permission sets.
CREATE TABLE ee_custom_roles (
    id          TEXT PRIMARY KEY,
    org_id      TEXT NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    key         text NOT NULL,
    name        text NOT NULL,
    description text NOT NULL DEFAULT '',
    permissions TEXT NOT NULL DEFAULT '[]',
    created_at  TIMESTAMP NOT NULL,
    updated_at  TIMESTAMP NOT NULL,
    UNIQUE (org_id, key)
);

-- Role assignments: a built-in or custom role for a member, org-wide (project_id = nil UUID) or per project.
CREATE TABLE ee_role_assignments (
    org_id         TEXT NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    user_id        TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    project_id     TEXT NOT NULL,
    role           text NOT NULL DEFAULT '',
    custom_role_id TEXT REFERENCES ee_custom_roles (id) ON DELETE CASCADE,
    source         text NOT NULL DEFAULT 'manual',
    created_at     TIMESTAMP NOT NULL,
    updated_at     TIMESTAMP NOT NULL,
    PRIMARY KEY (org_id, user_id, project_id),
    CHECK ((role = '') <> (custom_role_id IS NULL))
);
CREATE INDEX ee_role_assignments_project_idx ON ee_role_assignments (project_id);

-- SCIM 2.0 provisioning.
CREATE TABLE ee_scim_tokens (
    id           TEXT PRIMARY KEY,
    org_id       TEXT NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    name         text NOT NULL,
    prefix       text NOT NULL,
    hash         text NOT NULL UNIQUE,
    created_by   TEXT REFERENCES users (id) ON DELETE SET NULL,
    last_used_at TIMESTAMP,
    created_at   TIMESTAMP NOT NULL
);
CREATE INDEX ee_scim_tokens_org_idx ON ee_scim_tokens (org_id);

CREATE TABLE ee_scim_users (
    org_id      TEXT NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    user_id     TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    external_id text NOT NULL DEFAULT '',
    user_name   text NOT NULL,
    active      boolean NOT NULL DEFAULT TRUE,
    data        TEXT NOT NULL DEFAULT '{}',
    created_at  TIMESTAMP NOT NULL,
    updated_at  TIMESTAMP NOT NULL,
    PRIMARY KEY (org_id, user_id)
);
CREATE UNIQUE INDEX ee_scim_users_name_idx ON ee_scim_users (org_id, user_name);

CREATE TABLE ee_scim_groups (
    id           TEXT PRIMARY KEY,
    org_id       TEXT NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    display_name text NOT NULL,
    external_id  text NOT NULL DEFAULT '',
    created_at   TIMESTAMP NOT NULL,
    updated_at   TIMESTAMP NOT NULL,
    UNIQUE (org_id, display_name)
);

CREATE TABLE ee_scim_group_members (
    group_id TEXT NOT NULL REFERENCES ee_scim_groups (id) ON DELETE CASCADE,
    user_id  TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    PRIMARY KEY (group_id, user_id)
);
CREATE INDEX ee_scim_group_members_user_idx ON ee_scim_group_members (user_id);

-- Org-level enterprise settings (branding, audit retention, SCIM role mapping): one JSON document per key.
CREATE TABLE ee_org_settings (
    org_id     TEXT NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    key        text NOT NULL,
    value      TEXT NOT NULL,
    updated_at TIMESTAMP NOT NULL,
    PRIMARY KEY (org_id, key)
);

-- Per-project data retention.
CREATE TABLE ee_retention_policies (
    project_id      TEXT PRIMARY KEY REFERENCES projects (id) ON DELETE CASCADE,
    org_id          TEXT NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    enabled         boolean NOT NULL DEFAULT TRUE,
    run_days        integer NOT NULL,
    keep_baselines  boolean NOT NULL DEFAULT TRUE,
    last_applied_at TIMESTAMP,
    last_deleted    integer NOT NULL DEFAULT 0,
    total_deleted   INTEGER NOT NULL DEFAULT 0,
    created_at      TIMESTAMP NOT NULL,
    updated_at      TIMESTAMP NOT NULL
);
CREATE INDEX ee_retention_policies_org_idx ON ee_retention_policies (org_id);
CREATE INDEX runs_project_created_idx ON runs (project_id, created_at);

-- +goose Down
DROP INDEX IF EXISTS runs_project_created_idx;
DROP TABLE IF EXISTS ee_retention_policies;
DROP TABLE IF EXISTS ee_org_settings;
DROP TABLE IF EXISTS ee_scim_group_members;
DROP TABLE IF EXISTS ee_scim_groups;
DROP TABLE IF EXISTS ee_scim_users;
DROP TABLE IF EXISTS ee_scim_tokens;
DROP TABLE IF EXISTS ee_role_assignments;
DROP TABLE IF EXISTS ee_custom_roles;
DROP TABLE IF EXISTS ee_sso_identities;
DROP TABLE IF EXISTS ee_sso_providers;
DROP INDEX IF EXISTS audit_log_org_at_idx;
ALTER TABLE audit_log DROP COLUMN status;
ALTER TABLE audit_log DROP COLUMN request_id;
ALTER TABLE audit_log DROP COLUMN user_agent;
ALTER TABLE audit_log DROP COLUMN ip;
