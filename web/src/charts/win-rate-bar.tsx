import { cn } from '@/lib/cn'

export interface WinRateBarProps {
  /** Wins for A, ties, wins for B. */
  wins: number
  ties: number
  losses: number
  labelA?: string
  labelB?: string
  className?: string
}

/**
 * Pairwise win rate as one 100% bar: A (slot 1) · tie (neutral) · B (slot 4), 2px surface gaps,
 * rounded ends and direct labels (all values visible, so no separate table view is needed).
 */
export function WinRateBar({ wins, ties, losses, labelA = 'A', labelB = 'B', className }: WinRateBarProps) {
  const total = wins + ties + losses || 1
  const segs = [
    { key: 'a', label: `${labelA} wins`, value: wins, color: 'var(--chart-1)' },
    { key: 'tie', label: 'Tie', value: ties, color: 'var(--border-strong)' },
    { key: 'b', label: `${labelB} wins`, value: losses, color: 'var(--chart-4)' },
  ]
  const pct = (v: number) => `${Math.round((v / total) * 100)}%`
  return (
    <figure className={cn('flex flex-col gap-2.5', className)} aria-label={`Win rate: ${segs.map((s) => `${s.label} ${pct(s.value)}`).join(', ')}`}>
      <div className="flex items-end justify-between gap-4 text-sm">
        {[segs[0]!, segs[2]!].map((s, i) => (
          <div key={s.key} className={cn('flex flex-col', i === 1 && 'items-end')}>
            <span className="tabular text-lg font-semibold tracking-tight text-text">{pct(s.value)}</span>
            <span className="flex items-center gap-1.5 text-xs text-text-2">
              <span className="size-2 rounded-[2px]" style={{ background: s.color }} aria-hidden />
              {s.label}
            </span>
          </div>
        ))}
      </div>
      <div className="flex h-3 w-full gap-0.5" role="presentation">
        {segs
          .filter((s) => s.value > 0)
          .map((s) => (
            <div
              key={s.key}
              className="h-full first:rounded-l-[4px] last:rounded-r-[4px]"
              style={{ width: `${(s.value / total) * 100}%`, background: s.color }}
              title={`${s.label}: ${s.value} (${pct(s.value)})`}
            />
          ))}
      </div>
      <figcaption className="tabular flex justify-center gap-1 text-xs text-text-muted">
        {wins} · {ties} ties · {losses} <span className="sr-only">of {total} comparisons</span>
      </figcaption>
    </figure>
  )
}
