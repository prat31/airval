import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ArrowLeft, Check, ClipboardCheck, Plus, SkipForward } from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'
import { Screen } from '@/app/screen'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardBody, CardHeader } from '@/components/ui/card'
import { Combobox } from '@/components/ui/combobox'
import { Dialog } from '@/components/ui/dialog'
import { EmptyState } from '@/components/ui/empty-state'
import { Field } from '@/components/ui/field'
import { Input, Textarea } from '@/components/ui/input'
import { JsonViewer } from '@/components/ui/json-viewer'
import { Kbd } from '@/components/ui/kbd'
import { PageHeader } from '@/components/ui/page-header'
import { Select } from '@/components/ui/select'
import { Skeleton } from '@/components/ui/skeleton'
import { toast } from '@/components/ui/toast'
import { cn } from '@/lib/cn'
import { formatPercent } from '@/lib/format'
import { useHotkeys } from '@/lib/hotkeys'
import { useEvaluators } from '../evaluators/api'
import { errorMessage, fetchAll, http, unwrap, type S } from '../shared/api'
import { useProjectCtx } from '../shared/context'
import { str, useSearchState } from '../shared/search-state'
import { ErrorPanel, TagInput } from '../shared/ui'

function useQueues(projectId?: string) {
  return useQuery({
    queryKey: ['review-queues', projectId],
    enabled: !!projectId,
    refetchInterval: 30_000,
    queryFn: () =>
      fetchAll<S['ReviewQueue']>(async (cursor) => {
        const { data } = await http.GET('/v1/projects/{project}/review-queues', { params: { path: { project: projectId! }, query: { cursor, limit: 100 } } })
        return data ?? { items: [] }
      }),
  })
}

function NewQueueDialog({ open, onOpenChange, projectId }: { open: boolean; onOpenChange: (o: boolean) => void; projectId?: string }) {
  const qc = useQueryClient()
  const evaluators = useEvaluators(projectId)
  const [name, setName] = useState('')
  const [evaluatorId, setEvaluatorId] = useState<string | null>(null)
  const [mode, setMode] = useState<'all' | 'failures' | 'sample'>('failures')
  const [labels, setLabels] = useState<string[]>([])
  const [instructions, setInstructions] = useState('')
  const create = useMutation({
    mutationFn: () => unwrap(http.POST('/v1/projects/{project}/review-queues', { params: { path: { project: projectId! } }, body: { name: name.trim(), evaluator_id: evaluatorId ?? undefined, config: { mode, pass_fail: true, scale: { min: 1, max: 5 }, labels, instructions } } })),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['review-queues', projectId] })
      toast.success('Queue created')
      onOpenChange(false)
      setName('')
    },
  })
  const human = (evaluators.data ?? []).filter((e) => e.category === 'human')
  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title="New review queue"
      description="A queue collects results that need a human grade."
      footer={<><Button variant="ghost" onClick={() => onOpenChange(false)}>Cancel</Button><Button variant="primary" disabled={!name.trim()} loading={create.isPending} onClick={() => create.mutate()}>Create queue</Button></>}
    >
      <div className="flex flex-col gap-4">
        <Field label="Name"><Input autoFocus value={name} onChange={(e) => setName(e.target.value)} placeholder="Weekly spot check" /></Field>
        <Field label="Human evaluator" optional hint="Grades become this evaluator's scores.">
          <Combobox options={human.map((e) => ({ value: e.id, label: e.name }))} value={evaluatorId} onValueChange={setEvaluatorId} placeholder="None" emptyText="No human evaluators yet" />
        </Field>
        <Field label="Which results">
          <Select value={mode} onValueChange={(v) => setMode(v as typeof mode)} options={[{ value: 'failures', label: 'Failures only' }, { value: 'all', label: 'All results' }, { value: 'sample', label: 'A sample' }]} />
        </Field>
        <Field label="Labels" optional hint="Choices reviewers can tag a result with."><TagInput value={labels} onChange={setLabels} placeholder="e.g. hallucination" /></Field>
        <Field label="Instructions" optional><Textarea rows={3} value={instructions} onChange={(e) => setInstructions(e.target.value)} /></Field>
      </div>
    </Dialog>
  )
}

function Block({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <section>
      <h4 className="mb-1.5 text-xs font-semibold uppercase tracking-wide text-text-muted">{title}</h4>
      <div className="whitespace-pre-wrap rounded-md border border-border bg-surface-2/50 p-3 text-sm text-text">{children}</div>
    </section>
  )
}

const asText = (v: unknown) => (v === undefined || v === null ? '' : typeof v === 'string' ? v : JSON.stringify(v, null, 2))

function Grader({ queue, onBack }: { queue: S['ReviewQueue']; onBack: () => void }) {
  const qc = useQueryClient()
  const cfg = (queue.config ?? {}) as S['ReviewQueueConfig']
  const min = cfg.scale?.min ?? 1
  const max = cfg.scale?.max ?? 5
  const labels = cfg.labels ?? []
  const items = useQuery({
    queryKey: ['review-items', queue.id],
    queryFn: () => unwrap(http.GET('/v1/review-queues/{queue}/items', { params: { path: { queue: queue.id }, query: { status: ['pending', 'in_progress'], expand: true, limit: 200 } } })),
  })
  const list = useMemo(() => items.data?.items ?? [], [items.data])
  const [idx, setIdx] = useState(0)
  const [score, setScore] = useState<number | null>(null)
  const [passed, setPassed] = useState<boolean | null>(null)
  const [label, setLabel] = useState('')
  const [comment, setComment] = useState('')
  const item = list[Math.min(idx, Math.max(0, list.length - 1))]
  const result = item?.result

  useEffect(() => {
    setScore(null)
    setPassed(null)
    setLabel('')
    setComment('')
  }, [item?.id])

  const refresh = () => {
    void qc.invalidateQueries({ queryKey: ['review-items', queue.id] })
    void qc.invalidateQueries({ queryKey: ['review-queues'] })
  }
  const submit = useMutation({
    mutationFn: () =>
      unwrap(http.POST('/v1/annotations', { body: { result_id: item!.result_id, review_item_id: item!.id, evaluator_id: queue.evaluator_id ?? undefined, value: score !== null ? (score - min) / (max - min) : undefined, passed: passed ?? undefined, label: label || undefined, comment: comment || undefined, complete_item: true } })),
    onSuccess: () => {
      toast.success('Graded')
      refresh()
    },
  })
  const skip = useMutation({
    mutationFn: () => unwrap(http.PATCH('/v1/review-items/{item}', { params: { path: { item: item!.id } }, body: { status: 'skipped' } })),
    onSuccess: refresh,
  })
  const canSubmit = !!item && (score !== null || passed !== null || !!label)
  const total = queue.pending_count + queue.done_count
  const progress = total ? queue.done_count / total : 0

  useHotkeys({
    j: () => setIdx((i) => Math.min(list.length - 1, i + 1)),
    k: () => setIdx((i) => Math.max(0, i - 1)),
    p: () => setPassed(true),
    f: () => setPassed(false),
    s: () => item && skip.mutate(),
    'mod+enter': () => canSubmit && submit.mutate(),
    ...Object.fromEntries(Array.from({ length: max - min + 1 }, (_, i) => [String(min + i), () => setScore(min + i)])),
  })

  return (
    <Screen width="wide">
      <div>
        <Button variant="ghost" size="sm" icon={<ArrowLeft />} onClick={onBack}>Queues</Button>
      </div>
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-lg font-semibold tracking-tight text-text">{queue.name}</h1>
          <p className="text-sm text-text-2">{cfg.instructions || 'Grade each result, then move on.'}</p>
        </div>
        <div className="flex w-64 flex-col gap-1" aria-label="Progress">
          <div className="flex justify-between text-xs text-text-muted"><span>{queue.done_count} of {total} graded</span><span>{formatPercent(progress, 0)}</span></div>
          <div className="h-1.5 overflow-hidden rounded-full bg-surface-2" role="progressbar" aria-valuenow={Math.round(progress * 100)} aria-valuemin={0} aria-valuemax={100}><div className="h-full rounded-full bg-accent transition-[width] duration-200" style={{ width: `${progress * 100}%` }} /></div>
        </div>
      </div>
      {items.isLoading ? (
        <Skeleton className="h-96 w-full" />
      ) : items.error ? (
        <Card><ErrorPanel error={items.error} onRetry={() => void items.refetch()} /></Card>
      ) : !item || !result ? (
        <Card><EmptyState illustration="review" title="Queue cleared" description="Everything here has been graded. New results will appear as runs complete." action={<Button onClick={onBack}>Back to queues</Button>} /></Card>
      ) : (
        <div className="grid grid-cols-1 gap-4 lg:grid-cols-[minmax(0,1fr)_340px]">
          <Card>
            <CardHeader title={`Item ${Math.min(idx, list.length - 1) + 1} of ${list.length}`} description={`Result · trial ${result.trial + 1}`} actions={<Badge variant={result.status === 'passed' ? 'pass' : result.status === 'passed' ? 'neutral' : 'fail'}>{result.status}</Badge>} />
            <CardBody className="flex flex-col gap-4">
              <Block title="Input">{asText(result.input)}</Block>
              {result.expected !== undefined && result.expected !== null && <Block title="Expected">{asText(result.expected)}</Block>}
              <Block title="Output">{result.output || <span className="text-text-muted">(empty)</span>}</Block>
              {result.context && result.context.length > 0 && <Block title="Context">{result.context.join('\n\n')}</Block>}
              {result.tool_calls && result.tool_calls.length > 0 && <JsonViewer value={result.tool_calls} rootLabel="tool_calls" defaultExpandDepth={1} maxHeight={240} />}
              {result.scores?.length > 0 && (
                <div className="flex flex-wrap gap-2 text-xs text-text-2">{result.scores.map((s, i) => <Badge key={i} variant={s.passed === false ? 'fail' : 'neutral'}>{s.label || (s.value != null ? s.value.toFixed(2) : 'scored')}</Badge>)}</div>
              )}
            </CardBody>
          </Card>
          <Card className="self-start lg:sticky lg:top-4">
            <CardHeader title="Your grade" />
            <CardBody className="flex flex-col gap-4">
              <div>
                <div className="mb-1.5 text-sm font-medium text-text">Score</div>
                <div className="flex gap-1.5" role="radiogroup" aria-label="Score">
                  {Array.from({ length: max - min + 1 }, (_, i) => min + i).map((n) => (
                    <button key={n} type="button" role="radio" aria-checked={score === n} onClick={() => setScore(n)} className={cn('flex h-9 flex-1 flex-col items-center justify-center rounded-md border text-sm font-medium transition-colors focus-ring', score === n ? 'border-accent bg-accent-subtle text-accent' : 'border-border text-text-2 hover:bg-surface-hover')}>
                      {n}
                    </button>
                  ))}
                </div>
              </div>
              {cfg.pass_fail !== false && (
                <div className="grid grid-cols-2 gap-2">
                  <Button variant={passed === true ? 'primary' : 'secondary'} onClick={() => setPassed(true)}>Pass <Kbd>P</Kbd></Button>
                  <Button variant={passed === false ? 'danger' : 'secondary'} onClick={() => setPassed(false)}>Fail <Kbd>F</Kbd></Button>
                </div>
              )}
              {labels.length > 0 && (
                <div className="flex flex-wrap gap-1.5">
                  {labels.map((l) => (
                    <button key={l} type="button" aria-pressed={label === l} onClick={() => setLabel(label === l ? '' : l)} className={cn('inline-flex h-6 items-center rounded-sm px-2 text-xs ring-1 ring-inset transition-colors focus-ring', label === l ? 'bg-accent-subtle text-accent ring-accent' : 'text-text-2 ring-border hover:bg-surface-hover')}>{l}</button>
                  ))}
                </div>
              )}
              <Textarea rows={3} placeholder="Comment (optional)" aria-label="Comment" value={comment} onChange={(e) => setComment(e.target.value)} />
              <div className="flex items-center gap-2">
                <Button variant="primary" icon={<Check />} disabled={!canSubmit} loading={submit.isPending} onClick={() => submit.mutate()}>Submit <Kbd>⌘↵</Kbd></Button>
                <Button variant="ghost" icon={<SkipForward />} loading={skip.isPending} onClick={() => skip.mutate()}>Skip</Button>
              </div>
              <p className="text-xs text-text-muted">Keys: <Kbd>1</Kbd>–<Kbd>{max}</Kbd> score · <Kbd>J</Kbd>/<Kbd>K</Kbd> next/prev · <Kbd>S</Kbd> skip</p>
              {(submit.error || skip.error) && <p className="text-xs text-fail" role="alert">{errorMessage(submit.error ?? skip.error)}</p>}
            </CardBody>
          </Card>
        </div>
      )}
    </Screen>
  )
}

export function ReviewScreen() {
  const ctx = useProjectCtx()
  const queues = useQueues(ctx.projectId)
  const [search, patch] = useSearchState()
  const [creating, setCreating] = useState(false)
  const active = queues.data?.find((q) => q.id === str(search.queue))
  if (active) return <Grader queue={active} onBack={() => patch({ queue: null })} />

  const newButton = <Button variant="primary" icon={<Plus />} onClick={() => setCreating(true)}>New queue</Button>
  return (
    <Screen>
      <PageHeader icon={<ClipboardCheck />} title="Review" description="Human grading queues. Calibrate LLM judges and catch what automated scores miss." actions={newButton} />
      {queues.isLoading ? (
        <div className="grid grid-cols-1 gap-3 md:grid-cols-2 xl:grid-cols-3">{Array.from({ length: 3 }, (_, i) => <Skeleton key={i} className="h-32 rounded-md" />)}</div>
      ) : queues.error ? (
        <Card><ErrorPanel error={queues.error} onRetry={() => void queues.refetch()} /></Card>
      ) : (queues.data?.length ?? 0) === 0 ? (
        <Card><EmptyState illustration="review" title="Nothing to review" description="Create a queue, then send results to it from a run. Grade with 1–5, P / F and comments." action={newButton} /></Card>
      ) : (
        <div className="grid grid-cols-1 gap-3 md:grid-cols-2 xl:grid-cols-3">
          {queues.data!.map((q) => {
            const total = q.pending_count + q.done_count
            return (
              <button key={q.id} type="button" onClick={() => patch({ queue: q.id }, { push: true })} className="flex flex-col gap-3 rounded-md border border-border bg-surface p-4 text-left transition-colors hover:border-border-strong hover:bg-surface-hover focus-ring">
                <span className="flex items-center justify-between gap-2">
                  <span className="truncate text-base font-semibold text-text">{q.name}</span>
                  {q.pending_count > 0 ? <Badge variant="accent">{q.pending_count} to review</Badge> : <Badge variant="pass">Done</Badge>}
                </span>
                {q.description && <span className="line-clamp-2 text-sm text-text-2">{q.description}</span>}
                <span className="flex flex-col gap-1">
                  <span className="h-1.5 overflow-hidden rounded-full bg-surface-2"><span className="block h-full rounded-full bg-accent" style={{ width: `${total ? (q.done_count / total) * 100 : 0}%` }} /></span>
                  <span className="text-xs text-text-muted tabular">{q.done_count} of {total} graded</span>
                </span>
              </button>
            )
          })}
        </div>
      )}
      <NewQueueDialog open={creating} onOpenChange={setCreating} projectId={ctx.projectId} />
    </Screen>
  )
}
