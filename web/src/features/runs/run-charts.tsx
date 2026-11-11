import { useMemo } from 'react'
import { Heatmap, Histogram, HorizontalBars, LatencyDistribution } from '@/charts'
import { Select } from '@/components/ui/select'
import { formatCompact, formatDuration, formatUsd } from '@/lib/format'
import type { Evaluator, Run, RunAggregate } from './api'

/** Re-expand a server histogram into representative values (bin midpoints), capped for large runs. */
function expand(h: { edges: number[]; counts: number[] } | undefined, cap = 20_000): number[] {
  if (!h || !h.counts.length) return []
  const total = h.counts.reduce((a, b) => a + b, 0)
  const scale = total > cap ? cap / total : 1
  const out: number[] = []
  h.counts.forEach((c, i) => {
    const mid = ((h.edges[i] ?? 0) + (h.edges[i + 1] ?? h.edges[i] ?? 0)) / 2
    const n = Math.round(c * scale)
    for (let k = 0; k < n; k++) out.push(mid)
  })
  return out
}

export interface RunChartsProps {
  run: Run
  aggregates: RunAggregate[] | undefined
  baseline: RunAggregate[] | undefined
  evaluators: { id: string; name: string; threshold?: number }[]
  evaluatorById: Map<string, Evaluator>
  loading: boolean
  histEvaluator: string | undefined
  onHistEvaluator: (id: string) => void
}

export function RunCharts({ run, aggregates, baseline, evaluators, loading, histEvaluator, onHistEvaluator }: RunChartsProps) {
  const all = useMemo(() => (aggregates ?? []).filter((a) => a.slice_key === ''), [aggregates])
  const evId = histEvaluator && evaluators.some((e) => e.id === histEvaluator) ? histEvaluator : evaluators[0]?.id
  const ev = evaluators.find((e) => e.id === evId)

  const histSeries = useMemo(() => {
    const cur = all.find((a) => a.metric === 'score' && a.evaluator_id === evId)
    const base = (baseline ?? []).find((a) => a.slice_key === '' && a.metric === 'score' && a.evaluator_id === evId)
    const s = [{ name: 'This run', values: expand(cur?.histogram) }]
    if (base?.histogram) s.push({ name: 'Baseline', values: expand(base.histogram) })
    return s
  }, [all, baseline, evId])

  const latency = all.find((a) => a.metric === 'latency_ms')
  const latencyValues = useMemo(() => expand(latency?.histogram), [latency])

  const tags = useMemo(() => {
    const keys = new Set((aggregates ?? []).filter((a) => a.slice_key.startsWith('tag:')).map((a) => a.slice_key.slice(4)))
    return [...keys].sort()
  }, [aggregates])

  const passByTag = useMemo(
    () =>
      tags
        .map((t) => {
          const a = (aggregates ?? []).find((x) => x.metric === 'pass_rate' && x.slice_key === `tag:${t}` && !x.evaluator_id)
          return a?.pass_rate != null ? { label: t, value: a.pass_rate, n: a.n } : null
        })
        .filter((x): x is { label: string; value: number; n: number } => !!x),
    [aggregates, tags],
  )

  const heat = useMemo(
    () =>
      evaluators.map((e) =>
        tags.map((t) => (aggregates ?? []).find((x) => x.metric === 'score' && x.evaluator_id === e.id && x.slice_key === `tag:${t}`)?.mean ?? null),
      ),
    [aggregates, evaluators, tags],
  )

  const costs = useMemo(() => {
    const items: { label: string; value: number; n?: number }[] = []
    const target = all.find((a) => a.metric === 'cost_usd' && !a.evaluator_id)
    if (target?.sum != null) items.push({ label: 'Target', value: target.sum, n: target.n })
    for (const a of all.filter((x) => x.metric === 'cost_usd' && x.evaluator_id)) {
      const name = evaluators.find((e) => e.id === a.evaluator_id)?.name ?? 'Judge'
      if (a.sum) items.push({ label: `Judge · ${name}`, value: a.sum, n: a.n })
    }
    return items
  }, [all, evaluators])
  const maxCost = Math.max(0.0001, ...costs.map((c) => c.value))

  const empty = <p className="py-16 text-center text-sm text-text-muted">{loading ? 'Loading…' : 'No data yet'}</p>
  const noResults = !loading && run.completed === 0

  return (
    <div className="grid grid-cols-1 gap-3 lg:grid-cols-2">
      <Histogram
        title="Scores"
        description={ev?.threshold != null ? `Distribution · threshold ${ev.threshold.toFixed(2)}` : 'Distribution per result'}
        series={histSeries}
        threshold={ev?.threshold}
        loading={loading && !aggregates}
        empty={noResults || !histSeries[0]?.values.length ? empty : undefined}
        actions={
          evaluators.length > 0 && (
            <Select
              size="sm"
              aria-label="Evaluator"
              className="w-36"
              value={evId}
              onValueChange={onHistEvaluator}
              options={evaluators.map((e) => ({ value: e.id, label: e.name }))}
            />
          )
        }
        ariaLabel={`Score distribution for ${ev?.name ?? 'evaluator'}`}
      />
      <LatencyDistribution
        title="Latency"
        description={
          latency?.p50 != null
            ? `p50 ${formatDuration(latency.p50)} · p95 ${formatDuration(latency.p95 ?? 0)} · p99 ${formatDuration(latency.p99 ?? 0)}`
            : 'Target response time per result'
        }
        values={latencyValues}
        loading={loading && !aggregates}
        empty={noResults || !latencyValues.length ? empty : undefined}
        ariaLabel="Latency distribution with p50, p95 and p99 markers"
      />
      <HorizontalBars
        title="Pass rate by tag"
        description="Share of results passing all critical and major evaluators"
        items={passByTag}
        format="percent"
        measure="Pass rate"
        loading={loading && !aggregates}
        empty={!passByTag.length ? empty : undefined}
        ariaLabel="Pass rate by example tag"
      />
      <HorizontalBars
        title="Cost breakdown"
        description={`${formatUsd(run.cost_usd)} total · ${formatCompact(run.tokens_in)} tokens in · ${formatCompact(run.tokens_out)} tokens out`}
        items={costs}
        format="usd"
        max={maxCost}
        measure="Cost"
        sort="desc"
        loading={loading && !aggregates}
        empty={!costs.length ? empty : undefined}
        ariaLabel="Cost by source: target and LLM judges"
      />
      <Heatmap
        className="lg:col-span-2"
        title="Evaluator × tag"
        description="Mean score; darker is higher"
        rows={evaluators.map((e) => e.name)}
        columns={tags}
        values={heat}
        loading={loading && !aggregates}
        empty={!tags.length || !evaluators.length ? empty : undefined}
        ariaLabel="Mean score per evaluator and tag"
      />
    </div>
  )
}
