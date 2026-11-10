import { ChevronRight } from 'lucide-react'
import { Fragment, type ReactNode } from 'react'
import { cn } from '@/lib/cn'

export interface Crumb {
  label: ReactNode
  /** Rendered as a link via `renderLink` when provided. */
  href?: string
  icon?: ReactNode
}

export interface BreadcrumbsProps {
  items: Crumb[]
  /** Router-aware link renderer. Defaults to <a>. */
  renderLink?: (crumb: Crumb, className: string, children: ReactNode) => ReactNode
  className?: string
}

export function Breadcrumbs({ items, renderLink, className }: BreadcrumbsProps) {
  return (
    <nav aria-label="Breadcrumb" className={cn('min-w-0', className)}>
      <ol className="flex min-w-0 items-center gap-1 text-sm">
        {items.map((c, i) => {
          const last = i === items.length - 1
          const content = (
            <>
              {c.icon}
              <span className="truncate">{c.label}</span>
            </>
          )
          const linkClass =
            'flex min-w-0 items-center gap-1.5 rounded-sm px-1 py-0.5 text-text-muted transition-colors hover:text-text focus-ring [&_svg]:size-3.5'
          return (
            <Fragment key={i}>
              <li className={cn('flex min-w-0 items-center', last ? 'shrink' : 'shrink-[2]')}>
                {last || !c.href ? (
                  <span aria-current={last ? 'page' : undefined} className={cn('flex min-w-0 items-center gap-1.5 px-1 [&_svg]:size-3.5', last ? 'font-medium text-text' : 'text-text-muted')}>
                    {content}
                  </span>
                ) : renderLink ? (
                  renderLink(c, linkClass, content)
                ) : (
                  <a href={c.href} className={linkClass}>
                    {content}
                  </a>
                )}
              </li>
              {!last && (
                <li aria-hidden className="flex shrink-0 text-border-strong">
                  <ChevronRight className="size-3.5" />
                </li>
              )}
            </Fragment>
          )
        })}
      </ol>
    </nav>
  )
}
