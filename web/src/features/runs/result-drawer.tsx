import { useQuery } from '@tanstack/react-query'
import { ArrowDown, ArrowUp, Bot, ClipboardCheck, Copy, DatabaseZap, FileSearch, User, Wrench } from 'lucide-react'
import { useState } from 'react'
import { useWorkspace } from '@/app/workspace'
import { Badge } from '@/components/ui/badge'
import { Button, IconButton } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { CodeBlock } from '@/components/ui/code-block'
import { DiffViewer } from '@/components/ui/diff-viewer'
import { Drawer } from '@/components/ui/drawer'
import { JsonViewer } from '@/components/ui/json-viewer'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import { ScoreChip } from '@/components/ui/score-chip'
import { SegmentedControl } from '@/components/ui/segmented-control'
import { Select } from '@/components/ui/select'
import { SkeletonText } from '@/components/ui/skeleton'
import { StatusPill } from '@/components/ui/status-pill'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { toast } from '@/components/ui/toast'
import { Tooltip } from '@/components/ui/tooltip'
import { ApiError } from '@/lib/api/client'
import { cn } from '@/lib/cn'
import { formatCompact, formatDuration, formatUsd } from '@/lib/format'
import { useHotkeys } from '@/lib/hotkeys'
import { datasetsQuery, resultQuery, reviewQueuesQuery, useAddToDataset, useSendToReview, type Result } from './api'
import type { EvalColumn } from './results-table'
import type { DrawerTab } from './search'
import { resultStatus, shortId } from './ui'

const text = (v: unknown) => (v == null ? '' : typeof v === 'string' ? v : JSON.stringify(v, null, 2))

function Section({ title, children, actions }: { title: string; children: React.ReactNode; actions?: React.ReactNode }) {
  return (
    <section className="flex min-w-0 flex-col gap-2">
      <div className="flex items-center justify-between gap-2">
        <h3 className="text-xs font-medium uppercase tracking-wide text-text-muted">{title}</h3>
        {actions}
      </div>
      {children}
    </section>
  )
}

function TextPanel({ value, tone }: { value: string; tone?: 'fail' }) {
  return (
    <div
      className={cn(
        'max-h-80 min-h-16 overflow-auto whitespace-pre-wrap break-words rounded-md border border-border bg-surface-2 px-3.5 py-3 text-sm leading-6 text-text',
        tone === 'fail' && 'border-fail/30 bg-fail-subtle font-mono text-xs text-fail',
      )}
    >
      {value || <span className="text-text-muted">—</span>}
    </div>
  )
}

/** Build a cURL command from the recorded request (secrets stay as template references). */
export function toCurl(req: unknown): string | null {
  if (!req || typeof req !== 'object') return null
  const r = req as { method?: string; url?: string; headers?: Record<string, string>; body?: unknown }
  if (!r.url) return null
  const q = (s: string) => `'${s.replace(/'/g, `'\\''`)}'`
  const lines = [`curl -X ${r.method ?? 'POST'} ${q(r.url)}`]
  for (const [k, v] of Object.entries(r.headers ?? {})) lines.push(`  -H ${q(`${k}: ${v}`)}`)
  if (r.body !== undefined) lines.push(`  --data ${q(typeof r.body === 'string' ? r.body : JSON.stringify(r.body))}`)
  return lines.join(' \\\n')
}

function Transcript({ messages }: { messages: NonNullable<Result['transcript']> }) {
  return (
    <ol className="flex flex-col gap-3">
      {messages.map((m, i) => {
        const Icon = m.role === 'user' ? User : m.role === 'tool' ? Wrench : Bot
        return (
          <li key={i} className={cn('flex gap-3', m.role === 'system' && 'opacity-80')}>
            <span className="mt-0.5 flex size-6 shrink-0 items-center justify-center rounded-full border border-border bg-surface-2 text-text-2">
              <Icon className="size-3.5" aria-hidden />
            </span>
            <div className="min-w-0 flex-1">
              <div className="text-xs font-medium capitalize text-text-muted">{m.role}</div>
              <p className={cn('mt-0.5 whitespace-pre-wrap text-sm leading-6', m.role === 'assistant' ? 'text-text' : 'text-text-2')}>{m.content}</p>
              {m.tool_calls?.map((t, k) => (
                <Badge key={k} variant="outline" mono className="mt-1">
                  {t.name}()
                </Badge>
              ))}
            </div>
          </li>
        )
      })}
    </ol>
  )
}

function TraceTimeline({ steps }: { steps: NonNullable<Result['trace']> }) {
  const starts = steps.map((s) => (s.started_at ? Date.parse(s.started_at) : 0))
  const t0 = Math.min(...starts)
  const ends = steps.map((s, i) => starts[i]! + (s.duration ?? 0) / 1e6)
  const span = Math.max(1, Math.max(...ends) - t0)
  const kindIcon = { llm: Bot, tool: Wrench, retrieval: FileSearch, custom: DatabaseZap }
  return (
    <ol className="flex flex-col gap-1">
      {steps.map((s, i) => {
        const Icon = kindIcon[s.kind]
        const left = ((starts[i]! - t0) / span) * 100
        const width = Math.max(0.8, (((s.duration ?? 0) / 1e6) / span) * 100)
        return (
          <li key={i}>
            <details className="group rounded-md border border-transparent open:border-border open:bg-surface-2">
              <summary className="grid cursor-pointer list-none grid-cols-[minmax(0,180px)_1fr_64px] items-center gap-3 rounded-md px-2 py-1.5 hover:bg-surface-hover focus-ring">
                <span className="flex min-w-0 items-center gap-2 text-sm">
                  <Icon className="size-3.5 shrink-0 text-text-muted" aria-hidden />
                  <span className="truncate text-text">{s.name ?? s.kind}</span>
                </span>
                <span className="relative h-2 rounded-full bg-surface-2 group-open:bg-surface" aria-hidden>
                  <span className="absolute inset-y-0 rounded-full bg-accent" style={{ left: `${left}%`, width: `${width}%` }} />
                </span>
                <span className="tabular text-right text-xs text-text-2">{formatDuration((s.duration ?? 0) / 1e6)}</span>
              </summary>
              <div className="grid grid-cols-1 gap-2 px-2 pb-2 sm:grid-cols-2">
                <JsonViewer value={s.input ?? null} rootLabel="input" maxHeight={220} defaultExpandDepth={1} />
                <JsonViewer value={s.output ?? null} rootLabel="output" maxHeight={220} defaultExpandDepth={1} />
              </div>
            </details>
          </li>
        )
      })}
    </ol>
  )
}

function AddToDataset({ resultId, projectId }: { resultId: string; projectId: string }) {
  const datasets = useQuery(datasetsQuery(projectId))
  const add = useAddToDataset()
  const [ds, setDs] = useState<string>()
  const [useOutput, setUseOutput] = useState(false)
  const [open, setOpen] = useState(false)
  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button size="sm" icon={<DatabaseZap />}>
          Add to dataset
        </Button>
      </PopoverTrigger>
      <PopoverContent className="w-72" align="end" side="top">
        <div className="flex flex-col gap-3">
          <Select
            size="sm"
            aria-label="Dataset"
            placeholder={datasets.isPending ? 'Loading…' : datasets.data?.length ? 'Choose a dataset' : 'No datasets'}
            value={ds}
            onValueChange={setDs}
            options={(datasets.data ?? []).map((d) => ({ value: d.id, label: d.name }))}
          />
          <label className="flex items-center gap-2 text-sm text-text-2">
            <Checkbox checked={useOutput} onCheckedChange={(v) => setUseOutput(v === true)} />
            Use this output as the expected value
          </label>
          <Button
            size="sm"
            variant="primary"
            disabled={!ds}
            loading={add.isPending}
            onClick={() =>
              add.mutate(
                { datasetId: ds!, resultIds: [resultId], useOutput },
                {
                  onSuccess: () => {
                    toast.success('Added to the dataset draft')
                    setOpen(false)
                  },
                  onError: (e) => toast.error(e instanceof ApiError ? e.message : 'Could not add to dataset'),
                },
              )
            }
          >
            Add example
          </Button>
        </div>
      </PopoverContent>
    </Popover>
  )
}

function SendToReview({ resultId, projectId }: { resultId: string; projectId: string }) {
  const queues = useQuery(reviewQueuesQuery(projectId))
  const send = useSendToReview()
  const [open, setOpen] = useState(false)
  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button size="sm" icon={<ClipboardCheck />}>
          Send to review
        </Button>
      </PopoverTrigger>
      <PopoverContent className="w-64 p-1" align="end" side="top">
        {queues.isPending ? (
          <SkeletonText lines={2} className="p-2" />
        ) : !queues.data?.length ? (
          <p className="p-3 text-sm text-text-muted">No review queues yet. Create one under Review.</p>
        ) : (
          queues.data.map((q) => (
            <button
              key={q.id}
              type="button"
              className="flex w-full items-center justify-between gap-2 rounded-sm px-2 py-1.5 text-left text-sm text-text hover:bg-surface-hover focus-ring"
              onClick={() =>
                send.mutate(
                  { queueId: q.id, resultIds: [resultId] },
                  {
                    onSuccess: () => {
                      toast.success(`Sent to ${q.name}`)
                      setOpen(false)
                    },
                    onError: (e) => toast.error(e instanceof ApiError ? e.message : 'Could not send to review'),
                  },
                )
              }
            >
              <span className="truncate">{q.name}</span>
              <span className="tabular text-xs text-text-muted">{q.pending_count} pending</span>
            </button>
          ))
        )}
      </PopoverContent>
    </Popover>
  )
}

export interface ResultDrawerProps {
  runId: string
  resultId: string | undefined
  summary: Result | undefined
  tab: DrawerTab
  evaluators: EvalColumn[]
  onTab: (t: DrawerTab) => void
  onClose: () => void
  onPrev?: () => void
  onNext?: () => void
}

export function ResultDrawer({ runId, resultId, summary, tab, evaluators, onTab, onClose, onPrev, onNext }: ResultDrawerProps) {
  const { project } = useWorkspace()
  const full = useQuery(resultQuery(runId, resultId))
  const r = full.data ?? summary
  const [view, setView] = useState<'split' | 'diff'>('split')
  const [transcriptView, setTranscriptView] = useState(true)
  useHotkeys({ j: () => onNext?.(), k: () => onPrev?.() }, !!resultId)

  const curl = toCurl(full.data?.request)
  const evName = (id: string) => evaluators.find((e) => e.id === id)?.name ?? 'Evaluator'
  const threshold = (id: string) => evaluators.find((e) => e.id === id)?.threshold

  return (
    <Drawer
      open={!!resultId}
      onOpenChange={(o) => !o && onClose()}
      size="xl"
      title={
        <span className="flex items-center gap-2">
          {r && <StatusPill status={resultStatus(r)} size="sm" />}
          <span>Result</span>
          {r && <span className="font-mono text-sm font-normal text-text-muted">{shortId(r.example_id)}</span>}
          {r && r.trial > 0 && <Badge variant="outline">trial {r.trial + 1}</Badge>}
        </span>
      }
      description={
        r
          ? [
              r.cached ? 'cached' : formatDuration(r.latency_ms),
              r.ttft_ms ? `TTFT ${formatDuration(r.ttft_ms)}` : null,
              `${formatCompact(r.tokens_in)} in / ${formatCompact(r.tokens_out)} out`,
              formatUsd(r.cost_usd),
              r.tags.length ? r.tags.join(', ') : null,
            ]
              .filter(Boolean)
              .join(' · ')
          : 'Loading…'
      }
      actions={
        <>
          <Tooltip content="Previous" shortcut={['K']}>
            <IconButton label="Previous result" size="sm" disabled={!onPrev} onClick={onPrev}>
              <ArrowUp />
            </IconButton>
          </Tooltip>
          <Tooltip content="Next" shortcut={['J']}>
            <IconButton label="Next result" size="sm" disabled={!onNext} onClick={onNext}>
              <ArrowDown />
            </IconButton>
          </Tooltip>
        </>
      }
      footer={
        r && project ? (
          <div className="flex w-full flex-wrap items-center justify-end gap-2">
            <Button
              size="sm"
              variant="ghost"
              icon={<Copy />}
              disabled={!curl}
              onClick={() => {
                if (!curl) return
                void navigator.clipboard.writeText(curl).then(() => toast.success('cURL copied'))
              }}
            >
              Copy as cURL
            </Button>
            <SendToReview resultId={r.id} projectId={project.id} />
            <AddToDataset resultId={r.id} projectId={project.id} />
          </div>
        ) : undefined
      }
    >
      {!r ? (
        <SkeletonText lines={8} />
      ) : (
        <Tabs value={tab} onValueChange={(v) => onTab(v as DrawerTab)}>
          <TabsList className="-mt-2">
            <TabsTrigger value="output">Output</TabsTrigger>
            <TabsTrigger value="scores">
              Scores <span className="tabular text-xs text-text-muted">{r.scores.length}</span>
            </TabsTrigger>
            <TabsTrigger value="context">Context</TabsTrigger>
            <TabsTrigger value="trace">Trace</TabsTrigger>
            <TabsTrigger value="raw">Raw</TabsTrigger>
          </TabsList>

          <TabsContent value="output" className="flex flex-col gap-5">
            <Section title="Input">
              {r.input && Object.keys(r.input).length > 1 ? (
                <JsonViewer value={r.input} defaultExpandDepth={1} maxHeight={220} />
              ) : (
                <TextPanel value={text(r.input && Object.values(r.input)[0])} />
              )}
            </Section>
            {r.error && (
              <Section title="Error">
                <TextPanel value={r.error} tone="fail" />
              </Section>
            )}
            {full.data?.transcript?.length && transcriptView ? (
              <Section
                title="Conversation"
                actions={
                  <Button size="sm" variant="ghost" onClick={() => setTranscriptView(false)}>
                    Show final output
                  </Button>
                }
              >
                <div className="rounded-md border border-border p-4">
                  <Transcript messages={full.data.transcript} />
                </div>
              </Section>
            ) : (
              <Section
                title={r.expected != null ? 'Expected vs output' : 'Output'}
                actions={
                  <div className="flex items-center gap-2">
                    {full.data?.transcript?.length ? (
                      <Button size="sm" variant="ghost" onClick={() => setTranscriptView(true)}>
                        Show conversation
                      </Button>
                    ) : null}
                    {r.expected != null && (
                      <SegmentedControl<'split' | 'diff'>
                        size="sm"
                        aria-label="Comparison view"
                        value={view}
                        onValueChange={setView}
                        options={[
                          { value: 'split', label: 'Side by side' },
                          { value: 'diff', label: 'Diff' },
                        ]}
                      />
                    )}
                  </div>
                }
              >
                {r.expected == null ? (
                  <TextPanel value={r.output} />
                ) : view === 'diff' ? (
                  <DiffViewer oldValue={text(r.expected)} newValue={r.output} showViewToggle={false} maxHeight={360} />
                ) : (
                  <div className="grid grid-cols-1 gap-3 md:grid-cols-2">
                    <div className="flex min-w-0 flex-col gap-1">
                      <span className="text-xs text-text-muted">Expected</span>
                      <TextPanel value={text(r.expected)} />
                    </div>
                    <div className="flex min-w-0 flex-col gap-1">
                      <span className="text-xs text-text-muted">Output</span>
                      <TextPanel value={r.output} />
                    </div>
                  </div>
                )}
              </Section>
            )}
            <Section title="Scores">
              <div className="flex flex-wrap gap-2">
                {r.scores.map((s) => (
                  <button
                    key={s.evaluator_id}
                    type="button"
                    onClick={() => onTab('scores')}
                    className="flex items-center gap-2 rounded-md border border-border px-2 py-1 text-sm text-text-2 hover:border-border-strong focus-ring"
                  >
                    {evName(s.evaluator_id)}
                    <ScoreChip size="sm" value={s.value ?? null} passed={s.passed ?? undefined} threshold={threshold(s.evaluator_id)} />
                  </button>
                ))}
              </div>
            </Section>
          </TabsContent>

          <TabsContent value="scores" className="flex flex-col gap-3">
            {r.scores.length === 0 && <p className="text-sm text-text-muted">No scores for this result.</p>}
            {r.scores.map((s) => (
              <article key={s.evaluator_id} className="rounded-md border border-border p-4">
                <header className="flex flex-wrap items-center justify-between gap-2">
                  <div className="flex items-center gap-2">
                    <span className="text-sm font-semibold text-text">{evName(s.evaluator_id)}</span>
                    <ScoreChip value={s.value ?? null} passed={s.passed ?? undefined} threshold={threshold(s.evaluator_id)} />
                    {threshold(s.evaluator_id) != null && <span className="tabular text-xs text-text-muted">threshold {threshold(s.evaluator_id)!.toFixed(2)}</span>}
                  </div>
                  <span className="tabular text-xs text-text-muted">
                    {s.latency_ms != null ? formatDuration(s.latency_ms) : ''}
                    {s.cost_usd ? ` · ${formatUsd(s.cost_usd)}` : ''}
                  </span>
                </header>
                {s.reason && <p className="mt-2 whitespace-pre-wrap text-sm leading-6 text-text-2">{s.reason}</p>}
              </article>
            ))}
          </TabsContent>

          <TabsContent value="context" className="flex flex-col gap-2">
            {full.isPending ? (
              <SkeletonText lines={4} />
            ) : !full.data?.context?.length ? (
              <p className="text-sm text-text-muted">No retrieved context was recorded for this result.</p>
            ) : (
              <ol className="flex flex-col gap-2">
                {full.data.context.map((c, i) => (
                  <li key={i} className="flex gap-3 rounded-md border border-border p-3">
                    <span className="tabular flex size-5 shrink-0 items-center justify-center rounded-sm bg-surface-2 text-xs text-text-muted">{i + 1}</span>
                    <p className="whitespace-pre-wrap text-sm leading-6 text-text">{c}</p>
                  </li>
                ))}
              </ol>
            )}
          </TabsContent>

          <TabsContent value="trace" className="flex flex-col gap-5">
            {full.isPending ? (
              <SkeletonText lines={4} />
            ) : (
              <>
                {full.data?.trace?.length ? (
                  <Section title="Timeline">
                    <TraceTimeline steps={full.data.trace} />
                  </Section>
                ) : null}
                {full.data?.tool_calls?.length ? (
                  <Section title="Tool calls">
                    <div className="flex flex-col gap-2">
                      {full.data.tool_calls.map((t, i) => (
                        <div key={i} className="rounded-md border border-border p-3">
                          <div className="mb-2 flex items-center gap-2 text-sm font-medium text-text">
                            <Wrench className="size-3.5 text-text-muted" aria-hidden />
                            <span className="font-mono">{t.name}</span>
                          </div>
                          <div className="grid grid-cols-1 gap-2 sm:grid-cols-2">
                            <JsonViewer value={t.arguments ?? null} rootLabel="arguments" maxHeight={200} />
                            <JsonViewer value={t.result ?? null} rootLabel="result" maxHeight={200} />
                          </div>
                        </div>
                      ))}
                    </div>
                  </Section>
                ) : null}
                {full.data?.transcript?.length ? (
                  <Section title="Transcript">
                    <Transcript messages={full.data.transcript} />
                  </Section>
                ) : null}
                {!full.data?.trace?.length && !full.data?.tool_calls?.length && !full.data?.transcript?.length && (
                  <p className="text-sm text-text-muted">No trace was recorded. Agent targets that return steps show a timeline here.</p>
                )}
              </>
            )}
          </TabsContent>

          <TabsContent value="raw" className="flex flex-col gap-5">
            {full.isPending ? (
              <SkeletonText lines={6} />
            ) : (
              <>
                <Section title="Request">
                  {full.data?.request != null ? <CodeBlock code={JSON.stringify(full.data.request, null, 2)} language="json" maxHeight={320} /> : <p className="text-sm text-text-muted">Not recorded.</p>}
                </Section>
                <Section title="Response">
                  {full.data?.response != null ? <CodeBlock code={JSON.stringify(full.data.response, null, 2)} language="json" maxHeight={320} /> : <p className="text-sm text-text-muted">Not recorded.</p>}
                </Section>
                {curl && (
                  <Section title="cURL">
                    <CodeBlock code={curl} language="bash" maxHeight={200} />
                  </Section>
                )}
              </>
            )}
          </TabsContent>
        </Tabs>
      )}
    </Drawer>
  )
}
