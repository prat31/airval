/**
 * Spreadsheet-like example editor: virtualized rows, keyboard navigation, inline editing, row selection.
 * Cells are plain text; JSON values round-trip as JSON text. Only the draft is editable.
 */
import { useVirtualizer } from '@tanstack/react-virtual'
import { Plus } from 'lucide-react'
import { useCallback, useEffect, useRef, useState, type KeyboardEvent, type ReactNode } from 'react'
import { Checkbox } from '@/components/ui/checkbox'
import { Skeleton } from '@/components/ui/skeleton'
import { cn } from '@/lib/cn'
import type { S } from '../shared/api'

type Example = S['Example']

export interface Col {
  id: string
  label: string
  kind: 'input' | 'expected' | 'context' | 'tags'
  key?: string
  width: string
}

export const ROW_H = 36

const asText = (v: unknown): string => (v === undefined || v === null ? '' : typeof v === 'string' ? v : JSON.stringify(v))

export function cellText(ex: Example, col: Col): string {
  switch (col.kind) {
    case 'input':
      return asText(ex.input?.[col.key!])
    case 'expected':
      return asText(ex.expected)
    case 'context':
      return (ex.context ?? []).join('\n')
    case 'tags':
      return (ex.tags ?? []).join(', ')
  }
}

function parseLoose(text: string, original: unknown): unknown {
  const t = text.trim()
  if (t === '') return undefined
  const wasStructured = original !== undefined && original !== null && typeof original !== 'string'
  if (wasStructured || t.startsWith('{') || t.startsWith('[')) {
    try {
      return JSON.parse(t)
    } catch {
      /* keep as text */
    }
  }
  return text
}

/** Full example payload with one cell changed (updates send the whole example). */
export function applyEdit(ex: Example, col: Col, text: string): S['ExampleData'] {
  const base: S['ExampleData'] = { id: ex.id, input: { ...ex.input }, expected: ex.expected, context: ex.context, metadata: ex.metadata, tags: ex.tags, conversation: ex.conversation }
  switch (col.kind) {
    case 'input': {
      const v = parseLoose(text, ex.input?.[col.key!])
      if (v === undefined) delete base.input![col.key!]
      else base.input![col.key!] = v
      break
    }
    case 'expected':
      base.expected = parseLoose(text, ex.expected) ?? null
      break
    case 'context':
      base.context = text.split('\n').map((s) => s.trim()).filter(Boolean)
      break
    case 'tags':
      base.tags = text.split(',').map((s) => s.trim()).filter(Boolean)
      break
  }
  return base
}

/** Derive input-key columns from the data (and schema hints), plus the fixed columns. */
export function buildColumns(examples: Example[], schemaFields: string[] = [], extraKeys: string[] = []): Col[] {
  const keys = [...schemaFields]
  for (const e of examples.slice(0, 500)) for (const k of Object.keys(e.input ?? {})) if (!keys.includes(k)) keys.push(k)
  for (const k of extraKeys) if (!keys.includes(k)) keys.push(k)
  if (keys.length === 0) keys.push('question')
  const cols: Col[] = keys.slice(0, 8).map((k) => ({ id: `in:${k}`, label: k, kind: 'input', key: k, width: 'minmax(220px, 2fr)' }))
  cols.push({ id: 'expected', label: 'expected', kind: 'expected', width: 'minmax(220px, 2fr)' })
  cols.push({ id: 'context', label: 'context', kind: 'context', width: 'minmax(160px, 1fr)' })
  cols.push({ id: 'tags', label: 'tags', kind: 'tags', width: 'minmax(160px, 1fr)' })
  return cols
}

export interface GridProps {
  rows: Example[]
  columns: Col[]
  readOnly: boolean
  loading?: boolean
  hasMore?: boolean
  loadingMore?: boolean
  onLoadMore?: () => void
  selected: Set<string>
  onSelectedChange: (s: Set<string>) => void
  onEdit: (ex: Example, col: Col, text: string) => void
  onAddColumn?: () => void
  /** Row to focus + edit after creation. */
  focusRowId?: string | null
  onFocusHandled?: () => void
  empty?: ReactNode
  height?: number | string
}

export function ExampleGrid({ rows, columns, readOnly, loading, hasMore, loadingMore, onLoadMore, selected, onSelectedChange, onEdit, onAddColumn, focusRowId, onFocusHandled, empty, height = 'calc(100dvh - 22rem)' }: GridProps) {
  const parentRef = useRef<HTMLDivElement>(null)
  const [active, setActive] = useState<{ r: number; c: number } | null>(null)
  const [editing, setEditing] = useState<{ r: number; c: number; text: string } | null>(null)
  const template = `40px 44px ${columns.map((c) => c.width).join(' ')}`
  const minWidth = 84 + columns.length * 180

  const virt = useVirtualizer({ count: rows.length, getScrollElement: () => parentRef.current, estimateSize: () => ROW_H, overscan: 14 })
  const items = virt.getVirtualItems()
  const lastIndex = items.at(-1)?.index ?? 0

  useEffect(() => {
    if (hasMore && !loadingMore && rows.length > 0 && lastIndex >= rows.length - 25) onLoadMore?.()
  }, [lastIndex, rows.length, hasMore, loadingMore, onLoadMore])

  // Jump to (and edit) a freshly added row.
  useEffect(() => {
    if (!focusRowId) return
    const r = rows.findIndex((x) => x.id === focusRowId)
    if (r < 0) return
    virt.scrollToIndex(r, { align: 'end' })
    setActive({ r, c: 0 })
    if (!readOnly) setEditing({ r, c: 0, text: cellText(rows[r]!, columns[0]!) })
    onFocusHandled?.()
  }, [focusRowId, rows]) // eslint-disable-line react-hooks/exhaustive-deps

  const move = useCallback(
    (dr: number, dc: number) => {
      setActive((a) => {
        const cur = a ?? { r: 0, c: 0 }
        const r = Math.max(0, Math.min(rows.length - 1, cur.r + dr))
        const c = Math.max(0, Math.min(columns.length - 1, cur.c + dc))
        virt.scrollToIndex(r, { align: 'auto' })
        return { r, c }
      })
    },
    [rows.length, columns.length, virt],
  )

  const commit = (advance: 'down' | 'right' | null) => {
    if (!editing) return
    const ex = rows[editing.r]
    const col = columns[editing.c]
    if (ex && col && editing.text !== cellText(ex, col)) onEdit(ex, col, editing.text)
    setEditing(null)
    parentRef.current?.focus()
    if (advance === 'down') move(1, 0)
    if (advance === 'right') move(0, 1)
  }

  const onKeyDown = (e: KeyboardEvent<HTMLDivElement>) => {
    if (editing || !active) {
      if (!editing && !active && ['ArrowDown', 'ArrowRight', 'Enter'].includes(e.key)) {
        setActive({ r: 0, c: 0 })
        e.preventDefault()
      }
      return
    }
    const ex = rows[active.r]
    const col = columns[active.c]
    switch (e.key) {
      case 'ArrowDown':
      case 'j':
        move(1, 0)
        break
      case 'ArrowUp':
      case 'k':
        move(-1, 0)
        break
      case 'ArrowLeft':
        move(0, -1)
        break
      case 'ArrowRight':
        move(0, 1)
        break
      case 'Tab':
        move(0, e.shiftKey ? -1 : 1)
        break
      case 'Enter':
      case 'F2':
        if (!readOnly && ex && col) setEditing({ ...active, text: cellText(ex, col) })
        break
      case 'x':
        if (ex) {
          const next = new Set(selected)
          if (next.has(ex.id)) next.delete(ex.id)
          else next.add(ex.id)
          onSelectedChange(next)
        }
        break
      case 'Delete':
      case 'Backspace':
        if (!readOnly && ex && col && cellText(ex, col)) onEdit(ex, col, '')
        break
      case 'Escape':
        setActive(null)
        break
      default:
        // Start typing to replace the cell content.
        if (!readOnly && ex && col && e.key.length === 1 && !e.metaKey && !e.ctrlKey && !e.altKey) {
          setEditing({ ...active, text: e.key })
          e.preventDefault()
          return
        }
        return
    }
    e.preventDefault()
  }

  const allSelected = rows.length > 0 && rows.every((r) => selected.has(r.id))
  const someSelected = !allSelected && rows.some((r) => selected.has(r.id))

  if (loading) {
    return (
      <div className="flex flex-col gap-1 p-3" aria-busy="true">
        {Array.from({ length: 10 }, (_, i) => (
          <Skeleton key={i} className="h-8 w-full" />
        ))}
      </div>
    )
  }
  if (rows.length === 0) return <>{empty}</>

  return (
    <div
      ref={parentRef}
      role="grid"
      aria-label="Examples"
      aria-rowcount={rows.length}
      tabIndex={0}
      onKeyDown={onKeyDown}
      onFocus={() => !active && rows.length > 0 && setActive({ r: 0, c: 0 })}
      className="relative overflow-auto rounded-b-md focus:outline-none"
      style={{ height, minHeight: 280 }}
    >
      <div style={{ minWidth }}>
        <div role="row" className="sticky top-0 z-20 grid items-center border-b border-border bg-surface-2 text-xs font-medium text-text-muted" style={{ gridTemplateColumns: template, height: ROW_H }}>
          <div className="flex justify-center">
            <Checkbox
              aria-label="Select all loaded rows"
              checked={allSelected ? true : someSelected ? 'indeterminate' : false}
              onCheckedChange={(v) => onSelectedChange(v === true ? new Set(rows.map((r) => r.id)) : new Set())}
            />
          </div>
          <div className="px-1 text-right tabular">#</div>
          {columns.map((c, i) => (
            <div key={c.id} role="columnheader" className="flex items-center justify-between gap-2 truncate px-3">
              <span className={cn('truncate', c.kind === 'input' && 'font-mono')}>{c.label}</span>
              {i === columns.findLastIndex((x) => x.kind === 'input') && onAddColumn && !readOnly && (
                <button type="button" aria-label="Add input column" onClick={onAddColumn} className="flex size-5 items-center justify-center rounded-sm text-text-muted hover:bg-surface-hover hover:text-text focus-ring">
                  <Plus className="size-3.5" />
                </button>
              )}
            </div>
          ))}
        </div>
        <div style={{ height: virt.getTotalSize(), position: 'relative' }}>
          {items.map((vi) => {
            const ex = rows[vi.index]
            if (!ex) return null
            const isSel = selected.has(ex.id)
            const isEditingRow = editing?.r === vi.index
            return (
              <div
                key={ex.id}
                role="row"
                aria-rowindex={vi.index + 1}
                aria-selected={isSel}
                className={cn('group absolute left-0 top-0 grid w-full items-stretch border-b border-border text-sm', isSel ? 'bg-accent-subtle' : 'bg-surface hover:bg-surface-2/60')}
                style={{ gridTemplateColumns: template, height: ROW_H, transform: `translateY(${vi.start}px)`, zIndex: isEditingRow ? 10 : undefined }}
              >
                <div className="flex items-center justify-center">
                  <Checkbox
                    aria-label={`Select row ${vi.index + 1}`}
                    checked={isSel}
                    onCheckedChange={(v) => {
                      const next = new Set(selected)
                      if (v === true) next.add(ex.id)
                      else next.delete(ex.id)
                      onSelectedChange(next)
                    }}
                  />
                </div>
                <div className="flex items-center justify-end px-1 text-xs tabular text-text-muted">{vi.index + 1}</div>
                {columns.map((col, ci) => {
                  const isActive = active?.r === vi.index && active.c === ci
                  const isEditing = editing?.r === vi.index && editing.c === ci
                  const text = cellText(ex, col)
                  return (
                    <div
                      key={col.id}
                      role="gridcell"
                      aria-selected={isActive}
                      onClick={() => {
                        setActive({ r: vi.index, c: ci })
                        parentRef.current?.focus()
                      }}
                      onDoubleClick={() => !readOnly && setEditing({ r: vi.index, c: ci, text })}
                      className={cn('relative flex min-w-0 items-center border-l border-border px-3', isActive && 'ring-2 ring-inset ring-accent')}
                    >
                      {isEditing ? (
                        <textarea
                          autoFocus
                          value={editing.text}
                          rows={Math.min(8, Math.max(2, editing.text.split('\n').length))}
                          onChange={(e) => setEditing({ ...editing, text: e.target.value })}
                          onKeyDown={(e) => {
                            if (e.key === 'Escape') {
                              e.stopPropagation()
                              setEditing(null)
                              parentRef.current?.focus()
                            } else if (e.key === 'Enter' && !e.shiftKey) {
                              e.preventDefault()
                              commit('down')
                            } else if (e.key === 'Tab') {
                              e.preventDefault()
                              commit(e.shiftKey ? null : 'right')
                            }
                            e.stopPropagation()
                          }}
                          onBlur={() => commit(null)}
                          onFocus={(e) => (e.currentTarget.value.length > 1 ? e.currentTarget.select() : e.currentTarget.setSelectionRange(1, 1))}
                          className={cn('absolute left-0 top-0 z-10 min-h-9 w-full resize-none rounded-sm border border-accent bg-surface px-3 py-1.5 text-sm text-text shadow-popover focus:outline-none', col.kind === 'input' && 'font-mono text-xs')}
                          aria-label={`Edit ${col.label}`}
                        />
                      ) : col.kind === 'tags' ? (
                        <span className="flex min-w-0 items-center gap-1 overflow-hidden">
                          {(ex.tags ?? []).map((t) => (
                            <span key={t} className="inline-flex h-5 shrink-0 items-center rounded-sm bg-surface-2 px-1.5 text-xs font-medium text-text-2 ring-1 ring-inset ring-border">
                              {t}
                            </span>
                          ))}
                        </span>
                      ) : col.kind === 'context' ? (
                        text ? <span className="truncate text-text-2">{(ex.context ?? []).length} passage{(ex.context ?? []).length === 1 ? '' : 's'}: {text.replace(/\n/g, ' · ')}</span> : null
                      ) : (
                        <span className={cn('truncate', col.kind === 'expected' ? 'text-text-2' : 'text-text', text.startsWith('{') && 'font-mono text-xs')}>{text}</span>
                      )}
                    </div>
                  )
                })}
              </div>
            )
          })}
        </div>
        {loadingMore && <div className="px-4 py-2 text-xs text-text-muted">Loading more…</div>}
      </div>
    </div>
  )
}

/** Stable memo helper so grids don't rebuild columns every render. */
export function useColumns(examples: Example[], schemaFields: string[] | undefined, extra: string[]) {
  return buildColumns(examples, schemaFields, extra)
}
