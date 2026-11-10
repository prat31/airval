import { ChevronRight } from 'lucide-react'
import { useState, type ReactNode } from 'react'
import { cn } from '@/lib/cn'
import { CopyButton } from './code-block'

type Json = string | number | boolean | null | Json[] | { [k: string]: Json }

export interface JsonViewerProps {
  value: unknown
  /** Depth expanded by default. */
  defaultExpandDepth?: number
  /** Root label (e.g. "output"). */
  rootLabel?: string
  copyable?: boolean
  maxHeight?: number | string
  className?: string
}

function Primitive({ value }: { value: Json }) {
  if (value === null) return <span className="text-text-muted">null</span>
  if (typeof value === 'string')
    return <span className="break-all text-[var(--shiki-token-string)]">&quot;{value}&quot;</span>
  if (typeof value === 'number') return <span className="text-[var(--shiki-token-constant)]">{value}</span>
  if (typeof value === 'boolean') return <span className="text-[var(--shiki-token-keyword)]">{String(value)}</span>
  return null
}

function Node({ name, value, depth, expandDepth, last }: { name?: ReactNode; value: Json; depth: number; expandDepth: number; last: boolean }) {
  const isObj = value !== null && typeof value === 'object'
  const [open, setOpen] = useState(depth < expandDepth)
  const comma = last ? null : <span className="text-text-muted">,</span>
  const key = name !== undefined && (
    <>
      <span className="text-text-2">{name}</span>
      <span className="text-text-muted">: </span>
    </>
  )
  if (!isObj) {
    return (
      <div className="pl-4">
        {key}
        <Primitive value={value} />
        {comma}
      </div>
    )
  }
  const entries: [string, Json][] = Array.isArray(value) ? value.map((v, i) => [String(i), v]) : Object.entries(value)
  const [openB, closeB] = Array.isArray(value) ? ['[', ']'] : ['{', '}']
  if (entries.length === 0)
    return (
      <div className="pl-4">
        {key}
        <span className="text-text-muted">
          {openB}
          {closeB}
        </span>
        {comma}
      </div>
    )
  return (
    <div>
      <button
        type="button"
        onClick={() => setOpen((o) => !o)}
        aria-expanded={open}
        className="group flex items-center rounded-sm text-left hover:bg-surface-hover focus-ring"
      >
        <ChevronRight className={cn('size-3.5 shrink-0 text-text-muted transition-transform duration-150', open && 'rotate-90')} />
        <span className="pl-0.5">
          {key}
          <span className="text-text-muted">{openB}</span>
          {!open && (
            <>
              <span className="mx-1 rounded-sm bg-surface-hover px-1 text-[11px] text-text-muted">
                {entries.length} {Array.isArray(value) ? (entries.length === 1 ? 'item' : 'items') : entries.length === 1 ? 'key' : 'keys'}
              </span>
              <span className="text-text-muted">{closeB}</span>
              {comma}
            </>
          )}
        </span>
      </button>
      {open && (
        <>
          <div className="ml-[7px] border-l border-border pl-1.5">
            {entries.map(([k, v], i) => (
              <Node
                key={k}
                name={Array.isArray(value) ? undefined : `"${k}"`}
                value={v}
                depth={depth + 1}
                expandDepth={expandDepth}
                last={i === entries.length - 1}
              />
            ))}
          </div>
          <div className="pl-4">
            <span className="text-text-muted">{closeB}</span>
            {comma}
          </div>
        </>
      )}
    </div>
  )
}

/** Collapsible JSON tree for outputs, traces and raw request/response bodies. */
export function JsonViewer({ value, defaultExpandDepth = 2, rootLabel, copyable = true, maxHeight = 420, className }: JsonViewerProps) {
  const json = value as Json
  return (
    <div className={cn('group relative overflow-hidden rounded-md border border-border bg-surface-2', className)}>
      {copyable && (
        <CopyButton
          value={JSON.stringify(value, null, 2)}
          label="Copy JSON"
          className="absolute right-1.5 top-1.5 z-10 bg-surface-2 opacity-0 transition-opacity group-hover:opacity-100 focus-visible:opacity-100"
        />
      )}
      <div className="overflow-auto px-2 py-2.5 font-mono text-xs leading-5" style={{ maxHeight }} role="tree">
        <Node name={rootLabel} value={json} depth={0} expandDepth={defaultExpandDepth} last />
      </div>
    </div>
  )
}
