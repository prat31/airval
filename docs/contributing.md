# Contributing

Read [`AGENTS.md`](../AGENTS.md) first: it holds the rules that stay true across plans (non-negotiables, glossary, stack).

## Repo layout

```
cmd/airval/          single binary entrypoint (dev, server, worker, migrate, run, secrets, config, version)
internal/            core Go packages (Apache-2.0)
  api/               generated server (gen/) + handlers, middleware
  app/               wiring and lifecycle
  auth/ rbac/        sessions, passwords, API keys, roles
  store/             interfaces + models, sqlstore (Postgres + SQLite), migrations, contract tests
  queue/             NATS JetStream (embedded + client), events, KV leases, rate-limit buckets
  planner/ runner/   run planning and sharding; the worker pipeline, AIMD, ingest, finalizer
  targets/           Target adapters, templating, JSONPath, multi-turn, SSRF guard
  evaluators/        deterministic, model, human, registry, judge prompts
  aggregate/         statistics, Gate grammar and evaluation, failure budgets, comparisons
  importers/         csv, jsonl, json, parquet, Hugging Face, export
  scheduler/ webhooks/ notify/ cache/ secrets/ config/ telemetry/ webui/ ext/ evalcore/
ee/                  enterprise features (build tag ee, LICENSE-EE)
api/openapi.yaml     the API contract
web/                 React + TypeScript + Vite app (embedded into the binary)
deploy/              docker/ (Dockerfile, compose), helm/airval
tools/               mocktarget (fake AI service), docsgen (generates the evaluator reference)
plan/                numbered plans (001.md, 002.md, ...)
docs/                this documentation
```

## Make targets

| Target | What it does |
|---|---|
| `make build` | Builds `web/dist` and the CGO-free `bin/airval` with the UI embedded. |
| `make build-ee` | Builds `bin/airval-ee` with `-tags ee`. |
| `make dev` | `go run ./cmd/airval dev` (SQLite + embedded NATS in `./airval-data`). |
| `make web` | Builds only the UI. |
| `make gen` | Regenerates the Go server (oapi-codegen) and the TypeScript client from `api/openapi.yaml`. |
| `make test` | Go unit and SQLite contract tests. |
| `make pg-up`, `make test-pg` | Start a Postgres container and run store and API contract tests against it (`AIRVAL_TEST_PG_URL`). |
| `make lint` | `go vet` (core and ee) and gofmt. `make lint-full` adds golangci-lint. |
| `make check` | What CI runs: lint, tests (Postgres too when docker is available) and the web checks. Must pass before every commit. |
| `make bench` | Engine benchmark on Postgres plus micro-benchmarks (see [Performance](performance.md)). |

Store code runs its tests against both SQLite and Postgres. Keep the binary CGO-free and do not add infrastructure beyond
Postgres and NATS without a plan entry.

## Plan workflow

Big changes start with a plan file in `plan/NNN.md` (the highest number is the latest). Keep it updated when decisions change,
and add a new file for a new direction instead of rewriting history. `AGENTS.md` holds standards, plans hold decisions.
The API is contract-first: change `api/openapi.yaml`, run `make gen`, then implement.

## Adding a Target adapter

Adapters live in `internal/targets`. An adapter implements `Adapter` (`Kind()`, `Invoke(ctx, example, InvokeOptions)`), is built once per
Target version and must be safe for concurrent use.

1. Define a config struct with JSON tags (strict decoding rejects unknown fields). Embed `HTTPCommon` for timeout, pool and TLS
   settings, or `ChatCommon` for LLM providers. Implement `Validate()`, which also compiles every template, so typos surface at save time.
2. Register it in `newConfig` (and `Kinds()`), and add it to `New`. For an LLM provider implement the small provider interface used by `newChatAdapter` instead of a full adapter.
3. Obtain HTTP clients from the shared factory (`newTransport`) so the SSRF guard applies. Never build your own `http.Client`.
4. Render templates with `CompileTemplate`; resolve secrets through the render environment so they are redacted in `Response.Request`.
5. Return errors classified as rate limited (with `Retry-After`), transient or permanent. Do not retry; the runner owns that.
6. Add the kind to the API schema (`TargetKind`, a config schema) and run `make gen`.
7. Ship unit tests (a fake HTTP server) and a docs entry in [targets.md](targets.md) and `internal/targets/README.md`.

## Adding an Evaluator

Evaluators live in `internal/evaluators`. Each kind registers itself from `init()`:

```go
type MyConfig struct {
    Threshold float64 `json:"threshold" title:"Threshold" desc:"Shown under the field." min:"0" max:"1" default:"0.5"`
}
func (c *MyConfig) Validate() error { return nil }   // check the config and fill in defaults

func init() {
    register(KindInfo{
        Kind: "my_kind", Name: "My evaluator", Category: CategoryDeterministic, Group: groupMatch, DefaultThreshold: 1,
        Description: "One or two sentences shown in the picker.",
    }, func(c *MyConfig, _ Deps) (Evaluator, error) {
        return free("my_kind", func(ctx context.Context, in Input) (evalcore.Score, error) {
            return boolScore(in.Response.Output != "", "output is empty"), nil
        }), nil
    })
}
```

- The config struct's tags (`title`, `desc`, `enum`, `default`, `min`, `max`, `required`, `ui`) generate the JSON Schema served at
  `GET /v1/evaluators/catalog`, which drives the UI form and [evaluators-reference.md](evaluators-reference.md).
- Model-based kinds embed the shared judge config, add a prompt template under `internal/evaluators/prompts/` and a prompt version.
- Declare what the kind needs with `Requires` (`expected`, `context`, `baseline`, `transcript`, `tool_calls`) and where it applies (`ApplyTo`).
- Ship unit tests and benchmarks for hot paths, then regenerate the reference: `go run ./tools/docsgen > docs/evaluators-reference.md`.

## Documentation

Docs are Markdown in `docs/`. Use the glossary terms exactly (Organization, Project, Target, Dataset, Example, Evaluator, Suite,
Gate, Run, Result, Trial, Trigger, Baseline, Comparison, Review Queue, Secret). Verify every command you document by running it.
`docs/evaluators-reference.md` is generated, so edit the Go code and re-run `tools/docsgen` rather than the file.

## Enterprise code

Only under `ee/`, behind `-tags ee`, registered through `internal/ext`. Core must build and run without it (`make build`) and must
never import `ee/`.
