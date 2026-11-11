/* eslint-disable react-hooks/set-state-in-effect -- local drafts are seeded from server data */
import { CircleCheck, FileUp, Loader2, Sparkles, TriangleAlert, Upload } from 'lucide-react'
import { useCallback, useEffect, useMemo, useRef, useState, type DragEvent } from 'react'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Dialog } from '@/components/ui/dialog'
import { Field } from '@/components/ui/field'
import { Input, Textarea } from '@/components/ui/input'
import { SegmentedControl } from '@/components/ui/segmented-control'
import { Select } from '@/components/ui/select'
import { Stepper } from '@/components/ui/stepper'
import { Switch } from '@/components/ui/switch'
import { cn } from '@/lib/cn'
import { errorMessage, type S } from '../shared/api'
import { TagInput, useDebounced } from '../shared/ui'
import { importFile, importJson, useRefreshDraft, type ImportRequest } from './api'

type Format = S['DatasetImportFormat']
type SourceMode = 'upload' | 'paste' | 'huggingface'
type Mapping = NonNullable<ImportRequest['mapping']>

const formats: { value: Format; label: string }[] = [
  { value: 'csv', label: 'CSV' },
  { value: 'jsonl', label: 'JSONL' },
  { value: 'json', label: 'JSON' },
  { value: 'parquet', label: 'Parquet' },
]

export function detectFormat(name: string): Format {
  const ext = name.toLowerCase().split('.').pop() ?? ''
  if (ext === 'jsonl' || ext === 'ndjson') return 'jsonl'
  if (ext === 'json') return 'json'
  if (ext === 'parquet') return 'parquet'
  return 'csv'
}

const norm = (s: string) => s.toLowerCase().replace(/[^a-z0-9]/g, '')
const INPUT = ['input', 'question', 'prompt', 'query', 'instruction', 'text', 'message', 'utterance']
const EXPECTED = ['expected', 'answer', 'target', 'reference', 'groundtruth', 'label', 'output', 'response', 'completion', 'solution']
const CONTEXT = ['context', 'contexts', 'documents', 'passages', 'retrieved', 'docs', 'sources', 'evidence']
const TAGS = ['tags', 'tag', 'category', 'topic', 'type', 'difficulty']

/** Heuristic column mapping: input / expected / context / tags by name, the rest to metadata. */
export function suggestMapping(columns: string[]): Mapping {
  const find = (names: string[], used: Set<string>) => columns.find((c) => !used.has(c) && names.includes(norm(c)))
  const used = new Set<string>()
  const input = find(INPUT, used)
  if (input) used.add(input)
  const expected = find(EXPECTED, used)
  if (expected) used.add(expected)
  const context = find(CONTEXT, used)
  if (context) used.add(context)
  const tags = find(TAGS, used)
  if (tags) used.add(tags)
  const inputs = input ? [input] : columns.length ? [columns[0]!] : []
  if (!input && columns[0]) used.add(columns[0])
  return { input_columns: Object.fromEntries(inputs.map((c) => [c, c])), expected_column: expected, context_column: context, tags_column: tags, metadata_columns: columns.filter((c) => !used.has(c)) }
}

function Dropzone({ file, onFile }: { file: File | null; onFile: (f: File) => void }) {
  const input = useRef<HTMLInputElement>(null)
  const [over, setOver] = useState(false)
  const onDrop = (e: DragEvent) => {
    e.preventDefault()
    setOver(false)
    const f = e.dataTransfer.files[0]
    if (f) onFile(f)
  }
  return (
    <button
      type="button"
      onClick={() => input.current?.click()}
      onDragOver={(e) => {
        e.preventDefault()
        setOver(true)
      }}
      onDragLeave={() => setOver(false)}
      onDrop={onDrop}
      className={cn(
        'flex w-full flex-col items-center justify-center gap-2 rounded-md border border-dashed px-6 py-10 text-center transition-colors duration-150 focus-ring',
        over ? 'border-accent bg-accent-subtle' : 'border-border-strong bg-surface-2/40 hover:bg-surface-2',
      )}
    >
      <span className="flex size-10 items-center justify-center rounded-md border border-border bg-surface text-text-2">{file ? <FileUp className="size-5" /> : <Upload className="size-5" />}</span>
      {file ? (
        <>
          <span className="text-sm font-medium text-text">{file.name}</span>
          <span className="text-xs text-text-muted">{(file.size / 1024).toFixed(file.size > 102400 ? 0 : 1)} KB · click to replace</span>
        </>
      ) : (
        <>
          <span className="text-sm font-medium text-text">Drop a file here, or click to browse</span>
          <span className="text-xs text-text-muted">CSV, JSONL, JSON or Parquet</span>
        </>
      )}
      <input ref={input} type="file" accept=".csv,.jsonl,.ndjson,.json,.parquet" className="sr-only" tabIndex={-1} onChange={(e) => e.target.files?.[0] && onFile(e.target.files[0])} />
    </button>
  )
}

function ColumnPicker({ label, columns, value, onChange, multiple, hint }: { label: string; columns: string[]; value: string[]; onChange: (v: string[]) => void; multiple?: boolean; hint?: string }) {
  return (
    <div className="flex flex-col gap-1.5">
      <div className="flex items-baseline justify-between gap-2">
        <span className="text-sm font-medium text-text">{label}</span>
        {hint && <span className="text-xs text-text-muted">{hint}</span>}
      </div>
      <div className="flex flex-wrap gap-1">
        {columns.map((c) => {
          const on = value.includes(c)
          return (
            <button
              key={c}
              type="button"
              aria-pressed={on}
              onClick={() => onChange(multiple ? (on ? value.filter((x) => x !== c) : [...value, c]) : on ? [] : [c])}
              className={cn(
                'inline-flex h-6 items-center rounded-sm px-2 font-mono text-xs ring-1 ring-inset transition-colors focus-ring',
                on ? 'bg-accent-subtle text-accent ring-accent' : 'text-text-2 ring-border hover:bg-surface-hover',
              )}
            >
              {c}
            </button>
          )
        })}
      </div>
    </div>
  )
}

const short = (v: unknown, n = 90) => {
  const s = v === undefined || v === null ? '' : typeof v === 'string' ? v : JSON.stringify(v)
  return s.length > n ? `${s.slice(0, n)}…` : s
}

export function ImportWizard({ open, onOpenChange, datasetId, onDone }: { open: boolean; onOpenChange: (o: boolean) => void; datasetId: string; onDone?: () => void }) {
  const refresh = useRefreshDraft(datasetId)
  const [step, setStep] = useState(0)
  const [mode, setMode] = useState<SourceMode>('upload')
  const [file, setFile] = useState<File | null>(null)
  const [format, setFormat] = useState<Format>('csv')
  const [pasted, setPasted] = useState('')
  const [hfRepo, setHfRepo] = useState('')
  const [hfConfig, setHfConfig] = useState('')
  const [hfSplit, setHfSplit] = useState('train')
  const [limit, setLimit] = useState('')

  const [columns, setColumns] = useState<string[]>([])
  const [mapping, setMapping] = useState<Mapping>({})
  const [suggested, setSuggested] = useState(true)
  const [writeMode, setWriteMode] = useState<'append' | 'replace'>('append')
  const [extraTags, setExtraTags] = useState<string[]>([])
  const [publish, setPublish] = useState(false)
  const [preview, setPreview] = useState<S['ExampleData'][] | null>(null)
  const [previewErrors, setPreviewErrors] = useState<S['DatasetImportResult']['errors']>([])
  const [busy, setBusy] = useState<'source' | 'preview' | 'import' | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [result, setResult] = useState<S['DatasetImportResult'] | null>(null)

  const reset = useCallback(() => {
    setStep(0)
    setFile(null)
    setPasted('')
    setColumns([])
    setMapping({})
    setPreview(null)
    setResult(null)
    setError(null)
    setBusy(null)
    setSuggested(true)
    setPublish(false)
    setExtraTags([])
  }, [])
  useEffect(() => {
    if (!open) reset()
  }, [open, reset])

  const source = useMemo((): ImportRequest['source'] => {
    const lim = limit ? Number(limit) : undefined
    switch (mode) {
      case 'upload':
        return { kind: 'upload', format, limit: lim, hf_split: 'train' }
      case 'paste':
        return { kind: 'inline', format, data: pasted, limit: lim, hf_split: 'train' }
      case 'huggingface':
        return { kind: 'huggingface', hf_repo: hfRepo.trim(), hf_config: hfConfig || undefined, hf_split: hfSplit || 'train', limit: lim }
    }
  }, [mode, format, pasted, hfRepo, hfConfig, hfSplit, limit])

  const sourceReady = mode === 'upload' ? !!file : mode === 'paste' ? pasted.trim().length > 0 : /^[\w.-]+\/[\w.-]+$/.test(hfRepo.trim())

  const send = useCallback(
    (req: ImportRequest) => (mode === 'upload' && file ? importFile(datasetId, file, { ...req, source: { kind: 'upload', format, limit: req.source?.limit, hf_split: 'train' } }) : importJson(datasetId, req)),
    [mode, file, datasetId, format],
  )

  const runPreview = async (m: Mapping | undefined, first: boolean) => {
    setBusy(first ? 'source' : 'preview')
    setError(null)
    try {
      const r = await send({ source, mapping: m, preview: true, mode: writeMode })
      if (first) {
        const cols = r.source_preview?.columns.map((c) => c.name) ?? r.columns ?? []
        setColumns(cols)
        const sug = r.suggested_mapping ?? suggestMapping(cols)
        setMapping(sug)
        setSuggested(true)
        setStep(1)
        // Re-preview with the suggested mapping so the table already reflects it.
        const r2 = await send({ source, mapping: sug, preview: true, mode: writeMode })
        setPreview(r2.preview ?? r.preview ?? [])
        setPreviewErrors(r2.errors ?? r.errors ?? [])
      } else {
        setPreview(r.preview ?? [])
        setPreviewErrors(r.errors ?? [])
      }
    } catch (e) {
      setError(errorMessage(e))
    } finally {
      setBusy(null)
    }
  }

  // Live re-preview after the user edits the mapping.
  const debouncedMapping = useDebounced(mapping, 400)
  const firstMap = useRef(true)
  useEffect(() => {
    if (step !== 1) {
      firstMap.current = true
      return
    }
    if (firstMap.current) {
      firstMap.current = false
      return
    }
    void runPreview(debouncedMapping, false)
  }, [debouncedMapping]) // eslint-disable-line react-hooks/exhaustive-deps

  const doImport = async () => {
    setBusy('import')
    setError(null)
    setStep(2)
    try {
      const r = await send({ source, mapping, mode: writeMode, tags: extraTags.length ? extraTags : undefined, publish })
      setResult(r)
      refresh()
      onDone?.()
    } catch (e) {
      setError(errorMessage(e))
    } finally {
      setBusy(null)
    }
  }

  const setMap = (patch: Partial<Mapping>) => {
    setSuggested(false)
    setMapping((m) => ({ ...m, ...patch }))
  }
  const inputCols = Object.keys(mapping.input_columns ?? {})
  const noInput = inputCols.length === 0 && !mapping.whole_row_as_input

  const steps = [
    { id: 'source', label: 'Source', description: 'File or reference' },
    { id: 'map', label: 'Map columns', description: 'Preview' },
    { id: 'import', label: 'Import', description: 'Result' },
  ]

  let footer
  if (step === 0)
    footer = (
      <>
        <Button variant="ghost" onClick={() => onOpenChange(false)}>
          Cancel
        </Button>
        <Button variant="primary" disabled={!sourceReady} loading={busy === 'source'} onClick={() => void runPreview(undefined, true)}>
          Preview
        </Button>
      </>
    )
  else if (step === 1)
    footer = (
      <>
        <Button variant="ghost" onClick={() => setStep(0)}>
          Back
        </Button>
        <Button variant="primary" disabled={noInput || busy !== null} onClick={() => void doImport()}>
          Import examples
        </Button>
      </>
    )
  else
    footer = (
      <>
        {error && !busy && (
          <Button variant="ghost" onClick={() => setStep(1)}>
            Back to mapping
          </Button>
        )}
        <Button variant="primary" disabled={busy === 'import'} onClick={() => onOpenChange(false)}>
          {busy === 'import' ? 'Importing…' : 'Done'}
        </Button>
      </>
    )

  return (
    <Dialog open={open} onOpenChange={onOpenChange} size="lg" title="Import examples" description="Add examples to the draft from a file, pasted data or a Hugging Face dataset." footer={footer}>
      <div className="flex flex-col gap-5">
        <Stepper steps={steps} current={step} />

        {step === 0 && (
          <div className="flex flex-col gap-4">
            <SegmentedControl
              aria-label="Source"
              value={mode}
              onValueChange={setMode}
              options={[
                { value: 'upload', label: 'Upload' },
                { value: 'paste', label: 'Paste' },
                { value: 'huggingface', label: 'Hugging Face' },
              ]}
            />
            {mode === 'upload' && (
              <Dropzone
                file={file}
                onFile={(f) => {
                  setFile(f)
                  setFormat(detectFormat(f.name))
                }}
              />
            )}
            {mode === 'paste' && (
              <Field label="Data">
                <Textarea mono rows={9} value={pasted} onChange={(e) => setPasted(e.target.value)} placeholder={'question,answer\nWhat is 2+2?,4'} spellCheck={false} />
              </Field>
            )}
            {mode === 'huggingface' && (
              <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
                <Field label="Dataset" hint="Public repo id" className="sm:col-span-3">
                  <Input value={hfRepo} onChange={(e) => setHfRepo(e.target.value)} placeholder="openai/gsm8k" />
                </Field>
                <Field label="Config" optional>
                  <Input value={hfConfig} onChange={(e) => setHfConfig(e.target.value)} placeholder="main" />
                </Field>
                <Field label="Split">
                  <Input value={hfSplit} onChange={(e) => setHfSplit(e.target.value)} />
                </Field>
              </div>
            )}
            <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
              {mode !== 'huggingface' && (
                <Field label="Format">
                  <Select value={format} onValueChange={(v) => setFormat(v as Format)} options={formats} />
                </Field>
              )}
              <Field label="Row limit" optional>
                <Input type="number" min={1} value={limit} onChange={(e) => setLimit(e.target.value)} placeholder="All rows" />
              </Field>
            </div>
            {error && <ErrorNote message={error} />}
          </div>
        )}

        {step === 1 && (
          <div className="flex flex-col gap-5">
            <div className="flex items-center gap-2">
              <h4 className="text-sm font-semibold text-text">Column mapping</h4>
              {suggested && (
                <Badge variant="accent">
                  <Sparkles /> Suggested
                </Badge>
              )}
            </div>
            <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
              <ColumnPicker label="Input" hint="One or more columns" columns={columns} multiple value={inputCols} onChange={(v) => setMap({ input_columns: Object.fromEntries(v.map((c) => [c, mapping.input_columns?.[c] ?? c])) })} />
              <ColumnPicker label="Expected" columns={columns} value={mapping.expected_column ? [mapping.expected_column] : []} onChange={(v) => setMap({ expected_column: v[0] })} />
              <ColumnPicker label="Context" columns={columns} value={mapping.context_column ? [mapping.context_column] : []} onChange={(v) => setMap({ context_column: v[0] })} />
              <ColumnPicker label="Tags" hint="Comma-separated" columns={columns} value={mapping.tags_column ? [mapping.tags_column] : []} onChange={(v) => setMap({ tags_column: v[0] })} />
              <ColumnPicker label="Metadata" columns={columns} multiple value={mapping.metadata_columns ?? []} onChange={(v) => setMap({ metadata_columns: v })} />
              <div className="flex items-start justify-between gap-3 rounded-md border border-border p-3">
                <div>
                  <div className="text-sm font-medium text-text">Whole row as input</div>
                  <p className="mt-0.5 text-xs text-text-muted">Put every unmapped column into input.</p>
                </div>
                <Switch aria-label="Unmapped columns to metadata" checked={!!mapping.whole_row_as_input} onCheckedChange={(v) => setMap({ whole_row_as_input: v })} />
              </div>
            </div>
            {noInput && <ErrorNote message="Choose at least one input column." />}

            <div className="flex flex-col gap-2">
              <h4 className="flex items-center gap-2 text-sm font-semibold text-text">
                Preview {busy === 'preview' && <Loader2 className="size-3.5 animate-spin text-text-muted" />}
              </h4>
              <div className="overflow-x-auto rounded-md border border-border">
                <table className="w-full min-w-[560px] text-left text-sm">
                  <thead className="bg-surface-2 text-xs text-text-muted">
                    <tr>
                      <th className="px-3 py-2 font-medium">Input</th>
                      <th className="px-3 py-2 font-medium">Expected</th>
                      <th className="px-3 py-2 font-medium">Context</th>
                      <th className="px-3 py-2 font-medium">Tags</th>
                    </tr>
                  </thead>
                  <tbody>
                    {(preview ?? []).slice(0, 6).map((ex, i) => (
                      <tr key={i} className="border-t border-border align-top">
                        <td className="max-w-64 px-3 py-2 font-mono text-xs text-text">{short(Object.keys(ex.input ?? {}).length === 1 ? Object.values(ex.input!)[0] : ex.input)}</td>
                        <td className="max-w-48 px-3 py-2 text-text-2">{short(ex.expected)}</td>
                        <td className="max-w-40 px-3 py-2 text-text-2">{ex.context?.length ? `${ex.context.length} passages` : ''}</td>
                        <td className="px-3 py-2">
                          <span className="flex flex-wrap gap-1">{(ex.tags ?? []).map((t) => <Badge key={t}>{t}</Badge>)}</span>
                        </td>
                      </tr>
                    ))}
                    {(preview ?? []).length === 0 && (
                      <tr>
                        <td colSpan={4} className="px-3 py-6 text-center text-sm text-text-muted">
                          No rows to preview.
                        </td>
                      </tr>
                    )}
                  </tbody>
                </table>
              </div>
              {(previewErrors ?? []).length > 0 && <p className="text-xs text-warn">{previewErrors!.length} row(s) will be skipped. First: row {previewErrors![0]!.row}, {previewErrors![0]!.message}</p>}
            </div>

            <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
              <Field label="When the draft already has examples">
                <Select
                  value={writeMode}
                  onValueChange={(v) => setWriteMode(v as 'append' | 'replace')}
                  options={[
                    { value: 'append', label: 'Append to the draft' },
                    { value: 'replace', label: 'Replace the draft' },
                  ]}
                />
              </Field>
              <Field label="Tags for every example" optional>
                <TagInput value={extraTags} onChange={setExtraTags} placeholder="e.g. imported" />
              </Field>
            </div>
            <div className="flex items-center justify-between gap-3 rounded-md border border-border p-3">
              <div>
                <div className="text-sm font-medium text-text">Publish a version after importing</div>
                <p className="mt-0.5 text-xs text-text-muted">Freeze the imported draft so suites can pin it.</p>
              </div>
              <Switch aria-label="Publish after import" checked={publish} onCheckedChange={setPublish} />
            </div>
            {error && <ErrorNote message={error} />}
          </div>
        )}

        {step === 2 && (
          <div className="flex flex-col items-center gap-4 py-6 text-center">
            {busy === 'import' ? (
              <>
                <div className="h-1.5 w-64 overflow-hidden rounded-full bg-surface-2" role="progressbar" aria-label="Importing">
                  <div className="h-full w-1/3 animate-shimmer rounded-full bg-accent" style={{ backgroundSize: '200% 100%' }} />
                </div>
                <p className="text-sm text-text-2">Importing and validating rows…</p>
              </>
            ) : error ? (
              <ErrorNote message={error} />
            ) : result ? (
              <>
                <span className="flex size-10 items-center justify-center rounded-full bg-pass-subtle text-pass">
                  <CircleCheck className="size-5" />
                </span>
                <div>
                  <p className="text-md font-semibold text-text">
                    Imported {result.count.toLocaleString()} example{result.count === 1 ? '' : 's'}
                  </p>
                  {result.version && <p className="mt-0.5 text-sm text-text-2">Published as version {result.version.number}.</p>}
                </div>
                {(result.errors ?? []).length > 0 && (
                  <div className="w-full text-left">
                    <p className="mb-2 flex items-center gap-1.5 text-sm font-medium text-warn">
                      <TriangleAlert className="size-4" />
                      {result.errors!.length} row{result.errors!.length === 1 ? '' : 's'} skipped
                    </p>
                    <div className="max-h-48 overflow-auto rounded-md border border-border">
                      <table className="w-full text-sm">
                        <tbody>
                          {result.errors!.slice(0, 100).map((er) => (
                            <tr key={er.row} className="border-b border-border last:border-b-0">
                              <td className="w-20 px-3 py-1.5 tabular text-text-muted">Row {er.row}</td>
                              <td className="px-3 py-1.5 text-text-2">{er.message}</td>
                            </tr>
                          ))}
                        </tbody>
                      </table>
                    </div>
                  </div>
                )}
              </>
            ) : null}
          </div>
        )}
      </div>
    </Dialog>
  )
}

function ErrorNote({ message }: { message: string }) {
  return (
    <div className="flex items-start gap-2 rounded-md border border-fail/30 bg-fail-subtle p-3 text-sm text-fail" role="alert">
      <TriangleAlert className="mt-0.5 size-4 shrink-0" />
      <span className="min-w-0 break-words">{message}</span>
    </div>
  )
}
