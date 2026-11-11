import { Link, useNavigate } from '@tanstack/react-router'
import { Crosshair, MoreHorizontal, Plus, Trash2 } from 'lucide-react'
import { useMemo, useState } from 'react'
import { Screen } from '@/app/screen'
import { Badge } from '@/components/ui/badge'
import { Button, IconButton } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from '@/components/ui/dropdown-menu'
import { EmptyState } from '@/components/ui/empty-state'
import { FilterBar } from '@/components/ui/filter-bar'
import { PageHeader } from '@/components/ui/page-header'
import { Table, type ColumnDef } from '@/components/ui/table'
import { toast } from '@/components/ui/toast'
import { errorMessage, type S } from '../shared/api'
import { useProjectCtx } from '../shared/context'
import { useSearchState, str } from '../shared/search-state'
import { ConfirmDialog, ErrorPanel, RelTime } from '../shared/ui'
import { useDeleteTarget, useTargets } from './api'
import { kindMeta, kindOrder, targetEndpoint } from './kinds'

export function KindBadge({ kind }: { kind: S['TargetKind'] }) {
  const m = kindMeta[kind]
  return (
    <span className="inline-flex items-center gap-1.5 text-text-2 [&_svg]:size-3.5 [&_svg]:text-text-muted">
      <m.icon />
      {m.label}
    </span>
  )
}

export function TargetsScreen() {
  const ctx = useProjectCtx()
  const targets = useTargets(ctx.projectId)
  const navigate = useNavigate()
  const del = useDeleteTarget(ctx.projectId)
  const [search, patch] = useSearchState()
  const q = str(search.q)
  const kindsKey = str(search.kind)
  const kinds = kindsKey ? kindsKey.split(",") : []
  const [toDelete, setToDelete] = useState<S['Target'] | null>(null)

  const rows = useMemo(
    () =>
      (targets.data ?? []).filter((t) => {
        if (kinds.length && !kinds.includes(t.kind)) return false
        const hay = `${t.name} ${t.description} ${targetEndpoint(t.kind, t.current_version?.config as never)}`.toLowerCase()
        return !q || hay.includes(q.toLowerCase())
      }),
    [targets.data, q, kindsKey],
  )

  const columns: ColumnDef<S['Target'], unknown>[] = [
      {
        accessorKey: 'name',
        header: 'Target',
        size: 280,
        meta: { grow: true },
        cell: (c) => (
          <span className="flex min-w-0 flex-col">
            <span className="truncate font-medium text-text">{c.row.original.name}</span>
            <span className="truncate font-mono text-[11px] text-text-muted">{targetEndpoint(c.row.original.kind, c.row.original.current_version?.config as never) || c.row.original.description || '—'}</span>
          </span>
        ),
      },
      { id: 'kind', accessorFn: (t) => t.kind, header: 'Kind', size: 190, cell: (c) => <KindBadge kind={c.row.original.kind} /> },
      {
        id: 'version',
        header: 'Version',
        size: 90,
        enableSorting: false,
        cell: (c) => (c.row.original.current_version ? <Badge mono>v{c.row.original.current_version.number}</Badge> : <span className="text-text-muted">—</span>),
      },
      { accessorKey: 'updated_at', header: 'Updated', size: 130, cell: (c) => <RelTime iso={c.row.original.updated_at} /> },
      {
        id: 'actions',
        header: '',
        size: 48,
        enableSorting: false,
        cell: (c) => (
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <IconButton label={`Actions for ${c.row.original.name}`} size="sm" onClick={(e) => e.stopPropagation()}>
                <MoreHorizontal />
              </IconButton>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end">
              <DropdownMenuItem destructive icon={<Trash2 />} onSelect={() => setToDelete(c.row.original)}>
                Delete…
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        ),
      },
  ]

  const newButton = (
    <Button variant="primary" icon={<Plus />} asChild>
      <Link to="/o/$org/p/$project/targets/$target" params={{ ...ctx.params, target: 'new' }}>
        New target
      </Link>
    </Button>
  )

  return (
    <Screen>
      <PageHeader icon={<Crosshair />} title="Targets" description="Connections to the AI service under evaluation. Compare prod vs staging or model A vs B in one project." actions={newButton} />
      {targets.error ? (
        <Card>
          <ErrorPanel error={targets.error} onRetry={() => void targets.refetch()} />
        </Card>
      ) : !targets.isLoading && (targets.data?.length ?? 0) === 0 ? (
        <Card>
          <EmptyState
            illustration="targets"
            title="Connect a target"
            description="Point airval at an HTTP endpoint or an LLM provider. Test the connection live before you save."
            action={newButton}
          />
        </Card>
      ) : (
        <>
          <FilterBar
            search={q}
            onSearchChange={(v) => patch({ q: v })}
            searchPlaceholder="Search targets…"
            filters={[
              {
                id: 'kind',
                label: 'Kind',
                options: kindOrder.map((k) => ({ value: k, label: kindMeta[k].label, count: (targets.data ?? []).filter((t) => t.kind === k).length })),
                value: kinds,
                onChange: (v) => patch({ kind: v.join(',') }),
              },
            ]}
          />
          <Card>
            <Table
              aria-label="Targets"
              data={rows}
              columns={columns}
              getRowId={(t) => t.id}
              loading={targets.isLoading}
              onRowActivate={(t) => void navigate({ to: '/o/$org/p/$project/targets/$target', params: { ...ctx.params, target: t.id } })}
              empty={<EmptyState compact illustration="search" title="No targets match" description="Try a different search or clear the filters." />}
            />
          </Card>
        </>
      )}
      <ConfirmDialog
        open={!!toDelete}
        onOpenChange={(o) => !o && setToDelete(null)}
        title={`Delete ${toDelete?.name ?? 'target'}?`}
        description="Suites that use this target will stop running until you pick another. Past runs keep their results."
        loading={del.isPending}
        onConfirm={() =>
          toDelete &&
          del.mutate(toDelete.id, {
            onSuccess: () => {
              toast.success('Target deleted')
              setToDelete(null)
            },
            onError: (e) => toast.error(errorMessage(e)),
          })
        }
      />
    </Screen>
  )
}
