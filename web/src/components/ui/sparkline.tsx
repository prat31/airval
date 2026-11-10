import { useId } from 'react'
import { cn } from '@/lib/cn'

export interface SparklineProps {
  data: number[]
  width?: number
  height?: number
  /** Fixed domain; defaults to the data extent. */
  min?: number
  max?: number
  /** CSS color (token var). Defaults to the iris chart slot. */
  color?: string
  /** Area fill under the line. */
  area?: boolean
  /** Highlight the last point. */
  showLast?: boolean
  className?: string
  'aria-label'?: string
}

/**
 * Tiny inline SVG trend (no axes). Pure SVG so it is cheap enough for every table row and StatTile.
 */
export function Sparkline({
  data,
  width = 96,
  height = 28,
  min,
  max,
  color = 'var(--chart-1)',
  area = true,
  showLast = true,
  className,
  ...props
}: SparklineProps) {
  const gid = `spark${useId().replace(/[^a-zA-Z0-9_-]/g, '')}`
  if (data.length < 2) return <svg width={width} height={height} className={className} aria-hidden />
  const lo = min ?? Math.min(...data)
  const hi = max ?? Math.max(...data)
  const span = hi - lo || 1
  const pad = 3
  const pts = data.map((v, i) => {
    const x = (i / (data.length - 1)) * (width - pad * 2) + pad
    const y = height - pad - ((v - lo) / span) * (height - pad * 2)
    return [x, y] as const
  })
  const line = pts.map(([x, y], i) => `${i ? 'L' : 'M'}${x.toFixed(1)},${y.toFixed(1)}`).join(' ')
  const last = pts[pts.length - 1]!
  const first = pts[0]!
  return (
    <svg
      width={width}
      height={height}
      viewBox={`0 0 ${width} ${height}`}
      className={cn('shrink-0 overflow-visible', className)}
      role="img"
      aria-label={props['aria-label'] ?? `Trend: ${data.map((d) => +d.toFixed(2)).join(', ')}`}
    >
      {area && (
        <>
          <defs>
            <linearGradient id={gid} x1="0" x2="0" y1="0" y2="1">
              <stop offset="0%" style={{ stopColor: color, stopOpacity: 0.18 }} />
              <stop offset="100%" style={{ stopColor: color, stopOpacity: 0 }} />
            </linearGradient>
          </defs>
          <path d={`${line} L${last[0]},${height} L${first[0]},${height} Z`} fill={`url(#${gid})`} />
        </>
      )}
      <path d={line} fill="none" style={{ stroke: color }} strokeWidth={1.5} strokeLinecap="round" strokeLinejoin="round" />
      {showLast && <circle cx={last[0]} cy={last[1]} r={2.5} style={{ fill: color, stroke: 'var(--surface)' }} strokeWidth={1.5} />}
    </svg>
  )
}
