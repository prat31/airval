import type { ReactNode } from 'react'
import { cn } from '@/lib/cn'

export interface KeyValueItem {
  label: ReactNode
  value: ReactNode
  mono?: boolean
}

export interface KeyValueProps {
  items: KeyValueItem[]
  /** `rows`: label left, value right. `grid`: label above value, in columns. */
  layout?: 'rows' | 'grid'
  columns?: 2 | 3 | 4
  className?: string
}

export function KeyValue({ items, layout = 'rows', columns = 3, className }: KeyValueProps) {
  if (layout === 'grid') {
    const cols = { 2: 'sm:grid-cols-2', 3: 'sm:grid-cols-3', 4: 'sm:grid-cols-4' }[columns]
    return (
      <dl className={cn('grid grid-cols-1 gap-x-6 gap-y-4', cols, className)}>
        {items.map((it, i) => (
          <div key={i} className="min-w-0">
            <dt className="text-xs text-text-muted">{it.label}</dt>
            <dd className={cn('mt-0.5 truncate text-sm text-text', it.mono && 'font-mono text-xs')}>{it.value}</dd>
          </div>
        ))}
      </dl>
    )
  }
  return (
    <dl className={cn('divide-y divide-border', className)}>
      {items.map((it, i) => (
        <div key={i} className="flex items-baseline justify-between gap-4 py-2">
          <dt className="shrink-0 text-sm text-text-muted">{it.label}</dt>
          <dd className={cn('min-w-0 truncate text-right text-sm text-text', it.mono && 'font-mono text-xs')}>{it.value}</dd>
        </div>
      ))}
    </dl>
  )
}
