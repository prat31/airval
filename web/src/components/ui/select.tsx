import { Check, ChevronDown } from 'lucide-react'
import { Select as SelectPrimitive } from 'radix-ui'
import { forwardRef, type ComponentPropsWithoutRef, type ReactNode } from 'react'
import { cn } from '@/lib/cn'
import { fieldBase } from './input'
import { menuItem, overlaySurface } from './overlay'

export interface SelectOption {
  value: string
  label: ReactNode
  icon?: ReactNode
  disabled?: boolean
}

export interface SelectProps extends Omit<ComponentPropsWithoutRef<typeof SelectPrimitive.Root>, 'children'> {
  options: SelectOption[]
  placeholder?: string
  size?: 'sm' | 'md'
  className?: string
  id?: string
  invalid?: boolean
  'aria-label'?: string
  'aria-describedby'?: string
}

export const Select = forwardRef<HTMLButtonElement, SelectProps>(function Select(
  { options, placeholder = 'Select…', size = 'md', className, id, invalid, ...props },
  ref,
) {
  return (
    <SelectPrimitive.Root {...props}>
      <SelectPrimitive.Trigger
        ref={ref}
        id={id}
        aria-label={props['aria-label']}
        aria-describedby={props['aria-describedby']}
        aria-invalid={invalid || undefined}
        className={cn(
          fieldBase,
          'flex items-center justify-between gap-2 text-left text-sm data-[placeholder]:text-text-muted',
          size === 'sm' ? 'h-7 px-2' : 'h-8 px-2.5',
          '[&>span]:flex [&>span]:items-center [&>span]:gap-2 [&>span]:truncate [&_svg]:size-4 [&_svg]:text-text-muted',
          className,
        )}
      >
        <SelectPrimitive.Value placeholder={placeholder} />
        <SelectPrimitive.Icon asChild>
          <ChevronDown className="size-3.5! shrink-0" />
        </SelectPrimitive.Icon>
      </SelectPrimitive.Trigger>
      <SelectPrimitive.Portal>
        <SelectPrimitive.Content
          position="popper"
          sideOffset={4}
          className={cn(overlaySurface, 'max-h-(--radix-select-content-available-height) min-w-(--radix-select-trigger-width)')}
        >
          <SelectPrimitive.Viewport className="p-1">
            {options.map((o) => (
              <SelectPrimitive.Item key={o.value} value={o.value} disabled={o.disabled} className={cn(menuItem, 'pr-8')}>
                {o.icon}
                <SelectPrimitive.ItemText>{o.label}</SelectPrimitive.ItemText>
                <SelectPrimitive.ItemIndicator className="absolute right-2 flex">
                  <Check className="text-accent!" />
                </SelectPrimitive.ItemIndicator>
              </SelectPrimitive.Item>
            ))}
          </SelectPrimitive.Viewport>
        </SelectPrimitive.Content>
      </SelectPrimitive.Portal>
    </SelectPrimitive.Root>
  )
})
