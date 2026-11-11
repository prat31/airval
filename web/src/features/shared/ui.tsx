/**
 * Small composites shared by the configuration screens. Everything here is built from `components/ui` primitives
 * and design tokens; nothing is visual one-off.
 */
import { Plus, RefreshCw, Trash2, X } from 'lucide-react'
import { useEffect, useMemo, useRef, useState, type KeyboardEvent, type ReactNode } from 'react'
import { Button, IconButton } from '@/components/ui/button'
import { Dialog } from '@/components/ui/dialog'
import { EmptyState } from '@/components/ui/empty-state'
import { Input, Textarea } from '@/components/ui/input'
import { Tooltip } from '@/components/ui/tooltip'
import { cn } from '@/lib/cn'
import { formatDateTime, formatRelative } from '@/lib/format'
import { errorMessage } from './api'

/** Debounce a fast-changing value (search boxes, live previews). */
export function useDebounced<T>(value: T, ms = 250): T {
  const [v, setV] = useState(value)
  useEffect(() => {
    const t = setTimeout(() => setV(value), ms)
    return () => clearTimeout(t)
  }, [value, ms])
  return v
}

/** Inline error with retry. Used when a query fails; never swallows the API message. */
export function ErrorPanel({ error, onRetry, className }: { error: unknown; onRetry?: () => void; className?: string }) {
  return (
    <EmptyState
      compact
      illustration="error"
      className={className}
      title="Couldn't load this"
      description={errorMessage(error)}
      action={
        onRetry ? (
          <Button size="sm" icon={<RefreshCw />} onClick={onRetry}>
            Try again
          </Button>
        ) : undefined
      }
    />
  )
}

export function RelTime({ iso, className }: { iso?: string | null; className?: string }) {
  if (!iso) return <span className={cn('text-text-muted', className)}>never</span>
  const d = new Date(iso)
  return (
    <Tooltip content={formatDateTime(d)}>
      <time dateTime={iso} className={cn('text-text-2', className)}>
        {formatRelative(d)}
      </time>
    </Tooltip>
  )
}

/** A confirm dialog for destructive or irreversible actions. */
export function ConfirmDialog({
  open,
  onOpenChange,
  title,
  description,
  confirmLabel = 'Delete',
  danger = true,
  loading,
  onConfirm,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  title: ReactNode
  description?: ReactNode
  confirmLabel?: string
  danger?: boolean
  loading?: boolean
  onConfirm: () => void
}) {
  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      size="sm"
      title={title}
      description={description}
      footer={
        <>
          <Button variant="ghost" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button variant={danger ? 'danger' : 'primary'} loading={loading} onClick={onConfirm}>
            {confirmLabel}
          </Button>
        </>
      }
    />
  )
}

/** Titled group of form fields inside a card. */
export function FormSection({ title, description, children, className }: { title: ReactNode; description?: ReactNode; children: ReactNode; className?: string }) {
  return (
    <section className={cn('grid grid-cols-1 gap-x-8 gap-y-3 border-b border-border px-4 py-5 last:border-b-0 lg:grid-cols-[220px_1fr]', className)}>
      <div>
        <h3 className="text-sm font-semibold text-text">{title}</h3>
        {description && <p className="mt-1 text-xs text-text-muted">{description}</p>}
      </div>
      <div className="flex min-w-0 flex-col gap-4">{children}</div>
    </section>
  )
}

export function FieldRow({ children, cols = 2 }: { children: ReactNode; cols?: 2 | 3 }) {
  return <div className={cn('grid grid-cols-1 gap-3', cols === 2 ? 'sm:grid-cols-2' : 'sm:grid-cols-3')}>{children}</div>
}

/* -------------------------------------------------------------------------------------------------
 * TagInput: chips + free text. Enter / comma adds, Backspace removes the last.
 * ----------------------------------------------------------------------------------------------- */
export function TagInput({
  value,
  onChange,
  placeholder = 'Add…',
  id,
  suggestions,
  className,
}: {
  value: string[]
  onChange: (v: string[]) => void
  placeholder?: string
  id?: string
  suggestions?: string[]
  className?: string
}) {
  const [draft, setDraft] = useState('')
  const commit = (raw: string) => {
    const parts = raw
      .split(',')
      .map((s) => s.trim())
      .filter(Boolean)
    if (parts.length) onChange([...value, ...parts.filter((p) => !value.includes(p))])
    setDraft('')
  }
  const onKey = (e: KeyboardEvent<HTMLInputElement>) => {
    if (e.key === 'Enter' || e.key === ',') {
      if (draft.trim()) {
        e.preventDefault()
        commit(draft)
      }
    } else if (e.key === 'Backspace' && !draft && value.length) onChange(value.slice(0, -1))
  }
  const remaining = suggestions?.filter((s) => !value.includes(s)).slice(0, 6) ?? []
  return (
    <div className={cn('flex flex-col gap-1.5', className)}>
      <div className="flex min-h-8 flex-wrap items-center gap-1 rounded-sm border border-border bg-surface px-1.5 py-1 transition-[border-color,box-shadow] duration-150 focus-within:border-accent focus-within:ring-2 focus-within:ring-accent-subtle hover:border-border-strong">
        {value.map((t) => (
          <span key={t} className="inline-flex h-5 items-center gap-1 rounded-sm bg-surface-2 pl-1.5 pr-0.5 text-xs font-medium text-text-2 ring-1 ring-inset ring-border">
            {t}
            <button type="button" aria-label={`Remove ${t}`} className="flex size-4 items-center justify-center rounded-sm text-text-muted hover:bg-surface-hover hover:text-text focus-ring" onClick={() => onChange(value.filter((x) => x !== t))}>
              <X className="size-3" />
            </button>
          </span>
        ))}
        <input
          id={id}
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          onKeyDown={onKey}
          onBlur={() => draft.trim() && commit(draft)}
          placeholder={value.length ? '' : placeholder}
          className="h-5 min-w-24 flex-1 bg-transparent px-1 text-sm text-text outline-none placeholder:text-text-muted"
        />
      </div>
      {remaining.length > 0 && (
        <div className="flex flex-wrap gap-1">
          {remaining.map((s) => (
            <button key={s} type="button" onClick={() => onChange([...value, s])} className="inline-flex h-5 items-center gap-1 rounded-sm px-1.5 text-xs text-text-muted ring-1 ring-inset ring-border hover:bg-surface-hover hover:text-text focus-ring">
              <Plus className="size-3" />
              {s}
            </button>
          ))}
        </div>
      )}
    </div>
  )
}

/* -------------------------------------------------------------------------------------------------
 * KvEditor: editable string map (headers, query, variables).
 * ----------------------------------------------------------------------------------------------- */
type Row = { id: number; k: string; v: string }

export function KvEditor({
  value,
  onChange,
  keyPlaceholder = 'Key',
  valuePlaceholder = 'Value',
  addLabel = 'Add',
  monoValue,
  className,
}: {
  value: Record<string, string> | undefined
  onChange: (v: Record<string, string>) => void
  keyPlaceholder?: string
  valuePlaceholder?: string
  addLabel?: string
  monoValue?: boolean
  className?: string
}) {
  const nextId = useRef(1)
  const fromValue = (v: Record<string, string> | undefined): Row[] => Object.entries(v ?? {}).map(([k, val]) => ({ id: nextId.current++, k, v: String(val) }))
  const [rows, setRows] = useState<Row[]>(() => fromValue(value))
  const derived = (rs: Row[]) => Object.fromEntries(rs.filter((r) => r.k.trim()).map((r) => [r.k.trim(), r.v]))
  // Re-seed only when the outside value genuinely differs from what the rows already express (e.g. version restore).
  const outside = JSON.stringify(value ?? {})
  const [seen, setSeen] = useState(outside)
  if (outside !== seen) {
    setSeen(outside)
    if (outside !== JSON.stringify(derived(rows))) setRows(fromValue(value))
  }
  const commit = (rs: Row[]) => {
    setRows(rs)
    const d = derived(rs)
    setSeen(JSON.stringify(d))
    onChange(d)
  }
  return (
    <div className={cn('flex flex-col gap-1.5', className)}>
      {rows.map((r) => (
        <div key={r.id} className="grid grid-cols-[minmax(0,2fr)_minmax(0,3fr)_auto] items-center gap-1.5">
          <Input size="sm" value={r.k} placeholder={keyPlaceholder} aria-label={keyPlaceholder} className="font-mono text-xs" onChange={(e) => commit(rows.map((x) => (x.id === r.id ? { ...x, k: e.target.value } : x)))} />
          <Input size="sm" value={r.v} placeholder={valuePlaceholder} aria-label={valuePlaceholder} className={cn(monoValue && 'font-mono text-xs')} onChange={(e) => commit(rows.map((x) => (x.id === r.id ? { ...x, v: e.target.value } : x)))} />
          <IconButton label="Remove row" size="sm" onClick={() => commit(rows.filter((x) => x.id !== r.id))}>
            <Trash2 />
          </IconButton>
        </div>
      ))}
      <div>
        <Button size="sm" variant="ghost" icon={<Plus />} onClick={() => setRows([...rows, { id: nextId.current++, k: '', v: '' }])}>
          {addLabel}
        </Button>
      </div>
    </div>
  )
}

/* -------------------------------------------------------------------------------------------------
 * TemplateField: mono editor for Go-template strings with insertable variable hints.
 * ----------------------------------------------------------------------------------------------- */
export interface TemplateHint {
  label: string
  insert: string
  title?: string
}

export function templateProblem(text: string): string | null {
  const open = (text.match(/\{\{/g) ?? []).length
  const close = (text.match(/\}\}/g) ?? []).length
  if (open !== close) return 'Unbalanced {{ }} in template'
  return null
}

export function TemplateField({
  value,
  onChange,
  hints = [],
  rows = 1,
  placeholder,
  id,
  invalid,
  ...rest
}: {
  value: string
  onChange: (v: string) => void
  hints?: TemplateHint[]
  rows?: number
  placeholder?: string
  id?: string
  invalid?: boolean
  'aria-describedby'?: string
}) {
  const ref = useRef<HTMLTextAreaElement | HTMLInputElement | null>(null)
  const problem = useMemo(() => templateProblem(value), [value])
  const insert = (text: string) => {
    const el = ref.current
    if (!el) return onChange(value + text)
    const s = el.selectionStart ?? value.length
    const e = el.selectionEnd ?? value.length
    const next = value.slice(0, s) + text + value.slice(e)
    onChange(next)
    requestAnimationFrame(() => {
      el.focus()
      el.setSelectionRange(s + text.length, s + text.length)
    })
  }
  return (
    <div className="flex flex-col gap-1.5">
      {rows <= 1 ? (
        <Input id={id} ref={(el) => void (ref.current = el)} value={value} placeholder={placeholder} invalid={invalid || !!problem} spellCheck={false} className="font-mono text-xs" onChange={(e) => onChange(e.target.value)} {...rest} />
      ) : (
        <Textarea id={id} ref={(el) => void (ref.current = el)} mono rows={rows} value={value} placeholder={placeholder} invalid={invalid || !!problem} spellCheck={false} onChange={(e) => onChange(e.target.value)} {...rest} />
      )}
      {problem && <p className="text-xs text-fail">{problem}</p>}
      {hints.length > 0 && (
        <div className="flex flex-wrap items-center gap-1">
          <span className="mr-0.5 text-xs text-text-muted">Insert</span>
          {hints.map((h) => (
            <Tooltip key={h.insert} content={h.title ?? h.insert}>
              <button type="button" onClick={() => insert(h.insert)} className="inline-flex h-5 items-center rounded-sm bg-surface-2 px-1.5 font-mono text-[11px] text-text-2 ring-1 ring-inset ring-border hover:bg-surface-hover hover:text-text focus-ring">
                {h.label}
              </button>
            </Tooltip>
          ))}
        </div>
      )}
    </div>
  )
}

/** Pretty JSON text for editors; falls back to the raw string. */
export function pretty(v: unknown): string {
  if (v === undefined || v === null) return ''
  if (typeof v === 'string') return v
  try {
    return JSON.stringify(v, null, 2)
  } catch {
    return String(v)
  }
}

export function tryParse(text: string): { ok: true; value: unknown } | { ok: false; error: string } {
  if (!text.trim()) return { ok: true, value: undefined }
  try {
    return { ok: true, value: JSON.parse(text) }
  } catch (e) {
    return { ok: false, error: e instanceof Error ? e.message : 'Invalid JSON' }
  }
}

/**
 * Mono JSON editor with live validation. Commits parsed values only while valid, so the parent never sees broken JSON.
 */
export function JsonEditor({
  value,
  onChange,
  rows = 6,
  id,
  placeholder,
}: {
  value: unknown
  onChange: (v: unknown) => void
  rows?: number
  id?: string
  placeholder?: string
}) {
  const [text, setText] = useState(() => pretty(value))
  const [error, setError] = useState<string | null>(null)
  const last = useRef(pretty(value))
  const incoming = pretty(value)
  if (incoming !== last.current && !error) {
    last.current = incoming
    if (incoming !== text) setText(incoming)
  }
  return (
    <div className="flex flex-col gap-1">
      <Textarea
        id={id}
        mono
        rows={rows}
        spellCheck={false}
        value={text}
        placeholder={placeholder}
        invalid={!!error}
        onChange={(e) => {
          const t = e.target.value
          setText(t)
          const r = tryParse(t)
          if (r.ok) {
            setError(null)
            last.current = pretty(r.value)
            onChange(r.value)
          } else setError(r.error)
        }}
      />
      {error && <p className="text-xs text-fail">{error}</p>}
    </div>
  )
}
