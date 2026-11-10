-- +goose Up
CREATE TABLE organizations (
    id         uuid PRIMARY KEY,
    name       text NOT NULL,
    slug       text NOT NULL UNIQUE,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL
);

CREATE TABLE users (
    id            uuid PRIMARY KEY,
    email         text NOT NULL UNIQUE,
    name          text NOT NULL DEFAULT '',
    password_hash text,
    avatar_url    text NOT NULL DEFAULT '',
    created_at    timestamptz NOT NULL,
    updated_at    timestamptz NOT NULL
);

CREATE TABLE memberships (
    org_id     uuid NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    user_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    role       text NOT NULL CHECK (role IN ('owner', 'admin', 'editor', 'viewer')),
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    PRIMARY KEY (org_id, user_id)
);
CREATE INDEX memberships_user_idx ON memberships (user_id);

CREATE TABLE sessions (
    id           uuid PRIMARY KEY,
    user_id      uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    token_hash   text NOT NULL UNIQUE,
    csrf_token   text NOT NULL,
    user_agent   text NOT NULL DEFAULT '',
    ip           text NOT NULL DEFAULT '',
    expires_at   timestamptz NOT NULL,
    last_seen_at timestamptz NOT NULL,
    created_at   timestamptz NOT NULL
);
CREATE INDEX sessions_user_idx ON sessions (user_id);
CREATE INDEX sessions_expires_idx ON sessions (expires_at);

CREATE TABLE projects (
    id          uuid PRIMARY KEY,
    org_id      uuid NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    name        text NOT NULL,
    slug        text NOT NULL,
    description text NOT NULL DEFAULT '',
    color       text NOT NULL DEFAULT '',
    icon        text NOT NULL DEFAULT '',
    settings    jsonb NOT NULL DEFAULT '{}',
    created_at  timestamptz NOT NULL,
    updated_at  timestamptz NOT NULL,
    UNIQUE (org_id, slug)
);

CREATE TABLE api_keys (
    id           uuid PRIMARY KEY,
    org_id       uuid NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    project_id   uuid REFERENCES projects (id) ON DELETE CASCADE,
    name         text NOT NULL,
    prefix       text NOT NULL,
    hash         text NOT NULL UNIQUE,
    role         text NOT NULL CHECK (role IN ('owner', 'admin', 'editor', 'viewer')),
    created_by   uuid REFERENCES users (id) ON DELETE SET NULL,
    last_used_at timestamptz,
    expires_at   timestamptz,
    created_at   timestamptz NOT NULL
);
CREATE INDEX api_keys_org_idx ON api_keys (org_id, id);

CREATE TABLE secrets (
    id          uuid PRIMARY KEY,
    org_id      uuid NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    project_id  uuid REFERENCES projects (id) ON DELETE CASCADE,
    name        text NOT NULL,
    description text NOT NULL DEFAULT '',
    ciphertext  bytea NOT NULL,
    nonce       bytea NOT NULL,
    key_version integer NOT NULL,
    created_by  uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at  timestamptz NOT NULL,
    updated_at  timestamptz NOT NULL
);
CREATE UNIQUE INDEX secrets_scope_name_key
    ON secrets (org_id, COALESCE(project_id, '00000000-0000-0000-0000-000000000000'::uuid), name);

CREATE TABLE targets (
    id                 uuid PRIMARY KEY,
    org_id             uuid NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    project_id         uuid NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    name               text NOT NULL,
    description        text NOT NULL DEFAULT '',
    kind               text NOT NULL,
    current_version_id uuid,
    created_at         timestamptz NOT NULL,
    updated_at         timestamptz NOT NULL,
    UNIQUE (project_id, name)
);

CREATE TABLE target_versions (
    id         uuid PRIMARY KEY,
    org_id     uuid NOT NULL,
    project_id uuid NOT NULL,
    target_id  uuid NOT NULL REFERENCES targets (id) ON DELETE CASCADE,
    number     integer NOT NULL,
    config     jsonb NOT NULL,
    created_by uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL,
    UNIQUE (target_id, number)
);

CREATE TABLE datasets (
    id                uuid PRIMARY KEY,
    org_id            uuid NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    project_id        uuid NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    name              text NOT NULL,
    description       text NOT NULL DEFAULT '',
    schema            jsonb,
    latest_version_id uuid,
    created_at        timestamptz NOT NULL,
    updated_at        timestamptz NOT NULL,
    UNIQUE (project_id, name)
);

CREATE TABLE examples (
    id          uuid PRIMARY KEY,
    org_id      uuid NOT NULL,
    project_id  uuid NOT NULL,
    dataset_id  uuid NOT NULL REFERENCES datasets (id) ON DELETE CASCADE,
    revision_id uuid NOT NULL,
    deleted_at  timestamptz,
    created_at  timestamptz NOT NULL,
    updated_at  timestamptz NOT NULL
);
CREATE INDEX examples_dataset_live_idx ON examples (dataset_id, id) WHERE deleted_at IS NULL;

CREATE TABLE example_revisions (
    id           uuid PRIMARY KEY,
    example_id   uuid NOT NULL REFERENCES examples (id) ON DELETE CASCADE,
    dataset_id   uuid NOT NULL,
    input        jsonb NOT NULL,
    expected     jsonb,
    context      jsonb,
    metadata     jsonb,
    tags         jsonb NOT NULL DEFAULT '[]',
    conversation jsonb,
    created_at   timestamptz NOT NULL
);
CREATE INDEX example_revisions_example_idx ON example_revisions (example_id);
CREATE INDEX example_revisions_tags_idx ON example_revisions USING gin (tags);

CREATE TABLE dataset_versions (
    id            uuid PRIMARY KEY,
    org_id        uuid NOT NULL,
    project_id    uuid NOT NULL,
    dataset_id    uuid NOT NULL REFERENCES datasets (id) ON DELETE CASCADE,
    number        integer NOT NULL,
    note          text NOT NULL DEFAULT '',
    example_count integer NOT NULL DEFAULT 0,
    created_by    uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at    timestamptz NOT NULL,
    UNIQUE (dataset_id, number)
);

CREATE TABLE dataset_version_items (
    version_id  uuid NOT NULL REFERENCES dataset_versions (id) ON DELETE CASCADE,
    example_id  uuid NOT NULL,
    revision_id uuid NOT NULL,
    PRIMARY KEY (version_id, example_id)
);

CREATE TABLE evaluators (
    id          uuid PRIMARY KEY,
    org_id      uuid NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    project_id  uuid NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    name        text NOT NULL,
    description text NOT NULL DEFAULT '',
    kind        text NOT NULL,
    config      jsonb NOT NULL DEFAULT '{}',
    version     integer NOT NULL DEFAULT 1,
    created_at  timestamptz NOT NULL,
    updated_at  timestamptz NOT NULL,
    UNIQUE (project_id, name)
);

CREATE TABLE suites (
    id              uuid PRIMARY KEY,
    org_id          uuid NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    project_id      uuid NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    name            text NOT NULL,
    slug            text NOT NULL,
    description     text NOT NULL DEFAULT '',
    config          jsonb NOT NULL DEFAULT '{}',
    baseline_run_id uuid,
    created_at      timestamptz NOT NULL,
    updated_at      timestamptz NOT NULL,
    UNIQUE (project_id, slug)
);

CREATE TABLE gates (
    id         uuid PRIMARY KEY,
    org_id     uuid NOT NULL,
    project_id uuid NOT NULL,
    suite_id   uuid NOT NULL REFERENCES suites (id) ON DELETE CASCADE,
    name       text NOT NULL DEFAULT '',
    expr       jsonb NOT NULL,
    enabled    boolean NOT NULL DEFAULT TRUE,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL
);
CREATE INDEX gates_suite_idx ON gates (suite_id);

CREATE TABLE triggers (
    id                 uuid PRIMARY KEY,
    org_id             uuid NOT NULL,
    project_id         uuid NOT NULL,
    suite_id           uuid NOT NULL REFERENCES suites (id) ON DELETE CASCADE,
    name               text NOT NULL DEFAULT '',
    kind               text NOT NULL CHECK (kind IN ('manual', 'schedule', 'webhook', 'api')),
    config             jsonb NOT NULL DEFAULT '{}',
    enabled            boolean NOT NULL DEFAULT TRUE,
    secret_enc         bytea,
    secret_key_version integer NOT NULL DEFAULT 0,
    next_fire_at       timestamptz,
    last_fired_at      timestamptz,
    created_at         timestamptz NOT NULL,
    updated_at         timestamptz NOT NULL
);
CREATE INDEX triggers_suite_idx ON triggers (suite_id);
CREATE INDEX triggers_due_idx ON triggers (next_fire_at) WHERE enabled AND kind = 'schedule';

CREATE TABLE runs (
    id                 uuid PRIMARY KEY,
    org_id             uuid NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    project_id         uuid NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    suite_id           uuid REFERENCES suites (id) ON DELETE SET NULL,
    target_id          uuid REFERENCES targets (id) ON DELETE SET NULL,
    target_version_id  uuid,
    dataset_version_id uuid,
    name               text NOT NULL DEFAULT '',
    trigger_kind       text NOT NULL DEFAULT 'manual',
    trigger_ref        text NOT NULL DEFAULT '',
    labels             jsonb NOT NULL DEFAULT '{}',
    config             jsonb NOT NULL DEFAULT '{}',
    status             text NOT NULL,
    total              integer NOT NULL DEFAULT 0,
    completed          integer NOT NULL DEFAULT 0,
    failed             integer NOT NULL DEFAULT 0,
    errored            integer NOT NULL DEFAULT 0,
    started_at         timestamptz,
    finished_at        timestamptz,
    baseline_run_id    uuid,
    parent_run_id      uuid,
    gate_passed        boolean,
    summary            jsonb,
    cost_usd           double precision NOT NULL DEFAULT 0,
    tokens_in          bigint NOT NULL DEFAULT 0,
    tokens_out         bigint NOT NULL DEFAULT 0,
    error              text NOT NULL DEFAULT '',
    created_by         uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at         timestamptz NOT NULL,
    updated_at         timestamptz NOT NULL
);
CREATE INDEX runs_project_idx ON runs (project_id, id DESC);
CREATE INDEX runs_suite_idx ON runs (suite_id, id DESC);
CREATE INDEX runs_target_idx ON runs (target_id, id DESC);
CREATE INDEX runs_parent_idx ON runs (parent_run_id) WHERE parent_run_id IS NOT NULL;

CREATE TABLE results (
    id                  uuid PRIMARY KEY,
    run_id              uuid NOT NULL REFERENCES runs (id) ON DELETE CASCADE,
    example_id          uuid NOT NULL,
    example_revision_id uuid NOT NULL,
    trial               integer NOT NULL DEFAULT 0,
    status              text NOT NULL,
    output              text NOT NULL DEFAULT '',
    output_z            bytea,
    output_json         jsonb,
    context             jsonb,
    tool_calls          jsonb,
    trace               jsonb,
    transcript          jsonb,
    request             jsonb,
    response            jsonb,
    latency_ms          double precision NOT NULL DEFAULT 0,
    ttft_ms             double precision,
    tokens_in           integer NOT NULL DEFAULT 0,
    tokens_out          integer NOT NULL DEFAULT 0,
    cost_usd            double precision NOT NULL DEFAULT 0,
    error               text NOT NULL DEFAULT '',
    cached              boolean NOT NULL DEFAULT FALSE,
    created_at          timestamptz NOT NULL
);
CREATE UNIQUE INDEX results_run_example_trial_key ON results (run_id, example_id, trial);
CREATE INDEX results_run_status_idx ON results (run_id, status);
CREATE INDEX results_run_id_idx ON results (run_id, id);

CREATE TABLE scores (
    result_id    uuid NOT NULL REFERENCES results (id) ON DELETE CASCADE,
    evaluator_id uuid NOT NULL,
    run_id       uuid NOT NULL,
    value        double precision,
    label        text NOT NULL DEFAULT '',
    passed       boolean,
    reason       text NOT NULL DEFAULT '',
    meta         jsonb,
    latency_ms   double precision NOT NULL DEFAULT 0,
    cost_usd     double precision NOT NULL DEFAULT 0,
    created_at   timestamptz NOT NULL,
    PRIMARY KEY (result_id, evaluator_id)
);
CREATE INDEX scores_evaluator_result_idx ON scores (evaluator_id, result_id);
CREATE INDEX scores_run_evaluator_idx ON scores (run_id, evaluator_id);

CREATE TABLE run_aggregates (
    run_id       uuid NOT NULL REFERENCES runs (id) ON DELETE CASCADE,
    evaluator_id uuid NOT NULL,
    metric       text NOT NULL,
    slice_key    text NOT NULL DEFAULT '',
    n            integer NOT NULL DEFAULT 0,
    mean         double precision,
    min          double precision,
    max          double precision,
    p50          double precision,
    p90          double precision,
    p95          double precision,
    p99          double precision,
    stddev       double precision,
    sum          double precision,
    pass_rate    double precision,
    histogram    jsonb,
    updated_at   timestamptz NOT NULL,
    PRIMARY KEY (run_id, evaluator_id, metric, slice_key)
);

CREATE TABLE review_queues (
    id           uuid PRIMARY KEY,
    org_id       uuid NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    project_id   uuid NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    name         text NOT NULL,
    description  text NOT NULL DEFAULT '',
    evaluator_id uuid,
    config       jsonb NOT NULL DEFAULT '{}',
    created_at   timestamptz NOT NULL,
    updated_at   timestamptz NOT NULL
);
CREATE INDEX review_queues_project_idx ON review_queues (project_id, id);

CREATE TABLE review_items (
    id           uuid PRIMARY KEY,
    org_id       uuid NOT NULL,
    project_id   uuid NOT NULL,
    queue_id     uuid NOT NULL REFERENCES review_queues (id) ON DELETE CASCADE,
    run_id       uuid NOT NULL REFERENCES runs (id) ON DELETE CASCADE,
    result_id    uuid NOT NULL REFERENCES results (id) ON DELETE CASCADE,
    status       text NOT NULL DEFAULT 'pending',
    assigned_to  uuid REFERENCES users (id) ON DELETE SET NULL,
    completed_by uuid REFERENCES users (id) ON DELETE SET NULL,
    completed_at timestamptz,
    created_at   timestamptz NOT NULL,
    updated_at   timestamptz NOT NULL,
    UNIQUE (queue_id, result_id)
);
CREATE INDEX review_items_queue_status_idx ON review_items (queue_id, status, id);

CREATE TABLE annotations (
    id             uuid PRIMARY KEY,
    org_id         uuid NOT NULL,
    project_id     uuid NOT NULL,
    result_id      uuid NOT NULL REFERENCES results (id) ON DELETE CASCADE,
    review_item_id uuid REFERENCES review_items (id) ON DELETE SET NULL,
    user_id        uuid REFERENCES users (id) ON DELETE SET NULL,
    evaluator_id   uuid,
    value          double precision,
    label          text NOT NULL DEFAULT '',
    passed         boolean,
    comment        text NOT NULL DEFAULT '',
    created_at     timestamptz NOT NULL,
    updated_at     timestamptz NOT NULL
);
CREATE INDEX annotations_result_idx ON annotations (result_id);
CREATE INDEX annotations_project_idx ON annotations (project_id, id);
CREATE INDEX annotations_evaluator_idx ON annotations (evaluator_id) WHERE evaluator_id IS NOT NULL;

CREATE TABLE comparisons (
    id         uuid PRIMARY KEY,
    org_id     uuid NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    project_id uuid NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    name       text NOT NULL DEFAULT '',
    run_ids    jsonb NOT NULL,
    created_by uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL
);
CREATE INDEX comparisons_project_idx ON comparisons (project_id, id);

CREATE TABLE notification_channels (
    id         uuid PRIMARY KEY,
    org_id     uuid NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    name       text NOT NULL,
    kind       text NOT NULL CHECK (kind IN ('slack', 'webhook', 'email')),
    config     jsonb NOT NULL DEFAULT '{}',
    events     jsonb NOT NULL DEFAULT '[]',
    enabled    boolean NOT NULL DEFAULT TRUE,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL
);
CREATE INDEX notification_channels_org_idx ON notification_channels (org_id, id);

CREATE TABLE audit_log (
    id       uuid PRIMARY KEY,
    org_id   uuid NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    actor_id uuid,
    actor    text NOT NULL DEFAULT '',
    action   text NOT NULL,
    target   text NOT NULL DEFAULT '',
    diff     jsonb,
    at       timestamptz NOT NULL
);
CREATE INDEX audit_log_org_idx ON audit_log (org_id, id DESC);

CREATE TABLE cache_entries (
    key        text PRIMARY KEY,
    value      bytea NOT NULL,
    created_at timestamptz NOT NULL,
    expires_at timestamptz
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
