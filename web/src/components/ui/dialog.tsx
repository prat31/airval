import { X } from 'lucide-react'
import { Dialog as DialogPrimitive } from 'radix-ui'
import type { ReactNode } from 'react'
import { cn } from '@/lib/cn'
import { IconButton } from './button'

export const DialogRoot = DialogPrimitive.Root
export const DialogTrigger = DialogPrimitive.Trigger
export const DialogClose = DialogPrimitive.Close

export function DialogOverlay({ className }: { className?: string }) {
  return <DialogPrimitive.Overlay className={cn('fixed inset-0 z-50 bg-overlay animate-fade', className)} />
}

export interface DialogProps {
  open?: boolean
  onOpenChange?: (open: boolean) => void
  trigger?: ReactNode
  title: ReactNode
  description?: ReactNode
  children?: ReactNode
  /** Footer actions, right-aligned. */
  footer?: ReactNode
  size?: 'sm' | 'md' | 'lg'
  className?: string
}

const widths = { sm: 'max-w-sm', md: 'max-w-lg', lg: 'max-w-3xl' }

export function Dialog({ open, onOpenChange, trigger, title, description, children, footer, size = 'md', className }: DialogProps) {
  return (
    <DialogPrimitive.Root open={open} onOpenChange={onOpenChange}>
      {trigger && <DialogPrimitive.Trigger asChild>{trigger}</DialogPrimitive.Trigger>}
      <DialogPrimitive.Portal>
        <DialogOverlay />
        <DialogPrimitive.Content
          className={cn(
            'fixed left-1/2 top-[12vh] z-50 flex max-h-[76vh] w-[calc(100vw-32px)] -translate-x-1/2 flex-col',
            'rounded-lg border border-border bg-surface shadow-dialog animate-in focus:outline-none',
            widths[size],
            className,
          )}
        >
          <div className="flex items-start justify-between gap-4 px-5 pb-2 pt-4">
            <div className="flex flex-col gap-1">
              <DialogPrimitive.Title className="text-md font-semibold text-text">{title}</DialogPrimitive.Title>
              {description ? (
                <DialogPrimitive.Description className="text-sm text-text-2">{description}</DialogPrimitive.Description>
              ) : (
                <DialogPrimitive.Description className="sr-only">{typeof title === 'string' ? title : 'Dialog'}</DialogPrimitive.Description>
              )}
            </div>
            <DialogPrimitive.Close asChild>
              <IconButton label="Close" size="sm" className="-mr-1.5">
                <X />
              </IconButton>
            </DialogPrimitive.Close>
          </div>
          {children && <div className="min-h-0 flex-1 overflow-y-auto px-5 py-3">{children}</div>}
          {footer && (
            <div className="flex items-center justify-end gap-2 border-t border-border px-5 py-3">{footer}</div>
          )}
        </DialogPrimitive.Content>
      </DialogPrimitive.Portal>
    </DialogPrimitive.Root>
  )
}
