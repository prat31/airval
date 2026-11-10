import { forwardRef, type InputHTMLAttributes, type ReactNode, type TextareaHTMLAttributes } from 'react'
import { cn } from '@/lib/cn'

export const fieldBase = [
  'w-full rounded-sm border border-border bg-surface text-text placeholder:text-text-muted',
  'transition-[border-color,box-shadow] duration-150 ease-out',
  'hover:border-border-strong focus:outline-none focus-visible:border-accent focus-visible:ring-2 focus-visible:ring-accent-subtle focus-visible:outline-none',
  'disabled:cursor-not-allowed disabled:opacity-50',
  'aria-[invalid=true]:border-fail aria-[invalid=true]:focus-visible:ring-fail-subtle',
].join(' ')

export interface InputProps extends Omit<InputHTMLAttributes<HTMLInputElement>, 'size'> {
  size?: 'sm' | 'md'
  /** Leading adornment, e.g. a search icon. */
  leading?: ReactNode
  /** Trailing adornment, e.g. a keyboard hint. */
  trailing?: ReactNode
  invalid?: boolean
}

export const Input = forwardRef<HTMLInputElement, InputProps>(function Input(
  { className, size = 'md', leading, trailing, invalid, ...props },
  ref,
) {
  const input = (
    <input
      ref={ref}
      aria-invalid={invalid || undefined}
      className={cn(
        fieldBase,
        size === 'sm' ? 'h-7 px-2 text-sm' : 'h-8 px-2.5 text-sm',
        leading ? 'pl-8' : undefined,
        trailing ? 'pr-9' : undefined,
        !leading && !trailing && className,
      )}
      {...props}
    />
  )
  if (!leading && !trailing) return input
  return (
    <div className={cn('relative flex items-center', className)}>
      {leading && (
        <span className="pointer-events-none absolute left-2.5 flex text-text-muted [&_svg]:size-3.5">{leading}</span>
      )}
      {input}
      {trailing && <span className="absolute right-2 flex items-center text-text-muted">{trailing}</span>}
    </div>
  )
})

export interface TextareaProps extends TextareaHTMLAttributes<HTMLTextAreaElement> {
  invalid?: boolean
  mono?: boolean
}

export const Textarea = forwardRef<HTMLTextAreaElement, TextareaProps>(function Textarea(
  { className, invalid, mono, rows = 4, ...props },
  ref,
) {
  return (
    <textarea
      ref={ref}
      rows={rows}
      aria-invalid={invalid || undefined}
      className={cn(fieldBase, 'min-h-16 resize-y px-2.5 py-1.5 text-sm', mono && 'font-mono text-xs leading-5', className)}
      {...props}
    />
  )
})
