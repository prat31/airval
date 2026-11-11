import { keepPreviousData, useMutation, useQueries, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useNavigate, useSearch } from '@tanstack/react-router'
import { Bookmark, GitCompare, ListChecks, RotateCw, TrendingDown, TrendingUp } from 'lucide-react'
import { useMemo, useState } from 'react'
import { Screen } from '@/app/screen'
import { useWorkspace } from '@/app/workspace'
import { Scatter, WinRateBar } from '@/charts'
import { Button } from '@/components/ui/button'
import { Card, CardHeader } from '@/components/ui/card'
import { Checkbox } from '@/components/ui/checkbox'
import { Dialog } from '@/components/ui/dialog'
import { EmptyState } from '@/components/ui/empty-state'
import { Input } from '@/components/ui/input'
import { PageHeader } from '@/components/ui/page-header'
import { ScoreChip } from '@/components/ui/score-chip'
import { Select } from '@/components/ui/select'
import { Skeleton, SkeletonText } from '@/components/ui/skeleton'
import { toast } from '@/components/ui/toast'
import { Tooltip } from '@/components/ui/tooltip'
import { api, ApiError, unwrap } from '@/lib/api/client'
import type { components } from '@/lib/api/schema'
import { cn } from '@/lib/cn'
import { formatDuration, formatPercent, formatRelative, formatUsd } from '@/lib/format'
import { csvArrays, indexById, runsQuery, suitesQuery, type Result, type Run } from '@/features/runs/api'
import { preview } from '@/features/runs/results-table'
import { PassRateBar, RunLetter, RunStatusPill, runTitle, RUN_LETTERS, shortId } from '@/features/runs/ui'
import type { CompareSearch } from './search'

type Report = components['schemas']['ComparisonReport']
type ScorecardRow = components['schemas']['ScorecardRow']
type ScorecardCell = components['schemas']['ScorecardCell']
type ExampleDelta = components['schemas']['ExampleDelta']

const LIMIT = 25

const comparisonQuery = (runs: string[], ev?: string, against?: string) => ({
  queryKey: ['comparisons', runs.join(','), ev ?? '', against ?? ''] as const,
  queryFn: () => unwrap(api.GET('/v1/comparisons', { params: { query: { runs, evaluator_id: ev, against, limit: LIMIT } }, querySerializer: csvArrays })),
  enabled: runs.length >= 2,
  placeholderData: keepPreviousData,
  retry: false,
})

/** Rows where a higher value is worse. */
const lowerIsBetter = (key: string) => /latency|cost|tokens/.test(key)

function rowValue(row: ScorecardRow, c: ScorecardCell): { value: number | null; delta: number | null; kind: 'score' | 'percent' | 'ms' | 'usd' } {
  if (row.key === 'pass_rate') return { value: c.pass_rate ?? null, delta: c.delta_pass_rate ?? null, kind: 'percent' }
  if (row.key.includes('latency')) return { value: c.mean ?? null, delta: c.delta_mean ?? null, kind: 'ms' }
  if (row.key.includes('cost')) return { value: c.mean ?? null, delta: c.delta_mean ?? null, kind: 'usd' }
  return { value: c.mean ?? null, delta: c.delta_mean ?? null, kind: 'score' }
}

const fmt = (v: number, kind: 'score' | 'percent' | 'ms' | 'usd') =>
  kind === 'percent' ? formatPercent(v) : kind === 'ms' ? formatDuration(v) : kind === 'usd' ? formatUsd(v) : v.toFixed(3)

function fmtDelta(d: number, kind: 'score' | 'percent' | 'ms' | 'usd', base: number | null) {
  const sign = d > 0 ? '+' : d < 0 ? '−' : '±'
  const a = Math.abs(d)
  if (kind === 'percent') return `${sign}${(a * 100).toFixed(1)} pts`
  if (kind === 'score') return `${sign}${a.toFixed(3)}`
  if (base) return `${sign}${((a / Math.abs(base)) * 100).toFixed(0)}%`
  return `${sign}${kind === 'ms' ? formatDuration(a) : formatUsd(a)}`
}

/** Diverging cell: blue = improvement, red = regression, intensity by magnitude. Arrows carry direction too. */
function DeltaCell({ row, cell, baseValue }: { row: ScorecardRow; cell: ScorecardCell; baseValue: number | null }) {
  const { value, delta, kind } = rowValue(row, cell)
  if (value == null) return <span className="text-text-muted">—</span>
  if (delta == null) return <span className="tabular text-sm text-text">{fmt(value, kind)}</span>
  const rel = kind === 'score' || kind === 'percent' ? Math.abs(delta) / 0.1 : baseValue ? Math.abs(delta / baseValue) / 0.3 : 0
  const better = lowerIsBetter(row.key) ? delta < 0 : delta > 0
  const flat = Math.abs(delta) < (kind === 'percent' || kind === 'score' ? 0.0005 : 1e-9)
  const color = better ? 'var(--div-neg)' : 'var(--div-pos)'
  const strength = flat ? 0 : Math.round(8 + Math.min(1, rel) * 22)
  const ci = cell.ci_low != null && cell.ci_high != null ? `95% CI ${fmtDelta(cell.ci_low, kind, null)} … ${fmtDelta(cell.ci_high, kind, null)}` : null
  const sig = cell.significant
  return (
    <Tooltip
      content={
        <span className="flex flex-col gap-0.5">
          <span>{better ? 'Improvement' : flat ? 'No change' : 'Regression'} vs baseline</span>
          {ci && <span className="text-text-muted">{ci}</span>}
          {cell.p_value != null && <span className="text-text-muted">p = {cell.p_value.toFixed(3)}</span>}
          {cell.paired != null && <span className="text-text-muted">{cell.paired.toLocaleString()} paired examples</span>}
        </span>
      }
    >
      <span
        className="flex w-full items-center justify-between gap-2 rounded-sm px-2 py-1"
        style={{ background: strength ? `color-mix(in srgb, ${color} ${strength}%, transparent)` : undefined }}
      >
        <span className="tabular text-sm text-text">{fmt(value, kind)}</span>
        <span className={cn('tabular inline-flex items-center gap-0.5 text-xs font-medium', flat ? 'text-text-muted' : 'text-text')}>
          {!flat && <span aria-hidden>{delta > 0 ? '▲' : '▼'}</span>}
          {fmtDelta(delta, kind, baseValue)}
          {sig === false && !flat && <span className="text-text-muted" title="Not statistically significant">~</span>}
        </span>
      </span>
    </Tooltip>
  )
}

function RunPicker({ open, onOpenChange, initial, onApply }: { open: boolean; onOpenChange: (o: boolean) => void; initial: string[]; onApply: (ids: string[]) => void }) {
  const { project } = useWorkspace()
  const runs = useQuery({ ...runsQuery({ project_id: project?.id ?? '', limit: 200, status: ['succeeded', 'failed', 'canceled'] }), enabled: open && !!project })
  const suites = useQuery(suitesQuery(project?.id ?? ''))
  const suiteById = useMemo(() => indexById(suites.data), [suites.data])
  const [sel, setSel] = useState<string[] | null>(null)
  const [q, setQ] = useState('')
  const selected = sel ?? initial
  const toggle = (id: string) => setSel(selected.includes(id) ? selected.filter((x) => x !== id) : selected.length >= 4 ? selected : [...selected, id])
  const list = (runs.data ?? []).filter((r) => {
    if (!q) return true
    const hay = `${r.name} ${suiteById.get(r.suite_id ?? '')?.name ?? ''} ${r.id} ${Object.values(r.labels).join(' ')}`.toLowerCase()
    return hay.includes(q.toLowerCase())
  })
  return (
    <Dialog
      open={open}
      onOpenChange={(o) => {
        if (!o) setSel(null)
        onOpenChange(o)
      }}
      size="lg"
      title="Choose runs to compare"
      description="Pick 2–4 runs. The first one you pick is the baseline (A)."
      footer={
        <>
          <span className="mr-auto text-xs text-text-muted">{selected.length}/4 selected</span>
          <Button variant="ghost" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button
            variant="primary"
            disabled={selected.length < 2}
            onClick={() => {
              onApply(selected)
              setSel(null)
              onOpenChange(false)
            }}
          >
            Compare {selected.length >= 2 ? selected.length : ''} runs
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-3 pb-2">
        <Input size="sm" value={q} onChange={(e) => setQ(e.target.value)} placeholder="Filter by name, suite, label…" aria-label="Filter runs" />
        {runs.isPending ? (
          <SkeletonText lines={6} />
        ) : !list.length ? (
          <p className="py-8 text-center text-sm text-text-muted">No finished runs.</p>
        ) : (
          <ul className="flex max-h-[44vh] flex-col overflow-y-auto rounded-md border border-border">
            {list.map((r) => {
              const idx = selected.indexOf(r.id)
              return (
                <li key={r.id} className="border-b border-border last:border-b-0">
                  <label className={cn('flex cursor-pointer items-center gap-3 px-3 py-2 hover:bg-surface-hover', idx >= 0 && 'bg-accent-subtle')}>
                    <Checkbox checked={idx >= 0} onCheckedChange={() => toggle(r.id)} disabled={idx < 0 && selected.length >= 4} aria-label={`Select ${runTitle(r)}`} />
                    <span className="w-5">{idx >= 0 && <RunLetter index={idx} />}</span>
                    <span className="min-w-0 flex-1">
                      <span className="block truncate text-sm text-text">{runTitle(r, suiteById.get(r.suite_id ?? '')?.name)}</span>
                      <span className="block truncate text-xs text-text-muted">
                        <span className="font-mono">{shortId(r.id)}</span> · {formatRelative(new Date(r.created_at))}
                      </span>
                    </span>
                    <span className="hidden w-36 sm:block">
                      <PassRateBar value={r.summary?.pass_rate} />
                    </span>
                    <RunStatusPill run={r} size="sm" iconOnly />
                  </label>
                </li>
              )
            })}
          </ul>
        )}
      </div>
    </Dialog>
  )
}

function DeltaList({ title, icon, items, total, onPick, active, tone }: {
  title: string
  icon: React.ReactNode
  items: ExampleDelta[]
  total?: number
  onPick: (id: string) => void
  active?: string
  tone: 'neg' | 'pos'
}) {
  return (
    <Card className="flex min-w-0 flex-col">
      <CardHeader title={<span className="flex items-center gap-2 [&_svg]:size-4 [&_svg]:text-text-muted">{icon}{title}</span>} description={`${(total ?? items.length).toLocaleString()} examples`} />
      {items.length === 0 ? (
        <p className="px-4 pb-6 pt-4 text-sm text-text-muted">None — nothing moved beyond the noise threshold.</p>
      ) : (
        <ul className="max-h-[360px] overflow-y-auto border-t border-border">
          {items.map((d) => (
            <li key={d.example_id} className="border-b border-border last:border-b-0">
              <button
                type="button"
                onClick={() => onPick(d.example_id)}
                className={cn('flex w-full items-center gap-3 px-4 py-2 text-left hover:bg-surface-hover focus-ring', active === d.example_id && 'bg-accent-subtle')}
              >
                <span className="min-w-0 flex-1 truncate text-sm text-text">{d.input_preview || shortId(d.example_id)}</span>
                <span className="tabular shrink-0 font-mono text-xs text-text-muted">
                  {d.baseline.toFixed(2)} → {d.candidate.toFixed(2)}
                </span>
                <span
                  className="tabular w-16 shrink-0 rounded-sm px-1.5 py-0.5 text-right font-mono text-xs text-text"
                  style={{ background: `color-mix(in srgb, var(${tone === 'neg' ? '--div-pos' : '--div-neg'}) ${Math.round(10 + Math.min(1, Math.abs(d.delta) / 0.5) * 22)}%, transparent)` }}
                >
                  {d.delta > 0 ? '▲' : '▼'} {Math.abs(d.delta).toFixed(2)}
                </span>
              </button>
            </li>
          ))}
        </ul>
      )}
    </Card>
  )
}

function AlignedOutputs({ exampleId, runs, evaluatorId, onClose }: { exampleId: string; runs: Run[]; evaluatorId?: string; onClose: () => void }) {
  const { org, project } = useWorkspace()
  const results = useQueries({
    queries: runs.map((r) => ({
      queryKey: ['runs', 'results', r.id, { example_id: exampleId, trial: 0 }] as const,
      queryFn: async () =>
        (await unwrap(api.GET('/v1/runs/{run}/results', { params: { path: { run: r.id }, query: { example_id: exampleId, trial: 0, limit: 1 } } }))).items[0] ?? null,
    })),
  })
  const first = results.find((q) => q.data)?.data as Result | null | undefined
  return (
    <Card className="flex flex-col">
      <CardHeader
        title="Side-by-side outputs"
        description={first ? preview(first.input) : 'Loading example…'}
        actions={
          <Button size="sm" variant="ghost" onClick={onClose}>
            Close
          </Button>
        }
      />
      <div className="flex flex-col gap-3 px-4 pb-4 pt-2">
        {first?.expected != null && (
          <div className="rounded-md border border-dashed border-border-strong bg-surface-2 px-3 py-2">
            <div className="text-xs font-medium text-text-muted">Expected</div>
            <p className="mt-1 whitespace-pre-wrap text-sm leading-6 text-text-2">{typeof first.expected === 'string' ? first.expected : JSON.stringify(first.expected)}</p>
          </div>
        )}
        <div className={cn('grid grid-cols-1 gap-3', runs.length === 2 ? 'md:grid-cols-2' : runs.length === 3 ? 'md:grid-cols-3' : 'md:grid-cols-2 xl:grid-cols-4')}>
          {runs.map((r, i) => {
            const q = results[i]!
            const res = q.data as Result | null | undefined
            const s = res?.scores.find((x) => x.evaluator_id === evaluatorId) ?? res?.scores[0]
            return (
              <div key={r.id} className="flex min-w-0 flex-col rounded-md border border-border">
                <div className="flex items-center justify-between gap-2 border-b border-border px-3 py-2">
                  <span className="flex min-w-0 items-center gap-2">
                    <RunLetter index={i} />
                    <span className="truncate text-xs text-text-2">{runTitle(r)}</span>
                  </span>
                  {s && <ScoreChip size="sm" value={s.value ?? null} passed={s.passed ?? undefined} />}
                </div>
                {q.isPending ? (
                  <SkeletonText lines={3} className="p-3" />
                ) : !res ? (
                  <p className="p-3 text-sm text-text-muted">Not in this run.</p>
                ) : (
                  <>
                    <p className="max-h-64 overflow-y-auto whitespace-pre-wrap p-3 text-sm leading-6 text-text">{res.error ? <span className="font-mono text-xs text-fail">{res.error}</span> : res.output || '—'}</p>
                    {project && (
                      <Link
                        to="/o/$org/p/$project/runs/$run"
                        params={{ org: org.slug, project: project.slug, run: r.id }}
                        search={{ result: res.id }}
                        className="mt-auto border-t border-border px-3 py-1.5 text-xs text-accent hover:underline focus-ring"
                      >
                        Open result →
                      </Link>
                    )}
                  </>
                )}
              </div>
            )
          })}
        </div>
      </div>
    </Card>
  )
}

export function CompareScreen() {
  const { org, project } = useWorkspace()
  const search = useSearch({ from: '/o/$org/p/$project/compare' })
  const navigate = useNavigate({ from: '/o/$org/p/$project/compare' })
  const qc = useQueryClient()
  const [pickerOpen, setPickerOpen] = useState(false)
  const runIds = useMemo(() => (search.runs ?? '').split(',').filter(Boolean).slice(0, 4), [search.runs])
  const setSearch = (patch: Partial<CompareSearch>) => navigate({ search: (s) => ({ ...s, ...patch }), replace: true, resetScroll: false })
  const [against, setAgainst] = useState<string | undefined>()

  const report = useQuery(comparisonQuery(runIds, search.ev, against))
  const derived = useDerivedWins(report.data)
  const suites = useQuery(suitesQuery(project?.id ?? ''))
  const suiteById = useMemo(() => indexById(suites.data), [suites.data])
  const save = useMutation({
    mutationFn: () => unwrap(api.POST('/v1/projects/{project}/comparisons', { params: { path: { project: project!.id } }, body: { run_ids: runIds } })),
    onSuccess: () => {
      toast.success('Comparison saved')
      void qc.invalidateQueries({ queryKey: ['comparisons'] })
    },
    onError: (e) => toast.error(e instanceof ApiError ? e.message : 'Could not save'),
  })

  const header = (
    <PageHeader
      icon={<GitCompare />}
      title="Compare runs"
      description="Score deltas, regressions and improvements against the baseline (A)."
      actions={
        <>
          {runIds.length >= 2 && (
            <Button icon={<Bookmark />} loading={save.isPending} onClick={() => save.mutate()}>
              Save
            </Button>
          )}
          <Button variant={runIds.length >= 2 ? 'secondary' : 'primary'} icon={<ListChecks />} onClick={() => setPickerOpen(true)}>
            {runIds.length >= 2 ? 'Change runs' : 'Select runs'}
          </Button>
        </>
      }
    />
  )
  const picker = <RunPicker open={pickerOpen} onOpenChange={setPickerOpen} initial={runIds} onApply={(ids) => setSearch({ runs: ids.join(','), example: undefined })} />

  if (runIds.length < 2) {
    return (
      <Screen width="wide">
        {header}
        <Card>
          <EmptyState
            illustration="chart"
            title="Choose runs to compare"
            description="Select 2–4 runs here, or tick runs in the Runs list and choose Compare. The first run is the baseline."
            action={
              <Button variant="primary" icon={<ListChecks />} onClick={() => setPickerOpen(true)}>
                Select runs
              </Button>
            }
            secondaryAction={
              project && (
                <Button variant="ghost" asChild>
                  <Link to="/o/$org/p/$project/runs" params={{ org: org.slug, project: project.slug }}>
                    Go to runs
                  </Link>
                </Button>
              )
            }
          />
        </Card>
        {picker}
      </Screen>
    )
  }

  if (report.error && !report.data) {
    const e = report.error
    return (
      <Screen width="wide">
        {header}
        <Card>
          <EmptyState
            illustration="error"
            title={e instanceof ApiError && e.status === 501 ? 'Comparisons aren’t available on this server yet' : 'Couldn’t compare these runs'}
            description={e.message}
            action={
              <Button icon={<RotateCw />} onClick={() => report.refetch()}>
                Try again
              </Button>
            }
          />
        </Card>
        {picker}
      </Screen>
    )
  }

  const r: Report | undefined = report.data
  const runs = r?.runs ?? []
  const againstId = r?.against_run_id ?? runs[1]?.id
  const ai = Math.max(1, runs.findIndex((x) => x.id === againstId))
  const evaluators = r?.evaluators ?? r?.scorecard.filter((x) => x.evaluator_id).map((x) => ({ id: x.evaluator_id!, name: x.label })) ?? []
  const evName = evaluators.find((e) => e.id === r?.evaluator_id)?.name ?? 'score'
  const pw = r?.pairwise?.find((p) => p.run_id === againstId)
  const wins = pw ?? derived
  const name = (i: number) => (runs[i] ? `${RUN_LETTERS[i]} · ${runTitle(runs[i]!, suiteById.get(runs[i]!.suite_id ?? '')?.name)}` : RUN_LETTERS[i]!)

  return (
    <Screen width="wide">
      {header}
      {/* Run chips */}
      {!r ? (
        <div className="grid grid-cols-1 gap-3 md:grid-cols-2 xl:grid-cols-4">
          {runIds.map((id) => (
            <Skeleton key={id} className="h-[76px]" />
          ))}
        </div>
      ) : (
        <div className={cn('grid grid-cols-1 gap-3 md:grid-cols-2', runs.length > 2 && 'xl:grid-cols-4')}>
          {runs.map((run, i) => (
            <Card key={run.id} className={cn('flex flex-col gap-2 px-4 py-3', i === ai && runs.length > 2 && 'ring-1 ring-accent')}>
              <div className="flex items-center justify-between gap-2">
                <span className="flex min-w-0 items-center gap-2">
                  <RunLetter index={i} />
                  {project ? (
                    <Link
                      to="/o/$org/p/$project/runs/$run"
                      params={{ org: org.slug, project: project.slug, run: run.id }}
                      search={{}}
                      className="truncate rounded-sm text-sm font-medium text-text hover:underline focus-ring"
                    >
                      {runTitle(run, suiteById.get(run.suite_id ?? '')?.name)}
                    </Link>
                  ) : null}
                </span>
                {i === 0 ? (
                  <span className="text-xs font-medium text-text-muted">Baseline</span>
                ) : runs.length > 2 ? (
                  <Button size="sm" variant={i === ai ? 'secondary' : 'ghost'} onClick={() => setAgainst(run.id)} aria-pressed={i === ai}>
                    {i === ai ? 'Focused' : 'Focus'}
                  </Button>
                ) : (
                  <span className="text-xs font-medium text-text-muted">Candidate</span>
                )}
              </div>
              <div className="flex items-center gap-3 text-xs text-text-muted">
                <RunStatusPill run={run} size="sm" />
                <span className="font-mono">{shortId(run.id)}</span>
                <span>{formatRelative(new Date(run.created_at))}</span>
                {run.labels.git_sha && <span className="font-mono">{run.labels.git_sha.slice(0, 7)}</span>}
              </div>
            </Card>
          ))}
        </div>
      )}

      {/* Scorecard */}
      <Card className="overflow-hidden">
        <CardHeader
          title="Scorecard"
          description="Δ vs baseline · blue = better, red = worse · hover for confidence intervals"
          actions={
            evaluators.length > 0 && (
              <Select
                size="sm"
                aria-label="Ranking evaluator"
                className="w-44"
                value={r?.evaluator_id}
                onValueChange={(v) => setSearch({ ev: v, example: undefined })}
                options={evaluators.map((e) => ({ value: e.id, label: e.name }))}
              />
            )
          }
        />
        <div className="overflow-x-auto">
          <table className="mt-2 w-full min-w-[560px] border-t border-border text-sm">
            <thead className="bg-surface-2">
              <tr className="border-b border-border">
                <th scope="col" className="h-9 px-4 text-left text-xs font-medium text-text-muted">
                  Metric
                </th>
                {(r ? runs : runIds).map((_, i) => (
                  <th key={i} scope="col" className="h-9 px-3 text-left text-xs font-medium text-text-muted">
                    <span className="flex items-center gap-1.5">
                      <RunLetter index={i} />
                      {i === 0 ? 'Baseline' : 'Δ vs A'}
                    </span>
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {!r
                ? Array.from({ length: 5 }, (_, k) => (
                    <tr key={k} className="border-b border-border last:border-b-0">
                      <td className="px-4 py-2.5">
                        <Skeleton className="h-3 w-24" />
                      </td>
                      {runIds.map((id) => (
                        <td key={id} className="px-3 py-2.5">
                          <Skeleton className="h-3 w-20" />
                        </td>
                      ))}
                    </tr>
                  ))
                : r.scorecard.map((row) => {
                    const base = row.cells[0] ? rowValue(row, row.cells[0]).value : null
                    return (
                      <tr key={row.key} className={cn('border-b border-border last:border-b-0', row.evaluator_id === r.evaluator_id && 'bg-accent-subtle/40')}>
                        <th scope="row" className="px-4 py-1.5 text-left font-medium text-text">
                          {row.label}
                        </th>
                        {row.cells.map((c) => (
                          <td key={c.run_id} className="px-2 py-1.5">
                            <DeltaCell row={row} cell={c} baseValue={base} />
                          </td>
                        ))}
                      </tr>
                    )
                  })}
            </tbody>
          </table>
        </div>
      </Card>

      {/* Win rate + scatter */}
      <div className="grid grid-cols-1 gap-3 lg:grid-cols-3">
        <Card className="flex flex-col gap-4 p-4">
          <div>
            <h3 className="text-base font-semibold text-text">Win rate</h3>
            <p className="mt-0.5 text-xs text-text-muted">
              Per example on {evName}, {RUN_LETTERS[ai]} vs A (ties within ±0.05)
            </p>
          </div>
          {!r ? (
            <Skeleton className="h-16" />
          ) : wins ? (
            <WinRateBar wins={wins.wins} ties={wins.ties} losses={wins.losses} labelA={RUN_LETTERS[ai]} labelB="A" />
          ) : (
            <p className="text-sm text-text-muted">Not enough paired examples.</p>
          )}
          {r && (
            <dl className="mt-auto grid grid-cols-2 gap-3 border-t border-border pt-3 text-xs">
              <div>
                <dt className="text-text-muted">Regressions</dt>
                <dd className="tabular text-base font-semibold text-text">{(r.regressions_total ?? r.regressions.length).toLocaleString()}</dd>
              </div>
              <div>
                <dt className="text-text-muted">Improvements</dt>
                <dd className="tabular text-base font-semibold text-text">{(r.improvements_total ?? r.improvements.length).toLocaleString()}</dd>
              </div>
            </dl>
          )}
        </Card>
        <Scatter
          className="lg:col-span-2"
          title={`Per-example ${evName}`}
          description="Points below the diagonal regressed. Click a point to see outputs."
          points={(r?.scatter ?? []).map((p) => ({ id: p.example_id, a: p.a, b: p.b }))}
          labelA={name(0)}
          labelB={name(ai)}
          loading={!r}
          height={300}
          empty={r && !r.scatter?.length ? <p className="py-20 text-center text-sm text-text-muted">No paired examples to plot.</p> : undefined}
          onPointClick={(p) => setSearch({ example: p.id })}
          ariaLabel="Scatter of per-example scores, baseline versus candidate"
        />
      </div>

      {search.example && r && <AlignedOutputs exampleId={search.example} runs={runs} evaluatorId={r.evaluator_id} onClose={() => setSearch({ example: undefined })} />}

      <div className="grid grid-cols-1 gap-3 lg:grid-cols-2">
        {!r ? (
          <>
            <Skeleton className="h-72" />
            <Skeleton className="h-72" />
          </>
        ) : (
          <>
            <DeltaList title="Regressions" icon={<TrendingDown />} tone="neg" items={r.regressions} total={r.regressions_total} active={search.example} onPick={(id) => setSearch({ example: id })} />
            <DeltaList title="Improvements" icon={<TrendingUp />} tone="pos" items={r.improvements} total={r.improvements_total} active={search.example} onPick={(id) => setSearch({ example: id })} />
          </>
        )}
      </div>
      {picker}
    </Screen>
  )
}

/** Fallback when the server returns no pairwise summary: derive from the scatter. */
function useDerivedWins(r: Report | undefined) {
  return useMemo(() => {
    if (!r?.scatter?.length) return null
    let wins = 0
    let ties = 0
    let losses = 0
    for (const p of r.scatter) {
      const d = p.b - p.a
      if (d > 0.05) wins++
      else if (d < -0.05) losses++
      else ties++
    }
    return { wins, ties, losses }
  }, [r])
}
