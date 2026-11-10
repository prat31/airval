import { Switch as SwitchPrimitive } from 'radix-ui'
import { forwardRef, type ComponentPropsWithoutRef } from 'react'
import { cn } from '@/lib/cn'

export const Switch = forwardRef<HTMLButtonElement, ComponentPropsWithoutRef<typeof SwitchPrimitive.Root> & { size?: 'sm' | 'md' }>(
  function Switch({ className, size = 'md', ...props }, ref) {
    return (
      <SwitchPrimitive.Root
        ref={ref}
        className={cn(
          'inline-flex shrink-0 cursor-pointer items-center rounded-full border border-transparent p-0.5 transition-colors duration-150 ease-out focus-ring',
          'bg-border-strong data-[state=checked]:bg-accent disabled:cursor-not-allowed disabled:opacity-50',
          size === 'sm' ? 'h-4 w-7' : 'h-5 w-9',
          className,
        )}
        {...props}
      >
        <SwitchPrimitive.Thumb
          className={cn(
            'pointer-events-none block rounded-full bg-surface shadow-popover transition-transform duration-150 ease-out',
            'data-[state=unchecked]:translate-x-0',
            size === 'sm' ? 'size-3 data-[state=checked]:translate-x-3' : 'size-4 data-[state=checked]:translate-x-4',
          )}
        />
      </SwitchPrimitive.Root>
    )
  },
)
