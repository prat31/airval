# AGENTS.md — airval

Guidance for every coding agent (and human) working in this repo. Read it fully before changing anything.
The current master plan lives in `plan/` (highest number = latest). This file holds things that stay true across plans.

## What airval is

airval is a **self-hostable evaluation platform for AI services**. Teams register the AI service they want to test,
build datasets, define evaluation suites, run them (manually, on a schedule, from webhooks or CI), and study the
results in clear, beautiful visualizations. It is the flagship product. Quality bar is "would ship to paying customers".

## Non-negotiables

1. **Deployable service, three ways, always working:** a single static binary (`airval`), a Docker image
   (`ghcr.io/prat31/airval`), and a Helm chart (`deploy/helm/airval`). A change that breaks any of the three is not done.
2. **Fast runs are a product feature.** Every change on the execution path is judged by throughput and wall-clock time.
   Use bounded concurrency, adaptive rate limiting, caching, batching and streaming. Don't do unbounded fan-out,
   N+1 queries, per-row DB writes in hot loops, or synchronous waits that could be pipelined.
3. **The UI is beautiful, elegant and easy to navigate.** It uses only design tokens (see *Design system*). No raw hex,
   no ad-hoc spacing, no one-off components when a primitive exists. Every screen handles empty, loading and error states
   and works in light and dark.
4. **One consistent color palette**, defined once in `web/src/styles/tokens.css` and mirrored in `plan/001.md §Design system`.
   Change it only on purpose, in both places.
5. **Open core.** Core code is Apache-2.0. Enterprise-only code lives only under `ee/` (separate license) and
   core must build and run without it (`-tags ee` adds it). Core must never import `ee/`.
6. **API-first.** `api/openapi.yaml` is the contract. Server stubs and the TS client are generated from it, and the UI
   uses only the public API.
7. **Secrets are never logged or returned in plaintext** after creation. They are encrypted at rest (AES-256-GCM, master key from env).

## Glossary (use these words exactly, in code, API and UI)

| Term | Meaning |
|---|---|
| **Organization** | Tenant boundary. Has members with roles. |
| **Project** | One AI service under evaluation. Owns targets, datasets, suites, runs. |
| **Target** | A connection to the AI service (HTTP endpoint, LLM provider model, agent). A project can have several (e.g. prod vs staging, model A vs B). Versioned config. |
| **Dataset** | A collection of **Examples**. Versioned: every edit produces an immutable **Dataset Version**. |
| **Example** | One row: `input` (or a multi-turn script), optional `expected` output, `context`, `metadata`, `tags`. |
| **Evaluator** | A function that scores an output, producing a **Score** (0–1 number, label and/or pass/fail plus reasoning). Kinds: deterministic, LLM judge, human. |
| **Suite** | A reusable evaluation definition: dataset version(s) + evaluators + thresholds + execution settings. |
| **Gate** | A suite-level pass criterion (e.g. "faithfulness mean ≥ 0.85 and no critical failures"). It decides run pass/fail and CI exit codes. |
| **Run** | One execution of a suite against a target. Has a status, totals and aggregates. |
| **Result** | Output + scores for one example within a run (× **Trial** when repeated for nondeterminism). |
| **Trigger** | What starts a run: manual, schedule (cron), webhook, API/CI. |
| **Baseline** | The run a new run is compared against (pinned per suite or chosen ad hoc). |
| **Comparison** | Side-by-side diff of two or more runs: regressions, improvements, per-example deltas. |
| **Review Queue** | A queue of results waiting for human **Annotations**. |
| **Secret** | Encrypted credential referenced by targets and LLM judges (`{{ secret "OPENAI_KEY" }}`). |

Don't use: "test", "experiment", "job" or "case" as user-facing nouns for the above. ("Job" is fine internally for queue messages.)

## Stack

- **Backend:** Go (latest stable). `chi` router, `oapi-codegen`, `pgx` (Postgres) and `modernc.org/sqlite` (CGO-free),
  `sqlc` per dialect behind a `store.Store` interface, `goose` migrations, `log/slog`, OpenTelemetry, Prometheus.
- **Queue:** NATS JetStream. It is embedded in-process for `airval dev` (all-in-one) and runs as an external cluster in production.
- **DB:** SQLite only for all-in-one/dev. Postgres for server+worker deployments.
- **Frontend:** React + TypeScript + Vite, Tailwind v4 (tokens as CSS variables), Radix primitives, TanStack
  Router/Query/Table/Virtual, ECharts, cmdk. Built into `web/dist` and embedded into the binary via `go:embed`.
- **Release:** goreleaser (linux/darwin/windows × amd64/arm64), distroless multi-arch image, Helm chart in `deploy/helm`.

## Repo layout

```
cmd/airval/          single binary entrypoint (subcommands: dev, server, worker, migrate, run, version)
internal/            core Go packages (api, auth, store, queue, runner, targets, evaluators, scheduler, secrets…)
ee/                  enterprise features (separate license, build tag `ee`)
api/openapi.yaml     API contract
web/                 React app
deploy/docker, deploy/helm/airval
plan/                numbered plans (001.md, 002.md…). New big direction = new file, never rewrite history
```

## Working rules

- Big changes start with a plan file in `plan/NNN.md`. Keep plans updated when decisions change.
- `make check` (lint + tests for Go and web) must pass. Store code runs its tests against **both** SQLite and Postgres.
- Every new evaluator or target adapter ships with unit tests and a docs entry.
- Brand: the logo is the "monogram a" mark (`web/src/components/brand/brand.tsx`, colors `--brand-mark` /
  `--brand-mark-ink`). Never redraw it; static assets (favicons, PWA icons, `docs/assets/logo*`, social preview) are
  generated from the same geometry by `node web/scripts/brand-assets.mjs`.
- Use the design tokens and existing UI primitives. Any new chart follows the chart rules in `plan/001.md §Design system`.
- Keep the binary CGO-free.
- Don't add infrastructure dependencies (beyond Postgres + NATS) without a plan entry.
