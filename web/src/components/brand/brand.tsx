import { cn } from '@/lib/cn'

/**
 * The airval mark: a geometric lowercase "a" (bowl + stem) with a data point, on an iris tile.
 * Single source for the in-app logo; the static assets (favicon, PNG icons, docs/assets/logo.svg)
 * are exported from the same geometry. Colors come from --brand-mark / --brand-mark-ink, so the
 * mark is constant across themes and follows an enterprise white-label accent.
 */
export function BrandMark({ className, title }: { className?: string; title?: string }) {
  return (
    <svg
      viewBox="0 0 32 32"
      className={cn('size-8 shrink-0', className)}
      role={title ? 'img' : undefined}
      aria-hidden={title ? undefined : true}
      aria-label={title}
    >
      <rect width="32" height="32" rx="8" fill="var(--brand-mark)" />
      <circle cx="14" cy="17.5" r="5.3" fill="none" stroke="var(--brand-mark-ink)" strokeWidth="3" />
      <path d="M20.8 12.2V23.2" stroke="var(--brand-mark-ink)" strokeWidth="3" strokeLinecap="round" />
      <circle cx="23.6" cy="8.6" r="2.1" fill="var(--brand-mark-ink)" />
    </svg>
  )
}

/** Mark + "airval" wordmark (Inter semibold, tight tracking, text color). */
export function BrandLogo({ className, markClassName, wordmark = true, name = 'airval' }: { className?: string; markClassName?: string; wordmark?: boolean; name?: string }) {
  return (
    <span className={cn('inline-flex items-center gap-2', className)}>
      <BrandMark className={cn('size-7', markClassName)} />
      {wordmark && <span className="text-lg font-semibold tracking-tight text-text">{name}</span>}
    </span>
  )
}
