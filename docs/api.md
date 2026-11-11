# API

The public API is described by [`api/openapi.yaml`](../api/openapi.yaml) (OpenAPI 3.0.3). The web UI, the `airval run`
CLI and CI integrations use only this API; the Go server stubs and the TypeScript client are generated from it
(`make gen`). Paths live under `/v1`; operational endpoints `/healthz`, `/readyz` and `/metrics` are outside it.
`GET /v1/meta` (no auth) returns the version, edition (`core` or `ee`) and enabled features.

## Authentication

**API keys** (for scripts and CI). Create one in Settings or with `POST /v1/orgs/{org}/api-keys`
(`{"name":"ci","role":"editor"}`, optional `project_id` to scope it to one Project and `expires_at`). The key (`airv_...`) is
shown once; only its SHA-256 hash is stored. Send it as a bearer token:

```sh
curl -H "Authorization: Bearer airv_..." https://airval.example.com/v1/projects
```

**Browser sessions** use the `airval_session` cookie (`HttpOnly`, `SameSite=Lax`). Mutating requests made with a session
must also send the CSRF token (from `GET /v1/auth/me` or the `airval_csrf` cookie) in `X-CSRF-Token`. API keys need no CSRF
token. `POST /v1/auth/login` / `logout`, `GET /v1/auth/me`.

**Roles** `owner > admin > editor > viewer`: viewers read; editors create, edit and run; admins manage members, keys, Secrets
and notification channels; owners can delete the Organization. A key carries a role and can never exceed it.

Webhook triggers (`POST /hooks/{trigger}`) use an HMAC signature instead; see [Runs](runs.md#webhook).

## Conventions

- JSON bodies, UUIDv7 ids, RFC 3339 timestamps.
- Actions use a colon suffix on the resource: `POST /v1/runs/{run}:cancel`, `:rerun-failed`, `:rescore`,
  `POST /v1/targets/{target}:test`, `POST /v1/datasets/{dataset}:import`. (In shells write `${ID}:test`, since `$ID:t` is a
  zsh modifier.)
- Suites can be looked up by slug: `GET /v1/projects/{project}/suites/by-slug/{slug}`.

### Pagination

List endpoints are cursor-based: pass `limit` (1 to 1000, default 50) and `cursor` (the opaque `next_cursor` from the previous
page). `next_cursor` is `null` on the last page.

```sh
curl -H "$H" "$B/v1/runs?limit=100&suite_id=$SID&status=failed,errored"
curl -H "$H" "$B/v1/runs?limit=100&cursor=AaHJAI6N..."
```

Run lists filter by `project_id`, `suite_id`, `target_id`, `parent_run_id`, `status` (comma-separated), `trigger`, `label=key:value`
(repeatable), `from` and `to`. Result lists filter by `status`, `evaluator_id` with `passed` / `score_min` / `score_max`, `tag`, `q`
(substring search), `example_id`, `trial`, and `full=true` to include the raw request and response.

### Errors

Non-2xx responses carry:

```json
{ "code": "validation_failed", "message": "config does not match the suite schema: ...",
  "request_id": "host/abc-000013", "details": { "field": "config" } }
```

| HTTP | `code` |
|---|---|
| 400 | `bad_request`, `validation_failed` (for example an invalid Target config: `details.field` names it) |
| 401 | `unauthorized` |
| 403 | `forbidden` (role too low), `csrf` |
| 404 | `not_found` (also for resources you may not see) |
| 409 | `conflict` (duplicate name or slug), `setup_completed` |
| 422 | `validation_failed` on some endpoints |
| 429 | `rate_limited` |
| 501 | `not_implemented`, `not_configured` (enterprise features in the core build) |
| 500 | `internal` |

Include `request_id` when reporting problems; it appears in server logs.

## Live run events (SSE)

`GET /v1/runs/{run}/events` streams Server-Sent Events until the Run finishes. Each message:

```
id: 1042
event: run.progress
data: {"seq":1042,"type":"run.progress","run_id":"...","at":"2026-11-11T05:06:59Z","data":{"completed":120,"failed":3,"errored":0,"total":500,"cost_usd":0.41,"tokens_in":9800,"tokens_out":4100}}
```

`id` is the stream sequence. Reconnect with `Last-Event-ID: <id>` to resume exactly after it. A new connection starts with a
snapshot (`run.status` and `run.progress` read from the database, marked `"snapshot": true`, plus `run.finished` if the Run is
already over), then live events. A `retry: 3000` hint and a `: ping` comment every 15 seconds keep connections alive.
The stream ends after `run.finished`.

| `event` / `type` | `data` |
|---|---|
| `run.status` | `{"status": "...", "gate_passed": true\|false\|null}` (snapshot adds `total`) |
| `run.progress` | `{"completed", "failed", "errored", "total", "cost_usd", "tokens_in", "tokens_out"}`, once per ingest batch |
| `result` | **Batched**: `{"items": [...], "count": n}`. One event per ingest flush (every 250 ms or 200 rows) carrying up to 100 compact rows, not one event per Result. `count` is the number of Results in the batch. |
| `run.finished` | `{"status", "gate_passed", "summary": {...}, "error": "..."}` |

A compact result row in `items`:

```json
{ "id": "...", "example_id": "...", "trial": 0, "status": "failed", "latency_ms": 41.2, "cost_usd": 0.0002,
  "cached": false, "output": "first 280 characters...", "error": "optional",
  "scores": [ { "evaluator_id": "...", "value": 0.0, "passed": false, "label": "" } ] }
```

Because `items` is capped per batch, treat `result` events as a live preview and fetch `GET /v1/runs/{run}/results` for the
complete set. Browser example:

```js
const es = new EventSource(`/v1/runs/${runId}/events`);   // cookie session; for API keys use fetch + a stream reader
es.addEventListener("result", (e) => addRows(JSON.parse(e.data).data.items));
es.addEventListener("run.finished", () => es.close());
```

Note `EventSource` cannot set headers. With an API key, read the stream with `fetch` (the `airval run` CLI does this).

## Resource map

| Area | Endpoints |
|---|---|
| Auth and orgs | `/v1/setup`, `/v1/auth/*`, `/v1/orgs`, `/v1/orgs/{org}/members`, `/api-keys`, `/secrets`, `/notification-channels`, `/audit-log` |
| Projects | `/v1/projects`, `/v1/projects/{p}/overview`, `/trends` |
| Targets | `/v1/projects/{p}/targets`, `/v1/targets/{t}`, `/versions`, `:test` |
| Datasets | `/v1/projects/{p}/datasets`, `/v1/datasets/{d}`, `/examples`, `:import`, `:from-results`, `/versions`, `/versions/{n}/export` |
| Evaluators | `/v1/projects/{p}/evaluators`, `/v1/evaluators/catalog`, `:preview`, `/{e}/agreement` |
| Suites | `/v1/projects/{p}/suites`, `/v1/suites/{s}`, `/gates`, `/gates:validate`, `/triggers` |
| Runs | `/v1/runs`, `/v1/runs/{r}`, `/results`, `/aggregates`, `/events`, `:cancel`, `:rerun-failed`, `:rescore` |
| Comparisons | `/v1/comparisons?runs=a,b`, `/v1/projects/{p}/comparisons` |
| Review | `/v1/projects/{p}/review-queues`, `/v1/review-queues/{q}/items`, `/v1/review-items/{i}`, `/v1/annotations` |
| Hooks | `POST /hooks/{trigger}` |

The OpenAPI file is the authority for request and response schemas.
