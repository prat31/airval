import { RadioGroup as RadioPrimitive } from 'radix-ui'
import { forwardRef, type ComponentPropsWithoutRef, type ReactNode } from 'react'
import { cn } from '@/lib/cn'

export const RadioGroup = forwardRef<HTMLDivElement, ComponentPropsWithoutRef<typeof RadioPrimitive.Root>>(
  function RadioGroup({ className, ...props }, ref) {
    return <RadioPrimitive.Root ref={ref} className={cn('flex flex-col gap-2', className)} {...props} />
  },
)

export interface RadioItemProps extends ComponentPropsWithoutRef<typeof RadioPrimitive.Item> {
  label: ReactNode
  description?: ReactNode
}

export const RadioItem = forwardRef<HTMLButtonElement, RadioItemProps>(function RadioItem(
  { className, label, description, id, ...props },
  ref,
) {
  const itemId = id ?? `radio-${props.value}`
  return (
    <div className={cn('flex items-start gap-2.5', className)}>
      <RadioPrimitive.Item
        ref={ref}
        id={itemId}
        className={cn(
          'mt-0.5 inline-flex size-4 shrink-0 items-center justify-center rounded-full border border-border-strong bg-surface',
          'transition-colors duration-150 hover:border-text-muted focus-ring data-[state=checked]:border-accent',
          'disabled:cursor-not-allowed disabled:opacity-50',
        )}
        {...props}
      >
        <RadioPrimitive.Indicator className="block size-2 rounded-full bg-accent" />
      </RadioPrimitive.Item>
      <label htmlFor={itemId} className="flex cursor-pointer flex-col">
        <span className="text-sm text-text">{label}</span>
        {description && <span className="text-xs text-text-muted">{description}</span>}
      </label>
    </div>
  )
})
