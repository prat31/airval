import { useQueryClient, type InfiniteData } from '@tanstack/react-query'
import { useNavigate, useParams } from '@tanstack/react-router'
import { Database, Download, MoreHorizontal, Pencil, Plus, Rocket, Tag, Trash2, Upload, X } from 'lucide-react'
import { useCallback, useEffect, useMemo, useState } from 'react'
import { Screen } from '@/app/screen'
import { Badge } from '@/components/ui/badge'
import { Button, IconButton } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import { Combobox } from '@/components/ui/combobox'
import { Dialog } from '@/components/ui/dialog'
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuSeparator, DropdownMenuTrigger } from '@/components/ui/dropdown-menu'
import { EmptyState } from '@/components/ui/empty-state'
import { Field } from '@/components/ui/field'
import { Input, Textarea } from '@/components/ui/input'
import { PageHeader } from '@/components/ui/page-header'
import { Select } from '@/components/ui/select'
import { Skeleton } from '@/components/ui/skeleton'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { toast } from '@/components/ui/toast'
import { Tooltip } from '@/components/ui/tooltip'
import { errorMessage, type S } from '../shared/api'
import { useProjectCtx } from '../shared/context'
import { num, str, useSearchState } from '../shared/search-state'
import { ConfirmDialog, ErrorPanel, TagInput, useDebounced } from '../shared/ui'
import { datasetKeys, downloadExport, useDataset, useDatasetVersions, useDeleteDataset, useDeleteExamples, useExamples, usePublishVersion, useUpdateDataset, useUpsertExamples } from './api'
import { StatsTab, VersionsTab } from './dataset-tabs'
import { applyEdit, ExampleGrid, useColumns, type Col } from './grid'
import { ImportWizard } from './import-wizard'

type Example = S['Example']
type Tab = 'examples' | 'versions' | 'stats'
type ExamplePages = InfiniteData<S['ExampleList']>

export function DatasetDetailScreen() {
  const { dataset: id } = useParams({ strict: false }) as { dataset: string }
  const ctx = useProjectCtx()
  const navigate = useNavigate()
  const qc = useQueryClient()
  const [search, patch] = useSearchState()
  const tab = str(search.tab, 'examples') as Tab
  const version = num(search.v)
  const qParam = str(search.q)
  const tag = str(search.tag)
  const [qInput, setQInput] = useState(qParam)
  const q = useDebounced(qInput, 300)
  useEffect(() => {
    if (q !== qParam) patch({ q })
  }, [q]) // eslint-disable-line react-hooks/exhaustive-deps

  const dataset = useDataset(id)
  const versions = useDatasetVersions(id)
  const examples = useExamples(id, { version, q: qParam, tag })
  const upsert = useUpsertExamples(id)
  const remove = useDeleteExamples(id)
  const publish = usePublishVersion(id)
  const update = useUpdateDataset(id)
  const del = useDeleteDataset(ctx.projectId)

  const rows = useMemo(() => examples.data?.pages.flatMap((p) => p.items) ?? [], [examples.data])
  const [extraKeys, setExtraKeys] = useState<string[]>([])
  const columns = useColumns(rows, dataset.data?.schema?.input_fields, extraKeys)
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [focusRow, setFocusRow] = useState<string | null>(null)

  const [importing, setImporting] = useState(false)
  const [publishing, setPublishing] = useState(false)
  const [note, setNote] = useState('')
  const [bulkTagging, setBulkTagging] = useState(false)
  const [bulkTags, setBulkTags] = useState<string[]>([])
  const [deleteRows, setDeleteRows] = useState(false)
  const [renaming, setRenaming] = useState(false)
  const [confirmDelete, setConfirmDelete] = useState(false)
  const [newCol, setNewCol] = useState<string | null>(null)

  const readOnly = version !== undefined
  const d = dataset.data
  const tagOptions = useMemo(() => [...new Set(rows.flatMap((r) => r.tags ?? []))].sort().map((t) => ({ value: t, label: t })), [rows])

  // Keep fetching until a freshly added row is in memory.
  useEffect(() => {
    if (focusRow && !rows.some((r) => r.id === focusRow) && examples.hasNextPage && !examples.isFetchingNextPage) void examples.fetchNextPage()
  }, [focusRow, rows, examples])

  const patchRows = useCallback(
    (fn: (e: Example) => Example) => {
      qc.setQueriesData<ExamplePages>({ queryKey: ['examples', id] }, (old) => old && { ...old, pages: old.pages.map((p) => ({ ...p, items: p.items.map(fn) })) })
    },
    [qc, id],
  )

  const onEdit = (ex: Example, col: Col, text: string) => {
    const data = applyEdit(ex, col, text)
    // Optimistic: reflect the edit immediately, reconcile with the server's revision afterwards.
    patchRows((e) => (e.id === ex.id ? { ...e, input: (data.input ?? e.input) as Example['input'], expected: data.expected, context: data.context, tags: data.tags ?? e.tags } : e))
    upsert.mutate([data], {
      onError: (err) => {
        toast.error(errorMessage(err))
        void qc.invalidateQueries({ queryKey: ['examples', id] })
      },
    })
  }

  const addRow = () => {
    const key = columns.find((c) => c.kind === 'input')?.key ?? 'question'
    upsert.mutate([{ input: { [key]: '' } }], {
      onSuccess: (r) => {
        setFocusRow(r.examples[0]?.id ?? null)
      },
    })
  }

  const exportVersionNumber = version ?? d?.latest_version?.number
  const doExport = async (fmt: 'jsonl' | 'csv') => {
    if (!d || exportVersionNumber === undefined) return
    try {
      await downloadExport(d.id, exportVersionNumber, fmt, d.name.replace(/\s+/g, '-').toLowerCase())
    } catch (e) {
      toast.error(errorMessage(e))
    }
  }

  if (dataset.isLoading) {
    return (
      <Screen>
        <Skeleton className="h-10 w-72" />
        <Skeleton className="h-96 w-full" />
      </Screen>
    )
  }
  if (dataset.error || !d) {
    return (
      <Screen>
        <Card>
          <ErrorPanel error={dataset.error ?? new Error('Dataset not found')} onRetry={() => void dataset.refetch()} />
        </Card>
      </Screen>
    )
  }

  const versionList = versions.data ?? []
  const versionOptions = [
    { value: 'draft', label: d.has_draft_changes ? 'Draft · unpublished changes' : 'Draft' },
    ...versionList.map((v) => ({ value: String(v.number), label: `v${v.number}${v.note ? ` · ${v.note}` : ''}` })),
  ]
  const count = version !== undefined ? (versionList.find((v) => v.number === version)?.example_count ?? rows.length) : d.example_count
  const filtering = !!qParam || !!tag
  const hasRows = rows.length > 0

  const selectedRows = rows.filter((r) => selected.has(r.id))
  const bulkAddTags = () => {
    const payload = selectedRows.map((r) => ({ id: r.id, input: r.input, expected: r.expected, context: r.context, metadata: r.metadata, tags: [...new Set([...(r.tags ?? []), ...bulkTags])], conversation: r.conversation }))
    upsert.mutate(payload, {
      onSuccess: () => {
        toast.success(`Tagged ${payload.length} example${payload.length === 1 ? '' : 's'}`)
        setBulkTagging(false)
        setBulkTags([])
        setSelected(new Set())
      },
    })
  }

  return (
    <Screen>
      <PageHeader
        icon={<Database />}
        title={d.name}
        description={d.description || undefined}
        meta={
          <>
            <Badge>{count.toLocaleString()} examples</Badge>
            {d.has_draft_changes && !readOnly && <Badge variant="warn">Unpublished changes</Badge>}
          </>
        }
        actions={
          <>
            <Select
              aria-label="Version"
              className="w-56"
              value={version === undefined ? 'draft' : String(version)}
              onValueChange={(v) => {
                setSelected(new Set())
                patch({ v: v === 'draft' ? null : v })
              }}
              options={versionOptions}
            />
            <Button icon={<Upload />} disabled={readOnly} onClick={() => setImporting(true)}>
              Import
            </Button>
            <DropdownMenu>
              <Tooltip content={exportVersionNumber === undefined ? 'Publish a version to export' : `Export v${exportVersionNumber}`}>
                <span>
                  <DropdownMenuTrigger asChild>
                    <Button icon={<Download />} disabled={exportVersionNumber === undefined}>
                      Export
                    </Button>
                  </DropdownMenuTrigger>
                </span>
              </Tooltip>
              <DropdownMenuContent align="end">
                <DropdownMenuItem onSelect={() => void doExport('jsonl')}>JSONL</DropdownMenuItem>
                <DropdownMenuItem onSelect={() => void doExport('csv')}>CSV</DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
            <Button variant="primary" icon={<Rocket />} disabled={readOnly || (!d.has_draft_changes && versionList.length > 0) || d.example_count === 0} onClick={() => setPublishing(true)}>
              Publish version
            </Button>
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <IconButton label="More actions" variant="secondary">
                  <MoreHorizontal />
                </IconButton>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end">
                <DropdownMenuItem icon={<Pencil />} onSelect={() => setRenaming(true)}>
                  Edit details…
                </DropdownMenuItem>
                <DropdownMenuSeparator />
                <DropdownMenuItem destructive icon={<Trash2 />} onSelect={() => setConfirmDelete(true)}>
                  Delete dataset…
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          </>
        }
        tabs={
          <Tabs value={tab} onValueChange={(t) => patch({ tab: t === 'examples' ? null : t })}>
            <TabsList>
              <TabsTrigger value="examples">Examples</TabsTrigger>
              <TabsTrigger value="versions">Versions{versionList.length > 0 && <span className="ml-1.5 text-xs text-text-muted">{versionList.length}</span>}</TabsTrigger>
              <TabsTrigger value="stats">Column stats</TabsTrigger>
            </TabsList>
          </Tabs>
        }
      />

      {tab === 'examples' && (
        <Card className="overflow-hidden">
          {readOnly && (
            <div className="flex items-center justify-between gap-3 border-b border-border bg-surface-2 px-4 py-2 text-sm text-text-2">
              <span>
                Viewing <strong className="font-medium text-text">v{version}</strong>, a read-only snapshot.
              </span>
              <Button size="sm" variant="ghost" onClick={() => patch({ v: null })}>
                Back to draft
              </Button>
            </div>
          )}
          <div className="flex flex-wrap items-center gap-2 border-b border-border px-3 py-2">
            {selected.size > 0 && !readOnly ? (
              <>
                <span className="mr-1 text-sm font-medium text-text">{selected.size} selected</span>
                <Button size="sm" icon={<Tag />} onClick={() => setBulkTagging(true)}>
                  Add tags
                </Button>
                <Button size="sm" variant="danger" icon={<Trash2 />} onClick={() => setDeleteRows(true)}>
                  Delete
                </Button>
                <Button size="sm" variant="ghost" icon={<X />} onClick={() => setSelected(new Set())}>
                  Clear
                </Button>
              </>
            ) : (
              <>
                <Input size="sm" className="w-64" value={qInput} onChange={(e) => setQInput(e.target.value)} placeholder="Search input, expected, metadata…" aria-label="Search examples" />
                <Combobox size="sm" className="w-44" options={tagOptions} value={tag || null} onValueChange={(v) => patch({ tag: v })} placeholder="All tags" searchPlaceholder="Filter tags…" emptyText="No tags" aria-label="Filter by tag" />
                {filtering && (
                  <Button
                    size="sm"
                    variant="ghost"
                    onClick={() => {
                      setQInput('')
                      patch({ q: null, tag: null })
                    }}
                  >
                    Clear filters
                  </Button>
                )}
              </>
            )}
            <div className="ml-auto flex items-center gap-2">
              <span className="hidden text-xs text-text-muted md:inline">Enter to edit · Tab / arrows to move · x to select</span>
              {!readOnly && (
                <Button size="sm" icon={<Plus />} loading={upsert.isPending && !focusRow} onClick={addRow}>
                  Add row
                </Button>
              )}
            </div>
          </div>
          {examples.error ? (
            <ErrorPanel error={examples.error} onRetry={() => void examples.refetch()} />
          ) : (
            <ExampleGrid
              rows={rows}
              columns={columns}
              readOnly={readOnly}
              loading={examples.isLoading}
              hasMore={examples.hasNextPage}
              loadingMore={examples.isFetchingNextPage}
              onLoadMore={() => void examples.fetchNextPage()}
              selected={selected}
              onSelectedChange={setSelected}
              onEdit={onEdit}
              onAddColumn={() => setNewCol('')}
              focusRowId={focusRow}
              onFocusHandled={() => setFocusRow(null)}
              empty={
                filtering && !hasRows ? (
                  <EmptyState compact illustration="search" title="No examples match" description="Clear the search or tag filter to see everything." />
                ) : (
                  <EmptyState
                    illustration="datasets"
                    title="This dataset is empty"
                    description="Type examples into the grid, or import a file or Hugging Face dataset."
                    action={
                      <Button variant="primary" icon={<Plus />} disabled={readOnly} onClick={addRow}>
                        Add first example
                      </Button>
                    }
                    secondaryAction={
                      <Button icon={<Upload />} disabled={readOnly} onClick={() => setImporting(true)}>
                        Import data
                      </Button>
                    }
                  />
                )
              }
            />
          )}
        </Card>
      )}
      {tab === 'versions' && <VersionsTab dataset={d} onView={(v) => patch({ tab: null, v })} />}
      {tab === 'stats' && <StatsTab dataset={d} version={version} />}

      <ImportWizard open={importing} onOpenChange={setImporting} datasetId={d.id} />

      <Dialog
        open={publishing}
        onOpenChange={setPublishing}
        size="sm"
        title="Publish version"
        description={`Freeze the draft's ${d.example_count.toLocaleString()} examples as version ${(d.latest_version?.number ?? 0) + 1}. Published versions are immutable.`}
        footer={
          <>
            <Button variant="ghost" onClick={() => setPublishing(false)}>
              Cancel
            </Button>
            <Button
              variant="primary"
              loading={publish.isPending}
              onClick={() =>
                publish.mutate(note.trim(), {
                  onSuccess: (v) => {
                    toast.success(`Published v${v.number}`)
                    setPublishing(false)
                    setNote('')
                  },
                  onError: (e) => toast.error(errorMessage(e)),
                })
              }
            >
              Publish
            </Button>
          </>
        }
      >
        <Field label="Note" optional hint="What changed? Shown in the version list and diffs.">
          <Textarea autoFocus rows={3} value={note} onChange={(e) => setNote(e.target.value)} placeholder="Added 40 refund-policy edge cases" />
        </Field>
      </Dialog>

      <Dialog
        open={bulkTagging}
        onOpenChange={setBulkTagging}
        size="sm"
        title={`Add tags to ${selected.size} example${selected.size === 1 ? '' : 's'}`}
        footer={
          <>
            <Button variant="ghost" onClick={() => setBulkTagging(false)}>
              Cancel
            </Button>
            <Button variant="primary" disabled={bulkTags.length === 0} loading={upsert.isPending} onClick={bulkAddTags}>
              Add tags
            </Button>
          </>
        }
      >
        <TagInput value={bulkTags} onChange={setBulkTags} placeholder="Type a tag and press Enter" suggestions={tagOptions.map((t) => t.value)} />
      </Dialog>

      <ConfirmDialog
        open={deleteRows}
        onOpenChange={setDeleteRows}
        title={`Delete ${selected.size} example${selected.size === 1 ? '' : 's'}?`}
        description="They are removed from the draft. Published versions are unaffected."
        loading={remove.isPending}
        onConfirm={() =>
          remove.mutate([...selected], {
            onSuccess: () => {
              setSelected(new Set())
              setDeleteRows(false)
            },
          })
        }
      />

      <Dialog
        open={newCol !== null}
        onOpenChange={(o) => !o && setNewCol(null)}
        size="sm"
        title="Add input column"
        description="Input keys become {{ .input.name }} in target templates."
        footer={
          <>
            <Button variant="ghost" onClick={() => setNewCol(null)}>
              Cancel
            </Button>
            <Button
              variant="primary"
              disabled={!/^[A-Za-z_][\w]*$/.test(newCol ?? '')}
              onClick={() => {
                setExtraKeys((k) => [...k, newCol!])
                setNewCol(null)
              }}
            >
              Add column
            </Button>
          </>
        }
      >
        <Field label="Column name" hint="Letters, digits and underscores.">
          <Input autoFocus value={newCol ?? ''} onChange={(e) => setNewCol(e.target.value)} placeholder="customer_tier" />
        </Field>
      </Dialog>

      <RenameDialog open={renaming} onOpenChange={setRenaming} dataset={d} onSave={(b) => update.mutate(b, { onSuccess: () => setRenaming(false), onError: (e) => toast.error(errorMessage(e)) })} saving={update.isPending} />

      <ConfirmDialog
        open={confirmDelete}
        onOpenChange={setConfirmDelete}
        title={`Delete ${d.name}?`}
        description="All examples and published versions are removed. This cannot be undone."
        loading={del.isPending}
        onConfirm={() =>
          del.mutate(d.id, {
            onSuccess: () => {
              void qc.removeQueries({ queryKey: datasetKeys.one(d.id) })
              toast.success('Dataset deleted')
              void navigate({ to: '/o/$org/p/$project/datasets', params: ctx.params })
            },
            onError: (e) => toast.error(errorMessage(e)),
          })
        }
      />
    </Screen>
  )
}

function RenameDialog({ open, onOpenChange, dataset, onSave, saving }: { open: boolean; onOpenChange: (o: boolean) => void; dataset: S['Dataset']; onSave: (b: S['DatasetUpdate']) => void; saving: boolean }) {
  const [name, setName] = useState(dataset.name)
  const [description, setDescription] = useState(dataset.description)
  useEffect(() => {
    if (open) {
      setName(dataset.name)
      setDescription(dataset.description)
    }
  }, [open, dataset])
  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      size="sm"
      title="Edit details"
      footer={
        <>
          <Button variant="ghost" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button variant="primary" disabled={!name.trim()} loading={saving} onClick={() => onSave({ name: name.trim(), description })}>
            Save
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-4">
        <Field label="Name">
          <Input value={name} onChange={(e) => setName(e.target.value)} />
        </Field>
        <Field label="Description" optional>
          <Textarea rows={3} value={description} onChange={(e) => setDescription(e.target.value)} />
        </Field>
      </div>
    </Dialog>
  )
}
