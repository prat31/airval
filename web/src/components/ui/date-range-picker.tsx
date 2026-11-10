import { CalendarDays, ChevronDown, ChevronLeft, ChevronRight } from 'lucide-react'
import { useMemo, useState } from 'react'
import { cn } from '@/lib/cn'
import { Button, IconButton } from './button'
import { Popover, PopoverContent, PopoverTrigger } from './popover'

export interface DateRange {
  from: Date
  to: Date
  /** Preset id when the range came from a preset (kept relative, e.g. "7d"). */
  preset?: string
}

export interface DateRangePreset {
  id: string
  label: string
  /** Short label for the trigger, e.g. "7d". */
  short: string
  range: () => { from: Date; to: Date }
}

const day = 86_400_000
const startOfDay = (d: Date) => new Date(d.getFullYear(), d.getMonth(), d.getDate())
const sameDay = (a: Date, b: Date) => startOfDay(a).getTime() === startOfDay(b).getTime()

export const defaultDatePresets: DateRangePreset[] = [
  { id: '24h', label: 'Last 24 hours', short: '24h', range: () => ({ from: new Date(Date.now() - day), to: new Date() }) },
  { id: '7d', label: 'Last 7 days', short: '7d', range: () => ({ from: new Date(Date.now() - 7 * day), to: new Date() }) },
  { id: '30d', label: 'Last 30 days', short: '30d', range: () => ({ from: new Date(Date.now() - 30 * day), to: new Date() }) },
  { id: '90d', label: 'Last 90 days', short: '90d', range: () => ({ from: new Date(Date.now() - 90 * day), to: new Date() }) },
]

const fmt = (d: Date) => d.toLocaleDateString('en-US', { month: 'short', day: 'numeric' })
const WEEKDAYS = ['Mo', 'Tu', 'We', 'Th', 'Fr', 'Sa', 'Su']

function MonthGrid({
  month,
  from,
  to,
  hover,
  onPick,
  onHover,
}: {
  month: Date
  from: Date | null
  to: Date | null
  hover: Date | null
  onPick: (d: Date) => void
  onHover: (d: Date | null) => void
}) {
  const cells = useMemo(() => {
    const first = new Date(month.getFullYear(), month.getMonth(), 1)
    const offset = (first.getDay() + 6) % 7
    const days = new Date(month.getFullYear(), month.getMonth() + 1, 0).getDate()
    return [...Array.from({ length: offset }, () => null), ...Array.from({ length: days }, (_, i) => new Date(month.getFullYear(), month.getMonth(), i + 1))]
  }, [month])
  const end = to ?? hover
  const [lo, hi] = from && end ? (from <= end ? [from, end] : [end, from]) : [from, from]
  const today = new Date()
  return (
    <div role="grid" aria-label={month.toLocaleDateString('en-US', { month: 'long', year: 'numeric' })}>
      <div className="mb-1 grid grid-cols-7">
        {WEEKDAYS.map((w) => (
          <span key={w} className="flex h-7 items-center justify-center text-[11px] font-medium text-text-muted">
            {w}
          </span>
        ))}
      </div>
      <div className="grid grid-cols-7 gap-y-0.5" onMouseLeave={() => onHover(null)}>
        {cells.map((d, i) => {
          if (!d) return <span key={`e${i}`} />
          const isStart = lo && sameDay(d, lo)
          const isEnd = hi && sameDay(d, hi)
          const inRange = lo && hi && d > lo && d < hi
          const future = d > today
          return (
            <button
              key={d.toISOString()}
              type="button"
              disabled={future}
              onClick={() => onPick(d)}
              onMouseEnter={() => onHover(d)}
              aria-pressed={!!(isStart || isEnd)}
              className={cn(
                'tabular relative flex h-8 items-center justify-center text-sm text-text transition-colors focus-ring',
                'disabled:pointer-events-none disabled:text-text-muted disabled:opacity-40',
                inRange && 'bg-accent-subtle',
                isStart && hi && !sameDay(lo!, hi) && 'rounded-l-md bg-accent-subtle',
                isEnd && lo && !sameDay(lo, hi!) && 'rounded-r-md bg-accent-subtle',
                !inRange && !isStart && !isEnd && 'rounded-md hover:bg-surface-hover',
              )}
            >
              <span
                className={cn(
                  'flex size-8 items-center justify-center rounded-md',
                  (isStart || isEnd) && 'bg-accent font-medium text-on-accent',
                  sameDay(d, today) && !isStart && !isEnd && 'font-semibold text-accent',
                )}
              >
                {d.getDate()}
              </span>
            </button>
          )
        })}
      </div>
    </div>
  )
}

export interface DateRangePickerProps {
  value: DateRange
  onChange: (range: DateRange) => void
  presets?: DateRangePreset[]
  size?: 'sm' | 'md'
  className?: string
}

/** Date range with preset rows (relative) and a calendar for custom ranges. */
export function DateRangePicker({ value, onChange, presets = defaultDatePresets, size = 'sm', className }: DateRangePickerProps) {
  const [open, setOpen] = useState(false)
  const [month, setMonth] = useState(() => new Date(value.to.getFullYear(), value.to.getMonth(), 1))
  const [draftFrom, setDraftFrom] = useState<Date | null>(null)
  const [draftTo, setDraftTo] = useState<Date | null>(null)
  const [hover, setHover] = useState<Date | null>(null)

  const preset = presets.find((p) => p.id === value.preset)
  const label = preset ? preset.label : `${fmt(value.from)} – ${fmt(value.to)}`

  const pick = (d: Date) => {
    if (!draftFrom || draftTo) {
      setDraftFrom(d)
      setDraftTo(null)
    } else {
      const [a, b] = d < draftFrom ? [d, draftFrom] : [draftFrom, d]
      setDraftFrom(a)
      setDraftTo(b)
    }
  }

  return (
    <Popover
      open={open}
      onOpenChange={(o) => {
        setOpen(o)
        if (o) {
          setDraftFrom(value.preset ? null : value.from)
          setDraftTo(value.preset ? null : value.to)
        }
      }}
    >
      <PopoverTrigger asChild>
        <Button size={size} icon={<CalendarDays />} iconRight={<ChevronDown className="text-text-muted" />} className={className}>
          <span className="tabular">{label}</span>
        </Button>
      </PopoverTrigger>
      <PopoverContent className="flex w-auto p-0" align="end">
        <div className="flex w-40 flex-col gap-0.5 border-r border-border p-1.5">
          {presets.map((p) => (
            <button
              key={p.id}
              type="button"
              onClick={() => {
                onChange({ ...p.range(), preset: p.id })
                setOpen(false)
              }}
              className={cn(
                'flex h-8 items-center justify-between rounded-sm px-2 text-left text-sm transition-colors focus-ring',
                value.preset === p.id ? 'bg-accent-subtle font-medium text-accent' : 'text-text hover:bg-surface-hover',
              )}
            >
              {p.label}
              <span className="text-xs text-text-muted">{p.short}</span>
            </button>
          ))}
        </div>
        <div className="flex w-[280px] flex-col p-3">
          <div className="mb-2 flex items-center justify-between">
            <IconButton label="Previous month" size="sm" onClick={() => setMonth(new Date(month.getFullYear(), month.getMonth() - 1, 1))}>
              <ChevronLeft />
            </IconButton>
            <span className="text-sm font-medium text-text">{month.toLocaleDateString('en-US', { month: 'long', year: 'numeric' })}</span>
            <IconButton label="Next month" size="sm" onClick={() => setMonth(new Date(month.getFullYear(), month.getMonth() + 1, 1))}>
              <ChevronRight />
            </IconButton>
          </div>
          <MonthGrid month={month} from={draftFrom} to={draftTo} hover={draftTo ? null : hover} onPick={pick} onHover={setHover} />
          <div className="mt-3 flex items-center justify-between gap-2 border-t border-border pt-3">
            <span className="tabular text-xs text-text-muted">
              {draftFrom ? `${fmt(draftFrom)} – ${draftTo ? fmt(draftTo) : '…'}` : 'Pick a start date'}
            </span>
            <Button
              size="sm"
              variant="primary"
              disabled={!draftFrom || !draftTo}
              onClick={() => {
                if (!draftFrom || !draftTo) return
                onChange({ from: startOfDay(draftFrom), to: new Date(startOfDay(draftTo).getTime() + day - 1) })
                setOpen(false)
              }}
            >
              Apply
            </Button>
          </div>
        </div>
      </PopoverContent>
    </Popover>
  )
}
