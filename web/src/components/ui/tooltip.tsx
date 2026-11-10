import { Tooltip as TooltipPrimitive } from 'radix-ui'
import type { ReactNode } from 'react'
import { cn } from '@/lib/cn'
import { Kbd } from './kbd'

export const TooltipProvider = TooltipPrimitive.Provider

export interface TooltipProps {
  content: ReactNode
  /** Optional keyboard shortcut shown after the content, e.g. ['⌘', 'K']. */
  shortcut?: string[]
  side?: 'top' | 'right' | 'bottom' | 'left'
  align?: 'start' | 'center' | 'end'
  children: ReactNode
  delayDuration?: number
}

export function Tooltip({ content, shortcut, side = 'top', align = 'center', children, delayDuration }: TooltipProps) {
  return (
    <TooltipPrimitive.Root delayDuration={delayDuration}>
      <TooltipPrimitive.Trigger asChild>{children}</TooltipPrimitive.Trigger>
      <TooltipPrimitive.Portal>
        <TooltipPrimitive.Content
          side={side}
          align={align}
          sideOffset={6}
          collisionPadding={8}
          className={cn(
            'z-50 flex max-w-xs items-center gap-2 rounded-sm border border-border bg-surface px-2 py-1',
            'text-xs text-text shadow-popover animate-in',
          )}
        >
          {content}
          {shortcut && (
            <span className="flex gap-0.5">
              {shortcut.map((k) => (
                <Kbd key={k}>{k}</Kbd>
              ))}
            </span>
          )}
        </TooltipPrimitive.Content>
      </TooltipPrimitive.Portal>
    </TooltipPrimitive.Root>
  )
}
