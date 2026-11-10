import { Check } from 'lucide-react'
import { Fragment, type ReactNode } from 'react'
import { cn } from '@/lib/cn'

export interface Step {
  id: string
  label: ReactNode
  description?: ReactNode
  /** Mark a step as having a validation problem. */
  invalid?: boolean
}

export interface StepperProps {
  steps: Step[]
  /** Index of the current step. */
  current: number
  /** Allow jumping to completed (and current) steps. */
  onStepClick?: (index: number) => void
  orientation?: 'horizontal' | 'vertical'
  className?: string
}

export function Stepper({ steps, current, onStepClick, orientation = 'horizontal', className }: StepperProps) {
  const vertical = orientation === 'vertical'
  return (
    <ol className={cn('flex', vertical ? 'flex-col gap-1' : 'items-center gap-2', className)} aria-label="Progress">
      {steps.map((s, i) => {
        const state = i < current ? 'complete' : i === current ? 'current' : 'upcoming'
        const clickable = onStepClick && i <= current
        const Comp = clickable ? 'button' : 'div'
        return (
          <Fragment key={s.id}>
            <li className={cn(!vertical && 'min-w-0', vertical && 'w-full')}>
              <Comp
                {...(clickable ? { type: 'button' as const, onClick: () => onStepClick(i) } : {})}
                aria-current={state === 'current' ? 'step' : undefined}
                className={cn(
                  'group flex min-w-0 items-center gap-2.5 rounded-md text-left',
                  vertical ? 'w-full px-2 py-1.5' : 'py-1',
                  clickable && 'focus-ring hover:bg-surface-hover',
                  clickable && !vertical && 'px-1.5 -mx-1.5',
                  vertical && state === 'current' && 'bg-surface-hover',
                )}
              >
                <span
                  className={cn(
                    'tabular flex size-6 shrink-0 items-center justify-center rounded-full border text-xs font-semibold transition-colors',
                    state === 'complete' && 'border-accent bg-accent text-on-accent',
                    state === 'current' && 'border-accent bg-accent-subtle text-accent',
                    state === 'upcoming' && 'border-border-strong bg-surface text-text-muted',
                    s.invalid && 'border-fail bg-fail-subtle text-fail',
                  )}
                >
                  {state === 'complete' && !s.invalid ? <Check className="size-3.5" strokeWidth={2.5} /> : i + 1}
                </span>
                <span className="flex min-w-0 flex-col">
                  <span className={cn('truncate text-sm font-medium', state === 'upcoming' ? 'text-text-muted' : 'text-text')}>{s.label}</span>
                  {s.description && vertical && <span className="truncate text-xs text-text-muted">{s.description}</span>}
                </span>
              </Comp>
            </li>
            {!vertical && i < steps.length - 1 && (
              <li aria-hidden className={cn('h-px min-w-6 flex-1', i < current ? 'bg-accent' : 'bg-border')} />
            )}
          </Fragment>
        )
      })}
    </ol>
  )
}
