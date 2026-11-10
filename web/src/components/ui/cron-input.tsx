import { CronExpressionParser } from 'cron-parser'
import { CalendarClock, ChevronDown, CircleAlert } from 'lucide-react'
import { useId, useMemo } from 'react'
import { cn } from '@/lib/cn'
import { Button } from './button'
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from './dropdown-menu'
import { Input } from './input'

export const cronPresets = [
  { label: 'Every hour', value: '0 * * * *' },
  { label: 'Every day at 02:00', value: '0 2 * * *' },
  { label: 'Weekdays at 09:00', value: '0 9 * * 1-5' },
  { label: 'Every Monday at 08:00', value: '0 8 * * 1' },
  { label: 'Every 15 minutes', value: '*/15 * * * *' },
]

export interface CronPreview {
  valid: boolean
  error?: string
  next: Date[]
  description?: string
}

const pad = (n: string) => n.padStart(2, '0')
const DAYS = ['Sunday', 'Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday']

/** Plain-English summary for common cron shapes; falls back to undefined. */
export function describeCron(expr: string): string | undefined {
  const parts = expr.trim().split(/\s+/)
  if (parts.length !== 5) return undefined
  const [min, hour, dom, mon, dow] = parts as [string, string, string, string, string]
  const isNum = (s: string) => /^\d+$/.test(s)
  if (min.startsWith('*/') && hour === '*' && dom === '*' && mon === '*' && dow === '*') return `Every ${min.slice(2)} minutes`
  if (isNum(min) && hour === '*' && dom === '*' && mon === '*' && dow === '*') return `Every hour at :${pad(min)}`
  if (isNum(min) && isNum(hour) && mon === '*') {
    const at = `at ${pad(hour)}:${pad(min)}`
    if (dom === '*' && dow === '*') return `Every day ${at}`
    if (dom === '*' && dow === '1-5') return `Weekdays ${at}`
    if (dom === '*' && isNum(dow)) return `Every ${DAYS[Number(dow) % 7]} ${at}`
    if (isNum(dom) && dow === '*') return `Monthly on day ${dom} ${at}`
  }
  return undefined
}

export function previewCron(expr: string, tz?: string, count = 5, from = new Date()): CronPreview {
  if (!expr.trim()) return { valid: false, error: 'Enter a cron expression', next: [] }
  if (expr.trim().split(/\s+/).length !== 5) return { valid: false, error: 'Use 5 fields: minute hour day-of-month month day-of-week', next: [] }
  try {
    const it = CronExpressionParser.parse(expr, { tz, currentDate: from, strict: false })
    const next = Array.from({ length: count }, () => it.next().toDate())
    return { valid: true, next, description: describeCron(expr) }
  } catch (e) {
    return { valid: false, error: e instanceof Error ? e.message : 'Invalid expression', next: [] }
  }
}

export interface CronInputProps {
  value: string
  onChange: (value: string) => void
  /** IANA timezone used for the preview, e.g. "Europe/Berlin". Defaults to the browser's. */
  timezone?: string
  /** Number of upcoming fire times to preview. */
  previewCount?: number
  id?: string
  className?: string
}

/** Cron expression input with presets, plain-English summary and a "next N fires" preview. */
export function CronInput({ value, onChange, timezone, previewCount = 5, id, className }: CronInputProps) {
  const tz = timezone ?? Intl.DateTimeFormat().resolvedOptions().timeZone
  const preview = useMemo(() => previewCron(value, tz, previewCount), [value, tz, previewCount])
  const autoId = useId()
  const inputId = id ?? autoId
  const fmt = new Intl.DateTimeFormat('en-US', {
    weekday: 'short',
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
    timeZone: tz,
  })

  return (
    <div className={cn('flex flex-col gap-2', className)}>
      <div className="flex gap-2">
        <Input
          id={inputId}
          value={value}
          onChange={(e) => onChange(e.target.value)}
          invalid={!preview.valid && value.trim() !== ''}
          spellCheck={false}
          placeholder="0 2 * * *"
          aria-describedby={`${inputId}-preview`}
          className="flex-1 font-mono"
        />
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button iconRight={<ChevronDown className="text-text-muted" />}>Presets</Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end">
            {cronPresets.map((p) => (
              <DropdownMenuItem key={p.value} onSelect={() => onChange(p.value)} shortcut={p.value}>
                {p.label}
              </DropdownMenuItem>
            ))}
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
      <div id={`${inputId}-preview`} className="rounded-md border border-border bg-surface-2 px-3 py-2.5" aria-live="polite">
        {preview.valid ? (
          <>
            <div className="flex items-center justify-between gap-2 text-xs">
              <span className="flex items-center gap-1.5 font-medium text-text">
                <CalendarClock className="size-3.5 text-accent" />
                {preview.description ?? 'Custom schedule'}
              </span>
              <span className="text-text-muted">{tz}</span>
            </div>
            <ol className="mt-2 flex flex-col gap-1">
              {preview.next.map((d, i) => (
                <li key={d.getTime()} className="tabular flex items-center gap-2 text-xs text-text-2">
                  <span className="w-3 text-right text-text-muted">{i + 1}</span>
                  {fmt.format(d)}
                </li>
              ))}
            </ol>
          </>
        ) : (
          <p className="flex items-center gap-1.5 text-xs text-fail">
            <CircleAlert className="size-3.5 shrink-0" />
            {preview.error}
          </p>
        )}
      </div>
    </div>
  )
}
