import { ArrowDownRight, ArrowUpRight, Minus } from 'lucide-react'
import type { ReactNode } from 'react'
import { cn } from '@/lib/cn'
import { Skeleton } from './skeleton'
import { Sparkline } from './sparkline'

export interface StatTileProps {
  label: ReactNode
  value: ReactNode
  /** Unit or suffix shown after the value in muted text. */
  unit?: ReactNode
  /** Change vs. the comparison period. Formatted text + direction. */
  delta?: {
    value: string
    direction: 'up' | 'down' | 'flat'
    /** Whether "up" is good (pass rate) or bad (latency, cost). Default true. */
    upIsGood?: boolean
    /** Comparison context, e.g. "vs previous run". */
    context?: string
  }
  trend?: number[]
  /** Optional icon in the label row. */
  icon?: ReactNode
  loading?: boolean
  className?: string
}

/** One headline number. Prefer this over a chart when the job is one number. */
export function StatTile({ label, value, unit, delta, trend, icon, loading, className }: StatTileProps) {
  const good = delta ? (delta.direction === 'flat' ? null : (delta.direction === 'up') === (delta.upIsGood ?? true)) : null
  const DeltaIcon = delta?.direction === 'up' ? ArrowUpRight : delta?.direction === 'down' ? ArrowDownRight : Minus
  return (
    <div className={cn('flex min-w-0 flex-col gap-2 rounded-md border border-border bg-surface px-4 py-3.5', className)}>
      <div className="flex items-center gap-1.5 text-sm text-text-2 [&_svg]:size-3.5 [&_svg]:text-text-muted">
        {icon}
        <span className="truncate">{label}</span>
      </div>
      {loading ? (
        <div className="flex flex-col gap-2 pt-1">
          <Skeleton className="h-7 w-24" />
          <Skeleton className="h-3 w-32" />
        </div>
      ) : (
        <div className="flex flex-col gap-1">
          <div className="flex items-end justify-between gap-3">
            <div className="tabular flex min-w-0 items-baseline gap-1 text-xl font-semibold tracking-tight text-text">
              {value}
              {unit && <span className="text-sm font-normal text-text-muted">{unit}</span>}
            </div>
            {trend && trend.length > 1 && <Sparkline data={trend} width={80} height={28} className="mb-1" />}
          </div>
          {delta && (
            <div className="flex min-w-0 items-center gap-1.5 whitespace-nowrap text-xs">
              <span
                className={cn(
                  'tabular inline-flex items-center gap-0.5 font-medium',
                  good === null ? 'text-text-muted' : good ? 'text-pass' : 'text-fail',
                )}
              >
                <DeltaIcon className="size-3.5" aria-hidden />
                {delta.value}
              </span>
              {delta.context && <span className="truncate text-text-muted">{delta.context}</span>}
            </div>
          )}
        </div>
      )}
    </div>
  )
}
