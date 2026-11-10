-- +goose Up
CREATE TABLE organizations (
    id         TEXT PRIMARY KEY,
    name       text NOT NULL,
    slug       text NOT NULL UNIQUE,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL
);

CREATE TABLE users (
    id            TEXT PRIMARY KEY,
    email         text NOT NULL UNIQUE,
    name          text NOT NULL DEFAULT '',
    password_hash text,
    avatar_url    text NOT NULL DEFAULT '',
    created_at    TIMESTAMP NOT NULL,
    updated_at    TIMESTAMP NOT NULL
);

CREATE TABLE memberships (
    org_id     TEXT NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    user_id    TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    role       text NOT NULL CHECK (role IN ('owner', 'admin', 'editor', 'viewer')),
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL,
    PRIMARY KEY (org_id, user_id)
);
CREATE INDEX memberships_user_idx ON memberships (user_id);

CREATE TABLE sessions (
    id           TEXT PRIMARY KEY,
    user_id      TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    token_hash   text NOT NULL UNIQUE,
    csrf_token   text NOT NULL,
    user_agent   text NOT NULL DEFAULT '',
    ip           text NOT NULL DEFAULT '',
    expires_at   TIMESTAMP NOT NULL,
    last_seen_at TIMESTAMP NOT NULL,
    created_at   TIMESTAMP NOT NULL
);
CREATE INDEX sessions_user_idx ON sessions (user_id);
CREATE INDEX sessions_expires_idx ON sessions (expires_at);

CREATE TABLE projects (
    id          TEXT PRIMARY KEY,
    org_id      TEXT NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    name        text NOT NULL,
    slug        text NOT NULL,
    description text NOT NULL DEFAULT '',
    color       text NOT NULL DEFAULT '',
    icon        text NOT NULL DEFAULT '',
    settings    TEXT NOT NULL DEFAULT '{}',
    created_at  TIMESTAMP NOT NULL,
    updated_at  TIMESTAMP NOT NULL,
    UNIQUE (org_id, slug)
);

CREATE TABLE api_keys (
    id           TEXT PRIMARY KEY,
    org_id       TEXT NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    project_id   TEXT REFERENCES projects (id) ON DELETE CASCADE,
    name         text NOT NULL,
    prefix       text NOT NULL,
    hash         text NOT NULL UNIQUE,
    role         text NOT NULL CHECK (role IN ('owner', 'admin', 'editor', 'viewer')),
    created_by   TEXT REFERENCES users (id) ON DELETE SET NULL,
    last_used_at TIMESTAMP,
    expires_at   TIMESTAMP,
    created_at   TIMESTAMP NOT NULL
);
CREATE INDEX api_keys_org_idx ON api_keys (org_id, id);

CREATE TABLE secrets (
    id          TEXT PRIMARY KEY,
    org_id      TEXT NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    project_id  TEXT REFERENCES projects (id) ON DELETE CASCADE,
    name        text NOT NULL,
    description text NOT NULL DEFAULT '',
    ciphertext  BLOB NOT NULL,
    nonce       BLOB NOT NULL,
    key_version integer NOT NULL,
    created_by  TEXT REFERENCES users (id) ON DELETE SET NULL,
    created_at  TIMESTAMP NOT NULL,
    updated_at  TIMESTAMP NOT NULL
);
CREATE UNIQUE INDEX secrets_scope_name_key
    ON secrets (org_id, COALESCE(project_id, '00000000-0000-0000-0000-000000000000'), name);

CREATE TABLE targets (
    id                 TEXT PRIMARY KEY,
    org_id             TEXT NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    project_id         TEXT NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    name               text NOT NULL,
    description        text NOT NULL DEFAULT '',
    kind               text NOT NULL,
    current_version_id TEXT,
    created_at         TIMESTAMP NOT NULL,
    updated_at         TIMESTAMP NOT NULL,
    UNIQUE (project_id, name)
);

CREATE TABLE target_versions (
    id         TEXT PRIMARY KEY,
    org_id     TEXT NOT NULL,
    project_id TEXT NOT NULL,
    target_id  TEXT NOT NULL REFERENCES targets (id) ON DELETE CASCADE,
    number     integer NOT NULL,
    config     TEXT NOT NULL,
    created_by TEXT REFERENCES users (id) ON DELETE SET NULL,
    created_at TIMESTAMP NOT NULL,
    UNIQUE (target_id, number)
);

CREATE TABLE datasets (
    id                TEXT PRIMARY KEY,
    org_id            TEXT NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    project_id        TEXT NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    name              text NOT NULL,
    description       text NOT NULL DEFAULT '',
    schema            TEXT,
    latest_version_id TEXT,
    created_at        TIMESTAMP NOT NULL,
    updated_at        TIMESTAMP NOT NULL,
    UNIQUE (project_id, name)
);

CREATE TABLE examples (
    id          TEXT PRIMARY KEY,
    org_id      TEXT NOT NULL,
    project_id  TEXT NOT NULL,
    dataset_id  TEXT NOT NULL REFERENCES datasets (id) ON DELETE CASCADE,
    revision_id TEXT NOT NULL,
    deleted_at  TIMESTAMP,
    created_at  TIMESTAMP NOT NULL,
    updated_at  TIMESTAMP NOT NULL
);
CREATE INDEX examples_dataset_live_idx ON examples (dataset_id, id) WHERE deleted_at IS NULL;

CREATE TABLE example_revisions (
    id           TEXT PRIMARY KEY,
    example_id   TEXT NOT NULL REFERENCES examples (id) ON DELETE CASCADE,
    dataset_id   TEXT NOT NULL,
    input        TEXT NOT NULL,
    expected     TEXT,
    context      TEXT,
    metadata     TEXT,
    tags         TEXT NOT NULL DEFAULT '[]',
    conversation TEXT,
    created_at   TIMESTAMP NOT NULL
);
CREATE INDEX example_revisions_example_idx ON example_revisions (example_id);

CREATE TABLE dataset_versions (
    id            TEXT PRIMARY KEY,
    org_id        TEXT NOT NULL,
    project_id    TEXT NOT NULL,
    dataset_id    TEXT NOT NULL REFERENCES datasets (id) ON DELETE CASCADE,
    number        integer NOT NULL,
    note          text NOT NULL DEFAULT '',
    example_count integer NOT NULL DEFAULT 0,
    created_by    TEXT REFERENCES users (id) ON DELETE SET NULL,
    created_at    TIMESTAMP NOT NULL,
    UNIQUE (dataset_id, number)
);

CREATE TABLE dataset_version_items (
    version_id  TEXT NOT NULL REFERENCES dataset_versions (id) ON DELETE CASCADE,
    example_id  TEXT NOT NULL,
    revision_id TEXT NOT NULL,
    PRIMARY KEY (version_id, example_id)
);

CREATE TABLE evaluators (
    id          TEXT PRIMARY KEY,
    org_id      TEXT NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    project_id  TEXT NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    name        text NOT NULL,
    description text NOT NULL DEFAULT '',
    kind        text NOT NULL,
    config      TEXT NOT NULL DEFAULT '{}',
    version     integer NOT NULL DEFAULT 1,
    created_at  TIMESTAMP NOT NULL,
    updated_at  TIMESTAMP NOT NULL,
    UNIQUE (project_id, name)
);

CREATE TABLE suites (
    id              TEXT PRIMARY KEY,
    org_id          TEXT NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    project_id      TEXT NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    name            text NOT NULL,
    slug            text NOT NULL,
    description     text NOT NULL DEFAULT '',
    config          TEXT NOT NULL DEFAULT '{}',
    baseline_run_id TEXT,
    created_at      TIMESTAMP NOT NULL,
    updated_at      TIMESTAMP NOT NULL,
    UNIQUE (project_id, slug)
);

CREATE TABLE gates (
    id         TEXT PRIMARY KEY,
    org_id     TEXT NOT NULL,
    project_id TEXT NOT NULL,
    suite_id   TEXT NOT NULL REFERENCES suites (id) ON DELETE CASCADE,
    name       text NOT NULL DEFAULT '',
    expr       TEXT NOT NULL,
    enabled    boolean NOT NULL DEFAULT TRUE,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL
);
CREATE INDEX gates_suite_idx ON gates (suite_id);

CREATE TABLE triggers (
    id                 TEXT PRIMARY KEY,
    org_id             TEXT NOT NULL,
    project_id         TEXT NOT NULL,
    suite_id           TEXT NOT NULL REFERENCES suites (id) ON DELETE CASCADE,
    name               text NOT NULL DEFAULT '',
    kind               text NOT NULL CHECK (kind IN ('manual', 'schedule', 'webhook', 'api')),
    config             TEXT NOT NULL DEFAULT '{}',
    enabled            boolean NOT NULL DEFAULT TRUE,
    secret_enc         BLOB,
    secret_key_version integer NOT NULL DEFAULT 0,
    next_fire_at       TIMESTAMP,
    last_fired_at      TIMESTAMP,
    created_at         TIMESTAMP NOT NULL,
    updated_at         TIMESTAMP NOT NULL
);
CREATE INDEX triggers_suite_idx ON triggers (suite_id);
CREATE INDEX triggers_due_idx ON triggers (next_fire_at) WHERE enabled AND kind = 'schedule';

CREATE TABLE runs (
    id                 TEXT PRIMARY KEY,
    org_id             TEXT NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    project_id         TEXT NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    suite_id           TEXT REFERENCES suites (id) ON DELETE SET NULL,
    target_id          TEXT REFERENCES targets (id) ON DELETE SET NULL,
    target_version_id  TEXT,
    dataset_version_id TEXT,
    name               text NOT NULL DEFAULT '',
    trigger_kind       text NOT NULL DEFAULT 'manual',
    trigger_ref        text NOT NULL DEFAULT '',
    labels             TEXT NOT NULL DEFAULT '{}',
    config             TEXT NOT NULL DEFAULT '{}',
    status             text NOT NULL,
    total              integer NOT NULL DEFAULT 0,
    completed          integer NOT NULL DEFAULT 0,
    failed             integer NOT NULL DEFAULT 0,
    errored            integer NOT NULL DEFAULT 0,
    started_at         TIMESTAMP,
    finished_at        TIMESTAMP,
    baseline_run_id    TEXT,
    parent_run_id      TEXT,
    gate_passed        boolean,
    summary            TEXT,
    cost_usd           REAL NOT NULL DEFAULT 0,
    tokens_in          INTEGER NOT NULL DEFAULT 0,
    tokens_out         INTEGER NOT NULL DEFAULT 0,
    error              text NOT NULL DEFAULT '',
    created_by         TEXT REFERENCES users (id) ON DELETE SET NULL,
    created_at         TIMESTAMP NOT NULL,
    updated_at         TIMESTAMP NOT NULL
);
CREATE INDEX runs_project_idx ON runs (project_id, id DESC);
CREATE INDEX runs_suite_idx ON runs (suite_id, id DESC);
CREATE INDEX runs_target_idx ON runs (target_id, id DESC);
CREATE INDEX runs_parent_idx ON runs (parent_run_id) WHERE parent_run_id IS NOT NULL;

CREATE TABLE results (
    id                  TEXT PRIMARY KEY,
    run_id              TEXT NOT NULL REFERENCES runs (id) ON DELETE CASCADE,
    example_id          TEXT NOT NULL,
    example_revision_id TEXT NOT NULL,
    trial               integer NOT NULL DEFAULT 0,
    status              text NOT NULL,
    output              text NOT NULL DEFAULT '',
    output_z            BLOB,
    output_json         TEXT,
    context             TEXT,
    tool_calls          TEXT,
    trace               TEXT,
    transcript          TEXT,
    request             TEXT,
    response            TEXT,
    latency_ms          REAL NOT NULL DEFAULT 0,
    ttft_ms             REAL,
    tokens_in           integer NOT NULL DEFAULT 0,
    tokens_out          integer NOT NULL DEFAULT 0,
    cost_usd            REAL NOT NULL DEFAULT 0,
    error               text NOT NULL DEFAULT '',
    cached              boolean NOT NULL DEFAULT FALSE,
    created_at          TIMESTAMP NOT NULL
);
CREATE UNIQUE INDEX results_run_example_trial_key ON results (run_id, example_id, trial);
CREATE INDEX results_run_status_idx ON results (run_id, status);
CREATE INDEX results_run_id_idx ON results (run_id, id);

CREATE TABLE scores (
    result_id    TEXT NOT NULL REFERENCES results (id) ON DELETE CASCADE,
    evaluator_id TEXT NOT NULL,
    run_id       TEXT NOT NULL,
    value        REAL,
    label        text NOT NULL DEFAULT '',
    passed       boolean,
    reason       text NOT NULL DEFAULT '',
    meta         TEXT,
    latency_ms   REAL NOT NULL DEFAULT 0,
    cost_usd     REAL NOT NULL DEFAULT 0,
    created_at   TIMESTAMP NOT NULL,
    PRIMARY KEY (result_id, evaluator_id)
);
CREATE INDEX scores_evaluator_result_idx ON scores (evaluator_id, result_id);
CREATE INDEX scores_run_evaluator_idx ON scores (run_id, evaluator_id);

CREATE TABLE run_aggregates (
    run_id       TEXT NOT NULL REFERENCES runs (id) ON DELETE CASCADE,
    evaluator_id TEXT NOT NULL,
    metric       text NOT NULL,
    slice_key    text NOT NULL DEFAULT '',
    n            integer NOT NULL DEFAULT 0,
    mean         REAL,
    min          REAL,
    max          REAL,
    p50          REAL,
    p90          REAL,
    p95          REAL,
    p99          REAL,
    stddev       REAL,
    sum          REAL,
    pass_rate    REAL,
    histogram    TEXT,
    updated_at   TIMESTAMP NOT NULL,
    PRIMARY KEY (run_id, evaluator_id, metric, slice_key)
);

CREATE TABLE review_queues (
    id           TEXT PRIMARY KEY,
    org_id       TEXT NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    project_id   TEXT NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    name         text NOT NULL,
    description  text NOT NULL DEFAULT '',
    evaluator_id TEXT,
    config       TEXT NOT NULL DEFAULT '{}',
    created_at   TIMESTAMP NOT NULL,
    updated_at   TIMESTAMP NOT NULL
);
CREATE INDEX review_queues_project_idx ON review_queues (project_id, id);

CREATE TABLE review_items (
    id           TEXT PRIMARY KEY,
    org_id       TEXT NOT NULL,
    project_id   TEXT NOT NULL,
    queue_id     TEXT NOT NULL REFERENCES review_queues (id) ON DELETE CASCADE,
    run_id       TEXT NOT NULL REFERENCES runs (id) ON DELETE CASCADE,
    result_id    TEXT NOT NULL REFERENCES results (id) ON DELETE CASCADE,
    status       text NOT NULL DEFAULT 'pending',
    assigned_to  TEXT REFERENCES users (id) ON DELETE SET NULL,
    completed_by TEXT REFERENCES users (id) ON DELETE SET NULL,
    completed_at TIMESTAMP,
    created_at   TIMESTAMP NOT NULL,
    updated_at   TIMESTAMP NOT NULL,
    UNIQUE (queue_id, result_id)
);
CREATE INDEX review_items_queue_status_idx ON review_items (queue_id, status, id);

CREATE TABLE annotations (
    id             TEXT PRIMARY KEY,
    org_id         TEXT NOT NULL,
    project_id     TEXT NOT NULL,
    result_id      TEXT NOT NULL REFERENCES results (id) ON DELETE CASCADE,
    review_item_id TEXT REFERENCES review_items (id) ON DELETE SET NULL,
    user_id        TEXT REFERENCES users (id) ON DELETE SET NULL,
    evaluator_id   TEXT,
    value          REAL,
    label          text NOT NULL DEFAULT '',
    passed         boolean,
    comment        text NOT NULL DEFAULT '',
    created_at     TIMESTAMP NOT NULL,
    updated_at     TIMESTAMP NOT NULL
);
CREATE INDEX annotations_result_idx ON annotations (result_id);
CREATE INDEX annotations_project_idx ON annotations (project_id, id);
CREATE INDEX annotations_evaluator_idx ON annotations (evaluator_id) WHERE evaluator_id IS NOT NULL;

CREATE TABLE comparisons (
    id         TEXT PRIMARY KEY,
    org_id     TEXT NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    project_id TEXT NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    name       text NOT NULL DEFAULT '',
    run_ids    TEXT NOT NULL,
    created_by TEXT REFERENCES users (id) ON DELETE SET NULL,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL
);
CREATE INDEX comparisons_project_idx ON comparisons (project_id, id);

CREATE TABLE notification_channels (
    id         TEXT PRIMARY KEY,
    org_id     TEXT NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    name       text NOT NULL,
    kind       text NOT NULL CHECK (kind IN ('slack', 'webhook', 'email')),
    config     TEXT NOT NULL DEFAULT '{}',
    events     TEXT NOT NULL DEFAULT '[]',
    enabled    boolean NOT NULL DEFAULT TRUE,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL
);
CREATE INDEX notification_channels_org_idx ON notification_channels (org_id, id);

CREATE TABLE audit_log (
    id       TEXT PRIMARY KEY,
    org_id   TEXT NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    actor_id TEXT,
    actor    text NOT NULL DEFAULT '',
    action   text NOT NULL,
    target   text NOT NULL DEFAULT '',
    diff     TEXT,
    at       TIMESTAMP NOT NULL
);
CREATE INDEX audit_log_org_idx ON audit_log (org_id, id DESC);

CREATE TABLE cache_entries (
    key        text PRIMARY KEY,
    value      BLOB NOT NULL,
    created_at TIMESTAMP NOT NULL,
    expires_at TIMESTAMP
);
CREATE INDEX cache_entries_expires_idx ON cache_entries (expires_at) WHERE expires_at IS NOT NULL;

-- +goose Down
DROP TABLE IF EXISTS cache_entries;
DROP TABLE IF EXISTS audit_log;
DROP TABLE IF EXISTS notification_channels;
DROP TABLE IF EXISTS comparisons;
DROP TABLE IF EXISTS annotations;
DROP TABLE IF EXISTS review_items;
DROP TABLE IF EXISTS review_queues;
DROP TABLE IF EXISTS run_aggregates;
DROP TABLE IF EXISTS scores;
DROP TABLE IF EXISTS results;
DROP TABLE IF EXISTS runs;
DROP TABLE IF EXISTS triggers;
DROP TABLE IF EXISTS gates;
DROP TABLE IF EXISTS suites;
DROP TABLE IF EXISTS evaluators;
DROP TABLE IF EXISTS dataset_version_items;
DROP TABLE IF EXISTS dataset_versions;
DROP TABLE IF EXISTS example_revisions;
DROP TABLE IF EXISTS examples;
DROP TABLE IF EXISTS datasets;
DROP TABLE IF EXISTS target_versions;
DROP TABLE IF EXISTS targets;
DROP TABLE IF EXISTS secrets;
DROP TABLE IF EXISTS api_keys;
DROP TABLE IF EXISTS projects;
DROP TABLE IF EXISTS sessions;
DROP TABLE IF EXISTS memberships;
DROP TABLE IF EXISTS users;
DROP TABLE IF EXISTS organizations;
