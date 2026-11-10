import { diffLines, diffWordsWithSpace } from 'diff'
import { useMemo, useState } from 'react'
import { cn } from '@/lib/cn'
import { SegmentedControl } from './segmented-control'

export interface DiffViewerProps {
  /** Left / "before" text (e.g. expected output or baseline). */
  oldValue: string
  /** Right / "after" text (e.g. actual output). */
  newValue: string
  oldLabel?: string
  newLabel?: string
  /** `words` for prose (LLM outputs), `lines` for code/JSON. */
  mode?: 'words' | 'lines'
  /** Initial view. */
  view?: 'unified' | 'split'
  /** Show the unified/split switch. */
  showViewToggle?: boolean
  maxHeight?: number | string
  className?: string
}

/** Text diff with added/removed highlighting. Additions/removals also carry +/− markers (not color alone). */
export function DiffViewer({
  oldValue,
  newValue,
  oldLabel = 'Expected',
  newLabel = 'Output',
  mode = 'words',
  view: initialView = 'unified',
  showViewToggle = true,
  maxHeight = 420,
  className,
}: DiffViewerProps) {
  const [view, setView] = useState(initialView)
  const parts = useMemo(
    () => (mode === 'words' ? diffWordsWithSpace(oldValue, newValue) : diffLines(oldValue, newValue)),
    [oldValue, newValue, mode],
  )
  const added = parts.filter((p) => p.added).reduce((n, p) => n + (p.count ?? 1), 0)
  const removed = parts.filter((p) => p.removed).reduce((n, p) => n + (p.count ?? 1), 0)

  const ins = 'rounded-[3px] bg-diff-add text-diff-add-text no-underline decoration-diff-add-text'
  const del = 'rounded-[3px] bg-diff-del text-diff-del-text line-through decoration-1'

  const renderUnified = () =>
    mode === 'words' ? (
      <p className="whitespace-pre-wrap break-words px-3.5 py-3 text-sm leading-6 text-text">
        {parts.map((p, i) =>
          p.added ? (
            <ins key={i} className={ins}>
              {p.value}
            </ins>
          ) : p.removed ? (
            <del key={i} className={del}>
              {p.value}
            </del>
          ) : (
            <span key={i}>{p.value}</span>
          ),
        )}
      </p>
    ) : (
      <div className="py-2 font-mono text-xs leading-5">
        {parts.flatMap((p, i) =>
          p.value
            .replace(/\n$/, '')
            .split('\n')
            .map((line, j) => (
              <div key={`${i}-${j}`} className={cn('flex px-2', p.added && 'bg-diff-add', p.removed && 'bg-diff-del')}>
                <span
                  aria-hidden
                  className={cn('w-5 shrink-0 select-none text-center', p.added ? 'text-diff-add-text' : p.removed ? 'text-diff-del-text' : 'text-text-muted')}
                >
                  {p.added ? '+' : p.removed ? '−' : ' '}
                </span>
                <span className={cn('whitespace-pre-wrap break-all', p.added ? 'text-diff-add-text' : p.removed ? 'text-diff-del-text' : 'text-text')}>
                  <span className="sr-only">{p.added ? 'Added: ' : p.removed ? 'Removed: ' : ''}</span>
                  {line || ' '}
                </span>
              </div>
            )),
        )}
      </div>
    )

  const renderSide = (side: 'old' | 'new') => (
    <div className={cn('whitespace-pre-wrap break-words px-3.5 py-3 leading-6 text-text', mode === 'lines' ? 'font-mono text-xs leading-5' : 'text-sm')}>
      {parts.map((p, i) => {
        if (side === 'old' && p.added) return null
        if (side === 'new' && p.removed) return null
        if (p.added) return <ins key={i} className={ins}>{p.value}</ins>
        if (p.removed) return <del key={i} className={del}>{p.value}</del>
        return <span key={i}>{p.value}</span>
      })}
    </div>
  )

  return (
    <div className={cn('overflow-hidden rounded-md border border-border bg-surface', className)}>
      <div className="flex h-10 items-center justify-between gap-3 border-b border-border bg-surface-2 pl-3.5 pr-1.5">
        <div className="flex items-center gap-3 text-xs">
          <span className="tabular font-medium text-diff-add-text">+{added}</span>
          <span className="tabular font-medium text-diff-del-text">−{removed}</span>
          <span className="text-text-muted">
            {oldLabel} → {newLabel}
          </span>
        </div>
        {showViewToggle && (
          <SegmentedControl
            size="sm"
            aria-label="Diff view"
            value={view}
            onValueChange={setView}
            options={[
              { value: 'unified', label: 'Unified' },
              { value: 'split', label: 'Split' },
            ]}
          />
        )}
      </div>
      <div className="overflow-auto" style={{ maxHeight }}>
        {view === 'unified' ? (
          renderUnified()
        ) : (
          <div className="grid grid-cols-2 divide-x divide-border">
            <div>
              <div className="border-b border-border px-3.5 py-1.5 text-xs font-medium text-text-muted">{oldLabel}</div>
              {renderSide('old')}
            </div>
            <div>
              <div className="border-b border-border px-3.5 py-1.5 text-xs font-medium text-text-muted">{newLabel}</div>
              {renderSide('new')}
            </div>
          </div>
        )}
      </div>
    </div>
  )
}
