import { ToggleGroup } from 'radix-ui'
import type { ReactNode } from 'react'
import { cn } from '@/lib/cn'

export interface SegmentedOption<T extends string> {
  value: T
  label: ReactNode
  icon?: ReactNode
  /** Accessible name when label is icon-only. */
  ariaLabel?: string
}

export interface SegmentedControlProps<T extends string> {
  value: T
  onValueChange: (value: T) => void
  options: SegmentedOption<T>[]
  size?: 'sm' | 'md'
  className?: string
  'aria-label'?: string
}

export function SegmentedControl<T extends string>({
  value,
  onValueChange,
  options,
  size = 'md',
  className,
  ...props
}: SegmentedControlProps<T>) {
  return (
    <ToggleGroup.Root
      type="single"
      value={value}
      onValueChange={(v) => v && onValueChange(v as T)}
      aria-label={props['aria-label']}
      className={cn('inline-flex items-center gap-0.5 rounded-md border border-border bg-surface-2 p-0.5', className)}
    >
      {options.map((o) => (
        <ToggleGroup.Item
          key={o.value}
          value={o.value}
          aria-label={o.ariaLabel}
          className={cn(
            'inline-flex items-center justify-center gap-1.5 rounded-sm font-medium text-text-muted transition-colors duration-150',
            'hover:text-text focus-ring data-[state=on]:bg-surface data-[state=on]:text-text',
            'data-[state=on]:shadow-[0_0_0_1px_var(--border)] [&_svg]:size-3.5',
            size === 'sm' ? 'h-6 px-2 text-xs' : 'h-7 px-2.5 text-sm',
          )}
        >
          {o.icon}
          {o.label}
        </ToggleGroup.Item>
      ))}
    </ToggleGroup.Root>
  )
}
