import { cn } from '@/lib/cn'

export interface ScoreChipProps {
  /** Score value in 0..1, or null when the evaluator produced no numeric score. */
  value: number | null
  /** Pass threshold; when set, the chip shows pass/fail state with a glyph. */
  threshold?: number
  /** Explicit pass/fail (overrides threshold). */
  passed?: boolean
  /** Text label (e.g. "correct", "B wins") shown instead of / beside the number. */
  label?: string
  size?: 'sm' | 'md'
  className?: string
}

/**
 * Compact score display for tables and cards. A tiny bar encodes magnitude;
 * pass/fail is redundantly encoded with ✓/✗ and color.
 */
export function ScoreChip({ value, threshold, passed, label, size = 'md', className }: ScoreChipProps) {
  const state = passed ?? (value != null && threshold != null ? value >= threshold : undefined)
  const nearThreshold = state === true && value != null && threshold != null && value - threshold < 0.05
  const tone =
    state === undefined
      ? 'text-text bg-surface-2 ring-border'
      : state
        ? nearThreshold
          ? 'text-warn bg-warn-subtle ring-transparent'
          : 'text-pass bg-pass-subtle ring-transparent'
        : 'text-fail bg-fail-subtle ring-transparent'
  const glyph = state === undefined ? null : state ? '✓' : '✗'
  const text = value == null ? (label ?? '—') : value.toFixed(2)
  const ariaState = state === undefined ? '' : state ? ', passed' : ', failed'

  return (
    <span
      className={cn(
        'tabular inline-flex shrink-0 items-center gap-1.5 rounded-sm font-mono ring-1 ring-inset',
        size === 'sm' ? 'h-5 px-1.5 text-[11px]' : 'h-6 px-2 text-xs',
        tone,
        className,
      )}
      aria-label={`Score ${text}${ariaState}`}
    >
      {glyph && (
        <span aria-hidden className="font-sans text-[10px] font-bold">
          {glyph}
        </span>
      )}
      <span aria-hidden>{text}</span>
      {value != null && (
        <span aria-hidden className="relative h-1 w-6 overflow-hidden rounded-full bg-current/15">
          <span className="absolute inset-y-0 left-0 rounded-full bg-current" style={{ width: `${Math.max(0, Math.min(1, value)) * 100}%` }} />
        </span>
      )}
      {label && value != null && <span className="font-sans text-text-2">{label}</span>}
    </span>
  )
}
