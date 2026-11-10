import { useMemo } from 'react'
import { ChartFrame, EChart, type ChartFrameProps } from './chart'
import type { EChartsOption } from './echarts'
import type { CustomApi } from './histogram'
import { seriesColor, tooltipRow, tooltipTitle, type ChartTokens } from './theme'
import { binValues, formatValue, quantile } from './util'

export interface LatencyDistributionProps extends Omit<ChartFrameProps, 'children' | 'table'> {
  /** Latencies in milliseconds. */
  values: number[]
  bins?: number
  /** Clip the x domain at this quantile to keep long tails readable (default p99.5). */
  clipQuantile?: number
  ariaLabel?: string
}

/** Latency histogram with p50 / p95 / p99 rules. */
export function LatencyDistribution({ values, bins = 30, clipQuantile = 0.995, height = 220, ariaLabel, ...frame }: LatencyDistributionProps) {
  const stats = useMemo(() => {
    const sorted = [...values].sort((a, b) => a - b)
    const max = quantile(sorted, clipQuantile)
    return {
      p50: quantile(sorted, 0.5),
      p95: quantile(sorted, 0.95),
      p99: quantile(sorted, 0.99),
      bins: binValues(values, bins, 0, max),
      max,
    }
  }, [values, bins, clipQuantile])

  const option = useMemo(
    () =>
      (t: ChartTokens): EChartsOption => {
        const marker = (name: string, value: number, strong: boolean) => ({
          xAxis: value,
          name,
          lineStyle: { color: strong ? t.text : t.text2, type: strong ? ('solid' as const) : ([4, 3] as number[]), width: 1 },
          label: {
            formatter: `${name} ${formatValue(value, 'ms')}`,
            color: strong ? t.text : t.text2,
            fontSize: 11,
            fontWeight: strong ? 600 : 400,
            position: 'end' as const,
            distance: 4,
          },
        })
        return {
          grid: { top: 28, left: 4, right: 20, bottom: 4, outerBoundsMode: 'same', outerBoundsContain: 'axisLabel' },
          tooltip: {
            trigger: 'item',
            formatter: (p: unknown) => {
              const b = stats.bins[(p as { dataIndex: number }).dataIndex]!
              return tooltipTitle(t, `${formatValue(b.x0, 'ms')} – ${formatValue(b.x1, 'ms')}`) + tooltipRow(t, seriesColor(t, 0), 'Results', String(b.count))
            },
          },
          xAxis: {
            type: 'value',
            min: 0,
            max: stats.max,
            splitNumber: 6,
            axisLabel: { formatter: (v: number) => formatValue(v, 'ms'), hideOverlap: true, showMaxLabel: false },
            splitLine: { show: false },
          },
          yAxis: { type: 'value', minInterval: 1, splitNumber: 4 },
          series: [
            {
              type: 'custom',
              name: 'Latency',
              data: stats.bins.map((b) => [b.x0, b.x1, b.count]),
              encode: { x: [0, 1], y: 2 },
              renderItem: (_p: unknown, api: CustomApi) => {
                const count = api.value(2) as number
                if (!count) return null
                const [px0, py] = api.coord([api.value(0) as number, count]) as [number, number]
                const [px1, base] = api.coord([api.value(1) as number, 0]) as [number, number]
                const w = Math.max(1, px1 - px0 - 2)
                return {
                  type: 'rect',
                  shape: { x: px0 + 1, y: py, width: w, height: base - py, r: [Math.min(4, w / 2), Math.min(4, w / 2), 0, 0] },
                  style: { fill: seriesColor(t, 0) },
                }
              },
              markLine: {
                silent: true,
                symbol: 'none',
                data: [marker('p50', stats.p50, true), marker('p95', stats.p95, false), marker('p99', stats.p99, false)],
              },
            },
          ] as EChartsOption['series'],
        }
      },
    [stats],
  )

  const table = useMemo(
    () => ({
      columns: ['Latency', 'Results'],
      rows: [
        ['p50', formatValue(stats.p50, 'ms')],
        ['p95', formatValue(stats.p95, 'ms')],
        ['p99', formatValue(stats.p99, 'ms')],
        ...stats.bins.map((b) => [`${formatValue(b.x0, 'ms')} – ${formatValue(b.x1, 'ms')}`, b.count]),
      ],
    }),
    [stats],
  )

  return (
    <ChartFrame {...frame} height={height} table={table}>
      <EChart
        option={option}
        height={height}
        ariaLabel={ariaLabel ?? `Latency distribution, p50 ${formatValue(stats.p50, 'ms')}, p95 ${formatValue(stats.p95, 'ms')}, p99 ${formatValue(stats.p99, 'ms')}`}
      />
    </ChartFrame>
  )
}
