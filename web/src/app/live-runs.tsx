import { Link } from '@tanstack/react-router'
import { createContext, useContext, useMemo, useState, type ReactNode } from 'react'
import { Tooltip } from '@/components/ui/tooltip'
import { cn } from '@/lib/cn'

/**
 * Live-run indicator slot. The SSE run-events stream (milestone 6) will call `setRunning`.
 * The indicator hides itself when nothing is running.
 */
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

export function LiveRunIndicatorView({ count, className }: { count: number; className?: string }) {
  return (
    <span
      className={cn(
        'inline-flex h-7 items-center gap-2 rounded-full border border-border bg-surface pl-2.5 pr-3 text-xs font-medium text-text-2',
        className,
      )}
    >
      <span className="relative flex size-2">
        <span className="absolute inline-flex size-full animate-ping rounded-full bg-running opacity-60 motion-reduce:hidden" />
        <span className="relative inline-flex size-2 rounded-full bg-running" />
      </span>
      <span className="tabular">{count}</span> running
    </span>
  )
}

export function LiveRunIndicator({ org, project }: { org: string; project: string }) {
  const { running } = useLiveRuns()
  if (running <= 0) return null
  return (
    <Tooltip content="View running runs">
      <Link to="/o/$org/p/$project/runs" params={{ org, project }} className="rounded-full focus-ring">
        <LiveRunIndicatorView count={running} />
      </Link>
    </Tooltip>
  )
}
