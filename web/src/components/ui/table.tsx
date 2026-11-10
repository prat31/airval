import {
  flexRender,
  getCoreRowModel,
  getSortedRowModel,
  useReactTable,
  type ColumnDef,
  type OnChangeFn,
  type Row,
  type RowData,
  type RowSelectionState,
  type SortingState,
} from '@tanstack/react-table'
import { useVirtualizer } from '@tanstack/react-virtual'
import { ArrowDown, ArrowUp, ChevronsUpDown } from 'lucide-react'
import { useCallback, useEffect, useRef, useState, type CSSProperties, type KeyboardEvent, type ReactNode } from 'react'
import { cn } from '@/lib/cn'
import { Checkbox } from './checkbox'
import { Skeleton } from './skeleton'

declare module '@tanstack/react-table' {
  // eslint-disable-next-line @typescript-eslint/no-unused-vars
  interface ColumnMeta<TData extends RowData, TValue> {
    align?: 'left' | 'right' | 'center'
    /** Column absorbs remaining width. */
    grow?: boolean
    /** Use tabular numerals (numbers, durations, costs). */
    numeric?: boolean
    headerClassName?: string
    cellClassName?: string
  }
}

export type { ColumnDef, RowSelectionState, SortingState }

export interface TableProps<T> {
  data: T[]
  columns: ColumnDef<T, unknown>[]
  getRowId?: (row: T, index: number) => string
  /** Max height of the scroll area; the header sticks inside it. Defaults to auto (page scroll). */
  maxHeight?: number | string
  /** Row height in px (rows are fixed-height for smooth virtualization). */
  rowHeight?: number
  /** Virtualize when there are more rows than this. */
  virtualizeThreshold?: number

  sorting?: SortingState
  onSortingChange?: OnChangeFn<SortingState>
  /** Sort client-side (default true). Set false when the server sorts. */
  clientSort?: boolean

  enableRowSelection?: boolean
  rowSelection?: RowSelectionState
  onRowSelectionChange?: OnChangeFn<RowSelectionState>

  /** Called on click or Enter on the active row. */
  onRowActivate?: (row: T) => void
  /** Highlighted row (e.g. the one open in a drawer). */
  activeRowId?: string

  loading?: boolean
  /** Rendered when there are no rows and not loading. */
  empty?: ReactNode
  className?: string
  'aria-label'?: string
}

/** Checkbox column for row selection. Put it first in `columns`. */
export function selectionColumn<T>(): ColumnDef<T, unknown> {
  return {
    id: '__select',
    size: 40,
    enableSorting: false,
    header: ({ table }) => (
      <Checkbox
        aria-label="Select all rows"
        checked={table.getIsAllRowsSelected() ? true : table.getIsSomeRowsSelected() ? 'indeterminate' : false}
        onCheckedChange={(v) => table.toggleAllRowsSelected(v === true)}
      />
    ),
    cell: ({ row }) => (
      <Checkbox
        aria-label="Select row"
        checked={row.getIsSelected()}
        onCheckedChange={(v) => row.toggleSelected(v === true)}
        onClick={(e) => e.stopPropagation()}
      />
    ),
  }
}

/**
 * Data table: TanStack Table + Virtual. Sortable headers, sticky header, row selection,
 * keyboard navigation (j/k or ↑/↓ to move, Enter to open, x to select).
 */
export function Table<T>({
  data,
  columns,
  getRowId,
  maxHeight,
  rowHeight = 44,
  virtualizeThreshold = 80,
  sorting: sortingProp,
  onSortingChange,
  clientSort = true,
  enableRowSelection,
  rowSelection: rowSelectionProp,
  onRowSelectionChange,
  onRowActivate,
  activeRowId,
  loading,
  empty,
  className,
  ...props
}: TableProps<T>) {
  const [sortingState, setSortingState] = useState<SortingState>([])
  const [selectionState, setSelectionState] = useState<RowSelectionState>({})
  const sorting = sortingProp ?? sortingState
  const rowSelection = rowSelectionProp ?? selectionState

  // eslint-disable-next-line react-hooks/incompatible-library -- TanStack Table returns non-memoizable functions by design.
  const table = useReactTable({
    data,
    columns,
    getRowId,
    state: { sorting, rowSelection },
    onSortingChange: onSortingChange ?? setSortingState,
    onRowSelectionChange: onRowSelectionChange ?? setSelectionState,
    enableRowSelection: !!enableRowSelection,
    manualSorting: !clientSort,
    getCoreRowModel: getCoreRowModel(),
    getSortedRowModel: clientSort ? getSortedRowModel() : undefined,
  })

  const rows = table.getRowModel().rows
  const scrollRef = useRef<HTMLDivElement>(null)
  const virtual = rows.length > virtualizeThreshold
  const [cursor, setCursor] = useState(-1)

  const virtualizer = useVirtualizer({
    count: virtual ? rows.length : 0,
    getScrollElement: () => scrollRef.current,
    estimateSize: () => rowHeight,
    overscan: 12,
  })

  const scrollToRow = useCallback(
    (index: number) => {
      if (virtual) virtualizer.scrollToIndex(index, { align: 'auto' })
      else scrollRef.current?.querySelector<HTMLElement>(`[data-index="${index}"]`)?.scrollIntoView({ block: 'nearest' })
    },
    [virtual, virtualizer],
  )

  useEffect(() => {
    if (cursor >= rows.length) setCursor(rows.length - 1)
  }, [rows.length, cursor])

  const onKeyDown = (e: KeyboardEvent<HTMLDivElement>) => {
    if (e.target !== e.currentTarget) return
    const move = (delta: number) => {
      e.preventDefault()
      const next = Math.max(0, Math.min(rows.length - 1, (cursor < 0 ? (delta > 0 ? -1 : rows.length) : cursor) + delta))
      setCursor(next)
      scrollToRow(next)
    }
    if (e.key === 'j' || e.key === 'ArrowDown') move(1)
    else if (e.key === 'k' || e.key === 'ArrowUp') move(-1)
    else if (e.key === 'Enter' && cursor >= 0 && rows[cursor]) {
      e.preventDefault()
      onRowActivate?.(rows[cursor].original)
    } else if (e.key === 'x' && enableRowSelection && cursor >= 0 && rows[cursor]) {
      e.preventDefault()
      rows[cursor].toggleSelected()
    }
  }

  const headerGroups = table.getHeaderGroups()
  const colStyle = (size: number, grow?: boolean) => (grow ? { flex: `1 0 ${size}px`, minWidth: size } : { flex: `0 0 ${size}px`, width: size })
  const totalMin = table.getVisibleLeafColumns().reduce((s, c) => s + c.getSize(), 0)

  const renderRow = (row: Row<T>, index: number, style?: CSSProperties) => {
    const selected = row.getIsSelected()
    const active = activeRowId ? row.id === activeRowId : false
    const focused = index === cursor
    return (
      <tr
        key={row.id}
        data-index={index}
        aria-selected={enableRowSelection ? selected : undefined}
        onClick={() => {
          setCursor(index)
          onRowActivate?.(row.original)
        }}
        className={cn(
          'group/row flex w-full items-stretch border-b border-border transition-colors duration-100',
          onRowActivate && 'cursor-pointer',
          selected || active ? 'bg-accent-subtle' : 'hover:bg-surface-hover',
          focused && 'shadow-[inset_2px_0_0_var(--accent)]',
          focused && !selected && !active && 'bg-surface-hover',
        )}
        style={{ height: rowHeight, ...style }}
      >
        {row.getVisibleCells().map((cell) => {
          const meta = cell.column.columnDef.meta
          return (
            <td
              key={cell.id}
              className={cn(
                'flex min-w-0 items-center px-3 text-sm text-text first:pl-4 last:pr-4',
                meta?.align === 'right' && 'justify-end text-right',
                meta?.align === 'center' && 'justify-center',
                meta?.numeric && 'tabular',
                meta?.cellClassName,
              )}
              style={colStyle(cell.column.getSize(), meta?.grow)}
            >
              <div className="min-w-0 truncate">{flexRender(cell.column.columnDef.cell, cell.getContext())}</div>
            </td>
          )
        })}
      </tr>
    )
  }

  const virtualItems = virtual ? virtualizer.getVirtualItems() : []

  return (
    <div
      ref={scrollRef}
      tabIndex={0}
      role="region"
      aria-label={props['aria-label'] ?? 'Table'}
      onKeyDown={onKeyDown}
      className={cn(
        'relative overflow-auto rounded-md border border-border bg-surface focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent',
        className,
      )}
      style={{ maxHeight }}
    >
      <table className="grid w-full" style={{ minWidth: totalMin }} aria-rowcount={rows.length}>
        <thead className="sticky top-0 z-10 grid bg-surface-2">
          {headerGroups.map((hg) => (
            <tr key={hg.id} className="flex w-full border-b border-border">
              {hg.headers.map((header) => {
                const meta = header.column.columnDef.meta
                const sortable = header.column.getCanSort()
                const sorted = header.column.getIsSorted()
                const content = header.isPlaceholder ? null : flexRender(header.column.columnDef.header, header.getContext())
                return (
                  <th
                    key={header.id}
                    scope="col"
                    aria-sort={sorted === 'asc' ? 'ascending' : sorted === 'desc' ? 'descending' : undefined}
                    className={cn(
                      'flex h-9 min-w-0 items-center px-3 text-left text-xs font-medium text-text-muted first:pl-4 last:pr-4',
                      meta?.align === 'right' && 'justify-end',
                      meta?.align === 'center' && 'justify-center',
                      meta?.headerClassName,
                    )}
                    style={colStyle(header.getSize(), meta?.grow)}
                  >
                    {sortable ? (
                      <button
                        type="button"
                        onClick={header.column.getToggleSortingHandler()}
                        className={cn(
                          'group/sort -mx-1 inline-flex min-w-0 items-center gap-1 rounded-sm px-1 py-0.5 hover:text-text focus-ring',
                          sorted && 'text-text',
                          meta?.align === 'right' && 'flex-row-reverse',
                        )}
                      >
                        <span className="truncate">{content}</span>
                        {sorted === 'asc' ? (
                          <ArrowUp className="size-3 shrink-0" />
                        ) : sorted === 'desc' ? (
                          <ArrowDown className="size-3 shrink-0" />
                        ) : (
                          <ChevronsUpDown className="size-3 shrink-0 opacity-0 transition-opacity group-hover/sort:opacity-60" />
                        )}
                      </button>
                    ) : (
                      content
                    )}
                  </th>
                )
              })}
            </tr>
          ))}
        </thead>
        <tbody
          className="relative grid [&>tr:last-child]:border-b-0"
          style={virtual ? { height: virtualizer.getTotalSize() } : undefined}
        >
          {loading && rows.length === 0
            ? Array.from({ length: 6 }, (_, r) => (
                <tr key={r} className="flex w-full border-b border-border" style={{ height: rowHeight }}>
                  {table.getVisibleLeafColumns().map((c, i) => (
                    <td key={c.id} className="flex items-center px-3 first:pl-4" style={colStyle(c.getSize(), c.columnDef.meta?.grow)}>
                      <Skeleton className={cn('h-3', i === 0 ? 'w-4/5' : 'w-3/5')} />
                    </td>
                  ))}
                </tr>
              ))
            : rows.length === 0
              ? (
                  <tr className="flex w-full">
                    <td className="w-full">{empty ?? <p className="py-10 text-center text-sm text-text-muted">No rows</p>}</td>
                  </tr>
                )
              : virtual
                ? virtualItems.map((vi) =>
                    renderRow(rows[vi.index]!, vi.index, {
                      position: 'absolute',
                      top: 0,
                      left: 0,
                      transform: `translateY(${vi.start}px)`,
                    }),
                  )
                : rows.map((row, i) => renderRow(row, i))}
        </tbody>
      </table>
    </div>
  )
}
