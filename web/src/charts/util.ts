import { formatDuration } from '@/lib/format'

export type ValueFormat = 'percent' | 'score' | 'number' | 'ms' | 'usd'

export function formatValue(v: number, f: ValueFormat = 'number'): string {
  switch (f) {
    case 'percent':
      return `${+(v * 100).toFixed(1)}%`
    case 'score':
      return v.toFixed(2)
    case 'ms':
      return formatDuration(v)
    case 'usd':
      return `$${v < 1 ? v.toFixed(3) : v.toFixed(2)}`
    default:
      return Math.abs(v) >= 1000 ? new Intl.NumberFormat('en-US', { notation: 'compact', maximumFractionDigits: 1 }).format(v) : String(+v.toFixed(2))
  }
}

export interface Bin {
  x0: number
  x1: number
  count: number
}

/** Fixed-width bins over [min, max]. Values equal to max fall in the last bin. */
export function binValues(values: number[], bins: number, min?: number, max?: number): Bin[] {
  if (values.length === 0) return []
  const lo = min ?? Math.min(...values)
  const hi = max ?? Math.max(...values)
  const width = (hi - lo) / bins || 1
  const out: Bin[] = Array.from({ length: bins }, (_, i) => ({ x0: lo + i * width, x1: lo + (i + 1) * width, count: 0 }))
  for (const v of values) {
    if (v < lo || v > hi) continue
    const idx = Math.min(bins - 1, Math.floor((v - lo) / width))
    out[idx]!.count++
  }
  return out
}

export function quantile(sorted: number[], q: number): number {
  if (sorted.length === 0) return NaN
  const pos = (sorted.length - 1) * q
  const base = Math.floor(pos)
  const rest = pos - base
  const next = sorted[base + 1]
  return next !== undefined ? sorted[base]! + rest * (next - sorted[base]!) : sorted[base]!
}
