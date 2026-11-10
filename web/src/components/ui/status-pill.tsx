import { CircleCheck, CircleMinus, CircleX, Clock, LoaderCircle, TriangleAlert, type LucideIcon } from 'lucide-react'
import { cn } from '@/lib/cn'

export type Status = 'passed' | 'failed' | 'warning' | 'running' | 'queued' | 'skipped' | 'cancelled' | 'errored'

const config: Record<Status, { label: string; icon: LucideIcon; className: string; spin?: boolean }> = {
  passed: { label: 'Passed', icon: CircleCheck, className: 'bg-pass-subtle text-pass' },
  failed: { label: 'Failed', icon: CircleX, className: 'bg-fail-subtle text-fail' },
  errored: { label: 'Errored', icon: TriangleAlert, className: 'bg-fail-subtle text-fail' },
  warning: { label: 'Warning', icon: TriangleAlert, className: 'bg-warn-subtle text-warn' },
  running: { label: 'Running', icon: LoaderCircle, className: 'bg-running-subtle text-running', spin: true },
  queued: { label: 'Queued', icon: Clock, className: 'bg-skipped-subtle text-text-2' },
  skipped: { label: 'Skipped', icon: CircleMinus, className: 'bg-skipped-subtle text-skipped' },
  cancelled: { label: 'Cancelled', icon: CircleMinus, className: 'bg-skipped-subtle text-skipped' },
}

export interface StatusPillProps {
  status: Status
  /** Override the default label. */
  label?: string
  size?: 'sm' | 'md'
  /** Icon only (label still exposed to assistive tech). */
  iconOnly?: boolean
  className?: string
}

/** Run / result status. Meaning is always carried by icon + label, never color alone. */
export function StatusPill({ status, label, size = 'md', iconOnly, className }: StatusPillProps) {
  const c = config[status]
  const Icon = c.icon
  const text = label ?? c.label
  return (
    <span
      className={cn(
        'inline-flex shrink-0 items-center gap-1 whitespace-nowrap rounded-full font-medium',
        size === 'sm' ? 'h-5 text-xs' : 'h-6 text-xs',
        iconOnly ? (size === 'sm' ? 'w-5 justify-center' : 'w-6 justify-center') : size === 'sm' ? 'pl-1 pr-2' : 'pl-1.5 pr-2.5',
        c.className,
        className,
      )}
      title={iconOnly ? text : undefined}
    >
      <Icon className={cn('size-3.5 shrink-0', c.spin && 'animate-spin')} strokeWidth={2} aria-hidden />
      {iconOnly ? <span className="sr-only">{text}</span> : text}
    </span>
  )
}
