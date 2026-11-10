import { useMemo } from 'react'
import { ChartFrame, EChart, type ChartFrameProps } from './chart'
import type { EChartsOption } from './echarts'
import { tooltipRow, tooltipTitle, type ChartTokens } from './theme'
import { formatValue, type ValueFormat } from './util'

export interface HeatmapProps extends Omit<ChartFrameProps, 'children' | 'table' | 'height'> {
  /** Row labels (e.g. evaluators). */
  rows: string[]
  /** Column labels (e.g. tags). */
  columns: string[]
  /** values[row][col]; null = no data. */
  values: (number | null)[][]
  format?: ValueFormat
  domain?: [number, number]
  /** Print values inside cells (default when the grid is small enough). */
  showValues?: boolean
  rowLabel?: string
  columnLabel?: string
  ariaLabel?: string
}

/** Matrix of magnitudes on the sequential iris ramp (evaluator × tag). 2px surface gaps between cells. */
export function Heatmap({
  rows,
  columns,
  values,
  format = 'score',
  domain = [0, 1],
  showValues,
  rowLabel = 'Evaluator',
  columnLabel = 'Tag',
  ariaLabel,
  ...frame
}: HeatmapProps) {
  const cellH = 32
  const height = rows.length * cellH + 64
  const labels = showValues ?? columns.length <= 12

  const option = useMemo(
    () =>
      (t: ChartTokens): EChartsOption => {
        const [lo, hi] = domain
        const data = values.flatMap((r, ri) =>
          columns.map((_, ci) => {
            const v = r[ci] ?? null
            const norm = v == null ? 0 : (v - lo) / (hi - lo || 1)
            return {
              value: [ci, rows.length - 1 - ri, v ?? '-'],
              label: { color: norm > 0.55 ? (t.mode === 'light' ? t.surface : t.bg) : t.text },
            }
          }),
        )
        return {
          grid: { top: 4, left: 4, right: 4, bottom: 44, outerBoundsMode: 'same', outerBoundsContain: 'axisLabel' },
          tooltip: {
            trigger: 'item',
            formatter: (p: unknown) => {
              const [ci, ri, v] = (p as { value: [number, number, number | '-'] }).value
              const row = rows[rows.length - 1 - ri]!
              return tooltipTitle(t, `${row} · ${columns[ci]}`) + tooltipRow(t, null, 'Score', v === '-' ? 'No data' : formatValue(v, format))
            },
          },
          xAxis: {
            type: 'category',
            data: columns,
            position: 'top',
            axisLine: { show: false },
            splitLine: { show: false },
            axisLabel: { color: t.text2, interval: 0, rotate: columns.length > 12 ? 30 : 0, fontSize: 11, overflow: 'truncate', width: 84 },
          },
          yAxis: {
            type: 'category',
            data: [...rows].reverse(),
            axisLine: { show: false },
            splitLine: { show: false },
            axisLabel: { color: t.text2, fontSize: 12, width: 140, overflow: 'truncate' },
          },
          visualMap: {
            min: lo,
            max: hi,
            calculable: false,
            orient: 'horizontal',
            left: 'right',
            bottom: 0,
            itemWidth: 10,
            itemHeight: 140,
            precision: 2,
            text: [formatValue(hi, format), formatValue(lo, format)],
            textGap: 6,
            inRange: { color: t.sequential },
            textStyle: { color: t.muted, fontSize: 11 },
          },
          series: [
            {
              type: 'heatmap',
              data,
              label: { show: labels, fontSize: 11, formatter: (p: { value: unknown }) => {
                const v = (p.value as [number, number, number | '-'])[2]
                return v === '-' ? '' : formatValue(v, format)
              } },
              itemStyle: { borderColor: t.surface, borderWidth: 2, borderRadius: 4 },
              emphasis: { itemStyle: { borderColor: t.text, borderWidth: 1 } },
            },
          ],
        }
      },
    [rows, columns, values, format, domain, labels],
  )

  const table = useMemo(
    () => ({
      columns: [`${rowLabel} \\ ${columnLabel}`, ...columns],
      rows: rows.map((r, ri) => [r, ...columns.map((_, ci) => (values[ri]?.[ci] != null ? formatValue(values[ri]![ci]!, format) : '—'))]),
    }),
    [rows, columns, values, format, rowLabel, columnLabel],
  )

  return (
    <ChartFrame {...frame} height={height} table={table}>
      <EChart option={option} height={height} ariaLabel={ariaLabel ?? `${rowLabel} by ${columnLabel} heatmap`} />
    </ChartFrame>
  )
}
