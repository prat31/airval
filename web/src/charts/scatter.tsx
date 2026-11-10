import { useMemo } from 'react'
import { ChartFrame, EChart, type ChartFrameProps } from './chart'
import type { EChartsOption } from './echarts'
import { tooltipRow, tooltipTitle, type ChartTokens } from './theme'
import { formatValue, type ValueFormat } from './util'

export interface ScatterPoint {
  id: string
  /** Score in run A (baseline). */
  a: number
  /** Score in run B (candidate). */
  b: number
  label?: string
}

export interface ScatterProps extends Omit<ChartFrameProps, 'children' | 'table'> {
  points: ScatterPoint[]
  labelA?: string
  labelB?: string
  format?: ValueFormat
  domain?: [number, number]
  /** |b − a| below this counts as unchanged. */
  tolerance?: number
  onPointClick?: (p: ScatterPoint) => void
  ariaLabel?: string
}

/**
 * A-vs-B per-example scores with the y = x diagonal. Points above improved (blue ▲), below regressed (red ▼),
 * near the diagonal unchanged (gray ●). Shape + legend carry the meaning, not color alone. Max 3 series.
 */
export function Scatter({
  points,
  labelA = 'Baseline',
  labelB = 'Candidate',
  format = 'score',
  domain = [0, 1],
  tolerance = 0.02,
  onPointClick,
  height = 320,
  ariaLabel,
  ...frame
}: ScatterProps) {
  const groups = useMemo(() => {
    const improved: ScatterPoint[] = []
    const regressed: ScatterPoint[] = []
    const same: ScatterPoint[] = []
    for (const p of points) (p.b - p.a > tolerance ? improved : p.a - p.b > tolerance ? regressed : same).push(p)
    return { improved, regressed, same }
  }, [points, tolerance])

  const option = useMemo(
    () =>
      (t: ChartTokens): EChartsOption => {
        const mk = (name: string, pts: ScatterPoint[], color: string, symbol: string, rotate = 0) => ({
          type: 'scatter' as const,
          name,
          symbol,
          symbolRotate: rotate,
          symbolSize: 9,
          color,
          itemStyle: { color, borderColor: t.surface, borderWidth: 1.5, opacity: 0.9 },
          emphasis: { scale: 1.4 },
          data: pts.map((p) => ({ value: [p.a, p.b], id: p.id, name: p.label ?? p.id })),
        })
        return {
          legend: { top: 0, left: 0 },
          grid: { top: 36, left: 8, right: 16, bottom: 8, outerBoundsMode: 'same', outerBoundsContain: 'all' },
          tooltip: {
            trigger: 'item',
            formatter: (p: unknown) => {
              const { data, seriesName } = p as { data: { value: [number, number]; name: string }; seriesName: string }
              const [a, b] = data.value
              const d = b - a
              return (
                tooltipTitle(t, data.name) +
                tooltipRow(t, null, labelA, formatValue(a, format)) +
                tooltipRow(t, null, labelB, formatValue(b, format)) +
                tooltipRow(t, null, `${seriesName}`, `${d > 0 ? '▲ +' : d < 0 ? '▼ −' : ''}${formatValue(Math.abs(d), format)}`)
              )
            },
          },
          xAxis: {
            type: 'value',
            name: labelA,
            nameLocation: 'middle',
            nameGap: 28,
            min: domain[0],
            max: domain[1],
            splitNumber: 5,
            axisLabel: { formatter: (v: number) => formatValue(v, format) },
          },
          yAxis: {
            type: 'value',
            name: labelB,
            nameLocation: 'middle',
            nameGap: 40,
            min: domain[0],
            max: domain[1],
            splitNumber: 5,
            axisLabel: { formatter: (v: number) => formatValue(v, format) },
          },
          series: [
            {
              ...mk('Unchanged', groups.same, t.diverging.mid === t.border ? t.borderStrong : t.muted, 'circle'),
              markLine: {
                silent: true,
                symbol: 'none',
                lineStyle: { color: t.borderStrong, type: [4, 3] as number[], width: 1 },
                label: { show: false },
                data: [[{ coord: [domain[0], domain[0]] }, { coord: [domain[1], domain[1]] }]],
              },
            },
            mk('Improved', groups.improved, t.diverging.neg, 'triangle'),
            mk('Regressed', groups.regressed, t.diverging.pos, 'triangle', 180),
          ] as EChartsOption['series'],
        }
      },
    [groups, labelA, labelB, format, domain],
  )

  const onEvents = useMemo(
    () =>
      onPointClick
        ? {
            click: (params: unknown) => {
              const id = (params as { data?: { id?: string } }).data?.id
              const p = points.find((x) => x.id === id)
              if (p) onPointClick(p)
            },
          }
        : undefined,
    [onPointClick, points],
  )

  const table = useMemo(
    () => ({
      columns: ['Example', labelA, labelB, 'Δ'],
      rows: [...points]
        .sort((x, y) => x.b - x.a - (y.b - y.a))
        .map((p) => [p.label ?? p.id, formatValue(p.a, format), formatValue(p.b, format), `${p.b - p.a >= 0 ? '+' : '−'}${formatValue(Math.abs(p.b - p.a), format)}`]),
    }),
    [points, labelA, labelB, format],
  )

  return (
    <ChartFrame {...frame} height={height} table={table}>
      <EChart
        option={option}
        height={height}
        onEvents={onEvents}
        ariaLabel={ariaLabel ?? `${labelB} vs ${labelA}: ${groups.improved.length} improved, ${groups.regressed.length} regressed, ${groups.same.length} unchanged`}
      />
    </ChartFrame>
  )
}
