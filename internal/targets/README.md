# internal/targets

Target adapters: `Adapter.Invoke(ctx, example, InvokeOptions) (evalcore.Response, error)`.
Build with `targets.New(kind, rawConfig, deps)`; validate config with `targets.ValidateConfig` / `ParseConfig`
(strict: unknown fields are rejected). `targets.TestConnection` powers the UI playground.
Adapters classify errors (`ErrRateLimited{RetryAfter}`, `ErrTransient`, `ErrPermanent`; `Classify`) and never retry.

## Kinds and config

| kind | required | notable fields |
|---|---|---|
| `http` | `url` | `method` (default POST), `headers`, `body` (templates), `expect_status`, `response` (JSONPath: `output`, `output_json`, `context`, `tool_calls{path,id,name,arguments,result}`, `trace{path,kind,name,input,output,duration_ms}`, `input_tokens`, `output_tokens`, `cost_usd`), `stream{mode: sse\|ndjson, delta_path, done_sentinel, event}`, `session{path}` |
| `agent` | `url` + `response.trace` or `response.tool_calls` | same as `http` |
| `openai` | `model` | `base_url`, `path`, `api_key`, `auth_header`, `headers`, `query`, `system_prompt`, `user_template`, `temperature`, `max_tokens`, `max_tokens_param`, `tools`, `tool_choice`, `response_format`, `extra_body`, `stream`, `pricing` |
| `anthropic` | `model` | `base_url`, `api_key`, `anthropic_version`, `anthropic_beta`, `system_prompt`, `user_template`, `temperature`, `max_tokens`, `tools`, `tool_choice`, `extra_body`, `stream`, `pricing` |
| `bedrock` | `model`, `region`, `access_key_id`, `secret_access_key` | `session_token`, `endpoint`, `tool_config`, `additional_model_request_fields`, `extra_body` (Converse API, SigV4; no streaming) |
| `vertex` | `model`, `api_key` or `access_token` | `project_id` (Vertex AI) / none (Gemini API), `location`, `base_url`, `tools`, `extra_body` |

Every kind also accepts `timeout_ms`, `max_response_bytes`, `max_concurrency` (idle pool size) and `tls{ca_cert,client_cert,client_key,server_name,insecure_skip_verify}` (PEMs are templates, e.g. `{{ secret "CERT" }}`).

## Templates

`text/template`, compiled once per target version. Variables: `.input.x`, `.example.{id,metadata,expected,tags,context}`,
`.trial`, `.turn`, `.session`, `.messages` (`[{role,content}]`), `.message` (latest user message).
Functions: `secret`, `json`, `default`, `upper`, `lower`, `trim`, `join`, `replace`, `contains`.
`Response.Request` holds the rendered request with every resolved secret value replaced by `[REDACTED]`.

## Multi-turn

`Example.Conversation` drives one request per user turn. Scripted turns use roles `user` (sent), `system` (prepended)
and `assistant` (expectation only, not sent). Simulated conversations need `InvokeOptions.LLM`; the simulator ends the
conversation with `[END]` or at `max_turns`. Per-turn details are `TraceStep{Kind:"turn"}` (JSON `TurnDetail`); usage, cost and
latency are summed over turns (simulator cost is reported in the turn detail only).

## Security

All connections go through the SSRF guard (`NetworkPolicy` on `NewSSRFGuard`): cloud metadata and link-local ranges are
always denied; optional allow/deny CIDRs and `block_private`. Checks run on the resolved address at dial time.
Environment proxies are never used.

`tools/mocktarget` provides a deterministic fake AI service for tests and demos.
