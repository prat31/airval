<img src="assets/logo.svg" alt="" width="48" align="left">

# airval documentation

<br clear="left">

airval is a self-hostable evaluation platform for AI services. Register the service you want to test (a **Target**),
build versioned **Datasets**, define **Suites** of **Evaluators** with **Gates**, run them manually, on a schedule, from
webhooks or CI, and study the results in the UI.

## Start here

| Page | What it covers |
|---|---|
| [Getting started](getting-started.md) | Install (binary, Docker, Helm), sample project, your first Run |
| [Concepts](concepts.md) | The vocabulary: Organization, Project, Target, Dataset, Evaluator, Suite, Gate, Run, ... |

## Guides

| Page | What it covers |
|---|---|
| [Targets](targets.md) | Every Target kind and config field, templating, JSONPath mapping, streaming, multi-turn, SSRF policy |
| [Datasets](datasets.md) | Import formats, column mapping, Hugging Face, versions, export |
| [Evaluators](evaluators.md) | Choosing evaluators, LLM judges, pairwise, human review, `fail_fast`, severity |
| [Evaluator reference](evaluators-reference.md) | Every evaluator kind and config field (generated from code) |
| [Gates](gates.md) | Gate grammar, functions, failure budgets, CI exit codes |
| [Runs](runs.md) | Triggers (manual, schedule, webhook, API), sampling, trials, cache, re-run, re-score, comparisons |
| [CI](ci.md) | `airval run`, JUnit and Markdown output, GitHub Action, GitLab |
| [Performance](performance.md) | How the engine is fast, and the tuning knobs |

## Operating airval

| Page | What it covers |
|---|---|
| [Deployment](deployment.md) | Binary, Docker Compose, Helm, Postgres and NATS, scaling, key rotation, backups, observability |
| [Configuration](configuration.md) | Every config key, env var and default |
| [API](api.md) | API keys, pagination, errors, live events (SSE) |
| [Security](security.md) | Auth, secrets, SSRF policy, webhooks |
| [Enterprise](enterprise.md) | The `ee` build: SSO, SCIM, custom roles, audit log, retention, branding |
| [Contributing](contributing.md) | Repo layout, make targets, adding a Target adapter or Evaluator |

The API contract is [`api/openapi.yaml`](../api/openapi.yaml). The product plan is in [`plan/`](../plan/).

Some pages are partly generated from code: [evaluators-reference.md](evaluators-reference.md) with
`go run ./tools/docsgen > docs/evaluators-reference.md`.
