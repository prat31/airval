import { ArrowLeft, CircleCheck, CircleX, Play, Save, ShieldCheck } from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardBody, CardHeader } from '@/components/ui/card'
import { CodeBlock } from '@/components/ui/code-block'
import { Combobox } from '@/components/ui/combobox'
import { Field } from '@/components/ui/field'
import { Input, Textarea } from '@/components/ui/input'
import { JsonViewer } from '@/components/ui/json-viewer'
import { ScoreChip } from '@/components/ui/score-chip'
import { Skeleton } from '@/components/ui/skeleton'
import { StatTile } from '@/components/ui/stat-tile'
import { Switch } from '@/components/ui/switch'
import { toast } from '@/components/ui/toast'
import { formatDuration, formatPercent } from '@/lib/format'
import { errorMessage, type Obj, type S } from '../shared/api'
import { useProjectCtx } from '../shared/context'
import { stableStringify } from '../shared/obj'
import { SchemaForm, schemaDefaults, validateSchema, type JsonSchema } from '../shared/schema-form'
import { ErrorPanel, JsonEditor, useDebounced } from '../shared/ui'
import { categoryLabel, costLabel, useAgreement, useCatalog, useEvaluator, usePreviewEvaluator, useSaveEvaluator, type KindInfo } from './api'

function kappaLabel(k: number) {
  if (k < 0.2) return 'Slight'
  if (k < 0.4) return 'Fair'
  if (k < 0.6) return 'Moderate'
  if (k < 0.8) return 'Substantial'
  return 'Almost perfect'
}

function Agreement({ id }: { id: string }) {
  const a = useAgreement(id)
  return (
    <Card>
      <CardHeader title="Agreement with humans" description="How often this evaluator agrees with human annotations." />
      <CardBody>
        {a.isLoading ? (
          <Skeleton className="h-16 w-full" />
        ) : a.error ? (
          <p className="text-sm text-text-muted">Agreement isn't available yet.</p>
        ) : !a.data || a.data.n === 0 ? (
          <p className="text-sm text-text-2">No overlap yet. Send results scored by this evaluator to a review queue; human grades calibrate it.</p>
        ) : (
          <div className="grid grid-cols-3 gap-3">
            <StatTile label="Cohen's κ" value={a.data.cohens_kappa != null ? a.data.cohens_kappa.toFixed(2) : '—'} unit={a.data.cohens_kappa != null ? kappaLabel(a.data.cohens_kappa) : undefined} />
            <StatTile label="Agreement" value={a.data.percent_agreement != null ? formatPercent(a.data.percent_agreement, 0) : '—'} />
            <StatTile label="Mean abs. error" value={a.data.mean_abs_error != null ? a.data.mean_abs_error.toFixed(2) : '—'} unit={`n=${a.data.n}`} />
          </div>
        )}
      </CardBody>
    </Card>
  )
}

/** Live preview: paste an output (and optionally expected / context) and see the score + reasoning. */
function PreviewPanel({ projectId, kind, config, threshold, valid, auto }: { projectId?: string; kind: S['EvaluatorKind']; config: Obj; threshold?: number; valid: boolean; auto: boolean }) {
  const preview = usePreviewEvaluator()
  const [output, setOutput] = useState('The refund window is 30 days from the delivery date.')
  const [expected, setExpected] = useState('Refunds are available within 30 days of delivery.')
  const [context, setContext] = useState('')
  const [input, setInput] = useState<unknown>({ question: 'What is the refund window?' })
  const [auto_, setAuto] = useState(auto)
  useEffect(() => setAuto(auto), [auto, kind])

  const body = useMemo<S['EvaluatorPreviewRequest']>(
    () => ({
      project_id: projectId,
      kind,
      config,
      example: {
        input: (input && typeof input === 'object' ? input : {}) as Record<string, unknown>,
        expected: expected || undefined,
        context: context.split('\n').filter((l) => l.trim()),
      },
      response: { output, context: context.split('\n').filter((l) => l.trim()) },
    }),
    [projectId, kind, config, input, expected, context, output],
  )
  const debounced = useDebounced(stableStringify(body), 700)
  useEffect(() => {
    if (auto_ && valid && output.trim()) preview.mutate(JSON.parse(debounced) as S['EvaluatorPreviewRequest'])
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [debounced, auto_, valid])

  const score = preview.data
  return (
    <Card className="self-start lg:sticky lg:top-4">
      <CardHeader
        title="Live preview"
        description="Score a sample output with the current settings."
        actions={
          <label className="flex items-center gap-2 text-xs text-text-2">
            Auto-run
            <Switch size="sm" aria-label="Auto-run preview" checked={auto_} onCheckedChange={setAuto} />
          </label>
        }
      />
      <CardBody className="flex flex-col gap-3">
        <Field label="Output">
          <Textarea rows={3} value={output} onChange={(e) => setOutput(e.target.value)} />
        </Field>
        <Field label="Expected" optional>
          <Textarea rows={2} value={expected} onChange={(e) => setExpected(e.target.value)} />
        </Field>
        <Field label="Context" optional hint="One passage per line.">
          <Textarea rows={2} value={context} onChange={(e) => setContext(e.target.value)} />
        </Field>
        <Field label="Example input (JSON)" optional>
          <JsonEditor value={input} onChange={setInput} rows={3} />
        </Field>
        <div>
          <Button icon={<Play />} loading={preview.isPending} disabled={!valid || !output.trim()} onClick={() => preview.mutate(body)}>
            Score output
          </Button>
        </div>

        <div className="rounded-md border border-border bg-surface-2/50 p-3" aria-live="polite">
          {preview.isPending && !score ? (
            <Skeleton className="h-14 w-full" />
          ) : preview.error ? (
            <div className="flex items-start gap-2 text-sm text-fail" role="alert">
              <CircleX className="mt-0.5 size-4 shrink-0" />
              <span className="min-w-0 break-words">{errorMessage(preview.error)}</span>
            </div>
          ) : !score ? (
            <p className="text-sm text-text-muted">{valid ? 'Run the preview to see a score and the reasoning behind it.' : 'Fix the highlighted settings to preview.'}</p>
          ) : (
            <div className="flex flex-col gap-3">
              <div className="flex flex-wrap items-center gap-2">
                <ScoreChip value={score.value ?? null} threshold={threshold} passed={score.passed ?? undefined} label={score.label} />
                {score.passed != null && (
                  <span className={score.passed ? 'inline-flex items-center gap-1 text-sm text-pass' : 'inline-flex items-center gap-1 text-sm text-fail'}>
                    {score.passed ? <CircleCheck className="size-4" /> : <CircleX className="size-4" />}
                    {score.passed ? 'Passed' : 'Failed'}
                  </span>
                )}
                {score.label && score.value != null && <Badge>{score.label}</Badge>}
                <span className="ml-auto text-xs text-text-muted">
                  {score.latency_ms !== undefined ? formatDuration(score.latency_ms) : ''}
                  {score.cost_usd ? ` · $${score.cost_usd.toFixed(4)}` : ''}
                </span>
              </div>
              {score.reason && <p className="whitespace-pre-wrap text-sm text-text-2">{score.reason}</p>}
              {score.meta && Object.keys(score.meta).length > 0 && <JsonViewer value={score.meta} rootLabel="meta" defaultExpandDepth={1} maxHeight={200} />}
            </div>
          )}
        </div>
      </CardBody>
    </Card>
  )
}

export function EvaluatorEditor({ id, newKind, onBack, onSaved }: { id?: string; newKind?: string; onBack: () => void; onSaved: (e: S['Evaluator']) => void }) {
  const ctx = useProjectCtx()
  const catalog = useCatalog()
  const existing = useEvaluator(id)
  const save = useSaveEvaluator(ctx.projectId, id)

  const [kind, setKind] = useState<S['EvaluatorKind'] | null>((newKind as S['EvaluatorKind']) ?? null)
  const [name, setName] = useState('')
  const [description, setDescription] = useState('')
  const [config, setConfig] = useState<Obj>({})
  const [showErrors, setShowErrors] = useState(false)
  const [baseline, setBaseline] = useState('')

  const info: KindInfo | undefined = catalog.data?.items.find((k) => k.kind === kind)
  const schema = info?.config_schema as JsonSchema | undefined

  // Seed from the saved evaluator, or from the schema defaults for a new one.
  const seedKey = id ? existing.data?.id : `${newKind}:${catalog.data ? 1 : 0}`
  useEffect(() => {
    if (id && existing.data) {
      setKind(existing.data.kind)
      setName(existing.data.name)
      setDescription(existing.data.description)
      setConfig((existing.data.config ?? {}) as Obj)
      setBaseline(stableStringify({ n: existing.data.name, d: existing.data.description, k: existing.data.kind, c: existing.data.config ?? {} }))
    } else if (!id && info) {
      setName((n) => n || info.name)
      setConfig(schemaDefaults(schema))
      setBaseline('')
    }
  }, [seedKey]) // eslint-disable-line react-hooks/exhaustive-deps

  const errors = useMemo(() => validateSchema(schema, config), [schema, config])
  const nameError = !name.trim() ? 'Give the evaluator a name' : undefined
  const valid = !nameError && Object.keys(errors).length === 0
  const dirty = !id || stableStringify({ n: name, d: description, k: kind, c: config }) !== baseline

  if ((id && existing.isLoading) || catalog.isLoading) return <div className="flex flex-col gap-3"><Skeleton className="h-10 w-64" /><Skeleton className="h-96 w-full" /></div>
  if (existing.error || catalog.error) return <Card><ErrorPanel error={existing.error ?? catalog.error} onRetry={() => void (existing.error ? existing.refetch() : catalog.refetch())} /></Card>
  if (!kind || !info) return <Card><ErrorPanel error={new Error('Unknown evaluator kind')} onRetry={onBack} /></Card>

  const submit = () => {
    setShowErrors(true)
    if (!valid) return toast.error('Fix the highlighted fields first')
    save.mutate(
      { name: name.trim(), description, kind, config },
      {
        onSuccess: (e) => {
          toast.success(id ? 'Evaluator updated' : 'Evaluator created')
          setBaseline(stableStringify({ n: e.name, d: e.description, k: e.kind, c: e.config ?? {} }))
          onSaved(e)
        },
      },
    )
  }

  return (
    <div className="flex flex-col gap-4 pb-20">
      <div>
        <Button variant="ghost" size="sm" icon={<ArrowLeft />} onClick={onBack}>
          Evaluators
        </Button>
      </div>
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <div className="flex flex-wrap items-center gap-2">
            <h1 className="text-lg font-semibold tracking-tight text-text">{id ? name || 'Evaluator' : `New ${info.name} evaluator`}</h1>
            <Badge variant="accent">{categoryLabel[info.category]}</Badge>
            {existing.data && <Badge mono>v{existing.data.version}</Badge>}
          </div>
          {info.description && <p className="mt-0.5 max-w-2xl text-sm text-text-2">{info.description}</p>}
        </div>
      </div>

      <div className="grid grid-cols-1 gap-4 lg:grid-cols-[minmax(0,3fr)_minmax(0,2fr)]">
        <div className="flex min-w-0 flex-col gap-4">
          <Card>
            <CardHeader title="Definition" />
            <CardBody className="flex flex-col gap-4">
              <Field label="Name" error={showErrors ? nameError : undefined}>
                <Input value={name} onChange={(e) => setName(e.target.value)} autoFocus={!id} placeholder="Faithfulness" />
              </Field>
              <Field label="Description" optional>
                <Textarea rows={2} value={description} onChange={(e) => setDescription(e.target.value)} placeholder="What this checks, in your words." />
              </Field>
              {!id && (
                <Field label="Kind">
                  <Combobox
                    options={catalog.data!.items.map((k) => ({ value: k.kind, label: k.name, description: categoryLabel[k.category] }))}
                    value={kind}
                    onValueChange={(v) => {
                      if (!v) return
                      const next = catalog.data!.items.find((k) => k.kind === v)
                      setKind(v as S['EvaluatorKind'])
                      setConfig(schemaDefaults(next?.config_schema as JsonSchema | undefined))
                    }}
                  />
                </Field>
              )}
              <div className="flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-text-muted">
                {costLabel(info.cost) && <span>{costLabel(info.cost)}</span>}
                {info.default_threshold !== undefined && <span>Default threshold {info.default_threshold}</span>}
                {(info.requires ?? []).map((r) => (
                  <Badge key={r} variant="outline">needs {r.replace(/_/g, ' ')}</Badge>
                ))}
              </div>
            </CardBody>
          </Card>

          <Card>
            <CardHeader title="Settings" description={catalog.data?.fallback ? 'The evaluator catalog is unavailable, so settings are edited as raw JSON.' : undefined} />
            <CardBody>
              {schema?.properties && Object.keys(schema.properties).length > 0 ? (
                <SchemaForm schema={schema} value={config} onChange={setConfig} errors={showErrors || Object.keys(errors).length ? errors : {}} />
              ) : catalog.data?.fallback ? (
                <JsonEditor value={config} onChange={(v) => setConfig((v as Obj) ?? {})} rows={10} placeholder="{ }" />
              ) : (
                <p className="text-sm text-text-muted">This evaluator has no settings. It works out of the box.</p>
              )}
            </CardBody>
          </Card>

          {info.prompt && (
            <Card>
              <CardHeader
                title="Judge prompt"
                description="The exact template sent to the judge model. Set a custom prompt in settings to override it."
                actions={info.prompt_version ? <Badge mono><ShieldCheck /> {info.prompt_version}</Badge> : undefined}
              />
              <CardBody>
                <CodeBlock code={info.prompt} language="text" maxHeight={320} />
              </CardBody>
            </Card>
          )}
          {id && info.category !== 'deterministic' && <Agreement id={id} />}
        </div>

        <PreviewPanel projectId={ctx.projectId} kind={kind} config={config} threshold={info.default_threshold} valid={valid && info.category !== 'human'} auto={info.category === 'deterministic'} />
      </div>

      {dirty && (
        <div className="fixed inset-x-0 bottom-0 z-20 border-t border-border bg-surface/95 backdrop-blur supports-[backdrop-filter]:bg-surface/80">
          <div className="mx-auto flex max-w-[1280px] items-center justify-between gap-3 px-4 py-3 sm:px-6 lg:px-8">
            <span className="text-sm text-text-2">{id ? 'Unsaved changes. Saving bumps the evaluator version.' : 'Ready to save this evaluator.'}</span>
            <div className="flex items-center gap-2">
              <Button variant="ghost" onClick={onBack}>
                Cancel
              </Button>
              <Button variant="primary" icon={<Save />} loading={save.isPending} onClick={submit}>
                {id ? 'Save changes' : 'Create evaluator'}
              </Button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}

