import type { ReactNode } from 'react'
import { cn } from '@/lib/cn'

export interface PageHeaderProps {
  title: ReactNode
  description?: ReactNode
  /** Small icon tile before the title. */
  icon?: ReactNode
  /** Status pills / badges next to the title. */
  meta?: ReactNode
  /** Right-aligned actions (primary action last). */
  actions?: ReactNode
  /** Tabs or secondary nav rendered under the header. */
  tabs?: ReactNode
  className?: string
}

export function PageHeader({ title, description, icon, meta, actions, tabs, className }: PageHeaderProps) {
  return (
    <header className={cn('flex flex-col gap-4', tabs ? 'pb-0' : 'pb-2', className)}>
      <div className="flex flex-wrap items-start justify-between gap-x-6 gap-y-3">
        <div className="flex min-w-0 items-start gap-3">
          {icon && (
            <div className="mt-0.5 flex size-9 shrink-0 items-center justify-center rounded-md border border-border bg-surface text-text-2 [&_svg]:size-[18px]">
              {icon}
            </div>
          )}
          <div className="min-w-0">
            <div className="flex flex-wrap items-center gap-2">
              <h1 className="truncate text-lg font-semibold tracking-tight text-text">{title}</h1>
              {meta}
            </div>
            {description && <p className="mt-0.5 max-w-2xl text-sm text-text-2">{description}</p>}
          </div>
        </div>
        {actions && <div className="flex shrink-0 items-center gap-2">{actions}</div>}
      </div>
      {tabs}
    </header>
  )
}
