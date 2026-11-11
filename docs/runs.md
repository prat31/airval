# Runs

A **Run** is one execution of a Suite against one **Target**. Selecting several Targets (in the Suite, or with
`target_ids` when starting a Run) creates one Run each, which gives an automatic **Comparison**.

Status: `queued` -> `running` -> `succeeded` | `failed` | `errored` | `canceled`.

## Triggers

| Trigger | Starts a Run when | Create with |
|---|---|---|
| `manual` | someone presses **Run** in the UI | the UI |
| `schedule` | a cron expression fires | `POST /v1/suites/{suite}/triggers` |
| `webhook` | a signed HTTP request arrives | same |
| `api` | `POST /v1/runs` or `airval run` | an API key |

### Manual and API

```sh
curl -H "$H" -H 'content-type: application/json' $B/v1/runs -d '{
  "suite_id": "…", "name": "nightly", "labels": {"git_sha": "abc123"},
  "sampling": {"mode": "random", "percent": 10}, "trials": 3,
  "variables": {"base_url": "https://preview-42.example.com"} }'      # 201 {"runs":[...]}
```

All fields except `suite_id` are optional overrides: `target_ids`, `name`, `labels`, `sampling`, `trials` (1 to 10),
`baseline_run_id`, `variables`, `trigger_ref` (e.g. the CI job URL).

### Schedule

```sh
curl -H "$H" -H 'content-type: application/json' $B/v1/suites/$SID/triggers -d '{
  "name": "nightly", "kind": "schedule",
  "config": { "cron": "0 2 * * *", "timezone": "Europe/Berlin", "sampling": {"mode":"none"}, "labels": {"source":"cron"} } }'
```

5-field cron (and descriptors like `@daily`, `@every 1h`). The default timezone is UTC. The scheduler runs on every
server replica but only a leader (elected through a NATS KV lease) polls, and each fire is claimed atomically, so a
trigger fires exactly once. If the server was down, an overdue trigger fires once and its next time is computed from now;
missed occurrences are not replayed. The trigger can override `target_ids`, `sampling`, `trials`, `labels` and `variables`.

### Webhook

Create a `webhook` trigger. The response contains `webhook_url` and `webhook_secret` (`whsec_...`, **shown once**; rotate with
`POST /v1/suites/{suite}/triggers/{trigger}:rotate-secret`).

```json
{ "name": "deploy-hook", "kind": "webhook",
  "config": { "payload_variables": {"base_url": "$.deployment_url"},
              "payload_labels": {"git_sha": "$.sha"}, "labels": {"source": "deploy"} } }
```

- `payload_variables` maps run variable names to JSONPath expressions into the payload (so `{{ .vars.base_url }}` in a Target points at the deploy preview).
- `payload_labels` maps label names to JSONPath expressions. `git_sha`, `branch` and `url` are detected automatically from GitHub, GitLab and Vercel deploy payloads when not mapped.
- The Run gets the labels `trigger=<name>` plus the configured and extracted labels. Payloads are limited to 1 MiB.

**Signing.** Send `POST /hooks/{trigger_id}` with two headers:

- `X-Airval-Timestamp`: unix seconds.
- `X-Airval-Signature`: `sha256=` + hex `HMAC-SHA256(secret, timestamp + "." + body)`.

Requests more than 5 minutes off the server clock are rejected. A valid request returns `202` with the created Runs. The three
snippets below produce identical signatures.

**bash**

```sh
BODY='{"sha":"deadbeef","deployment_url":"https://preview-123.example.com"}'
TS=$(date +%s)
SIG=$(printf '%s.%s' "$TS" "$BODY" | openssl dgst -sha256 -hmac "$SECRET" -hex | sed 's/^.* //')
curl -X POST "$AIRVAL_URL/hooks/$TRIGGER_ID" -H 'Content-Type: application/json' \
  -H "X-Airval-Timestamp: $TS" -H "X-Airval-Signature: sha256=$SIG" -d "$BODY"
```

**Node.js**

```js
import crypto from "node:crypto";
const body = JSON.stringify({ sha: "deadbeef", deployment_url: "https://preview-123.example.com" });
const ts = Math.floor(Date.now() / 1000).toString();
const sig = crypto.createHmac("sha256", process.env.SECRET).update(`${ts}.${body}`).digest("hex");
await fetch(`${process.env.AIRVAL_URL}/hooks/${process.env.TRIGGER_ID}`, {
  method: "POST",
  headers: { "Content-Type": "application/json", "X-Airval-Timestamp": ts, "X-Airval-Signature": `sha256=${sig}` },
  body,
});
```

**Python**

```python
import hmac, hashlib, json, os, time, urllib.request

body = json.dumps({"sha": "deadbeef", "deployment_url": "https://preview-123.example.com"}).encode()
ts = str(int(time.time()))
sig = hmac.new(os.environ["SECRET"].encode(), ts.encode() + b"." + body, hashlib.sha256).hexdigest()
req = urllib.request.Request(f"{os.environ['AIRVAL_URL']}/hooks/{os.environ['TRIGGER_ID']}", data=body, method="POST",
    headers={"Content-Type": "application/json", "X-Airval-Timestamp": ts, "X-Airval-Signature": f"sha256={sig}"})
print(urllib.request.urlopen(req).read().decode())
```

The signature covers the exact bytes sent, so sign the same string you transmit. Note that `webhook_url` is built from
`server.base_url`; set it to the public URL.

## Sampling

Run on a subset for fast PR checks and run everything nightly.

| `sampling` | Meaning |
|---|---|
| `{"mode":"none"}` | All Examples (default). |
| `{"mode":"random","n":50}` or `{"mode":"random","percent":10}` | Random sample. |
| `{"mode":"stratified","percent":10}` | A sample per tag, so every tag stays represented. |
| `"seed": 42` | Deterministic sampling. |

CLI: `--sample 50`, `--sample 10%`, `--sample-mode stratified`.

## Trials

`trials` (1 to 10) repeats each Example, producing one Result per Trial. Use it to measure nondeterminism: variance of
scores and pass rates across Trials. Set it on the Suite or per Run (`--trials 3`).

## Cache

| Policy | Default | Effect |
|---|---|---|
| `cache.target` | `false` | Cache Target responses, keyed on the Target version, Example revision and Trial. A re-run of an unchanged Example costs no Target call. |
| `cache.judges` | `true` | Cache LLM-judge and embedding results (model, prompt, output). |

The cache is shared across workers (an in-process LRU in front of a database table). Cached Results are marked `cached`
and the Run summary reports a `cache_hit_ratio`. Turn target caching off when you want to observe live behavior.

## Re-run failed, re-score

- **Re-run failed**: `POST /v1/runs/{run}:rerun-failed` creates a child Run (`parent_run_id`) covering only the failed and errored Results.
- **Re-score**: `POST /v1/runs/{run}:rescore` re-evaluates the stored outputs with the Suite's current Evaluators (or `evaluator_ids`), **without calling the Target**. Change a rubric or add an Evaluator and re-grade old Runs for the price of the judge calls only (and zero with a warm judge cache).

## Baselines and comparison

A Suite's `baseline` policy is `{"policy": "last_passing"}` (default; optionally on another `target_id`), `{"policy":"pinned","run_id":"…"}` or `none`. You can also pass `baseline_run_id` when creating a Run, and set the Suite's pinned Baseline.
The Baseline feeds `regression_vs_baseline(...)` in [Gates](gates.md) and the `pairwise` Evaluator.

`GET /v1/comparisons?runs=a,b,c` (2 to 4 Runs) returns a scorecard of means and pass rates with deltas versus the first
Run, a paired bootstrap confidence interval, p-value and a `significant` flag, plus the Examples that regressed or
improved most. Save a view with `POST /v1/comparisons`. In the UI, select Runs in the Runs list and press **Compare**.

## Cancel, notifications

`POST /v1/runs/{run}:cancel` cancels; workers check between Examples. Notification channels (Slack incoming webhook,
signed generic webhook, SMTP email; set per Organization) fire on `run.finished`, `run.failed`, `run.errored` and
`gate.failed`. Slack and webhook payloads include the Gate verdicts and a link to the Run. Generic webhooks are signed
with the same `X-Airval-Timestamp` / `X-Airval-Signature` scheme using the channel's `signing_secret`.
