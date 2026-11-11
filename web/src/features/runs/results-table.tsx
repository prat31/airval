import { useInfiniteQuery } from '@tanstack/react-query'
import { RotateCw, Search, SlidersHorizontal, X } from 'lucide-react'
import { useEffect, useMemo, useState, type UIEvent } from 'react'
import { Button } from '@/components/ui/button'
import { EmptyState } from '@/components/ui/empty-state'
import { Input } from '@/components/ui/input'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import { ScoreChip } from '@/components/ui/score-chip'
import { SegmentedControl } from '@/components/ui/segmented-control'
import { Select } from '@/components/ui/select'
import { StatusPill } from '@/components/ui/status-pill'
import { Table, type ColumnDef } from '@/components/ui/table'
import { cn } from '@/lib/cn'
import { formatDuration } from '@/lib/format'
import { resultsInfiniteQuery, type Result, type ResultListQuery, type Run } from './api'
import type { ResultFilter, RunDetailSearch } from './search'
import { resultStatus } from './ui'

export interface EvalColumn {
  id: string
  name: string
  threshold?: number
}

export function preview(v: unknown): string {
  if (v == null) return ''
  if (typeof v === 'string') return v
  if (typeof v === 'object') {
    const o = v as Record<string, unknown>
    const first = ['question', 'input', 'query', 'prompt', 'message'].map((k) => o[k]).find((x) => typeof x === 'string')
    if (typeof first === 'string') return first
  }
  return JSON.stringify(v)
}

/** Build the server-side filter from URL state. */
export function resultQueryFromSearch(s: RunDetailSearch): ResultListQuery {
  const q: ResultListQuery = {}
  const status = s.status ?? 'all'
  if (s.ev) {
    q.evaluator_id = s.ev
    if (status === 'failed') q.passed = false
    else if (status === 'passed') q.passed = true
    else if (status === 'errored') q.status = ['errored']
    if (s.min != null) q.score_min = s.min
    if (s.max != null) q.score_max = s.max
  } else if (status !== 'all') q.status = [status]
  if (s.tag) q.tag = s.tag
  if (s.q) q.q = s.q
  return q
}

export function useResults(runId: string, search: RunDetailSearch) {
  const query = useMemo(() => resultQueryFromSearch(search), [search])
  return useInfiniteQuery(resultsInfiniteQuery(runId, query))
}

interface Props {
  run: Run
  search: RunDetailSearch
  setSearch: (patch: Partial<RunDetailSearch>) => void
  evaluators: EvalColumn[]
  tags: string[]
  results: ReturnType<typeof useResults>
  onOpen: (r: Result) => void
  wide: boolean
}

export function ResultsTable({ run, search, setSearch, evaluators, tags, results, onOpen, wide }: Props) {
  const [q, setQ] = useState(search.q ?? '')
  // Debounce full-text search into the URL.
  useEffect(() => {
    const t = setTimeout(() => {
      if ((search.q ?? '') !== q) setSearch({ q: q || undefined })
    }, 300)
    return () => clearTimeout(t)
  }, [q, search.q, setSearch])

  const rows = useMemo(() => results.data?.pages.flatMap((p) => p.items) ?? [], [results.data])
  const shownEvals = wide ? evaluators : evaluators.slice(0, 3)

  const columns = useMemo<ColumnDef<Result, unknown>[]>(
    () => [
      { id: 'status', header: '', size: 44, enableSorting: false, cell: ({ row }) => <StatusPill status={resultStatus(row.original)} size="sm" iconOnly /> },
      {
        id: 'input',
        header: 'Input',
        size: 220,
        meta: { grow: true },
        enableSorting: false,
        cell: ({ row }) => <span className="text-text">{preview(row.original.input)}</span>,
      },
      {
        id: 'output',
        header: 'Output',
        size: 220,
        meta: { grow: true },
        enableSorting: false,
        cell: ({ row }) =>
          row.original.error ? (
            <span className="font-mono text-xs text-fail">{row.original.error}</span>
          ) : (
            <span className="text-text-2">{row.original.output || '—'}</span>
          ),
      },
      ...shownEvals.map<ColumnDef<Result, unknown>>((e) => ({
        id: `ev:${e.id}`,
        header: () => (
          <span className="truncate" title={e.name}>
            {e.name}
          </span>
        ),
        size: 96,
        enableSorting: false,
        cell: ({ row }) => {
          const s = row.original.scores.find((x) => x.evaluator_id === e.id)
          if (!s) return <span className="text-text-muted">—</span>
          return <ScoreChip size="sm" value={s.value ?? null} passed={s.passed ?? undefined} threshold={e.threshold} label={s.value == null ? s.label : undefined} />
        },
      })),
      {
        id: 'latency',
        header: 'Latency',
        size: 84,
        enableSorting: false,
        meta: { align: 'right', numeric: true },
        cell: ({ row }) => <span className={cn('text-text-2', row.original.cached && 'text-text-muted')}>{row.original.cached ? 'cached' : formatDuration(row.original.latency_ms)}</span>,
      },
    ],
    [shownEvals],
  )

  const onScroll = (e: UIEvent<HTMLDivElement>) => {
    const el = e.target as HTMLElement
    if (el.scrollHeight - el.scrollTop - el.clientHeight < 600 && results.hasNextPage && !results.isFetchingNextPage) void results.fetchNextPage()
  }

  const status = search.status ?? 'all'
  const passedCount = Math.max(0, run.completed - run.failed - run.errored)
  const scoreActive = !!search.ev
  const anyFilter = status !== 'all' || !!search.ev || !!search.tag || !!search.q
  const ev = evaluators.find((e) => e.id === search.ev)

  return (
    <section className="flex flex-col gap-3" aria-label="Results">
      <div className="flex flex-wrap items-center gap-2">
        <h2 className="mr-2 text-md font-semibold text-text">Results</h2>
        <SegmentedControl<ResultFilter>
          size="sm"
          aria-label="Result status"
          value={status}
          onValueChange={(v) => setSearch({ status: v === 'all' ? undefined : v })}
          options={[
            { value: 'all', label: <span className="tabular">All {run.completed.toLocaleString()}</span> },
            { value: 'failed', label: <span className="tabular">Failed {run.failed.toLocaleString()}</span> },
            { value: 'passed', label: <span className="tabular">Passed {passedCount.toLocaleString()}</span> },
            ...(run.errored ? [{ value: 'errored' as const, label: <span className="tabular">Errored {run.errored.toLocaleString()}</span> }] : []),
          ]}
        />
        <Popover>
          <PopoverTrigger asChild>
            <button
              type="button"
              className={cn(
                'inline-flex h-7 items-center gap-1.5 rounded-md border px-2 text-sm transition-colors focus-ring [&_svg]:size-3.5',
                scoreActive ? 'border-accent/40 bg-accent-subtle text-text' : 'border-dashed border-border-strong text-text-2 hover:text-text',
              )}
            >
              <SlidersHorizontal className={scoreActive ? 'text-accent' : 'text-text-muted'} />
              {scoreActive ? (
                <span className="tabular">
                  {ev?.name ?? 'Evaluator'} {search.min ?? 0}–{search.max ?? 1}
                </span>
              ) : (
                'Score'
              )}
            </button>
          </PopoverTrigger>
          <PopoverContent className="w-64">
            <div className="flex flex-col gap-3">
              <label className="flex flex-col gap-1.5 text-xs text-text-muted">
                Evaluator
                <Select
                  size="sm"
                  aria-label="Evaluator"
                  value={search.ev}
                  placeholder="Any evaluator"
                  onValueChange={(v) => setSearch({ ev: v })}
                  options={evaluators.map((e) => ({ value: e.id, label: e.name }))}
                />
              </label>
              <div className="grid grid-cols-2 gap-2">
                <label className="flex flex-col gap-1.5 text-xs text-text-muted">
                  Min
                  <Input
                    size="sm"
                    inputMode="decimal"
                    disabled={!search.ev}
                    defaultValue={search.min ?? ''}
                    placeholder="0"
                    onBlur={(e) => setSearch({ min: e.target.value === '' ? undefined : Number(e.target.value) })}
                  />
                </label>
                <label className="flex flex-col gap-1.5 text-xs text-text-muted">
                  Max
                  <Input
                    size="sm"
                    inputMode="decimal"
                    disabled={!search.ev}
                    defaultValue={search.max ?? ''}
                    placeholder="1"
                    onBlur={(e) => setSearch({ max: e.target.value === '' ? undefined : Number(e.target.value) })}
                  />
                </label>
              </div>
              {ev?.threshold != null && (
                <Button size="sm" onClick={() => setSearch({ min: undefined, max: +(ev.threshold! - 0.001).toFixed(3) })}>
                  Below threshold ({ev.threshold.toFixed(2)})
                </Button>
              )}
              <p className="text-xs text-text-muted">With an evaluator chosen, “Failed” means failed on that evaluator.</p>
              {scoreActive && (
                <Button size="sm" variant="ghost" icon={<X />} onClick={() => setSearch({ ev: undefined, min: undefined, max: undefined })}>
                  Clear score filter
                </Button>
              )}
            </div>
          </PopoverContent>
        </Popover>
        {tags.length > 0 && (
          <Select
            size="sm"
            aria-label="Tag"
            className="w-36"
            value={search.tag ?? '__all'}
            onValueChange={(v) => setSearch({ tag: v === '__all' ? undefined : v })}
            options={[{ value: '__all', label: 'All tags' }, ...tags.map((t) => ({ value: t, label: t }))]}
          />
        )}
        <Input size="sm" value={q} onChange={(e) => setQ(e.target.value)} placeholder="Search input, output, errors…" leading={<Search />} aria-label="Search results" className="w-full sm:w-64" />
        {anyFilter && (
          <Button
            variant="ghost"
            size="sm"
            icon={<X />}
            onClick={() => {
              setQ('')
              setSearch({ status: undefined, ev: undefined, min: undefined, max: undefined, tag: undefined, q: undefined })
            }}
          >
            Clear
          </Button>
        )}
      </div>
      {results.error && !results.data ? (
        <div className="rounded-md border border-border bg-surface">
          <EmptyState
            compact
            illustration="error"
            title="Couldn’t load results"
            description={results.error.message}
            action={
              <Button size="sm" icon={<RotateCw />} onClick={() => results.refetch()}>
                Try again
              </Button>
            }
          />
        </div>
      ) : (
        <div onScrollCapture={onScroll}>
          <Table
            aria-label="Results"
            data={rows}
            columns={columns}
            getRowId={(r) => r.id}
            rowHeight={44}
            maxHeight={640}
            loading={results.isPending}
            activeRowId={search.result}
            onRowActivate={onOpen}
            className={cn('min-h-48 transition-opacity', results.isPlaceholderData && 'opacity-60')}
            empty={
              anyFilter ? (
                <EmptyState compact illustration="search" title="No results match" description="Loosen the filters to see more results." />
              ) : (
                <EmptyState
                  compact
                  illustration="runs"
                  title={run.status === 'queued' ? 'Waiting for a worker' : run.status === 'running' ? 'Results are on their way' : 'No results'}
                  description={run.status === 'queued' || run.status === 'running' ? 'Rows stream in here as soon as examples finish.' : 'This run produced no results.'}
                />
              )
            }
          />
        </div>
      )}
      <p className="tabular px-1 text-xs text-text-muted">
        {rows.length.toLocaleString()} shown{results.hasNextPage ? ' · scroll for more' : ''}
        {results.isFetchingNextPage ? ' · loading…' : ''} · <kbd className="font-mono">j</kbd>/<kbd className="font-mono">k</kbd> to move, Enter to open
      </p>
    </section>
  )
}
