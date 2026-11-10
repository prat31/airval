import type { ReactNode } from 'react'
import { cn } from '@/lib/cn'

/** Page container: consistent gutters and max width for every screen. */
export function Screen({ children, width = 'default', className }: { children: ReactNode; width?: 'narrow' | 'default' | 'wide' | 'full'; className?: string }) {
  const max = { narrow: 'max-w-3xl', default: 'max-w-[1280px]', wide: 'max-w-[1600px]', full: 'max-w-none' }[width]
  return <div className={cn('mx-auto flex w-full flex-col gap-6 px-4 py-6 sm:px-6 lg:px-8', max, className)}>{children}</div>
}
