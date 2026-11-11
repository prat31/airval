# airval Helm chart

Deploys airval: `server` (API + UI + scheduler), `worker`, a migration Job, and optionally NATS JetStream and a
CloudNativePG database.

## Quickstart

```sh
helm dependency build deploy/helm/airval          # fetches the nats subchart
helm install airval deploy/helm/airval -n airval --create-namespace \
  --set postgresql.external.url='postgres://user:pass@db:5432/airval?sslmode=require' \
  --set config.baseUrl=https://airval.example.com --set config.cookieSecure=true
helm test airval -n airval                         # GET /readyz through the Service
```

## What gets installed

| Piece | Notes |
|---|---|
| server Deployment, Service, optional HPA, PDB | probes `/healthz` and `/readyz`; Prometheus `/metrics` |
| worker Deployment, PDB, HPA **or** KEDA ScaledObject | KEDA scales on JetStream consumer lag (`RUNS_JOBS`/`workers`); workers expose no HTTP port, so they have no probes |
| migration Job | `airval migrate up` as a pre-install/pre-upgrade hook |
| NATS | official `nats` subchart (JetStream, 3 replicas) or `nats.enabled=false` + `nats.external.url` |
| Postgres | `postgresql.external.url` / `existingSecret` (recommended) or `postgresql.cnpg.enabled` (evaluation only; needs the CloudNativePG operator) |
| Ingress or Gateway API HTTPRoute | mutually exclusive; configure `config.baseUrl` to match |
| ServiceMonitor, NetworkPolicy | optional |

## Secrets

* `secrets.existingSecret`: a Secret with `secrets-master-key` (base64 of 32 bytes, `airval secrets gen-key`), and
  optionally `secrets-previous-keys` and `bootstrap-admin-password` (key names are configurable in `secrets.keys`).
* Otherwise the chart creates a Secret. If `secrets.masterKey` is empty a key is generated on first install and kept
  stable across upgrades via `lookup`. The Secret is a Helm hook resource, so it survives `helm uninstall`.
  `lookup` does not work with `helm template` or Argo CD: use `existingSecret` or set `secrets.masterKey` there.
* Database URL: `postgresql.external.url` (stored in the chart Secret) or `postgresql.external.existingSecret` + key.

Back up the master key: losing it makes every stored secret unreadable. Rotate with `secrets.keyVersion`,
`secrets.previousKeys` and `airval secrets rotate`.

## Notes

* Migrations run as a hook before the new pods roll out. With `postgresql.cnpg.enabled` the database does not exist
  before install, so the server migrates itself on first start and the Job runs on upgrades only.
* Server pods are stateless; scale `server.replicaCount` or enable `server.autoscaling`.
* Behind ingress-nginx raise `nginx.ingress.kubernetes.io/proxy-read-timeout` (run progress streams over SSE).
* Hardened by default: non-root, read-only root filesystem, all capabilities dropped, seccomp `RuntimeDefault`.
* `deploy/helm/validate.sh` lints and renders every file in `ci/`, then runs kubeconform.

See `values.yaml` (documented) and `values.schema.json` for all options.
