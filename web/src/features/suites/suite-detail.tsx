import { Link, useNavigate, useParams } from '@tanstack/react-router'
import { ChevronDown, CircleCheck, CircleX, Layers, Pencil, Play, Trash2 } from 'lucide-react'
import { useState } from 'react'
import { Screen } from '@/app/screen'
import { TrendLine } from '@/charts'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardBody, CardHeader } from '@/components/ui/card'
import { Dialog } from '@/components/ui/dialog'
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuSeparator, DropdownMenuTrigger } from '@/components/ui/dropdown-menu'
import { EmptyState } from '@/components/ui/empty-state'
import { Field } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { KeyValue } from '@/components/ui/key-value'
import { PageHeader } from '@/components/ui/page-header'
import { Skeleton } from '@/components/ui/skeleton'
import { StatusPill } from '@/components/ui/status-pill'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { toast } from '@/components/ui/toast'
import { useDatasets } from '../datasets/api'
import { categoryLabel, useEvaluators } from '../evaluators/api'
import { errorMessage, type Obj, type S } from '../shared/api'
import { useProjectCtx } from '../shared/context'
import { getIn } from '../shared/obj'
import { num, str, useSearchState } from '../shared/search-state'
import { ConfirmDialog, ErrorPanel, RelTime } from '../shared/ui'
import { useTargets } from '../targets/api'
import { useDeleteSuite, useGates, useRunSuite, useSuite, useSuiteRuns, useSuiteTrend } from './api'
import { SuiteEditor } from './suite-editor'
import { TriggersPanel as Triggers } from './triggers'
import { runStatus } from './suites-screen'

type Tab = 'overview' | 'gates' | 'triggers' | 'history'

function RunOptions({ suite, open, onOpenChange, onRun }: { suite: S['Suite']; open: boolean; onOpenChange: (o: boolean) => void; onRun: (b: Partial<S['RunCreate']>) => void }) {
  const targets = useTargets(suite.project_id)
  const suiteTargets = getIn<string[]>(suite.config, ['target_ids'], [])
  const [ids, setIds] = useState<string[]>(suiteTargets)
  const [sample, setSample] = useState('')
  const [trials, setTrials] = useState('')
  const [label, setLabel] = useState('')
  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      size="sm"
      title="Run with options"
      footer={
        <>
          <Button variant="ghost" onClick={() => onOpenChange(false)}>Cancel</Button>
          <Button
            variant="primary"
            icon={<Play />}
            disabled={ids.length === 0}
            onClick={() =>
              onRun({
                target_ids: ids,
                sampling: sample ? { mode: 'random', n: Number(sample) } : undefined,
                trials: trials ? Number(trials) : undefined,
                labels: label.trim() ? { label: label.trim() } : undefined,
              })
            }
          >
            Run
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-4">
        <Field label="Targets" hint="Several targets produce one run each, ready to compare.">
          <div className="flex flex-col gap-1">
            {(targets.data ?? []).map((t) => (
              <label key={t.id} className="flex items-center gap-2 rounded-sm px-1 py-1 text-sm text-text hover:bg-surface-hover">
                <input type="checkbox" className="accent-[var(--accent)]" checked={ids.includes(t.id)} onChange={(e) => setIds(e.target.checked ? [...ids, t.id] : ids.filter((x) => x !== t.id))} />
                {t.name}
              </label>
            ))}
          </div>
        </Field>
        <div className="grid grid-cols-2 gap-3">
          <Field label="Sample size" optional><Input type="number" min={1} value={sample} onChange={(e) => setSample(e.target.value)} placeholder="All" /></Field>
          <Field label="Trials" optional><Input type="number" min={1} max={10} value={trials} onChange={(e) => setTrials(e.target.value)} /></Field>
        </div>
        <Field label="Label" optional><Input value={label} onChange={(e) => setLabel(e.target.value)} placeholder="e.g. prompt-v7" /></Field>
      </div>
    </Dialog>
  )
}

function Overview({ suite, projectId }: { suite: S['Suite']; projectId?: string }) {
  const ctx = useProjectCtx()
  const evaluators = useEvaluators(projectId)
  const targets = useTargets(projectId)
  const datasets = useDatasets(projectId)
  const runs = useSuiteRuns(projectId, suite.id, 10)
  const cfg = suite.config as Obj
  const tIds = getIn<string[]>(cfg, ['target_ids'], [])
  const dsets = getIn<{ dataset_id: string; version: number | null }[]>(cfg, ['datasets'], [])
  const evs = getIn<S['SuiteEvaluator'][]>(cfg, ['evaluators'], [])
  const sampling = getIn<S['Sampling']>(cfg, ['sampling'], { mode: 'none' })
  const last = runs.data?.items[0]
  const evalName = (id: string) => evaluators.data?.find((e) => e.id === id)

  return (
    <div className="grid grid-cols-1 gap-3 lg:grid-cols-[minmax(0,2fr)_minmax(0,1fr)]">
      <div className="flex min-w-0 flex-col gap-3">
        <Card>
          <CardHeader title="Configuration" />
          <CardBody>
            <KeyValue
              columns={2}
              layout="grid"
              items={[
                { label: 'Targets', value: tIds.map((id) => targets.data?.find((t) => t.id === id)?.name ?? '…').join(', ') || '—' },
                { label: 'Datasets', value: dsets.map((d) => `${datasets.data?.find((x) => x.id === d.dataset_id)?.name ?? '…'} ${d.version ? `v${d.version}` : '(latest)'}`).join(', ') || '—' },
                { label: 'Trials', value: String(getIn<number>(cfg, ['trials'], 1)) },
                { label: 'Sampling', value: sampling.mode === 'none' || !sampling.mode ? 'All examples' : `${sampling.mode}${sampling.n ? ` · ${sampling.n}` : ''}${sampling.percent ? ` · ${sampling.percent}%` : ''}` },
                { label: 'Concurrency', value: String(getIn<number | undefined>(cfg, ['limits', 'max_concurrency']) ?? 'Target default') },
                { label: 'Cache', value: `${getIn<boolean>(cfg, ['cache', 'target'], false) ? 'Target' : 'No target cache'} · ${getIn<boolean>(cfg, ['cache', 'judges'], true) ? 'Judges' : 'No judge cache'}` },
                { label: 'Baseline', value: ({ none: 'None', pinned: 'Pinned run', last_passing: 'Last passing run' } as Record<string, string>)[getIn<string>(cfg, ['baseline', 'policy'], 'last_passing')] ?? '' },
              ]}
            />
          </CardBody>
        </Card>
        <Card>
          <CardHeader title="Evaluators" description={`${evs.length} attached`} />
          {evs.length === 0 ? (
            <EmptyState compact illustration="evaluators" title="No evaluators attached" />
          ) : (
            <div className="overflow-x-auto px-2 pb-2">
              <table className="w-full min-w-[480px] text-sm">
                <thead><tr className="text-left text-xs text-text-muted"><th className="px-2 py-2 font-medium">Evaluator</th><th className="px-2 py-2 font-medium">Threshold</th><th className="px-2 py-2 font-medium">Severity</th><th className="px-2 py-2 font-medium">Weight</th><th className="px-2 py-2 font-medium">Applies to</th></tr></thead>
                <tbody>
                  {evs.map((e) => {
                    const ev = evalName(e.evaluator_id)
                    return (
                      <tr key={e.evaluator_id} className="border-t border-border">
                        <td className="px-2 py-2"><span className="font-medium text-text">{ev?.name ?? '…'}</span> {ev && <Badge variant={ev.category === 'model' ? 'accent' : 'neutral'}>{categoryLabel[ev.category]}</Badge>}</td>
                        <td className="px-2 py-2 tabular text-text-2">≥ {e.threshold ?? '—'}</td>
                        <td className="px-2 py-2 text-text-2">{e.severity ?? 'major'}</td>
                        <td className="px-2 py-2 tabular text-text-2">{e.weight ?? 1}</td>
                        <td className="px-2 py-2 text-text-2">{(e.apply_to ?? 'final').replace('_', ' ')}{e.fail_fast ? ' · fail fast' : ''}</td>
                      </tr>
                    )
                  })}
                </tbody>
              </table>
            </div>
          )}
        </Card>
      </div>
      <Card className="self-start">
        <CardHeader title="Latest run" />
        <CardBody>
          {runs.isLoading ? (
            <Skeleton className="h-20 w-full" />
          ) : !last ? (
            <p className="text-sm text-text-muted">This suite hasn't run yet. Use “Run now”.</p>
          ) : (
            <div className="flex flex-col gap-2 text-sm">
              <div className="flex items-center gap-2"><StatusPill status={runStatus(last.status)} /><RelTime iso={last.created_at} /></div>
              <div className="text-text-2 tabular">{last.completed}/{last.total} done · {last.failed} failed · {last.errored} errored</div>
              <Link to="/o/$org/p/$project/runs/$run" params={{ ...ctx.params, run: last.id }} className="text-accent hover:underline">Open run</Link>
            </div>
          )}
        </CardBody>
      </Card>
    </div>
  )
}

function GatesTab({ suite, onEdit }: { suite: S['Suite']; onEdit: () => void }) {
  const gates = useGates(suite.id)
  return (
    <Card>
      <CardHeader title="Gates" description="A run passes when every enabled gate holds." actions={<Button size="sm" icon={<Pencil />} onClick={onEdit}>Edit gates</Button>} />
      {gates.isLoading ? (
        <CardBody><Skeleton className="h-16 w-full" /></CardBody>
      ) : gates.error ? (
        <ErrorPanel error={gates.error} onRetry={() => void gates.refetch()} />
      ) : (gates.data?.items.length ?? 0) === 0 ? (
        <EmptyState compact illustration="suites" title="No gates" description="Without gates a finished run always counts as passed." action={<Button size="sm" variant="primary" onClick={onEdit}>Add a gate</Button>} />
      ) : (
        <ul className="divide-y divide-border">
          {gates.data!.items.map((g) => (
            <li key={g.id} className="flex items-center gap-3 px-4 py-3">
              {g.enabled ? <CircleCheck className="size-4 text-text-muted" /> : <CircleX className="size-4 text-text-muted" />}
              <div className="min-w-0 flex-1">
                <div className="text-sm font-medium text-text">{g.name || 'Gate'}</div>
                <code className="block truncate font-mono text-xs text-text-2">{g.text}</code>
              </div>
              {!g.enabled && <Badge>Disabled</Badge>}
            </li>
          ))}
        </ul>
      )}
    </Card>
  )
}

function HistoryTab({ suite, projectId }: { suite: S['Suite']; projectId?: string }) {
  const evaluators = useEvaluators(projectId)
  const evs = getIn<S['SuiteEvaluator'][]>(suite.config, ['evaluators'], [])
  const pass = useSuiteTrend(projectId, suite.id, 'pass_rate')
  const means = useSuiteTrend(projectId, suite.id, 'score_mean')
  const lat = useSuiteTrend(projectId, suite.id, 'latency_p95')
  const seriesOf = (d?: S['Trend']) => (d?.series ?? []).map((s) => ({ name: s.label || s.key, points: s.points.map((p) => ({ t: p.t, v: p.value ?? null })) }))
  const names = new Map(evs.map((e) => [e.evaluator_id, evaluators.data?.find((x) => x.id === e.evaluator_id)?.name]))
  const meanSeries = seriesOf(means.data).map((s, i) => ({ ...s, name: names.get(means.data?.series[i]?.evaluator_id ?? '') ?? s.name }))
  const empty = <EmptyState compact illustration="chart" title="No runs yet" description="History appears after the first run." />
  return (
    <div className="flex flex-col gap-3">
      <TrendLine title="Pass rate" series={seriesOf(pass.data)} yDomain={[0, 1]} loading={pass.isLoading} empty={empty} />
      <TrendLine title="Mean score per evaluator" description="Per run" series={meanSeries.slice(0, 6)} format="score" yDomain={[0, 1]} area={false} loading={means.isLoading} empty={empty} />
      <TrendLine title="Latency p95" series={seriesOf(lat.data)} format="ms" loading={lat.isLoading} empty={empty} />
    </div>
  )
}

export function SuiteDetailScreen() {
  const { suite: key } = useParams({ strict: false }) as { suite: string }
  const ctx = useProjectCtx()
  const navigate = useNavigate()
  const [search, patch] = useSearchState()
  const suite = useSuite(ctx.projectId, key)
  const run = useRunSuite()
  const del = useDeleteSuite(ctx.projectId)
  const [options, setOptions] = useState(false)
  const [confirmDelete, setConfirmDelete] = useState(false)
  const tab = str(search.tab, 'overview') as Tab
  const editing = !!search.edit

  if (suite.isLoading || ctx.isLoading) return <Screen><Skeleton className="h-10 w-72" /><Skeleton className="h-96 w-full" /></Screen>
  if (suite.error || !suite.data) return <Screen><Card><ErrorPanel error={suite.error ?? ctx.error ?? new Error('Suite not found')} onRetry={() => void suite.refetch()} /></Card></Screen>
  const s = suite.data
  if (editing) return <SuiteEditor suite={s} initialStep={num(search.step) ?? 0} onClose={() => patch({ edit: null, step: null })} />

  const start = (body: Partial<S['RunCreate']> = {}) =>
    run.mutate(
      { suite_id: s.id, ...body },
      {
        onSuccess: (r) => {
          setOptions(false)
          toast.success(r.runs.length > 1 ? `Started ${r.runs.length} runs` : 'Run started')
          const first = r.runs[0]
          if (first) void navigate({ to: '/o/$org/p/$project/runs/$run', params: { ...ctx.params, run: first.id } })
        },
        onError: (e) => toast.error(errorMessage(e)),
      },
    )

  return (
    <Screen>
      <PageHeader
        icon={<Layers />}
        title={s.name}
        description={s.description || undefined}
        meta={<Badge mono>{s.slug}</Badge>}
        actions={
          <>
            <Button icon={<Pencil />} onClick={() => patch({ edit: 1 })}>Edit</Button>
            <div className="flex">
              <Button variant="primary" icon={<Play />} loading={run.isPending} className="rounded-r-none" onClick={() => start()}>Run now</Button>
              <DropdownMenu>
                <DropdownMenuTrigger asChild>
                  <Button variant="primary" aria-label="More run options" className="rounded-l-none border-l border-on-accent/30 px-1.5"><ChevronDown /></Button>
                </DropdownMenuTrigger>
                <DropdownMenuContent align="end">
                  <DropdownMenuItem onSelect={() => setOptions(true)}>Run with options…</DropdownMenuItem>
                  <DropdownMenuSeparator />
                  <DropdownMenuItem destructive icon={<Trash2 />} onSelect={() => setConfirmDelete(true)}>Delete suite…</DropdownMenuItem>
                </DropdownMenuContent>
              </DropdownMenu>
            </div>
          </>
        }
        tabs={
          <Tabs value={tab} onValueChange={(t) => patch({ tab: t === 'overview' ? null : t })}>
            <TabsList>
              <TabsTrigger value="overview">Overview</TabsTrigger>
              <TabsTrigger value="gates">Gates</TabsTrigger>
              <TabsTrigger value="triggers">Triggers</TabsTrigger>
              <TabsTrigger value="history">History</TabsTrigger>
            </TabsList>
          </Tabs>
        }
      />
      {tab === 'overview' && <Overview suite={s} projectId={ctx.projectId} />}
      {tab === 'gates' && <GatesTab suite={s} onEdit={() => patch({ edit: 1, step: 3 })} />}
      {tab === 'triggers' && <Triggers suiteId={s.id} />}
      {tab === 'history' && <HistoryTab suite={s} projectId={ctx.projectId} />}
      <RunOptions suite={s} open={options} onOpenChange={setOptions} onRun={start} />
      <ConfirmDialog open={confirmDelete} onOpenChange={setConfirmDelete} title={`Delete ${s.name}?`} description="Its gates and triggers are removed. Past runs keep their results." loading={del.isPending} onConfirm={() => del.mutate(s.id, { onSuccess: () => { toast.success('Suite deleted'); void navigate({ to: '/o/$org/p/$project/suites', params: ctx.params }) } })} />
    </Screen>
  )
}
