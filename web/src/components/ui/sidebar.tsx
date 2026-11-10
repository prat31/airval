import { Slot } from 'radix-ui'
import { createContext, forwardRef, useContext, type AnchorHTMLAttributes, type HTMLAttributes, type ReactNode } from 'react'
import { cn } from '@/lib/cn'
import { Tooltip } from './tooltip'

const SidebarContext = createContext({ collapsed: false })
export const useSidebar = () => useContext(SidebarContext)

export interface SidebarProps extends HTMLAttributes<HTMLElement> {
  collapsed?: boolean
  header?: ReactNode
  footer?: ReactNode
}

/** App navigation rail. Collapses to icons (labels move into tooltips). */
export function Sidebar({ collapsed = false, header, footer, className, children, ...props }: SidebarProps) {
  return (
    <SidebarContext.Provider value={{ collapsed }}>
      <aside
        data-collapsed={collapsed || undefined}
        className={cn(
          'flex h-full shrink-0 flex-col border-r border-border bg-bg transition-[width] duration-200 ease-out',
          collapsed ? 'w-(--sidebar-width-collapsed)' : 'w-(--sidebar-width)',
          className,
        )}
        {...props}
      >
        {header && <div className={cn('flex shrink-0 flex-col gap-1 p-2', collapsed && 'items-center')}>{header}</div>}
        <nav className={cn('flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto overflow-x-hidden p-2', collapsed && 'items-center')}>
          {children}
        </nav>
        {footer && <div className={cn('flex shrink-0 flex-col gap-1 border-t border-border p-2', collapsed && 'items-center')}>{footer}</div>}
      </aside>
    </SidebarContext.Provider>
  )
}

export function SidebarSection({ title, children, className }: { title?: string; children: ReactNode; className?: string }) {
  const { collapsed } = useSidebar()
  return (
    <div className={cn('flex w-full flex-col gap-px', className)} role="group" aria-label={title}>
      {title && !collapsed && <div className="px-2 pb-1 text-xs font-medium text-text-muted">{title}</div>}
      {children}
    </div>
  )
}

export interface SidebarItemProps extends AnchorHTMLAttributes<HTMLAnchorElement> {
  icon: ReactNode
  label: string
  active?: boolean
  /** Count or status shown at the right. */
  badge?: ReactNode
  /** Shortcut hint shown in the collapsed tooltip, e.g. ['G', 'R']. */
  shortcut?: string[]
  asChild?: boolean
}

export const SidebarItem = forwardRef<HTMLAnchorElement, SidebarItemProps>(function SidebarItem(
  { icon, label, active, badge, shortcut, asChild, className, children, ...props },
  ref,
) {
  const { collapsed } = useSidebar()
  const Comp = asChild ? Slot.Root : 'a'
  const inner = (
    <Comp
      ref={ref}
      aria-current={active ? 'page' : undefined}
      className={cn(
        'group flex h-8 items-center gap-2.5 rounded-md text-sm font-medium transition-colors duration-150 focus-ring',
        '[&_svg]:size-4 [&_svg]:shrink-0',
        collapsed ? 'size-9 justify-center' : 'w-full px-2',
        active
          ? 'bg-surface text-text shadow-[0_0_0_1px_var(--border)] [&_svg]:text-accent'
          : 'text-text-2 hover:bg-surface-hover hover:text-text [&_svg]:text-text-muted hover:[&_svg]:text-text-2',
        className,
      )}
      {...props}
    >
      {asChild ? (
        children
      ) : (
        <>
          {icon}
          {!collapsed && <span className="flex-1 truncate">{label}</span>}
          {!collapsed && badge}
        </>
      )}
    </Comp>
  )
  return collapsed ? (
    <Tooltip content={label} shortcut={shortcut} side="right">
      {inner}
    </Tooltip>
  ) : (
    inner
  )
})
