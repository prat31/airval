import { Command } from 'cmdk'
import { Check, ListFilter, Search, X } from 'lucide-react'
import type { ReactNode } from 'react'
import { cn } from '@/lib/cn'
import { Button } from './button'
import { Input } from './input'
import { menuItem } from './overlay'
import { Popover, PopoverContent, PopoverTrigger } from './popover'

export interface FilterOption {
  value: string
  label: string
  icon?: ReactNode
  count?: number
}

export interface FilterDef {
  id: string
  label: string
  icon?: ReactNode
  options: FilterOption[]
  value: string[]
  onChange: (value: string[]) => void
}

export interface FilterChipProps extends Omit<FilterDef, 'id'> {
  className?: string
}

/** Multi-select filter as a compact chip with a searchable popover. */
export function FilterChip({ label, icon, options, value, onChange, className }: FilterChipProps) {
  const active = value.length > 0
  const selectedLabels = options.filter((o) => value.includes(o.value)).map((o) => o.label)
  return (
    <Popover>
      <PopoverTrigger asChild>
        <button
          type="button"
          className={cn(
            'inline-flex h-7 shrink-0 items-center gap-1.5 rounded-md border px-2 text-sm transition-colors duration-150 focus-ring [&_svg]:size-3.5',
            active
              ? 'border-accent/40 bg-accent-subtle text-text'
              : 'border-dashed border-border-strong text-text-2 hover:border-text-muted hover:text-text',
            className,
          )}
        >
          <span className={active ? 'text-accent' : 'text-text-muted'}>{icon ?? <ListFilter />}</span>
          <span>{label}</span>
          {active && (
            <>
              <span className="h-3.5 w-px bg-border-strong" aria-hidden />
              <span className="max-w-40 truncate font-medium">
                {selectedLabels.length > 2 ? `${selectedLabels.length} selected` : selectedLabels.join(', ')}
              </span>
            </>
          )}
        </button>
      </PopoverTrigger>
      <PopoverContent className="w-60 p-0">
        <Command loop>
          {options.length > 6 && (
            <div className="flex items-center gap-2 border-b border-border px-2.5">
              <Search className="size-3.5 text-text-muted" />
              <Command.Input placeholder={`Filter ${label.toLowerCase()}…`} className="h-9 w-full bg-transparent text-sm outline-none placeholder:text-text-muted" />
            </div>
          )}
          <Command.List className="max-h-64 overflow-y-auto p-1">
            <Command.Empty className="px-2 py-4 text-center text-sm text-text-muted">No matches</Command.Empty>
            {options.map((o) => {
              const checked = value.includes(o.value)
              return (
                <Command.Item
                  key={o.value}
                  value={o.label}
                  onSelect={() => onChange(checked ? value.filter((v) => v !== o.value) : [...value, o.value])}
                  className={cn(menuItem, 'data-[selected=true]:bg-surface-hover')}
                >
                  <span
                    className={cn(
                      'flex size-4 items-center justify-center rounded-[4px] border',
                      checked ? 'border-accent bg-accent text-on-accent' : 'border-border-strong',
                    )}
                  >
                    {checked && <Check className="size-3! text-on-accent!" strokeWidth={2.5} />}
                  </span>
                  {o.icon}
                  <span className="flex-1 truncate">{o.label}</span>
                  {o.count != null && <span className="tabular text-xs text-text-muted">{o.count}</span>}
                </Command.Item>
              )
            })}
          </Command.List>
          {value.length > 0 && (
            <div className="border-t border-border p-1">
              <button type="button" onClick={() => onChange([])} className={cn(menuItem, 'w-full justify-center text-text-2 hover:bg-surface-hover')}>
                Clear filter
              </button>
            </div>
          )}
        </Command>
      </PopoverContent>
    </Popover>
  )
}

export interface FilterBarProps {
  search?: string
  onSearchChange?: (value: string) => void
  searchPlaceholder?: string
  filters?: FilterDef[]
  /** Extra controls (e.g. a DateRangePicker) placed after the filter chips. */
  children?: ReactNode
  /** Right-aligned actions (view options, export…). */
  actions?: ReactNode
  className?: string
}

/** One row of filters above a table or chart grid. */
export function FilterBar({ search, onSearchChange, searchPlaceholder = 'Search…', filters = [], children, actions, className }: FilterBarProps) {
  const activeCount = filters.reduce((n, f) => n + (f.value.length ? 1 : 0), 0) + (search ? 1 : 0)
  return (
    <div className={cn('flex flex-wrap items-center gap-2', className)} role="toolbar" aria-label="Filters">
      {onSearchChange && (
        <Input
          size="sm"
          value={search ?? ''}
          onChange={(e) => onSearchChange(e.target.value)}
          placeholder={searchPlaceholder}
          leading={<Search />}
          aria-label="Search"
          className="w-56"
        />
      )}
      {filters.map(({ id, ...f }) => (
        <FilterChip key={id} {...f} />
      ))}
      {children}
      {activeCount > 0 && (
        <Button
          variant="ghost"
          size="sm"
          icon={<X />}
          onClick={() => {
            onSearchChange?.('')
            filters.forEach((f) => f.onChange([]))
          }}
        >
          Clear
        </Button>
      )}
      {actions && <div className="ml-auto flex items-center gap-2">{actions}</div>}
    </div>
  )
}
