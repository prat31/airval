# Deployment verification

Date: 2026-11-11. Commit under test: `9a6a540` plus the fixes listed below. Host: macOS arm64, Docker 29.8.1, Go 1.27.3.
Every channel was exercised with the same scripted flow, [`deploy/verify/flow.sh`](verify/flow.sh): log in as the
bootstrap admin, create an API key, create a project, an HTTP target pointing at `tools/mocktarget` (`/rag`, accuracy 0.8),
a dataset imported from CSV, three deterministic evaluators (`equals`, `regex`, `latency_ms`), a suite and a gate, then
`airval run --gate --markdown` twice: a passing gate (`mean(exact_match) >= 0.5`, expect exit 0) and a deliberately
failing gate (`>= 0.999`, expect exit 1).

## Results

| Channel | Result | 1,000 examples (wall clock, run duration) |
|---|---|---|
| Binary (`make build`, `airval dev`, SQLite + embedded NATS) | pass: exit 0 / exit 1 as expected | 1.2 s run (about 48k examples/min); `make build` 15 s |
| Docker image + compose (Postgres 17, NATS, migrate, server, 2 workers, mocktarget) | pass | 0.3-0.4 s run (150-190k examples/min); image build 32 s (warm cache) |
| Helm on kind v1.31 (bundled NATS x3, dev Postgres, 2 server, 2 -> 3 workers) | pass | 0.8-0.9 s run; 5,000 examples: 1.2 s |
| goreleaser snapshot + `scripts/install.sh` | pass | n/a |

Markdown report for the failing gate shows the red banner and the gate detail (`got 0.795`); the passing one shows the
green banner. Pass rate 79.5% (205 of 1,000 failed) matches the mock's 0.8 accuracy.

## 1. Binary

`make build` then `./bin/airval dev` in a temp dir with `AIRVAL_BOOTSTRAP_ADMIN_EMAIL/PASSWORD`; `/readyz` reports
database and nats ok. Flow passes. No issues.

## 2. Docker / compose

`docker compose --profile mocktarget up -d` (ports remapped through `.env` because 8080/18080 were taken on the host).
Migrate exits 0, server healthy. Workers had no healthcheck: added `airval-probe http://127.0.0.1:8081/readyz`
(workers serve `/healthz`, `/readyz`, `/metrics` on `worker.listen`). Both workers report `healthy`.
Distribution (worker `/metrics`, `airval_examples_processed_total`, two 1,000-example runs): worker-1 1,175,
worker-2 825. No warn/error log lines. `docker compose down -v` removed containers, volumes and network.

## 3. Helm on kind

kind v0.25 (`go install sigs.k8s.io/kind`), helm 3.16.4 via the `alpine/helm` image, locally built image loaded with
`kind load docker-image` (`image.pullPolicy=Never`). Values: `postgresql.dev.enabled`, bootstrap admin.

* Install `--wait` ready in about 107 s (3 NATS pods dominate). Server/worker pods restarted 3-4 times during the first
  minute: the headless dev-Postgres Service has no DNS record until the pod is ready, and the apps exit on a failed
  database ping. They converge by themselves; with a managed Postgres this does not occur.
* `helm test`: passes after the fix below.
* Port-forward + `flow.sh` (mock target deployed as a pod): pass, both exit codes as expected.
* `helm upgrade`: revision 2 deployed, migration hook Job re-ran (`airval-migrate` created and completed), the
  `secrets-master-key` and `postgres-password` in Secret `airval` unchanged (compared before/after).
* Scale workers to 3 (`--set worker.replicaCount=3`): third pod ready in about 5 s; a 5,000-example run was split
  across all three (2,851+783 / 3,729+1,001 / 2,888+748 passed+failed counters, cumulative).
* Cluster deleted afterwards (`kind delete cluster`).

## 4. goreleaser and install.sh

`go run github.com/goreleaser/goreleaser/v2@latest release --snapshot --clean --skip=publish,sign,sbom,docker,validate`
succeeded in 48 s: six archives (linux/darwin/windows x amd64/arm64), probe binaries, `checksums.txt`, Homebrew cask.
Binary sizes 55-59 MiB; archives 18-20 MiB. `scripts/install.sh` against the snapshot served by
`python3 -m http.server` (`AIRVAL_DOWNLOAD_BASE`): checksum verified, installed, `airval version` prints `0.0.1-next`.
Not exercised here (tools missing, release-time only): docker images via goreleaser (`dockers_v2`), SBOM (syft), cosign.

## Issues found and fixes

1. **Helm: server, worker and migration pods crash-looped** with `'nats.port' cannot parse value as 'int'`.
   Kubernetes service-link env vars (`AIRVAL_NATS_PORT=tcp://10.x:4222`, from the Service `airval-nats`; likewise
   `AIRVAL_PORT` for a Service named `airval`) land in the `AIRVAL_*` config namespace. Fix: `enableServiceLinks: false`
   on the server, worker and migration pod specs. Chart-only; no Go change. (Worth hardening the config loader too:
   ignore values that look like `tcp://` link variables. Not done.)
2. **Helm: `helm test` failed**: BusyBox `wget` has no `--waitretry`/`--retry-connrefused`. The test now polls in a
   shell loop for up to 2 minutes.
3. **Helm: no worker probes.** Added startup/liveness (`/healthz`) and readiness (`/readyz`) probes on port 8081 and a
   named `metrics` container port.
4. **Helm: no simple database for evaluation without an operator.** Added `postgresql.dev.enabled` (single-pod
   `postgres:17-alpine` StatefulSet, emptyDir or PVC, generated password kept stable via `lookup`, listed in
   `values.schema.json`, `ci/dev-values.yaml`, chart README). Like CNPG, the server migrates itself on first start and
   the hook Job runs on upgrades only.
5. **Compose: worker healthcheck** added (see section 2).
6. **OTLP**: values comments and `ci/full-values.yaml` described gRPC :4317; the code exports OTLP over HTTP, so they
   now say HTTP :4318. No compose/README text mentioned the port.
7. **`deploy/helm/validate.sh`** failed once `Chart.lock` existed (`dependency build` needs a configured repo inside the
   helm container): it now falls back to `dependency update`. `Chart.lock` is committed; `charts/*.tgz` is git-ignored.
8. Added `deploy/verify/flow.sh`, the reusable end-to-end flow used above.

Cosmetic, left alone: the Markdown report HTML-escapes `>=` in gate names inside table cells (`&gt;=`).

## Reproduce

```sh
make build && ./bin/airval dev &          # with AIRVAL_BOOTSTRAP_ADMIN_* set
go run ./tools/mocktarget &
ADMIN_PASSWORD=... N=1000 deploy/verify/flow.sh
```
