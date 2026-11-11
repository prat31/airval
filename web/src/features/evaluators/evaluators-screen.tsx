import { Gauge, MoreHorizontal, Plus, Trash2 } from 'lucide-react'
import { useMemo, useState } from 'react'
import { Screen } from '@/app/screen'
import { Badge } from '@/components/ui/badge'
import { Button, IconButton } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from '@/components/ui/dropdown-menu'
import { EmptyState } from '@/components/ui/empty-state'
import { FilterBar } from '@/components/ui/filter-bar'
import { PageHeader } from '@/components/ui/page-header'
import { SegmentedControl } from '@/components/ui/segmented-control'
import { Skeleton } from '@/components/ui/skeleton'
import { Table, type ColumnDef } from '@/components/ui/table'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { toast } from '@/components/ui/toast'
import { errorMessage, type S } from '../shared/api'
import { useProjectCtx } from '../shared/context'
import { str, useSearchState } from '../shared/search-state'
import { ConfirmDialog, ErrorPanel, RelTime } from '../shared/ui'
import { categoryLabel, costLabel, humanizeKind, useCatalog, useDeleteEvaluator, useEvaluators, type KindInfo } from './api'
import { EvaluatorEditor } from './evaluator-editor'

type Cat = 'all' | S['EvaluatorCategory']

function CatalogGrid({ items, q, cat, onPick }: { items: KindInfo[]; q: string; cat: Cat; onPick: (k: KindInfo) => void }) {
  const filtered = items.filter((k) => (cat === 'all' || k.category === cat) && (!q || `${k.name} ${k.kind} ${k.description} ${k.group}`.toLowerCase().includes(q.toLowerCase())))
  const groups = useMemo(() => {
    const m = new Map<string, KindInfo[]>()
    for (const k of filtered) {
      const key = `${categoryLabel[k.category]} · ${k.group}`
      m.set(key, [...(m.get(key) ?? []), k])
    }
    return [...m.entries()]
  }, [filtered])
  if (filtered.length === 0) return <Card><EmptyState compact illustration="search" title="No evaluators match" description="Try a different search or category." /></Card>
  return (
    <div className="flex flex-col gap-6">
      {groups.map(([group, kinds]) => (
        <section key={group} aria-label={group}>
          <h3 className="mb-2 text-xs font-semibold uppercase tracking-wide text-text-muted">{group}</h3>
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-3">
            {kinds.map((k) => (
              <button
                key={k.kind}
                type="button"
                onClick={() => onPick(k)}
                className="group flex flex-col gap-1.5 rounded-md border border-border bg-surface p-3.5 text-left transition-colors duration-150 hover:border-border-strong hover:bg-surface-hover focus-ring"
              >
                <span className="flex items-center justify-between gap-2">
                  <span className="truncate text-base font-semibold text-text">{k.name}</span>
                  <Plus className="size-4 shrink-0 text-text-muted opacity-0 transition-opacity group-hover:opacity-100 group-focus-visible:opacity-100" aria-hidden />
                </span>
                <span className="line-clamp-2 text-sm text-text-2">{k.description || humanizeKind(k.kind)}</span>
                <span className="mt-1 flex flex-wrap items-center gap-1.5">
                  <Badge variant={k.category === 'model' ? 'accent' : 'neutral'}>{categoryLabel[k.category]}</Badge>
                  {costLabel(k.cost) && <Badge variant="outline">{costLabel(k.cost)}</Badge>}
                  {(k.requires ?? []).slice(0, 2).map((r) => (
                    <Badge key={r} variant="outline">needs {r.replace(/_/g, ' ')}</Badge>
                  ))}
                </span>
              </button>
            ))}
          </div>
        </section>
      ))}
    </div>
  )
}

export function EvaluatorsScreen() {
  const ctx = useProjectCtx()
  const catalog = useCatalog()
  const evaluators = useEvaluators(ctx.projectId)
  const del = useDeleteEvaluator(ctx.projectId)
  const [search, patch] = useSearchState()
  const editId = str(search.edit)
  const newKind = str(search.new)
  const q = str(search.q)
  const cat = str(search.cat, 'all') as Cat
  const [toDelete, setToDelete] = useState<S['Evaluator'] | null>(null)

  const kindInfo = useMemo(() => new Map((catalog.data?.items ?? []).map((k) => [k.kind, k])), [catalog.data])
  const hasOwn = (evaluators.data?.length ?? 0) > 0
  const tab = str(search.tab, hasOwn || evaluators.isPending ? 'library' : 'catalog')

  const columns: ColumnDef<S['Evaluator'], unknown>[] = [
      {
        accessorKey: 'name',
        header: 'Evaluator',
        size: 280,
        meta: { grow: true },
        cell: (c) => (
          <span className="flex min-w-0 flex-col">
            <span className="truncate font-medium text-text">{c.row.original.name}</span>
            {c.row.original.description && <span className="truncate text-xs text-text-muted">{c.row.original.description}</span>}
          </span>
        ),
      },
      { id: 'kind', accessorFn: (e) => e.kind, header: 'Kind', size: 200, cell: (c) => <span className="text-text-2">{kindInfo.get(c.row.original.kind)?.name ?? humanizeKind(c.row.original.kind)}</span> },
      { id: 'category', accessorFn: (e) => e.category, header: 'Type', size: 130, cell: (c) => <Badge variant={c.row.original.category === 'model' ? 'accent' : 'neutral'}>{categoryLabel[c.row.original.category]}</Badge> },
      { accessorKey: 'version', header: 'Version', size: 90, cell: (c) => <Badge mono>v{c.row.original.version}</Badge> },
      { accessorKey: 'updated_at', header: 'Updated', size: 120, cell: (c) => <RelTime iso={c.row.original.updated_at} /> },
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

  if (editId || newKind) {
    return (
      <Screen>
        <EvaluatorEditor
          id={editId || undefined}
          newKind={newKind || undefined}
          onBack={() => patch({ edit: null, new: null })}
          onSaved={(e) => patch({ edit: e.id, new: null }, { push: false })}
        />
      </Screen>
    )
  }

  const rows = (evaluators.data ?? []).filter((e) => !q || `${e.name} ${e.kind} ${e.description}`.toLowerCase().includes(q.toLowerCase()))
  const newButton = (
    <Button variant="primary" icon={<Plus />} onClick={() => patch({ tab: 'catalog' })}>
      New evaluator
    </Button>
  )

  return (
    <Screen>
      <PageHeader
        icon={<Gauge />}
        title="Evaluators"
        description="Deterministic checks, LLM judges and human review. Start from a preset, tune it, and preview it live."
        actions={newButton}
        tabs={
          <Tabs value={tab} onValueChange={(t) => patch({ tab: t, q: null })}>
            <TabsList>
              <TabsTrigger value="library">Your evaluators{hasOwn && <span className="ml-1.5 text-xs text-text-muted">{evaluators.data!.length}</span>}</TabsTrigger>
              <TabsTrigger value="catalog">Catalog</TabsTrigger>
            </TabsList>
          </Tabs>
        }
      />

      {tab === 'library' ? (
        evaluators.error ? (
          <Card><ErrorPanel error={evaluators.error} onRetry={() => void evaluators.refetch()} /></Card>
        ) : !evaluators.isPending && !hasOwn ? (
          <Card>
            <EmptyState
              illustration="evaluators"
              title="Score outputs your way"
              description="Start from presets like correctness, faithfulness or JSON schema, or write a rubric for an LLM judge."
              action={<Button variant="primary" onClick={() => patch({ tab: 'catalog' })}>Browse the catalog</Button>}
            />
          </Card>
        ) : (
          <>
            <FilterBar search={q} onSearchChange={(v) => patch({ q: v })} searchPlaceholder="Search evaluators…" />
            <Card>
              <Table
                aria-label="Evaluators"
                data={rows}
                columns={columns}
                getRowId={(e) => e.id}
                loading={evaluators.isPending}
                onRowActivate={(e) => patch({ edit: e.id }, { push: true })}
                empty={<EmptyState compact illustration="search" title="No evaluators match" />}
              />
            </Card>
          </>
        )
      ) : catalog.isPending ? (
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-3">
          {Array.from({ length: 9 }, (_, i) => <Skeleton key={i} className="h-28 w-full rounded-md" />)}
        </div>
      ) : catalog.error ? (
        <Card><ErrorPanel error={catalog.error} onRetry={() => void catalog.refetch()} /></Card>
      ) : (
        <>
          <FilterBar
            search={q}
            onSearchChange={(v) => patch({ q: v })}
            searchPlaceholder="Search the catalog…"
            actions={
              <SegmentedControl
                aria-label="Category"
                size="sm"
                value={cat}
                onValueChange={(v) => patch({ cat: v === 'all' ? null : v })}
                options={[
                  { value: 'all', label: 'All' },
                  { value: 'deterministic', label: 'Deterministic' },
                  { value: 'model', label: 'LLM judge' },
                  { value: 'human', label: 'Human' },
                ]}
              />
            }
          />
          <CatalogGrid items={catalog.data!.items} q={q} cat={cat} onPick={(k) => patch({ new: k.kind }, { push: true })} />
        </>
      )}

      <ConfirmDialog
        open={!!toDelete}
        onOpenChange={(o) => !o && setToDelete(null)}
        title={`Delete ${toDelete?.name ?? 'evaluator'}?`}
        description="Suites that use it will lose this evaluator. Past scores are kept."
        loading={del.isPending}
        onConfirm={() =>
          toDelete &&
          del.mutate(toDelete.id, {
            onSuccess: () => {
              toast.success('Evaluator deleted')
              setToDelete(null)
            },
            onError: (e) => toast.error(errorMessage(e)),
          })
        }
      />
    </Screen>
  )
}
