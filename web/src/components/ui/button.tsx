import { cva, type VariantProps } from 'class-variance-authority'
import { Loader2 } from 'lucide-react'
import { Slot } from 'radix-ui'
import { forwardRef, type ButtonHTMLAttributes, type ReactNode } from 'react'
import { cn } from '@/lib/cn'

export const buttonVariants = cva(
  [
    'relative inline-flex shrink-0 select-none items-center justify-center gap-1.5 whitespace-nowrap font-medium',
    'rounded-md border transition-[background-color,border-color,color,box-shadow] duration-150 ease-out',
    'focus-ring disabled:pointer-events-none disabled:opacity-50',
    '[&_svg]:pointer-events-none [&_svg]:shrink-0',
  ],
  {
    variants: {
      variant: {
        primary: 'border-transparent bg-accent text-on-accent hover:bg-accent-hover',
        secondary: 'border-border bg-surface text-text hover:border-border-strong hover:bg-surface-hover',
        ghost: 'border-transparent bg-transparent text-text-2 hover:bg-surface-hover hover:text-text',
        danger: 'border-transparent bg-fail text-on-accent hover:opacity-90',
      },
      size: {
        sm: 'h-7 px-2.5 text-sm [&_svg]:size-3.5',
        md: 'h-8 px-3 text-sm [&_svg]:size-4',
      },
    },
    defaultVariants: { variant: 'secondary', size: 'md' },
  },
)

export interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement>, VariantProps<typeof buttonVariants> {
  asChild?: boolean
  loading?: boolean
  /** Icon rendered before the label. */
  icon?: ReactNode
  /** Icon rendered after the label. */
  iconRight?: ReactNode
}

export const Button = forwardRef<HTMLButtonElement, ButtonProps>(function Button(
  { className, variant, size, asChild, loading, icon, iconRight, children, disabled, type, ...props },
  ref,
) {
  const Comp = asChild ? Slot.Root : 'button'
  if (asChild) {
    return (
      <Comp ref={ref} className={cn(buttonVariants({ variant, size }), className)} {...props}>
        {children}
      </Comp>
    )
  }
  return (
    <button
      ref={ref}
      type={type ?? 'button'}
      className={cn(buttonVariants({ variant, size }), className)}
      disabled={disabled || loading}
      aria-busy={loading || undefined}
      {...props}
    >
      {loading ? <Loader2 className="animate-spin" aria-hidden /> : icon}
      {children}
      {iconRight}
    </button>
  )
})

const iconButtonVariants = cva(
  [
    'inline-flex shrink-0 items-center justify-center rounded-md border transition-colors duration-150 ease-out',
    'focus-ring disabled:pointer-events-none disabled:opacity-50 [&_svg]:shrink-0',
  ],
  {
    variants: {
      variant: {
        ghost: 'border-transparent text-text-muted hover:bg-surface-hover hover:text-text',
        secondary: 'border-border bg-surface text-text-2 hover:border-border-strong hover:bg-surface-hover hover:text-text',
        primary: 'border-transparent bg-accent text-on-accent hover:bg-accent-hover',
      },
      size: {
        sm: 'size-7 [&_svg]:size-3.5',
        md: 'size-8 [&_svg]:size-4',
      },
    },
    defaultVariants: { variant: 'ghost', size: 'md' },
  },
)

export interface IconButtonProps extends ButtonHTMLAttributes<HTMLButtonElement>, VariantProps<typeof iconButtonVariants> {
  /** Accessible name — required because the button has no visible text. */
  label: string
}

export const IconButton = forwardRef<HTMLButtonElement, IconButtonProps>(function IconButton(
  { className, variant, size, label, type, children, ...props },
  ref,
) {
  return (
    <button
      ref={ref}
      type={type ?? 'button'}
      aria-label={label}
      className={cn(iconButtonVariants({ variant, size }), className)}
      {...props}
    >
      {children}
    </button>
  )
})
