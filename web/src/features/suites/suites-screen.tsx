import { Link, useNavigate } from '@tanstack/react-router'
import { Layers, MoreHorizontal, Plus, Trash2 } from 'lucide-react'
import { useMemo, useState } from 'react'
import { Screen } from '@/app/screen'
import { Badge } from '@/components/ui/badge'
import { Button, IconButton } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from '@/components/ui/dropdown-menu'
import { EmptyState } from '@/components/ui/empty-state'
import { FilterBar } from '@/components/ui/filter-bar'
import { PageHeader } from '@/components/ui/page-header'
import { StatusPill, type Status } from '@/components/ui/status-pill'
import { Table, type ColumnDef } from '@/components/ui/table'
import { toast } from '@/components/ui/toast'
import { formatPercent } from '@/lib/format'
import { errorMessage, type Obj, type S } from '../shared/api'
import { useProjectCtx } from '../shared/context'
import { str, useSearchState } from '../shared/search-state'
import { ConfirmDialog, ErrorPanel, RelTime } from '../shared/ui'
import { useDeleteSuite, useSuites } from './api'

export const runStatus = (s: S['RunStatus']): Status => ({ queued: 'queued', running: 'running', succeeded: 'passed', failed: 'failed', errored: 'errored', canceled: 'cancelled' } as const)[s]

export function SuitesScreen() {
  const ctx = useProjectCtx()
  const suites = useSuites(ctx.projectId)
  const del = useDeleteSuite(ctx.projectId)
  const navigate = useNavigate()
  const [search, patch] = useSearchState()
  const q = str(search.q)
  const [toDelete, setToDelete] = useState<S['Suite'] | null>(null)
  const rows = useMemo(() => (suites.data ?? []).filter((s) => !q || `${s.name} ${s.description}`.toLowerCase().includes(q.toLowerCase())), [suites.data, q])

  const columns = useMemo<ColumnDef<S['Suite'], unknown>[]>(
    () => [
      {
        accessorKey: 'name',
        header: 'Suite',
        size: 280,
        meta: { grow: true },
        cell: (c) => (
          <span className="flex min-w-0 flex-col">
            <span className="truncate font-medium text-text">{c.row.original.name}</span>
            <span className="truncate font-mono text-[11px] text-text-muted">{c.row.original.slug}</span>
          </span>
        ),
      },
      {
        id: 'config',
        header: 'Setup',
        size: 220,
        enableSorting: false,
        cell: (c) => {
          const cfg = c.row.original.config as Obj
          const n = (k: string) => (Array.isArray(cfg[k]) ? (cfg[k] as unknown[]).length : 0)
          return (
            <span className="flex items-center gap-1.5 text-xs text-text-2">
              <Badge>{n('target_ids')} target{n('target_ids') === 1 ? '' : 's'}</Badge>
              <Badge>{n('datasets')} dataset{n('datasets') === 1 ? '' : 's'}</Badge>
              <Badge>{n('evaluators')} evaluators</Badge>
            </span>
          )
        },
      },
      {
        id: 'last',
        header: 'Last run',
        size: 190,
        enableSorting: false,
        cell: (c) => {
          const r = c.row.original.last_run
          if (!r) return <span className="text-text-muted">Never run</span>
          const rate = r.total ? (r.total - r.failed - r.errored) / r.total : undefined
          return (
            <span className="flex items-center gap-2">
              <StatusPill status={runStatus(r.status)} size="sm" />
              {rate !== undefined && r.completed > 0 && <span className="tabular text-xs text-text-2">{formatPercent(rate, 0)}</span>}
            </span>
          )
        },
      },
      { accessorKey: 'updated_at', header: 'Updated', size: 120, cell: (c) => <RelTime iso={c.row.original.updated_at} /> },
      {
        id: 'actions',
        header: '',
        size: 48,
        enableSorting: false,
        cell: (c) => (
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <IconButton label={`Actions for ${c.row.original.name}`} size="sm" onClick={(e) => e.stopPropagation()}><MoreHorizontal /></IconButton>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end">
              <DropdownMenuItem destructive icon={<Trash2 />} onSelect={() => setToDelete(c.row.original)}>Delete…</DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        ),
      },
    ],
    [],
  )

  const newSuite = (
    <Button variant="primary" icon={<Plus />} asChild>
      <Link to="/o/$org/p/$project/suites/new" params={ctx.params}>New suite</Link>
    </Button>
  )
  return (
    <Screen>
      <PageHeader icon={<Layers />} title="Suites" description="Reusable evaluation definitions: data, evaluators, gates and triggers." actions={newSuite} />
      {suites.error ? (
        <Card><ErrorPanel error={suites.error} onRetry={() => void suites.refetch()} /></Card>
      ) : !suites.isPending && (suites.data?.length ?? 0) === 0 ? (
        <Card><EmptyState illustration="suites" title="Create your first suite" description="A suite pairs a dataset version with evaluators and gates. Run it manually, on a schedule or from CI." action={newSuite} /></Card>
      ) : (
        <>
          <FilterBar search={q} onSearchChange={(v) => patch({ q: v })} searchPlaceholder="Search suites…" />
          <Card>
            <Table
              aria-label="Suites"
              data={rows}
              columns={columns}
              getRowId={(s) => s.id}
              loading={suites.isPending}
              onRowActivate={(s) => void navigate({ to: '/o/$org/p/$project/suites/$suite', params: { ...ctx.params, suite: s.slug } })}
              empty={<EmptyState compact illustration="search" title="No suites match" />}
            />
          </Card>
        </>
      )}
      <ConfirmDialog
        open={!!toDelete}
        onOpenChange={(o) => !o && setToDelete(null)}
        title={`Delete ${toDelete?.name ?? 'suite'}?`}
        description="Its gates and triggers are removed. Past runs keep their results."
        loading={del.isPending}
        onConfirm={() => toDelete && del.mutate(toDelete.id, { onSuccess: () => { toast.success('Suite deleted'); setToDelete(null) }, onError: (e) => toast.error(errorMessage(e)) })}
      />
    </Screen>
  )
}
