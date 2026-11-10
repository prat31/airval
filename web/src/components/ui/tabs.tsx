import { Tabs as TabsPrimitive } from 'radix-ui'
import { forwardRef, type ComponentPropsWithoutRef } from 'react'
import { cn } from '@/lib/cn'

export const Tabs = TabsPrimitive.Root

export const TabsList = forwardRef<HTMLDivElement, ComponentPropsWithoutRef<typeof TabsPrimitive.List>>(function TabsList(
  { className, ...props },
  ref,
) {
  return <TabsPrimitive.List ref={ref} className={cn('flex items-center gap-4 border-b border-border', className)} {...props} />
})

export const TabsTrigger = forwardRef<HTMLButtonElement, ComponentPropsWithoutRef<typeof TabsPrimitive.Trigger>>(
  function TabsTrigger({ className, ...props }, ref) {
    return (
      <TabsPrimitive.Trigger
        ref={ref}
        className={cn(
          'relative -mb-px inline-flex h-9 items-center gap-1.5 border-b-2 border-transparent text-sm font-medium text-text-muted',
          'transition-colors duration-150 hover:text-text focus-visible:outline-none focus-visible:text-text',
          'focus-visible:after:absolute focus-visible:after:inset-x-[-4px] focus-visible:after:inset-y-1.5 focus-visible:after:rounded-sm focus-visible:after:ring-2 focus-visible:after:ring-accent',
          'data-[state=active]:border-accent data-[state=active]:text-text [&_svg]:size-4',
          className,
        )}
        {...props}
      />
    )
  },
)

export const TabsContent = forwardRef<HTMLDivElement, ComponentPropsWithoutRef<typeof TabsPrimitive.Content>>(
  function TabsContent({ className, ...props }, ref) {
    return <TabsPrimitive.Content ref={ref} className={cn('pt-4 focus-visible:outline-none', className)} {...props} />
  },
)
