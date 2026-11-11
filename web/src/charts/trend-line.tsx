import { useMemo } from 'react'
import { formatDateTime } from '@/lib/format'
import { ChartFrame, EChart, type ChartFrameProps } from './chart'
import type { EChartsOption } from './echarts'
import { MAX_SERIES, seriesColor, tooltipRow, tooltipTitle, withAlpha, type ChartTokens } from './theme'
import { formatValue, type ValueFormat } from './util'

export interface TrendPoint {
  t: Date | number | string
  v: number | null
}

export interface TrendSeries {
  /** Stable id: color follows the entity, never its rank. Pass `colorIndex` to pin a slot. */
  name: string
  points: TrendPoint[]
  colorIndex?: number
}

export interface TrendLineProps extends Omit<ChartFrameProps, 'children' | 'table'> {
  series: TrendSeries[]
  format?: ValueFormat
  /** Fixed y domain, e.g. [0, 1] for pass rates. */
  yDomain?: [number, number]
  /** Dashed reference rule (e.g. gate threshold). */
  threshold?: { value: number; label?: string }
  /** Subtle area under a single series. */
  area?: boolean
  ariaLabel?: string
}

/** Multi-series line over time: 2px lines, crosshair tooltip, legend for ≥2 and direct end labels for ≤4. */
export function TrendLine({ series, format = 'percent', yDomain, threshold, area = true, height = 240, ariaLabel, ...frame }: TrendLineProps) {
  if (series.length > MAX_SERIES) {
    console.warn(`TrendLine: ${series.length} series exceeds ${MAX_SERIES}; extra series are dropped. Fold them into "Other".`)
  }
  const shown = series.slice(0, MAX_SERIES)

  const option = useMemo(
    () =>
      (t: ChartTokens): EChartsOption => {
        const multi = shown.length > 1
        const direct = shown.length <= 4 && multi
        // Reserve room for direct end labels (≈6.5px per char at 11px).
        const labelRoom = Math.min(160, Math.max(...shown.map((s) => s.name.length)) * 6.5 + 14)
        return {
          legend: multi ? { top: 0, left: 0, data: shown.map((s) => s.name) } : { show: false },
          grid: { top: multi ? 36 : 16, left: 4, right: direct ? labelRoom : 16, bottom: 4, outerBoundsMode: 'same', outerBoundsContain: 'axisLabel' },
          tooltip: {
            trigger: 'axis',
            axisPointer: { type: 'line' },
            formatter: (params: unknown) => {
              const ps = params as { axisValue: number; seriesName: string; color: string; value: [number, number | null] }[]
              if (!ps.length) return ''
              return (
                tooltipTitle(t, formatDateTime(new Date(ps[0]!.axisValue))) +
                ps
                  .filter((p) => p.value[1] != null)
                  .sort((a, b) => (b.value[1] ?? 0) - (a.value[1] ?? 0))
                  .map((p) => tooltipRow(t, p.color, p.seriesName, formatValue(p.value[1]!, format)))
                  .join('')
              )
            },
          },
          xAxis: { type: 'time', axisLabel: { hideOverlap: true } },
          yAxis: {
            type: 'value',
            min: yDomain?.[0],
            max: yDomain?.[1],
            splitNumber: 4,
            // A fixed 0–1 percent domain gets even 25% steps (splitNumber alone yields 0/30/60/90/100).
            ...(format === 'percent' && yDomain?.[0] === 0 && yDomain?.[1] === 1 ? { interval: 0.25 } : {}),
            axisLabel: { formatter: (v: number) => formatValue(v, format) },
          },
          series: shown.map((s, i) => {
            const color = seriesColor(t, s.colorIndex ?? i)
            return {
              type: 'line' as const,
              name: s.name,
              color,
              showSymbol: false,
              symbolSize: 8,
              connectNulls: false,
              lineStyle: { width: 2 },
              emphasis: { focus: 'series' as const },
              data: s.points.map((p) => [new Date(p.t).getTime(), p.v]),
              endLabel: direct ? { show: true, formatter: '{a}', color: t.text2, fontSize: 11, distance: 6 } : undefined,
              labelLayout: direct ? { moveOverlap: 'shiftY' as const } : undefined,
              areaStyle:
                area && !multi
                  ? {
                      color: {
                        type: 'linear' as const,
                        x: 0,
                        y: 0,
                        x2: 0,
                        y2: 1,
                        colorStops: [
                          { offset: 0, color: withAlpha(color, 0.16) },
                          { offset: 1, color: withAlpha(color, 0) },
                        ],
                      },
                    }
                  : undefined,
              markLine:
                threshold && i === 0
                  ? {
                      silent: true,
                      symbol: 'none',
                      lineStyle: { color: t.muted, type: [4, 3] as number[], width: 1 },
                      label: { formatter: threshold.label ?? formatValue(threshold.value, format), color: t.text2, fontSize: 11, position: 'insideStartTop' as const },
                      data: [{ yAxis: threshold.value }],
                    }
                  : undefined,
            }
          }),
        }
      },
    [shown, format, yDomain, threshold, area],
  )

  const table = useMemo(() => {
    const times = [...new Set(shown.flatMap((s) => s.points.map((p) => new Date(p.t).getTime())))].sort((a, b) => a - b)
    const lookup = shown.map((s) => new Map(s.points.map((p) => [new Date(p.t).getTime(), p.v])))
    return {
      columns: ['Time', ...shown.map((s) => s.name)],
      rows: times.map((tm) => [formatDateTime(new Date(tm)), ...lookup.map((m) => (m.get(tm) != null ? formatValue(m.get(tm)!, format) : '—'))]),
    }
  }, [shown, format])

  return (
    <ChartFrame {...frame} height={height} table={table}>
      <EChart option={option} height={height} ariaLabel={ariaLabel ?? `Trend of ${shown.map((s) => s.name).join(', ')} over time`} />
    </ChartFrame>
  )
}
