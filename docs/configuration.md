# Configuration

Precedence, lowest first: built-in defaults, an optional YAML file (`--config path` or `AIRVAL_CONFIG=path`), then
`AIRVAL_*` environment variables.

**Env mapping:** strip the `AIRVAL_` prefix, lower-case, and the first `_` separates section from key:

| Env var | Key |
|---|---|
| `AIRVAL_DATABASE_URL` | `database.url` |
| `AIRVAL_SECRETS_MASTER_KEY` | `secrets.master_key` |
| `AIRVAL_BOOTSTRAP_ADMIN_EMAIL` | `bootstrap.admin_email` |
| `AIRVAL_WORKERS_CONCURRENCY` | `workers.concurrency` |

`airval config print` prints the effective configuration with secrets redacted (it uses the `dev` defaults when no
other context applies, so check `database.url` and friends against your own file and environment).

```yaml
# airval.yaml
database:
  url: postgres://airval:pass@db:5432/airval?sslmode=require
nats:
  url: nats://nats:4222
secrets:
  master_key: ""            # prefer AIRVAL_SECRETS_MASTER_KEY from a secret store
server:
  base_url: https://airval.example.com
auth:
  cookie_secure: true
workers:
  concurrency: 256
```

`server` and `worker` fail to start without `database.url` and `secrets.master_key`. `airval dev` defaults to SQLite in
`./airval-data`, enables the embedded worker, logs as text, traces everything, and generates `airval-data/master.key`
when no key is set.

## Keys

### database

| Key | Env | Default | Description |
|---|---|---|---|
| `database.url` | `AIRVAL_DATABASE_URL` | empty (dev: `sqlite://./airval-data/airval.db`) | `postgres://...` or a SQLite path. Postgres for server and worker deployments. |
| `database.max_open_conns` | `AIRVAL_DATABASE_MAX_OPEN_CONNS` | `0` (driver default) | Connection pool cap. |
| `database.auto_migrate` | `AIRVAL_DATABASE_AUTO_MIGRATE` | `true` | Apply migrations on startup. Set `false` when a separate migrate step owns schema changes (the compose file and Helm chart do). |

### nats

| Key | Env | Default | Description |
|---|---|---|---|
| `nats.url` | `AIRVAL_NATS_URL` | empty | External NATS with JetStream. Empty starts an embedded server (single process only; not for multi-replica deployments). |
| `nats.store_dir` | `AIRVAL_NATS_STORE_DIR` | `./airval-data/nats` | JetStream directory of the embedded server. |
| `nats.port` | `AIRVAL_NATS_PORT` | `-1` | Embedded server port (`-1`: in-process only, no listener). |
| `nats.creds_file` | `AIRVAL_NATS_CREDS_FILE` | empty | NATS credentials file. |
| `nats.replicas` | `AIRVAL_NATS_REPLICAS` | `1` | Stream replicas (use 3 on a 3-node cluster). |
| `nats.stream_max_age_hours` | `AIRVAL_NATS_STREAM_MAX_AGE_HOURS` | `72` | Retention of run events. |

### secrets

| Key | Env | Default | Description |
|---|---|---|---|
| `secrets.master_key` | `AIRVAL_SECRETS_MASTER_KEY` | empty | Base64 of 32 random bytes (AES-256-GCM). Required outside dev. Generate with `airval secrets gen-key`. |
| `secrets.key_version` | `AIRVAL_SECRETS_KEY_VERSION` | `1` | Label of the current key. Bump when rotating. |
| `secrets.previous_keys` | `AIRVAL_SECRETS_PREVIOUS_KEYS` | empty | Older keys that must stay readable: `1:<base64>,2:<base64>`. |

### server

| Key | Env | Default | Description |
|---|---|---|---|
| `server.addr` | `AIRVAL_SERVER_ADDR` | `:8080` | Listen address. |
| `server.base_url` | `AIRVAL_SERVER_BASE_URL` | `http://localhost:8080` | Public URL. Used in links (Run URLs in notifications, webhook URLs). |
| `server.shutdown_timeout` | `AIRVAL_SERVER_SHUTDOWN_TIMEOUT` | `20s` | Graceful shutdown deadline. |
| `server.ui_dir` | `AIRVAL_SERVER_UI_DIR` | empty | Serve the UI from disk instead of the embedded build (frontend development). |
| `server.max_upload_mb` | `AIRVAL_SERVER_MAX_UPLOAD_MB` | `512` | Dataset import upload cap. |

### auth

| Key | Env | Default | Description |
|---|---|---|---|
| `auth.session_ttl` | `AIRVAL_AUTH_SESSION_TTL` | `720h` | Session lifetime. |
| `auth.cookie_secure` | `AIRVAL_AUTH_COOKIE_SECURE` | `false` | Set `true` behind HTTPS. |
| `auth.cookie_domain` | `AIRVAL_AUTH_COOKIE_DOMAIN` | empty | Cookie domain. |
| `auth.allow_signup` | `AIRVAL_AUTH_ALLOW_SIGNUP` | `false` | Let anyone create an account (normally admins invite). |

### bootstrap

Creates the first owner and Organization at startup when no users exist (used by Helm). Needs both email and password.

| Key | Env | Default |
|---|---|---|
| `bootstrap.admin_email` | `AIRVAL_BOOTSTRAP_ADMIN_EMAIL` | empty |
| `bootstrap.admin_password` | `AIRVAL_BOOTSTRAP_ADMIN_PASSWORD` | empty |
| `bootstrap.admin_name` | `AIRVAL_BOOTSTRAP_ADMIN_NAME` | `Admin` |
| `bootstrap.org_name` | `AIRVAL_BOOTSTRAP_ORG_NAME` | `Default` |

### workers and worker

| Key | Env | Default | Description |
|---|---|---|---|
| `workers.concurrency` | `AIRVAL_WORKERS_CONCURRENCY` | `256` | Examples in flight per worker. |
| `workers.judge_concurrency` | `AIRVAL_WORKERS_JUDGE_CONCURRENCY` | `32` | LLM-judge calls in flight per worker. |
| `workers.shutdown_timeout` | `AIRVAL_WORKERS_SHUTDOWN_TIMEOUT` | `30s` | Drain deadline; unfinished shards are returned to the queue. |
| `workers.embedded` | `AIRVAL_WORKERS_EMBEDDED` | `false` (dev: `true`) | Run the worker loop inside the server process. |
| `worker.listen` | `AIRVAL_WORKER_LISTEN` | `:8081` | Worker's operational listener: `/healthz`, `/readyz`, `/metrics`. Empty disables it. |

### telemetry

| Key | Env | Default | Description |
|---|---|---|---|
| `telemetry.log_level` | `AIRVAL_TELEMETRY_LOG_LEVEL` | `info` | slog level. |
| `telemetry.log_format` | `AIRVAL_TELEMETRY_LOG_FORMAT` | `json` (dev: `text`) | `json` or `text`. |
| `telemetry.otlp_endpoint` | `AIRVAL_TELEMETRY_OTLP_ENDPOINT` | empty | OTLP over HTTP (`host:port` is plain HTTP, or a full `http(s)://` URL). Empty disables export. |
| `telemetry.service_name` | `AIRVAL_TELEMETRY_SERVICE_NAME` | `airval` | |
| `telemetry.sample_ratio` | `AIRVAL_TELEMETRY_SAMPLE_RATIO` | `0.1` (dev: `1`) | Trace sampling ratio. |
| `telemetry.metrics` | `AIRVAL_TELEMETRY_METRICS` | `true` | Serve `/metrics`. |

### security

| Key | Env | Default | Description |
|---|---|---|---|
| `security.allow_cidrs` | `AIRVAL_SECURITY_ALLOW_CIDRS` | empty | Comma-separated. When set, outbound requests (Targets, webhooks) may only reach these ranges. |
| `security.deny_cidrs` | `AIRVAL_SECURITY_DENY_CIDRS` | empty | Comma-separated extra denied ranges. Cloud metadata and link-local are always denied. |

## Other variables

| Variable | Used by | Description |
|---|---|---|
| `AIRVAL_CONFIG` | all commands | Path of the YAML file (same as `--config`). |
| `AIRVAL_URL`, `AIRVAL_API_KEY`, `AIRVAL_PROJECT` | `airval run` | Server URL, API key, Project for slug lookup. |
| `AIRVAL_VERSION`, `AIRVAL_INSTALL_DIR` | `scripts/install.sh` | Release tag and install directory. |
| `AIRVAL_TEST_PG_URL`, `AIRVAL_BENCH_EXAMPLES` | tests, `make bench` | See [Contributing](contributing.md). |
