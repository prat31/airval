import { useNavigate } from '@tanstack/react-router'
import { Database, MoreHorizontal, Plus, Trash2, Upload } from 'lucide-react'
import { useMemo, useState } from 'react'
import { Screen } from '@/app/screen'
import { Badge } from '@/components/ui/badge'
import { Button, IconButton } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import { Dialog } from '@/components/ui/dialog'
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from '@/components/ui/dropdown-menu'
import { EmptyState } from '@/components/ui/empty-state'
import { Field } from '@/components/ui/field'
import { FilterBar } from '@/components/ui/filter-bar'
import { Input, Textarea } from '@/components/ui/input'
import { PageHeader } from '@/components/ui/page-header'
import { Table, type ColumnDef } from '@/components/ui/table'
import { toast } from '@/components/ui/toast'
import { errorMessage, type S } from '../shared/api'
import { useProjectCtx } from '../shared/context'
import { str, useSearchState } from '../shared/search-state'
import { ConfirmDialog, ErrorPanel, RelTime } from '../shared/ui'
import { useCreateDataset, useDatasets, useDeleteDataset } from './api'

export function NewDatasetDialog({ open, onOpenChange, projectId, onCreated }: { open: boolean; onOpenChange: (o: boolean) => void; projectId?: string; onCreated: (d: S['Dataset']) => void }) {
  const create = useCreateDataset(projectId)
  const [name, setName] = useState('')
  const [description, setDescription] = useState('')
  const [touched, setTouched] = useState(false)
  const submit = () => {
    setTouched(true)
    if (!name.trim()) return
    create.mutate(
      { name: name.trim(), description },
      {
        onSuccess: (d) => {
          setName('')
          setDescription('')
          setTouched(false)
          onOpenChange(false)
          onCreated(d)
        },
      },
    )
  }
  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      size="sm"
      title="New dataset"
      description="Start empty, then add examples inline or import a file."
      footer={
        <>
          <Button variant="ghost" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button variant="primary" loading={create.isPending} onClick={submit}>
            Create dataset
          </Button>
        </>
      }
    >
      <form
        className="flex flex-col gap-4"
        onSubmit={(e) => {
          e.preventDefault()
          submit()
        }}
      >
        <Field label="Name" error={touched && !name.trim() ? 'Give the dataset a name' : undefined}>
          <Input autoFocus value={name} onChange={(e) => setName(e.target.value)} placeholder="Support questions" />
        </Field>
        <Field label="Description" optional>
          <Textarea rows={2} value={description} onChange={(e) => setDescription(e.target.value)} />
        </Field>
      </form>
    </Dialog>
  )
}

export function DatasetsScreen() {
  const ctx = useProjectCtx()
  const datasets = useDatasets(ctx.projectId)
  const del = useDeleteDataset(ctx.projectId)
  const navigate = useNavigate()
  const [search, patch] = useSearchState()
  const q = str(search.q)
  const [creating, setCreating] = useState(false)
  const [toDelete, setToDelete] = useState<S['Dataset'] | null>(null)

  const open = (id: string, extra?: Record<string, unknown>) => void navigate({ to: '/o/$org/p/$project/datasets/$dataset', params: { ...ctx.params, dataset: id }, search: extra as never })
  const rows = useMemo(() => (datasets.data ?? []).filter((d) => !q || `${d.name} ${d.description}`.toLowerCase().includes(q.toLowerCase())), [datasets.data, q])

  const columns = useMemo<ColumnDef<S['Dataset'], unknown>[]>(
    () => [
      {
        accessorKey: 'name',
        header: 'Dataset',
        size: 300,
        meta: { grow: true },
        cell: (c) => (
          <span className="flex min-w-0 flex-col">
            <span className="truncate font-medium text-text">{c.row.original.name}</span>
            {c.row.original.description && <span className="truncate text-xs text-text-muted">{c.row.original.description}</span>}
          </span>
        ),
      },
      { accessorKey: 'example_count', header: 'Examples', size: 110, cell: (c) => <span className="tabular text-text-2">{c.row.original.example_count.toLocaleString()}</span> },
      {
        id: 'version',
        header: 'Latest version',
        size: 170,
        enableSorting: false,
        cell: (c) => (
          <span className="flex items-center gap-1.5">
            {c.row.original.latest_version ? <Badge mono>v{c.row.original.latest_version.number}</Badge> : <span className="text-text-muted">unpublished</span>}
            {c.row.original.has_draft_changes && <Badge variant="warn">Draft changes</Badge>}
          </span>
        ),
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
    ],
    [],
  )

  const newButton = (
    <Button variant="primary" icon={<Plus />} onClick={() => setCreating(true)}>
      New dataset
    </Button>
  )

  return (
    <Screen>
      <PageHeader icon={<Database />} title="Datasets" description="Versioned collections of examples. Every published version is immutable, so runs stay reproducible." actions={newButton} />
      {datasets.error ? (
        <Card>
          <ErrorPanel error={datasets.error} onRetry={() => void datasets.refetch()} />
        </Card>
      ) : !datasets.isPending && (datasets.data?.length ?? 0) === 0 ? (
        <Card>
          <EmptyState
            illustration="datasets"
            title="No datasets yet"
            description="Import CSV, JSONL, JSON or Parquet, pull from Hugging Face, or type examples straight into the editor."
            action={newButton}
            secondaryAction={
              <Button variant="ghost" icon={<Upload />} onClick={() => setCreating(true)}>
                Import data
              </Button>
            }
          />
        </Card>
      ) : (
        <>
          <FilterBar search={q} onSearchChange={(v) => patch({ q: v })} searchPlaceholder="Search datasets…" />
          <Card>
            <Table
              aria-label="Datasets"
              data={rows}
              columns={columns}
              getRowId={(d) => d.id}
              loading={datasets.isPending}
              onRowActivate={(d) => open(d.id)}
              empty={<EmptyState compact illustration="search" title="No datasets match" description="Try a different search." />}
            />
          </Card>
        </>
      )}
      <NewDatasetDialog open={creating} onOpenChange={setCreating} projectId={ctx.projectId} onCreated={(d) => open(d.id)} />
      <ConfirmDialog
        open={!!toDelete}
        onOpenChange={(o) => !o && setToDelete(null)}
        title={`Delete ${toDelete?.name ?? 'dataset'}?`}
        description="All examples and published versions are removed. Suites using it must be updated. This cannot be undone."
        loading={del.isPending}
        onConfirm={() =>
          toDelete &&
          del.mutate(toDelete.id, {
            onSuccess: () => {
              toast.success('Dataset deleted')
              setToDelete(null)
            },
            onError: (e) => toast.error(errorMessage(e)),
          })
        }
      />
    </Screen>
  )
}
