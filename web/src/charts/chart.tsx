import { BarChart3, Table2 } from 'lucide-react'
import { useEffect, useLayoutEffect, useRef, useState, type ReactNode } from 'react'
import { SegmentedControl } from '@/components/ui/segmented-control'
import { Skeleton } from '@/components/ui/skeleton'
import { cn } from '@/lib/cn'
import { useTheme } from '@/lib/theme'
import { echarts, type EChartsOption } from './echarts'
import { buildTheme, readTokens, type ChartTokens } from './theme'

export type OptionInput = EChartsOption | ((tokens: ChartTokens) => EChartsOption)

export interface EChartProps {
  /** ECharts option, or a function of design tokens (preferred: colors stay token-driven). */
  option: OptionInput
  height?: number
  /** Accessible description of what the chart shows. */
  ariaLabel: string
  className?: string
  /** Event handlers, e.g. { click: (p) => … }. */
  onEvents?: Record<string, (params: unknown) => void>
}

/**
 * Low-level ECharts host. Builds its theme from tokens, re-themes on theme switch (no remount),
 * resizes with its container and disposes on unmount.
 */
export function EChart({ option, height = 240, ariaLabel, className, onEvents }: EChartProps) {
  const ref = useRef<HTMLDivElement>(null)
  const chartRef = useRef<echarts.EChartsType | null>(null)
  const { resolved } = useTheme()
  const optionRef = useRef(option)
  useLayoutEffect(() => {
    optionRef.current = option
  })

  // Init + resize + dispose.
  useEffect(() => {
    const el = ref.current
    if (!el) return
    const tokens = readTokens(el)
    const chart = echarts.init(el, buildTheme(tokens), { renderer: 'canvas' })
    chartRef.current = chart
    const ro = new ResizeObserver(() => chart.resize())
    ro.observe(el)
    return () => {
      ro.disconnect()
      chart.dispose()
      chartRef.current = null
    }
  }, [])

  // Theme + option updates.
  useEffect(() => {
    const el = ref.current
    const chart = chartRef.current
    if (!el || !chart) return
    const tokens = readTokens(el)
    chart.setTheme(buildTheme(tokens), { silent: true })
    const o = optionRef.current
    chart.setOption(
      {
        aria: { enabled: true, label: { description: ariaLabel } },
        animationDuration: 200,
        animationDurationUpdate: 160,
        animationEasing: 'cubicOut',
        ...(typeof o === 'function' ? o(tokens) : o),
      },
      { notMerge: true, lazyUpdate: true },
    )
  }, [option, resolved, ariaLabel])

  // Events.
  useEffect(() => {
    const chart = chartRef.current
    if (!chart || !onEvents) return
    for (const [name, fn] of Object.entries(onEvents)) chart.on(name, fn)
    return () => {
      for (const [name, fn] of Object.entries(onEvents)) chart.off(name, fn)
    }
  }, [onEvents])

  return <div ref={ref} role="img" aria-label={ariaLabel} className={cn('w-full', className)} style={{ height }} />
}

export interface ChartTable {
  columns: string[]
  rows: (string | number)[][]
}

export interface ChartFrameProps {
  title?: ReactNode
  description?: ReactNode
  /** Extra header controls (left of the chart/table toggle). */
  actions?: ReactNode
  /** Data shown by "View as table". */
  table?: ChartTable
  loading?: boolean
  /** Shown instead of the chart when there's no data. */
  empty?: ReactNode
  height?: number
  /** Render without card chrome (border/background). */
  bare?: boolean
  className?: string
  children: ReactNode
}

/** Card chrome around a chart: title, actions, and the "View as table" toggle. */
export function ChartFrame({ title, description, actions, table, loading, empty, height = 240, bare, className, children }: ChartFrameProps) {
  const [view, setView] = useState<'chart' | 'table'>('chart')
  return (
    <section className={cn(!bare && 'rounded-md border border-border bg-surface', 'flex min-w-0 flex-col', className)}>
      {(title || actions || table) && (
        <header className={cn('flex items-start justify-between gap-3', bare ? 'pb-3' : 'px-4 pb-2 pt-3.5')}>
          <div className="min-w-0">
            {title && <h3 className="truncate text-base font-semibold text-text">{title}</h3>}
            {description && <p className="mt-0.5 text-xs text-text-muted">{description}</p>}
          </div>
          <div className="flex shrink-0 items-center gap-2">
            {actions}
            {table && (
              <SegmentedControl
                size="sm"
                aria-label="Chart view"
                value={view}
                onValueChange={setView}
                options={[
                  { value: 'chart', label: null, icon: <BarChart3 />, ariaLabel: 'View as chart' },
                  { value: 'table', label: null, icon: <Table2 />, ariaLabel: 'View as table' },
                ]}
              />
            )}
          </div>
        </header>
      )}
      <div className={cn(!bare && 'px-4 pb-4')}>
        {loading ? (
          <Skeleton style={{ height }} className="w-full rounded-md" />
        ) : empty ? (
          <div style={{ minHeight: height }} className="flex items-center justify-center">
            {empty}
          </div>
        ) : view === 'table' && table ? (
          <div className="overflow-auto rounded-sm border border-border" style={{ maxHeight: Math.max(height, 200) }}>
            <table className="w-full text-sm">
              <thead className="sticky top-0 bg-surface-2">
                <tr>
                  {table.columns.map((c, i) => (
                    <th key={c} scope="col" className={cn('h-8 px-3 text-xs font-medium text-text-muted', i === 0 ? 'text-left' : 'text-right')}>
                      {c}
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {table.rows.map((r, ri) => (
                  <tr key={ri} className="border-t border-border">
                    {r.map((cell, ci) => (
                      <td key={ci} className={cn('h-8 px-3 text-text', ci === 0 ? 'text-left text-text-2' : 'tabular text-right')}>
                        {cell}
                      </td>
                    ))}
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : (
          children
        )}
      </div>
    </section>
  )
}
