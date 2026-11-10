import { useMemo } from 'react'
import { ChartFrame, EChart, type ChartFrameProps } from './chart'
import type { EChartsOption } from './echarts'
import { seriesColor, tooltipRow, tooltipTitle, type ChartTokens } from './theme'
import { binValues, formatValue, type Bin } from './util'

export interface HistogramSeries {
  name: string
  values: number[]
}

export interface HistogramProps extends Omit<ChartFrameProps, 'children' | 'table'> {
  /** One series (this run) or two (e.g. run vs baseline). */
  series: HistogramSeries[]
  bins?: number
  /** Domain; scores default to [0, 1]. */
  domain?: [number, number]
  /** Pass threshold drawn as a dashed rule. */
  threshold?: number
  /** x formatting. */
  format?: 'score' | 'percent' | 'number'
  ariaLabel?: string
}

/** Score distribution: fixed-width bins, 2px gaps, 4px rounded tops, optional threshold rule. */
export function Histogram({ series, bins = 20, domain = [0, 1], threshold, format = 'score', height = 220, ariaLabel, ...frame }: HistogramProps) {
  const binned = useMemo(() => series.map((s) => binValues(s.values, bins, domain[0], domain[1])), [series, bins, domain])

  const option = useMemo(
    () =>
      (t: ChartTokens): EChartsOption => {
        const n = series.length
        return {
          legend: n > 1 ? { top: 0, left: 0 } : { show: false },
          grid: { top: n > 1 ? 40 : 24, left: 4, right: 12, bottom: 4, outerBoundsMode: 'same', outerBoundsContain: 'axisLabel' },
          tooltip: {
            trigger: 'item',
            formatter: (p: unknown) => {
              const { dataIndex } = p as { dataIndex: number }
              const b = binned[0]![dataIndex]!
              return (
                tooltipTitle(t, `${formatValue(b.x0, format)} – ${formatValue(b.x1, format)}`) +
                series.map((s, i) => tooltipRow(t, seriesColor(t, i), s.name, String(binned[i]![dataIndex]!.count))).join('')
              )
            },
          },
          xAxis: {
            type: 'value',
            min: domain[0],
            max: domain[1],
            splitNumber: 5,
            axisLabel: { formatter: (v: number) => formatValue(v, format) },
            splitLine: { show: false },
          },
          yAxis: { type: 'value', minInterval: 1, splitNumber: 4 },
          series: series.map((s, si) => ({
            type: 'custom',
            name: s.name,
            color: seriesColor(t, si),
            encode: { x: [0, 1], y: 2, tooltip: 2 },
            data: binned[si]!.map((b: Bin) => [b.x0, b.x1, b.count]),
            renderItem: (_params: unknown, api: CustomApi) => {
              const x0 = api.value(0) as number
              const x1 = api.value(1) as number
              const count = api.value(2) as number
              const [px0, py] = api.coord([x0, count]) as [number, number]
              const [px1, pBase] = api.coord([x1, 0]) as [number, number]
              const slot = (px1 - px0) / n
              const left = px0 + slot * si + 1 // 1px each side = 2px gap between adjacent fills
              const width = Math.max(1, slot - 2)
              const h = pBase - py
              if (count === 0) return null
              return {
                type: 'rect',
                shape: { x: left, y: py, width, height: h, r: [Math.min(4, width / 2), Math.min(4, width / 2), 0, 0] },
                style: { fill: seriesColor(t, si) },
                emphasis: { style: { opacity: 0.85 } },
              }
            },
            markLine:
              threshold != null && si === 0
                ? {
                    silent: true,
                    symbol: 'none',
                    lineStyle: { color: t.text2, type: [4, 3], width: 1 },
                    label: { formatter: `threshold ${formatValue(threshold, format)}`, color: t.text2, fontSize: 11, position: 'end', distance: 4 },
                    data: [{ xAxis: threshold }],
                  }
                : undefined,
          })) as EChartsOption['series'],
        }
      },
    [binned, series, domain, threshold, format],
  )

  const table = useMemo(
    () => ({
      columns: ['Bin', ...series.map((s) => s.name)],
      rows: binned[0]?.map((b, i) => [`${formatValue(b.x0, format)} – ${formatValue(b.x1, format)}`, ...binned.map((bs) => bs[i]!.count)]) ?? [],
    }),
    [binned, series, format],
  )

  return (
    <ChartFrame {...frame} height={height} table={table}>
      <EChart option={option} height={height} ariaLabel={ariaLabel ?? `Histogram of ${series.map((s) => s.name).join(' and ')}`} />
    </ChartFrame>
  )
}

/** Subset of the ECharts custom-series API we use. */
export interface CustomApi {
  value: (dim: number) => number | string
  coord: (point: number[]) => number[]
  size?: (v: number[]) => number[]
}
