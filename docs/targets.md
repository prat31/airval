# Targets

A **Target** connects airval to the AI service under evaluation. Its `kind` selects an adapter and its `config` (JSON)
is versioned on every edit. Config is validated strictly: unknown fields are rejected with `400 validation_failed`
(`details.field` names the problem). Adapters classify failures (rate limited, transient, permanent) and never retry;
the engine owns retries, backoff and adaptive concurrency (see [Performance](performance.md)).

| Kind | Use it for | Required |
|---|---|---|
| `http` | Any REST endpoint | `url` |
| `agent` | An HTTP service whose response carries a trace or tool calls | `url` and `response.trace` and/or `response.tool_calls` |
| `openai` | OpenAI and compatible APIs (Azure OpenAI, Ollama, vLLM, LM Studio, Together, Groq...) | `model` |
| `anthropic` | Anthropic Messages API | `model` |
| `bedrock` | AWS Bedrock Converse API (SigV4, no streaming) | `model`, `region`, `access_key_id`, `secret_access_key` |
| `vertex` | Google Gemini via Vertex AI or the Gemini API | `model`, and `api_key` or `access_token` |

## Fields common to every kind

| Field | Default | Description |
|---|---|---|
| `timeout_ms` | 60000 | Bounds the whole call including streaming. |
| `max_response_bytes` | 10 MiB | Response body cap. |
| `max_concurrency` | 64 | Size of the per-host idle connection pool. Set it to the concurrency you intend to use. (Run concurrency itself is set on the Suite `limits`.) |
| `tls.ca_cert` | | Custom CA (PEM, a template: `{{ secret "CA" }}`). |
| `tls.client_cert`, `tls.client_key` | | mTLS client certificate and key. Must be set together. |
| `tls.server_name` | | SNI / verification name override. |
| `tls.insecure_skip_verify` | `false` | Skip certificate verification (testing only). |

## `http` and `agent`

| Field | Default | Description |
|---|---|---|
| `url` | required | Template. |
| `method` | `POST` | `GET`, `POST`, `PUT`, `PATCH` or `DELETE`. |
| `headers` | | Map of header name to template value. |
| `body` | | Request body template (usually JSON). |
| `expect_status` | any 2xx | List of accepted status codes. |
| `response` | | JSONPath mapping, below. |
| `stream` | | Streaming response handling, below. |
| `session` | | `{ "path": "<JSONPath>" }`: extract a session id for multi-turn. |

```json
{
  "method": "POST",
  "url": "{{ .vars.base_url }}/ask",
  "headers": { "Authorization": "Bearer {{ secret \"SVC_TOKEN\" }}", "Content-Type": "application/json" },
  "body": "{\"question\": {{ json .input.question }}, \"user\": \"airval\"}",
  "response": {
    "output": "$.answer",
    "context": "$.sources[*].text",
    "input_tokens": "$.usage.prompt_tokens",
    "output_tokens": "$.usage.completion_tokens",
    "cost_usd": "$.usage.cost"
  }
}
```

### Response mapping (JSONPath)

All fields are optional JSONPath expressions evaluated against the JSON response.

| Field | Description |
|---|---|
| `output` | Final text. Default: the whole body as text. |
| `output_json` | Path to a structured output (used by `json_schema`, `jsonpath_equals`, ...). |
| `context` | Path yielding strings or objects (objects become JSON text): the retrieved context for RAG evaluators. |
| `tool_calls` | `{ path, id, name, arguments, result }`. `path` selects the tool-call objects; the others are relative paths (defaults `$.id`, `$.name`, `$.arguments`, `$.result`). |
| `trace` | `{ path, kind, name, input, output, duration_ms }`. `path` selects step objects; the others are relative (defaults `$.kind`, `$.name`, `$.input`, `$.output`, `$.duration_ms`). |
| `input_tokens`, `output_tokens` | Token usage. |
| `cost_usd` | Cost of the call. |

Supported JSONPath subset: `$` root, `.name` / `['name']`, `.*` / `[*]`, `[0]` / `[-1]`, slices `[1:3]` `[:2]` `[-2:]`,
recursive descent `..name`, and filters `[?(@.k == 'v')]` with `==`, `!=`, `<`, `<=`, `>`, `>=` or bare existence `@.k`.
For streams, every event is mapped and the last non-empty value wins.

An `agent` target is an `http` target that must map a `trace` and/or `tool_calls`, which unlocks `tool_called`,
`tool_sequence` and `tool_use_correctness`.

### Streaming

```json
"stream": { "mode": "sse", "delta_path": "$.choices[0].delta.content", "done_sentinel": "[DONE]" }
```

| Field | Description |
|---|---|
| `mode` | `sse` or `ndjson`. Required. |
| `delta_path` | JSONPath to the text delta in each event. Required. The output is the accumulated text. |
| `done_sentinel` | SSE data that ends the stream (default `[DONE]`). |
| `event` | Only accept SSE events with this name. |

Streaming records time to first token (TTFT), usable by the `latency_ms` Evaluator with `metric: ttft`.

## `openai`

| Field | Default | Description |
|---|---|---|
| `model` | required | Model name. |
| `base_url` | `https://api.openai.com/v1` | Any compatible endpoint. |
| `path` | `/chat/completions` | |
| `api_key` | | Template, e.g. `{{ secret "OPENAI_KEY" }}`. |
| `auth_header` | `Authorization` (Bearer) | Use `api-key` for Azure. |
| `headers`, `query` | | Extra headers and query parameters (e.g. `api-version`). |
| `system_prompt` | | Template. |
| `user_template` | the lone string in `input`, else its JSON | Renders the user message for single-turn Examples. |
| `temperature` | provider default | 0 to 2. |
| `max_tokens` | | |
| `max_tokens_param` | `max_tokens` | Use `max_completion_tokens` for o-series models. |
| `tools`, `tool_choice` | | OpenAI tools array and choice. |
| `response_format` | | e.g. `{"type":"json_object"}`. |
| `extra_body` | | Merged into the request body last. |
| `stream` | `false` | Stream to record TTFT. |
| `pricing` | built-in table | `{ "input_per_1m": 2.5, "output_per_1m": 10 }` USD per million tokens, to override cost calculation. |

```json
{ "base_url": "https://api.openai.com/v1", "model": "gpt-4.1-mini", "api_key": "{{ secret \"OPENAI_KEY\" }}",
  "system_prompt": "You are a support assistant for {{ .input.product }}.",
  "user_template": "{{ .input.question }}", "temperature": 0 }
```

## `anthropic`

`model`, `base_url` (default `https://api.anthropic.com`), `api_key`, `anthropic_version` (default `2023-06-01`),
`anthropic_beta`, `headers`, `system_prompt`, `user_template`, `temperature`, `max_tokens`, `tools`, `tool_choice`,
`extra_body`, `stream`, `pricing`. They behave as for `openai`.

```json
{ "model": "claude-sonnet-4-5", "api_key": "{{ secret \"ANTHROPIC_KEY\" }}", "max_tokens": 1024 }
```

## `bedrock`

`model`, `region`, `access_key_id`, `secret_access_key` (templates, use secrets), optional `session_token`, `endpoint`
(default `https://bedrock-runtime.{region}.amazonaws.com`), `tool_config` (Converse `toolConfig`),
`additional_model_request_fields`, `extra_body`, plus `system_prompt`, `user_template`, `temperature`, `max_tokens`,
`pricing`. No streaming.

## `vertex`

`model`, and either `api_key` (Gemini API, sent as `x-goog-api-key`) or `access_token` (OAuth bearer, for Vertex AI).
With `project_id` it targets Vertex AI (`location`, default `us-central1`); without it the Gemini API. Also `base_url`,
`tools` (`[{"functionDeclarations": [...]}]`), `extra_body`, `system_prompt`, `user_template`, `temperature`,
`max_tokens`, `pricing`. No streaming.

## Templates

Strings marked "template" use Go `text/template`, compiled once per Target version. Missing keys render empty.

| Variable | Meaning |
|---|---|
| `.input.x` | A field of the Example's `input`. |
| `.example.id`, `.example.metadata`, `.example.expected`, `.example.tags`, `.example.context` | Other Example fields. |
| `.trial` | 0-based Trial number. |
| `.turn` | 0-based turn index (multi-turn). |
| `.session` | Session id extracted by `session.path` from the previous turn. |
| `.messages` | Conversation so far: `[{role, content}]`. |
| `.message` | The latest user message. |
| `.vars.NAME` | A run variable (see below). |

| Function | Example |
|---|---|
| `secret` | `{{ secret "OPENAI_KEY" }}` |
| `json` | `{{ json .input.question }}` renders a JSON-escaped value, quotes included |
| `default` | `{{ default "en" .input.lang }}` |
| `upper`, `lower`, `trim` | `{{ upper .input.code }}` |
| `join` | `{{ join ", " .example.tags }}` |
| `replace` | `{{ replace "-" "_" .input.id }}` |
| `contains` | `{{ if contains "refund" .input.question }}...{{ end }}` |

Rendered requests shown in test output and stored on Results have every resolved secret value replaced by `[REDACTED]`.

### Variables: `{{ .vars.NAME }}`

`{{ .vars.NAME }}` is substituted **before** the config is parsed, anywhere in it, including plain fields like `url` or
`base_url`. Values come from the Suite's `variables`, overridden by Run variables (`airval run --var NAME=value`, the
`variables` field of `POST /v1/runs`, or webhook `payload_variables`). Referencing an unset variable fails the Run.
Typical use: point one Target at a deploy preview URL per Run.

```json
{ "url": "{{ .vars.base_url }}/ask", "body": "{\"q\": {{ json .input.question }}}", "response": { "output": "$.answer" } }
```

## Secrets

Credentials are stored as **Secrets** (Organization-wide or Project-scoped), encrypted at rest and write-only through
the API. Reference them as `{{ secret "NAME" }}` in any templated field. Names match `^[A-Za-z_][A-Za-z0-9_]{0,127}$`.

```sh
curl -s -H "$H" -H 'content-type: application/json' $B/v1/orgs/$ORG/secrets \
  -d '{"name":"OPENAI_KEY","value":"sk-..."}'
```

## Multi-turn: scripts and simulation

An Example may carry a `conversation` instead of (or alongside) `input`. The adapter keeps state across turns: the
`messages` array, or a session id extracted with `session.path` and exposed as `.session`.

Scripted turns:

```json
{ "conversation": { "turns": [
  { "role": "system", "content": "The customer is on the Pro plan." },
  { "role": "user", "content": "I want a refund.", "expected": "asks for the order number" },
  { "role": "user", "content": "Order 1234." }
] } }
```

Roles: `user` is sent (one request per user turn), `system` is prepended, `assistant`/`tool` turns are expectations only.
`expected` on a user turn is a per-turn expectation. Usage, cost and latency are summed over turns, and each turn is
recorded as a trace step.

Simulated user:

```json
{ "conversation": { "simulation": {
  "persona": "An impatient customer", "goal": "Get a refund for order 1234",
  "first_message": "My order never arrived.", "max_turns": 8, "stop_condition": "The refund is confirmed"
} } }
```

An LLM plays the user until it emits `[END]` or `max_turns` is reached (default 5, maximum 50). Simulator cost is
reported in the turn detail, not in the Result cost.

> Note: the worker does not yet supply a simulator model, so simulated Examples currently fail with a permanent error
> ("requires a user-simulator LLM"). Scripted conversations work.

`conversation_goal_completion` and `conversation_coherence` score whole conversations; set an Evaluator's
`apply_to` to `per_turn` or `conversation` to score turns or the whole transcript.

## Network policy (SSRF guard)

Every Target connection, and webhooks and notifications, go through an SSRF guard that checks the **resolved**
address at dial time (so DNS rebinding and redirects cannot bypass it). Environment proxies are never used.

- Always denied: link-local and cloud metadata ranges (`169.254.0.0/16`, `fe80::/10`, `fd00:ec2::/32`,
  `100.100.100.200`, `192.0.0.192`).
- `security.deny_cidrs`: extra denied ranges (comma-separated).
- `security.allow_cidrs`: when set, only these ranges are reachable (allow-list mode). They never override the
  always-denied ranges.

Set them with `AIRVAL_SECURITY_ALLOW_CIDRS` / `AIRVAL_SECURITY_DENY_CIDRS`. Loopback and private addresses are
allowed by default, which is why a local mock service works in `airval dev`.

## Test connection

`POST /v1/targets/{target}:test` (saved Target) or `POST /v1/projects/{project}/targets:test` (an unsaved `kind` +
`config`) sends one sample Example (`example`, or the first of `dataset_id`; optional `variables`) and returns:
`ok`, `latency_ms`, the rendered `request` (secrets redacted), the `raw_response`, the mapped `response`, and on failure
`error` and `error_class`. The UI shows it as a playground, including how each JSONPath matched. Use it before saving.
