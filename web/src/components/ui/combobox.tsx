import { Command } from 'cmdk'
import { Check, ChevronsUpDown, Search } from 'lucide-react'
import { useState, type ReactNode } from 'react'
import { cn } from '@/lib/cn'
import { fieldBase } from './input'
import { menuItem } from './overlay'
import { Popover, PopoverContent, PopoverTrigger } from './popover'

export interface ComboboxOption {
  value: string
  label: string
  description?: string
  icon?: ReactNode
  /** Extra search terms. */
  keywords?: string[]
}

interface BaseProps {
  options: ComboboxOption[]
  placeholder?: string
  searchPlaceholder?: string
  emptyText?: string
  size?: 'sm' | 'md'
  className?: string
  id?: string
  disabled?: boolean
  'aria-label'?: string
}

export type ComboboxProps = BaseProps &
  (
    | { multiple?: false; value: string | null; onValueChange: (value: string | null) => void }
    | { multiple: true; value: string[]; onValueChange: (value: string[]) => void }
  )

/** Searchable select (single or multi). */
export function Combobox(props: ComboboxProps) {
  const { options, placeholder = 'Select…', searchPlaceholder = 'Search…', emptyText = 'No matches', size = 'md', className, id, disabled } = props
  const [open, setOpen] = useState(false)
  const selected = props.multiple ? props.value : props.value ? [props.value] : []
  const selectedOptions = options.filter((o) => selected.includes(o.value))

  const toggle = (value: string) => {
    if (props.multiple) {
      props.onValueChange(selected.includes(value) ? selected.filter((v) => v !== value) : [...selected, value])
    } else {
      props.onValueChange(value === props.value ? null : value)
      setOpen(false)
    }
  }

  const display =
    selectedOptions.length === 0 ? (
      <span className="text-text-muted">{placeholder}</span>
    ) : props.multiple && selectedOptions.length > 1 ? (
      <span className="truncate">
        {selectedOptions[0]!.label} <span className="text-text-muted">+{selectedOptions.length - 1}</span>
      </span>
    ) : (
      <span className="flex min-w-0 items-center gap-2 truncate [&_svg]:size-4 [&_svg]:text-text-muted">
        {selectedOptions[0]!.icon}
        {selectedOptions[0]!.label}
      </span>
    )

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <button
          id={id}
          type="button"
          role="combobox"
          aria-expanded={open}
          aria-label={props['aria-label']}
          disabled={disabled}
          className={cn(
            fieldBase,
            'flex items-center justify-between gap-2 text-left text-sm',
            size === 'sm' ? 'h-7 px-2' : 'h-8 px-2.5',
            className,
          )}
        >
          {display}
          <ChevronsUpDown className="size-3.5 shrink-0 text-text-muted" />
        </button>
      </PopoverTrigger>
      <PopoverContent className="w-(--radix-popover-trigger-width) min-w-56 p-0">
        <Command loop>
          <div className="flex items-center gap-2 border-b border-border px-2.5">
            <Search className="size-3.5 text-text-muted" />
            <Command.Input
              placeholder={searchPlaceholder}
              className="h-9 w-full bg-transparent text-sm text-text outline-none placeholder:text-text-muted"
            />
          </div>
          <Command.List className="max-h-64 overflow-y-auto p-1">
            <Command.Empty className="px-2 py-6 text-center text-sm text-text-muted">{emptyText}</Command.Empty>
            {options.map((o) => {
              const isSelected = selected.includes(o.value)
              return (
                <Command.Item
                  key={o.value}
                  value={o.value}
                  keywords={[o.label, ...(o.keywords ?? [])]}
                  onSelect={() => toggle(o.value)}
                  className={cn(menuItem, 'h-auto min-h-8 py-1.5 pr-8 data-[selected=true]:bg-surface-hover')}
                >
                  {o.icon}
                  <span className="flex min-w-0 flex-col">
                    <span className="truncate">{o.label}</span>
                    {o.description && <span className="truncate text-xs text-text-muted">{o.description}</span>}
                  </span>
                  {isSelected && <Check className="absolute right-2 text-accent!" />}
                </Command.Item>
              )
            })}
          </Command.List>
        </Command>
      </PopoverContent>
    </Popover>
  )
}
