/* eslint-disable react-hooks/set-state-in-effect -- local drafts are seeded from server data */
import { useNavigate, useParams } from '@tanstack/react-router'
import { Activity, Crosshair, History, RotateCcw, Save, Settings2, Trash2, Zap } from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'
import { Screen } from '@/app/screen'
import { TrendLine } from '@/charts'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardBody, CardHeader } from '@/components/ui/card'
import { DiffViewer } from '@/components/ui/diff-viewer'
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from '@/components/ui/dropdown-menu'
import { EmptyState } from '@/components/ui/empty-state'
import { Field } from '@/components/ui/field'
import { Input, Textarea } from '@/components/ui/input'
import { PageHeader } from '@/components/ui/page-header'
import { Skeleton } from '@/components/ui/skeleton'
import { StatTile } from '@/components/ui/stat-tile'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { toast } from '@/components/ui/toast'
import { cn } from '@/lib/cn'
import { formatDuration, formatPercent } from '@/lib/format'
import { errorMessage, type Obj, type S } from '../shared/api'
import { useProjectCtx } from '../shared/context'
import { useDatasets } from '../datasets/api'
import { stableStringify } from '../shared/obj'
import { useSearchState, str } from '../shared/search-state'
import { ConfirmDialog, ErrorPanel, FormSection, RelTime } from '../shared/ui'
import { useCreateTarget, useDeleteTarget, useTarget, useTargetRuns, useTargetTrend, useTargetVersions, useUpdateTarget } from './api'
import { defaultConfig, kindMeta, kindOrder, validateTarget, type TargetKind } from './kinds'
import { Playground } from './playground'
import { TargetForm } from './target-form'

type Tab = 'configuration' | 'playground' | 'versions' | 'health'

function KindPicker({ onPick }: { onPick: (k: TargetKind) => void }) {
  return (
    <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3">
      {kindOrder.map((k) => {
        const m = kindMeta[k]
        return (
          <button
            key={k}
            type="button"
            onClick={() => onPick(k)}
            className="group flex flex-col gap-2 rounded-md border border-border bg-surface p-4 text-left transition-colors duration-150 hover:border-border-strong hover:bg-surface-hover focus-ring"
          >
            <span className="flex size-9 items-center justify-center rounded-md border border-border bg-surface-2 text-text-2 group-hover:text-accent [&_svg]:size-[18px]">
              <m.icon />
            </span>
            <span className="text-base font-semibold text-text">{m.label}</span>
            <span className="text-sm text-text-2">{m.description}</span>
          </button>
        )
      })}
    </div>
  )
}

function VersionsTab({ targetId, current, onRestore }: { targetId: string; current: Obj; onRestore: (v: S['TargetVersion']) => void }) {
  const versions = useTargetVersions(targetId)
  const [selected, setSelected] = useState<number | null>(null)
  const list = versions.data ?? []
  const active = list.find((v) => v.number === selected) ?? list[1] ?? list[0]
  if (versions.error) return <Card><ErrorPanel error={versions.error} onRetry={() => void versions.refetch()} /></Card>
  if (versions.isLoading) return <Card className="p-4"><Skeleton className="h-48 w-full" /></Card>
  return (
    <div className="grid grid-cols-1 gap-3 lg:grid-cols-[280px_minmax(0,1fr)]">
      <Card className="self-start">
        <CardHeader title="Versions" description="Every config change creates an immutable version." />
        <ul className="flex flex-col p-2">
          {list.map((v, i) => (
            <li key={v.id}>
              <button
                type="button"
                aria-current={active?.id === v.id}
                onClick={() => setSelected(v.number)}
                className={cn('flex w-full items-center justify-between gap-2 rounded-md px-2.5 py-2 text-left text-sm transition-colors focus-ring', active?.id === v.id ? 'bg-accent-subtle text-text' : 'text-text-2 hover:bg-surface-hover')}
              >
                <span className="flex items-center gap-2">
                  <Badge mono variant={i === 0 ? 'accent' : 'neutral'}>v{v.number}</Badge>
                  {i === 0 && <span className="text-xs text-text-muted">current</span>}
                </span>
                <RelTime iso={v.created_at} className="text-xs" />
              </button>
            </li>
          ))}
        </ul>
      </Card>
      <Card className="min-w-0">
        {active ? (
          <>
            <CardHeader
              title={`v${active.number} vs. editor`}
              description="What would change if you saved the editor as a new version."
              actions={
                <Button size="sm" icon={<RotateCcw />} onClick={() => onRestore(active)}>
                  Load into editor
                </Button>
              }
            />
            <CardBody>
              <DiffViewer oldValue={stableStringify(active.config)} newValue={stableStringify(current)} oldLabel={`v${active.number}`} newLabel="Editor" mode="lines" maxHeight={520} />
            </CardBody>
          </>
        ) : (
          <EmptyState compact title="No versions yet" />
        )}
      </Card>
    </div>
  )
}

function HealthTab({ projectId, targetId }: { projectId?: string; targetId: string }) {
  const p95 = useTargetTrend(projectId, targetId, 'latency_p95')
  const p50 = useTargetTrend(projectId, targetId, 'latency_p50')
  const pass = useTargetTrend(projectId, targetId, 'pass_rate')
  const runs = useTargetRuns(projectId, targetId)
  const toSeries = (name: string, d?: S['Trend']) => ({ name, points: (d?.series[0]?.points ?? []).map((p) => ({ t: p.t, v: p.value ?? null })) })
  const latency = [toSeries('p50', p50.data), toSeries('p95', p95.data)]
  const hasData = latency.some((s) => s.points.length > 0)
  const items = runs.data?.items ?? []
  const totals = items.reduce((a, r) => ({ total: a.total + r.total, errored: a.errored + r.errored }), { total: 0, errored: 0 })
  const lastP95 = latency[1]!.points.at(-1)?.v
  const lastPass = toSeries('pass', pass.data).points.at(-1)?.v
  const loading = p95.isLoading || runs.isLoading
  if (!loading && !hasData && items.length === 0) {
    return (
      <Card>
        <EmptyState illustration="chart" title="No health data yet" description="Latency and error rate appear after the first run against this target." />
      </Card>
    )
  }
  return (
    <div className="flex flex-col gap-3">
      <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
        <StatTile label="Latency p95" loading={loading} value={lastP95 != null ? formatDuration(lastP95) : '—'} trend={latency[1]!.points.map((p) => p.v ?? 0).slice(-20)} />
        <StatTile label="Error rate" loading={loading} value={totals.total ? formatPercent(totals.errored / totals.total) : '—'} unit={totals.total ? `of ${totals.total} calls` : undefined} delta={undefined} />
        <StatTile label="Latest pass rate" loading={loading} value={lastPass != null ? formatPercent(lastPass) : '—'} trend={toSeries('pass', pass.data).points.map((p) => p.v ?? 0).slice(-20)} />
      </div>
      <TrendLine title="Latency" description="Per run, p50 and p95" series={latency} format="ms" area={false} loading={loading} empty={<EmptyState compact illustration="chart" title="No runs yet" />} />
      <TrendLine title="Pass rate" description="Per run" series={[toSeries('Pass rate', pass.data)]} yDomain={[0, 1]} loading={pass.isLoading} empty={<EmptyState compact illustration="chart" title="No runs yet" />} />
    </div>
  )
}

export function TargetDetailScreen() {
  const { target: targetId } = useParams({ strict: false }) as { target: string }
  const isNew = targetId === 'new'
  const ctx = useProjectCtx()
  const navigate = useNavigate()
  const [search, patch] = useSearchState()
  const tab = (str(search.tab, 'configuration') as Tab) ?? 'configuration'

  const target = useTarget(targetId)
  const create = useCreateTarget(ctx.projectId)
  const update = useUpdateTarget(isNew ? undefined : targetId, ctx.projectId)
  const del = useDeleteTarget(ctx.projectId)
  const datasets = useDatasets(ctx.projectId)

  const [kind, setKind] = useState<TargetKind | null>(null)
  const [name, setName] = useState('')
  const [description, setDescription] = useState('')
  const [config, setConfig] = useState<Obj>({})
  const [baseline, setBaseline] = useState('')
  const [showErrors, setShowErrors] = useState(false)
  const [confirmDelete, setConfirmDelete] = useState(false)

  // Seed the editor from the server record (and re-seed after a save creates a new version).
  const serverKey = target.data ? `${target.data.id}:${target.data.current_version?.number}:${target.data.updated_at}` : ''
  useEffect(() => {
    if (!target.data) return
    const cfg = (target.data.current_version?.config ?? {}) as Obj
    setKind(target.data.kind)
    setName(target.data.name)
    setDescription(target.data.description)
    setConfig(cfg)
    setBaseline(stableStringify({ n: target.data.name, d: target.data.description, c: cfg }))
    setShowErrors(false)
  }, [serverKey]) // eslint-disable-line react-hooks/exhaustive-deps

  const errors = useMemo(() => (kind ? validateTarget(kind, config) : {}), [kind, config])
  const nameError = !name.trim() ? 'Give the target a name' : undefined
  const dirty = isNew ? !!kind : stableStringify({ n: name, d: description, c: config }) !== baseline
  const inputFields = useMemo(() => {
    const ds = datasets.data?.find((d) => d.schema?.input_fields?.length)
    return ds?.schema?.input_fields ?? ['question']
  }, [datasets.data])

  if (!isNew && target.isLoading) {
    return (
      <Screen>
        <Skeleton className="h-10 w-72" />
        <Skeleton className="h-96 w-full" />
      </Screen>
    )
  }
  if (!isNew && target.error) {
    return (
      <Screen>
        <Card>
          <ErrorPanel error={target.error} onRetry={() => void target.refetch()} />
        </Card>
      </Screen>
    )
  }

  const setTab = (t: string) => patch({ tab: t === 'configuration' ? null : t })

  const save = () => {
    setShowErrors(true)
    if (!kind || nameError || Object.keys(errors).length) {
      toast.error('Fix the highlighted fields first')
      return
    }
    if (isNew) {
      create.mutate(
        { name: name.trim(), description, kind, config },
        {
          onSuccess: (t) => {
            toast.success('Target created')
            void navigate({ to: '/o/$org/p/$project/targets/$target', params: { ...ctx.params, target: t.id }, replace: true, search: { tab: 'playground' } as never })
          },
        },
      )
    } else {
      update.mutate(
        { name: name.trim(), description, config },
        { onSuccess: () => toast.success('Saved. A new version was created.') },
      )
    }
  }

  if (isNew && !kind) {
    return (
      <Screen>
        <PageHeader icon={<Crosshair />} title="New target" description="Choose how airval talks to your AI service." />
        <KindPicker
          onPick={(k) => {
            setKind(k)
            setConfig(defaultConfig(k))
            setName('')
          }}
        />
      </Screen>
    )
  }
  if (!kind) return null

  const meta = kindMeta[kind]
  const version = target.data?.current_version
  const saving = create.isPending || update.isPending

  return (
    <Screen className="pb-24">
      <PageHeader
        icon={<meta.icon />}
        title={isNew ? 'New target' : name || 'Target'}
        description={meta.label}
        meta={version ? <Badge mono>v{version.number}</Badge> : undefined}
        actions={
          <>
            <Button icon={<Zap />} onClick={() => setTab('playground')}>
              Test connection
            </Button>
            {!isNew && (
              <DropdownMenu>
                <DropdownMenuTrigger asChild>
                  <Button aria-label="More actions" icon={<Settings2 />} />
                </DropdownMenuTrigger>
                <DropdownMenuContent align="end">
                  <DropdownMenuItem destructive icon={<Trash2 />} onSelect={() => setConfirmDelete(true)}>
                    Delete target…
                  </DropdownMenuItem>
                </DropdownMenuContent>
              </DropdownMenu>
            )}
          </>
        }
        tabs={
          <Tabs value={tab} onValueChange={setTab}>
            <TabsList>
              <TabsTrigger value="configuration">Configuration</TabsTrigger>
              <TabsTrigger value="playground">Playground</TabsTrigger>
              {!isNew && <TabsTrigger value="versions">Versions</TabsTrigger>}
              {!isNew && <TabsTrigger value="health">Health</TabsTrigger>}
            </TabsList>
          </Tabs>
        }
      />

      {tab === 'configuration' && (
        <Card>
          <FormSection title="Identity" description="How this target appears in suites and run comparisons.">
            <Field label="Name" error={showErrors ? nameError : undefined}>
              <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="Production" autoFocus={isNew} />
            </Field>
            <Field label="Description" optional>
              <Textarea rows={2} value={description} onChange={(e) => setDescription(e.target.value)} placeholder="What this endpoint or model is, and who owns it." />
            </Field>
          </FormSection>
          <TargetForm kind={kind} config={config} onChange={setConfig} errors={showErrors ? errors : errorsFor(errors, config)} inputFields={inputFields} />
        </Card>
      )}
      {tab === 'playground' && (
        <Playground kind={kind} config={config} projectId={ctx.projectId} disabledReason={Object.keys(errors).length ? 'Complete the required fields in Configuration first.' : undefined} />
      )}
      {tab === 'versions' && !isNew && (
        <VersionsTab
          targetId={targetId}
          current={config}
          onRestore={(v) => {
            setConfig(v.config as Obj)
            setTab('configuration')
            toast.info(`Loaded v${v.number} into the editor. Save to publish it as a new version.`)
          }}
        />
      )}
      {tab === 'health' && !isNew && <HealthTab projectId={ctx.projectId} targetId={targetId} />}

      {dirty && tab !== 'health' && (
        <div className="fixed inset-x-0 bottom-0 z-20 border-t border-border bg-surface/95 backdrop-blur supports-[backdrop-filter]:bg-surface/80">
          <div className="mx-auto flex max-w-[1280px] items-center justify-between gap-3 px-4 py-3 sm:px-6 lg:px-8">
            <span className="flex items-center gap-2 text-sm text-text-2">
              <Activity className="size-4 text-text-muted" />
              {isNew ? 'Ready to create this target.' : 'Unsaved changes. Saving creates a new version.'}
            </span>
            <div className="flex items-center gap-2">
              {!isNew && (
                <Button
                  variant="ghost"
                  icon={<History />}
                  onClick={() => {
                    if (target.data) {
                      setName(target.data.name)
                      setDescription(target.data.description)
                      setConfig((target.data.current_version?.config ?? {}) as Obj)
                    }
                  }}
                >
                  Discard
                </Button>
              )}
              <Button variant="primary" icon={<Save />} loading={saving} onClick={save}>
                {isNew ? 'Create target' : 'Save version'}
              </Button>
            </div>
          </div>
        </div>
      )}
      <ConfirmDialog
        open={confirmDelete}
        onOpenChange={setConfirmDelete}
        title={`Delete ${name || 'this target'}?`}
        description="Suites that use this target will stop running until you pick another. Past runs keep their results."
        loading={del.isPending}
        onConfirm={() =>
          del.mutate(targetId, {
            onSuccess: () => {
              toast.success('Target deleted')
              void navigate({ to: '/o/$org/p/$project/targets', params: ctx.params })
            },
            onError: (e) => toast.error(errorMessage(e)),
          })
        }
      />
    </Screen>
  )
}

/** Before the first save attempt only surface mapping-path syntax errors (they are hard to spot), not "required". */
function errorsFor(errors: Record<string, string>, _config: Obj) {
  return Object.fromEntries(Object.entries(errors).filter(([, m]) => m.startsWith('Not a valid')))
}
