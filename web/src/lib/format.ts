const nf = (opts: Intl.NumberFormatOptions) => new Intl.NumberFormat('en-US', opts)

const pct0 = nf({ style: 'percent', maximumFractionDigits: 0 })
const pct1 = nf({ style: 'percent', minimumFractionDigits: 1, maximumFractionDigits: 1 })
const compact = nf({ notation: 'compact', maximumFractionDigits: 1 })
const usd = nf({ style: 'currency', currency: 'USD', maximumFractionDigits: 2 })
const usdSmall = nf({ style: 'currency', currency: 'USD', maximumFractionDigits: 4 })

export const formatPercent = (v: number, digits: 0 | 1 = 1) => (digits === 0 ? pct0 : pct1).format(v)
export const formatScore = (v: number) => v.toFixed(2)
export const formatCompact = (v: number) => compact.format(v)
export const formatUsd = (v: number) => (Math.abs(v) < 1 && v !== 0 ? usdSmall : usd).format(v)

export function formatDuration(ms: number): string {
  if (ms < 1000) return `${Math.round(ms)} ms`
  const s = ms / 1000
  if (s < 60) return `${s.toFixed(s < 10 ? 1 : 0)} s`
  const m = Math.floor(s / 60)
  const rs = Math.round(s % 60)
  if (m < 60) return `${m}m ${rs}s`
  return `${Math.floor(m / 60)}h ${m % 60}m`
}

/** Signed delta, e.g. "+2.4 pts" for percentages or "+0.03" for scores. */
export function formatDelta(v: number, kind: 'percent' | 'score' | 'number' = 'number'): string {
  const sign = v > 0 ? '+' : v < 0 ? '−' : '±'
  const abs = Math.abs(v)
  if (kind === 'percent') return `${sign}${(abs * 100).toFixed(1)} pts`
  if (kind === 'score') return `${sign}${abs.toFixed(2)}`
  return `${sign}${compact.format(abs)}`
}

const rtf = new Intl.RelativeTimeFormat('en', { numeric: 'auto' })
export function formatRelative(date: Date, now = new Date()): string {
  const diff = (date.getTime() - now.getTime()) / 1000
  const abs = Math.abs(diff)
  if (abs < 45) return 'just now'
  if (abs < 3600) return rtf.format(Math.round(diff / 60), 'minute')
  if (abs < 86400) return rtf.format(Math.round(diff / 3600), 'hour')
  return rtf.format(Math.round(diff / 86400), 'day')
}

export const formatDateTime = (d: Date) =>
  d.toLocaleString('en-US', { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' })
export const formatDate = (d: Date) => d.toLocaleDateString('en-US', { month: 'short', day: 'numeric', year: 'numeric' })
