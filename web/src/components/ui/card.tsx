import type { HTMLAttributes, ReactNode } from 'react'
import { cn } from '@/lib/cn'

/** Surface container. Cards use a 1px border, never a shadow. */
export function Card({ className, ...props }: HTMLAttributes<HTMLDivElement>) {
  return <div className={cn('rounded-md border border-border bg-surface', className)} {...props} />
}

export interface CardHeaderProps extends Omit<HTMLAttributes<HTMLDivElement>, 'title'> {
  title: ReactNode
  description?: ReactNode
  actions?: ReactNode
}

export function CardHeader({ title, description, actions, className, ...props }: CardHeaderProps) {
  return (
    <div className={cn('flex items-start justify-between gap-3 px-4 pb-1 pt-3.5', className)} {...props}>
      <div className="min-w-0">
        <h3 className="truncate text-base font-semibold text-text">{title}</h3>
        {description && <p className="mt-0.5 text-xs text-text-muted">{description}</p>}
      </div>
      {actions && <div className="flex shrink-0 items-center gap-1">{actions}</div>}
    </div>
  )
}

export function CardBody({ className, ...props }: HTMLAttributes<HTMLDivElement>) {
  return <div className={cn('px-4 py-3', className)} {...props} />
}

export function CardFooter({ className, ...props }: HTMLAttributes<HTMLDivElement>) {
  return <div className={cn('flex items-center gap-2 border-t border-border px-4 py-2.5', className)} {...props} />
}
