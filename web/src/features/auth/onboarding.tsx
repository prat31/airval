/**
 * First-run wizard (plan §11.2 #1): project → target (live test connection) → dataset (import or
 * starter examples) → evaluator presets → first run. The sample project is one click away on the
 * first step. Progress lives in the URL (`?org&project&step`), so a reload resumes where you were.
 */
import { useQuery } from '@tanstack/react-query'
import { Navigate, useNavigate, useSearch } from '@tanstack/react-router'
import { ArrowLeft, ArrowRight, Check, Database, FileUp, Play, Sparkles, Wand2 } from 'lucide-react'
import { useMemo, useState, type ReactNode } from 'react'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Field } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Select } from '@/components/ui/select'
import { Skeleton } from '@/components/ui/skeleton'
import { Stepper } from '@/components/ui/stepper'
import { toast } from '@/components/ui/toast'
import { cn } from '@/lib/cn'
import { http, errorMessage, unwrap, type Obj, type S } from '../shared/api'
import { useCreateDataset, useExamples } from '../datasets/api'
import { ImportWizard } from '../datasets/import-wizard'
import { useSaveEvaluator } from '../evaluators/api'
import { useRunSuite } from '../suites/api'
import { useCreateTarget } from '../targets/api'
import { defaultConfig, kindMeta, validateTarget, type TargetKind } from '../targets/kinds'
import { Playground } from '../targets/playground'
import { TargetForm } from '../targets/target-form'
import { isUnauthorized, meQuery, projectsQuery, type ApiProject } from './api'
import { SampleProjectCard, useSampleAvailable } from './sample-project'
import { ProjectForm } from './workspace-dialogs'

export interface OnboardingSearch {
  org?: string
  project?: string
  step?: number
}

export function validateOnboardingSearch(s: Record<string, unknown>): OnboardingSearch {
  const out: OnboardingSearch = {}
  if (typeof s.org === 'string') out.org = s.org
  if (typeof s.project === 'string') out.project = s.project
  const step = Number(s.step)
  if (Number.isInteger(step) && step >= 0 && step <= 4) out.step = step
  return out
}

const steps = [
  { id: 'project', label: 'Project' },
  { id: 'target', label: 'Target' },
  { id: 'dataset', label: 'Dataset' },
  { id: 'evaluators', label: 'Evaluators' },
  { id: 'run', label: 'First run' },
]

/** What the wizard has created so far (kept in memory; the project itself is in the URL). */
interface Draft {
  target?: S['Target']
  dataset?: S['Dataset']
  hasExpected?: boolean
  evaluators?: { evaluator: S['Evaluator']; threshold?: number }[]
}

/**
 * "Skip setup": leave the wizard from any step. Opens the project being set up (or the org's first);
 * with none, the organization home, which handles zero projects. Nothing is created on the user's behalf.
 */
function useSkipSetup(org: { slug: string } | undefined, existing: ApiProject | undefined) {
  const navigate = useNavigate()
  return () => {
    if (!org) return
    if (existing) void navigate({ to: '/o/$org/p/$project/overview', params: { org: org.slug, project: existing.slug }, replace: true })
    else void navigate({ to: '/o/$org', params: { org: org.slug }, replace: true })
  }
}

export function OnboardingScreen() {
  const search = useSearch({ strict: false }) as OnboardingSearch
  const navigate = useNavigate()
  const me = useQuery(meQuery)
  const org = me.data?.orgs.find((o) => o.slug === search.org) ?? me.data?.orgs[0]
  const projects = useQuery({ ...projectsQuery(org?.id ?? ''), enabled: !!org })
  const project = projects.data?.find((p) => p.slug === search.project)
  const [draft, setDraft] = useState<Draft>({})
  const skip = useSkipSetup(org, project ?? projects.data?.[0])

  if (isUnauthorized(me.error)) return <Navigate to="/login" search={{ next: '/onboarding' }} replace />
  if (me.data && !org) return <Navigate to="/" replace />

  const step = project ? Math.max(1, search.step ?? 1) : 0
  const go = (next: number, p: ApiProject | undefined = project) =>
    void navigate({ to: '/onboarding', search: { org: org?.slug, project: p?.slug, step: next }, replace: next === 0 })
  const finish = () => project && org && void navigate({ to: '/o/$org/p/$project/overview', params: { org: org.slug, project: project.slug } })

  const loading = !org || (!!search.project && projects.isLoading)

  return (
    <div className="mx-auto flex w-full max-w-3xl flex-col gap-6">
      <div className="text-center">
        <h1 className="text-xl font-semibold tracking-tight text-text">Let’s evaluate your AI service</h1>
        <p className="mt-1 text-sm text-text-2">
          {org ? (
            <>
              Set up a project in <span className="font-medium text-text">{org.name}</span>. It takes about three minutes.
            </>
          ) : (
            'Set up a project. It takes about three minutes.'
          )}
        </p>
      </div>
      <div className="rounded-lg border border-border bg-surface shadow-popover">
        <div className="overflow-x-auto border-b border-border px-5 py-4">
          <Stepper steps={steps} current={step} onStepClick={project ? (i) => i > 0 && go(i) : undefined} />
        </div>
        {loading ? (
          <div className="flex flex-col gap-4 p-5">
            <Skeleton className="h-8 w-full" />
            <Skeleton className="h-8 w-full" />
            <Skeleton className="h-16 w-full" />
          </div>
        ) : step === 0 || !project ? (
          <div className="flex flex-col gap-5 p-5">
            <SampleProjectCard orgId={org.id} orgSlug={org.slug} />
            <div>
              <h2 className="text-md font-semibold text-text">Name your project</h2>
              <p className="mb-5 mt-0.5 text-sm text-text-2">A project is one AI service under evaluation. It owns targets, datasets, suites and runs.</p>
              <ProjectForm orgId={org.id} onCreated={(p) => go(1, p)} submitLabel="Create project" />
            </div>
          </div>
        ) : step === 1 ? (
          <TargetStep project={project} onBack={() => go(0)} onDone={(target) => (setDraft((d) => ({ ...d, target })), go(2))} onSkip={finish} />
        ) : step === 2 ? (
          <DatasetStep project={project} onBack={() => go(1)} onDone={(dataset, hasExpected) => (setDraft((d) => ({ ...d, dataset, hasExpected })), go(3))} onSkip={finish} />
        ) : step === 3 ? (
          <EvaluatorStep project={project} hasExpected={draft.hasExpected ?? true} onBack={() => go(2)} onDone={(evaluators) => (setDraft((d) => ({ ...d, evaluators })), go(4))} onSkip={finish} />
        ) : (
          <RunStep project={project} orgSlug={org.slug} draft={draft} onBack={() => go(3)} onRestart={() => go(1)} />
        )}
      </div>
      {org && projects.data && (
        <p className="text-center text-xs text-text-muted">
          Prefer to explore first?{' '}
          <button type="button" onClick={skip} className="rounded-sm font-medium text-accent hover:underline focus-ring">
            Skip setup
          </button>
        </p>
      )}
    </div>
  )
}

function StepBody({ title, description, children, footer }: { title: string; description: ReactNode; children: ReactNode; footer: ReactNode }) {
  return (
    <div className="flex flex-col">
      <div className="flex flex-col gap-5 p-5">
        <div>
          <h2 className="text-md font-semibold text-text">{title}</h2>
          <p className="mt-0.5 text-sm text-text-2">{description}</p>
        </div>
        {children}
      </div>
      <div className="flex flex-wrap items-center justify-between gap-2 border-t border-border px-5 py-3">{footer}</div>
    </div>
  )
}

function Back({ onClick }: { onClick: () => void }) {
  return (
    <Button variant="ghost" icon={<ArrowLeft />} onClick={onClick}>
      Back
    </Button>
  )
}

// ---------------------------------------------------------------- target

const wizardKinds: TargetKind[] = ['http', 'openai', 'anthropic', 'agent']

function TargetStep({ project, onBack, onDone, onSkip }: { project: ApiProject; onBack: () => void; onDone: (t: S['Target']) => void; onSkip: () => void }) {
  const mockAvailable = useSampleAvailable()
  const [kind, setKind] = useState<TargetKind>('http')
  const [name, setName] = useState('Production')
  const [config, setConfig] = useState<Obj>(() => defaultConfig('http'))
  const [touched, setTouched] = useState(false)
  const errors = touched ? validateTarget(kind, config) : {}
  const create = useCreateTarget(project.id)

  const useMock = () => {
    setKind('http')
    setName('Built-in mock')
    setConfig({
      ...defaultConfig('http'),
      url: `${window.location.origin}/_mock/rag`,
      response: { output: '$.answer', context: '$.context[*].text', input_tokens: '$.usage.input_tokens', output_tokens: '$.usage.output_tokens', cost_usd: '$.cost_usd' },
    })
  }
  const save = () => {
    setTouched(true)
    if (!name.trim() || Object.keys(validateTarget(kind, config)).length) return
    create.mutate(
      { name: name.trim(), kind, config },
      { onSuccess: onDone, onError: (err) => toast.error('Couldn’t save the target', { description: errorMessage(err) }) },
    )
  }

  return (
    <StepBody
      title="Connect a target"
      description="The AI service to evaluate: an HTTP endpoint or a provider model. Test the connection before saving."
      footer={
        <>
          <Back onClick={onBack} />
          <div className="flex gap-2">
            <Button variant="ghost" onClick={onSkip}>
              Finish later
            </Button>
            <Button variant="primary" iconRight={<ArrowRight />} loading={create.isPending} onClick={save}>
              Save target
            </Button>
          </div>
        </>
      }
    >
      {mockAvailable && (
        <button
          type="button"
          onClick={useMock}
          className="flex items-center gap-3 rounded-md border border-dashed border-border-strong px-3 py-2.5 text-left text-sm text-text-2 transition-colors hover:border-accent hover:text-text focus-ring"
        >
          <Wand2 className="size-4 shrink-0 text-accent" aria-hidden />
          <span>
            <span className="font-medium text-text">No endpoint handy?</span> Use airval’s built-in mock AI service (a RAG-style support bot).
          </span>
        </button>
      )}
      <div className="grid gap-4 sm:grid-cols-2">
        <Field label="Name" error={touched && !name.trim() ? 'Required' : undefined}>
          <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="Production" />
        </Field>
        <Field label="Kind">
          <Select
            value={kind}
            onValueChange={(v) => {
              setKind(v as TargetKind)
              setConfig(defaultConfig(v as TargetKind))
            }}
            options={wizardKinds.map((k) => ({ value: k, label: kindMeta[k].label }))}
          />
        </Field>
      </div>
      <TargetForm kind={kind} config={config} onChange={setConfig} errors={errors} />
      <div className="rounded-md border border-border bg-surface-2 p-3">
        <Playground kind={kind} config={config} projectId={project.id} />
      </div>
    </StepBody>
  )
}

// ---------------------------------------------------------------- dataset

const starterQuestions = [
  'How do I reset my password?',
  'Where is my order #48213?',
  'Can I return an item after 30 days?',
  'Do you ship to Canada?',
  'How do I update my billing address?',
  'Why was I charged twice this month?',
  'How do I cancel my subscription?',
  'Is my data deleted when I close my account?',
  'Can I change the delivery date of my order?',
  'How do I contact a human agent?',
]

function DatasetStep({ project, onBack, onDone, onSkip }: { project: ApiProject; onBack: () => void; onDone: (d: S['Dataset'], hasExpected: boolean) => void; onSkip: () => void }) {
  const [name, setName] = useState('Golden set')
  const [dataset, setDataset] = useState<S['Dataset'] | null>(null)
  const [importOpen, setImportOpen] = useState(false)
  const [busy, setBusy] = useState(false)
  const create = useCreateDataset(project.id)
  const examples = useExamples(dataset?.id)
  const loaded = useMemo(() => examples.data?.pages.flatMap((p) => p.items) ?? [], [examples.data])

  const ensure = async (): Promise<S['Dataset']> => {
    if (dataset) return dataset
    const d = await create.mutateAsync({ name: name.trim() || 'Golden set' })
    setDataset(d)
    return d
  }
  const finish = async (d: S['Dataset']) => {
    await unwrap(http.POST('/v1/datasets/{dataset}/versions', { params: { path: { dataset: d.id } }, body: { note: 'Initial version' } }))
    const first = await unwrap(http.GET('/v1/datasets/{dataset}/examples', { params: { path: { dataset: d.id }, query: { limit: 50 } } }))
    onDone(d, first.items.some((e) => e.expected !== undefined && e.expected !== null && e.expected !== ''))
  }
  const starter = async () => {
    setBusy(true)
    try {
      const d = await ensure()
      await unwrap(
        http.POST('/v1/datasets/{dataset}/examples', {
          params: { path: { dataset: d.id } },
          body: { examples: starterQuestions.map((q) => ({ input: { question: q }, tags: ['starter'] })) },
        }),
      )
      await finish(d)
    } catch (err) {
      toast.error('Couldn’t create the dataset', { description: errorMessage(err) })
    } finally {
      setBusy(false)
    }
  }
  const openImport = async () => {
    try {
      await ensure()
      setImportOpen(true)
    } catch (err) {
      toast.error('Couldn’t create the dataset', { description: errorMessage(err) })
    }
  }

  return (
    <StepBody
      title="Add a dataset"
      description="Examples to run against the target: an input (e.g. a question), optionally an expected answer, context and tags."
      footer={
        <>
          <Back onClick={onBack} />
          <div className="flex gap-2">
            <Button variant="ghost" onClick={onSkip}>
              Finish later
            </Button>
            {dataset && loaded.length > 0 && (
              <Button variant="primary" iconRight={<ArrowRight />} loading={busy} onClick={() => (setBusy(true), finish(dataset).finally(() => setBusy(false)))}>
                Continue with {loaded.length} examples
              </Button>
            )}
          </div>
        </>
      }
    >
      <Field label="Dataset name">
        <Input value={dataset?.name ?? name} onChange={(e) => setName(e.target.value)} disabled={!!dataset} />
      </Field>
      <div className="grid gap-3 sm:grid-cols-2">
        <Choice icon={<FileUp />} title="Import a file" description="CSV, JSONL, JSON or Parquet. Map columns to input, expected, context and tags." onClick={openImport} disabled={busy} />
        <Choice
          icon={<Database />}
          title="Start with 10 starter examples"
          description="Common support questions (no expected answers). Edit or extend them later."
          onClick={starter}
          loading={busy}
        />
      </div>
      {dataset && <ImportWizard open={importOpen} onOpenChange={setImportOpen} datasetId={dataset.id} onDone={() => (setImportOpen(false), setBusy(true), finish(dataset).finally(() => setBusy(false)))} />}
    </StepBody>
  )
}

function Choice({ icon, title, description, onClick, disabled, loading, selected }: { icon: ReactNode; title: string; description: string; onClick: () => void; disabled?: boolean; loading?: boolean; selected?: boolean }) {
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={disabled || loading}
      aria-busy={loading || undefined}
      className={cn(
        'flex items-start gap-3 rounded-md border bg-surface p-4 text-left transition-colors duration-150 focus-ring disabled:opacity-60',
        selected ? 'border-accent bg-accent-subtle' : 'border-border hover:border-border-strong hover:bg-surface-hover',
      )}
    >
      <span className="flex size-8 shrink-0 items-center justify-center rounded-md border border-border bg-surface-2 text-text-2 [&_svg]:size-4">{icon}</span>
      <span className="min-w-0">
        <span className="block text-sm font-medium text-text">{loading ? 'Working…' : title}</span>
        <span className="mt-0.5 block text-xs text-text-muted">{description}</span>
      </span>
    </button>
  )
}

// ---------------------------------------------------------------- evaluators

interface Preset {
  id: string
  name: string
  kind: S['EvaluatorKind']
  description: string
  config: Obj
  threshold?: number
  needsExpected?: boolean
}

const presets: Preset[] = [
  { id: 'exact', name: 'Exact match', kind: 'equals', description: 'Output equals the expected answer (whitespace and case normalized).', config: {}, needsExpected: true },
  { id: 'similar', name: 'Answer similarity', kind: 'levenshtein', description: 'Edit similarity to the expected answer; partial credit for near misses.', config: {}, threshold: 0.8, needsExpected: true },
  { id: 'rouge', name: 'ROUGE-L', kind: 'rouge_l', description: 'Overlap with the expected answer, for longer free-text responses.', config: {}, threshold: 0.5, needsExpected: true },
  { id: 'latency', name: 'Latency under 5 s', kind: 'latency_ms', description: 'Fails responses slower than 5 seconds.', config: { max_ms: 5000 } },
  { id: 'pii', name: 'No personal data', kind: 'no_pii', description: 'Flags emails, phone numbers, card numbers and other PII in the output.', config: {} },
  { id: 'refusal', name: 'Not a refusal', kind: 'refusal', description: 'Fails when the assistant refuses to answer.', config: {} },
]

function EvaluatorStep({ project, hasExpected, onBack, onDone, onSkip }: { project: ApiProject; hasExpected: boolean; onBack: () => void; onDone: (e: { evaluator: S['Evaluator']; threshold?: number }[]) => void; onSkip: () => void }) {
  const [picked, setPicked] = useState<Set<string>>(() => new Set(hasExpected ? ['exact', 'similar', 'latency'] : ['latency', 'pii', 'refusal']))
  const [busy, setBusy] = useState(false)
  const save = useSaveEvaluator(project.id)
  const toggle = (id: string) => setPicked((s) => (s.has(id) ? new Set([...s].filter((x) => x !== id)) : new Set([...s, id])))

  const create = async () => {
    setBusy(true)
    try {
      const out = []
      for (const p of presets.filter((p) => picked.has(p.id))) {
        const ev = await save.mutateAsync({ name: p.name, description: p.description, kind: p.kind, config: p.config })
        out.push({ evaluator: ev, threshold: p.threshold })
      }
      onDone(out)
    } catch (err) {
      toast.error('Couldn’t create the evaluators', { description: errorMessage(err) })
    } finally {
      setBusy(false)
    }
  }

  return (
    <StepBody
      title="Pick evaluators"
      description="Deterministic checks run instantly and need no API keys. Add LLM judges (faithfulness, helpfulness…) later from Evaluators."
      footer={
        <>
          <Back onClick={onBack} />
          <div className="flex gap-2">
            <Button variant="ghost" onClick={onSkip}>
              Finish later
            </Button>
            <Button variant="primary" iconRight={<ArrowRight />} loading={busy} disabled={picked.size === 0} onClick={create}>
              Add {picked.size} evaluator{picked.size === 1 ? '' : 's'}
            </Button>
          </div>
        </>
      }
    >
      <ul className="flex flex-col divide-y divide-border rounded-md border border-border">
        {presets.map((p) => {
          const off = p.needsExpected && !hasExpected
          return (
            <li key={p.id}>
              <label className={cn('flex cursor-pointer items-start gap-3 px-4 py-3 hover:bg-surface-hover', off && 'opacity-60')}>
                <Checkbox checked={picked.has(p.id)} onCheckedChange={() => toggle(p.id)} className="mt-0.5" aria-label={p.name} />
                <span className="min-w-0 flex-1">
                  <span className="flex items-center gap-2 text-sm font-medium text-text">
                    {p.name}
                    {p.needsExpected && <Badge variant="neutral">needs expected</Badge>}
                  </span>
                  <span className="mt-0.5 block text-xs text-text-muted">{p.description}</span>
                </span>
              </label>
            </li>
          )
        })}
      </ul>
      {!hasExpected && <p className="text-xs text-text-muted">Your dataset has no expected answers yet, so reference-based checks would fail every example.</p>}
    </StepBody>
  )
}

// ---------------------------------------------------------------- first run

function RunStep({ project, orgSlug, draft, onBack, onRestart }: { project: ApiProject; orgSlug: string; draft: Draft; onBack: () => void; onRestart: () => void }) {
  const navigate = useNavigate()
  const [name, setName] = useState('Smoke suite')
  const [gate, setGate] = useState(true)
  const [busy, setBusy] = useState(false)
  const run = useRunSuite()
  const ready = !!draft.target && !!draft.dataset && !!draft.evaluators?.length

  const start = async () => {
    if (!ready) return
    setBusy(true)
    try {
      const suite = await unwrap(
        http.POST('/v1/projects/{project}/suites', {
          params: { path: { project: project.id } },
          body: {
            name: name.trim() || 'Smoke suite',
            config: {
              trials: 1,
              target_ids: [draft.target!.id],
              datasets: [{ dataset_id: draft.dataset!.id }],
              evaluators: draft.evaluators!.map((e) => ({ evaluator_id: e.evaluator.id, weight: 1, ...(e.threshold !== undefined ? { threshold: e.threshold } : {}) })),
            },
          },
        }),
      )
      if (gate) await unwrap(http.POST('/v1/suites/{suite}/gates', { params: { path: { suite: suite.id } }, body: { name: 'Pass rate', text: 'pass_rate(all) >= 0.8', enabled: true } }))
      const created = await run.mutateAsync({ suite_id: suite.id })
      const first = created.runs[0]
      toast.success('First run started')
      if (first) void navigate({ to: '/o/$org/p/$project/runs/$run', params: { org: orgSlug, project: project.slug, run: first.id } })
    } catch (err) {
      toast.error('Couldn’t start the run', { description: errorMessage(err) })
      setBusy(false)
    }
  }

  return (
    <StepBody
      title="Create a suite and run it"
      description="A suite bundles the target, data, evaluators and gates. You can schedule it or call it from CI later."
      footer={
        <>
          <Back onClick={onBack} />
          <Button variant="primary" icon={<Play />} loading={busy} disabled={!ready} onClick={start}>
            Run suite
          </Button>
        </>
      }
    >
      {ready ? (
        <>
          <Field label="Suite name">
            <Input value={name} onChange={(e) => setName(e.target.value)} />
          </Field>
          <dl className="grid gap-x-6 gap-y-2 rounded-md border border-border bg-surface-2 p-4 text-sm sm:grid-cols-[auto_1fr]">
            <dt className="text-text-muted">Target</dt>
            <dd className="font-medium text-text">{draft.target!.name}</dd>
            <dt className="text-text-muted">Dataset</dt>
            <dd className="font-medium text-text">{draft.dataset!.name}</dd>
            <dt className="text-text-muted">Evaluators</dt>
            <dd className="flex flex-wrap gap-1.5">
              {draft.evaluators!.map((e) => (
                <Badge key={e.evaluator.id} variant="neutral">
                  {e.evaluator.name}
                </Badge>
              ))}
            </dd>
          </dl>
          <label className="flex cursor-pointer items-start gap-3">
            <Checkbox checked={gate} onCheckedChange={(v) => setGate(v === true)} className="mt-0.5" aria-label="Add a pass-rate gate" />
            <span>
              <span className="block text-sm font-medium text-text">
                Add a gate: <code className="font-mono text-xs">pass_rate(all) &gt;= 0.8</code>
              </span>
              <span className="block text-xs text-text-muted">Runs pass when at least 80% of examples pass every evaluator. CI uses this for its exit code.</span>
            </span>
          </label>
        </>
      ) : (
        <div className="flex flex-col items-start gap-3 rounded-md border border-border bg-surface-2 p-4 text-sm text-text-2">
          <span className="flex items-center gap-2">
            <Sparkles className="size-4 text-accent" aria-hidden />
            Some steps were skipped in this session. Pick a target, dataset and evaluators to run your first suite.
          </span>
          <Button size="sm" icon={<Check />} onClick={onRestart}>
            Resume setup
          </Button>
        </div>
      )}
    </StepBody>
  )
}
