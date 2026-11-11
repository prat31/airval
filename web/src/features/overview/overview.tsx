import { useQuery } from '@tanstack/react-query'
import { Link, useNavigate } from '@tanstack/react-router'
import { Activity, ArrowRight, CircleCheck, CircleDollarSign, CircleX, Gauge, LayoutDashboard, Layers, Play, RotateCw, Target } from 'lucide-react'
import { useMemo, useState } from 'react'
import { Screen } from '@/app/screen'
import { useWorkspace } from '@/app/workspace'
import { TrendLine } from '@/charts'
import { Button } from '@/components/ui/button'
import { Card, CardHeader } from '@/components/ui/card'
import { EmptyState } from '@/components/ui/empty-state'
import { PageHeader } from '@/components/ui/page-header'
import { SegmentedControl } from '@/components/ui/segmented-control'
import { Skeleton } from '@/components/ui/skeleton'
import { StatTile } from '@/components/ui/stat-tile'
import { formatCompact, formatDelta, formatPercent, formatRelative, formatUsd } from '@/lib/format'
import { indexById, isActive, overviewQuery, runsQuery, suitesQuery, targetsQuery, trendsQuery } from '@/features/runs/api'
import { RunSuiteDialog } from '@/features/runs/run-suite-dialog'
import { PassRateBar, ProgressBar, RunStatusPill, runTitle, TriggerIcon } from '@/features/runs/ui'

const DAY = 86_400_000
type Window = '7d' | '30d' | '90d'

export function OverviewScreen() {
  const { org, project } = useWorkspace()
  const pid = project?.id ?? ''
  const params = { org: org.slug, project: project?.slug ?? '' }
  const navigate = useNavigate()
  const [runOpen, setRunOpen] = useState(false)
  const [win, setWin] = useState<Window>('30d')

  const overview = useQuery(overviewQuery(pid))
  // Hour-rounded so the query key stays stable across renders.
  const [now] = useState(() => Math.floor(Date.now() / 3_600_000) * 3_600_000)
  const from = useMemo(() => new Date(now - (win === '7d' ? 7 : win === '30d' ? 30 : 90) * DAY).toISOString(), [win, now])
  const trend = useQuery(trendsQuery(pid, { metric: 'pass_rate', bucket: 'run', from }))
  const recent = useQuery({ ...runsQuery({ project_id: pid, limit: 8 }), refetchInterval: 10_000 })
  const suites = useQuery(suitesQuery(pid))
  const targets = useQuery(targetsQuery(pid))
  const suiteById = useMemo(() => indexById(suites.data), [suites.data])
  const targetById = useMemo(() => indexById(targets.data), [targets.data])

  const o = overview.data
  const series = useMemo(() => {
    // One line per suite; color follows the suite's position in the suite list (stable identity).
    const order = (suites.data ?? []).map((s) => s.id)
    const bySuite = new Map<string, { name: string; points: { t: string; v: number | null }[]; idx: number }>()
    for (const s of trend.data?.series ?? []) {
      const key = s.suite_id ?? s.key
      const cur = bySuite.get(key) ?? { name: suiteById.get(key)?.name ?? s.label, points: [], idx: Math.max(0, order.indexOf(key)) }
      cur.points.push(...s.points.map((p) => ({ t: p.t, v: p.value })))
      bySuite.set(key, cur)
    }
    return [...bySuite.values()]
      .filter((s) => s.points.length > 0)
      .slice(0, 6)
      .map((s) => ({ name: s.name, colorIndex: s.idx % 6, points: s.points.sort((a, b) => Date.parse(a.t) - Date.parse(b.t)) }))
  }, [trend.data, suiteById, suites.data])

  const noRuns = recent.data?.length === 0
  const noSuites = suites.data?.length === 0

  if (noRuns && noSuites) {
    return (
      <Screen>
        <PageHeader icon={<LayoutDashboard />} title="Overview" description={project?.description ?? 'Health of this project across suites and targets.'} />
        <Card>
          <EmptyState
            illustration="suites"
            title="Create your first suite"
            description="A suite combines a target, a dataset, evaluators and gates. Run it manually, on a schedule or from CI — results land here."
            action={
              <Button variant="primary" icon={<Layers />} asChild>
                <Link to="/o/$org/p/$project/suites/new" params={params}>
                  <Layers />
                  Create your first suite
                </Link>
              </Button>
            }
            secondaryAction={
              <Button variant="ghost" asChild>
                <Link to="/o/$org/p/$project/targets" params={params}>
                  Connect a target first
                </Link>
              </Button>
            }
          />
        </Card>
      </Screen>
    )
  }

  const runsDelta = o ? o.runs_7d - o.runs_prev_7d : 0
  const spendDelta = o?.spend_prev_7d_usd != null ? o.spend_7d_usd - o.spend_prev_7d_usd : null
  const failing = o?.failing_suites ?? []

  return (
    <Screen>
      <PageHeader
        icon={<LayoutDashboard />}
        title="Overview"
        description={project?.description ?? 'Health of this project across suites and targets.'}
        actions={
          <Button variant="primary" icon={<Play />} onClick={() => setRunOpen(true)}>
            Run suite
          </Button>
        }
      />

      {overview.error ? (
        <Card>
          <EmptyState
            compact
            illustration="error"
            title="Couldn’t load project stats"
            description={overview.error.message}
            action={
              <Button size="sm" icon={<RotateCw />} onClick={() => overview.refetch()}>
                Try again
              </Button>
            }
          />
        </Card>
      ) : (
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-4">
          <StatTile
            label="Runs · last 7 days"
            icon={<Activity />}
            loading={!o}
            value={o ? formatCompact(o.runs_7d) : '—'}
            delta={o ? { value: `${runsDelta >= 0 ? '+' : '−'}${Math.abs(runsDelta)}`, direction: runsDelta === 0 ? 'flat' : runsDelta > 0 ? 'up' : 'down', upIsGood: true, context: 'vs prior 7 days' } : undefined}
          />
          <StatTile
            label="Latest pass rate"
            icon={<Target />}
            loading={!o}
            value={o?.latest_pass_rate != null ? formatPercent(o.latest_pass_rate) : '—'}
            delta={
              o?.pass_rate_delta != null
                ? { value: formatDelta(o.pass_rate_delta, 'percent'), direction: Math.abs(o.pass_rate_delta) < 0.0005 ? 'flat' : o.pass_rate_delta > 0 ? 'up' : 'down', context: 'vs previous run' }
                : undefined
            }
          />
          <StatTile label="Average score" icon={<Gauge />} loading={!o} value={o?.avg_score != null ? o.avg_score.toFixed(2) : '—'} unit={o?.avg_score != null ? 'latest run' : undefined} />
          <StatTile
            label="Spend · last 7 days"
            icon={<CircleDollarSign />}
            loading={!o}
            value={o ? formatUsd(o.spend_7d_usd) : '—'}
            delta={
              spendDelta != null && o?.spend_prev_7d_usd
                ? { value: `${spendDelta >= 0 ? '+' : '−'}${Math.round(Math.abs(spendDelta / o.spend_prev_7d_usd) * 100)}%`, direction: Math.abs(spendDelta) < 0.005 ? 'flat' : spendDelta > 0 ? 'up' : 'down', upIsGood: false, context: 'vs prior 7 days' }
                : undefined
            }
          />
        </div>
      )}

      <div className="grid grid-cols-1 gap-3 lg:grid-cols-3">
        <TrendLine
          className="lg:col-span-2"
          title="Pass rate by suite"
          description="Per finished run"
          series={series}
          format="percent"
          yDomain={[0, 1]}
          height={260}
          loading={trend.isPending}
          empty={
            trend.error ? (
              <p className="py-20 text-center text-sm text-text-muted">Trends are unavailable: {trend.error.message}</p>
            ) : !series.length ? (
              <p className="py-20 text-center text-sm text-text-muted">No finished runs in this window.</p>
            ) : undefined
          }
          actions={
            <SegmentedControl<Window>
              size="sm"
              aria-label="Time window"
              value={win}
              onValueChange={setWin}
              options={[
                { value: '7d', label: '7d' },
                { value: '30d', label: '30d' },
                { value: '90d', label: '90d' },
              ]}
            />
          }
          ariaLabel="Pass rate per suite over time"
        />
        <Card className="flex flex-col">
          <CardHeader
            title="Failing gates"
            description={o ? (failing.length ? `${failing.length} suite${failing.length === 1 ? '' : 's'} failing on the latest run` : 'Latest run of every suite') : ' '}
          />
          <div className="flex flex-1 flex-col px-2 pb-2 pt-1">
            {!o ? (
              <div className="flex flex-col gap-3 px-2 pt-2">
                {[0, 1, 2].map((i) => (
                  <div key={i} className="flex items-center gap-3">
                    <Skeleton className="size-6 rounded-full" />
                    <div className="flex flex-1 flex-col gap-1.5">
                      <Skeleton className="h-3 w-3/4" />
                      <Skeleton className="h-2.5 w-1/2" />
                    </div>
                  </div>
                ))}
              </div>
            ) : failing.length === 0 ? (
              <div className="flex flex-1 flex-col items-center justify-center gap-2 py-8 text-center">
                <span className="flex size-9 items-center justify-center rounded-full bg-pass-subtle text-pass">
                  <CircleCheck className="size-5" aria-hidden />
                </span>
                <p className="text-sm font-medium text-text">All gates passing</p>
                <p className="max-w-56 text-xs text-text-muted">Every suite’s latest finished run met its pass criteria.</p>
              </div>
            ) : (
              <ul className="flex flex-col">
                {failing.map((f) => (
                  <li key={f.suite_id}>
                    <Link
                      to="/o/$org/p/$project/runs/$run"
                      params={{ ...params, run: f.run_id }}
                      search={{}}
                      className="group flex items-center gap-3 rounded-md px-2 py-2 transition-colors hover:bg-surface-hover focus-ring"
                    >
                      <span className="flex size-7 shrink-0 items-center justify-center rounded-full bg-fail-subtle text-fail">
                        <CircleX className="size-4" aria-hidden />
                      </span>
                      <span className="min-w-0 flex-1">
                        <span className="block truncate text-sm font-medium text-text">{f.name ?? suiteById.get(f.suite_id)?.name ?? 'Suite'}</span>
                        <span className="block text-xs text-text-muted">Gate failed on latest run</span>
                      </span>
                      <ArrowRight className="size-4 text-text-muted opacity-0 transition-opacity group-hover:opacity-100" aria-hidden />
                    </Link>
                  </li>
                ))}
              </ul>
            )}
          </div>
        </Card>
      </div>

      <Card>
        <CardHeader
          title="Recent runs"
          actions={
            <Button size="sm" variant="ghost" iconRight={<ArrowRight />} asChild>
              <Link to="/o/$org/p/$project/runs" params={params}>
                All runs
                <ArrowRight />
              </Link>
            </Button>
          }
        />
        {recent.isPending ? (
          <div className="flex flex-col gap-3 px-4 pb-4 pt-2">
            {Array.from({ length: 5 }, (_, i) => (
              <Skeleton key={i} className="h-8 w-full" />
            ))}
          </div>
        ) : recent.error ? (
          <EmptyState compact illustration="error" title="Couldn’t load runs" description={recent.error.message} action={<Button size="sm" onClick={() => recent.refetch()}>Retry</Button>} />
        ) : noRuns ? (
          <EmptyState
            compact
            illustration="runs"
            title="No runs yet"
            description="Runs appear here as soon as a suite is executed — manually, on a schedule, from a webhook or CI."
            action={
              <Button variant="primary" size="sm" icon={<Play />} onClick={() => setRunOpen(true)}>
                Run suite
              </Button>
            }
          />
        ) : (
          <ul className="divide-y divide-border border-t border-border">
            {recent.data!.map((r) => (
              <li key={r.id}>
                <button
                  type="button"
                  onClick={() => navigate({ to: '/o/$org/p/$project/runs/$run', params: { ...params, run: r.id } })}
                  className="grid w-full grid-cols-[96px_minmax(0,1fr)_120px] items-center gap-3 px-4 py-2.5 text-left transition-colors hover:bg-surface-hover focus-ring sm:grid-cols-[104px_minmax(0,1fr)_28px_160px_96px]"
                >
                  <span className="justify-self-start"><RunStatusPill run={r} size="sm" /></span>
                  <span className="min-w-0">
                    <span className="block truncate text-sm font-medium text-text">{runTitle(r, suiteById.get(r.suite_id ?? '')?.name)}</span>
                    <span className="block truncate text-xs text-text-muted">{targetById.get(r.target_id ?? '')?.name ?? '—'}</span>
                  </span>
                  <span className="hidden sm:block">
                    <TriggerIcon kind={r.trigger_kind} />
                  </span>
                  <span className="hidden sm:block">{isActive(r.status) ? <ProgressBar run={r} /> : <PassRateBar value={r.summary?.pass_rate} />}</span>
                  <span className="text-right text-xs text-text-muted">{formatRelative(new Date(r.created_at))}</span>
                </button>
              </li>
            ))}
          </ul>
        )}
      </Card>
      <RunSuiteDialog open={runOpen} onOpenChange={setRunOpen} />
    </Screen>
  )
}
