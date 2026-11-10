import { Check, Minus } from 'lucide-react'
import { Checkbox as CheckboxPrimitive } from 'radix-ui'
import { forwardRef, type ComponentPropsWithoutRef } from 'react'
import { cn } from '@/lib/cn'

export const Checkbox = forwardRef<HTMLButtonElement, ComponentPropsWithoutRef<typeof CheckboxPrimitive.Root>>(
  function Checkbox({ className, ...props }, ref) {
    return (
      <CheckboxPrimitive.Root
        ref={ref}
        className={cn(
          'peer inline-flex size-4 shrink-0 items-center justify-center rounded-[4px] border border-border-strong bg-surface',
          'transition-colors duration-150 ease-out hover:border-text-muted focus-ring',
          'data-[state=checked]:border-accent data-[state=checked]:bg-accent data-[state=checked]:text-on-accent',
          'data-[state=indeterminate]:border-accent data-[state=indeterminate]:bg-accent data-[state=indeterminate]:text-on-accent',
          'disabled:cursor-not-allowed disabled:opacity-50',
          className,
        )}
        {...props}
      >
        <CheckboxPrimitive.Indicator className="flex items-center justify-center">
          {props.checked === 'indeterminate' ? (
            <Minus className="size-3" strokeWidth={2.5} />
          ) : (
            <Check className="size-3" strokeWidth={2.5} />
          )}
        </CheckboxPrimitive.Indicator>
      </CheckboxPrimitive.Root>
    )
  },
)
