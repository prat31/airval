import type { HTMLAttributes } from 'react'
import { cn } from '@/lib/cn'

/** Shimmering placeholder. Compose skeletons that match the final layout. */
export function Skeleton({ className, ...props }: HTMLAttributes<HTMLDivElement>) {
  return (
    <div
      aria-hidden
      className={cn(
        'animate-shimmer rounded-sm bg-[linear-gradient(90deg,var(--surface-2)_0%,var(--surface-hover)_50%,var(--surface-2)_100%)] bg-[length:200%_100%]',
        className,
      )}
      {...props}
    />
  )
}

export function SkeletonText({ lines = 3, className }: { lines?: number; className?: string }) {
  return (
    <div className={cn('flex flex-col gap-2', className)} aria-hidden>
      {Array.from({ length: lines }, (_, i) => (
        <Skeleton key={i} className={cn('h-3', i === lines - 1 ? 'w-3/5' : 'w-full')} />
      ))}
    </div>
  )
}

/** Skeleton of a data table: header + rows. */
export function SkeletonTable({ rows = 6, columns = 5, className }: { rows?: number; columns?: number; className?: string }) {
  return (
    <div className={cn('overflow-hidden rounded-md border border-border bg-surface', className)} role="status" aria-label="Loading">
      <div className="flex h-9 items-center gap-6 border-b border-border bg-surface-2 px-4">
        {Array.from({ length: columns }, (_, i) => (
          <Skeleton key={i} className={cn('h-2.5', i === 0 ? 'w-32' : 'w-16')} />
        ))}
      </div>
      {Array.from({ length: rows }, (_, r) => (
        <div key={r} className="flex h-11 items-center gap-6 border-b border-border px-4 last:border-b-0">
          {Array.from({ length: columns }, (_, i) => (
            <Skeleton key={i} className={cn('h-3', i === 0 ? 'w-48' : 'w-16')} style={{ opacity: 1 - r * 0.1 }} />
          ))}
        </div>
      ))}
    </div>
  )
}
