import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useNavigate, useParams, useSearch } from '@tanstack/react-router'
import {
  Ban,
  CircleCheck,
  CircleX,
  Coins,
  Copy,
  Gauge,
  GitCompare,
  MoreHorizontal,
  Radio,
  RefreshCcw,
  RotateCw,
  Sparkles,
  Timer,
  Zap,
} from 'lucide-react'
import { useCallback, useEffect, useMemo, useState } from 'react'
import { Screen } from '@/app/screen'
import { useWorkspace } from '@/app/workspace'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import { Combobox } from '@/components/ui/combobox'
import { Dialog } from '@/components/ui/dialog'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { EmptyState } from '@/components/ui/empty-state'
import { Skeleton } from '@/components/ui/skeleton'
import { StatTile } from '@/components/ui/stat-tile'
import { toast } from '@/components/ui/toast'
import { Tooltip } from '@/components/ui/tooltip'
import { ApiError } from '@/lib/api/client'
import { cn } from '@/lib/cn'
import { formatCompact, formatDateTime, formatDelta, formatDuration, formatPercent, formatRelative, formatUsd } from '@/lib/format'
import { useMediaQuery } from '@/lib/hotkeys'
import {
  aggregatesQuery,
  evaluatorsQuery,
  indexById,
  isActive,
  runQuery,
  suitesQuery,
  targetsQuery,
  trendsQuery,
  useRunAction,
  type Result,
  type Run,
  type RunAggregate,
} from './api'
import { useRunEvents, type LiveState } from './live'
import { ResultDrawer } from './result-drawer'
import { ResultsTable, useResults, type EvalColumn } from './results-table'
import { RunCharts } from './run-charts'
import type { RunDetailSearch } from './search'
import { formatRunDuration, LabelBadges, ProgressBar, RunStatusPill, runTitle, shortId, TriggerIcon } from './ui'

function LiveBadge({ state }: { state: LiveState }) {
  if (state === 'idle') return null
  const live = state === 'live'
  return (
    <Tooltip content={live ? 'Streaming live updates' : state === 'connecting' ? 'Connecting to the live stream…' : 'Reconnecting — counters refresh in the background'}>
      <span
        className={cn(
          'inline-flex h-6 items-center gap-1.5 rounded-full px-2 text-xs font-medium',
          live ? 'bg-running-subtle text-running' : 'bg-skipped-subtle text-text-2',
        )}
      >
        <Radio className={cn('size-3.5', live && 'animate-pulse motion-reduce:animate-none')} aria-hidden />
        {live ? 'Live' : state === 'connecting' ? 'Connecting' : 'Reconnecting'}
      </span>
    </Tooltip>
  )
}

function Metric({ icon, label, value, hint }: { icon: React.ReactNode; label: string; value: React.ReactNode; hint?: string }) {
  return (
    <div className="flex min-w-0 flex-col gap-0.5 bg-surface px-4 py-3 [&_svg]:size-3.5 [&_svg]:text-text-muted" title={hint}>
      <span className="flex items-center gap-1.5 text-xs text-text-muted">
        {icon}
        {label}
      </span>
      <span className="tabular truncate text-base font-semibold text-text">{value}</span>
    </div>
  )
}

function GateVerdict({ run }: { run: Run }) {
  const gates = run.summary?.gates ?? []
  if (!gates.length) return null
  const passed = gates.filter((g) => g.passed).length
  const active = isActive(run.status)
  const allPass = passed === gates.length
  return (
    <Card className="overflow-hidden">
      <div className="flex flex-wrap items-center justify-between gap-2 border-b border-border px-4 py-2.5">
        <div className="flex items-center gap-2">
          {active ? (
            <span className="text-sm font-semibold text-text">Gates · provisional</span>
          ) : allPass ? (
            <span className="flex items-center gap-1.5 text-sm font-semibold text-pass">
              <CircleCheck className="size-4" aria-hidden /> All gates passed
            </span>
          ) : (
            <span className="flex items-center gap-1.5 text-sm font-semibold text-fail">
              <CircleX className="size-4" aria-hidden /> Gate failed
            </span>
          )}
          <span className="tabular text-xs text-text-muted">
            {passed}/{gates.length} passing
          </span>
        </div>
        {!active && <span className="text-xs text-text-muted">{allPass ? 'CI exit code 0' : 'CI exit code 1'}</span>}
      </div>
      <ul className="grid grid-cols-1 divide-y divide-border md:grid-cols-2 md:divide-y-0">
        {gates.map((g, i) => (
          <li key={g.gate_id || i} className={cn('flex items-center gap-2.5 px-4 py-2', i >= 2 && 'md:border-t md:border-border')}>
            {g.passed ? <CircleCheck className="size-4 shrink-0 text-pass" aria-label="passed" /> : <CircleX className="size-4 shrink-0 text-fail" aria-label="failed" />}
            <code className="min-w-0 flex-1 truncate font-mono text-xs text-text">{g.text || g.name}</code>
            {g.value != null && <span className={cn('tabular shrink-0 font-mono text-xs', g.passed ? 'text-text-2' : 'text-fail')}>{Math.abs(g.value) >= 10 ? Math.round(g.value) : g.value.toFixed(3)}</span>}
          </li>
        ))}
      </ul>
    </Card>
  )
}

function RescoreDialog({ open, onOpenChange, evaluators, defaults, onConfirm, pending }: {
  open: boolean
  onOpenChange: (o: boolean) => void
  evaluators: { id: string; name: string }[]
  defaults: string[]
  onConfirm: (ids: string[]) => void
  pending: boolean
}) {
  const [ids, setIds] = useState<string[] | null>(null)
  const value = ids ?? defaults
  return (
    <Dialog
      open={open}
      onOpenChange={(o) => {
        if (!o) setIds(null)
        onOpenChange(o)
      }}
      size="sm"
      title="Re-score run"
      description="Re-evaluate the existing outputs without calling the target. Creates a new run."
      footer={
        <>
          <Button variant="ghost" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button variant="primary" icon={<Sparkles />} loading={pending} disabled={!value.length} onClick={() => onConfirm(value)}>
            Re-score
          </Button>
        </>
      }
    >
      <div className="pb-2">
        <Combobox multiple options={evaluators.map((e) => ({ value: e.id, label: e.name }))} value={value} onValueChange={setIds} placeholder="Evaluators" />
      </div>
    </Dialog>
  )
}

function Scorecards({
  evaluators,
  aggregates,
  baseline,
  trends,
  loading,
}: {
  evaluators: EvalColumn[]
  aggregates: RunAggregate[] | undefined
  baseline: RunAggregate[] | undefined
  trends: Map<string, number[]>
  loading: boolean
}) {
  if (loading)
    return (
      <div className="grid grid-cols-2 gap-3 md:grid-cols-3 xl:grid-cols-6">
        {Array.from({ length: Math.max(3, evaluators.length || 4) }, (_, i) => (
          <StatTile key={i} label="Evaluator" value="—" loading />
        ))}
      </div>
    )
  if (!evaluators.length) return null
  const find = (aggs: RunAggregate[] | undefined, id: string) => aggs?.find((a) => a.metric === 'score' && a.evaluator_id === id && a.slice_key === '')
  return (
    <div className={cn('grid grid-cols-2 gap-3 md:grid-cols-3', evaluators.length >= 5 ? 'xl:grid-cols-6' : 'xl:grid-cols-4')}>
      {evaluators.map((e) => {
        const cur = find(aggregates, e.id)
        const base = find(baseline, e.id)
        const d = cur?.mean != null && base?.mean != null ? cur.mean - base.mean : null
        return (
          <StatTile
            key={e.id}
            label={e.name}
            value={cur?.mean != null ? cur.mean.toFixed(2) : '—'}
            unit={cur?.pass_rate != null ? `${formatPercent(cur.pass_rate, 0)} pass` : undefined}
            delta={
              d != null
                ? { value: formatDelta(d, 'score'), direction: Math.abs(d) < 0.005 ? 'flat' : d > 0 ? 'up' : 'down', context: 'vs baseline' }
                : base === undefined && baseline
                  ? undefined
                  : undefined
            }
            trend={trends.get(e.id)}
          />
        )
      })}
    </div>
  )
}

export function RunDetailScreen() {
  const { run: runId } = useParams({ from: '/o/$org/p/$project/runs/$run' })
  const search = useSearch({ from: '/o/$org/p/$project/runs/$run' })
  const navigate = useNavigate({ from: '/o/$org/p/$project/runs/$run' })
  const { org, project } = useWorkspace()
  const pid = project?.id ?? ''
  const params = { org: org.slug, project: project?.slug ?? '' }
  const qc = useQueryClient()
  const wide = useMediaQuery('(min-width: 1280px)')

  const setSearch = useCallback(
    (patch: Partial<RunDetailSearch>) => navigate({ search: (s) => ({ ...s, ...patch }), replace: true, resetScroll: false }),
    [navigate],
  )

  const runQ = useQuery({ ...runQuery(runId), refetchInterval: (q) => (q.state.data && isActive(q.state.data.status) ? 10_000 : false) })
  const run = runQ.data
  const active = !!run && isActive(run.status)
  const live = useRunEvents(runId, active)
  // When the stream is down, poll a little faster.
  useEffect(() => {
    if (!active || live === 'live') return
    const t = setInterval(() => void qc.invalidateQueries({ queryKey: ['runs', 'detail', runId] }), 3_000)
    return () => clearInterval(t)
  }, [active, live, qc, runId])

  const suites = useQuery(suitesQuery(pid))
  const targets = useQuery(targetsQuery(pid))
  const evaluatorsQ = useQuery(evaluatorsQuery(pid))
  const aggs = useQuery(aggregatesQuery(runId))
  const baseAggs = useQuery(aggregatesQuery(run?.baseline_run_id))
  const evaluatorById = useMemo(() => indexById(evaluatorsQ.data), [evaluatorsQ.data])
  const suite = suites.data?.find((s) => s.id === run?.suite_id)
  const target = targets.data?.find((t) => t.id === run?.target_id)

  // Evaluator columns: suite order first, then anything else that produced scores.
  const evaluators: EvalColumn[] = useMemo(() => {
    const out: EvalColumn[] = []
    const seen = new Set<string>()
    for (const se of suite?.config.evaluators ?? []) {
      seen.add(se.evaluator_id)
      out.push({ id: se.evaluator_id, name: evaluatorById.get(se.evaluator_id)?.name ?? shortId(se.evaluator_id), threshold: se.threshold })
    }
    for (const a of aggs.data ?? []) {
      if (a.metric === 'score' && a.evaluator_id && !seen.has(a.evaluator_id)) {
        seen.add(a.evaluator_id)
        out.push({ id: a.evaluator_id, name: evaluatorById.get(a.evaluator_id)?.name ?? shortId(a.evaluator_id) })
      }
    }
    return out
  }, [suite, aggs.data, evaluatorById])

  const tags = useMemo(() => [...new Set((aggs.data ?? []).filter((a) => a.slice_key.startsWith('tag:')).map((a) => a.slice_key.slice(4)))].sort(), [aggs.data])

  // Sparklines: last 10 runs of this suite per evaluator.
  const trend = useQuery(trendsQuery(pid, { suite_id: run?.suite_id ?? undefined, metric: 'score_mean', bucket: 'run' }, !!run?.suite_id))
  const runTargetId = run?.target_id
  const trendMap = useMemo(() => {
    const m = new Map<string, number[]>()
    for (const s of trend.data?.series ?? []) {
      if (!s.evaluator_id || (s.target_id && runTargetId && s.target_id !== runTargetId)) continue
      m.set(s.evaluator_id, s.points.filter((p) => p.value != null).slice(-10).map((p) => p.value!))
    }
    return m
  }, [trend.data, runTargetId])

  const results = useResults(runId, search)
  const rows = useMemo(() => results.data?.pages.flatMap((p) => p.items) ?? [], [results.data])
  const openIndex = search.result ? rows.findIndex((r) => r.id === search.result) : -1
  const openResult = (r: Result) => setSearch({ result: r.id })
  const step = (d: number) => {
    const next = rows[openIndex + d]
    if (next) setSearch({ result: next.id })
    if (d > 0 && openIndex + d >= rows.length - 5 && results.hasNextPage) void results.fetchNextPage()
  }

  const actions = useRunAction(runId)
  const [rescoreOpen, setRescoreOpen] = useState(false)
  const [cancelOpen, setCancelOpen] = useState(false)
  const goToRun = (r: Run) => navigate({ to: '/o/$org/p/$project/runs/$run', params: { ...params, run: r.id }, search: {} })
  const onActionError = (what: string) => (e: unknown) =>
    toast.error(e instanceof ApiError && e.status === 501 ? `${what} isn’t available on this server yet.` : e instanceof ApiError ? e.message : `Could not ${what.toLowerCase()}.`)

  if (runQ.error) {
    const nf = runQ.error instanceof ApiError && runQ.error.status === 404
    return (
      <Screen>
        <Card>
          <EmptyState
            illustration={nf ? 'search' : 'error'}
            title={nf ? 'Run not found' : 'Couldn’t load this run'}
            description={nf ? 'It may have been deleted, or it belongs to another project.' : runQ.error.message}
            action={
              nf ? (
                <Button variant="primary" asChild>
                  <Link to="/o/$org/p/$project/runs" params={params}>
                    All runs
                  </Link>
                </Button>
              ) : (
                <Button icon={<RotateCw />} onClick={() => runQ.refetch()}>
                  Try again
                </Button>
              )
            }
          />
        </Card>
      </Screen>
    )
  }

  if (!run) {
    return (
      <Screen width="wide">
        <div className="flex flex-col gap-2">
          <Skeleton className="h-7 w-72" />
          <Skeleton className="h-4 w-96" />
        </div>
        <Skeleton className="h-16 w-full" />
        <div className="grid grid-cols-2 gap-3 md:grid-cols-3 xl:grid-cols-6">
          {Array.from({ length: 6 }, (_, i) => (
            <StatTile key={i} label="Evaluator" value="—" loading />
          ))}
        </div>
        <div className="grid grid-cols-1 gap-3 lg:grid-cols-2">
          <Skeleton className="h-[280px]" />
          <Skeleton className="h-[280px]" />
        </div>
        <Skeleton className="h-[420px]" />
      </Screen>
    )
  }

  const pct = run.total ? run.completed / run.total : 0
  const thr = run.summary?.throughput_per_min
  const etaMs = active && thr && thr > 0 ? ((run.total - run.completed) / thr) * 60_000 : null
  const failedCount = run.failed + run.errored

  return (
    <Screen width="wide">
      {/* Header */}
      <header className="flex flex-col gap-3">
        <div className="flex flex-wrap items-start justify-between gap-x-6 gap-y-3">
          <div className="min-w-0">
            <div className="flex flex-wrap items-center gap-2">
              <h1 className="truncate text-lg font-semibold tracking-tight text-text">{runTitle(run, suite?.name)}</h1>
              <RunStatusPill run={run} />
              <LiveBadge state={live} />
            </div>
            <div className="mt-1 flex flex-wrap items-center gap-x-3 gap-y-1 text-sm text-text-2">
              {suite && (
                <Link to="/o/$org/p/$project/suites/$suite" params={{ ...params, suite: suite.id }} className="rounded-sm hover:text-text focus-ring">
                  {suite.name}
                </Link>
              )}
              {target && (
                <>
                  <span className="text-text-muted" aria-hidden>
                    →
                  </span>
                  <span>{target.name}</span>
                </>
              )}
              <span className="text-border-strong" aria-hidden>
                |
              </span>
              <TriggerIcon kind={run.trigger_kind} withLabel />
              <Tooltip content={formatDateTime(new Date(run.created_at))}>
                <span className="text-text-muted">{formatRelative(new Date(run.created_at))}</span>
              </Tooltip>
              <span className="font-mono text-xs text-text-muted">{shortId(run.id)}</span>
              <LabelBadges labels={run.labels} max={4} />
            </div>
          </div>
          <div className="flex shrink-0 items-center gap-2">
            {run.baseline_run_id && (
              <Button icon={<GitCompare />} asChild>
                <Link to="/o/$org/p/$project/compare" params={params} search={{ runs: `${run.baseline_run_id},${run.id}` }}>
                  <GitCompare />
                  Compare to baseline
                </Link>
              </Button>
            )}
            {active ? (
              <Button variant="danger" icon={<Ban />} onClick={() => setCancelOpen(true)}>
                Cancel run
              </Button>
            ) : (
              failedCount > 0 && (
                <Button
                  icon={<RefreshCcw />}
                  loading={actions.rerunFailed.isPending}
                  onClick={() => actions.rerunFailed.mutate(undefined, { onSuccess: (r) => (toast.success('Re-running failed results'), goToRun(r)), onError: onActionError('Re-run failed') })}
                >
                  Re-run failed
                </Button>
              )
            )}
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button aria-label="More actions" className="px-2">
                  <MoreHorizontal />
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end" className="w-56">
                <DropdownMenuItem icon={<Sparkles />} disabled={active} onSelect={() => setRescoreOpen(true)}>
                  Re-score with evaluators…
                </DropdownMenuItem>
                <DropdownMenuItem
                  icon={<RefreshCcw />}
                  disabled={active || failedCount === 0}
                  onSelect={() => actions.rerunFailed.mutate(undefined, { onSuccess: goToRun, onError: onActionError('Re-run failed') })}
                >
                  Re-run failed ({failedCount})
                </DropdownMenuItem>
                <DropdownMenuSeparator />
                <DropdownMenuItem icon={<Copy />} onSelect={() => void navigator.clipboard.writeText(run.id).then(() => toast.success('Run id copied'))}>
                  Copy run id
                </DropdownMenuItem>
                <DropdownMenuItem
                  icon={<Copy />}
                  onSelect={() => void navigator.clipboard.writeText(`airval run --suite ${suite?.slug ?? run.suite_id} --gate`).then(() => toast.success('CLI command copied'))}
                >
                  Copy CLI command
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          </div>
        </div>
        {run.error && (
          <div role="alert" className="rounded-md border border-fail/30 bg-fail-subtle px-3 py-2 font-mono text-xs text-fail">
            {run.error}
          </div>
        )}
      </header>

      {/* Metrics strip + live progress */}
      <Card className="overflow-hidden">
        {(active || run.status === 'canceled') && (
          <div className="flex flex-col gap-2 border-b border-border px-4 py-3">
            <div className="flex flex-wrap items-center justify-between gap-2 text-sm">
              <span className="tabular text-text">
                <span className="font-semibold">{run.completed.toLocaleString()}</span>
                <span className="text-text-muted"> / {run.total.toLocaleString()} results</span>
                <span className="ml-2 text-text-2">{formatPercent(pct, 0)}</span>
              </span>
              <span className="tabular flex items-center gap-3 text-xs text-text-2">
                <span className="flex items-center gap-1">
                  <span className="size-2 rounded-full bg-accent" aria-hidden />
                  {(run.completed - failedCount).toLocaleString()} passed
                </span>
                <span className="flex items-center gap-1">
                  <span className="size-2 rounded-full bg-fail" aria-hidden />
                  {run.failed.toLocaleString()} failed
                </span>
                {run.errored > 0 && (
                  <span className="flex items-center gap-1">
                    <span className="size-2 rounded-full bg-warn" aria-hidden />
                    {run.errored.toLocaleString()} errored
                  </span>
                )}
                {etaMs != null && <span className="text-text-muted">~{formatDuration(etaMs)} left</span>}
              </span>
            </div>
            <ProgressBar run={run} />
          </div>
        )}
        <div className="grid grid-cols-2 gap-px bg-border sm:grid-cols-3 lg:grid-cols-6">
          <Metric icon={<Gauge />} label="Pass rate" value={run.summary?.pass_rate != null ? formatPercent(run.summary.pass_rate) : '—'} />
          <Metric icon={<Timer />} label="Duration" value={formatRunDuration(run)} />
          <Metric icon={<Zap />} label="Throughput" value={thr ? `${formatCompact(thr)}/min` : '—'} hint="Results per minute" />
          <Metric
            icon={<Timer />}
            label="Latency p50 / p95"
            value={run.summary?.latency_p50_ms != null ? `${formatDuration(run.summary.latency_p50_ms)} / ${formatDuration(run.summary.latency_p95_ms ?? 0)}` : '—'}
          />
          <Metric icon={<Coins />} label="Cost" value={formatUsd(run.cost_usd)} hint={`${run.tokens_in.toLocaleString()} tokens in, ${run.tokens_out.toLocaleString()} out`} />
          <Metric
            icon={<Sparkles />}
            label="Tokens · cache hits"
            value={`${formatCompact(run.tokens_in + run.tokens_out)}${run.summary?.cache_hit_ratio != null ? ` · ${formatPercent(run.summary.cache_hit_ratio, 0)}` : ''}`}
          />
        </div>
        {run.baseline_run_id && (
          <div className="flex items-center gap-2 border-t border-border px-4 py-2 text-xs text-text-muted">
            Baseline
            <Link
              to="/o/$org/p/$project/runs/$run"
              params={{ ...params, run: run.baseline_run_id }}
              search={{}}
              className="rounded-sm font-mono text-accent hover:underline focus-ring"
            >
              {shortId(run.baseline_run_id)}
            </Link>
            {run.parent_run_id && (
              <>
                <span aria-hidden>·</span> Parent
                <Link to="/o/$org/p/$project/runs/$run" params={{ ...params, run: run.parent_run_id }} search={{}} className="rounded-sm font-mono text-accent hover:underline focus-ring">
                  {shortId(run.parent_run_id)}
                </Link>
              </>
            )}
            {run.summary?.stopped_early && <Badge variant="warn">stopped early</Badge>}
          </div>
        )}
      </Card>

      <GateVerdict run={run} />

      <section className="flex flex-col gap-3" aria-label="Scorecard">
        <h2 className="text-md font-semibold text-text">Scorecard</h2>
        <Scorecards evaluators={evaluators} aggregates={aggs.data} baseline={baseAggs.data} trends={trendMap} loading={aggs.isPending || (suites.isPending && !evaluators.length)} />
        {aggs.error && <p className="text-sm text-fail">Couldn’t load aggregates: {aggs.error.message}</p>}
      </section>

      <RunCharts
        run={run}
        aggregates={aggs.data}
        baseline={baseAggs.data}
        evaluators={evaluators}
        evaluatorById={evaluatorById}
        loading={aggs.isPending}
        histEvaluator={search.hist}
        onHistEvaluator={(id) => setSearch({ hist: id })}
      />

      <ResultsTable run={run} search={search} setSearch={setSearch} evaluators={evaluators} tags={tags} results={results} onOpen={openResult} wide={wide} />

      <ResultDrawer
        runId={runId}
        resultId={search.result}
        summary={openIndex >= 0 ? rows[openIndex] : undefined}
        tab={search.tab ?? 'output'}
        evaluators={evaluators}
        onTab={(t) => setSearch({ tab: t === 'output' ? undefined : t })}
        onClose={() => setSearch({ result: undefined, tab: undefined })}
        onPrev={openIndex > 0 ? () => step(-1) : undefined}
        onNext={openIndex >= 0 && (openIndex < rows.length - 1 || results.hasNextPage) ? () => step(1) : undefined}
      />

      <RescoreDialog
        open={rescoreOpen}
        onOpenChange={setRescoreOpen}
        evaluators={(evaluatorsQ.data ?? []).map((e) => ({ id: e.id, name: e.name }))}
        defaults={evaluators.map((e) => e.id)}
        pending={actions.rescore.isPending}
        onConfirm={(ids) =>
          actions.rescore.mutate(ids, {
            onSuccess: (r) => {
              setRescoreOpen(false)
              toast.success('Re-scoring started')
              goToRun(r)
            },
            onError: onActionError('Re-score'),
          })
        }
      />
      <Dialog
        open={cancelOpen}
        onOpenChange={setCancelOpen}
        size="sm"
        title="Cancel this run?"
        description={`${run.completed.toLocaleString()} of ${run.total.toLocaleString()} results are done. Finished results are kept; remaining examples are skipped.`}
        footer={
          <>
            <Button variant="ghost" onClick={() => setCancelOpen(false)}>
              Keep running
            </Button>
            <Button
              variant="danger"
              loading={actions.cancel.isPending}
              onClick={() =>
                actions.cancel.mutate(undefined, {
                  onSuccess: () => {
                    setCancelOpen(false)
                    toast.success('Run cancelled')
                  },
                  onError: onActionError('Cancel'),
                })
              }
            >
              Cancel run
            </Button>
          </>
        }
      />
    </Screen>
  )
}
