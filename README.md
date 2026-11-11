# airval

Self-hostable evaluation platform for AI services: register the service you want to test (a **Target**),
build versioned **Datasets**, define **Suites** of **Evaluators** with **Gates**, run them manually, on a
schedule, from webhooks or CI, and study the results in the UI.

The full product plan is in [`plan/001.md`](plan/001.md); contributor rules are in [`AGENTS.md`](AGENTS.md).

## Quickstart

### 1. Single binary (all-in-one)

```sh
make build            # builds web/dist and bin/airval (CGO-free, UI embedded)
./bin/airval dev      # SQLite + embedded NATS in ./airval-data, http://localhost:8080
```

Open http://localhost:8080 and complete the setup screen (first owner + organization).
`airval dev` generates and persists a secrets master key in `./airval-data/master.key`.

### 2. Docker

```sh
docker run -p 8080:8080 -v airval-data:/data -w /data ghcr.io/airval/airval:latest dev
```

For production use `deploy/docker/compose.yaml` (Postgres + NATS + server + workers).

### 3. Kubernetes (Helm)

```sh
helm install airval deploy/helm/airval \
  --set secretsMasterKey="$(airval secrets gen-key)" \
  --set postgresql.external.url=postgres://... --set nats.external.url=nats://...
```

### Server + worker (production)

```sh
export AIRVAL_DATABASE_URL=postgres://user:pass@db:5432/airval
export AIRVAL_NATS_URL=nats://nats:4222
export AIRVAL_SECRETS_MASTER_KEY="$(airval secrets gen-key)"   # 32 bytes, base64
airval migrate up
airval server     # API + UI + scheduler (scale horizontally)
airval worker     # executes runs (scale horizontally)
```

Optional first-boot admin (Helm-friendly): `AIRVAL_BOOTSTRAP_ADMIN_EMAIL`, `AIRVAL_BOOTSTRAP_ADMIN_PASSWORD`,
`AIRVAL_BOOTSTRAP_ORG_NAME`.

### CI

```sh
AIRVAL_URL=https://airval.example.com AIRVAL_API_KEY=airv_... \
  airval run --suite <suite-id> --label git_sha=$GITHUB_SHA --gate
```

## Configuration

YAML file (`--config` / `AIRVAL_CONFIG`) plus `AIRVAL_*` env overrides: strip the prefix, lower-case, and the first
`_` separates section and key (`AIRVAL_DATABASE_URL` → `database.url`). `airval config print` shows the effective,
redacted config. Main keys: `database.url`, `nats.url` (empty = embedded), `secrets.master_key`,
`secrets.key_version`, `secrets.previous_keys`, `server.addr`, `server.base_url`, `auth.session_ttl`,
`auth.cookie_secure`, `workers.concurrency`, `telemetry.log_level|log_format|otlp_endpoint|sample_ratio`.

Rotate the master key: set the new key + `secrets.key_version` (+1), put the old one in
`secrets.previous_keys` (`1:<base64>`), then run `airval secrets rotate`.

## Development

```sh
make check      # go vet + gofmt + Go tests (SQLite, and Postgres via docker) + web checks
make test-pg    # store/API contract tests against Postgres (starts the airval-pg container)
make gen        # regenerate the Go server and TS client from api/openapi.yaml
```

Layout: `cmd/airval` (binary), `internal/*` (core, Apache-2.0), `ee/` (enterprise, `-tags ee`, LICENSE-EE),
`api/openapi.yaml` (the contract), `web/` (React UI). Operational endpoints: `/healthz`, `/readyz`, `/metrics`.

## License

Core: Apache-2.0 ([LICENSE](LICENSE)). `ee/`: commercial ([LICENSE-EE](LICENSE-EE)).
