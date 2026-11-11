import { useInfiniteQuery, useQuery } from '@tanstack/react-query'
import { Link, useNavigate, useSearch } from '@tanstack/react-router'
import { GitCompare, Play, RotateCw } from 'lucide-react'
import { useMemo, useState, type UIEvent } from 'react'
import { Screen } from '@/app/screen'
import { useWorkspace } from '@/app/workspace'
import { Button } from '@/components/ui/button'
import { defaultDatePresets, DateRangePicker, type DateRange, type DateRangePreset } from '@/components/ui/date-range-picker'
import { EmptyState } from '@/components/ui/empty-state'
import { FilterBar } from '@/components/ui/filter-bar'
import { PageHeader } from '@/components/ui/page-header'
import { selectionColumn, Table, type ColumnDef, type RowSelectionState } from '@/components/ui/table'
import { Tooltip } from '@/components/ui/tooltip'
import { formatDateTime, formatRelative, formatUsd } from '@/lib/format'
import { useHotkeys, useMediaQuery } from '@/lib/hotkeys'
import { indexById, isActive, runsInfiniteQuery, suitesQuery, targetsQuery, type Run, type RunListQuery, type RunStatus, type TriggerKind } from './api'
import { RunSuiteDialog } from './run-suite-dialog'
import type { RunsSearch } from './search'
import { formatRunDuration, LabelBadges, PassRateBar, ProgressBar, RunStatusPill, runTitle, shortId, triggerMeta, TriggerIcon } from './ui'

const allTime: DateRangePreset = { id: 'all', label: 'All time', short: 'All', range: () => ({ from: new Date(0), to: new Date() }) }
const presets = [...defaultDatePresets, allTime]

const statusOptions: { value: RunStatus; label: string }[] = [
  { value: 'succeeded', label: 'Passed' },
  { value: 'failed', label: 'Failed' },
  { value: 'running', label: 'Running' },
  { value: 'queued', label: 'Queued' },
  { value: 'errored', label: 'Errored' },
  { value: 'canceled', label: 'Cancelled' },
]

const csv = (v?: string) => (v ? v.split(',').filter(Boolean) : [])

export function RunsScreen() {
  const { org, project } = useWorkspace()
  const search = useSearch({ from: '/o/$org/p/$project/runs' })
  const navigate = useNavigate({ from: '/o/$org/p/$project/runs' })
  const params = { org: org.slug, project: project?.slug ?? '' }
  const pid = project?.id ?? ''
  const [runOpen, setRunOpen] = useState(false)
  const [selection, setSelection] = useState<RowSelectionState>({})
  const wide = useMediaQuery('(min-width: 1180px)')

  const setSearch = (patch: Partial<RunsSearch>) => navigate({ search: (s) => ({ ...s, ...patch }), replace: true })

  const status = csv(search.status)
  const trigger = csv(search.trigger)
  const suite = csv(search.suite)
  const target = csv(search.target)

  // Date range → server filter.
  const range: DateRange = useMemo(() => {
    if (search.range === 'custom' && search.from && search.to) return { from: new Date(search.from), to: new Date(search.to), preset: undefined }
    const p = presets.find((x) => x.id === (search.range ?? 'all')) ?? allTime
    return { ...p.range(), preset: p.id }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [search.range, search.from, search.to])

  const query: RunListQuery = {
    project_id: pid,
    status: status.length ? (status as RunStatus[]) : undefined,
    trigger: trigger.length === 1 ? (trigger[0] as TriggerKind) : undefined,
    suite_id: suite.length === 1 ? suite[0] : undefined,
    target_id: target.length === 1 ? target[0] : undefined,
    from: range.preset === 'all' ? undefined : range.from.toISOString().slice(0, 13) + ':00:00Z',
    to: range.preset === 'custom' || !range.preset ? range.to.toISOString() : undefined,
  }
  const runs = useInfiniteQuery({
    ...runsInfiniteQuery(query),
    refetchInterval: (q) => (q.state.data?.pages[0]?.items.some((r) => isActive(r.status)) ? 4_000 : false),
  })
  const suites = useQuery(suitesQuery(pid))
  const targets = useQuery(targetsQuery(pid))
  const suiteById = useMemo(() => indexById(suites.data), [suites.data])
  const targetById = useMemo(() => indexById(targets.data), [targets.data])

  const rows = useMemo(() => {
    const all = runs.data?.pages.flatMap((p) => p.items) ?? []
    const q = search.q?.toLowerCase().trim()
    return all.filter((r) => {
      if (trigger.length > 1 && !trigger.includes(r.trigger_kind)) return false
      if (suite.length > 1 && !suite.includes(r.suite_id ?? '')) return false
      if (target.length > 1 && !target.includes(r.target_id ?? '')) return false
      if (!q) return true
      const hay = `${r.name} ${suiteById.get(r.suite_id ?? '')?.name ?? ''} ${targetById.get(r.target_id ?? '')?.name ?? ''} ${r.id} ${Object.entries(r.labels)
        .map(([k, v]) => `${k}:${v} ${v}`)
        .join(' ')}`.toLowerCase()
      return hay.includes(q)
    })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [runs.data, search.q, search.trigger, search.suite, search.target, suiteById, targetById])

  const selectedIds = Object.keys(selection).filter((k) => selection[k])
  const canCompare = selectedIds.length >= 2 && selectedIds.length <= 4
  const compare = () => {
    if (!canCompare) return
    // Oldest first: the earliest run becomes the baseline.
    const ordered = rows.filter((r) => selection[r.id]).reverse().map((r) => r.id)
    navigate({ to: '/o/$org/p/$project/compare', params, search: { runs: ordered.join(',') } })
  }
  useHotkeys({ c: compare, n: () => setRunOpen(true) })

  const columns = useMemo<ColumnDef<Run, unknown>[]>(() => {
    const cols: (ColumnDef<Run, unknown> | false)[] = [
      selectionColumn<Run>(),
      { id: 'status', header: 'Status', size: 116, enableSorting: false, cell: ({ row }) => <RunStatusPill run={row.original} size="sm" /> },
      {
        id: 'run',
        header: 'Run',
        size: 240,
        meta: { grow: true },
        accessorFn: (r) => runTitle(r, suiteById.get(r.suite_id ?? '')?.name),
        cell: ({ row }) => {
          const r = row.original
          const suiteName = suiteById.get(r.suite_id ?? '')?.name
          return (
            <div className="flex min-w-0 flex-col">
              <span className="truncate font-medium text-text">{runTitle(r, suiteName)}</span>
              <span className="truncate text-xs text-text-muted">
                {r.name && suiteName ? `${suiteName} · ` : ''}
                <span className="font-mono">{shortId(r.id)}</span>
                {!wide && <> · {targetById.get(r.target_id ?? '')?.name ?? '—'}</>}
              </span>
            </div>
          )
        },
      },
      wide && {
        id: 'target',
        header: 'Target',
        size: 170,
        accessorFn: (r) => targetById.get(r.target_id ?? '')?.name ?? '',
        cell: ({ getValue }) => <span className="truncate text-text-2">{(getValue() as string) || '—'}</span>,
      },
      { id: 'trigger', header: 'Trigger', size: 72, enableSorting: false, meta: { align: 'center' }, cell: ({ row }) => <TriggerIcon kind={row.original.trigger_kind} /> },
      {
        id: 'pass',
        header: 'Pass rate',
        size: 160,
        accessorFn: (r) => r.summary?.pass_rate ?? -1,
        cell: ({ row }) => {
          const r = row.original
          if (isActive(r.status))
            return (
              <span className="flex w-full items-center gap-2">
                <span className="tabular w-11 shrink-0 text-right text-xs text-text-2">{r.total ? Math.floor((r.completed / r.total) * 100) : 0}%</span>
                <ProgressBar run={r} className="flex-1" />
              </span>
            )
          return <PassRateBar value={r.summary?.pass_rate} />
        },
      },
      {
        id: 'score',
        header: 'Avg score',
        size: 92,
        meta: { align: 'right', numeric: true },
        accessorFn: (r) => r.summary?.mean_score ?? -1,
        cell: ({ row }) => (row.original.summary?.mean_score != null ? row.original.summary.mean_score.toFixed(2) : <span className="text-text-muted">—</span>),
      },
      wide && {
        id: 'duration',
        header: 'Duration',
        size: 92,
        meta: { align: 'right', numeric: true },
        enableSorting: false,
        cell: ({ row }) => <span className="text-text-2">{formatRunDuration(row.original)}</span>,
      },
      wide && {
        id: 'cost',
        header: 'Cost',
        size: 84,
        meta: { align: 'right', numeric: true },
        accessorFn: (r) => r.cost_usd,
        cell: ({ row }) => <span className="text-text-2">{row.original.cost_usd ? formatUsd(row.original.cost_usd) : '—'}</span>,
      },
      { id: 'labels', header: 'Labels', size: 180, enableSorting: false, cell: ({ row }) => <LabelBadges labels={row.original.labels} max={wide ? 2 : 1} /> },
      {
        id: 'created',
        header: 'Started',
        size: 112,
        meta: { align: 'right' },
        accessorFn: (r) => r.created_at,
        cell: ({ row }) => (
          <Tooltip content={formatDateTime(new Date(row.original.created_at))}>
            <span className="text-text-2">{formatRelative(new Date(row.original.created_at))}</span>
          </Tooltip>
        ),
      },
    ]
    return cols.filter(Boolean) as ColumnDef<Run, unknown>[]
  }, [suiteById, targetById, wide])

  const onScroll = (e: UIEvent<HTMLDivElement>) => {
    const el = e.target as HTMLElement
    if (el.scrollHeight - el.scrollTop - el.clientHeight < 400 && runs.hasNextPage && !runs.isFetchingNextPage) void runs.fetchNextPage()
  }

  const filtered = !!(search.q || status.length || trigger.length || suite.length || target.length || (search.range && search.range !== 'all'))
  const total = rows.length

  return (
    <Screen width="wide">
      <PageHeader
        icon={<Play />}
        title="Runs"
        description="Every execution of a suite against a target."
        actions={
          <>
            <Tooltip content={canCompare ? 'Compare selected runs' : 'Select 2–4 runs to compare'} shortcut={['C']}>
              <span>
                <Button icon={<GitCompare />} disabled={!canCompare} onClick={compare}>
                  Compare{selectedIds.length ? ` (${selectedIds.length})` : ''}
                </Button>
              </span>
            </Tooltip>
            <Tooltip content="Run a suite" shortcut={['N']}>
              <Button variant="primary" icon={<Play />} onClick={() => setRunOpen(true)}>
                Run suite
              </Button>
            </Tooltip>
          </>
        }
      />
      <FilterBar
        search={search.q ?? ''}
        onSearchChange={(q) => setSearch({ q: q || undefined })}
        searchPlaceholder="Search runs, labels, git sha…"
        filters={[
          { id: 'status', label: 'Status', value: status, onChange: (v) => setSearch({ status: v.join(',') || undefined }), options: statusOptions },
          {
            id: 'trigger',
            label: 'Trigger',
            value: trigger,
            onChange: (v) => setSearch({ trigger: v.join(',') || undefined }),
            options: (Object.keys(triggerMeta) as TriggerKind[]).map((k) => {
              const M = triggerMeta[k].icon
              return { value: k, label: triggerMeta[k].label, icon: <M className="size-3.5 text-text-muted" /> }
            }),
          },
          {
            id: 'suite',
            label: 'Suite',
            value: suite,
            onChange: (v) => setSearch({ suite: v.join(',') || undefined }),
            options: (suites.data ?? []).map((s) => ({ value: s.id, label: s.name })),
          },
          {
            id: 'target',
            label: 'Target',
            value: target,
            onChange: (v) => setSearch({ target: v.join(',') || undefined }),
            options: (targets.data ?? []).map((t) => ({ value: t.id, label: t.name })),
          },
        ]}
        actions={
          <DateRangePicker
            presets={presets}
            value={range}
            onChange={(r) =>
              setSearch(
                r.preset && r.preset !== 'custom'
                  ? { range: r.preset === 'all' ? undefined : r.preset, from: undefined, to: undefined }
                  : { range: 'custom', from: r.from.toISOString(), to: r.to.toISOString() },
              )
            }
          />
        }
      />
      {runs.error && !runs.data ? (
        <div className="rounded-md border border-border bg-surface">
          <EmptyState
            illustration="error"
            title="Couldn’t load runs"
            description={runs.error.message}
            action={
              <Button icon={<RotateCw />} onClick={() => runs.refetch()}>
                Try again
              </Button>
            }
          />
        </div>
      ) : (
        <div onScrollCapture={onScroll} className="flex flex-col gap-2">
          <Table
            aria-label="Runs"
            data={rows}
            columns={columns}
            getRowId={(r) => r.id}
            rowHeight={52}
            virtualizeThreshold={60}
            maxHeight="calc(100dvh - 248px)"
            enableRowSelection
            rowSelection={selection}
            onRowSelectionChange={setSelection}
            loading={runs.isPending}
            onRowActivate={(r) => navigate({ to: '/o/$org/p/$project/runs/$run', params: { ...params, run: r.id } })}
            className="min-h-64"
            empty={
              filtered ? (
                <EmptyState
                  compact
                  illustration="search"
                  title="No runs match these filters"
                  description="Try a wider date range or clear some filters."
                  action={
                    <Button size="sm" onClick={() => navigate({ search: {}, replace: true })}>
                      Clear filters
                    </Button>
                  }
                />
              ) : (
                <EmptyState
                  illustration="runs"
                  title="No runs yet"
                  description={
                    <>
                      Start a run from a suite, or trigger one from CI with{' '}
                      <code className="rounded-sm bg-surface-2 px-1 py-0.5 font-mono text-xs text-text">airval run --gate</code>.
                    </>
                  }
                  action={
                    <Button variant="primary" icon={<Play />} onClick={() => setRunOpen(true)}>
                      Run suite
                    </Button>
                  }
                  secondaryAction={
                    <Button variant="ghost" asChild>
                      <Link to="/o/$org/p/$project/suites" params={params}>
                        Go to suites
                      </Link>
                    </Button>
                  }
                />
              )
            }
          />
          {total > 0 && (
            <p className="tabular px-1 text-xs text-text-muted">
              {total.toLocaleString()} run{total === 1 ? '' : 's'}
              {runs.hasNextPage ? ' loaded · scroll for more' : ''}
              {runs.isFetchingNextPage ? ' · loading…' : ''}
              {selectedIds.length > 0 && <> · {selectedIds.length} selected</>}
            </p>
          )}
        </div>
      )}
      <RunSuiteDialog open={runOpen} onOpenChange={setRunOpen} />
    </Screen>
  )
}
