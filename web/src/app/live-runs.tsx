import { useQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { createContext, forwardRef, useContext, useMemo, useState, type ComponentPropsWithoutRef, type ReactNode } from 'react'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import { indexById, liveRunsQuery, suitesQuery } from '@/features/runs/api'
import { ProgressBar, runTitle } from '@/features/runs/ui'
import { cn } from '@/lib/cn'
import { formatPercent } from '@/lib/format'
import { useWorkspace } from './workspace'

/** Kept for compatibility; the indicator now reads running runs from the API directly. */
interface LiveRunsValue {
  running: number
  setRunning: (n: number) => void
}

const LiveRunsContext = createContext<LiveRunsValue>({ running: 0, setRunning: () => {} })

export function LiveRunsProvider({ children }: { children: ReactNode }) {
  const [running, setRunning] = useState(0)
  const value = useMemo(() => ({ running, setRunning }), [running])
  return <LiveRunsContext.Provider value={value}>{children}</LiveRunsContext.Provider>
}

export const useLiveRuns = () => useContext(LiveRunsContext)

export const LiveRunIndicatorView = forwardRef<HTMLButtonElement, { count: number; className?: string } & ComponentPropsWithoutRef<'button'>>(
  function LiveRunIndicatorView({ count, className, ...props }, ref) {
    return (
      <button
        ref={ref}
        type="button"
        className={cn(
          'inline-flex h-7 items-center gap-2 rounded-full border border-border bg-surface pl-2.5 pr-3 text-xs font-medium text-text-2 transition-colors hover:border-border-strong hover:text-text focus-ring',
          className,
        )}
        {...props}
      >
        <span className="relative flex size-2">
          <span className="absolute inline-flex size-full animate-ping rounded-full bg-running opacity-60 motion-reduce:hidden" />
          <span className="relative inline-flex size-2 rounded-full bg-running" />
        </span>
        <span className="tabular">{count}</span> running
      </button>
    )
  },
)

/** Top-bar pill: count of queued/running runs in this project, with a popover list. Hidden when idle. */
export function LiveRunIndicator({ org, project }: { org: string; project: string }) {
  const { project: p } = useWorkspace()
  const live = useQuery(liveRunsQuery(p?.id ?? ''))
  const suites = useQuery({ ...suitesQuery(p?.id ?? ''), enabled: (live.data?.length ?? 0) > 0 })
  const suiteById = useMemo(() => indexById(suites.data), [suites.data])
  const runs = live.data ?? []
  if (!runs.length) return null
  return (
    <Popover>
      <PopoverTrigger asChild>
        <LiveRunIndicatorView count={runs.length} aria-label={`${runs.length} runs in progress`} />
      </PopoverTrigger>
      <PopoverContent align="end" className="w-80 p-0">
        <div className="border-b border-border px-3 py-2 text-xs font-medium text-text-muted">In progress</div>
        <ul className="max-h-80 overflow-y-auto p-1">
          {runs.map((r) => (
            <li key={r.id}>
              <Link
                to="/o/$org/p/$project/runs/$run"
                params={{ org, project, run: r.id }}
                search={{}}
                className="flex flex-col gap-1.5 rounded-sm px-2 py-2 hover:bg-surface-hover focus-ring"
              >
                <span className="flex items-center justify-between gap-2">
                  <span className="truncate text-sm text-text">{runTitle(r, suiteById.get(r.suite_id ?? '')?.name)}</span>
                  <span className="tabular shrink-0 text-xs text-text-muted">
                    {r.status === 'queued' ? 'Queued' : formatPercent(r.total ? r.completed / r.total : 0, 0)}
                  </span>
                </span>
                <ProgressBar run={r} />
              </Link>
            </li>
          ))}
        </ul>
        <div className="border-t border-border p-1">
          <Link
            to="/o/$org/p/$project/runs"
            params={{ org, project }}
            search={{ status: 'running,queued' }}
            className="block rounded-sm px-2 py-1.5 text-center text-xs text-text-2 hover:bg-surface-hover hover:text-text focus-ring"
          >
            View all in Runs
          </Link>
        </div>
      </PopoverContent>
    </Popover>
  )
}
