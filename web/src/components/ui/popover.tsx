import { Popover as PopoverPrimitive } from 'radix-ui'
import { forwardRef, type ComponentPropsWithoutRef } from 'react'
import { cn } from '@/lib/cn'
import { overlaySurface } from './overlay'

export const Popover = PopoverPrimitive.Root
export const PopoverTrigger = PopoverPrimitive.Trigger
export const PopoverAnchor = PopoverPrimitive.Anchor
export const PopoverClose = PopoverPrimitive.Close

export const PopoverContent = forwardRef<HTMLDivElement, ComponentPropsWithoutRef<typeof PopoverPrimitive.Content>>(
  function PopoverContent({ className, align = 'start', sideOffset = 6, ...props }, ref) {
    return (
      <PopoverPrimitive.Portal>
        <PopoverPrimitive.Content
          ref={ref}
          align={align}
          sideOffset={sideOffset}
          collisionPadding={12}
          className={cn(overlaySurface, 'p-3', className)}
          {...props}
        />
      </PopoverPrimitive.Portal>
    )
  },
)
