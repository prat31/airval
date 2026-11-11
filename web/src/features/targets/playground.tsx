import { CircleCheck, CircleX, Play, TriangleAlert } from 'lucide-react'
import { useMemo, useState } from 'react'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardBody, CardHeader } from '@/components/ui/card'
import { CodeBlock } from '@/components/ui/code-block'
import { Combobox } from '@/components/ui/combobox'
import { Field } from '@/components/ui/field'
import { JsonViewer } from '@/components/ui/json-viewer'
import { SegmentedControl } from '@/components/ui/segmented-control'
import { Skeleton } from '@/components/ui/skeleton'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Textarea } from '@/components/ui/input'
import { cn } from '@/lib/cn'
import { formatDuration, formatUsd } from '@/lib/format'
import { errorMessage, type Obj, type S } from '../shared/api'
import { useDatasets, useExamples } from '../datasets/api'
import { isObj, redact } from '../shared/obj'
import { KvEditor, pretty, tryParse } from '../shared/ui'
import { useTestDraft } from './api'
import { isHttpLike, type TargetKind } from './kinds'

const SAMPLE = '{\n  "question": "What is the refund window?"\n}'

function preview(v: unknown, max = 120): string {
  const s = typeof v === 'string' ? v : JSON.stringify(v)
  return s.length > max ? `${s.slice(0, max)}…` : s
}

/** Per-path match table: each configured JSONPath evaluated against the raw response. */
function MatchTable({ mapping }: { mapping: NonNullable<S['TargetTestResult']['mapping']> }) {
  const rows = mapping.map((m) => [m.field, m] as const)
  if (!rows.length) return <p className="px-4 py-6 text-center text-sm text-text-muted">No response mapping configured for this target kind.</p>
  return (
    <table className="w-full text-sm">
      <thead>
        <tr className="border-b border-border text-left text-xs text-text-muted">
          <th className="px-4 py-2 font-medium">Field</th>
          <th className="px-2 py-2 font-medium">Path</th>
          <th className="px-2 py-2 font-medium">Result</th>
          <th className="px-4 py-2 font-medium">Value</th>
        </tr>
      </thead>
      <tbody>
        {rows.map(([field, m]) => {
          const path = m.path
          const status = m.matched ? 'match' : 'none'
          return (
            <tr key={field} className="border-b border-border last:border-b-0 align-top">
              <td className="px-4 py-2 font-medium text-text">{field.replace(/_/g, ' ')}</td>
              <td className="px-2 py-2 font-mono text-xs text-text-2">{String(path)}</td>
              <td className="px-2 py-2">
                {status === 'match' ? (
                  <Badge variant="pass">
                    <CircleCheck /> Match
                  </Badge>
                ) : (
                  <Badge variant="warn">
                    <TriangleAlert /> No match
                  </Badge>
                )}
              </td>
              <td className="max-w-80 px-4 py-2 font-mono text-xs text-text-2">
                <span className="line-clamp-2 break-all">{status === 'match' ? preview(m.value) : '—'}</span>
              </td>
            </tr>
          )
        })}
      </tbody>
    </table>
  )
}

function MappedOutput({ r }: { r: S['TargetResponse'] }) {
  return (
    <div className="flex flex-col gap-4 p-4">
      <div>
        <div className="mb-1 text-xs font-medium text-text-muted">Output</div>
        <div className="whitespace-pre-wrap rounded-md border border-border bg-surface-2 p-3 text-sm text-text">{r.output || <span className="text-text-muted">(empty)</span>}</div>
      </div>
      {r.context && r.context.length > 0 && (
        <div>
          <div className="mb-1 text-xs font-medium text-text-muted">Context ({r.context.length})</div>
          <ul className="flex flex-col gap-1.5">
            {r.context.map((c, i) => (
              <li key={i} className="rounded-md border border-border px-3 py-2 text-sm text-text-2">
                {c}
              </li>
            ))}
          </ul>
        </div>
      )}
      {r.tool_calls && r.tool_calls.length > 0 && (
        <div>
          <div className="mb-1 text-xs font-medium text-text-muted">Tool calls ({r.tool_calls.length})</div>
          <JsonViewer value={r.tool_calls} defaultExpandDepth={2} />
        </div>
      )}
      {r.trace && r.trace.length > 0 && (
        <div>
          <div className="mb-1 text-xs font-medium text-text-muted">Trace ({r.trace.length} steps)</div>
          <JsonViewer value={r.trace} defaultExpandDepth={1} />
        </div>
      )}
      <div className="flex flex-wrap gap-x-6 gap-y-1 text-xs text-text-2">
        {r.latency_ms !== undefined && <span>Latency {formatDuration(r.latency_ms)}</span>}
        {r.ttft_ms !== undefined && r.ttft_ms > 0 && <span>First token {formatDuration(r.ttft_ms)}</span>}
        {r.usage && (
          <span className="tabular">
            {r.usage.input_tokens} in / {r.usage.output_tokens} out tokens
          </span>
        )}
        {r.cost_usd !== undefined && r.cost_usd > 0 && <span>{formatUsd(r.cost_usd)}</span>}
        {r.cached && <Badge variant="neutral">cached</Badge>}
      </div>
    </div>
  )
}

export function Playground({ kind, config, projectId, disabledReason }: { kind: TargetKind; config: Obj; projectId?: string; disabledReason?: string }) {
  const [mode, setMode] = useState<'sample' | 'paste'>('paste')
  const [datasetId, setDatasetId] = useState<string | null>(null)
  const [exampleId, setExampleId] = useState<string | null>(null)
  const [paste, setPaste] = useState(SAMPLE)
  const [vars, setVars] = useState<Record<string, string>>({})
  const test = useTestDraft(projectId)
  const datasets = useDatasets(projectId)
  const examples = useExamples(mode === 'sample' ? (datasetId ?? undefined) : undefined)
  const exampleList = useMemo(() => examples.data?.pages.flatMap((p) => p.items) ?? [], [examples.data])

  const parsed = tryParse(paste)
  const example: S['ExampleData'] | undefined = useMemo(() => {
    if (mode === 'sample') {
      const ex = exampleList.find((e) => e.id === exampleId)
      return ex ? { input: ex.input, expected: ex.expected, context: ex.context, metadata: ex.metadata, tags: ex.tags } : undefined
    }
    return parsed.ok && isObj(parsed.value) ? { input: parsed.value } : undefined
  }, [mode, exampleList, exampleId, parsed])

  const run = () => test.mutate({ kind, config, example, variables: Object.keys(vars).length ? vars : undefined })
  const result = test.data
  const rawParsed = useMemo(() => {
    const raw = result?.raw_response
    if (typeof raw === 'string') {
      const p = tryParse(raw)
      return p.ok ? p.value : raw
    }
    return raw
  }, [result])

  const canRun = !disabledReason && (mode === 'paste' ? parsed.ok && isObj(parsed.value) : !!example)

  return (
    <div className="grid grid-cols-1 gap-3 lg:grid-cols-[minmax(0,2fr)_minmax(0,3fr)]">
      <Card className="self-start">
        <CardHeader title="Test input" description="Send one example through the current (unsaved) configuration." />
        <CardBody className="flex flex-col gap-4">
          <SegmentedControl
            aria-label="Input source"
            value={mode}
            onValueChange={setMode}
            options={[
              { value: 'paste', label: 'Paste input' },
              { value: 'sample', label: 'Sample example' },
            ]}
          />
          {mode === 'paste' ? (
            <Field label="Input (JSON)" hint="Keys become {{ .input.key }} in your templates." error={!parsed.ok ? parsed.error : parsed.ok && parsed.value !== undefined && !isObj(parsed.value) ? 'Input must be a JSON object' : undefined}>
              <Textarea mono rows={7} spellCheck={false} value={paste} onChange={(e) => setPaste(e.target.value)} />
            </Field>
          ) : (
            <div className="flex flex-col gap-3">
              <Field label="Dataset">
                <Combobox
                  options={(datasets.data ?? []).map((d) => ({ value: d.id, label: d.name, description: `${d.example_count} examples` }))}
                  value={datasetId}
                  onValueChange={(v) => {
                    setDatasetId(v)
                    setExampleId(null)
                  }}
                  placeholder={datasets.isLoading ? 'Loading…' : 'Choose a dataset'}
                  emptyText="No datasets yet"
                />
              </Field>
              <Field label="Example">
                <Combobox
                  options={exampleList.map((e, i) => ({ value: e.id, label: preview(Object.values(e.input)[0] ?? `Example ${i + 1}`, 70), description: (e.tags ?? []).join(', ') || undefined }))}
                  value={exampleId}
                  onValueChange={setExampleId}
                  placeholder={!datasetId ? 'Pick a dataset first' : examples.isLoading ? 'Loading…' : 'Choose an example'}
                  disabled={!datasetId}
                  emptyText="This dataset has no examples"
                />
              </Field>
            </div>
          )}
          <Field label="Variable overrides" optional hint="Override {{ .vars.* }} for this test only.">
            <KvEditor value={vars} onChange={setVars} keyPlaceholder="name" valuePlaceholder="value" addLabel="Add override" monoValue />
          </Field>
          <div className="flex items-center gap-3">
            <Button variant="primary" icon={<Play />} loading={test.isPending} disabled={!canRun} onClick={run}>
              Run test
            </Button>
            {disabledReason && <span className="text-xs text-text-muted">{disabledReason}</span>}
          </div>
        </CardBody>
      </Card>

      <Card className="min-h-80 min-w-0">
        <CardHeader
          title="Result"
          actions={
            result ? (
              <span className="flex items-center gap-2 text-xs text-text-2">
                {result.status_code !== undefined && result.status_code > 0 && <Badge variant={result.status_code < 400 ? 'pass' : 'fail'} mono>{result.status_code}</Badge>}
                {result.ok ? (
                  <span className="inline-flex items-center gap-1 text-pass"><CircleCheck className="size-3.5" /> Connected</span>
                ) : (
                  <span className="inline-flex items-center gap-1 text-fail"><CircleX className="size-3.5" /> Failed</span>
                )}
              </span>
            ) : undefined
          }
        />
        {test.isPending ? (
          <CardBody className="flex flex-col gap-3">
            <Skeleton className="h-4 w-1/3" />
            <Skeleton className="h-24 w-full" />
            <Skeleton className="h-4 w-1/2" />
          </CardBody>
        ) : test.error ? (
          <CardBody>
            <div className="flex items-start gap-2 rounded-md border border-fail/30 bg-fail-subtle p-3 text-sm text-fail" role="alert">
              <CircleX className="mt-0.5 size-4 shrink-0" />
              <span>{errorMessage(test.error)}</span>
            </div>
          </CardBody>
        ) : !result ? (
          <div className="flex flex-col items-center gap-1 px-6 py-16 text-center">
            <p className="text-sm font-medium text-text">Nothing sent yet</p>
            <p className="max-w-xs text-xs text-text-muted">Run a test to see the redacted request, the raw response, the mapped output and what each JSONPath matched.</p>
          </div>
        ) : (
          <Tabs defaultValue={result.ok ? 'mapped' : 'raw'} className="mt-1">
            <div className="px-4">
              <TabsList>
                <TabsTrigger value="mapped">Mapped output</TabsTrigger>
                <TabsTrigger value="request">Request</TabsTrigger>
                <TabsTrigger value="raw">Raw response</TabsTrigger>
                {isHttpLike(kind) && <TabsTrigger value="match">Path matches</TabsTrigger>}
              </TabsList>
            </div>
            {!result.ok && result.error && (
              <div className="mx-4 mt-3 flex items-start gap-2 rounded-md border border-fail/30 bg-fail-subtle p-3 text-sm text-fail" role="alert">
                <CircleX className="mt-0.5 size-4 shrink-0" />
                <span className="min-w-0 break-words">{result.error}</span>
              </div>
            )}
            <TabsContent value="mapped" className={cn('focus:outline-none')}>
              {result.response ? <MappedOutput r={result.response} /> : <p className="px-4 py-6 text-sm text-text-muted">No output could be mapped. Check the response mapping.</p>}
            </TabsContent>
            <TabsContent value="request" className="p-4">
              <p className="mb-2 text-xs text-text-muted">Credentials and secret values are redacted.</p>
              {result.request !== undefined ? <CodeBlock code={pretty(redact(result.request))} language="json" maxHeight={360} /> : <p className="text-sm text-text-muted">No request was recorded.</p>}
            </TabsContent>
            <TabsContent value="raw" className="p-4">
              {result.raw_response === undefined ? (
                <p className="text-sm text-text-muted">No response body.</p>
              ) : isObj(rawParsed) || Array.isArray(rawParsed) ? (
                <JsonViewer value={rawParsed} defaultExpandDepth={3} maxHeight={360} />
              ) : (
                <CodeBlock code={pretty(rawParsed)} language="text" maxHeight={360} />
              )}
            </TabsContent>
            {isHttpLike(kind) && (
              <TabsContent value="match">
                <MatchTable mapping={result.mapping ?? []} />
              </TabsContent>
            )}
          </Tabs>
        )}
      </Card>
    </div>
  )
}
