# Performance

Fast Runs are a product feature. The engine is designed so that wall-clock time is dominated by your AI service and
your rate limits, not by airval. The acceptance targets (`make bench`) are at least 5,000 Examples per minute end to end
with a 10 ms mock Target and three deterministic Evaluators on Postgres, and under 2 ms p95 of engine overhead per Example.

## How the engine achieves speed

**Sharding.** The server splits a Run into shards of 25 to 100 Examples (about 64 shards per Run) and publishes them to a
NATS JetStream work queue. Any worker takes any shard, so a Run spreads across all workers, and a crashed worker's shard
is redelivered. Each worker pulls only as many shards as its concurrency can use.

**Bounded concurrency.** Each worker runs up to `workers.concurrency` (default 256) Examples at once through a pipeline:
render request, call the Target, deterministic Evaluators, LLM judges, persist. Target calls and judge calls have
separate concurrency limits, so a slow judge never blocks Target calls.

**Adaptive concurrency (AIMD).** Per Target, the limiter keeps a window that grows by 1 per window of successes and
halves (at most once per observed round trip) on 429, 5xx or timeouts. A `Retry-After` header pauses every caller until
it expires. Throughput therefore settles near the service's real capacity without a stream of errors. The Run summary
reports `rate_limited_ratio` so you can see how much time was spent throttled. Disable with `limits.adaptive: false`.

**Rate limits across workers.** Optional `rpm` and `tpm` limits on the Suite are enforced by distributed token buckets in
NATS KV, leased locally in about one-second slices, so many workers share one limit without hitting KV per request.

**Retries.** Rate-limited and transient errors are retried up to 4 attempts with full-jitter exponential backoff
(200 ms base, 30 s cap), never shorter than `Retry-After`. Permanent errors (bad request, auth) fail fast.

**Response cache.** Target responses (opt-in: `cache.target`) are keyed on the Target version, Example revision and
Trial; judge and embedding results (default on) on the judge model, prompt and output. Re-scoring a Run costs zero
Target calls. A two-level cache (in-process LRU, then a shared database table with write-behind) keeps lookups off the hot path.

**Short-circuit and early stop.** `fail_fast` Evaluators skip expensive judges for Examples that already failed a cheap
check; a `failure_budget` cancels doomed Runs. See [Evaluators](evaluators.md) and [Gates](gates.md).

**Sampling.** A 10% stratified sample for pull requests, the full dataset nightly. See [Runs](runs.md#sampling).

**Batched writes.** Results are buffered and flushed every 250 ms or 200 rows: a multi-row insert / `COPY` on Postgres
(with several concurrent writers) or one batched transaction on SQLite. Outputs over 8 KiB are stored zstd-compressed.
Progress counters are updated once per batch, never per row.

**Connection reuse.** One HTTP client per Target host with keep-alive and HTTP/2 where offered, and request bodies come
from templates compiled once per Target version.

**Streaming.** Results reach the UI live over SSE in batched `result` events, and time to first token is recorded for
streaming Targets.

## Tuning knobs

| Knob | Where | Default | Guidance |
|---|---|---|---|
| `workers.concurrency` | config / `AIRVAL_WORKERS_CONCURRENCY` / Helm `worker.concurrency` | 256 | Examples in flight per worker. Raise it for slow Targets (high latency, high limits); lower it if a worker is CPU-bound. |
| `workers.judge_concurrency` | config | 32 | LLM-judge calls in flight per worker. Raise it if judges are the bottleneck and your provider allows it. |
| Worker replicas | compose `--scale worker=N`, Helm `worker.replicaCount`, HPA or KEDA | 2 | Total throughput scales with replicas until the Target or database saturates. |
| Suite `limits.max_concurrency` | Suite config | | Upper bound of the AIMD window for the Target. Set it to what the service can take. |
| Suite `limits.rpm`, `limits.tpm` | Suite config | 0 (unlimited) | Provider quotas, shared across all workers. |
| Suite `limits.adaptive` | Suite config | `true` | Turn AIMD off to pin concurrency. |
| Suite `cache.target` / `cache.judges` | Suite config | `false` / `true` | See above. |
| Suite `trials` | Suite config | 1 | Multiplies calls. |
| `sampling` | Suite or Run | none | Fewer Examples. |
| Judge `samples` | Evaluator config | 1 | N-sample voting multiplies judge calls. |
| Target `max_concurrency` | Target config | 64 | Idle connection pool size for the host. |
| Target `timeout_ms` | Target config | 60000 | Bound slow calls so they do not hold slots. |
| `database.max_open_conns` | config | driver default | Raise for many concurrent writers on Postgres. |

Diagnosing a slow Run: check the Run summary for `rate_limited_ratio` (the Target is the limit; raise quotas or accept it),
`cache_hit_ratio`, `latency_p95_ms` and `throughput_per_min`; check `airval_queue_pending_messages` (workers are the limit
when it stays high) and `airval_db_flush_duration_seconds` (the database is the limit when it grows).

## Benchmarks

```sh
make bench                    # engine benchmark on Postgres plus micro-benchmarks (needs docker)
AIRVAL_BENCH_EXAMPLES=20000 make bench
BENCH_OUT=bench.txt make bench
```

`tools/mocktarget` is a configurable fake service (latency, jitter, error rate, 429 rate) for your own experiments:
`go run ./tools/mocktarget -addr :9090 -latency 50ms -rate-limit-rate 0.05`.
