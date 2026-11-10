import { cva, type VariantProps } from 'class-variance-authority'
import type { HTMLAttributes } from 'react'
import { cn } from '@/lib/cn'

const badgeVariants = cva(
  'inline-flex h-5 shrink-0 items-center gap-1 whitespace-nowrap rounded-sm px-1.5 text-xs font-medium [&_svg]:size-3',
  {
    variants: {
      variant: {
        neutral: 'bg-surface-2 text-text-2 ring-1 ring-inset ring-border',
        accent: 'bg-accent-subtle text-accent',
        outline: 'text-text-2 ring-1 ring-inset ring-border',
        pass: 'bg-pass-subtle text-pass',
        fail: 'bg-fail-subtle text-fail',
        warn: 'bg-warn-subtle text-warn',
      },
      mono: { true: 'font-mono text-[11px] font-normal' },
    },
    defaultVariants: { variant: 'neutral' },
  },
)

export interface BadgeProps extends HTMLAttributes<HTMLSpanElement>, VariantProps<typeof badgeVariants> {}

/** Small label: tags, labels (git sha), counts. Status meaning belongs to StatusPill. */
export function Badge({ className, variant, mono, ...props }: BadgeProps) {
  return <span className={cn(badgeVariants({ variant, mono }), className)} {...props} />
}
