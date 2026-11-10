import { useMemo } from 'react'
import { ChartFrame, EChart, type ChartFrameProps } from './chart'
import type { EChartsOption } from './echarts'
import { seriesColor, tooltipRow, tooltipTitle, type ChartTokens } from './theme'
import { formatValue, type ValueFormat } from './util'

export interface BarItem {
  label: string
  value: number
  /** Sample size, shown in the tooltip/table. */
  n?: number
}

export interface HorizontalBarsProps extends Omit<ChartFrameProps, 'children' | 'table' | 'height'> {
  items: BarItem[]
  format?: ValueFormat
  /** Domain max (default 1 for percent/score). */
  max?: number
  sort?: 'desc' | 'asc' | 'none'
  threshold?: number
  /** Value labels at bar ends (default on). */
  showValues?: boolean
  /** Name of the measure, e.g. "Pass rate". */
  measure?: string
  ariaLabel?: string
}

/** Ranked horizontal bars (e.g. pass rate by tag). Height grows with the number of bars. */
export function HorizontalBars({
  items,
  format = 'percent',
  max,
  sort = 'desc',
  threshold,
  showValues = true,
  measure = 'Pass rate',
  ariaLabel,
  ...frame
}: HorizontalBarsProps) {
  const sorted = useMemo(() => {
    const copy = [...items]
    if (sort !== 'none') copy.sort((a, b) => (sort === 'desc' ? b.value - a.value : a.value - b.value))
    return copy.reverse() // ECharts draws category axis bottom-up
  }, [items, sort])
  const domainMax = max ?? (format === 'percent' || format === 'score' ? 1 : undefined)
  const height = Math.max(120, sorted.length * 28 + 32)

  const option = useMemo(
    () =>
      (t: ChartTokens): EChartsOption => ({
        grid: { top: 8, left: 4, right: showValues ? 48 : 16, bottom: 4, outerBoundsMode: 'same', outerBoundsContain: 'axisLabel' },
        tooltip: {
          trigger: 'item',
          formatter: (p: unknown) => {
            const it = sorted[(p as { dataIndex: number }).dataIndex]!
            return tooltipTitle(t, it.label) + tooltipRow(t, seriesColor(t, 0), measure, formatValue(it.value, format)) + (it.n != null ? tooltipRow(t, null, 'Results', String(it.n)) : '')
          },
        },
        xAxis: { type: 'value', max: domainMax, min: 0, splitNumber: 4, interval: domainMax === 1 ? 0.25 : undefined, axisLabel: { formatter: (v: number) => formatValue(v, format) } },
        yAxis: {
          type: 'category',
          data: sorted.map((s) => s.label),
          axisLine: { show: false },
          axisLabel: { color: t.text2, fontSize: 12, width: 120, overflow: 'truncate' },
        },
        series: [
          {
            type: 'bar',
            name: measure,
            data: sorted.map((s) => s.value),
            barWidth: 14,
            color: seriesColor(t, 0),
            itemStyle: { borderRadius: [0, 4, 4, 0] },
            showBackground: true,
            backgroundStyle: { color: t.surface2, borderRadius: [0, 4, 4, 0] },
            label: showValues
              ? { show: true, position: 'right', color: t.text2, fontSize: 11, formatter: (p: { value: unknown }) => formatValue(p.value as number, format) }
              : undefined,
            markLine:
              threshold != null
                ? { silent: true, symbol: 'none', lineStyle: { color: t.text2, type: [4, 3], width: 1 }, label: { show: false }, data: [{ xAxis: threshold }] }
                : undefined,
          },
        ],
      }),
    [sorted, format, domainMax, threshold, showValues, measure],
  )

  const table = useMemo(
    () => ({
      columns: ['Label', measure, ...(items.some((i) => i.n != null) ? ['Results'] : [])],
      rows: [...sorted].reverse().map((s) => [s.label, formatValue(s.value, format), ...(s.n != null ? [s.n] : [])]),
    }),
    [sorted, format, measure, items],
  )

  return (
    <ChartFrame {...frame} height={height} table={table}>
      <EChart option={option} height={height} ariaLabel={ariaLabel ?? `${measure} by category`} />
    </ChartFrame>
  )
}
