# Gates

A **Gate** is a Suite-level pass criterion. A Run **succeeds** when it finishes and every enabled Gate passes (or the
Suite has no Gates); it is **failed** when a Gate fails. The Gate verdict drives `airval run --gate`'s exit code.

Gates are written as text, stored as an AST (so the visual builder and the API share one format) and always displayed as
canonical text.

```
mean(faithfulness) >= 0.85 and count(critical_fail) == 0
```

## Grammar

```
expr    = or
or      = and { ("or" | "||") and }
and     = unary { ("and" | "&&") unary }
unary   = ("not" | "!") unary | primary
primary = "(" expr ")" | compare
compare = operand cmp operand            cmp = >= | > | <= | < | == | = | !=
operand = number | call                  number may end in % (95% = 0.95)
call    = ident "(" [ ident { "," ident } ] ")"
```

Keywords are case-insensitive. Arguments are Evaluator names (or ids), built-in metrics, `all`, or a count category.
In the API, evaluators may be referenced by name; the server stores their ids.

## Functions

| Function | Arguments | Meaning |
|---|---|---|
| `mean`, `median`, `p50`, `p90`, `p95`, `p99`, `min`, `max`, `stddev` | evaluator or metric | Statistic of the score (or metric) over all Results. |
| `sum` | metric | Total, e.g. `sum(cost_usd)`. |
| `pass_rate` | evaluator or `all` | Share of Results that passed (0..1). `pass_rate(all)` counts Results where every Evaluator passed. |
| `count` | `fail`, `critical_fail`, `major_fail`, `minor_fail`, `error`, `pending`, `pass` or `results`, optionally followed by an evaluator | Number of Results in a category. |
| `regression_vs_baseline` | evaluator | Mean score change versus the Baseline Run over shared Examples. Negative means worse. |

Built-in metrics: `latency_ms`, `cost_usd`, `tokens`, `input_tokens`, `output_tokens`.

## Examples

```
pass_rate(all) >= 95%
mean(correctness) >= 0.8 and p95(latency_ms) < 2000
count(critical_fail) == 0
count(fail, has_citation) <= 3
sum(cost_usd) < 5
regression_vs_baseline(correctness) > -0.02
(mean(faithfulness) >= 0.85 or pass_rate(faithfulness) >= 0.9) and not count(error) > 5
```

## Semantics worth knowing

- **No data fails.** If a metric has no observations (for example every score errored), the comparison fails.
- **No Baseline skips.** A clause using `regression_vs_baseline` is skipped (counts as passing) when the Run has no Baseline.
- A Run where every Result errored is `errored`, not `succeeded`.
- Each Gate result records `passed`, the measured `value` and a `detail` explaining the failing clause, e.g.
  `pass_rate(has-answer) >= 0.5: got 0.3333`.

## Managing Gates

```sh
# Validate text without saving: returns the canonical text, referenced evaluators, or error + position + suggestion
curl -H "$H" -H 'content-type: application/json' "$B/v1/suites/$SID/gates:validate" \
  -d '{"text":"mean(faithfulness) >= 0.85"}'

curl -H "$H" -H 'content-type: application/json' "$B/v1/suites/$SID/gates" \
  -d '{"name":"quality","text":"mean(faithfulness) >= 0.85 and count(critical_fail) == 0"}'
```

Send `text` or `expr` (the AST), not both. A Suite can have several Gates, and each can be disabled with `enabled: false`.

## Early stop and failure budgets

A doomed CI Run should not burn minutes. Set a failure budget on the Suite:

```json
"failure_budget": { "critical_failures": 50 }
```

When more than 50 critical failures have accumulated, the Run is cancelled early, finishes as `failed` with
`summary.stopped_early = true` and a `stop_reason`, and its Gate verdict is a failure.

> The engine's budget tracker also supports `max_fails`, `max_errors` and `max_fail_rate` (with `min_results`, default 50),
> but only `critical_failures` is exposed in the Suite config today.

## CI exit codes

`airval run --gate` waits for the Run(s) and exits:

| Code | Meaning |
|---|---|
| `0` | Every Run succeeded and every Gate passed. |
| `1` | A Gate failed, a Run errored, was cancelled or stopped early, or the command itself failed (bad flags, server unreachable, timeout). |

See [CI](ci.md).
