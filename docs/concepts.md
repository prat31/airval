# Concepts

```
Organization ── members (owner | admin | editor | viewer), API keys, Secrets, notification channels
 └─ Project  (one AI service under evaluation)
     ├─ Target ─────── versioned connection config            ┐
     ├─ Dataset ────── Examples ── immutable Dataset Versions ├─▶ Suite ──▶ Run ──▶ Results (× Trials)
     ├─ Evaluator ──── scores an output (Score 0..1)          ┘     │  ▲          └─ Scores
     └─ Review Queue ◀── human Annotations                        Gates  Triggers (manual · schedule · webhook · API/CI)
                                                                  │
                      Baseline ◀── a Run   ──▶  Comparison (2-4 Runs side by side)
```

| Term | What it is |
|---|---|
| **Organization** | The tenant boundary. Has members with roles `owner > admin > editor > viewer`. Viewers read, editors create/edit/run, admins manage members, keys, Secrets and notification channels, owners can delete the Organization. |
| **Project** | One AI service under evaluation. Owns Targets, Datasets, Evaluators, Suites and Runs. Project settings include `retention_days` and a default Target. |
| **Target** | A connection to the AI service: an HTTP endpoint, an LLM provider model or an agent. A Project can have several (prod vs staging, model A vs B). Every edit creates a new immutable Target version, and a Run records the version it used. See [Targets](targets.md). |
| **Dataset** | A collection of **Examples**. Edits accumulate in a draft; **Publish** freezes it into an immutable **Dataset Version**. See [Datasets](datasets.md). |
| **Example** | One row: `input` (an object of template variables, e.g. `{"question": "..."}`) or a multi-turn `conversation`, plus optional `expected`, `context`, `metadata` and `tags`. |
| **Evaluator** | A reusable function that scores an output and yields a **Score**: a number in 0..1, a label and/or pass/fail plus a reason. Kinds: deterministic, model (LLM judge, embeddings) and human. See [Evaluators](evaluators.md). |
| **Suite** | A reusable evaluation definition: Target(s), Dataset version(s), Evaluators with weight, threshold and severity, `trials`, sampling, limits, cache policy, baseline policy, variables and a failure budget. |
| **Gate** | A Suite-level pass criterion such as `mean(faithfulness) >= 0.85 and count(critical_fail) == 0`. It decides whether a Run passes, and the CI exit code. See [Gates](gates.md). |
| **Run** | One execution of a Suite against one Target (selecting several Targets creates one Run each). Status: `queued`, `running`, `succeeded` (finished, Gates passed or none), `failed` (a Gate failed), `errored`, `canceled`. |
| **Result / Trial** | The output and Scores for one Example in a Run. With `trials` > 1 each Example is repeated, giving one Result per Trial, which exposes nondeterminism. |
| **Trigger** | What starts a Run: `manual`, `schedule` (cron), `webhook` (HMAC-signed) or `api` (API key or `airval run`). See [Runs](runs.md). |
| **Baseline** | The Run a new Run is compared against: a pinned Run, the last passing Run, or none. Gates can use `regression_vs_baseline(...)`, and the `pairwise` Evaluator judges against it. |
| **Comparison** | A side-by-side diff of 2 to 4 Runs: scorecard deltas with confidence intervals, regressions and improvements per Example. |
| **Review Queue** | A queue of Results waiting for human **Annotations**, filled by `human` Evaluators. Annotations can calibrate LLM judges (agreement, Cohen's kappa). |
| **Secret** | An encrypted credential (AES-256-GCM) referenced as `{{ secret "OPENAI_KEY" }}` by Targets, judges and notification channels. Write-only through the API. |

## How a Run flows

1. **Plan.** The server resolves the Suite (Target version, Dataset version, Evaluators, Gates) into an immutable snapshot, splits Examples into shards and publishes them to the queue.
2. **Execute.** Workers pull shards. Each Example x Trial: render request, call the Target, run deterministic Evaluators, then LLM judges, then persist.
3. **Ingest.** Results are written in batches; progress and compact result rows stream to the UI over SSE.
4. **Finalize.** Aggregates (means, percentiles, pass rates, per-tag slices, cost) are computed, Gates are evaluated, notifications and webhooks fire.

Measuring and deciding are separate: Evaluators produce Scores, Gates turn Scores into a verdict.

Avoid the words "test", "experiment", "job" or "case" for these concepts; they are not airval's vocabulary.
