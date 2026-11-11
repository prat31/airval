/**
 * Small presentational pieces shared by run screens (list, detail, overview, compare).
 */
import { CalendarClock, MousePointerClick, SquareTerminal, Webhook, type LucideIcon } from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { StatusPill, type Status } from '@/components/ui/status-pill'
import { Tooltip } from '@/components/ui/tooltip'
import { cn } from '@/lib/cn'
import { formatDuration, formatPercent } from '@/lib/format'
import type { Result, Run, RunStatus, TriggerKind } from './api'

/** UUIDv7 ids share their timestamp prefix, so the random tail is the readable part. */
export const shortId = (id: string) => id.replace(/-/g, '').slice(-7)

export function runStatus(run: Pick<Run, 'status' | 'gate_passed'>): Status {
  const map: Record<RunStatus, Status> = {
    queued: 'queued',
    running: 'running',
    succeeded: run.gate_passed === false ? 'failed' : 'passed',
    failed: 'failed',
    errored: 'errored',
    canceled: 'cancelled',
  }
  return map[run.status]
}

export function RunStatusPill({ run, size, iconOnly }: { run: Pick<Run, 'status' | 'gate_passed'>; size?: 'sm' | 'md'; iconOnly?: boolean }) {
  const s = runStatus(run)
  const label = run.status === 'failed' && run.gate_passed === false ? 'Gate failed' : undefined
  return <StatusPill status={s} size={size} iconOnly={iconOnly} label={label} />
}

export function resultStatus(r: Pick<Result, 'status'>): Status {
  return r.status === 'passed' ? 'passed' : r.status === 'failed' ? 'failed' : r.status === 'errored' ? 'errored' : 'skipped'
}

export const triggerMeta: Record<TriggerKind, { label: string; icon: LucideIcon }> = {
  manual: { label: 'Manual', icon: MousePointerClick },
  schedule: { label: 'Schedule', icon: CalendarClock },
  webhook: { label: 'Webhook', icon: Webhook },
  api: { label: 'API / CI', icon: SquareTerminal },
}

export function TriggerIcon({ kind, withLabel }: { kind: TriggerKind; withLabel?: boolean }) {
  const m = triggerMeta[kind]
  const icon = <m.icon className="size-3.5 shrink-0 text-text-muted" aria-hidden />
  if (withLabel)
    return (
      <span className="inline-flex items-center gap-1.5 text-text-2">
        {icon}
        {m.label}
      </span>
    )
  return (
    <Tooltip content={m.label}>
      <span className="inline-flex size-6 items-center justify-center rounded-sm" aria-label={m.label} role="img">
        {icon}
      </span>
    </Tooltip>
  )
}

/** Value + thin magnitude bar. The number carries the meaning; the bar only aids scanning. */
export function PassRateBar({ value, className }: { value: number | null | undefined; className?: string }) {
  if (value == null) return <span className="text-text-muted">—</span>
  return (
    <span className={cn('flex w-full items-center gap-2', className)}>
      <span className="tabular w-11 shrink-0 text-right text-sm text-text">{formatPercent(value, value === 1 ? 0 : 1)}</span>
      <span className="relative h-1.5 min-w-8 flex-1 overflow-hidden rounded-full bg-surface-2 ring-1 ring-inset ring-border" aria-hidden>
        <span className="absolute inset-y-0 left-0 rounded-full bg-accent" style={{ width: `${Math.max(0, Math.min(1, value)) * 100}%` }} />
      </span>
    </span>
  )
}

/** Live progress bar: passed / failed / errored segments over the planned total. */
export function ProgressBar({ run, className }: { run: Pick<Run, 'total' | 'completed' | 'failed' | 'errored'>; className?: string }) {
  const total = Math.max(1, run.total)
  const ok = Math.max(0, run.completed - run.failed - run.errored)
  const seg = (n: number) => `${(n / total) * 100}%`
  return (
    <div
      className={cn('flex h-1.5 w-full overflow-hidden rounded-full bg-surface-2', className)}
      role="progressbar"
      aria-valuemin={0}
      aria-valuemax={run.total}
      aria-valuenow={run.completed}
      aria-label={`${run.completed} of ${run.total} results`}
    >
      <span className="h-full bg-accent transition-[width] duration-200 ease-out" style={{ width: seg(ok) }} />
      <span className="h-full bg-fail transition-[width] duration-200 ease-out" style={{ width: seg(run.failed) }} />
      <span className="h-full bg-warn transition-[width] duration-200 ease-out" style={{ width: seg(run.errored) }} />
    </div>
  )
}

export function LabelBadges({ labels, max = 2, className }: { labels: Record<string, string>; max?: number; className?: string }) {
  const entries = Object.entries(labels ?? {}).sort(([a], [b]) => (a === 'git_sha' ? -1 : b === 'git_sha' ? 1 : a.localeCompare(b)))
  if (!entries.length) return <span className="text-text-muted">—</span>
  const shown = entries.slice(0, max)
  const rest = entries.slice(max)
  return (
    <span className={cn('flex min-w-0 items-center gap-1', className)}>
      {shown.map(([k, v]) => (
        <Badge key={k} variant="outline" mono title={`${k}: ${v}`} className="max-w-36 truncate">
          {k === 'git_sha' ? v.slice(0, 7) : `${k}:${v}`}
        </Badge>
      ))}
      {rest.length > 0 && (
        <Tooltip content={rest.map(([k, v]) => `${k}: ${v}`).join(' · ')}>
          <span className="text-xs text-text-muted">+{rest.length}</span>
        </Tooltip>
      )}
    </span>
  )
}

export function runDurationMs(run: Pick<Run, 'started_at' | 'finished_at' | 'status'>, now = Date.now()): number | null {
  if (!run.started_at) return null
  const end = run.finished_at ? Date.parse(run.finished_at) : run.status === 'running' ? now : null
  return end == null ? null : Math.max(0, end - Date.parse(run.started_at))
}

export const formatRunDuration = (run: Pick<Run, 'started_at' | 'finished_at' | 'status'>) => {
  const ms = runDurationMs(run)
  return ms == null ? '—' : formatDuration(ms)
}

/** Human name: explicit run name, else the suite name, else the short id. */
export function runTitle(run: Pick<Run, 'name' | 'id'>, suiteName?: string) {
  return run.name || suiteName || `Run ${shortId(run.id)}`
}

/** Fixed slot per run position in a comparison (A, B, C, D). */
export const RUN_LETTERS = ['A', 'B', 'C', 'D'] as const
export function RunLetter({ index, className }: { index: number; className?: string }) {
  return (
    <span
      className={cn('inline-flex size-5 shrink-0 items-center justify-center rounded-sm text-[11px] font-semibold text-text', className)}
      style={{
        background: `color-mix(in srgb, var(--chart-${index + 1}) 20%, transparent)`,
        boxShadow: `inset 0 0 0 1px var(--chart-${index + 1})`,
      }}
      aria-label={`Run ${RUN_LETTERS[index]}`}
    >
      {RUN_LETTERS[index]}
    </span>
  )
}
