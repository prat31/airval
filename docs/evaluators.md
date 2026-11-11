# Evaluators

An **Evaluator** scores one output and returns a **Score**: `value` (0..1, optional), `label`, `passed` and a `reason`.
airval ships 38 kinds in three categories. The full list with every config field is in the
[Evaluator reference](evaluators-reference.md) (generated from the code, also served at `GET /v1/evaluators/catalog`).

| Category | Cost | Examples |
|---|---|---|
| Deterministic | free (microseconds, pure Go) | `equals`, `contains`, `regex`, `is_json`, `json_schema`, `jsonpath_equals`, `numeric_close`, `levenshtein`, `rouge_l`, `bleu`, `length`, `latency_ms`, `cost_usd`, `tokens`, `tool_called`, `tool_sequence`, `no_pii`, `refusal` |
| Model | one or more LLM or embedding calls | `llm_rubric`, `correctness`, `faithfulness`, `groundedness`, `answer_relevance`, `context_relevance`, `context_recall`, `hallucination`, `toxicity`, `bias`, `conciseness`, `helpfulness`, `conversation_goal_completion`, `conversation_coherence`, `tool_use_correctness`, `embedding_similarity`, `pairwise` |
| Human | a person's time | `human` |

## Using an Evaluator in a Suite

Create the Evaluator once in the Project (a name, a `kind`, a `config`), then attach it to Suites with Suite-level settings:

```json
{ "evaluator_id": "…", "weight": 1, "threshold": 0.8, "severity": "critical",
  "apply_to": "final", "fail_fast": false }
```

| Setting | Meaning |
|---|---|
| `threshold` | A Result passes this Evaluator when `value >= threshold`. Each kind has a default (listed in the reference). |
| `weight` | Weight of the Evaluator (default 1). |
| `severity` | `critical`, `major` or `minor`. Gates can count failures by severity (`count(critical_fail)`), and `failure_budget` can stop a Run early on critical failures. |
| `apply_to` | `final` (the final output), `per_turn`, `conversation` or `trace`. Which values a kind supports is in the reference. |
| `fail_fast` | See below. |

Evaluator configs are strict (unknown fields rejected). Preview an Evaluator on a pasted output, without a Run, with
`POST /v1/evaluators:preview`.

## Choosing evaluators

Start cheap and deterministic, then add model judges where meaning matters.

| You want to know | Use |
|---|---|
| Did it return the exact/expected answer? | `equals`, `contains` (uses the Example's `expected` when `values` is empty), `numeric_close` |
| Is the format right? | `is_json`, `json_schema`, `regex`, `starts_with`, `ends_with`, `length` |
| Is it close to a reference? | `levenshtein`, `rouge_l`, `bleu`, `embedding_similarity` |
| Is it within budget? | `latency_ms` (total or `ttft`), `cost_usd`, `tokens` |
| Did the agent call the right tools? | `tool_called`, `tool_sequence`, `tool_use_correctness` |
| Is it safe? | `no_pii`, `refusal`, `toxicity`, `bias` |
| Is a RAG answer grounded? | `faithfulness` / `groundedness`, `context_relevance`, `context_recall`, `hallucination` |
| Is it correct and useful, in free text? | `correctness` (vs expected), `answer_relevance`, `helpfulness`, `conciseness`, or `llm_rubric` for your own criteria |
| Did a conversation succeed? | `conversation_goal_completion`, `conversation_coherence` |
| Is B better than A? | `pairwise` |
| A person must decide | `human` |

## LLM judges and bring-your-own keys

Every model-based Evaluator calls an LLM (or an embedding model) you configure. Common judge fields:

| Field | Default | Description |
|---|---|---|
| `provider` | required | e.g. `openai`, `anthropic`. |
| `model` | required | The judge model. Stronger models grade more reliably. |
| `secret` | | Name of the **Secret** holding the provider API key. Keys are never stored in the Evaluator. |
| `temperature` | 0 | Keep 0 for repeatable grades. |
| `samples` | 1 (max 9) | N-sample voting: the median score (majority for verdicts). Reduces noise at proportional cost. |
| `max_tokens` | 1024 | |
| `logprobs` | `auto` | `auto` uses probability-weighted scores when the provider returns token probabilities; `two_pass` makes one extra short call to read them (G-Eval); `off` uses the integer score. |
| `no_cache` | false | Judge results are cached by default (see below). |

```json
{ "name": "faithful", "kind": "faithfulness",
  "config": { "provider": "openai", "model": "gpt-4.1", "secret": "OPENAI_KEY", "samples": 3 } }
```

`llm_rubric` is the do-it-yourself judge: a plain-language `rubric`, an optional `criteria` checklist, a score scale
(`scale_min`..`scale_max`, normalized to 0..1) and optional `anchors` that describe each score:

```json
{ "kind": "llm_rubric", "config": { "provider": "anthropic", "model": "claude-sonnet-4-5", "secret": "ANTHROPIC_KEY",
  "rubric": "Mentions the refund window and links to the policy page",
  "criteria": ["States 30 days", "Includes a link"], "scale_min": 1, "scale_max": 5,
  "anchors": { "5": "Complete and cites the policy", "1": "Wrong or missing" } } }
```

Judge prompts are versioned (`prompt_version` in the catalog) and visible to users (`prompt` in the catalog entry).
Judge calls run in a separate pool with their own concurrency (`workers.judge_concurrency`), so slow judges do not block
Target calls, and judge cost is tracked separately.

## Pairwise

`pairwise` compares each output with the **Baseline** Run's output for the same Example: win = 1, tie = 0.5, loss = 0.
By default it judges both orderings to cancel position bias (`no_swap: true` halves the cost). It needs a Baseline;
configure the Suite's `baseline` policy (`pinned`, `last_passing` or `none`) or pass `baseline_run_id` when creating a Run.
The default threshold 0.5 means "not worse than the baseline".

## Human review

A `human` Evaluator sends Results to a **Review Queue**. `mode` selects what: `all`, `failures` (only Results another
Evaluator failed or errored on) or `sample` (a deterministic share, `sample_rate`, so re-runs review the same
Examples). `scale` is `pass_fail`, `1_5` or `labels` (with `labels`), `instructions` are shown to reviewers and `queue`
names the queue. Reviewers grade with keyboard shortcuts in the Review screen; each grade is an **Annotation**.
`GET /v1/evaluators/{evaluator}/agreement` reports how well an Evaluator agrees with human Annotations (percent
agreement, Cohen's kappa, Pearson correlation, a confusion matrix), so you can calibrate judges.

## `fail_fast`

Mark a cheap deterministic Evaluator `fail_fast: true` (for example `is_json` or `regex`). If it fails for an Example,
the expensive model judges are skipped for that Example. This saves judge cost and time on outputs that are already
known to be bad.

## Severity

`severity` classifies how bad a failure is. Gates read it through `count(critical_fail)`, `count(major_fail)` and
`count(minor_fail)`, and the Suite's `failure_budget.critical_failures` stops a Run early once exceeded
(see [Gates](gates.md)). Use `critical` for safety and correctness invariants, `minor` for style.

## Caching

Judge and embedding results are cached by default, keyed on the judge model, prompt and output, so re-scoring a Run
costs nothing for unchanged judgments. Disable per Evaluator with `no_cache` or per Suite with `cache.judges: false`.
