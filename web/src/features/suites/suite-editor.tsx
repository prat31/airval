import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useNavigate } from '@tanstack/react-router'
import { ArrowLeft, ArrowRight, Check, Crosshair, Database, Gauge, Plus, Save, Trash2 } from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'
import { Screen } from '@/app/screen'
import { Badge } from '@/components/ui/badge'
import { Button, IconButton } from '@/components/ui/button'
import { Card, CardBody, CardHeader } from '@/components/ui/card'
import { Combobox } from '@/components/ui/combobox'
import { EmptyState } from '@/components/ui/empty-state'
import { Field } from '@/components/ui/field'
import { Input, Textarea } from '@/components/ui/input'
import { PageHeader } from '@/components/ui/page-header'
import { SegmentedControl } from '@/components/ui/segmented-control'
import { Select } from '@/components/ui/select'
import { Skeleton } from '@/components/ui/skeleton'
import { Stepper } from '@/components/ui/stepper'
import { Switch } from '@/components/ui/switch'
import { toast } from '@/components/ui/toast'
import { cn } from '@/lib/cn'
import { formatDuration, formatUsd } from '@/lib/format'
import { Layers } from 'lucide-react'
import { useDatasets, useDatasetVersions } from '../datasets/api'
import { categoryLabel, useCatalog, useEvaluators } from '../evaluators/api'
import { errorMessage, http, unwrap, type Obj, type S } from '../shared/api'
import { useProjectCtx } from '../shared/context'
import { getIn } from '../shared/obj'
import { FieldRow, KvEditor, RelTime } from '../shared/ui'
import { kindMeta, targetEndpoint } from '../targets/kinds'
import { useTargets } from '../targets/api'
import { suiteKeys, useGates } from './api'
import { gateExpr, gateFromServer, gateProblem, GatesBuilder, type GateDraft } from './gates-builder'

export const steps = [
  { id: 'target', label: 'Target', description: 'What to evaluate' },
  { id: 'data', label: 'Data', description: 'Dataset versions' },
  { id: 'evaluators', label: 'Evaluators', description: 'How to score' },
  { id: 'gates', label: 'Gates', description: 'Pass criteria' },
  { id: 'execution', label: 'Execution', description: 'Trials, sampling, cache' },
]

interface Draft {
  name: string
  description: string
  targetIds: string[]
  datasets: { dataset_id: string; version: number | null }[]
  evaluators: S['SuiteEvaluator'][]
  gates: GateDraft[]
  trials: number
  samplingMode: 'none' | 'random' | 'stratified'
  samplingN: string
  samplingPercent: string
  maxConcurrency: string
  rpm: string
  cacheTarget: boolean
  cacheJudges: boolean
  baselinePolicy: 'none' | 'pinned' | 'last_passing'
  baselineRun: string
  criticalBudget: string
  timeout: string
  variables: Record<string, string>
}

const emptyDraft = (): Draft => ({
  name: '',
  description: '',
  targetIds: [],
  datasets: [],
  evaluators: [],
  gates: [],
  trials: 1,
  samplingMode: 'none',
  samplingN: '',
  samplingPercent: '',
  maxConcurrency: '',
  rpm: '',
  cacheTarget: false,
  cacheJudges: true,
  baselinePolicy: 'last_passing',
  baselineRun: '',
  criticalBudget: '',
  timeout: '',
  variables: {},
})

function draftFromSuite(s: S['Suite'], gates: S['Gate'][]): Draft {
  const c = s.config as Obj
  const g = <T,>(path: string[], f?: T) => getIn<T>(c, path, f)
  return {
    name: s.name,
    description: s.description,
    targetIds: g<string[]>(['target_ids'], []),
    datasets: g<{ dataset_id: string; version: number | null }[]>(['datasets'], []).map((d) => ({ dataset_id: d.dataset_id, version: d.version ?? null })),
    evaluators: g<S['SuiteEvaluator'][]>(['evaluators'], []),
    gates: gates.map(gateFromServer),
    trials: g<number>(['trials'], 1),
    samplingMode: g<Draft['samplingMode']>(['sampling', 'mode'], 'none'),
    samplingN: String(g<number | undefined>(['sampling', 'n']) ?? ''),
    samplingPercent: String(g<number | undefined>(['sampling', 'percent']) ?? ''),
    maxConcurrency: String(g<number | undefined>(['limits', 'max_concurrency']) ?? ''),
    rpm: String(g<number | undefined>(['limits', 'rpm']) ?? ''),
    cacheTarget: g<boolean>(['cache', 'target'], false),
    cacheJudges: g<boolean>(['cache', 'judges'], true),
    baselinePolicy: g<Draft['baselinePolicy']>(['baseline', 'policy'], 'last_passing'),
    baselineRun: g<string>(['baseline', 'run_id'], ''),
    criticalBudget: String(g<number | undefined>(['failure_budget', 'critical_failures']) ?? ''),
    timeout: String(g<number | undefined>(['timeout_seconds']) ?? ''),
    variables: g<Record<string, string>>(['variables'], {}),
  }
}

function configFromDraft(d: Draft): S['SuiteConfig'] {
  const n = (s: string) => (s.trim() === '' ? undefined : Number(s))
  const cfg: Obj = {
    target_ids: d.targetIds,
    datasets: d.datasets.map((x) => ({ dataset_id: x.dataset_id, version: x.version })),
    evaluators: d.evaluators,
    trials: d.trials,
    cache: { target: d.cacheTarget, judges: d.cacheJudges },
    baseline: d.baselinePolicy === 'pinned' ? { policy: 'pinned', run_id: d.baselineRun } : { policy: d.baselinePolicy },
  }
  if (d.samplingMode !== 'none') cfg.sampling = { mode: d.samplingMode, n: n(d.samplingN), percent: n(d.samplingPercent) }
  const limits = { max_concurrency: n(d.maxConcurrency), rpm: n(d.rpm) }
  if (limits.max_concurrency || limits.rpm) cfg.limits = limits
  if (n(d.criticalBudget)) cfg.failure_budget = { critical_failures: n(d.criticalBudget) }
  if (n(d.timeout)) cfg.timeout_seconds = n(d.timeout)
  if (Object.keys(d.variables).length) cfg.variables = d.variables
  return cfg
}

/* --------------------------------------------------------------------------------------------- steps */

function TargetStep({ d, set }: { d: Draft; set: (p: Partial<Draft>) => void }) {
  const ctx = useProjectCtx()
  const targets = useTargets(ctx.projectId)
  const toggle = (id: string) => set({ targetIds: d.targetIds.includes(id) ? d.targetIds.filter((x) => x !== id) : [...d.targetIds, id] })
  return (
    <div className="flex flex-col gap-4">
      <Card>
        <CardHeader title="Details" />
        <CardBody className="flex flex-col gap-4">
          <Field label="Name">
            <Input value={d.name} onChange={(e) => set({ name: e.target.value })} placeholder="Regression suite" autoFocus />
          </Field>
          <Field label="Description" optional>
            <Textarea rows={2} value={d.description} onChange={(e) => set({ description: e.target.value })} />
          </Field>
        </CardBody>
      </Card>
      <Card>
        <CardHeader title="Targets" description="One run per target. Select several to get an automatic comparison (prod vs staging, model A vs B)." />
        <CardBody>
          {targets.isLoading ? (
            <Skeleton className="h-24 w-full" />
          ) : (targets.data?.length ?? 0) === 0 ? (
            <EmptyState compact illustration="targets" title="No targets yet" description="Connect the AI service you want to evaluate first." action={<Button asChild variant="primary" size="sm"><Link to="/o/$org/p/$project/targets/$target" params={{ ...ctx.params, target: 'new' }}>New target</Link></Button>} />
          ) : (
            <ul className="grid grid-cols-1 gap-2 sm:grid-cols-2">
              {targets.data!.map((t) => {
                const on = d.targetIds.includes(t.id)
                const M = kindMeta[t.kind]
                return (
                  <li key={t.id}>
                    <button
                      type="button"
                      aria-pressed={on}
                      onClick={() => toggle(t.id)}
                      className={cn('flex w-full items-center gap-3 rounded-md border p-3 text-left transition-colors focus-ring', on ? 'border-accent bg-accent-subtle' : 'border-border hover:border-border-strong hover:bg-surface-hover')}
                    >
                      <span className="flex size-8 shrink-0 items-center justify-center rounded-md border border-border bg-surface text-text-2 [&_svg]:size-4"><M.icon /></span>
                      <span className="min-w-0 flex-1">
                        <span className="block truncate text-sm font-medium text-text">{t.name}</span>
                        <span className="block truncate font-mono text-[11px] text-text-muted">{targetEndpoint(t.kind, t.current_version?.config as Obj) || M.label}</span>
                      </span>
                      <span className={cn('flex size-5 shrink-0 items-center justify-center rounded-sm border', on ? 'border-accent bg-accent text-on-accent' : 'border-border-strong')}>{on && <Check className="size-3.5" />}</span>
                    </button>
                  </li>
                )
              })}
            </ul>
          )}
        </CardBody>
      </Card>
    </div>
  )
}

function DatasetRow({ row, taken, onChange, onRemove }: { row: Draft['datasets'][number]; taken: string[]; onChange: (r: Draft['datasets'][number]) => void; onRemove: () => void }) {
  const ctx = useProjectCtx()
  const datasets = useDatasets(ctx.projectId)
  const versions = useDatasetVersions(row.dataset_id || undefined)
  const unpublished = !!row.dataset_id && versions.data && versions.data.length === 0
  return (
    <div className="flex flex-col gap-1.5">
      <div className="grid grid-cols-[minmax(0,1fr)_minmax(0,200px)_auto] items-center gap-2">
        <Combobox
          aria-label="Dataset"
          options={(datasets.data ?? []).filter((x) => x.id === row.dataset_id || !taken.includes(x.id)).map((x) => ({ value: x.id, label: x.name, description: `${x.example_count} examples` }))}
          value={row.dataset_id || null}
          onValueChange={(v) => onChange({ dataset_id: v ?? '', version: null })}
          placeholder="Choose a dataset"
        />
        <Select
          aria-label="Version"
          value={row.version === null ? 'latest' : String(row.version)}
          onValueChange={(v) => onChange({ ...row, version: v === 'latest' ? null : Number(v) })}
          options={[{ value: 'latest', label: 'Latest published' }, ...(versions.data ?? []).map((v) => ({ value: String(v.number), label: `v${v.number} · ${v.example_count} ex.` }))]}
        />
        <IconButton label="Remove dataset" onClick={onRemove}>
          <Trash2 />
        </IconButton>
      </div>
      {unpublished && <p className="text-xs text-warn">No published version yet. Publish one from the dataset page so runs have something to pin.</p>}
    </div>
  )
}

function DataStep({ d, set }: { d: Draft; set: (p: Partial<Draft>) => void }) {
  const ctx = useProjectCtx()
  return (
    <Card>
      <CardHeader title="Datasets" description="Runs use immutable dataset versions. Pin one, or follow the latest published." />
      <CardBody className="flex flex-col gap-3">
        {d.datasets.length === 0 ? (
          <EmptyState compact illustration="datasets" title="Add a dataset" description="Pick the examples this suite scores." action={<Button size="sm" variant="primary" icon={<Plus />} onClick={() => set({ datasets: [{ dataset_id: '', version: null }] })}>Add dataset</Button>} secondaryAction={<Button size="sm" variant="ghost" asChild><Link to="/o/$org/p/$project/datasets" params={ctx.params}>Manage datasets</Link></Button>} />
        ) : (
          <>
            {d.datasets.map((row, i) => (
              <DatasetRow key={i} row={row} taken={d.datasets.map((x) => x.dataset_id)} onChange={(r) => set({ datasets: d.datasets.map((x, j) => (j === i ? r : x)) })} onRemove={() => set({ datasets: d.datasets.filter((_, j) => j !== i) })} />
            ))}
            <div>
              <Button size="sm" variant="ghost" icon={<Plus />} onClick={() => set({ datasets: [...d.datasets, { dataset_id: '', version: null }] })}>
                Add another dataset
              </Button>
            </div>
          </>
        )}
      </CardBody>
    </Card>
  )
}

const severities: { value: S['Severity']; label: string }[] = [
  { value: 'critical', label: 'Critical' },
  { value: 'major', label: 'Major' },
  { value: 'minor', label: 'Minor' },
]
const applyTos: { value: S['ApplyTo']; label: string }[] = [
  { value: 'final', label: 'Final output' },
  { value: 'per_turn', label: 'Each turn' },
  { value: 'conversation', label: 'Conversation' },
  { value: 'trace', label: 'Trace' },
]

function EvaluatorsStep({ d, set }: { d: Draft; set: (p: Partial<Draft>) => void }) {
  const ctx = useProjectCtx()
  const evaluators = useEvaluators(ctx.projectId)
  const catalog = useCatalog()
  const byId = new Map((evaluators.data ?? []).map((e) => [e.id, e]))
  const available = (evaluators.data ?? []).filter((e) => !d.evaluators.some((x) => x.evaluator_id === e.id))
  const update = (i: number, p: Partial<S['SuiteEvaluator']>) => set({ evaluators: d.evaluators.map((x, j) => (j === i ? { ...x, ...p } : x)) })
  return (
    <Card>
      <CardHeader title="Evaluators" description="Each scores every result. Threshold decides pass/fail; severity feeds gates like “no critical failures”." />
      <CardBody className="flex flex-col gap-3">
        {evaluators.isLoading ? (
          <Skeleton className="h-24 w-full" />
        ) : (evaluators.data?.length ?? 0) === 0 ? (
          <EmptyState compact illustration="evaluators" title="No evaluators yet" description="Create one from a preset such as correctness, faithfulness or JSON schema." action={<Button asChild variant="primary" size="sm"><Link to="/o/$org/p/$project/evaluators" params={ctx.params} search={{ tab: 'catalog' } as never}>Browse the catalog</Link></Button>} />
        ) : (
          <>
            {d.evaluators.length > 0 && (
              <ul className="flex flex-col gap-2">
                {d.evaluators.map((se, i) => {
                  const ev = byId.get(se.evaluator_id)
                  const info = catalog.data?.items.find((k) => k.kind === ev?.kind)
                  return (
                    <li key={se.evaluator_id} className="rounded-md border border-border p-3">
                      <div className="mb-3 flex items-center gap-2">
                        <Gauge className="size-4 text-text-muted" />
                        <span className="text-sm font-medium text-text">{ev?.name ?? 'Unknown evaluator'}</span>
                        {ev && <Badge variant={ev.category === 'model' ? 'accent' : 'neutral'}>{categoryLabel[ev.category]}</Badge>}
                        <IconButton className="ml-auto" label={`Remove ${ev?.name ?? 'evaluator'}`} size="sm" onClick={() => set({ evaluators: d.evaluators.filter((_, j) => j !== i) })}>
                          <Trash2 />
                        </IconButton>
                      </div>
                      <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-6">
                        <Field label="Threshold">
                          <Input type="number" min={0} max={1} step={0.05} value={se.threshold === undefined ? '' : String(se.threshold)} placeholder={info?.default_threshold !== undefined ? String(info.default_threshold) : '0.5'} onChange={(e) => update(i, { threshold: e.target.value === '' ? undefined : Number(e.target.value) })} />
                        </Field>
                        <Field label="Severity">
                          <Select value={se.severity ?? 'major'} onValueChange={(v) => update(i, { severity: v as S['Severity'] })} options={severities} />
                        </Field>
                        <Field label="Weight">
                          <Input type="number" min={0} step={0.5} value={String(se.weight ?? 1)} onChange={(e) => update(i, { weight: e.target.value === '' ? undefined : Number(e.target.value) })} />
                        </Field>
                        <Field label="Applies to" className="col-span-2 sm:col-span-1 lg:col-span-2">
                          <Select value={se.apply_to ?? 'final'} onValueChange={(v) => update(i, { apply_to: v as S['ApplyTo'] })} options={applyTos} />
                        </Field>
                        <div className="flex items-end justify-between gap-2 pb-1.5">
                          <label className="flex items-center gap-2 text-sm text-text-2">
                            Fail fast
                            <Switch size="sm" aria-label="Fail fast" checked={!!se.fail_fast} onCheckedChange={(v) => update(i, { fail_fast: v || undefined })} />
                          </label>
                        </div>
                      </div>
                    </li>
                  )
                })}
              </ul>
            )}
            <Combobox
              aria-label="Add evaluator"
              options={available.map((e) => ({ value: e.id, label: e.name, description: `${categoryLabel[e.category]} · ${e.kind.replace(/_/g, ' ')}` }))}
              value={null}
              onValueChange={(id) => {
                if (!id) return
                const ev = byId.get(id)
                const info = catalog.data?.items.find((k) => k.kind === ev?.kind)
                set({ evaluators: [...d.evaluators, { evaluator_id: id, weight: 1, threshold: info?.default_threshold ?? 0.5, severity: 'major', apply_to: 'final' }] })
              }}
              placeholder={available.length ? 'Add an evaluator…' : 'All evaluators added'}
              disabled={available.length === 0}
              emptyText="Nothing left to add"
            />
          </>
        )}
      </CardBody>
    </Card>
  )
}

function ExecutionStep({ d, set }: { d: Draft; set: (p: Partial<Draft>) => void }) {
  return (
    <div className="flex flex-col gap-4">
      <Card>
        <CardHeader title="Trials & sampling" description="Repeat examples to measure nondeterminism, or run a representative slice for quick feedback." />
        <CardBody className="flex flex-col gap-4">
          <Field label="Trials per example" hint="Scores are aggregated across trials; flaky examples are flagged.">
            <SegmentedControl aria-label="Trials" value={String(d.trials)} onValueChange={(v) => set({ trials: Number(v) })} options={[1, 2, 3, 5, 10].map((n) => ({ value: String(n), label: String(n) }))} />
          </Field>
          <FieldRow cols={3}>
            <Field label="Sampling">
              <Select
                value={d.samplingMode}
                onValueChange={(v) => set({ samplingMode: v as Draft['samplingMode'] })}
                options={[
                  { value: 'none', label: 'All examples' },
                  { value: 'random', label: 'Random sample' },
                  { value: 'stratified', label: 'Stratified by tag' },
                ]}
              />
            </Field>
            {d.samplingMode !== 'none' && (
              <>
                <Field label="Examples (n)" optional>
                  <Input type="number" min={1} value={d.samplingN} onChange={(e) => set({ samplingN: e.target.value })} />
                </Field>
                <Field label="Or percent" optional>
                  <Input type="number" min={1} max={100} value={d.samplingPercent} onChange={(e) => set({ samplingPercent: e.target.value })} />
                </Field>
              </>
            )}
          </FieldRow>
        </CardBody>
      </Card>
      <Card>
        <CardHeader title="Concurrency & caching" description="Overrides for this suite. Leave empty to use the target's limits." />
        <CardBody className="flex flex-col gap-4">
          <FieldRow>
            <Field label="Max concurrency" optional>
              <Input type="number" min={1} value={d.maxConcurrency} placeholder="target default" onChange={(e) => set({ maxConcurrency: e.target.value })} />
            </Field>
            <Field label="Requests / min" optional>
              <Input type="number" min={0} value={d.rpm} placeholder="target default" onChange={(e) => set({ rpm: e.target.value })} />
            </Field>
          </FieldRow>
          <div className="flex items-start justify-between gap-4">
            <div>
              <div className="text-sm font-medium text-text">Cache target responses</div>
              <p className="mt-0.5 text-xs text-text-muted">Reuse outputs for identical requests. Turn off to measure real latency.</p>
            </div>
            <Switch aria-label="Cache target responses" checked={d.cacheTarget} onCheckedChange={(v) => set({ cacheTarget: v })} />
          </div>
          <div className="flex items-start justify-between gap-4">
            <div>
              <div className="text-sm font-medium text-text">Cache judge verdicts</div>
              <p className="mt-0.5 text-xs text-text-muted">Skip repeat LLM judge calls for unchanged output and rubric.</p>
            </div>
            <Switch aria-label="Cache judge verdicts" checked={d.cacheJudges} onCheckedChange={(v) => set({ cacheJudges: v })} />
          </div>
        </CardBody>
      </Card>
      <Card>
        <CardHeader title="Baseline & safety" />
        <CardBody className="flex flex-col gap-4">
          <FieldRow>
            <Field label="Compare new runs against">
              <Select
                value={d.baselinePolicy}
                onValueChange={(v) => set({ baselinePolicy: v as Draft['baselinePolicy'] })}
                options={[
                  { value: 'last_passing', label: 'Last passing run' },
                  { value: 'pinned', label: 'A pinned run' },
                  { value: 'none', label: 'Nothing' },
                ]}
              />
            </Field>
            {d.baselinePolicy === 'pinned' && (
              <Field label="Run ID">
                <Input className="font-mono text-xs" value={d.baselineRun} onChange={(e) => set({ baselineRun: e.target.value })} placeholder="Paste a run ID" />
              </Field>
            )}
          </FieldRow>
          <FieldRow>
            <Field label="Stop after N critical failures" optional hint="Cancels the run early and saves spend.">
              <Input type="number" min={1} value={d.criticalBudget} onChange={(e) => set({ criticalBudget: e.target.value })} />
            </Field>
            <Field label="Run timeout (seconds)" optional>
              <Input type="number" min={1} value={d.timeout} onChange={(e) => set({ timeout: e.target.value })} />
            </Field>
          </FieldRow>
          <Field label="Variables" optional hint="Override the target's {{ .vars.* }} for every run of this suite.">
            <KvEditor value={d.variables} onChange={(v) => set({ variables: v })} keyPlaceholder="name" valuePlaceholder="value" addLabel="Add variable" monoValue />
          </Field>
        </CardBody>
      </Card>
    </div>
  )
}

/* ---------------------------------------------------------------------------------- summary panel */

function SummaryPanel({ d, suiteId }: { d: Draft; suiteId?: string }) {
  const ctx = useProjectCtx()
  const datasets = useDatasets(ctx.projectId)
  const evaluators = useEvaluators(ctx.projectId)
  const targets = useTargets(ctx.projectId)

  // Historical run stats: this suite's runs when editing, otherwise the first target's.
  const history = useQuery({
    queryKey: ['estimate-runs', ctx.projectId, suiteId, d.targetIds[0]],
    enabled: !!ctx.projectId && (!!suiteId || !!d.targetIds[0]),
    queryFn: () => unwrap(http.GET('/v1/runs', { params: { query: { project_id: ctx.projectId!, suite_id: suiteId, target_id: suiteId ? undefined : d.targetIds[0], limit: 5 } } })),
  })

  // Examples per dataset: versions carry counts; "latest" falls back to the dataset's latest version / draft count.
  const versionCounts = useQuery({
    queryKey: ['estimate-counts', d.datasets.map((x) => `${x.dataset_id}:${x.version}`).join(',')],
    enabled: d.datasets.some((x) => x.dataset_id),
    queryFn: async () => {
      const out: Record<string, number> = {}
      await Promise.all(
        d.datasets.filter((x) => x.dataset_id && x.version !== null).map(async (x) => {
          const v = await unwrap(http.GET('/v1/datasets/{dataset}/versions/{number}', { params: { path: { dataset: x.dataset_id, number: x.version! } } }))
          out[`${x.dataset_id}:${x.version}`] = v.example_count
        }),
      )
      return out
    },
  })

  const examples = d.datasets.reduce((sum, x) => {
    const ds = datasets.data?.find((y) => y.id === x.dataset_id)
    const n = x.version !== null ? (versionCounts.data?.[`${x.dataset_id}:${x.version}`] ?? 0) : (ds?.latest_version ? (ds.example_count ?? 0) : 0)
    return sum + n
  }, 0)
  const sampled = d.samplingMode === 'none' ? examples : d.samplingN ? Math.min(examples, Number(d.samplingN)) : d.samplingPercent ? Math.ceil((examples * Number(d.samplingPercent)) / 100) : examples
  const targetCount = Math.max(1, d.targetIds.length)
  const calls = sampled * d.trials * targetCount
  const modelEvals = d.evaluators.filter((e) => evaluators.data?.find((x) => x.id === e.evaluator_id)?.category === 'model').length
  const judgeCalls = calls * modelEvals

  const runs = (history.data?.items ?? []).filter((r) => r.total > 0 && (r.status === 'succeeded' || r.status === 'failed'))
  const lastSummary = (runs[0] as unknown as { summary?: Obj } | undefined)?.summary
  const p50 = typeof lastSummary?.latency_p50_ms === 'number' ? (lastSummary.latency_p50_ms as number) : undefined
  const costPerCall = runs.length ? runs.reduce((a, r) => a + r.cost_usd, 0) / Math.max(1, runs.reduce((a, r) => a + r.total, 0)) : undefined
  const hit = typeof lastSummary?.cache_hit_ratio === 'number' ? (lastSummary.cache_hit_ratio as number) : 0
  const conc = Number(d.maxConcurrency) || 16
  const effective = d.cacheTarget ? calls * (1 - hit) : calls
  const latency = p50 ?? 800
  const durationMs = calls > 0 ? ((effective * latency) / conc) * (1 + (modelEvals ? 0.6 : 0.05)) : 0
  const cost = costPerCall !== undefined ? costPerCall * effective : undefined

  const rows: { label: string; value: React.ReactNode }[] = [
    { label: 'Targets', value: d.targetIds.length ? d.targetIds.map((id) => targets.data?.find((t) => t.id === id)?.name ?? '…').join(', ') : '—' },
    { label: 'Examples', value: examples ? `${sampled.toLocaleString()}${sampled !== examples ? ` of ${examples.toLocaleString()}` : ''}` : '—' },
    { label: 'Trials', value: String(d.trials) },
    { label: 'Evaluators', value: d.evaluators.length ? `${d.evaluators.length}${modelEvals ? ` (${modelEvals} LLM)` : ''}` : '—' },
    { label: 'Gates', value: String(d.gates.filter((g) => g.enabled).length) },
  ]
  return (
    <Card className="lg:sticky lg:top-4">
      <CardHeader title="Summary" description="Estimate for one run" />
      <CardBody className="flex flex-col gap-4">
        <dl className="flex flex-col gap-2 text-sm">
          {rows.map((r) => (
            <div key={r.label} className="flex items-baseline justify-between gap-3">
              <dt className="text-text-muted">{r.label}</dt>
              <dd className="min-w-0 truncate text-right font-medium text-text">{r.value}</dd>
            </div>
          ))}
        </dl>
        <div className="grid grid-cols-2 gap-3 border-t border-border pt-4">
          <div>
            <div className="text-xs text-text-muted">Duration</div>
            <div className="text-lg font-semibold tabular text-text">{calls ? `~${formatDuration(durationMs)}` : '—'}</div>
          </div>
          <div>
            <div className="text-xs text-text-muted">Cost</div>
            <div className="text-lg font-semibold tabular text-text">{cost !== undefined && calls ? `~${formatUsd(cost)}` : '—'}</div>
          </div>
        </div>
        <p className="text-xs text-text-muted">
          {calls ? `${calls.toLocaleString()} target calls${judgeCalls ? ` + ${judgeCalls.toLocaleString()} judge calls` : ''}. ` : ''}
          {runs.length ? `Based on your last ${runs.length} run${runs.length === 1 ? '' : 's'} (p50 ${formatDuration(latency)}, concurrency ${conc}).` : `No history yet: assuming ${formatDuration(latency)} per call at concurrency ${conc}. Cost appears after the first run.`}
        </p>
        {runs[0] && <p className="text-xs text-text-muted">Last run <RelTime iso={runs[0].created_at} className="text-xs text-text-muted" /></p>}
      </CardBody>
    </Card>
  )
}

/* ------------------------------------------------------------------------------------ the editor */

export function SuiteEditor({ suite, initialStep = 0, onClose }: { suite?: S['Suite']; initialStep?: number; onClose?: () => void }) {
  const ctx = useProjectCtx()
  const navigate = useNavigate()
  const qc = useQueryClient()
  const gatesQ = useGates(suite?.id)
  const evaluators = useEvaluators(ctx.projectId)
  const [step, setStep] = useState(initialStep)
  const [d, setD] = useState<Draft>(() => (suite ? draftFromSuite(suite, []) : emptyDraft()))
  const [seeded, setSeeded] = useState(!suite)
  const [saving, setSaving] = useState(false)

  // Gates load separately for an existing suite.
  useEffect(() => {
    if (suite && gatesQ.data && !seeded) {
      setD(draftFromSuite(suite, gatesQ.data.items))
      setSeeded(true)
    }
  }, [suite, gatesQ.data, seeded])

  const set = (p: Partial<Draft>) => setD((x) => ({ ...x, ...p }))
  const attached = d.evaluators.map((se) => ({ id: se.evaluator_id, name: evaluators.data?.find((e) => e.id === se.evaluator_id)?.name ?? 'evaluator' }))

  const problems = useMemo(() => {
    const p: (string | null)[] = [
      !d.name.trim() ? 'Name the suite' : d.targetIds.length === 0 ? 'Select at least one target' : null,
      d.datasets.length === 0 || d.datasets.some((x) => !x.dataset_id) ? 'Choose at least one dataset' : null,
      d.evaluators.length === 0 ? 'Add at least one evaluator' : null,
      d.gates.map(gateProblem).find(Boolean) ?? null,
      d.baselinePolicy === 'pinned' && !d.baselineRun.trim() ? 'Provide the baseline run ID' : null,
    ]
    return p
  }, [d])
  const stepsView = steps.map((s, i) => ({ ...s, invalid: i < step && !!problems[i] }))
  const allValid = problems.every((x) => !x)
  const isLast = step === steps.length - 1

  const save = async () => {
    if (!allValid) {
      const first = problems.findIndex(Boolean)
      setStep(first)
      return toast.error(problems[first]!)
    }
    setSaving(true)
    try {
      const config = configFromDraft(d)
      let saved: S['Suite']
      if (suite) saved = await unwrap(http.PATCH('/v1/suites/{suite}', { params: { path: { suite: suite.id } }, body: { name: d.name.trim(), description: d.description, config } }))
      else saved = await unwrap(http.POST('/v1/projects/{project}/suites', { params: { path: { project: ctx.projectId! } }, body: { name: d.name.trim(), description: d.description, config } }))

      // Reconcile gates.
      const existing = gatesQ.data?.items ?? []
      const keep = new Set(d.gates.filter((g) => g.id).map((g) => g.id))
      await Promise.all([
        ...existing.filter((g) => !keep.has(g.id)).map((g) => unwrap(http.DELETE('/v1/suites/{suite}/gates/{gate}', { params: { path: { suite: saved.id, gate: g.id } } }))),
        ...d.gates.map((g) => {
          const expr = gateExpr(g)!
          const body = { name: g.name.trim() || undefined, expr, enabled: g.enabled }
          return g.id
            ? unwrap(http.PATCH('/v1/suites/{suite}/gates/{gate}', { params: { path: { suite: saved.id, gate: g.id } }, body }))
            : unwrap(http.POST('/v1/suites/{suite}/gates', { params: { path: { suite: saved.id } }, body }))
        }),
      ])
      void qc.invalidateQueries({ queryKey: ['suites'] })
      void qc.invalidateQueries({ queryKey: ['suite'] })
      void qc.invalidateQueries({ queryKey: suiteKeys.gates(saved.id) })
      toast.success(suite ? 'Suite saved' : 'Suite created')
      void navigate({ to: '/o/$org/p/$project/suites/$suite', params: { ...ctx.params, suite: saved.slug }, search: {} as never })
    } catch (e) {
      toast.error(errorMessage(e))
    } finally {
      setSaving(false)
    }
  }

  if (suite && !seeded) {
    return (
      <Screen>
        <Skeleton className="h-10 w-72" />
        <Skeleton className="h-96 w-full" />
      </Screen>
    )
  }

  const stepIcons = [Crosshair, Database, Gauge, Check, Layers]
  const StepIcon = stepIcons[step]!
  return (
    <Screen>
      <PageHeader icon={<Layers />} title={suite ? `Edit ${suite.name}` : 'New suite'} description="Define what to run, how to score it and when it passes." actions={onClose && <Button variant="ghost" onClick={onClose}>Cancel</Button>} />
      <Card className="px-4 py-3">
        <Stepper steps={stepsView} current={step} onStepClick={setStep} />
      </Card>
      <div className="grid grid-cols-1 gap-4 lg:grid-cols-[minmax(0,1fr)_300px]">
        <div className="flex min-w-0 flex-col gap-4">
          <h2 className="flex items-center gap-2 text-md font-semibold text-text">
            <StepIcon className="size-4 text-text-muted" /> {steps[step]!.label}
            <span className="text-sm font-normal text-text-muted">{steps[step]!.description}</span>
          </h2>
          {step === 0 && <TargetStep d={d} set={set} />}
          {step === 1 && <DataStep d={d} set={set} />}
          {step === 2 && <EvaluatorsStep d={d} set={set} />}
          {step === 3 && (
            attached.length === 0 ? (
              <Card><EmptyState compact illustration="evaluators" title="Add evaluators first" description="Gates are written against evaluator scores." action={<Button size="sm" variant="primary" onClick={() => setStep(2)}>Go to evaluators</Button>} /></Card>
            ) : (
              <GatesBuilder gates={d.gates} onChange={(gates) => set({ gates })} evaluators={attached} />
            )
          )}
          {step === 4 && <ExecutionStep d={d} set={set} />}
          <div className="flex items-center justify-between gap-2 border-t border-border pt-4">
            <Button variant="ghost" icon={<ArrowLeft />} disabled={step === 0} onClick={() => setStep(step - 1)}>
              Back
            </Button>
            <div className="flex items-center gap-2">
              {!isLast && (
                <Button variant={suite ? 'secondary' : 'primary'} iconRight={<ArrowRight />} disabled={!!problems[step] && step < 3} onClick={() => setStep(step + 1)}>
                  Next
                </Button>
              )}
              {(isLast || suite) && (
                <Button variant="primary" icon={<Save />} loading={saving} onClick={() => void save()}>
                  {suite ? 'Save suite' : 'Create suite'}
                </Button>
              )}
            </div>
          </div>
        </div>
        <SummaryPanel d={d} suiteId={suite?.id} />
      </div>
    </Screen>
  )
}
