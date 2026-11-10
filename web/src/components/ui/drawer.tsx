import { X } from 'lucide-react'
import { Dialog as DialogPrimitive } from 'radix-ui'
import type { ReactNode } from 'react'
import { cn } from '@/lib/cn'
import { IconButton } from './button'
import { DialogOverlay } from './dialog'

export interface DrawerProps {
  open?: boolean
  onOpenChange?: (open: boolean) => void
  trigger?: ReactNode
  title: ReactNode
  description?: ReactNode
  /** Extra header controls (e.g. prev/next), rendered before the close button. */
  actions?: ReactNode
  children?: ReactNode
  footer?: ReactNode
  size?: 'md' | 'lg' | 'xl'
  /** Keep the page interactive behind the drawer (no overlay). */
  modal?: boolean
}

const widths = { md: 'w-[480px]', lg: 'w-[720px]', xl: 'w-[960px]' }

/** Right-side panel for details (e.g. a Result) without leaving the page. */
export function Drawer({ open, onOpenChange, trigger, title, description, actions, children, footer, size = 'lg', modal = true }: DrawerProps) {
  return (
    <DialogPrimitive.Root open={open} onOpenChange={onOpenChange} modal={modal}>
      {trigger && <DialogPrimitive.Trigger asChild>{trigger}</DialogPrimitive.Trigger>}
      <DialogPrimitive.Portal>
        {modal && <DialogOverlay className="bg-overlay/60" />}
        <DialogPrimitive.Content
          className={cn(
            'fixed inset-y-0 right-0 z-50 flex max-w-full flex-col border-l border-border bg-surface shadow-dialog',
            'animate-drawer focus:outline-none',
            widths[size],
          )}
        >
          <div className="flex h-14 shrink-0 items-center justify-between gap-3 border-b border-border px-5">
            <div className="min-w-0">
              <DialogPrimitive.Title className="truncate text-base font-semibold text-text">{title}</DialogPrimitive.Title>
              <DialogPrimitive.Description className={description ? 'truncate text-xs text-text-muted' : 'sr-only'}>
                {description ?? (typeof title === 'string' ? title : 'Details')}
              </DialogPrimitive.Description>
            </div>
            <div className="flex items-center gap-1">
              {actions}
              <DialogPrimitive.Close asChild>
                <IconButton label="Close" size="sm">
                  <X />
                </IconButton>
              </DialogPrimitive.Close>
            </div>
          </div>
          <div className="min-h-0 flex-1 overflow-y-auto p-5">{children}</div>
          {footer && <div className="flex items-center justify-end gap-2 border-t border-border px-5 py-3">{footer}</div>}
        </DialogPrimitive.Content>
      </DialogPrimitive.Portal>
    </DialogPrimitive.Root>
  )
}
