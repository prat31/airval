# Deployment

airval deploys three ways, all built from the same binary: a single static binary, a Docker image
(`ghcr.io/prat31/airval`) and a Helm chart (`deploy/helm/airval`).

| Mode | Processes | Database | Queue | Use for |
|---|---|---|---|---|
| `airval dev` | one process (server + worker + scheduler) | SQLite in `./airval-data` | embedded NATS | Trying things out, local development, small single-node setups |
| `airval server` + `airval worker` | any number of each | Postgres | external NATS JetStream | Anything shared |

## Requirements

- **Postgres** (tested with 17) for server and worker deployments. SQLite is for `dev` only.
- **NATS 2.x with JetStream enabled.** It carries the work queue, run events (SSE), leader election, rate-limit buckets
  and locks. Use 3 replicas for availability (set `nats.replicas: 3`).
- **A secrets master key** (32 random bytes, base64).
- **Outbound network access** from workers to your Targets and LLM providers. Servers need outbound access for
  notifications and Hugging Face imports.

## Single binary

```sh
export AIRVAL_DATABASE_URL=postgres://user:pass@db:5432/airval
export AIRVAL_NATS_URL=nats://nats:4222
export AIRVAL_SECRETS_MASTER_KEY="$(airval secrets gen-key)"     # store it safely, see Key rotation
export AIRVAL_SERVER_BASE_URL=https://airval.example.com AIRVAL_AUTH_COOKIE_SECURE=true
airval migrate up          # apply schema migrations
airval server              # API + UI + scheduler + planner. Stateless: run several behind a load balancer
airval worker              # executes runs. Run as many as needed
```

`airval migrate status|up|down` manage the schema (`down` rolls back the latest migration). `server` migrates on startup
unless `database.auto_migrate` is `false`. Servers and workers do not need to share a filesystem.
Without a service manager, run the commands under systemd (`Restart=on-failure`, `TimeoutStopSec=60` so workers can
drain).

Operational endpoints: `/healthz` (liveness), `/readyz` (database and NATS) and `/metrics` on the server; the worker
serves the same on `worker.listen` (default `:8081`).

Put TLS in front (ingress, load balancer or reverse proxy) and set `auth.cookie_secure=true` and `server.base_url`. Run
progress streams over SSE, so raise proxy read timeouts and disable response buffering (the server sends
`X-Accel-Buffering: no`).

## Docker

```sh
docker run -p 8080:8080 -v airval-data:/data ghcr.io/prat31/airval:latest dev
```

The image is distroless, non-root (uid 65532), multi-arch, and works with a read-only root filesystem; state lives in the
`/data` volume. The default command is `server`. `/usr/local/bin/airval-probe <url>` is included for healthchecks
(there is no shell).

## Docker Compose

`deploy/docker/compose.yaml` runs Postgres 17, NATS JetStream, a one-shot `migrate` service, the server and 2 workers.

```sh
cd deploy/docker
cp .env.example .env     # set AIRVAL_SECRETS_MASTER_KEY (docker run --rm ghcr.io/prat31/airval secrets gen-key) and POSTGRES_PASSWORD
docker compose up -d --build            # http://localhost:8080 ; /readyz reports database + NATS
docker compose up -d --scale worker=4   # more workers
docker compose --profile mocktarget up -d   # fake AI service at http://mocktarget:9090
```

`.env` options: `AIRVAL_IMAGE`, `AIRVAL_SERVER_BASE_URL`, `AIRVAL_AUTH_COOKIE_SECURE`, `AIRVAL_PORT`,
`AIRVAL_BOOTSTRAP_ADMIN_EMAIL` / `AIRVAL_BOOTSTRAP_ADMIN_PASSWORD` / `AIRVAL_BOOTSTRAP_ORG_NAME`,
`AIRVAL_WORKERS_CONCURRENCY`, `AIRVAL_TELEMETRY_LOG_LEVEL`. Services run read-only with all capabilities dropped.
Workers have a 45 s stop grace period to finish in-flight work.

## Kubernetes (Helm)

```sh
helm dependency build deploy/helm/airval          # fetches the nats subchart
helm install airval deploy/helm/airval -n airval --create-namespace \
  --set postgresql.external.url='postgres://user:pass@db:5432/airval?sslmode=require' \
  --set config.baseUrl=https://airval.example.com --set config.cookieSecure=true
helm test airval -n airval                         # GET /readyz through the Service
```

The chart installs a `server` Deployment (+ Service, optional HPA, PDB), a `worker` Deployment (+ PDB, HPA or KEDA), a
migration Job (pre-install / pre-upgrade hook running `airval migrate up`), the NATS subchart (JetStream, 3 replicas) or an
external NATS, optional CloudNativePG, Ingress or Gateway API HTTPRoute, ServiceMonitor and NetworkPolicy. Pods are hardened
(non-root, read-only root filesystem, no capabilities, seccomp `RuntimeDefault`). See
[`deploy/helm/airval/README.md`](../deploy/helm/airval/README.md) and `values.schema.json`.

### Values reference

| Value | Default | Description |
|---|---|---|
| `image.repository`, `image.tag`, `image.pullPolicy` | `ghcr.io/prat31/airval`, chart appVersion, `IfNotPresent` | Image. |
| `pullSecrets` | `[]` | Image pull secrets. |
| `config.baseUrl` | `""` | Public URL (`server.base_url`). Set it with Ingress or Gateway. |
| `config.logLevel`, `config.logFormat` | `info`, `json` | Logging. |
| `config.cookieSecure` | `false` | Set `true` over HTTPS. |
| `config.allowSignup` | `false` | `auth.allow_signup`. |
| `config.otlpEndpoint`, `config.traceSampleRatio` | `""`, `0.1` | Trace export and sampling. |
| `config.env` | `{}` | Extra `AIRVAL_*` settings as a map, e.g. `AIRVAL_SECURITY_DENY_CIDRS`. |
| `config.extraEnv` | `[]` | Raw container env entries added to every pod. |
| `secrets.existingSecret` | `""` | Existing Secret with the master key (recommended for GitOps). |
| `secrets.keys.masterKey`, `.previousKeys`, `.bootstrapAdminPassword` | `secrets-master-key`, `secrets-previous-keys`, `bootstrap-admin-password` | Key names inside that Secret. |
| `secrets.masterKey` | `""` | Chart-created Secret: master key. Empty generates one on first install and keeps it across upgrades via `lookup` (not available in `helm template` / Argo CD). |
| `secrets.keyVersion`, `secrets.previousKeys` | `1`, `""` | Rotation settings for the chart-created Secret. |
| `bootstrap.adminEmail`, `.adminName`, `.orgName`, `.adminPassword` | `""`, `Admin`, `Default`, `""` | First-boot admin. |
| `postgresql.external.url` | `""` | Database URL (stored in the chart Secret)... |
| `postgresql.external.existingSecret`, `.existingSecretKey` | `""`, `database-url` | ...or a Secret you manage. |
| `postgresql.external.maxOpenConns` | `0` | `database.max_open_conns`. |
| `postgresql.cnpg.enabled`, `.instances`, `.imageName`, `.storage.size`, `.storage.storageClass`, `.resources` | `false`, `1`, `""`, `10Gi`, `""`, `{}` | Optional CloudNativePG cluster for evaluation (operator required). |
| `nats.enabled` | `true` | Bundle the official NATS subchart (JetStream, 3 replicas, 10Gi PVC). |
| `nats.external.url` | `""` | Use with `nats.enabled=false`. Other `nats.*` values configure the subchart. |
| `server.replicaCount` | `2` | Server replicas. |
| `server.service.type`, `.port`, `.annotations` | `ClusterIP`, `80`, `{}` | Service. |
| `server.resources` | requests 100m / 128Mi, limit 512Mi | |
| `server.autoscaling.enabled`, `.minReplicas`, `.maxReplicas`, `.targetCPUUtilizationPercentage` | `false`, `2`, `6`, `70` | HPA. |
| `server.pdb.enabled`, `.minAvailable` | `true`, `1` | PodDisruptionBudget. |
| `server.terminationGracePeriodSeconds` | `40` | |
| `worker.replicaCount` | `2` | Worker replicas. |
| `worker.concurrency`, `worker.judgeConcurrency` | `256`, `32` | `workers.concurrency`, `workers.judge_concurrency`. |
| `worker.resources` | requests 250m / 256Mi, limit 1Gi | |
| `worker.hpa.enabled`, `.minReplicas`, `.maxReplicas`, `.targetCPUUtilizationPercentage` | `false`, `2`, `20`, `70` | CPU autoscaling. Use HPA or KEDA, not both. |
| `worker.keda.enabled`, `.minReplicaCount`, `.maxReplicaCount`, `.pollingInterval`, `.cooldownPeriod` | `false`, `1`, `50`, `15`, `120` | KEDA ScaledObject on JetStream consumer lag. |
| `worker.keda.lagThreshold` | `100` | Pending jobs per replica before scaling out. |
| `worker.keda.natsMonitoringEndpoint`, `.account` | `""`, `$G` | NATS monitoring `host:port` (defaults to the bundled service on `:8222`). |
| `worker.pdb.enabled`, `.maxUnavailable` | `true`, `1` | |
| `worker.terminationGracePeriodSeconds` | `60` | Keep above `workers.shutdown_timeout` (30 s). |
| `server.*` / `worker.*` | | Also `podAnnotations`, `podLabels`, `nodeSelector`, `tolerations`, `affinity`, `topologySpreadConstraints`, `priorityClassName`, `extraEnv`. |
| `migration.enabled`, `.backoffLimit`, `.activeDeadlineSeconds`, `.resources` | `true`, `3`, `600`, small | Migration hook Job. |
| `ingress.enabled`, `.className`, `.annotations`, `.hosts`, `.tls` | `false` | Ingress. |
| `gateway.enabled`, `.hostnames`, `.parentRefs`, `.annotations` | `false` | Gateway API HTTPRoute (alternative to Ingress). |
| `serviceAccount.create`, `.name`, `.annotations` | `true` | |
| `serviceMonitor.enabled`, `.interval`, `.scrapeTimeout`, `.labels` | `false`, `30s`, `10s` | Prometheus Operator. |
| `networkPolicy.enabled`, `.ingressFrom`, `.extraEgress`, `.databasePort` | `false`, `[]`, `[]`, `5432` | DNS, Postgres, NATS and HTTPS egress are allowed by default. |
| `podSecurityContext`, `securityContext` | hardened | Non-root 65532, read-only root FS, no capabilities. |
| `test.image.repository`, `.tag` | `busybox`, `1.37` | `helm test` pod. |

## Scaling

- **Servers** are stateless. Scale on CPU. The scheduler runs on every replica but only the elected leader fires triggers.
- **Workers** scale throughput. Each takes shards from the queue; add replicas for more parallelism. Scale on CPU (HPA) or, better, on
  queue depth with **KEDA**: `worker.keda.enabled=true` scales on JetStream consumer lag (one replica per `lagThreshold`
  pending jobs, up to `maxReplicaCount`), and down when queues drain. Workers drain gracefully on SIGTERM: they stop pulling,
  finish in-flight work up to `workers.shutdown_timeout`, and return the rest to the queue.
- Rate limits you set on a Suite (`rpm`, `tpm`) are shared across workers, so adding workers does not exceed provider quotas.
- Mind the database: each worker batches writes, but hundreds of workers need `database.max_open_conns` and Postgres sized accordingly.

## Secrets master key and rotation

Stored Secrets are encrypted with AES-256-GCM using `secrets.master_key`. **Back up the key; losing it makes every stored
secret unreadable.** `airval dev` keeps its key in `airval-data/master.key`.

Rotate:

1. Generate a new key: `airval secrets gen-key`.
2. Set `secrets.master_key` to the new key and `secrets.key_version` to the old version + 1.
3. Put the old key in `secrets.previous_keys` as `<old version>:<old key>` (comma-separate several).
4. Restart, then run `airval secrets rotate` to re-encrypt every secret with the current key.
5. Once `rotate` reports success, remove the old key from `previous_keys`.

In Helm: `secrets.masterKey` / `secrets.keyVersion` / `secrets.previousKeys` (chart-created Secret), or the same keys in
your existing Secret.

## Backups and upgrades

- **Postgres is the system of record.** Back it up (`pg_dump`, managed snapshots, or CloudNativePG backups) together with
  the master key, stored separately. NATS holds transient data (queued shards, live events, locks): it can be rebuilt, but
  in-flight Runs would be interrupted.
- **Upgrade:** take a backup, deploy the new version. Migrations apply via `airval migrate up` (compose `migrate` service,
  Helm hook Job, or auto-migrate on `server` start). Servers and workers of mixed versions are only safe during a rolling
  update of the same release; upgrade workers and servers together. Roll back by restoring the backup and
  the previous image (`airval migrate down` rolls back one migration at a time if a migration provides it).
- Run snapshots are immutable, so in-flight Runs keep the configuration they started with.

## Observability

**Logs.** `log/slog`, JSON by default (`telemetry.log_format`), level from `telemetry.log_level`. Run, shard and example ids
are attached. Secret values are never logged.

**Metrics.** Prometheus at `/metrics` (server and worker): `airval_http_requests_total`, `airval_http_request_duration_seconds`,
`airval_examples_processed_total`, `airval_queue_pending_messages`, `airval_target_latency_seconds`,
`airval_target_throttled_total`, `airval_judge_cost_usd_total`, `airval_db_flush_duration_seconds`,
`airval_runs_finished_total`. Enable the Helm `serviceMonitor` for Prometheus Operator.

**Tracing.** OpenTelemetry spans cover server, queue, worker and Target calls. Set `telemetry.otlp_endpoint` (OTLP over
HTTP) and `telemetry.sample_ratio`. With no endpoint, no traces are exported.

**Health.** `/healthz` always answers if the process is up. `/readyz` checks the database and NATS (503 when not ready).
