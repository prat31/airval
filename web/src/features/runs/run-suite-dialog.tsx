import { useQuery } from '@tanstack/react-query'
import { Link, useNavigate } from '@tanstack/react-router'
import { CircleAlert, Play, Plus, X } from 'lucide-react'
import { useState, type FormEvent, type KeyboardEvent } from 'react'
import { useWorkspace } from '@/app/workspace'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Combobox } from '@/components/ui/combobox'
import { Dialog } from '@/components/ui/dialog'
import { EmptyState } from '@/components/ui/empty-state'
import { Field } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { SegmentedControl } from '@/components/ui/segmented-control'
import { Skeleton } from '@/components/ui/skeleton'
import { toast } from '@/components/ui/toast'
import { ApiError } from '@/lib/api/client'
import { suitesQuery, targetsQuery, useCreateRun, type RunCreate, type Suite } from './api'

type SampleMode = 'all' | 'count' | 'percent'

function RunSuiteForm({
  suites,
  defaultSuiteId,
  onDone,
  create,
}: {
  suites: Suite[]
  defaultSuiteId?: string
  onDone: () => void
  create: ReturnType<typeof useCreateRun>
}) {
  const { org, project } = useWorkspace()
  const targets = useQuery(targetsQuery(project?.id ?? ''))
  const navigate = useNavigate()

  const [suiteId, setSuiteId] = useState<string | null>(defaultSuiteId ?? suites[0]?.id ?? null)
  const suite = suites.find((s) => s.id === suiteId)
  const [targetIds, setTargetIds] = useState<string[] | null>(null)
  const effectiveTargets = targetIds ?? suite?.config.target_ids ?? []
  const [sample, setSample] = useState<SampleMode>('all')
  const [sampleN, setSampleN] = useState('100')
  const [trials, setTrials] = useState(String(suite?.config.trials ?? 1))
  const [name, setName] = useState('')
  const [labels, setLabels] = useState<[string, string][]>([])
  const [labelDraft, setLabelDraft] = useState('')

  const trialsN = Number(trials)
  const trialsInvalid = !Number.isInteger(trialsN) || trialsN < 1 || trialsN > 10
  const n = Number(sampleN)
  const sampleInvalid = sample !== 'all' && (!Number.isFinite(n) || n <= 0 || (sample === 'percent' && n > 100))

  const addLabel = () => {
    const m = labelDraft.trim().match(/^([\w.-]+)\s*[=:]\s*(.+)$/)
    if (!m) return
    setLabels((ls) => [...ls.filter(([k]) => k !== m[1]), [m[1]!, m[2]!.trim()]])
    setLabelDraft('')
  }
  const onLabelKey = (e: KeyboardEvent<HTMLInputElement>) => {
    if (e.key === 'Enter' || e.key === ',') {
      e.preventDefault()
      addLabel()
    } else if (e.key === 'Backspace' && !labelDraft && labels.length) setLabels((ls) => ls.slice(0, -1))
  }

  const submit = (e: FormEvent) => {
    e.preventDefault()
    if (!suiteId || trialsInvalid || sampleInvalid) return
    const body: RunCreate = {
      suite_id: suiteId,
      target_ids: targetIds ?? undefined,
      trials: trialsN,
      name: name.trim() || undefined,
      labels: labels.length ? Object.fromEntries(labels) : undefined,
      sampling: sample === 'all' ? undefined : sample === 'count' ? { mode: 'random', n } : { mode: 'random', percent: n },
    }
    create.mutate(body, {
      onSuccess: ({ runs }) => {
        onDone()
        const first = runs[0]
        toast.success(runs.length > 1 ? `Started ${runs.length} runs` : 'Run started')
        if (first && project)
          navigate({ to: '/o/$org/p/$project/runs/$run', params: { org: org.slug, project: project.slug, run: first.id } })
      },
    })
  }

  const notAvailable = create.error instanceof ApiError && create.error.status === 501

  return (
    <form id="run-suite-form" onSubmit={submit} className="flex flex-col gap-4 pb-1">
      {create.error != null && (
        <div role="alert" className="flex items-start gap-2 rounded-md bg-fail-subtle px-3 py-2 text-sm text-fail">
          <CircleAlert className="mt-0.5 size-4 shrink-0" aria-hidden />
          <span>
            {notAvailable
              ? 'This server can’t execute runs yet (the run engine isn’t enabled). Your suite is saved — try again once the worker is running.'
              : create.error instanceof ApiError
                ? create.error.message
                : 'Could not start the run.'}
          </span>
        </div>
      )}
      <Field label="Suite">
        <Combobox
          options={suites.map((s) => ({ value: s.id, label: s.name, description: s.description || undefined }))}
          value={suiteId}
          onValueChange={(v) => {
            setSuiteId(v)
            setTargetIds(null)
            const s = suites.find((x) => x.id === v)
            if (s) setTrials(String(s.config.trials ?? 1))
          }}
          placeholder="Choose a suite"
          searchPlaceholder="Search suites…"
        />
      </Field>
      <Field label="Targets" hint={effectiveTargets.length > 1 ? 'One run per target — they’ll be comparable side by side.' : 'Defaults to the suite’s targets.'}>
        <Combobox
          multiple
          options={(targets.data ?? []).map((t) => ({ value: t.id, label: t.name, description: t.kind }))}
          value={effectiveTargets}
          onValueChange={setTargetIds}
          placeholder={targets.isPending ? 'Loading targets…' : 'Suite default'}
          searchPlaceholder="Search targets…"
        />
      </Field>
      <div className="grid grid-cols-1 gap-4 sm:grid-cols-[1fr_auto]">
        <Field label="Sample" error={sampleInvalid ? (sample === 'percent' ? 'Enter 1–100.' : 'Enter a positive number.') : undefined}>
          <div className="flex items-center gap-2">
            <SegmentedControl<SampleMode>
              aria-label="Sampling"
              value={sample}
              onValueChange={setSample}
              options={[
                { value: 'all', label: 'All' },
                { value: 'count', label: 'Random N' },
                { value: 'percent', label: 'Percent' },
              ]}
            />
            {sample !== 'all' && (
              <Input
                aria-label={sample === 'count' ? 'Number of examples' : 'Percent of examples'}
                value={sampleN}
                onChange={(e) => setSampleN(e.target.value)}
                inputMode="numeric"
                className="w-20"
                trailing={sample === 'percent' ? <span className="text-xs text-text-muted">%</span> : undefined}
              />
            )}
          </div>
        </Field>
        <Field label="Trials" error={trialsInvalid ? '1–10' : undefined}>
          <Input value={trials} onChange={(e) => setTrials(e.target.value)} inputMode="numeric" className="w-20" aria-label="Trials per example" />
        </Field>
      </div>
      <Field label="Name" optional>
        <Input value={name} onChange={(e) => setName(e.target.value)} placeholder={suite ? `${suite.name} · manual` : 'Run name'} />
      </Field>
      <Field label="Labels" optional hint="key=value, then Enter — e.g. git_sha=4be1c09">
        <div className="flex min-h-8 flex-wrap items-center gap-1 rounded-sm border border-border bg-surface px-1.5 py-1 focus-within:border-accent">
          {labels.map(([k, v]) => (
            <Badge key={k} variant="neutral" mono className="gap-1 pr-0.5">
              {k}={v}
              <button
                type="button"
                aria-label={`Remove label ${k}`}
                onClick={() => setLabels((ls) => ls.filter(([x]) => x !== k))}
                className="rounded-sm p-0.5 text-text-muted hover:text-text focus-ring"
              >
                <X className="size-3" />
              </button>
            </Badge>
          ))}
          <input
            value={labelDraft}
            onChange={(e) => setLabelDraft(e.target.value)}
            onKeyDown={onLabelKey}
            onBlur={addLabel}
            placeholder={labels.length ? '' : 'branch=main'}
            className="h-6 min-w-24 flex-1 bg-transparent px-1 font-mono text-xs text-text outline-none placeholder:text-text-muted"
            aria-label="Add label"
          />
        </div>
      </Field>
      <input type="submit" hidden />
    </form>
  )
}

export function RunSuiteDialog({ open, onOpenChange, defaultSuiteId }: { open: boolean; onOpenChange: (o: boolean) => void; defaultSuiteId?: string }) {
  const { org, project } = useWorkspace()
  const suites = useQuery({ ...suitesQuery(project?.id ?? ''), enabled: open && !!project })
  const create = useCreateRun()
  const reset = create.reset
  const hasSuites = (suites.data?.length ?? 0) > 0
  return (
    <Dialog
      open={open}
      onOpenChange={(o) => {
        if (!o) reset()
        onOpenChange(o)
      }}
      title="Run suite"
      description="Execute a suite against one or more targets. Results stream in live."
      footer={
        hasSuites ? (
          <>
            <Button variant="ghost" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <Button type="submit" form="run-suite-form" variant="primary" icon={<Play />} loading={create.isPending}>
              Start run
            </Button>
          </>
        ) : undefined
      }
    >
      {suites.isPending ? (
        <div className="flex flex-col gap-4 pb-2">
          {[0, 1, 2].map((i) => (
            <div key={i} className="flex flex-col gap-1.5">
              <Skeleton className="h-3.5 w-16" />
              <Skeleton className="h-8 w-full" />
            </div>
          ))}
        </div>
      ) : suites.error ? (
        <EmptyState compact illustration="error" title="Couldn’t load suites" description={suites.error.message} action={<Button onClick={() => suites.refetch()}>Retry</Button>} />
      ) : !hasSuites ? (
        <EmptyState
          compact
          illustration="suites"
          title="No suites yet"
          description="A suite bundles a dataset, evaluators and gates. Create one to start running."
          action={
            project && (
              <Button variant="primary" size="sm" icon={<Plus />} asChild>
                <Link to="/o/$org/p/$project/suites/new" params={{ org: org.slug, project: project.slug }} onClick={() => onOpenChange(false)}>
                  New suite
                </Link>
              </Button>
            )
          }
        />
      ) : (
        open && <RunSuiteForm suites={suites.data!} defaultSuiteId={defaultSuiteId} create={create} onDone={() => onOpenChange(false)} />
      )}
    </Dialog>
  )
}
