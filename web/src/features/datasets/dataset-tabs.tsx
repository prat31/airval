import { Download, Eye } from 'lucide-react'
import { useMemo, useState } from 'react'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardBody, CardHeader } from '@/components/ui/card'
import { DiffViewer } from '@/components/ui/diff-viewer'
import { EmptyState } from '@/components/ui/empty-state'
import { Field } from '@/components/ui/field'
import { Select } from '@/components/ui/select'
import { Skeleton } from '@/components/ui/skeleton'
import { StatTile } from '@/components/ui/stat-tile'
import { toast } from '@/components/ui/toast'
import { cn } from '@/lib/cn'
import { formatPercent } from '@/lib/format'
import { errorMessage, type S } from '../shared/api'
import { stableStringify } from '../shared/obj'
import { ErrorPanel, RelTime } from '../shared/ui'
import { downloadExport, useAllExamples, useDatasetVersions } from './api'

type Example = S['Example']

/* ------------------------------------------------------------------------------------------------ Versions + diff */

const DRAFT = 'draft'

function exampleView(e: Example) {
  return { input: e.input, expected: e.expected, context: e.context, metadata: e.metadata, tags: e.tags }
}

export function VersionsTab({ dataset, onView }: { dataset: S['Dataset']; onView: (v: number | null) => void }) {
  const versions = useDatasetVersions(dataset.id)
  const list = versions.data ?? []
  const [from, setFrom] = useState<string | null>(null)
  const [to, setTo] = useState<string>(DRAFT)
  const fromKey = from ?? (list[0] ? String(list[0].number) : null)
  const num = (k: string | null) => (k && k !== DRAFT ? Number(k) : undefined)

  const a = useAllExamples(dataset.id, num(fromKey), !!fromKey)
  const b = useAllExamples(dataset.id, num(to), true)

  const diff = useMemo(() => {
    if (!a.data || !b.data) return null
    const am = new Map(a.data.map((e) => [e.id, e]))
    const bm = new Map(b.data.map((e) => [e.id, e]))
    const added = b.data.filter((e) => !am.has(e.id))
    const removed = a.data.filter((e) => !bm.has(e.id))
    const changed = b.data.filter((e) => am.has(e.id) && am.get(e.id)!.revision_id !== e.revision_id).map((e) => ({ before: am.get(e.id)!, after: e }))
    return { added, removed, changed }
  }, [a.data, b.data])

  const exportVersion = async (v: S['DatasetVersion'], fmt: 'jsonl' | 'csv') => {
    try {
      await downloadExport(dataset.id, v.number, fmt, dataset.name.replace(/\s+/g, '-').toLowerCase())
    } catch (e) {
      toast.error(errorMessage(e))
    }
  }

  if (versions.error) return <Card><ErrorPanel error={versions.error} onRetry={() => void versions.refetch()} /></Card>
  if (versions.isLoading) return <Card className="p-4"><Skeleton className="h-40 w-full" /></Card>
  if (list.length === 0)
    return (
      <Card>
        <EmptyState illustration="datasets" title="No published versions" description="Publish the draft to freeze it as an immutable version. Suites can pin a version for reproducible runs." />
      </Card>
    )

  const options = [{ value: DRAFT, label: 'Draft (current edits)' }, ...list.map((v) => ({ value: String(v.number), label: `v${v.number}${v.note ? ` · ${v.note}` : ''}` }))]
  return (
    <div className="flex flex-col gap-3">
      <Card>
        <CardHeader title="Published versions" description="Immutable snapshots. Unchanged examples are shared between versions." />
        <ul className="divide-y divide-border">
          {list.map((v, i) => (
            <li key={v.id} className="flex flex-wrap items-center gap-x-4 gap-y-1 px-4 py-2.5">
              <Badge mono variant={i === 0 ? 'accent' : 'neutral'}>v{v.number}</Badge>
              <span className="min-w-0 flex-1 truncate text-sm text-text">{v.note || <span className="text-text-muted">No note</span>}</span>
              <span className="text-xs tabular text-text-2">{v.example_count.toLocaleString()} examples</span>
              <RelTime iso={v.created_at} className="w-24 text-right text-xs" />
              <span className="flex items-center gap-1">
                <Button size="sm" variant="ghost" icon={<Eye />} onClick={() => onView(v.number)}>
                  View
                </Button>
                <Button size="sm" variant="ghost" icon={<Download />} onClick={() => void exportVersion(v, 'jsonl')}>
                  JSONL
                </Button>
                <Button size="sm" variant="ghost" icon={<Download />} onClick={() => void exportVersion(v, 'csv')}>
                  CSV
                </Button>
              </span>
            </li>
          ))}
        </ul>
      </Card>

      <Card>
        <CardHeader title="Compare versions" description="What changed between two snapshots." />
        <CardBody className="flex flex-col gap-4">
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <Field label="From">
              <Select value={fromKey ?? ''} onValueChange={setFrom} options={options.filter((o) => o.value !== DRAFT)} />
            </Field>
            <Field label="To">
              <Select value={to} onValueChange={setTo} options={options} />
            </Field>
          </div>
          {a.isLoading || b.isLoading || !diff ? (
            <Skeleton className="h-32 w-full" />
          ) : (
            <>
              <div className="grid grid-cols-3 gap-3">
                <StatTile label="Added" value={`+${diff.added.length}`} />
                <StatTile label="Changed" value={String(diff.changed.length)} />
                <StatTile label="Removed" value={`−${diff.removed.length}`} />
              </div>
              {diff.added.length + diff.removed.length + diff.changed.length === 0 ? (
                <p className="text-sm text-text-muted">These two snapshots are identical.</p>
              ) : (
                <div className="flex flex-col gap-3">
                  {diff.changed.slice(0, 20).map(({ before, after }) => (
                    <div key={after.id} className="rounded-md border border-border">
                      <div className="border-b border-border bg-surface-2 px-3 py-1.5 text-xs font-medium text-text-muted">Changed example</div>
                      <DiffViewer bare oldValue={stableStringify(exampleView(before))} newValue={stableStringify(exampleView(after))} mode="lines" maxHeight={260} />
                    </div>
                  ))}
                  {diff.added.slice(0, 20).map((e) => (
                    <div key={e.id} className="rounded-md border border-border">
                      <div className="border-b border-border bg-pass-subtle px-3 py-1.5 text-xs font-medium text-pass">Added</div>
                      <pre className="overflow-x-auto whitespace-pre-wrap p-3 font-mono text-xs text-text-2">{stableStringify(exampleView(e))}</pre>
                    </div>
                  ))}
                  {diff.removed.slice(0, 20).map((e) => (
                    <div key={e.id} className="rounded-md border border-border">
                      <div className="border-b border-border bg-fail-subtle px-3 py-1.5 text-xs font-medium text-fail">Removed</div>
                      <pre className="overflow-x-auto whitespace-pre-wrap p-3 font-mono text-xs text-text-2">{stableStringify(exampleView(e))}</pre>
                    </div>
                  ))}
                  {diff.added.length > 20 || diff.removed.length > 20 || diff.changed.length > 20 ? <p className="text-xs text-text-muted">Showing the first 20 of each kind.</p> : null}
                </div>
              )}
            </>
          )}
        </CardBody>
      </Card>
    </div>
  )
}

/* ------------------------------------------------------------------------------------------------ Column stats */

interface ColStat {
  name: string
  filled: number
  unique: number
  avgLen: number
  maxLen: number
}

const asText = (v: unknown): string => (v === undefined || v === null ? '' : typeof v === 'string' ? v : JSON.stringify(v))

export function computeStats(examples: Example[]): { cols: ColStat[]; tags: [string, number][] } {
  const acc = new Map<string, { filled: number; uniq: Set<string>; total: number; max: number }>()
  const touch = (name: string, text: string) => {
    const a = acc.get(name) ?? { filled: 0, uniq: new Set<string>(), total: 0, max: 0 }
    if (text !== '') {
      a.filled++
      a.total += text.length
      a.max = Math.max(a.max, text.length)
      if (a.uniq.size < 5000) a.uniq.add(text)
    }
    acc.set(name, a)
  }
  const tagCounts = new Map<string, number>()
  for (const e of examples) {
    for (const [k, v] of Object.entries(e.input ?? {})) touch(`input.${k}`, asText(v))
    touch('expected', asText(e.expected))
    touch('context', (e.context ?? []).join('\n'))
    for (const [k, v] of Object.entries(e.metadata ?? {})) touch(`metadata.${k}`, asText(v))
    for (const t of e.tags ?? []) tagCounts.set(t, (tagCounts.get(t) ?? 0) + 1)
  }
  const cols = [...acc.entries()].map(([name, a]) => ({ name, filled: a.filled, unique: a.uniq.size, avgLen: a.filled ? Math.round(a.total / a.filled) : 0, maxLen: a.max }))
  return { cols, tags: [...tagCounts.entries()].sort((x, y) => y[1] - x[1]) }
}

export function StatsTab({ dataset, version }: { dataset: S['Dataset']; version?: number }) {
  const all = useAllExamples(dataset.id, version)
  const stats = useMemo(() => (all.data ? computeStats(all.data) : null), [all.data])
  if (all.error) return <Card><ErrorPanel error={all.error} onRetry={() => void all.refetch()} /></Card>
  if (all.isLoading || !stats) return <Card className="p-4"><Skeleton className="h-48 w-full" /></Card>
  const total = all.data!.length
  if (total === 0) return <Card><EmptyState illustration="chart" title="No examples to analyze" description="Add or import examples to see column statistics and the tag cloud." /></Card>
  const maxTag = stats.tags[0]?.[1] ?? 1
  return (
    <div className="grid grid-cols-1 gap-3 lg:grid-cols-[minmax(0,2fr)_minmax(0,1fr)]">
      <Card className="min-w-0">
        <CardHeader title="Columns" description={`${total.toLocaleString()} examples`} />
        <div className="overflow-x-auto px-2 pb-2">
          <table className="w-full min-w-[480px] text-sm">
            <thead>
              <tr className="text-left text-xs text-text-muted">
                <th className="px-2 py-2 font-medium">Column</th>
                <th className="px-2 py-2 font-medium">Filled</th>
                <th className="px-2 py-2 text-right font-medium">Unique</th>
                <th className="px-2 py-2 text-right font-medium">Avg length</th>
                <th className="px-2 py-2 text-right font-medium">Max</th>
              </tr>
            </thead>
            <tbody>
              {stats.cols.map((c) => (
                <tr key={c.name} className="border-t border-border">
                  <td className="px-2 py-2 font-mono text-xs text-text">{c.name}</td>
                  <td className="px-2 py-2">
                    <span className="flex items-center gap-2">
                      <span className="h-1.5 w-24 overflow-hidden rounded-full bg-surface-2" aria-hidden>
                        <span className={cn('block h-full rounded-full', c.filled / total < 0.9 ? 'bg-warn' : 'bg-accent')} style={{ width: `${(c.filled / total) * 100}%` }} />
                      </span>
                      <span className="tabular text-xs text-text-2">{formatPercent(c.filled / total, 0)}</span>
                    </span>
                  </td>
                  <td className="px-2 py-2 text-right tabular text-text-2">{c.unique.toLocaleString()}</td>
                  <td className="px-2 py-2 text-right tabular text-text-2">{c.avgLen.toLocaleString()}</td>
                  <td className="px-2 py-2 text-right tabular text-text-2">{c.maxLen.toLocaleString()}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </Card>
      <Card className="self-start">
        <CardHeader title="Tags" description={`${stats.tags.length} distinct`} />
        <CardBody>
          {stats.tags.length === 0 ? (
            <p className="text-sm text-text-muted">No tags yet. Tag examples to slice results by topic or difficulty.</p>
          ) : (
            <div className="flex flex-wrap items-baseline gap-x-3 gap-y-1.5">
              {stats.tags.slice(0, 60).map(([t, n]) => (
                <span key={t} className="inline-flex items-baseline gap-1 text-text-2" style={{ fontSize: `${12 + Math.round((n / maxTag) * 6)}px` }}>
                  <span className="font-medium text-text">{t}</span>
                  <span className="text-xs tabular text-text-muted">{n}</span>
                </span>
              ))}
            </div>
          )}
        </CardBody>
      </Card>
    </div>
  )
}
