import { Label as LabelPrimitive } from 'radix-ui'
import { useId, type ComponentPropsWithoutRef, type ReactElement, type ReactNode, cloneElement, isValidElement } from 'react'
import { cn } from '@/lib/cn'

export function Label({ className, ...props }: ComponentPropsWithoutRef<typeof LabelPrimitive.Root>) {
  return <LabelPrimitive.Root className={cn('text-sm font-medium text-text', className)} {...props} />
}

export interface FieldProps {
  label: ReactNode
  hint?: ReactNode
  error?: ReactNode
  optional?: boolean
  className?: string
  /** A single form control; it receives id / aria-describedby / aria-invalid. */
  children: ReactElement<Record<string, unknown>>
}

/** Label + control + hint/error, wired for accessibility. */
export function Field({ label, hint, error, optional, className, children }: FieldProps) {
  const id = useId()
  const describedBy = error ? `${id}-error` : hint ? `${id}-hint` : undefined
  const control = isValidElement(children)
    ? cloneElement(children, { id, 'aria-describedby': describedBy, invalid: error ? true : undefined })
    : children
  return (
    <div className={cn('flex flex-col gap-1.5', className)}>
      <Label htmlFor={id} className="flex items-baseline gap-1.5">
        {label}
        {optional && <span className="text-xs font-normal text-text-muted">Optional</span>}
      </Label>
      {control}
      {error ? (
        <p id={`${id}-error`} className="text-xs text-fail">
          {error}
        </p>
      ) : hint ? (
        <p id={`${id}-hint`} className="text-xs text-text-muted">
          {hint}
        </p>
      ) : null}
    </div>
  )
}
