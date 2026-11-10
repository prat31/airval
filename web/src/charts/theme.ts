/**
 * ECharts themes generated from design tokens (tokens.css). Nothing here hard-codes a color:
 * values are read from CSS variables so light/dark (and any [data-theme] subtree) stay in sync.
 * Rules: plan/001.md §12.3–12.4.
 */
import { echarts } from './echarts'

export interface ChartTokens {
  mode: 'light' | 'dark'
  text: string
  text2: string
  muted: string
  border: string
  borderStrong: string
  surface: string
  surface2: string
  bg: string
  accent: string
  /** Categorical slots 1–6, fixed order, never cycled. */
  categorical: string[]
  /** Sequential iris ramp, low → high magnitude (already oriented for the mode). */
  sequential: string[]
  /** Diverging: improvement (blue) ← midpoint gray → regression (red). */
  diverging: { neg: string; mid: string; pos: string }
  status: { pass: string; fail: string; warn: string }
  fontSans: string
  fontMono: string
}

export const MAX_SERIES = 6
const IRIS_STEPS = [50, 100, 200, 300, 400, 500, 600, 700, 800, 900]

export function readTokens(el: Element = document.documentElement): ChartTokens {
  const cs = getComputedStyle(el)
  const v = (name: string) => cs.getPropertyValue(name).trim()
  const themed = el.closest('[data-theme]')?.getAttribute('data-theme')
  const mode: 'light' | 'dark' = themed === 'dark' ? 'dark' : themed === 'light' ? 'light' : matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light'
  const iris = IRIS_STEPS.map((s) => v(`--iris-${s}`))
  return {
    mode,
    text: v('--text'),
    text2: v('--text-2'),
    muted: v('--text-muted'),
    border: v('--border'),
    borderStrong: v('--border-strong'),
    surface: v('--surface'),
    surface2: v('--surface-2'),
    bg: v('--bg'),
    accent: v('--accent'),
    categorical: [1, 2, 3, 4, 5, 6].map((i) => v(`--chart-${i}`)),
    // Light: pale → deep. Dark: deep → pale, so higher magnitude always means more contrast with the surface.
    sequential: mode === 'light' ? iris.slice(0, 9) : [...iris].reverse().slice(0, 9),
    diverging: { neg: v('--div-neg'), mid: v('--div-mid'), pos: v('--div-pos') },
    status: { pass: v('--pass'), fail: v('--fail'), warn: v('--warn') },
    fontSans: v('--font-sans'),
    fontMono: v('--font-mono'),
  }
}

/** Build the ECharts theme object from tokens. */
export function buildTheme(t: ChartTokens) {
  const axisCommon = {
    axisLine: { show: true, lineStyle: { color: t.borderStrong, width: 1 } },
    axisTick: { show: false },
    axisLabel: { color: t.muted, fontSize: 11, fontFamily: t.fontSans, margin: 10 },
    splitLine: { show: true, lineStyle: { color: t.border, width: 1, type: 'solid' as const } },
    splitArea: { show: false },
    nameTextStyle: { color: t.muted, fontSize: 11 },
  }
  return {
    color: t.categorical,
    backgroundColor: 'transparent',
    textStyle: { fontFamily: t.fontSans, color: t.text2, fontSize: 12 },
    title: { textStyle: { color: t.text, fontSize: 13, fontWeight: 600 }, subtextStyle: { color: t.muted } },
    grid: { left: 4, right: 16, top: 16, bottom: 4, outerBoundsMode: 'same', outerBoundsContain: 'axisLabel' },
    line: {
      lineStyle: { width: 2 },
      symbol: 'circle',
      symbolSize: 8,
      showSymbol: false,
      smooth: false,
      emphasis: { focus: 'series', lineStyle: { width: 2 } },
    },
    bar: { itemStyle: { borderRadius: [4, 4, 0, 0] }, barCategoryGap: '24%', barGap: '8%' },
    scatter: { symbolSize: 8, itemStyle: { borderColor: t.surface, borderWidth: 1.5, opacity: 0.9 } },
    heatmap: { itemStyle: { borderColor: t.surface, borderWidth: 2, borderRadius: 4 } },
    categoryAxis: { ...axisCommon, splitLine: { show: false } },
    valueAxis: { ...axisCommon, axisLine: { show: false } },
    timeAxis: { ...axisCommon, splitLine: { show: false } },
    logAxis: { ...axisCommon, axisLine: { show: false } },
    legend: {
      icon: 'roundRect',
      itemWidth: 10,
      itemHeight: 10,
      itemGap: 16,
      textStyle: { color: t.text2, fontSize: 12 },
      inactiveColor: t.borderStrong,
      pageTextStyle: { color: t.muted },
    },
    tooltip: {
      backgroundColor: t.surface,
      borderColor: t.border,
      borderWidth: 1,
      padding: [8, 10],
      textStyle: { color: t.text, fontSize: 12, fontFamily: t.fontSans },
      extraCssText: `border-radius:8px;box-shadow:var(--elevation-popover);`,
      axisPointer: {
        lineStyle: { color: t.borderStrong, width: 1 },
        crossStyle: { color: t.borderStrong, width: 1 },
        shadowStyle: { color: t.surface2, opacity: 0.6 },
        label: { backgroundColor: t.text, color: t.surface },
      },
    },
    visualMap: { textStyle: { color: t.muted, fontSize: 11 }, inRange: { color: t.sequential } },
    markLine: { lineStyle: { color: t.muted }, label: { color: t.text2 } },
  }
}

/** Register `airval-light` / `airval-dark` by reading tokens from detached themed probes. */
export function registerAirvalThemes() {
  for (const mode of ['light', 'dark'] as const) {
    const probe = document.createElement('div')
    probe.dataset.theme = mode
    probe.style.display = 'none'
    document.body.appendChild(probe)
    echarts.registerTheme(`airval-${mode}`, buildTheme(readTokens(probe)))
    probe.remove()
  }
}

export function seriesColor(t: ChartTokens, index: number): string {
  if (index >= MAX_SERIES) {
    throw new Error(`Charts support at most ${MAX_SERIES} series; fold the rest into "Other" or use small multiples.`)
  }
  return t.categorical[index]!
}

/** Tooltip row HTML: colored key + name + right-aligned value (text stays in text tokens). */
export function tooltipRow(t: ChartTokens, color: string | null, name: string, value: string) {
  const key = color ? `<span style="display:inline-block;width:8px;height:8px;border-radius:2px;background:${color};margin-right:8px"></span>` : ''
  return `<div style="display:flex;align-items:center;justify-content:space-between;gap:16px;line-height:20px"><span style="display:flex;align-items:center;color:${t.text2}">${key}${name}</span><span style="font-variant-numeric:tabular-nums;font-weight:500;color:${t.text}">${value}</span></div>`
}

export function tooltipTitle(t: ChartTokens, title: string) {
  return `<div style="color:${t.muted};font-size:11px;margin-bottom:4px">${title}</div>`
}

/** Apply alpha to a token color (hex or rgb()). Canvas can't parse color-mix(), so do it here. */
export function withAlpha(color: string, alpha: number): string {
  const c = color.trim()
  const hex = c.match(/^#([0-9a-f]{3}|[0-9a-f]{6})$/i)
  if (hex) {
    const h = hex[1]!.length === 3 ? hex[1]!.replace(/./g, (x) => x + x) : hex[1]!
    const n = parseInt(h, 16)
    return `rgba(${(n >> 16) & 255}, ${(n >> 8) & 255}, ${n & 255}, ${alpha})`
  }
  const rgb = c.match(/^rgba?\(([^)]+)\)$/i)
  if (rgb) {
    const [r, g, b] = rgb[1]!.split(/[\s,/]+/).filter(Boolean)
    return `rgba(${r}, ${g}, ${b}, ${alpha})`
  }
  return c
}
